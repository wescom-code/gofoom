package proxy

import (
	"fmt"
	"io"
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
