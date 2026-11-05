package proxy

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"
)

func TestDirectNonConnectForward(t *testing.T) {
	// 1. Setup mock target HTTP server
	targetServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Custom-Echo", "Gofoom-Pass")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("Hello from target backend! Method: " + r.Method))
	}))
	defer targetServer.Close()

	// 2. Start Gofoom Proxy Server on a free local port
	cfg := DefaultServerConfig()
	cfg.LocalPort = 13129
	cfg.Upstream = UpstreamConfig{
		Type: ProxyTypeDirect,
	}

	proxySrv := NewServer(cfg)
	if err := proxySrv.Start(); err != nil {
		t.Fatalf("Failed to start proxy server: %v", err)
	}
	defer proxySrv.Stop()

	time.Sleep(50 * time.Millisecond) // Allow listener to settle

	// 3. Create client configured with Gofoom as its proxy
	proxyURL, _ := url.Parse(fmt.Sprintf("http://127.0.0.1:%d", cfg.LocalPort))
	client := &http.Client{
		Transport: &http.Transport{
			Proxy: http.ProxyURL(proxyURL),
		},
		Timeout: 5 * time.Second,
	}

	// 4. Send GET request through proxy
	resp, err := client.Get(targetServer.URL + "/test-path")
	if err != nil {
		t.Fatalf("Client GET through proxy failed: %v", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("Failed to read response body: %v", err)
	}

	if resp.StatusCode != http.StatusOK {
		t.Errorf("Expected status 200, got %d", resp.StatusCode)
	}

	if !strings.Contains(string(body), "Hello from target backend! Method: GET") {
		t.Errorf("Unexpected body content: %s", string(body))
	}

	if resp.Header.Get("X-Custom-Echo") != "Gofoom-Pass" {
		t.Errorf("Missing custom header from target")
	}

	// Check stats
	stats := proxySrv.GetStats()
	if stats.TotalRequests == 0 {
		t.Errorf("Expected stats.TotalRequests > 0, got %d", stats.TotalRequests)
	}
	if stats.BytesIn() == 0 && stats.BytesReceived == 0 {
		t.Errorf("Expected bytes received > 0")
	}
}

func (s *Stats) BytesIn() uint64 {
	return s.BytesReceived
}

func TestDirectConnectTunnel(t *testing.T) {
	// 1. Setup mock target TLS/HTTPS server
	targetServer := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("Secure TLS payload passed through Gofoom tunnel!"))
	}))
	defer targetServer.Close()

	// 2. Start Gofoom Proxy Server
	cfg := DefaultServerConfig()
	cfg.LocalPort = 13130
	cfg.Upstream = UpstreamConfig{
		Type: ProxyTypeDirect,
	}

	proxySrv := NewServer(cfg)
	if err := proxySrv.Start(); err != nil {
		t.Fatalf("Failed to start proxy server: %v", err)
	}
	defer proxySrv.Stop()

	time.Sleep(50 * time.Millisecond)

	// 3. Create client with Gofoom proxy and trust target TLS cert
	proxyURL, _ := url.Parse(fmt.Sprintf("http://127.0.0.1:%d", cfg.LocalPort))
	client := targetServer.Client()
	client.Transport.(*http.Transport).Proxy = http.ProxyURL(proxyURL)

	// 4. Send HTTPS request through proxy (triggers HTTP CONNECT)
	resp, err := client.Get(targetServer.URL + "/secure-endpoint")
	if err != nil {
		t.Fatalf("Client HTTPS through proxy tunnel failed: %v", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("Failed to read response body: %v", err)
	}

	if resp.StatusCode != http.StatusOK {
		t.Errorf("Expected status 200, got %d", resp.StatusCode)
	}

	if !strings.Contains(string(body), "Secure TLS payload") {
		t.Errorf("Unexpected body content: %s", string(body))
	}

	// Verify stats
	stats := proxySrv.GetStats()
	if stats.TotalConnects == 0 {
		t.Errorf("Expected stats.TotalConnects > 0, got %d", stats.TotalConnects)
	}
	if stats.TotalRequests == 0 {
		t.Errorf("Expected stats.TotalRequests > 0, got %d", stats.TotalRequests)
	}
	if stats.BytesReceived == 0 {
		t.Errorf("Expected stats.BytesReceived > 0, got %d", stats.BytesReceived)
	}
	if stats.BytesSent == 0 {
		t.Errorf("Expected stats.BytesSent > 0, got %d", stats.BytesSent)
	}
}

