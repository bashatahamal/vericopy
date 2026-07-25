//go:build windows

package wakelock

import (
	"runtime"
	"sync"

	"golang.org/x/sys/windows"
)

const (
	esContinuous     = 0x80000000
	esSystemRequired = 0x00000001
)

var setThreadExecutionState = windows.NewLazySystemDLL("kernel32.dll").NewProc("SetThreadExecutionState")

// SetThreadExecutionState is documented as thread-specific: the call that
// clears ES_SYSTEM_REQUIRED must run on the same OS thread that set it, or
// the assertion is never actually cleared. Go goroutines are not pinned to
// OS threads, so an Acquire and its matching Release could easily land on
// different threads and leak the sleep-prevention state indefinitely. A
// single goroutine locked to one OS thread for the process lifetime, fed by
// a channel, keeps every call on that same thread regardless of which
// goroutine calls Acquire or invokes the returned Release.
var (
	requests  = make(chan bool)
	startOnce sync.Once
)

func ensureWorker() {
	startOnce.Do(func() {
		go func() {
			runtime.LockOSThread()
			for acquire := range requests {
				if acquire {
					_, _, _ = setThreadExecutionState.Call(uintptr(esContinuous | esSystemRequired))
				} else {
					_, _, _ = setThreadExecutionState.Call(uintptr(esContinuous))
				}
			}
		}()
	})
}

// Acquire keeps Windows from entering system sleep until Release is called.
// It deliberately does not pass ES_DISPLAY_REQUIRED: the display and any
// lock screen may still turn off, only system suspend is prevented.
func Acquire(_ string) Release {
	ensureWorker()
	requests <- true
	var once sync.Once
	return func() {
		once.Do(func() { requests <- false })
	}
}
