//go:build !windows

package goals

import (
	"net"
	"os"
	"path/filepath"
)

func listenGoalTransport() (net.Listener, string, string, error) {
	directory, err := os.MkdirTemp("", "9lg-")
	if err != nil {
		return nil, "", "", err
	}
	// Unix sockets can have short OS limits; macOS's temp directory is long.
	socket := filepath.Join(directory, "s")
	listener, err := net.Listen("unix", socket)
	if err != nil {
		_ = os.RemoveAll(directory)
		return nil, "", "", err
	}
	return listener, socket, directory, nil
}
