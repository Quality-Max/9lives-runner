package goals

import (
	"net"
	"time"
)

// closeRetry is how long Close waits before it repeats the close request.
const closeRetry = 50 * time.Millisecond

// resignalingListener repeats Close until the wrapped listener reports that
// it has stopped. go-winio v0.6.2's pipe listener sends one close signal on
// an unbuffered channel and waits for its listener goroutine to exit, but a
// connect started by Accept selects on the same channel. When that connect
// takes the signal and reports an error other than ErrFileClosed, the
// listener goroutine goes back to waiting for a signal that never comes, so
// Close never returns. http.Server.Close holds the server's lock throughout,
// so Serve and every later caller wait with it: a Windows arm64 CI run hung
// that way for ten minutes. A repeated Close reaches the waiting goroutine;
// once the listener has stopped, extra Close calls return immediately.
type resignalingListener struct{ net.Listener }

func (l resignalingListener) Close() error {
	done := make(chan error, 1)
	go func() { done <- l.Listener.Close() }()
	retry := time.NewTicker(closeRetry)
	defer retry.Stop()
	for {
		select {
		case err := <-done:
			return err
		case <-retry.C:
			go func() { _ = l.Listener.Close() }()
		}
	}
}
