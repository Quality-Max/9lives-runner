//go:build unix || windows

package runner

import (
	"errors"
	"io"
	"os"
	"os/exec"
	"sync"
	"time"
)

// outputDrainTimeout is how long the runner waits, after the process group is
// gone, for every output pipe to reach end of file before it looks for pipes
// that a writer outside the group still holds open.
var outputDrainTimeout = 5 * time.Second

// outputReleasePoll is how often the runner rechecks a held pipe after the
// drain window.
const outputReleasePoll = 10 * time.Millisecond

// ErrOutputIncomplete reports that the runner stopped reading a command's
// output without being able to show that no written byte was still unread.
// A command with this error never counts as successful.
var ErrOutputIncomplete = errors.New("command output may be incomplete: the runner stopped reading a pipe it could not show was drained")

// ownedOutput copies a command's non-file stdout and stderr through pipes the
// runner reads itself. exec.Cmd would close its own pipes WaitDelay after the
// leader exits and discard whatever an exited process had already written but
// the copying goroutine had not yet read. Under load that dropped whole
// diagnostics, and a successful exit then reported truncated output as
// complete. The runner instead drains after it has stopped the process group,
// when every owned writer has closed its end.
type ownedOutput struct {
	streams []*ownedStream
	writers []*os.File
	done    chan copyResult
}

// ownedStream is the runner's read end of one output pipe. The mutex orders
// closing against inspecting the pipe, so the drain never queries a closed
// descriptor and the copy can tell a close by the drain from a read failure.
type ownedStream struct {
	mu            sync.Mutex
	reader        *os.File
	closed        bool
	closedByDrain bool
}

// copyResult keeps a sink that rejected output apart from a pipe that failed
// to read; a read ended by the drain's own close is neither.
type copyResult struct {
	sinkErr, readErr error
}

// attachOwnedOutput replaces command.Stdout and command.Stderr when they are
// not files. Call closeChildEnds after Start and drain before returning.
func attachOwnedOutput(command *exec.Cmd) (*ownedOutput, error) {
	output := &ownedOutput{done: make(chan copyResult, 2)}
	stdout, err := output.attach(command.Stdout)
	if err != nil {
		output.abort()
		return nil, err
	}
	stderr := stdout
	// Like exec.Cmd, one writer used for both streams gets one pipe, so its
	// writes are never concurrent.
	if command.Stderr == nil || !interfaceEqual(command.Stderr, command.Stdout) {
		if stderr, err = output.attach(command.Stderr); err != nil {
			output.abort()
			return nil, err
		}
	}
	command.Stdout, command.Stderr = stdout, stderr
	return output, nil
}

func (o *ownedOutput) attach(w io.Writer) (io.Writer, error) {
	if w == nil {
		return nil, nil
	}
	if _, ok := w.(*os.File); ok {
		return w, nil
	}
	reader, writer, err := os.Pipe()
	if err != nil {
		return nil, err
	}
	stream := &ownedStream{reader: reader}
	o.streams, o.writers = append(o.streams, stream), append(o.writers, writer)
	go func() { o.done <- stream.copyTo(w) }()
	return writer, nil
}

// copyTo copies until end of file. It does not use io.Copy, whose ReaderFrom
// path would merge a sink failure into the read error.
func (s *ownedStream) copyTo(w io.Writer) copyResult {
	// Closing on a sink failure lets writers fail instead of filling the pipe.
	defer s.close(false)
	buffer := make([]byte, 32*1024)
	for {
		n, err := s.reader.Read(buffer)
		if n > 0 {
			written, writeErr := w.Write(buffer[:n])
			if writeErr == nil && written != n {
				writeErr = io.ErrShortWrite
			}
			if writeErr != nil {
				return copyResult{sinkErr: writeErr}
			}
		}
		switch {
		case err == io.EOF:
			return copyResult{}
		case err != nil && s.releasedByDrain():
			return copyResult{}
		case err != nil:
			return copyResult{readErr: err}
		}
	}
}

func (s *ownedStream) close(byDrain bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.closed {
		s.closed, s.closedByDrain = true, byDrain
		_ = s.reader.Close()
	}
}

func (s *ownedStream) releasedByDrain() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.closedByDrain
}

// release closes the read end once nothing written to it is left unread and
// a writer still holds it open. Only a process outside the stopped group can
// hold it then, and closing loses nothing it wrote before this check. While
// bytes or end of file are pending the copy still owns them, however slowly
// its sink accepts them. It reports whether it closed a pipe it could not
// inspect, which may have cut pending output.
func (s *ownedStream) release() (cut bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return false
	}
	held, err := pipeHeldEmpty(s.reader)
	if err == nil && !held {
		return false
	}
	s.closed, s.closedByDrain = true, true
	_ = s.reader.Close()
	return err != nil
}

// closeChildEnds closes the parent's copies of the child's pipe ends, so each
// copy ends when the processes holding the other copies exit.
func (o *ownedOutput) closeChildEnds() {
	for _, writer := range o.writers {
		_ = writer.Close()
	}
}

func (o *ownedOutput) abort() {
	o.closeChildEnds()
	_ = o.drain(0)
}

// drain waits for every copy to finish and reports sink failures, read
// failures and pipes closed without proof that they were drained. After
// window it releases pipes that are empty but still held open by an escaped
// writer; that alone is not an error. It never abandons bytes already in a
// pipe, so a slow sink delays the drain rather than truncating it.
func (o *ownedOutput) drain(window time.Duration) error {
	var errs []error
	cut := false
	collect := func(result copyResult) {
		errs = append(errs, result.sinkErr, result.readErr)
	}
	pending := len(o.streams)
	timer := time.NewTimer(window)
	defer timer.Stop()
	for waiting := true; waiting && pending > 0; {
		select {
		case result := <-o.done:
			collect(result)
			pending--
		case <-timer.C:
			waiting = false
		}
	}
	if pending > 0 {
		ticker := time.NewTicker(outputReleasePoll)
		defer ticker.Stop()
		for pending > 0 {
			for _, stream := range o.streams {
				if stream.release() {
					cut = true
				}
			}
			select {
			case result := <-o.done:
				collect(result)
				pending--
			case <-ticker.C:
			}
		}
	}
	if cut {
		errs = append(errs, ErrOutputIncomplete)
	}
	return errors.Join(errs...)
}

// finish drains output and merges the result into the command's error the way
// startAndWait reports exec.Cmd.Wait. A pipe an escaped writer held open does
// not turn the leader's exit status into a failure, but a sink that failed to
// accept output, a failed read or possibly incomplete output does fail an
// otherwise successful command.
func (o *ownedOutput) finish(err error) error {
	if drainErr := o.drain(outputDrainTimeout); err == nil {
		return drainErr
	}
	return err
}

// interfaceEqual compares writers without panicking on non-comparable types,
// as exec.Cmd does.
func interfaceEqual(a, b any) (equal bool) {
	defer func() {
		if recover() != nil {
			equal = false
		}
	}()
	return a == b
}
