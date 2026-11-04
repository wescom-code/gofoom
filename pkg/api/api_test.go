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
	appCfg := cfgMgr.Get()
	appCfg.LocalPort = 13135
	_ = cfgMgr.Update(appCfg)
	initialCfg := cfgMgr.ToServerConfig()
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
	var readCfg config.AppConfig
	if err := json.NewDecoder(resp.Body).Decode(&readCfg); err != nil {
		t.Fatalf("Failed to decode JSON config: %v", err)
	}
	resp.Body.Close()
	if readCfg.LocalPort != 13135 {
		t.Errorf("Expected LocalPort 13135, got %d", readCfg.LocalPort)
	}

	// 6. POST /config
	updateCfg := readCfg
	updateCfg.ProxyType = "HTTP"
	updateCfg.ProxyHost = "192.168.1.100"
	updateCfg.ProxyPort = 8080
	cfgData, _ := json.Marshal(updateCfg)

	resp, err = http.Post(apiURL+"/config", "application/json", bytes.NewReader(cfgData))
	if err != nil {
		t.Fatalf("Failed to POST /config: %v", err)
	}
	body, _ = io.ReadAll(resp.Body)
	resp.Body.Close()
	if !strings.Contains(string(body), "changed") {
		t.Errorf("Expected changed message, got %s", string(body))
	}

	// 7. Test CORS Preflight OPTIONS
	req, _ := http.NewRequest(http.MethodOptions, apiURL+"/config", nil)
	req.Header.Set("Origin", "http://wails.localhost")
	req.Header.Set("Access-Control-Request-Method", "POST")
	req.Header.Set("Access-Control-Request-Private-Network", "true")
	resp, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("Failed to execute OPTIONS preflight: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Errorf("Expected 200 for OPTIONS preflight, got %d", resp.StatusCode)
	}
	if resp.Header.Get("Access-Control-Allow-Origin") != "*" {
		t.Errorf("Expected Access-Control-Allow-Origin: *")
	}
	if resp.Header.Get("Access-Control-Allow-Private-Network") != "true" {
		t.Errorf("Expected Access-Control-Allow-Private-Network: true")
	}

	// 8. Test /validate while proxy is down
	resp, err = http.Get(apiURL + "/validate")
	if err != nil {
		t.Fatalf("Failed to GET /validate: %v", err)
	}
	body, _ = io.ReadAll(resp.Body)
	resp.Body.Close()
	if !strings.Contains(string(body), "down") {
		t.Errorf("Expected down message for /validate, got %s", string(body))
	}

	// 9. Test /stats
	resp, err = http.Get(apiURL + "/stats")
	if err != nil {
		t.Fatalf("Failed to GET /stats: %v", err)
	}
	var statMap map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&statMap); err != nil {
		t.Fatalf("Failed to decode /stats JSON: %v", err)
	}
	resp.Body.Close()
	if _, ok := statMap["activeConnections"]; !ok {
		t.Errorf("Expected activeConnections in /stats")
	}
	if _, ok := statMap["totalRequests"]; !ok {
		t.Errorf("Expected totalRequests in /stats")
	}

	// 10. Test /logs
	resp, err = http.Get(apiURL + "/logs")
	if err != nil {
		t.Fatalf("Failed to GET /logs: %v", err)
	}
	var logsList []map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&logsList); err != nil {
		t.Fatalf("Failed to decode /logs JSON: %v", err)
	}
	resp.Body.Close()

	// 10b. Test /blacklist/clear
	resp, err = http.Post(apiURL+"/blacklist/clear", "application/json", nil)
	if err != nil {
		t.Fatalf("Failed to POST /blacklist/clear: %v", err)
	}
	var clearResp map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&clearResp); err != nil {
		t.Fatalf("Failed to decode /blacklist/clear JSON: %v", err)
	}
	resp.Body.Close()
	if _, ok := clearResp["cleared"]; !ok {
		t.Errorf("Expected cleared key in response")
	}

	// 11. Test /shutdown
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
