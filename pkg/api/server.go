package api

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"gofoom/pkg/config"
	"gofoom/pkg/proxy"
	"gofoom/pkg/sysproxy"
)

// Server provides the local REST API server compatible with Winfoom's foomcli.
type Server struct {
	mu           sync.Mutex
	port         int
	listener     net.Listener
	httpServer   *http.Server
	proxyServer  *proxy.ProxyServer
	configMgr    *config.ConfigManager
	shutdownFunc func()
	isRunning    bool
}

// NewServer creates a new API server.
func NewServer(port int, ps *proxy.ProxyServer, cm *config.ConfigManager, onShutdown func()) *Server {
	if port <= 0 {
		port = 9999
	}
	return &Server{
		port:         port,
		proxyServer:  ps,
		configMgr:    cm,
		shutdownFunc: onShutdown,
	}
}

// Start launches the API HTTP listener.
func (s *Server) Start() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.isRunning {
		return nil
	}

	addr := fmt.Sprintf("127.0.0.1:%d", s.port)
	listener, err := net.Listen("tcp", addr)
	if err != nil {
		return fmt.Errorf("api: failed to listen on %s: %w", addr, err)
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/status", s.handleStatus)
	mux.HandleFunc("/stats", s.handleStats)
	mux.HandleFunc("/logs", s.handleLogs)
	mux.HandleFunc("/start", s.handleStart)
	mux.HandleFunc("/stop", s.handleStop)
	mux.HandleFunc("/config", s.handleConfig)
	mux.HandleFunc("/autodetect", s.handleAutodetect)
	mux.HandleFunc("/validate", s.handleValidate)
	mux.HandleFunc("/shutdown", s.handleShutdown)

	s.listener = listener
	s.httpServer = &http.Server{
		Handler:      corsMiddleware(mux),
		ReadTimeout:  10 * time.Second,
		WriteTimeout: 10 * time.Second,
	}
	s.isRunning = true

	go func() {
		_ = s.httpServer.Serve(listener)
	}()

	return nil
}

// corsMiddleware adds CORS and Private Network Access headers to support WebView2 & browser requests.
func corsMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization, *")
		w.Header().Set("Access-Control-Allow-Private-Network", "true")
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusOK)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// Stop terminates the API server.
func (s *Server) Stop() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.isRunning || s.httpServer == nil {
		return nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	err := s.httpServer.Shutdown(ctx)
	s.isRunning = false
	return err
}

func (s *Server) handleStatus(w http.ResponseWriter, r *http.Request) {
	state := "stopped"
	if s.proxyServer.IsRunning() {
		state = "up"
	}
	_, _ = fmt.Fprintf(w, "The local proxy server is %s\n", state)
}

func (s *Server) handleStart(w http.ResponseWriter, r *http.Request) {
	if s.proxyServer.IsRunning() {
		_, _ = fmt.Fprintln(w, "The local proxy server is already running")
		return
	}
	sc := s.configMgr.ToServerConfig()
	s.proxyServer.UpdateConfig(sc)
	if err := s.proxyServer.Start(); err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = fmt.Fprintf(w, "Failed to start the local proxy: %v\n", err)
		return
	}
	_, _ = fmt.Fprintln(w, "The local proxy server has been started")
}

func (s *Server) handleStop(w http.ResponseWriter, r *http.Request) {
	if !s.proxyServer.IsRunning() {
		_, _ = fmt.Fprintln(w, "The local proxy server is not running")
		return
	}
	if err := s.proxyServer.Stop(); err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = fmt.Fprintf(w, "Failed to stop the local proxy: %v\n", err)
		return
	}
	_, _ = fmt.Fprintln(w, "The local proxy server has been stopped")
}

func (s *Server) handleConfig(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodGet {
		masked := s.configMgr.GetMasked()
		data, err := json.MarshalIndent(masked, "", "  ")
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(data)
		return
	}

	if r.Method == http.MethodPost {
		if s.proxyServer.IsRunning() {
			_, _ = fmt.Fprintln(w, "The local proxy server is up, you need to stop it before applying configuration")
			return
		}

		body, err := io.ReadAll(r.Body)
		if err != nil {
			http.Error(w, "Failed to read request body", http.StatusBadRequest)
			return
		}

		var newCfg config.AppConfig
		if err := json.Unmarshal(body, &newCfg); err != nil {
			http.Error(w, fmt.Sprintf("Invalid JSON: %v", err), http.StatusBadRequest)
			return
		}

		if err := s.configMgr.Update(newCfg); err != nil {
			http.Error(w, fmt.Sprintf("Failed to save config: %v", err), http.StatusInternalServerError)
			return
		}

		_, _ = fmt.Fprintln(w, "Proxy configuration changed")
	}
}

