//go:build windows

package tier2

import "golang.org/x/sys/windows"

// Preserve inaccessible processes. Only a missing PID or a signaled process
// handle establishes that a stale, owned healing copy may be removed.
func pidMayBeAlive(pid int) bool {
	if pid <= 0 {
		return true
	}
	process, err := windows.OpenProcess(windows.SYNCHRONIZE, false, uint32(pid))
	if err == windows.ERROR_INVALID_PARAMETER {
		return false
	}
	if err != nil {
		return true
	}
	defer windows.CloseHandle(process)
	state, err := windows.WaitForSingleObject(process, 0)
	return err != nil || state != windows.WAIT_OBJECT_0
}
