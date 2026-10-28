//go:build unix || windows

package runner

import (
	"bytes"
	"errors"
	"sync"
	"testing"
	"time"
)

// stalledSink holds its first write long enough that the process has exited
// before the copy reads the rest of the pipe, as a descheduled copying
// goroutine on a loaded machine does. stall defaults to 600ms.
type stalledSink struct {
	mu     sync.Mutex
	data   bytes.Buffer
	writes int
	stall  time.Duration
}

func (s *stalledSink) Write(p []byte) (int, error) {
	s.mu.Lock()
	s.writes++
	first, stall := s.writes == 1, s.stall
	s.mu.Unlock()
	if first {
		if stall == 0 {
			stall = 600 * time.Millisecond
		}
		time.Sleep(stall)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.data.Write(p)
}

func (s *stalledSink) String() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.data.String()
}

func (s *stalledSink) Len() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.data.Len()
}

// slowSink accepts every write but takes delay per call, as a sink behind a
// slow consumer or a copy goroutine starved on a loaded machine does.
type slowSink struct {
	stalledSink
	delay time.Duration
}

func (s *slowSink) Write(p []byte) (int, error) {
	time.Sleep(s.delay)
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.data.Write(p)
}

var errSinkRejected = errors.New("sink rejected output")

type failingSink struct{}

func (failingSink) Write([]byte) (int, error) { return 0, errSinkRejected }

// shortenOutputDrain sets the drain window for one test and restores it.
func shortenOutputDrain(t *testing.T, window time.Duration) {
	t.Helper()
	previous := outputDrainTimeout.Swap(int64(window))
	t.Cleanup(func() { outputDrainTimeout.Store(previous) })
}

// limitOutputBacklog sets, for one test, how long and how many bytes the
// drain keeps reading after its window, and restores both.
func limitOutputBacklog(t *testing.T, limit time.Duration, bytes int64) {
	t.Helper()
	previousLimit := outputBacklogTimeout.Swap(int64(limit))
	previousBytes := outputBacklogBytes.Swap(bytes)
	t.Cleanup(func() {
		outputBacklogTimeout.Store(previousLimit)
		outputBacklogBytes.Store(previousBytes)
	})
}
