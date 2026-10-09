//go:build !unix && !windows

package runner

import (
	"context"
	"os/exec"
)

func startAndWait(ctx context.Context, command *exec.Cmd) (error, *Termination) {
	if err := command.Start(); err != nil {
		return err, nil
	}
	done := make(chan error, 1)
	go func() { done <- command.Wait() }()
	select {
	case err := <-done:
		return err, nil
	case <-ctx.Done():
		termination := &Termination{Detail: ctx.Err().Error()}
		if ctx.Err() == context.DeadlineExceeded {
			termination.Kind = "timeout"
		} else {
			termination.Kind = "canceled"
		}
		if err := command.Process.Kill(); err == nil {
			termination.Signal = "SIGKILL"
		}
		return <-done, termination
	}
}
