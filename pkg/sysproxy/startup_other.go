//go:build !windows

package sysproxy

// IsWindowsStartupEnabled stub for non-windows platforms.
func IsWindowsStartupEnabled() bool {
	return false
}

// SetWindowsStartup stub for non-windows platforms.
func SetWindowsStartup(enable bool) error {
	return nil
}
