package config

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sync"

	"gofoom/pkg/proxy"
	"gofoom/pkg/sysproxy"
)

// AppConfig represents persisted user settings matching Winfoom JSON schema.
type AppConfig struct {
	ProxyType             string `json:"proxyType"`
	ProxyHost             string `json:"proxyHost"`
	ProxyPort             int    `json:"proxyPort"`
	ProxyUsername         string `json:"proxyUsername"`
	ProxyPassword         string `json:"proxyPassword"`
	LocalPort             int    `json:"localPort"`
	ProxyTestURL          string `json:"proxyTestUrl"`
	UseCurrentCredentials bool   `json:"useCurrentCredentials"`
	HttpAuthProtocol      string `json:"httpAuthProtocol"`
	PacFileLocation       string `json:"pacFileLocation"`
	AutoStart             bool   `json:"autostart"`
	AutoDetect            bool   `json:"autodetect"`
	LaunchOnStartup       bool   `json:"launchOnStartup"`
}

// ConfigManager handles loading and persisting settings.
type ConfigManager struct {
	mu       sync.RWMutex
	filePath string
	current  AppConfig
}

// DefaultAppConfig returns default configuration.
func DefaultAppConfig() AppConfig {
	return AppConfig{
		ProxyType:             "DIRECT",
		LocalPort:             3129,
		ProxyTestURL:          "https://example.com",
		UseCurrentCredentials: true,
		HttpAuthProtocol:      "NTLM",
	}
}

// NewConfigManager initializes a config manager with a file path.
func NewConfigManager(filePath string) *ConfigManager {
	if filePath == "" {
		filePath = "gofoom.json"
	}
	def := DefaultAppConfig()
	def.LaunchOnStartup = sysproxy.IsWindowsStartupEnabled()
	cm := &ConfigManager{
		filePath: filePath,
		current:  def,
	}
	_ = cm.Load()
	return cm
}

// Load loads settings from file.
func (cm *ConfigManager) Load() error {
	cm.mu.Lock()
	defer cm.mu.Unlock()

	data, err := os.ReadFile(cm.filePath)
	if err != nil {
		return err
	}

	var cfg AppConfig
	if err := json.Unmarshal(data, &cfg); err != nil {
		return err
	}
	cfg.LaunchOnStartup = sysproxy.IsWindowsStartupEnabled()
	cm.current = cfg
	return nil
}

// Save persists current settings to file.
func (cm *ConfigManager) Save() error {
	cm.mu.RLock()
	data, err := json.MarshalIndent(cm.current, "", "  ")
	cm.mu.RUnlock()
	if err != nil {
		return err
	}

	dir := filepath.Dir(cm.filePath)
	if dir != "" && dir != "." {
		_ = os.MkdirAll(dir, 0755)
	}

	return os.WriteFile(cm.filePath, data, 0644)
}

// Get returns the current configuration.
func (cm *ConfigManager) Get() AppConfig {
	cm.mu.RLock()
	defer cm.mu.RUnlock()
	return cm.current
}

// GetMasked returns configuration with password hidden.
func (cm *ConfigManager) GetMasked() AppConfig {
	cfg := cm.Get()
	if cfg.ProxyPassword != "" {
		cfg.ProxyPassword = "******"
	}
	return cfg
}

// Update updates the configuration. If password is mask string, keep old password.
func (cm *ConfigManager) Update(newCfg AppConfig) error {
	cm.mu.Lock()
	if newCfg.ProxyPassword == "******" {
		newCfg.ProxyPassword = cm.current.ProxyPassword
	}
	if newCfg.LaunchOnStartup != cm.current.LaunchOnStartup {
		_ = sysproxy.SetWindowsStartup(newCfg.LaunchOnStartup)
	}
	cm.current = newCfg
	cm.mu.Unlock()
	return cm.Save()
}

// ToServerConfig converts AppConfig to proxy.ServerConfig.
func (cm *ConfigManager) ToServerConfig() proxy.ServerConfig {
	c := cm.Get()
	pt, _ := proxy.NormalizeProxyType(c.ProxyType)
	sc := proxy.DefaultServerConfig()
	sc.LocalPort = c.LocalPort
	sc.ProxyTestURL = c.ProxyTestURL
	sc.Upstream = proxy.UpstreamConfig{
		Type:                  pt,
		Host:                  c.ProxyHost,
		Port:                  c.ProxyPort,
		Username:              c.ProxyUsername,
		Password:              c.ProxyPassword,
		UseCurrentCredentials: c.UseCurrentCredentials,
		HttpAuthProtocol:      c.HttpAuthProtocol,
		PacFileLocation:       c.PacFileLocation,
	}
	return sc
}
