// Package wakelock best-effort prevents the operating system from sleeping
// for the duration of a transfer, so a long queue of large files does not
// get interrupted by the machine suspending partway through.
//
// Acquiring a lock is advisory and never fatal: on any platform where the
// underlying mechanism is unavailable or fails, Acquire still returns a
// working Release that simply does nothing. A transfer must never fail or
// behave differently because sleep prevention could not be arranged.
package wakelock

// Release ends a wake lock acquired by Acquire. It is safe to call more than
// once, and safe to call on a lock that silently failed to acquire.
type Release func()
