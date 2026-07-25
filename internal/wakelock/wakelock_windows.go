//go:build windows

package wakelock

import "golang.org/x/sys/windows"

const (
	esContinuous     = 0x80000000
	esSystemRequired = 0x00000001
)

var setThreadExecutionState = windows.NewLazySystemDLL("kernel32.dll").NewProc("SetThreadExecutionState")

// Acquire keeps Windows from entering system sleep until Release is called.
// It deliberately does not pass ES_DISPLAY_REQUIRED: the display and any
// lock screen may still turn off, only system suspend is prevented.
func Acquire(_ string) Release {
	_, _, _ = setThreadExecutionState.Call(uintptr(esContinuous | esSystemRequired))
	released := false
	return func() {
		if released {
			return
		}
		released = true
		_, _, _ = setThreadExecutionState.Call(uintptr(esContinuous))
	}
}
