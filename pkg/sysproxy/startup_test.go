package sysproxy

import (
	"runtime"
	"testing"
)

func TestWindowsStartupToggle(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("Skipping Windows startup registry test on non-windows platform")
	}

	initial := IsWindowsStartupEnabled()

	// Toggle to the opposite state
	target := !initial
	if err := SetWindowsStartup(target); err != nil {
		t.Fatalf("Failed to set Windows startup to %v: %v", target, err)
	}

	// Verify new state
	if cur := IsWindowsStartupEnabled(); cur != target {
		t.Errorf("Expected IsWindowsStartupEnabled() == %v, got %v", target, cur)
	}

	// Restore original state
	if err := SetWindowsStartup(initial); err != nil {
		t.Fatalf("Failed to restore initial Windows startup state to %v: %v", initial, err)
	}

	if cur := IsWindowsStartupEnabled(); cur != initial {
		t.Errorf("Expected restored state %v, got %v", initial, cur)
	}
}
