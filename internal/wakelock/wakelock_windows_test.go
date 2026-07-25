//go:build windows

package wakelock_test

import (
	"sync"
	"testing"

	"github.com/bashatahamal/vericopy/internal/wakelock"
)

// TestAcquireReleaseFromDifferentGoroutines guards against the original bug:
// SetThreadExecutionState is thread-specific, so Acquire and Release calls
// landing on different OS threads (which is normal for goroutines) must
// still correctly clear the sleep-prevention state. This can't observe the
// real OS sleep state, but it does exercise many concurrent acquire/release
// pairs across goroutines to catch a deadlock or panic in the single-worker
// channel design.
func TestAcquireReleaseFromDifferentGoroutines(t *testing.T) {
	var wg sync.WaitGroup
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			release := wakelock.Acquire("stress test")
			// Release deliberately from a goroutine other than the one that
			// acquired it, mirroring Acquire in one goroutine (job dispatch)
			// and Release in another (finishJob).
			var releaseWG sync.WaitGroup
			releaseWG.Add(1)
			go func() {
				defer releaseWG.Done()
				release()
			}()
			releaseWG.Wait()
		}()
	}
	wg.Wait()
}
