//go:build windows

package sysproxy

import (
	"crypto/sha256"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"golang.org/x/sys/windows/registry"
)

const runKeyPath = `Software\Microsoft\Windows\CurrentVersion\Run`

func getAppRegistryName() string {
	exePath, err := os.Executable()
	if err != nil {
		return "Gofoom"
	}
	dirClean := strings.ToLower(filepath.Clean(filepath.Dir(exePath)))
	h := sha256.Sum256([]byte(dirClean))
	return fmt.Sprintf("Gofoom_%x", h[:4])
}

// IsWindowsStartupEnabled checks if Gofoom is registered in HKCU Run key for this instance.
func IsWindowsStartupEnabled() bool {
	k, err := registry.OpenKey(registry.CURRENT_USER, runKeyPath, registry.QUERY_VALUE)
	if err != nil {
		return false
	}
	defer k.Close()

	name := getAppRegistryName()
	val, _, err := k.GetStringValue(name)
	if err == nil && strings.TrimSpace(val) != "" {
		return true
	}

	// Legacy fallback check for "Gofoom" key
	val, _, err = k.GetStringValue("Gofoom")
	if err == nil && strings.TrimSpace(val) != "" {
		exePath, _ := os.Executable()
		if exePath != "" && strings.Contains(strings.ToLower(val), strings.ToLower(exePath)) {
			return true
		}
	}
	return false
}

// SetWindowsStartup enables or disables launching Gofoom at Windows logon for this instance.
func SetWindowsStartup(enable bool) error {
	k, err := registry.OpenKey(registry.CURRENT_USER, runKeyPath, registry.SET_VALUE)
	if err != nil {
		return err
	}
	defer k.Close()

	name := getAppRegistryName()
	if enable {
		exePath, err := os.Executable()
		if err != nil {
			return err
		}
		cmd := `"` + exePath + `" -minimized`
		return k.SetStringValue(name, cmd)
	}

	_ = k.DeleteValue(name)
	// Also clean legacy "Gofoom" if it points to us
	if val, _, err := k.GetStringValue("Gofoom"); err == nil {
		exePath, _ := os.Executable()
		if exePath != "" && strings.Contains(strings.ToLower(val), strings.ToLower(exePath)) {
			_ = k.DeleteValue("Gofoom")
		}
	}
	return nil
}
