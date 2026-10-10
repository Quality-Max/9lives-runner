//go:build windows

package goals

import (
	"crypto/rand"
	"encoding/hex"
	"net"

	"github.com/Microsoft/go-winio"
	"golang.org/x/sys/windows"
)

func listenGoalTransport() (net.Listener, string, string, error) {
	user, err := windows.GetCurrentProcessToken().GetTokenUser()
	if err != nil {
		return nil, "", "", err
	}
	var nonce [16]byte
	if _, err := rand.Read(nonce[:]); err != nil {
		return nil, "", "", err
	}
	// Node's Windows socketPath uses named pipes, not AF_UNIX sockets. A
	// protected DACL grants access only to the engine's user and its workers.
	socket := `\\.\pipe\9l-goal-` + hex.EncodeToString(nonce[:])
	listener, err := winio.ListenPipe(socket, &winio.PipeConfig{SecurityDescriptor: "D:P(A;;GA;;;" + user.User.Sid.String() + ")"})
	if err != nil {
		return nil, "", "", err
	}
	return resignalingListener{listener}, socket, "", nil
}