func TestPacProxyFailoverAndBlacklist(t *testing.T) {
	// 1. Setup mock target TLS/HTTPS server
	targetServer := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("PAC failover to direct succeeded!"))
	}))
	defer targetServer.Close()

	// 2. Create a local PAC script pointing to a non-existent port first, then DIRECT
	pacScript := fmt.Sprintf(`function FindProxyForURL(url, host) {
		return "PROXY 127.0.0.1:54321; DIRECT";
	}`)
	pacFile := t.TempDir() + "/test.pac"
	if err := os.WriteFile(pacFile, []byte(pacScript), 0644); err != nil {
		t.Fatalf("Failed to write test PAC file: %v", err)
	}

	// 3. Start Gofoom Proxy Server configured with PAC
	cfg := DefaultServerConfig()
	cfg.LocalPort = 13131
	cfg.ConnectTimeout = 1 * time.Second
	cfg.Upstream = UpstreamConfig{
		Type:             ProxyTypePAC,
		PacFileLocation:  pacFile,
		BlacklistTimeout: 5,
	}

	proxySrv := NewServer(cfg)
	if err := proxySrv.Start(); err != nil {
		t.Fatalf("Failed to start proxy server: %v", err)
	}
	defer proxySrv.Stop()

	time.Sleep(50 * time.Millisecond)

	// 4. Client sends HTTPS request through Gofoom proxy
	proxyURL, _ := url.Parse(fmt.Sprintf("http://127.0.0.1:%d", cfg.LocalPort))
	client := targetServer.Client()
	client.Transport.(*http.Transport).Proxy = http.ProxyURL(proxyURL)

	resp, err := client.Get(targetServer.URL + "/pac-test")
	if err != nil {
		t.Fatalf("Client request failed during PAC failover: %v", err)
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	if !strings.Contains(string(body), "PAC failover to direct succeeded!") {
		t.Errorf("Unexpected body: %s", string(body))
	}

	// 5. Verify that 127.0.0.1:54321 was added to the PAC blacklist
	eval := proxySrv.GetPacEvaluator()
	if eval == nil {
		t.Fatalf("Expected non-nil PacEvaluator")
	}
	if !eval.Blacklist().IsBlacklisted("127.0.0.1", 54321) {
		t.Errorf("Expected 127.0.0.1:54321 to be blacklisted after failure")
	}

	// 6. Test ClearPacBlacklist
	cleared := proxySrv.ClearPacBlacklist()
	if cleared != 1 {
		t.Errorf("Expected 1 entry cleared, got %d", cleared)
	}
	if eval.Blacklist().IsBlacklisted("127.0.0.1", 54321) {
		t.Errorf("Expected 127.0.0.1:54321 to no longer be blacklisted after ClearPacBlacklist")
	}
}

