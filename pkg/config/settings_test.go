package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestResolveConfigPath(t *testing.T) {
	// 1. Empty path should resolve to absolute path ending with gofoom.json
	resolved := ResolveConfigPath("")
	if !filepath.IsAbs(resolved) {
		t.Errorf("Expected absolute path, got %s", resolved)
	}
	if filepath.Base(resolved) != "gofoom.json" {
		t.Errorf("Expected base gofoom.json, got %s", filepath.Base(resolved))
	}

	// 2. Absolute path should be returned cleanly
	tmpFile := filepath.Join(t.TempDir(), "custom.json")
	resolvedAbs := ResolveConfigPath(tmpFile)
	if resolvedAbs != filepath.Clean(tmpFile) {
		t.Errorf("Expected %s, got %s", tmpFile, resolvedAbs)
	}
}

func TestConfigManagerLifecycle(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "test_config.json")

	cm := NewConfigManager(configPath)
	if cm.FilePath() != filepath.Clean(configPath) {
		t.Errorf("Expected filePath %s, got %s", configPath, cm.FilePath())
	}

	// Test UpdatePreferences
	err := cm.UpdatePreferences(UserPreferences{
		AutoStart:       true,
		StartMinimized:  true,
		LaunchOnStartup: false,
		MemoryPolicy:    "hide",
		UILayoutMode:    "split",
	})
	if err != nil {
		t.Fatalf("UpdatePreferences failed: %v", err)
	}

	// Verify file was written
	if _, err := os.Stat(configPath); err != nil {
		t.Fatalf("Config file was not created: %v", err)
	}

	// Reload in a new manager
	cm2 := NewConfigManager(configPath)
	cfg := cm2.Get()
	if !cfg.AutoStart {
		t.Errorf("Expected AutoStart true, got false")
	}
	if !cfg.StartMinimized {
		t.Errorf("Expected StartMinimized true, got false")
	}
	if cfg.MemoryPolicy != "hide" {
		t.Errorf("Expected MemoryPolicy 'hide', got '%s'", cfg.MemoryPolicy)
	}
	if cfg.UILayoutMode != "split" {
		t.Errorf("Expected UILayoutMode 'split', got '%s'", cfg.UILayoutMode)
	}
}
