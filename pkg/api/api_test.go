package api

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"gofoom/pkg/config"
	"gofoom/pkg/proxy"
)

func TestApiServerEndpoints(t *testing.T) {
	// Setup proxy server & config manager
	cfgMgr := config.NewConfigManager("test_gofoom.json")
	initialCfg := cfgMgr.ToServerConfig()
	initialCfg.LocalPort = 13135
	proxySrv := proxy.NewServer(initialCfg)

	shutdownCalled := false
	apiSrv := NewServer(19999, proxySrv, cfgMgr, func() {
		shutdownCalled = true
	})

	if err := apiSrv.Start(); err != nil {
		t.Fatalf("Failed to start API server: %v", err)
	}
	defer apiSrv.Stop()

	time.Sleep(50 * time.Millisecond)

	apiURL := "http://127.0.0.1:19999"

	// 1. Check initial status -> stopped
	resp, err := http.Get(apiURL + "/status")
	if err != nil {
		t.Fatalf("Failed to GET /status: %v", err)
	}
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if !strings.Contains(string(body), "stopped") {
		t.Errorf("Expected stopped status, got %s", string(body))
	}

	// 2. Start proxy via /start
	resp, err = http.Post(apiURL+"/start", "text/plain", nil)
	if err != nil {
		t.Fatalf("Failed to POST /start: %v", err)
	}
	body, _ = io.ReadAll(resp.Body)
	resp.Body.Close()
	if !strings.Contains(string(body), "started") {
		t.Errorf("Expected started message, got %s", string(body))
	}

	// 3. Check status -> up
	resp, err = http.Get(apiURL + "/status")
	if err != nil {
		t.Fatalf("Failed to GET /status: %v", err)
	}
	body, _ = io.ReadAll(resp.Body)
	resp.Body.Close()
	if !strings.Contains(string(body), "up") {
		t.Errorf("Expected up status, got %s", string(body))
	}

	// 4. Stop proxy via /stop
	resp, err = http.Post(apiURL+"/stop", "text/plain", nil)
	if err != nil {
		t.Fatalf("Failed to POST /stop: %v", err)
	}
	body, _ = io.ReadAll(resp.Body)
	resp.Body.Close()
	if !strings.Contains(string(body), "stopped") {
		t.Errorf("Expected stopped message, got %s", string(body))
	}

	// 5. GET /config
	resp, err = http.Get(apiURL + "/config")
	if err != nil {
		t.Fatalf("Failed to GET /config: %v", err)
	}
	var appCfg config.AppConfig
	if err := json.NewDecoder(resp.Body).Decode(&appCfg); err != nil {
		t.Fatalf("Failed to decode JSON config: %v", err)
	}
	resp.Body.Close()
	if appCfg.LocalPort != 3129 {
		t.Errorf("Expected default LocalPort 3129, got %d", appCfg.LocalPort)
	}

	// 6. POST /config
	appCfg.ProxyType = "HTTP"
	appCfg.ProxyHost = "192.168.1.100"
	appCfg.ProxyPort = 8080
	cfgData, _ := json.Marshal(appCfg)

	resp, err = http.Post(apiURL+"/config", "application/json", bytes.NewReader(cfgData))
	if err != nil {
		t.Fatalf("Failed to POST /config: %v", err)
	}
	body, _ = io.ReadAll(resp.Body)
	resp.Body.Close()
	if !strings.Contains(string(body), "changed") {
		t.Errorf("Expected changed message, got %s", string(body))
	}

	// 7. Test /shutdown
	resp, err = http.Post(apiURL+"/shutdown", "text/plain", nil)
	if err != nil {
		t.Fatalf("Failed to POST /shutdown: %v", err)
	}
	resp.Body.Close()
	time.Sleep(300 * time.Millisecond)
	if !shutdownCalled {
		t.Errorf("Expected shutdown callback to be triggered")
	}
}
