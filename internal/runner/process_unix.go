//go:build unix

package runner

import (
	"context"
	"errors"
	"os/exec"
	"syscall"
	"time"
)

// startAndWait puts the adapter command in its own process group so Ctrl-C and
// deadlines stop browser/test-runner children as well as the immediate process.
func startAndWait(ctx context.Context, command *exec.Cmd) (error, *Termination) {
	command.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	command.WaitDelay = 50 * time.Millisecond
	if err := command.Start(); err != nil {
		return err, nil
	}
	done := make(chan error, 1)
	go func() { done <- command.Wait() }()
	select {
	case err := <-done:
		observed := observedSignal(command)
		if errors.Is(err, exec.ErrWaitDelay) && command.ProcessState != nil && command.ProcessState.Success() {
			err = nil
		}
		// A test launcher can exit successfully while leaving browser children in
		// its group. Clean those owned children before reporting completion.
		if !groupAlive(command.Process.Pid) {
			return err, observed
		}
		termination := stopGroup(command.Process.Pid, "cleanup", "leader exited while owned children remained")
		if observed != nil {
			termination.ObservedSignal = observed.ObservedSignal
		}
		return err, termination
	case <-ctx.Done():
		termination := &Termination{Detail: ctx.Err().Error()}
		if ctx.Err() == context.DeadlineExceeded {
			termination.Kind = "timeout"
		} else {
			termination.Kind = "canceled"
		}
		stopped := stopGroup(command.Process.Pid, termination.Kind, termination.Detail)
		termination.Signal, termination.EscalationSignal, termination.EscalationAfterMS = stopped.Signal, stopped.EscalationSignal, stopped.EscalationAfterMS
		err := <-done
		if observed := observedSignal(command); observed != nil {
			termination.ObservedSignal = observed.ObservedSignal
		}
		return err, termination
	}
}

func observedSignal(command *exec.Cmd) *Termination {
	if command.ProcessState == nil {
		return nil
	}
	status, ok := command.ProcessState.Sys().(syscall.WaitStatus)
	if !ok || !status.Signaled() {
		return nil
	}
	return &Termination{ObservedSignal: int(status.Signal())}
}

func stopGroup(pid int, kind, detail string) *Termination {
	termination := &Termination{Kind: kind, Detail: detail}
	if err := syscall.Kill(-pid, syscall.SIGTERM); err == nil {
		termination.Signal = "SIGTERM"
	}
	deadline := time.NewTimer(2 * time.Second)
	defer deadline.Stop()
	ticker := time.NewTicker(10 * time.Millisecond)
	defer ticker.Stop()
	for groupAlive(pid) {
		select {
		case <-ticker.C:
		case <-deadline.C:
			if err := syscall.Kill(-pid, syscall.SIGKILL); err == nil {
				termination.EscalationSignal, termination.EscalationAfterMS = "SIGKILL", 2000
			}
			// The leader has already been waited for. Descendants can briefly be
			// zombies while the system reaps them; do not hang a runner forever
			// waiting for kill(0) to stop observing that transient state.
			return termination
		}
	}
	return termination
}

func groupAlive(pid int) bool {
	err := syscall.Kill(-pid, 0)
	return err == nil || err == syscall.EPERM
}