func TestRestartAndStatsCounting(t *testing.T) {
	targetServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("Payload 1234567890"))
	}))
	defer targetServer.Close()

	cfg := DefaultServerConfig()
	cfg.LocalPort = 13132
	proxySrv := NewServer(cfg)
	if err := proxySrv.Start(); err != nil {
		t.Fatalf("Start 1 failed: %v", err)
	}

	proxyURL, _ := url.Parse(fmt.Sprintf("http://127.0.0.1:%d", cfg.LocalPort))
	client := &http.Client{
		Transport: &http.Transport{
			Proxy: http.ProxyURL(proxyURL),
		},
		Timeout: 3 * time.Second,
	}

	// First request
	resp, err := client.Get(targetServer.URL + "/req1")
	if err != nil {
		t.Fatalf("Req 1 failed: %v", err)
	}
	_, _ = io.ReadAll(resp.Body)
	resp.Body.Close()

	st1 := proxySrv.GetStats()
	t.Logf("st1: In=%d, Out=%d, Req=%d", st1.BytesReceived, st1.BytesSent, st1.TotalRequests)
	if st1.BytesReceived == 0 {
		t.Errorf("Expected st1.BytesReceived > 0")
	}

	// Stop then Start
	if err := proxySrv.Stop(); err != nil {
		t.Fatalf("Stop failed: %v", err)
	}
	cfg.AllowLan = true
	proxySrv.UpdateConfig(cfg)
	if err := proxySrv.Start(); err != nil {
		t.Fatalf("Start 2 failed: %v", err)
	}
	defer proxySrv.Stop()

	// Second request
	resp2, err := client.Get(targetServer.URL + "/req2")
	if err != nil {
		t.Fatalf("Req 2 failed: %v", err)
	}
	_, _ = io.ReadAll(resp2.Body)
	resp2.Body.Close()

	st2 := proxySrv.GetStats()
	t.Logf("st2: In=%d, Out=%d, Req=%d", st2.BytesReceived, st2.BytesSent, st2.TotalRequests)
	if st2.BytesReceived <= st1.BytesReceived {
		t.Errorf("Expected st2.BytesReceived > st1.BytesReceived, got st1=%d st2=%d", st1.BytesReceived, st2.BytesReceived)
	}
}

func TestConnectTunnelRestartAndStatsCounting(t *testing.T) {
	targetServer := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("TLS payload 1234567890"))
	}))
	defer targetServer.Close()

	cfg := DefaultServerConfig()
	cfg.LocalPort = 13133
	proxySrv := NewServer(cfg)
	if err := proxySrv.Start(); err != nil {
		t.Fatalf("Start 1 failed: %v", err)
	}

	proxyURL, _ := url.Parse(fmt.Sprintf("http://127.0.0.1:%d", cfg.LocalPort))
	client := targetServer.Client()
	client.Transport.(*http.Transport).Proxy = http.ProxyURL(proxyURL)

	// First request
	resp, err := client.Get(targetServer.URL + "/tls1")
	if err != nil {
		t.Fatalf("Req 1 failed: %v", err)
	}
	_, _ = io.ReadAll(resp.Body)
	resp.Body.Close()

	st1 := proxySrv.GetStats()
	t.Logf("st1: In=%d, Out=%d, Connects=%d, Req=%d", st1.BytesReceived, st1.BytesSent, st1.TotalConnects, st1.TotalRequests)
	if st1.BytesReceived == 0 || st1.BytesSent == 0 {
		t.Errorf("Expected st1.BytesReceived > 0 and st1.BytesSent > 0")
	}

	// Stop then Start
	if err := proxySrv.Stop(); err != nil {
		t.Fatalf("Stop failed: %v", err)
	}
	cfg.AllowLan = true
	proxySrv.UpdateConfig(cfg)
	if err := proxySrv.Start(); err != nil {
		t.Fatalf("Start 2 failed: %v", err)
	}
	defer proxySrv.Stop()

	// Second request
	resp2, err := client.Get(targetServer.URL + "/tls2")
	if err != nil {
		t.Fatalf("Req 2 failed: %v", err)
	}
	_, _ = io.ReadAll(resp2.Body)
	resp2.Body.Close()

	st2 := proxySrv.GetStats()
	t.Logf("st2: In=%d, Out=%d, Connects=%d, Req=%d", st2.BytesReceived, st2.BytesSent, st2.TotalConnects, st2.TotalRequests)
	if st2.BytesReceived <= st1.BytesReceived || st2.BytesSent <= st1.BytesSent {
		t.Errorf("Expected st2 > st1, got st1=(In:%d, Out:%d) st2=(In:%d, Out:%d)",
			st1.BytesReceived, st1.BytesSent, st2.BytesReceived, st2.BytesSent)
	}
}

