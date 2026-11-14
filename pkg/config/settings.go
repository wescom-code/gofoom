package config

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
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
	AllowLan              bool   `json:"allowLan"`
	ProxyTestURL          string `json:"proxyTestUrl"`
	UseCurrentCredentials bool   `json:"useCurrentCredentials"`
	HttpAuthProtocol      string `json:"httpAuthProtocol"`
	PacFileLocation       string `json:"pacFileLocation"`
	BlacklistTimeout      int    `json:"blacklistTimeout,omitempty"`
	AutoStart             bool   `json:"autostart"`
	StartMinimized        bool   `json:"startMinimized"`
	AutoDetect            bool   `json:"autodetect"`
	LaunchOnStartup       bool   `json:"launchOnStartup"`
	ApiPort               int    `json:"apiPort,omitempty"`
	MemoryPolicy          string `json:"memoryPolicy,omitempty"`
	UILayoutMode          string `json:"uiLayoutMode,omitempty"`
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
		ApiPort:               9999,
		ProxyTestURL:          "https://example.com",
		UseCurrentCredentials: true,
		HttpAuthProtocol:      "NTLM",
		BlacklistTimeout:      5,
		MemoryPolicy:          "none",
		UILayoutMode:          "auto",
	}
}

func isSystemDir(dir string) bool {
	clean := filepath.Clean(dir)
	sysRoot := os.Getenv("SystemRoot")
	if sysRoot != "" && strings.HasPrefix(strings.ToLower(clean), strings.ToLower(sysRoot)) {
		return true
	}
	winDir := os.Getenv("windir")
	if winDir != "" && strings.HasPrefix(strings.ToLower(clean), strings.ToLower(winDir)) {
		return true
	}
	return false
}

// ResolveConfigPath resolves a configuration file path to an absolute path.
// When Gofoom is started by Windows autostart (where working directory defaults to C:\Windows\System32),
// this ensures gofoom.json is loaded from next to the executable instead of failing with Access Denied.
func ResolveConfigPath(filePath string) string {
	if filePath == "" {
		filePath = "gofoom.json"
	}
	if filepath.IsAbs(filePath) {
		return filepath.Clean(filePath)
	}

	cwd, cwdErr := os.Getwd()
	// If CWD is valid and NOT a Windows system directory:
	if cwdErr == nil && !isSystemDir(cwd) {
		candidate := filepath.Join(cwd, filePath)
		if _, err := os.Stat(candidate); err == nil {
			return filepath.Clean(candidate)
		}
	}

	// Fallback to checking next to the executable
	if exePath, err := os.Executable(); err == nil {
		exeDir := filepath.Dir(exePath)
		candidate := filepath.Join(exeDir, filePath)
		if _, err := os.Stat(candidate); err == nil {
			return filepath.Clean(candidate)
		}
		// If neither exists yet (first run) and CWD is a system dir, create next to executable
		if cwdErr != nil || isSystemDir(cwd) {
			return filepath.Clean(candidate)
		}
	}

	// Standard fallback
	if abs, err := filepath.Abs(filePath); err == nil {
		return filepath.Clean(abs)
	}
	return filepath.Clean(filePath)
}

// NewConfigManager initializes a config manager with a file path.
func NewConfigManager(filePath string) *ConfigManager {
	filePath = ResolveConfigPath(filePath)
	def := DefaultAppConfig()
	def.LaunchOnStartup = sysproxy.IsWindowsStartupEnabled()
	cm := &ConfigManager{
		filePath: filePath,
		current:  def,
	}
	_ = cm.Load()
	return cm
}

// FilePath returns the resolved absolute configuration file path.
func (cm *ConfigManager) FilePath() string {
	cm.mu.RLock()
	defer cm.mu.RUnlock()
	return cm.filePath
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

// UserPreferences represents user preferences that can be updated independently of proxy network settings.
type UserPreferences struct {
	AutoStart       bool   `json:"autostart"`
	StartMinimized  bool   `json:"startMinimized"`
	LaunchOnStartup bool   `json:"launchOnStartup"`
	MemoryPolicy    string `json:"memoryPolicy"`
	UILayoutMode    string `json:"uiLayoutMode,omitempty"`
}

// UpdatePreferences updates only the preferences (system & UI settings) and persists to file.
func (cm *ConfigManager) UpdatePreferences(pref UserPreferences) error {
	cm.mu.Lock()
	cm.current.AutoStart = pref.AutoStart
	cm.current.StartMinimized = pref.StartMinimized
	if pref.LaunchOnStartup != cm.current.LaunchOnStartup {
		_ = sysproxy.SetWindowsStartup(pref.LaunchOnStartup)
	}
	cm.current.LaunchOnStartup = pref.LaunchOnStartup
	if pref.MemoryPolicy != "" {
		cm.current.MemoryPolicy = pref.MemoryPolicy
	}
	if pref.UILayoutMode != "" {
		cm.current.UILayoutMode = pref.UILayoutMode
	}
	cm.mu.Unlock()
	return cm.Save()
}

// UpdateMemoryPolicy updates only the memory policy without modifying proxy settings.
func (cm *ConfigManager) UpdateMemoryPolicy(policy string) error {
	cm.mu.Lock()
	cm.current.MemoryPolicy = policy
	cm.mu.Unlock()
	return cm.Save()
}

// ToServerConfig converts AppConfig to proxy.ServerConfig.
func (cm *ConfigManager) ToServerConfig() proxy.ServerConfig {
	c := cm.Get()
	pt, _ := proxy.NormalizeProxyType(c.ProxyType)
	sc := proxy.DefaultServerConfig()
	sc.LocalPort = c.LocalPort
	sc.ApiPort = c.ApiPort
	if sc.ApiPort <= 0 {
		sc.ApiPort = 9999
	}
	sc.AllowLan = c.AllowLan
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
		BlacklistTimeout:      c.BlacklistTimeout,
	}
	return sc
}
