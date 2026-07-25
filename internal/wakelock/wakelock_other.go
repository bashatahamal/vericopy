//go:build !windows && !darwin && !linux

package wakelock

// Acquire has no supported mechanism on this platform and is a no-op.
func Acquire(_ string) Release {
	return func() {}
}