func TestForwardNoXForwardedFor(t *testing.T) {
	var receivedXFF string
	targetServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedXFF = r.Header.Get("X-Forwarded-For")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	}))
	defer targetServer.Close()

	cfg := DefaultServerConfig()
	cfg.LocalPort = 13135
	cfg.Upstream = UpstreamConfig{Type: ProxyTypeDirect}
	proxySrv := NewServer(cfg)
	if err := proxySrv.Start(); err != nil {
		t.Fatalf("Failed to start proxy server: %v", err)
	}
	defer proxySrv.Stop()
	time.Sleep(50 * time.Millisecond)

	proxyURL, _ := url.Parse(fmt.Sprintf("http://127.0.0.1:%d", cfg.LocalPort))
	client := &http.Client{
		Transport: &http.Transport{
			Proxy: http.ProxyURL(proxyURL),
		},
		Timeout: 5 * time.Second,
	}

	resp, err := client.Get(targetServer.URL + "/test-ha")
	if err != nil {
		t.Fatalf("Get failed: %v", err)
	}
	resp.Body.Close()

	if receivedXFF != "" {
		t.Errorf("Expected NO X-Forwarded-For header, got: %q", receivedXFF)
	}
}

func TestWebSocketUpgradeForward(t *testing.T) {
	targetServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.EqualFold(r.Header.Get("Upgrade"), "websocket") {
			http.Error(w, "Not websocket", http.StatusBadRequest)
			return
		}
		hijacker, ok := w.(http.Hijacker)
		if !ok {
			http.Error(w, "Hijack not supported", http.StatusInternalServerError)
			return
		}
		conn, buf, err := hijacker.Hijack()
		if err != nil {
			return
		}
		defer conn.Close()

		// Send 101 Switching Protocols response
		_, _ = conn.Write([]byte("HTTP/1.1 101 Switching Protocols\r\nUpgrade: websocket\r\nConnection: Upgrade\r\n\r\n"))

		// Read client data and echo it back
		var data [64]byte
		n, err := buf.Read(data[:])
		if n > 0 {
			_, _ = conn.Write(append([]byte("ECHO: "), data[:n]...))
		}
	}))
	defer targetServer.Close()

	cfg := DefaultServerConfig()
	cfg.LocalPort = 13136
	cfg.Upstream = UpstreamConfig{Type: ProxyTypeDirect}
	proxySrv := NewServer(cfg)
	if err := proxySrv.Start(); err != nil {
		t.Fatalf("Failed to start proxy server: %v", err)
	}
	defer proxySrv.Stop()
	time.Sleep(50 * time.Millisecond)

	// Connect raw TCP client to proxy
	clientConn, err := net.Dial("tcp", fmt.Sprintf("127.0.0.1:%d", cfg.LocalPort))
	if err != nil {
		t.Fatalf("Dial proxy failed: %v", err)
	}
	defer clientConn.Close()

	// Send forward proxy request with Upgrade header
	u, _ := url.Parse(targetServer.URL)
	req := fmt.Sprintf("GET %s/api/websocket HTTP/1.1\r\nHost: %s\r\nUpgrade: websocket\r\nConnection: Upgrade\r\nSec-WebSocket-Key: dGVzdA==\r\n\r\n", targetServer.URL, u.Host)
	if _, err := clientConn.Write([]byte(req)); err != nil {
		t.Fatalf("Write request failed: %v", err)
	}

	reader := bufio.NewReader(clientConn)
	statusLine, err := reader.ReadString('\n')
	if err != nil {
		t.Fatalf("Read status line failed: %v", err)
	}
	if !strings.Contains(statusLine, "101") {
		t.Fatalf("Expected 101 Switching Protocols, got: %s", statusLine)
	}

	// Consume headers until blank line
	for {
		line, err := reader.ReadString('\n')
		if err != nil || strings.TrimSpace(line) == "" {
			break
		}
	}

	// Send frame/payload over full-duplex tunnel
	msg := []byte("ping-homeassistant-ws")
	if _, err := clientConn.Write(msg); err != nil {
		t.Fatalf("Write ws msg failed: %v", err)
	}

	reply := make([]byte, 64)
	n, err := reader.Read(reply)
	if err != nil {
		t.Fatalf("Read ws echo failed: %v", err)
	}
	if !strings.Contains(string(reply[:n]), "ECHO: ping-homeassistant-ws") {
		t.Errorf("Unexpected reply: %s", string(reply[:n]))
	}

	// Verify stats counted the duplex traffic
	stats := proxySrv.GetStats()
	if stats.BytesReceived == 0 || stats.BytesSent == 0 {
		t.Errorf("Expected traffic counted, got In=%d, Out=%d", stats.BytesReceived, stats.BytesSent)
	}
}

