//go:build unix

package tier2

import "syscall"

// pidMayBeAlive fails closed: an inaccessible or existing PID is preserved.
func pidMayBeAlive(pid int) bool {
	err := syscall.Kill(pid, 0)
	return err == nil || err == syscall.EPERM || err != syscall.ESRCH
}
