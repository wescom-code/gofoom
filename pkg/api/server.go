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

// Port returns the active bound API port.
func (s *Server) Port() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.port
}

// Start launches the API HTTP listener.
func (s *Server) Start() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.isRunning {
		return nil
	}

	var listener net.Listener
	var err error
	actualPort := s.port

	bindHost := "127.0.0.1"
	if s.configMgr != nil && s.configMgr.Get().AllowLan {
		bindHost = "0.0.0.0"
	}

	// Try requested port; if occupied, search for next available port (up to +20)
	for i := 0; i < 20; i++ {
		addr := fmt.Sprintf("%s:%d", bindHost, actualPort)
		listener, err = net.Listen("tcp", addr)
		if err == nil {
			break
		}
		actualPort++
	}

	if err != nil {
		return fmt.Errorf("api: failed to listen on %s:%d-%d: %w", bindHost, s.port, actualPort-1, err)
	}

	s.port = actualPort

	mux := http.NewServeMux()
	mux.HandleFunc("/", s.handleRoot)
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

// Rebind restarts the API server to apply updated network listening bindings.
func (s *Server) Rebind() error {
	_ = s.Stop()
	return s.Start()
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
		cur := s.configMgr.Get()

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

		lanChanged := newCfg.AllowLan != cur.AllowLan
		portChanged := newCfg.LocalPort != cur.LocalPort

		if err := s.configMgr.Update(newCfg); err != nil {
			http.Error(w, fmt.Sprintf("Failed to save config: %v", err), http.StatusInternalServerError)
			return
		}

		if s.proxyServer.IsRunning() {
			sc := s.configMgr.ToServerConfig()
			if lanChanged || portChanged {
				_ = s.proxyServer.Restart(sc)
			} else {
				s.proxyServer.UpdateConfig(sc)
			}
		}

		if lanChanged {
			time.AfterFunc(150*time.Millisecond, func() {
				_ = s.Rebind()
			})
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

func (s *Server) handleRoot(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	statusStr := "STOPPED"
	if s.proxyServer.IsRunning() {
		statusStr = "RUNNING"
	}
	cfg := s.configMgr.Get()
	stats := s.proxyServer.GetStats()
	listenDesc := "127.0.0.1 (Local Only)"
	if cfg.AllowLan {
		listenDesc = "0.0.0.0 (LAN Accessible)"
	}

	html := fmt.Sprintf(`<!DOCTYPE html>
<html>
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>Gofoom Management API</title>
<style>
body { font-family: -apple-system, BlinkMacSystemFont, "Segoe UI", Roboto, sans-serif; background: #0f172a; color: #f8fafc; margin: 0; padding: 40px 20px; display: flex; justify-content: center; }
.card { background: #1e293b; border: 1px solid #334155; border-radius: 12px; max-width: 600px; width: 100%%; padding: 28px; box-shadow: 0 10px 25px rgba(0,0,0,0.4); }
h1 { margin-top: 0; color: #60a5fa; font-size: 24px; display: flex; align-items: center; justify-content: space-between; }
.badge { display: inline-block; padding: 4px 12px; border-radius: 20px; font-size: 13px; font-weight: 600; }
.running { background: rgba(16, 185, 129, 0.2); color: #34d399; border: 1px solid #10b981; }
.stopped { background: rgba(148, 163, 184, 0.2); color: #94a3b8; border: 1px solid #64748b; }
.table { width: 100%%; margin: 20px 0; border-collapse: collapse; font-size: 14px; }
.table td { padding: 10px 0; border-bottom: 1px solid #334155; }
.table td:first-child { color: #94a3b8; width: 42%%; }
.links { display: flex; flex-wrap: wrap; gap: 10px; margin-top: 20px; }
.link-btn { display: inline-block; padding: 8px 16px; background: #334155; color: #38bdf8; text-decoration: none; border-radius: 6px; font-size: 13px; font-weight: 600; transition: all 0.2s; }
.link-btn:hover { background: #475569; color: #fff; }
</style>
</head>
<body>
<div class="card">
  <h1>Gofoom API <span class="badge %s">%s</span></h1>
  <p style="color:#94a3b8; margin: 0 0 16px 0;">Local proxy facade and management service (v1.1.0) is active.</p>
  <table class="table">
    <tr><td>Proxy Status</td><td><b>%s</b> on port <b>%d</b></td></tr>
    <tr><td>Listening Interface</td><td>%s</td></tr>
    <tr><td>Upstream Proxy Type</td><td>%s</td></tr>
    <tr><td>Active Connections</td><td>%d</td></tr>
    <tr><td>Total Requests Processed</td><td>%d</td></tr>
    <tr><td>Data Transferred (In / Out)</td><td>%.2f KB / %.2f KB</td></tr>
  </table>
  <div style="font-weight: 600; margin-bottom: 8px; color: #e2e8f0;">Available API Endpoints:</div>
  <div class="links">
    <a class="link-btn" href="/status">/status</a>
    <a class="link-btn" href="/stats">/stats</a>
    <a class="link-btn" href="/config">/config</a>
    <a class="link-btn" href="/logs">/logs</a>
  </div>
</div>
</body>
</html>`,
		strings.ToLower(statusStr), statusStr,
		statusStr, cfg.LocalPort,
		listenDesc,
		cfg.ProxyType,
		stats.ActiveConnections,
		stats.TotalRequests,
		float64(stats.BytesReceived)/1024.0,
		float64(stats.BytesSent)/1024.0,
	)
	_, _ = w.Write([]byte(html))
}
