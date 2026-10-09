//go:build !unix && !windows

package tier2

// Without a portable process-identity check, preservation is safer than sweep.
func pidMayBeAlive(int) bool { return true }