func TestClientCanceledDoesNotBlacklistPacProxy(t *testing.T) {
	// 1. Setup a healthy but slow mock upstream proxy
	mockProxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(300 * time.Millisecond)
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("mock response"))
	}))
	defer mockProxy.Close()

	uProxy, _ := url.Parse(mockProxy.URL)
	var proxyPort int
	fmt.Sscanf(uProxy.Port(), "%d", &proxyPort)

	// 2. PAC script pointing to this healthy mock proxy
	pacScript := fmt.Sprintf(`function FindProxyForURL(url, host) {
		return "PROXY 127.0.0.1:%d; DIRECT";
	}`, proxyPort)
	pacFile := t.TempDir() + "/test_cancel.pac"
	if err := os.WriteFile(pacFile, []byte(pacScript), 0644); err != nil {
		t.Fatalf("Failed to write test PAC file: %v", err)
	}

	cfg := DefaultServerConfig()
	cfg.LocalPort = 13137
	cfg.ConnectTimeout = 2 * time.Second
	cfg.Upstream = UpstreamConfig{
		Type:             ProxyTypePAC,
		PacFileLocation:  pacFile,
		BlacklistTimeout: 5,
	}

	proxySrv := NewServer(cfg)
	if err := proxySrv.Start(); err != nil {
		t.Fatalf("Failed to start proxy server: %v", err)
	}
	defer proxySrv.Stop()
	time.Sleep(50 * time.Millisecond)

	// 3. Client sends request with a short 20ms timeout so context cancels during request
	proxyURL, _ := url.Parse(fmt.Sprintf("http://127.0.0.1:%d", cfg.LocalPort))
	client := &http.Client{
		Transport: &http.Transport{
			Proxy: http.ProxyURL(proxyURL),
		},
	}

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()

	req, _ := http.NewRequestWithContext(ctx, "GET", "http://example.com/test-cancel", nil)
	_, _ = client.Do(req)

	time.Sleep(50 * time.Millisecond)

	// 4. Verify that the healthy mockProxy was NOT blacklisted!
	eval := proxySrv.GetPacEvaluator()
	if eval == nil {
		t.Fatalf("Expected non-nil PacEvaluator")
	}
	if eval.Blacklist().IsBlacklisted("127.0.0.1", proxyPort) {
		t.Errorf("Expected 127.0.0.1:%d NOT to be blacklisted when client canceled the request", proxyPort)
	}

	// 5. Verify that errors were not incremented
	stats := proxySrv.GetStats()
	if stats.ErrorsCount != 0 {
		t.Errorf("Expected ErrorsCount == 0 for client canceled request, got %d", stats.ErrorsCount)
	}
}
