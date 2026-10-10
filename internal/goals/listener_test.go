package goals

import (
	"net"
	"sync"
	"testing"
	"time"
)

// swallowingListener reproduces the go-winio race: the first Close request is
// consumed without stopping the listener, so that Close waits until a later
// request stops it.
type swallowingListener struct {
	net.Listener
	mu       sync.Mutex
	requests int
	stopped  chan struct{}
}

func (l *swallowingListener) Close() error {
	l.mu.Lock()
	l.requests++
	if l.requests == 2 {
		close(l.stopped)
	}
	l.mu.Unlock()
	<-l.stopped
	return nil
}

func TestCloseIsRepeatedUntilTheListenerStops(t *testing.T) {
	inner := &swallowingListener{stopped: make(chan struct{})}
	closed := make(chan error, 1)
	go func() { closed <- resignalingListener{inner}.Close() }()
	select {
	case err := <-closed:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Close waited on a swallowed close request")
	}
	// Close calls made after the listener stopped return at once.
	if err := (resignalingListener{inner}).Close(); err != nil {
		t.Fatal(err)
	}
}
