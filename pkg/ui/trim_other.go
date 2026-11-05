//go:build !windows

package ui

import "runtime/debug"

func setEfficiencyMode(enable bool) {}

func trimMemory() {
	debug.FreeOSMemory()
}
