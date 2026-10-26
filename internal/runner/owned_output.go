package runner

import (
	"errors"
	"io"
	"os"
	"os/exec"
	"time"
)

// outputDrainTimeout bounds reading a command's output after its process group
// is gone. Only a writer that escaped the group can hold a pipe open that long.
var outputDrainTimeout = 5 * time.Second

// ownedOutput copies a command's non-file stdout and stderr through pipes the
// runner reads itself. exec.Cmd would close its own pipes WaitDelay after the
// leader exits and discard whatever an exited process had already written but
// the copying goroutine had not yet read. Under load that dropped whole
// diagnostics, and a successful exit then reported truncated output as
// complete. The runner instead drains after it has stopped the process group,
// when every owned writer has closed its end.
type ownedOutput struct {
	readers, writers []*os.File
	done             chan error
}

// attachOwnedOutput replaces command.Stdout and command.Stderr when they are
// not files. Call closeChildEnds after Start and drain before returning.
func attachOwnedOutput(command *exec.Cmd) (*ownedOutput, error) {
	output := &ownedOutput{done: make(chan error, 2)}
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
	o.readers, o.writers = append(o.readers, reader), append(o.writers, writer)
	go func() {
		_, err := io.Copy(w, reader)
		_ = reader.Close()
		o.done <- err
	}()
	return writer, nil
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

// drain waits for every copy to reach end of file. After timeout it closes the
// read ends, because a process outside the group still holds a write end, and
// returns exec.ErrWaitDelay.
func (o *ownedOutput) drain(timeout time.Duration) error {
	var first error
	timer := time.NewTimer(timeout)
	defer timer.Stop()
	for pending := len(o.readers); pending > 0; pending-- {
		select {
		case err := <-o.done:
			if first == nil {
				first = err
			}
		case <-timer.C:
			for _, reader := range o.readers {
				_ = reader.Close()
			}
			for ; pending > 0; pending-- {
				<-o.done
			}
			return exec.ErrWaitDelay
		}
	}
	return first
}

// finish drains output and merges the result into the command's error the way
// startAndWait reports exec.Cmd.Wait: a drain bounded by an escaped writer
// does not turn the leader's exit status into a failure, but a sink that
// failed to accept output does fail an otherwise successful command.
func (o *ownedOutput) finish(err error) error {
	drainErr := o.drain(outputDrainTimeout)
	if err == nil && drainErr != nil && !errors.Is(drainErr, exec.ErrWaitDelay) {
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
