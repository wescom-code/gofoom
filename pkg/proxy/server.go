package proxy

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"sync"
	"time"

	"gofoom/pkg/pac"
)

// ProxyServer is the local proxy facade server.
type ProxyServer struct {
	mu           sync.RWMutex
	config       ServerConfig
	stats        Stats
	listener     net.Listener
	httpServer   *http.Server
	transport    *http.Transport
	isRunning    bool
	recentLogs   []LogEvent
	logListeners []chan LogEvent
	logMu        sync.RWMutex
	pacEvaluator *pac.PacEvaluator
}

// NewServer creates a new ProxyServer instance.
func NewServer(cfg ServerConfig) *ProxyServer {
	s := &ProxyServer{
		config: cfg,
	}
	s.initPacEvaluator()
	s.transport = s.createTransport()
	return s
}

// Start opens the local port and starts serving proxy requests.
func (s *ProxyServer) Start() error {
	s.mu.Lock()
	if s.isRunning {
		s.mu.Unlock()
		return fmt.Errorf("proxy server is already running on port %d", s.config.LocalPort)
	}

	bindHost := "127.0.0.1"
	scope := "Local only"
	if s.config.AllowLan {
		bindHost = "0.0.0.0"
		scope = "LAN accessible"
	}
	addr := fmt.Sprintf("%s:%d", bindHost, s.config.LocalPort)
	listener, err := net.Listen("tcp", addr)
	if err != nil {
		s.mu.Unlock()
		return fmt.Errorf("failed to listen on %s: %w", addr, err)
	}

	s.listener = listener
	s.httpServer = &http.Server{
		Handler:      s,
		ReadTimeout:  s.config.ReadTimeout,
		WriteTimeout: s.config.WriteTimeout,
	}
	s.isRunning = true
	s.mu.Unlock()

	s.log(LogEvent{
		Timestamp: time.Now(),
		Level:     "INFO",
		Message:   fmt.Sprintf("Gofoom Proxy Server started on %s (%s, Upstream: %s)", addr, scope, s.config.Upstream.Type),
	})

	go func() {
		if err := s.httpServer.Serve(listener); err != nil && err != http.ErrServerClosed {
			s.log(LogEvent{
				Timestamp: time.Now(),
				Level:     "ERROR",
				Message:   fmt.Sprintf("Server error: %v", err),
			})
			s.mu.Lock()
			s.isRunning = false
			s.mu.Unlock()
		}
	}()

	return nil
}

// Stop gracefully stops the proxy server.
func (s *ProxyServer) Stop() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if !s.isRunning || s.httpServer == nil {
		return nil
	}

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	err := s.httpServer.Shutdown(ctx)
	s.isRunning = false
	if s.transport != nil {
		s.transport.CloseIdleConnections()
	}

	s.log(LogEvent{
		Timestamp: time.Now(),
		Level:     "INFO",
		Message:   "Gofoom Proxy Server stopped",
	})
	return err
}

// IsRunning reports whether the proxy is currently running.
func (s *ProxyServer) IsRunning() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.isRunning
}

// GetConfig returns a copy of current server configuration.
func (s *ProxyServer) GetConfig() ServerConfig {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.config
}

// UpdateConfig updates the configuration and re-initializes transport.
func (s *ProxyServer) UpdateConfig(cfg ServerConfig) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.config = cfg
	s.initPacEvaluator()
	if s.transport != nil {
		s.transport.CloseIdleConnections()
	}
	s.transport = s.createTransport()
}

func (s *ProxyServer) initPacEvaluator() {
	if s.config.Upstream.Type == ProxyTypePAC && s.config.Upstream.PacFileLocation != "" {
		eval, err := pac.NewEvaluatorFromLocation(s.config.Upstream.PacFileLocation)
		if err != nil {
			s.log(LogEvent{
				Timestamp: time.Now(),
				Level:     "WARN",
				Message:   fmt.Sprintf("Failed to load PAC script from %s: %v", s.config.Upstream.PacFileLocation, err),
			})
			s.pacEvaluator = nil
			return
		}
		ttl := time.Duration(s.config.Upstream.BlacklistTimeout) * time.Minute
		if ttl <= 0 {
			ttl = 5 * time.Minute
		}
		eval.SetBlacklistTTL(ttl)
		s.pacEvaluator = eval
		s.log(LogEvent{
			Timestamp: time.Now(),
			Level:     "INFO",
			Message:   fmt.Sprintf("PAC evaluator loaded from %s (Blacklist TTL: %v)", s.config.Upstream.PacFileLocation, ttl),
		})
	} else {
		s.pacEvaluator = nil
	}
}

// ClearPacBlacklist clears the blacklist of unreachable PAC proxies and returns the cleared count.
func (s *ProxyServer) ClearPacBlacklist() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.pacEvaluator != nil {
		cleared := s.pacEvaluator.Blacklist().Clear()
		s.log(LogEvent{
			Timestamp: time.Now(),
			Level:     "INFO",
			Message:   fmt.Sprintf("Cleared %d entries from PAC proxy blacklist", cleared),
		})
		return cleared
	}
	return 0
}

// GetPacEvaluator returns the active PAC evaluator if any.
func (s *ProxyServer) GetPacEvaluator() *pac.PacEvaluator {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.pacEvaluator
}

// Restart restarts the server with potentially new configuration.
func (s *ProxyServer) Restart(cfg ServerConfig) error {
	_ = s.Stop()
	s.UpdateConfig(cfg)
	return s.Start()
}

// GetStats returns current traffic statistics.
func (s *ProxyServer) GetStats() Stats {
	return s.stats
}

func (s *ProxyServer) getTransport() *http.Transport {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.transport
}

// ServeHTTP delegates incoming HTTP requests to handleConnect or handleForward.
func (s *ProxyServer) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodConnect {
		s.handleConnect(w, r)
	} else {
		s.handleForward(w, r)
	}
}

// SubscribeLogs subscribes a channel to receive live LogEvents.
// Returns the event channel and an unsubscribe cancellation function.
func (s *ProxyServer) SubscribeLogs(bufSize int) (<-chan LogEvent, func()) {
	if bufSize <= 0 {
		bufSize = 100
	}
	ch := make(chan LogEvent, bufSize)

	s.logMu.Lock()
	s.logListeners = append(s.logListeners, ch)
	s.logMu.Unlock()

	unsub := func() {
		s.logMu.Lock()
		defer s.logMu.Unlock()
		for i, listener := range s.logListeners {
			if listener == ch {
				s.logListeners = append(s.logListeners, s.logListeners[i+1:]...)
				close(ch)
				break
			}
		}
	}
	return ch, unsub
}

func (s *ProxyServer) log(event LogEvent) {
	s.logMu.Lock()
	if len(s.recentLogs) >= 200 {
		s.recentLogs = s.recentLogs[1:]
	}
	s.recentLogs = append(s.recentLogs, event)
	listeners := make([]chan LogEvent, len(s.logListeners))
	copy(listeners, s.logListeners)
	s.logMu.Unlock()

	for _, ch := range listeners {
		select {
		case ch <- event:
		default:
			// Non-blocking drop if subscriber is too slow
		}
	}
}

// GetRecentLogs returns a snapshot of recent log events.
func (s *ProxyServer) GetRecentLogs() []LogEvent {
	s.logMu.RLock()
	defer s.logMu.RUnlock()
	cp := make([]LogEvent, len(s.recentLogs))
	copy(cp, s.recentLogs)
	return cp
}
