package api

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"gofoom/pkg/config"
	"gofoom/pkg/proxy"
	"gofoom/pkg/version"
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
	updateCfg.MemoryPolicy = "hide"
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

	// Verify MemoryPolicy was updated
	if cfgMgr.Get().MemoryPolicy != "hide" {
		t.Errorf("Expected MemoryPolicy 'hide', got '%s'", cfgMgr.Get().MemoryPolicy)
	}

	// 6b. Test /preferences endpoint (Get & Post)
	resp, err = http.Get(apiURL + "/preferences")
	if err != nil {
		t.Fatalf("Failed to GET /preferences: %v", err)
	}
	var prefResp map[string]interface{}
	_ = json.NewDecoder(resp.Body).Decode(&prefResp)
	resp.Body.Close()
	if prefResp["memoryPolicy"] != "hide" {
		t.Errorf("Expected GET /preferences memoryPolicy 'hide', got %v", prefResp["memoryPolicy"])
	}

	prefData, _ := json.Marshal(config.UserPreferences{
		AutoStart:      true,
		StartMinimized: true,
		MemoryPolicy:   "none",
		UILayoutMode:   "split",
	})
	resp, err = http.Post(apiURL+"/preferences", "application/json", bytes.NewReader(prefData))
	if err != nil {
		t.Fatalf("Failed to POST /preferences: %v", err)
	}
	body, _ = io.ReadAll(resp.Body)
	resp.Body.Close()
	if !strings.Contains(string(body), "saved successfully") {
		t.Errorf("Expected saved message, got %s", string(body))
	}
	if cfgMgr.Get().MemoryPolicy != "none" {
		t.Errorf("Expected MemoryPolicy 'none', got '%s'", cfgMgr.Get().MemoryPolicy)
	}
	if !cfgMgr.Get().AutoStart {
		t.Errorf("Expected AutoStart true, got false")
	}
	if !cfgMgr.Get().StartMinimized {
		t.Errorf("Expected StartMinimized true, got false")
	}
	if cfgMgr.Get().UILayoutMode != "split" {
		t.Errorf("Expected UILayoutMode 'split', got '%s'", cfgMgr.Get().UILayoutMode)
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

	// 10c. Test Rebind preserves port!
	oldPort := apiSrv.Port()
	if err := apiSrv.Rebind(); err != nil {
		t.Fatalf("Rebind failed: %v", err)
	}
	if apiSrv.Port() != oldPort {
		t.Errorf("PORT DRIFTED! Expected port %d after Rebind, got %d", oldPort, apiSrv.Port())
	}

	// 10d. Test /version endpoint
	resp, err = http.Get(apiURL + "/version")
	if err != nil {
		t.Fatalf("Failed to GET /version: %v", err)
	}
	var verInfo map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&verInfo); err != nil {
		t.Fatalf("Failed to decode /version JSON: %v", err)
	}
	resp.Body.Close()
	if verInfo["version"] != version.Version {
		t.Errorf("Expected version %s, got %v", version.Version, verInfo["version"])
	}
	if verInfo["name"] != version.Name {
		t.Errorf("Expected name %s, got %v", version.Name, verInfo["name"])
	}

	// 10e. Test HTML dashboard contains dynamic version
	resp, err = http.Get(apiURL + "/")
	if err != nil {
		t.Fatalf("Failed to GET /: %v", err)
	}
	body, _ = io.ReadAll(resp.Body)
	resp.Body.Close()
	expectedVerTag := fmt.Sprintf("(v%s)", version.Version)
	if !strings.Contains(string(body), expectedVerTag) {
		t.Errorf("Expected HTML dashboard to contain %s, got: %s", expectedVerTag, string(body))
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

func TestAllowLanToggleAndStats(t *testing.T) {
	targetServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("Target server response 1234567890"))
	}))
	defer targetServer.Close()

	cfgFile := t.TempDir() + "/gofoom.json"
	cfgMgr := config.NewConfigManager(cfgFile)
	appCfg := cfgMgr.Get()
	appCfg.LocalPort = 13136
	appCfg.AllowLan = false
	_ = cfgMgr.Update(appCfg)

	initialCfg := cfgMgr.ToServerConfig()
	proxySrv := proxy.NewServer(initialCfg)

	apiSrv := NewServer(19998, proxySrv, cfgMgr, nil)
	if err := apiSrv.Start(); err != nil {
		t.Fatalf("Failed to start API server: %v", err)
	}
	defer apiSrv.Stop()

	apiURL := "http://127.0.0.1:19998"

	// Start proxy
	resp, err := http.Post(apiURL+"/start", "text/plain", nil)
	if err != nil {
		t.Fatalf("Failed to start proxy: %v", err)
	}
	resp.Body.Close()

	proxyURL, _ := url.Parse("http://127.0.0.1:13136")
	client := &http.Client{
		Transport: &http.Transport{Proxy: http.ProxyURL(proxyURL)},
		Timeout:   3 * time.Second,
	}

	// 1. Request 1
	r1, err := client.Get(targetServer.URL + "/req1")
	if err != nil {
		t.Fatalf("Req 1 failed: %v", err)
	}
	_, _ = io.ReadAll(r1.Body)
	r1.Body.Close()

	// Check stats 1
	resp, err = http.Get(apiURL + "/stats")
	if err != nil {
		t.Fatalf("Failed to get stats 1: %v", err)
	}
	var st1 map[string]interface{}
	_ = json.NewDecoder(resp.Body).Decode(&st1)
	resp.Body.Close()
	in1 := st1["bytesReceived"].(float64)
	t.Logf("Stats 1 bytesReceived: %v", in1)
	if in1 == 0 {
		t.Errorf("Expected in1 > 0")
	}

	// 2. Toggle AllowLan to true via POST /config
	appCfg.AllowLan = true
	cfgBytes, _ := json.Marshal(appCfg)
	resp, err = http.Post(apiURL+"/config", "application/json", bytes.NewReader(cfgBytes))
	if err != nil {
		t.Fatalf("Failed to post config: %v", err)
	}
	resp.Body.Close()

	// Wait for Rebind
	time.Sleep(400 * time.Millisecond)

	// 3. Request 2 through proxy
	r2, err := client.Get(targetServer.URL + "/req2")
	if err != nil {
		t.Fatalf("Req 2 failed after allowLan change: %v", err)
	}
	_, _ = io.ReadAll(r2.Body)
	r2.Body.Close()

	// Check stats 2
	resp, err = http.Get(apiURL + "/stats")
	if err != nil {
		t.Fatalf("Failed to get stats 2 on %s: %v", apiURL, err)
	}
	var st2 map[string]interface{}
	_ = json.NewDecoder(resp.Body).Decode(&st2)
	resp.Body.Close()
	in2 := st2["bytesReceived"].(float64)
	t.Logf("Stats 2 bytesReceived: %v (st1 was %v)", in2, in1)
	if in2 <= in1 {
		t.Errorf("Stats did not increase! in1=%v in2=%v", in1, in2)
	}
}