func (s *Server) handleAutodetect(w http.ResponseWriter, r *http.Request) {
	if s.proxyServer.IsRunning() {
		_, _ = fmt.Fprintln(w, "The local proxy server is up, you need to stop it before auto-detecting configuration")
		return
	}

	dp, err := sysproxy.DetectSystemProxy()
	if err != nil || dp == nil {
		_, _ = fmt.Fprintln(w, "No proxy configuration found on your system")
		return
	}

	cur := s.configMgr.Get()
	cur.ProxyType = dp.Type
	cur.ProxyHost = dp.Host
	cur.ProxyPort = dp.Port
	cur.PacFileLocation = dp.PacURL
	_ = s.configMgr.Update(cur)

	_, _ = fmt.Fprintf(w, "Autodetect succeeded: %+v\n", dp)
}

func (s *Server) handleValidate(w http.ResponseWriter, r *http.Request) {
	if !s.proxyServer.IsRunning() {
		w.WriteHeader(http.StatusServiceUnavailable)
		_, _ = fmt.Fprintln(w, "The local proxy server is down, you need to start it before validating configuration")
		return
	}

	cfg := s.configMgr.Get()
	testURL := r.URL.Query().Get("url")
	if testURL == "" {
		testURL = cfg.ProxyTestURL
	}
	if testURL == "" {
		testURL = "https://example.com"
	}

	// Ensure scheme
	if !strings.HasPrefix(testURL, "http://") && !strings.HasPrefix(testURL, "https://") {
		testURL = "http://" + testURL
	}

	parsedURL, err := url.Parse(testURL)
	if err != nil || parsedURL.Host == "" {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = fmt.Fprintf(w, "Invalid test URL format: %q\n", testURL)
		return
	}

	proxyAddr := fmt.Sprintf("http://127.0.0.1:%d", cfg.LocalPort)
	client := &http.Client{
		Transport: &http.Transport{
			Proxy: http.ProxyURL(&url.URL{Scheme: "http", Host: fmt.Sprintf("127.0.0.1:%d", cfg.LocalPort)}),
		},
		Timeout: 6 * time.Second,
	}

	resp, err := client.Get(testURL)
	if err != nil {
		w.WriteHeader(http.StatusBadGateway)
		_, _ = fmt.Fprintf(w, "Validation failed via %s: %v\n", proxyAddr, err)
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		w.WriteHeader(http.StatusBadGateway)
		switch resp.StatusCode {
		case http.StatusBadGateway:
			_, _ = fmt.Fprintln(w, "Validation failed: Wrong proxy host/port (502 Bad Gateway)")
		case http.StatusProxyAuthRequired:
			_, _ = fmt.Fprintln(w, "Validation failed: Wrong username/password (407 Proxy Authentication Required)")
		case http.StatusGatewayTimeout, http.StatusNotFound:
			_, _ = fmt.Fprintf(w, "Validation failed: Cannot connect to target URL (HTTP %d)\n", resp.StatusCode)
		default:
			_, _ = fmt.Fprintf(w, "Validation failed with status code: %d %s\n", resp.StatusCode, resp.Status)
		}
		return
	}

	_, _ = fmt.Fprintf(w, "The configuration is valid (Connected to %s, Status: %d)\n", parsedURL.Host, resp.StatusCode)
}

func (s *Server) handleShutdown(w http.ResponseWriter, r *http.Request) {
	_, _ = fmt.Fprintln(w, "Shutdown initiated")
	if s.shutdownFunc != nil {
		go func() {
			time.Sleep(200 * time.Millisecond)
			s.shutdownFunc()
		}()
	}
}

func (s *Server) handleStats(w http.ResponseWriter, r *http.Request) {
	st := s.proxyServer.GetStats()
	active := st.ActiveConnections
	if active < 0 {
		active = 0
	}
	resp := struct {
		ActiveConnections int64  `json:"activeConnections"`
		TotalRequests     uint64 `json:"totalRequests"`
		TotalConnects     uint64 `json:"totalConnects"`
		BytesReceived     uint64 `json:"bytesReceived"`
		BytesSent         uint64 `json:"bytesSent"`
		ErrorsCount       uint64 `json:"errorsCount"`
		IsRunning         bool   `json:"isRunning"`
	}{
		ActiveConnections: active,
		TotalRequests:     st.TotalRequests,
		TotalConnects:     st.TotalConnects,
		BytesReceived:     st.BytesReceived,
		BytesSent:         st.BytesSent,
		ErrorsCount:       st.ErrorsCount,
		IsRunning:         s.proxyServer.IsRunning(),
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(resp)
}

func (s *Server) handleLogs(w http.ResponseWriter, r *http.Request) {
	logs := s.proxyServer.GetRecentLogs()
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(logs)
}
