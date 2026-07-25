//go:build darwin

package wakelock

import "os/exec"

// Acquire shells out to the standard "caffeinate" utility for the duration
// of the lock: -i prevents idle system sleep, -s prevents system sleep on
// AC power. Killing the process on Release ends the assertion.
func Acquire(_ string) Release {
	cmd := exec.Command("caffeinate", "-i", "-s")
	if err := cmd.Start(); err != nil {
		return func() {}
	}
	released := false
	return func() {
		if released {
			return
		}
		released = true
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
	}
}
