//go:build windows

package runner

import (
	"crypto/rand"
	"errors"
	"fmt"
	"os"
	"unsafe"

	"golang.org/x/sys/windows"
)

var procPeekNamedPipe = windows.NewLazySystemDLL("kernel32.dll").NewProc("PeekNamedPipe")

// PeekNamedPipe can block behind a pending ReadFile on a synchronous handle.
// Use an overlapped reader so inspecting and closing a held output pipe can
// interrupt its pending read. The child receives a synchronous write handle.
func newOwnedOutputPipe() (*os.File, *os.File, error) {
	var nonce [16]byte
	if _, err := rand.Read(nonce[:]); err != nil {
		return nil, nil, err
	}
	name := fmt.Sprintf(`\\.\pipe\9l-output-%x`, nonce)
	path, err := windows.UTF16PtrFromString(name)
	if err != nil {
		return nil, nil, err
	}
	handle, err := windows.CreateNamedPipe(path,
		windows.PIPE_ACCESS_INBOUND|windows.FILE_FLAG_OVERLAPPED|windows.FILE_FLAG_FIRST_PIPE_INSTANCE,
		windows.PIPE_TYPE_BYTE|windows.PIPE_READMODE_BYTE|windows.PIPE_WAIT|windows.PIPE_REJECT_REMOTE_CLIENTS,
		1, 0, outputCopyBuffer, 0, nil)
	if err != nil {
		return nil, nil, fmt.Errorf("create owned output pipe: %w", err)
	}
	reader := os.NewFile(uintptr(handle), name)
	writeHandle, err := windows.CreateFile(path, windows.GENERIC_WRITE, 0, nil, windows.OPEN_EXISTING,
		windows.FILE_ATTRIBUTE_NORMAL|windows.SECURITY_SQOS_PRESENT|windows.SECURITY_ANONYMOUS, 0)
	if err != nil {
		_ = reader.Close()
		return nil, nil, fmt.Errorf("open owned output pipe writer: %w", err)
	}
	return reader, os.NewFile(uintptr(writeHandle), name), nil
}

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
