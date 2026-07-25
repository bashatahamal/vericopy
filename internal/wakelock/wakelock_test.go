package wakelock_test

import (
	"testing"

	"github.com/bashatahamal/vericopy/internal/wakelock"
)

func TestAcquireReturnsAnIdempotentRelease(t *testing.T) {
	release := wakelock.Acquire("unit test")
	if release == nil {
		t.Fatal("Acquire returned a nil Release")
	}
	release()
	release() // calling twice must not panic on any platform
}
