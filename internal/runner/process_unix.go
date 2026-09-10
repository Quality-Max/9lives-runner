//go:build unix

package runner

import (
	"context"
	"os/exec"
	"syscall"
	"time"
)

// startAndWait puts the adapter command in its own process group so Ctrl-C and
// deadlines stop browser/test-runner children as well as the immediate process.
func startAndWait(ctx context.Context, command *exec.Cmd) error {
	command.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	if err := command.Start(); err != nil {
		return err
	}
	done := make(chan error, 1)
	go func() { done <- command.Wait() }()
	select {
	case err := <-done:
		return err
	case <-ctx.Done():
		_ = syscall.Kill(-command.Process.Pid, syscall.SIGTERM)
		select {
		case err := <-done:
			return err
		case <-time.After(2 * time.Second):
			_ = syscall.Kill(-command.Process.Pid, syscall.SIGKILL)
			return <-done
		}
	}
}
