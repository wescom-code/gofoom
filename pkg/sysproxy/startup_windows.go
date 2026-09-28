//go:build windows

package sysproxy

import (
	"os"
	"strings"

	"golang.org/x/sys/windows/registry"
)

const runKeyPath = `Software\Microsoft\Windows\CurrentVersion\Run`
const appRegistryName = "Gofoom"

// IsWindowsStartupEnabled checks if Gofoom is registered in HKCU Run key.
func IsWindowsStartupEnabled() bool {
	k, err := registry.OpenKey(registry.CURRENT_USER, runKeyPath, registry.QUERY_VALUE)
	if err != nil {
		return false
	}
	defer k.Close()

	val, _, err := k.GetStringValue(appRegistryName)
	return err == nil && strings.TrimSpace(val) != ""
}

// SetWindowsStartup enables or disables launching Gofoom at Windows logon.
func SetWindowsStartup(enable bool) error {
	k, err := registry.OpenKey(registry.CURRENT_USER, runKeyPath, registry.SET_VALUE)
	if err != nil {
		return err
	}
	defer k.Close()

	if enable {
		exePath, err := os.Executable()
		if err != nil {
			return err
		}
		cmd := `"` + exePath + `" -minimized`
		return k.SetStringValue(appRegistryName, cmd)
	}

	_ = k.DeleteValue(appRegistryName)
	return nil
}
