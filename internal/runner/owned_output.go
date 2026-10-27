//go:build unix || windows

package runner

import (
	"context"
	"errors"
	"io"
	"os"
	"os/exec"
	"sync"
	"sync/atomic"
	"time"
)

// outputDrainTimeout is how long the runner waits, after the process group is
// gone, for every output pipe to reach end of file before it looks for pipes
// that a writer outside the group still holds open. It is atomic so a test
// that shortens it cannot race a drain running in another goroutine.
var outputDrainTimeout atomic.Int64

// After the window, the only output still owed to a sink is the backlog an
// exited writer left in the pipe: at most one pipe buffer, which an
// unprivileged writer can grow to 1 MiB on Linux and is smaller elsewhere.
// outputBacklogBytes bounds what the copy may read past the window, one copy
// buffer more than that because the copy counts a read only after it returns;
// anything beyond can only come from a writer outside the stopped group that
// is still writing. outputBacklogTimeout bounds how long the backlog may take
// to reach a slow sink, so a writer that trickles just fast enough to keep the
// pipe readable cannot hold the drain open either. Reaching either bound
// closes the pipe and reports ErrOutputIncomplete unless the pipe was shown
// to be empty. Both are atomic for the same reason as the window.
var outputBacklogTimeout, outputBacklogBytes atomic.Int64

const outputCopyBuffer = 32 * 1024

func init() {
	outputDrainTimeout.Store(int64(5 * time.Second))
	outputBacklogTimeout.Store(int64(30 * time.Second))
	outputBacklogBytes.Store(1<<20 + outputCopyBuffer)
}

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
	// read counts bytes the copy has taken from the pipe. allowance is the
	// count past which, after the drain window, the drain stops reading; only
	// the drain goroutine uses it.
	read      atomic.Int64
	allowance int64
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
	reader, writer, err := newOwnedOutputPipe()
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
	buffer := make([]byte, outputCopyBuffer)
	for {
		n, err := s.reader.Read(buffer)
		if n > 0 {
			s.read.Add(int64(n))
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
// bytes or end of file are pending the copy still owns them, until it has
// read past its allowance: those bytes come from a writer that is still
// writing, so release closes the pipe instead of reading on. It reports
// whether it closed a pipe that may have held unread output: one it could not
// inspect or one cut at the allowance.
func (s *ownedStream) release() (cut bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return false
	}
	held, err := pipeHeldEmpty(s.reader)
	if err == nil && !held && s.read.Load() <= s.allowance {
		return false
	}
	s.closed, s.closedByDrain = true, true
	_ = s.reader.Close()
	return err != nil || !held
}

// cut closes the read end when the backlog time limit has passed. Only a pipe
// shown to be empty is closed without reporting possible loss.
func (s *ownedStream) cut() bool {
	if s.release() {
		return true
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return false
	}
	s.closed, s.closedByDrain = true, true
	_ = s.reader.Close()
	return true
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
	_ = o.drain(context.Background(), 0)
}

// drain waits for every copy to finish and reports sink failures, read
// failures and pipes closed without proof that they were drained.
//
// Until window ends it waits for every pipe to reach end of file. After it,
// it releases pipes that are empty but still held open by an escaped writer;
// that alone is not an error. It keeps reading bytes already in a pipe, so a
// slow sink delays the drain rather than truncating it, but only within the
// outputBacklogBytes and outputBacklogTimeout bounds: a writer outside the
// group that keeps the pipe readable is cut off with ErrOutputIncomplete.
//
// The window also ends when ctx is done. A context that ends during the
// window is reported, so a deadline or cancellation can interrupt waiting for
// an escaped writer and the command does not count as successful; a context
// already done when the drain starts, as after a timeout or cancellation
// stopped the group, is already part of the command's result. The backlog is
// not cut by ctx: it is bounded, and it is the output that explains why the
// command was stopped.
//
// The drain does not return while a copy is inside its sink's Write, which
// would let the copy write into the caller's sink after the command returned.
// A sink that never returns blocks the drain, as it would block exec.Cmd.
func (o *ownedOutput) drain(ctx context.Context, window time.Duration) error {
	var errs []error
	cut := false
	pending := len(o.streams)
	collect := func(result copyResult) {
		errs = append(errs, result.sinkErr, result.readErr)
		pending--
	}
	interruptible := ctx.Err() == nil
	timer := time.NewTimer(window)
	defer timer.Stop()
	for waiting := true; waiting && pending > 0; {
		select {
		case result := <-o.done:
			collect(result)
		case <-timer.C:
			waiting = false
		case <-ctx.Done():
			waiting = false
			if interruptible {
				errs = append(errs, ctx.Err())
			}
		}
	}
	if pending > 0 {
		allowance := outputBacklogBytes.Load()
		for _, stream := range o.streams {
			stream.allowance = stream.read.Load() + allowance
		}
		limit := time.NewTimer(time.Duration(outputBacklogTimeout.Load()))
		defer limit.Stop()
		ticker := time.NewTicker(outputReleasePoll)
		defer ticker.Stop()
		release := (*ownedStream).release
		for pending > 0 {
			for _, stream := range o.streams {
				if release(stream) {
					cut = true
				}
			}
			select {
			case result := <-o.done:
				collect(result)
			case <-ticker.C:
			case <-limit.C:
				release = (*ownedStream).cut
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
// accept output, a failed read, possibly incomplete output or a context that
// ended during the drain window does fail an otherwise successful command.
//
// A command that already failed keeps its exit error, joined with any drain
// error, so a sink failure or incomplete output is never hidden behind a
// non-zero exit; callers read the exit code with errors.As.
func (o *ownedOutput) finish(ctx context.Context, err error) error {
	drainErr := o.drain(ctx, time.Duration(outputDrainTimeout.Load()))
	switch {
	case drainErr == nil:
		return err
	case err == nil:
		return drainErr
	}
	return errors.Join(err, drainErr)
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
