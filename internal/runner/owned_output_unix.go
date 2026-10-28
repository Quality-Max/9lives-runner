//go:build unix

package runner

import (
	"fmt"
	"os"

	"golang.org/x/sys/unix"
)

// pipeHeldEmpty reports whether reader has no unread byte while some process
// still holds the pipe's write end. A zero-timeout poll finds the pipe
// unreadable only in that state: buffered bytes set POLLIN, and a closed
// write end sets POLLHUP or POLLIN for the end of file the copy has yet to
// read.
func pipeHeldEmpty(reader *os.File) (bool, error) {
	raw, err := reader.SyscallConn()
	if err != nil {
		return false, err
	}
	var held bool
	var pollErr error
	if err := raw.Control(func(fd uintptr) {
		fds := []unix.PollFd{{Fd: int32(fd), Events: unix.POLLIN}}
		for {
			n, err := unix.Poll(fds, 0)
			if err == unix.EINTR {
				continue
			}
			switch {
			case err != nil:
				pollErr = fmt.Errorf("inspect output pipe: %w", err)
			case n == 0:
				held = true
			case fds[0].Revents&(unix.POLLIN|unix.POLLHUP) == 0:
				pollErr = fmt.Errorf("inspect output pipe: poll events %#x", fds[0].Revents)
			}
			return
		}
	}); err != nil {
		return false, err
	}
	return held, pollErr
}
