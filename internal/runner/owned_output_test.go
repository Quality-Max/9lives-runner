//go:build unix || windows

package runner

import (
	"bytes"
	"context"
	"errors"
	"os"
	"strings"
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

// pipeHeldEmpty is what lets the drain close a pipe without losing output, so
// each of its three states is checked on the platform's own primitive: poll
// on Unix, PeekNamedPipe on Windows.
func TestPipeHeldEmptyTellsPendingDataHeldEmptyAndEndOfFile(t *testing.T) {
	reader, writer, err := newOwnedOutputPipe()
	if err != nil {
		t.Fatal(err)
	}
	defer reader.Close()
	defer writer.Close()
	if _, err := writer.WriteString("pending"); err != nil {
		t.Fatal(err)
	}
	if held, err := pipeHeldEmpty(reader); err != nil || held {
		t.Fatalf("pipe with unread data: held=%v err=%v, want false, nil", held, err)
	}
	buffer := make([]byte, 64)
	if n, err := reader.Read(buffer); err != nil || string(buffer[:n]) != "pending" {
		t.Fatalf("read %q, %v", buffer[:n], err)
	}
	if held, err := pipeHeldEmpty(reader); err != nil || !held {
		t.Fatalf("empty pipe whose write end is still open: held=%v err=%v, want true, nil", held, err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	if held, err := pipeHeldEmpty(reader); err != nil || held {
		t.Fatalf("pipe at end of file: held=%v err=%v, want false, nil", held, err)
	}
	if _, err := reader.Read(buffer); err == nil {
		t.Fatal("pipe at end of file returned data")
	}
}

// Releasing a held, empty pipe must end a copy blocked in Read on it, and that
// read error is the drain's own close rather than a read failure. On Windows
// this requires an overlapped read handle: PeekNamedPipe must not wait for
// the pending ReadFile before the drain can close it.
func TestOwnedStreamReleaseEndsAPendingRead(t *testing.T) {
	output := &ownedOutput{done: make(chan copyResult, 2)}
	sink := &slowSink{}
	childEnd, err := output.attach(sink)
	if err != nil {
		t.Fatal(err)
	}
	writer := childEnd.(*os.File)
	defer writer.Close()
	if _, err := writer.WriteString("before\n"); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(5 * time.Second)
	for sink.String() != "before\n" {
		if time.Now().After(deadline) {
			t.Fatalf("copy did not deliver output: %q", sink.String())
		}
		time.Sleep(5 * time.Millisecond)
	}
	// The copy is now blocked reading a pipe whose write end is still open.
	time.Sleep(50 * time.Millisecond)
	released := make(chan bool, 1)
	go func() { released <- output.streams[0].release() }()
	select {
	case cut := <-released:
		if cut {
			t.Fatal("releasing a pipe shown to be empty reported a cut")
		}
	case <-time.After(5 * time.Second):
		_ = writer.Close()
		t.Fatal("inspecting the held pipe blocked behind its pending read")
	}
	select {
	case result := <-output.done:
		if result.readErr != nil || result.sinkErr != nil {
			t.Fatalf("release reported as a failure: %+v", result)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("closing the read end did not end the pending read")
	}
}

// writeUntilClosed writes to writer until the drain closes the read end, as
// an escaped process that never stops logging does, then closes its end.
func writeUntilClosed(writer *os.File) (stop func()) {
	done := make(chan struct{})
	go func() {
		defer close(done)
		defer writer.Close()
		chunk := bytes.Repeat([]byte("y\n"), 4096)
		for {
			if _, err := writer.Write(chunk); err != nil {
				return
			}
		}
	}()
	return func() {
		writer.Close()
		<-done
	}
}

// A writer outside the stopped group that keeps the pipe readable must not
// keep the drain reading forever. After the window the drain reads at most
// the byte allowance a finite backlog can need, then closes the pipe and
// reports that output may be incomplete.
func TestOwnedOutputStopsReadingAWriterThatNeverStops(t *testing.T) {
	limitOutputBacklog(t, time.Minute, 128<<10)
	output := &ownedOutput{done: make(chan copyResult, 2)}
	sink := &slowSink{delay: 5 * time.Millisecond}
	childEnd, err := output.attach(sink)
	if err != nil {
		t.Fatal(err)
	}
	stop := writeUntilClosed(childEnd.(*os.File))
	defer stop()
	result := make(chan error, 1)
	started := time.Now()
	go func() { result <- output.drain(context.Background(), 50*time.Millisecond) }()
	select {
	case err := <-result:
		if !errors.Is(err, ErrOutputIncomplete) {
			t.Fatalf("err=%v, want ErrOutputIncomplete", err)
		}
		if elapsed := time.Since(started); elapsed > 10*time.Second {
			t.Fatalf("drain took %s", elapsed)
		}
	case <-time.After(20 * time.Second):
		stop()
		t.Fatal("drain kept reading a writer that never stops")
	}
	if sink.Len() == 0 {
		t.Fatal("output written before the cut was lost")
	}
}

// Bytes still queued when the backlog time limit ends, because the sink is
// stalled and a writer outside the group still holds the pipe, cannot be
// shown to have been delivered: the drain closes the pipe and reports it.
func TestOwnedOutputCutsBacklogPastTheTimeLimit(t *testing.T) {
	limitOutputBacklog(t, 150*time.Millisecond, 1<<20)
	output := &ownedOutput{done: make(chan copyResult, 2)}
	sink := &stalledSink{stall: 1500 * time.Millisecond}
	childEnd, err := output.attach(sink)
	if err != nil {
		t.Fatal(err)
	}
	writer := childEnd.(*os.File)
	defer writer.Close()
	if _, err := writer.WriteString("first\n"); err != nil {
		t.Fatal(err)
	}
	// Let the copy take "first" into its stalled write, then queue more.
	time.Sleep(100 * time.Millisecond)
	if _, err := writer.WriteString("queued\n"); err != nil {
		t.Fatal(err)
	}
	started := time.Now()
	err = output.drain(context.Background(), 50*time.Millisecond)
	if !errors.Is(err, ErrOutputIncomplete) {
		t.Fatalf("err=%v, want ErrOutputIncomplete", err)
	}
	if elapsed := time.Since(started); elapsed > 5*time.Second {
		t.Fatalf("drain took %s", elapsed)
	}
	if got := sink.String(); got != "first\n" {
		t.Fatalf("sink=%q, want only the output read before the cut", got)
	}
}

// Cancelling the command while the drain waits for a writer outside the group
// to close a pipe ends the wait at once and fails the command. A pipe shown to
// be empty is released, so nothing already written is lost.
func TestOwnedOutputCancellationEndsTheDrainWindow(t *testing.T) {
	output := &ownedOutput{done: make(chan copyResult, 2)}
	sink := &slowSink{}
	childEnd, err := output.attach(sink)
	if err != nil {
		t.Fatal(err)
	}
	writer := childEnd.(*os.File)
	defer writer.Close()
	if _, err := writer.WriteString("kept\n"); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	timer := time.AfterFunc(200*time.Millisecond, cancel)
	defer timer.Stop()
	started := time.Now()
	err = output.drain(ctx, 30*time.Second)
	if elapsed := time.Since(started); elapsed > 5*time.Second {
		t.Fatalf("cancellation did not end the drain window: %s", elapsed)
	}
	if !errors.Is(err, context.Canceled) || errors.Is(err, ErrOutputIncomplete) {
		t.Fatalf("err=%v, want context.Canceled without ErrOutputIncomplete", err)
	}
	if got := sink.String(); !strings.Contains(got, "kept\n") {
		t.Fatalf("output written before cancellation was lost: %q", got)
	}
}
