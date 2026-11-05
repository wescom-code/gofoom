//go:build windows

package ui

import (
	"os"
	"runtime/debug"
	"unsafe"

	"golang.org/x/sys/windows"
)

var (
	modpsapi                  = windows.NewLazySystemDLL("psapi.dll")
	procEmptyWorkingSet       = modpsapi.NewProc("EmptyWorkingSet")
	kernel32                  = windows.NewLazySystemDLL("kernel32.dll")
	procSetProcessInformation = kernel32.NewProc("SetProcessInformation")
)

type processPowerThrottlingState struct {
	Version     uint32
	ControlMask uint32
	StateMask   uint32
}

const (
	processPowerThrottling               = 4
	processPowerThrottlingCurrentVer     = 1
	processPowerThrottlingExecutionSpeed = 0x1
)

// getAllDescendantPids returns all child and grandchild process IDs of parentPid.
func getAllDescendantPids(parentPid uint32) []uint32 {
	snapshot, err := windows.CreateToolhelp32Snapshot(windows.TH32CS_SNAPPROCESS, 0)
	if err != nil {
		return nil
	}
	defer windows.CloseHandle(snapshot)

	childrenMap := make(map[uint32][]uint32)
	var entry windows.ProcessEntry32
	entry.Size = uint32(unsafe.Sizeof(entry))

	if err := windows.Process32First(snapshot, &entry); err != nil {
		return nil
	}

	for {
		childrenMap[entry.ParentProcessID] = append(childrenMap[entry.ParentProcessID], entry.ProcessID)
		if err := windows.Process32Next(snapshot, &entry); err != nil {
			break
		}
	}

	var result []uint32
	queue := []uint32{parentPid}
	visited := make(map[uint32]bool)
	visited[parentPid] = true

	for len(queue) > 0 {
		curr := queue[0]
		queue = queue[1:]
		for _, child := range childrenMap[curr] {
			if !visited[child] {
				visited[child] = true
				result = append(result, child)
				queue = append(queue, child)
			}
		}
	}

	return result
}

// setEfficiencyMode enables or disables Windows Efficiency Mode (EcoQoS / Green Leaf 🍃)
// for Gofoom and all descendant WebView2 processes (msedgewebview2.exe).
func setEfficiencyMode(enable bool) {
	if procSetProcessInformation.Find() != nil {
		return
	}

	state := processPowerThrottlingState{
		Version:     processPowerThrottlingCurrentVer,
		ControlMask: processPowerThrottlingExecutionSpeed,
	}
	if enable {
		state.StateMask = processPowerThrottlingExecutionSpeed
	}

	// 1. Apply to Gofoom process
	_, _, _ = procSetProcessInformation.Call(
		uintptr(windows.CurrentProcess()),
		uintptr(processPowerThrottling),
		uintptr(unsafe.Pointer(&state)),
		uintptr(unsafe.Sizeof(state)),
	)

	// 2. Apply to all descendant processes (msedgewebview2.exe Browser, GPU, Renderer, Utility)
	descendants := getAllDescendantPids(uint32(os.Getpid()))
	desiredAccess := uint32(windows.PROCESS_QUERY_LIMITED_INFORMATION | windows.PROCESS_SET_INFORMATION)
	for _, pid := range descendants {
		hChild, err := windows.OpenProcess(desiredAccess, false, pid)
		if err == nil {
			_, _, _ = procSetProcessInformation.Call(
				uintptr(hChild),
				uintptr(processPowerThrottling),
				uintptr(unsafe.Pointer(&state)),
				uintptr(unsafe.Sizeof(state)),
			)
			_ = windows.CloseHandle(hChild)
		}
	}
}

// trimMemory forces the Go runtime and OS memory manager to reclaim unused RAM,
// trimming the working set of both Gofoom and all descendant WebView2 processes.
func trimMemory() {
	// 1. Force Go runtime GC and return memory to OS
	debug.FreeOSMemory()

	// 2. Trim our own process working set
	if procEmptyWorkingSet.Find() == nil {
		_, _, _ = procEmptyWorkingSet.Call(uintptr(windows.CurrentProcess()))
	}

	// 3. Trim all descendant processes (msedgewebview2.exe Browser, GPU, Renderer, Utility)
	descendants := getAllDescendantPids(uint32(os.Getpid()))
	desiredAccess := uint32(windows.PROCESS_SET_QUOTA | windows.PROCESS_QUERY_INFORMATION)
	for _, pid := range descendants {
		hChild, err := windows.OpenProcess(desiredAccess, false, pid)
		if err == nil {
			if procEmptyWorkingSet.Find() == nil {
				_, _, _ = procEmptyWorkingSet.Call(uintptr(hChild))
			}
			_ = windows.CloseHandle(hChild)
		}
	}
}
