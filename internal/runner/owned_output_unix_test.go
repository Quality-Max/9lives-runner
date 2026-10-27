//go:build unix

package runner

import (
	"bytes"
	"context"
	"errors"
	"os"
	"strings"
	"testing"
	"time"
)

// These tests simulate a writer that outlives the stopped process group and
// still holds a pipe open. That happens on Unix, where a process can leave the
// group (setsid). On Windows every owned process stays in a job object that is
// stopped before the drain, and pipeHeldEmpty does not inspect pipes there.

// pipeHeldEmpty is what lets the drain close a pipe without losing output, so
// each of its three states is checked with poll.
func TestPipeHeldEmptyTellsPendingDataHeldEmptyAndEndOfFile(t *testing.T) {
	reader, writer, err := os.Pipe()
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
// read error is the drain's own close rather than a read failure.
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
	if cut := output.streams[0].release(); cut {
		t.Fatal("releasing a pipe shown to be empty reported a cut")
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
