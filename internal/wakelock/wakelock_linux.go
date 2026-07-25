//go:build linux

package wakelock

import "os/exec"

// Acquire shells out to systemd-inhibit, holding a sleep/idle inhibitor for
// as long as the "sleep infinity" placeholder process runs. Systems without
// systemd simply get no sleep prevention; the transfer itself is unaffected.
func Acquire(reason string) Release {
	path, err := exec.LookPath("systemd-inhibit")
	if err != nil {
		return func() {}
	}
	cmd := exec.Command(path, "--what=sleep:idle", "--who=Vericopy", "--why="+reason, "--mode=block", "sleep", "infinity")
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
