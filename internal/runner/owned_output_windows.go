//go:build windows

package runner

import (
	"errors"
	"fmt"
	"os"
	"unsafe"

	"golang.org/x/sys/windows"
)

var procPeekNamedPipe = windows.NewLazySystemDLL("kernel32.dll").NewProc("PeekNamedPipe")

// pipeHeldEmpty reports whether reader has no unread byte while some process
// still holds the pipe's write end. PeekNamedPipe reports the bytes available
// and fails with ERROR_BROKEN_PIPE once every write end is closed and nothing
// is left, which is the end of file the copy has yet to read.
func pipeHeldEmpty(reader *os.File) (bool, error) {
	raw, err := reader.SyscallConn()
	if err != nil {
		return false, err
	}
	var held bool
	var peekErr error
	if err := raw.Control(func(handle uintptr) {
		var available uint32
		ok, _, callErr := procPeekNamedPipe.Call(handle, 0, 0, 0, uintptr(unsafe.Pointer(&available)), 0)
		switch {
		case ok != 0:
			held = available == 0
		case errors.Is(callErr, windows.ERROR_BROKEN_PIPE):
		default:
			peekErr = fmt.Errorf("inspect output pipe: %w", callErr)
		}
	}); err != nil {
		return false, err
	}
	return held, peekErr
}
