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
// Track ancestry too: detached web servers are outside that process group.
func startAndWait(ctx context.Context, command *exec.Cmd) (error, *Termination) {
	if err := ctx.Err(); err != nil {
		return err, nil
	}
	command.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	command.WaitDelay = 50 * time.Millisecond
	if err := command.Start(); err != nil {
		return err, nil
	}
	tree := newUnixProcessTree(command.Process.Pid)
	tree.refresh()
	done := make(chan error, 1)
	go func() { done <- command.Wait() }()
	ticker := time.NewTicker(50 * time.Millisecond)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			tree.refresh()
		case err := <-done:
			tree.refresh()
			observed := observedSignal(command)
			if errors.Is(err, exec.ErrWaitDelay) && command.ProcessState != nil && command.ProcessState.Success() {
				err = nil
			}
			// A successful launcher can leave children in its group or a separate
			// session. Clean owned children before reporting completion.
			if !groupAlive(command.Process.Pid) && len(tree.live) == 0 {
				return errors.Join(err, tree.err, ctx.Err()), observed
			}
			termination := stopGroup(command.Process.Pid, "cleanup", "leader exited while owned children remained", tree)
			if observed != nil {
				termination.ObservedSignal = observed.ObservedSignal
			}
			return errors.Join(err, tree.err, ctx.Err()), termination
		case <-ctx.Done():
			termination := &Termination{Detail: ctx.Err().Error()}
			if ctx.Err() == context.DeadlineExceeded {
				termination.Kind = "timeout"
			} else {
				termination.Kind = "canceled"
			}
			stopped := stopGroup(command.Process.Pid, termination.Kind, termination.Detail, tree)
			termination.Signal, termination.EscalationSignal, termination.EscalationAfterMS = stopped.Signal, stopped.EscalationSignal, stopped.EscalationAfterMS
			err := <-done
			if observed := observedSignal(command); observed != nil {
				termination.ObservedSignal = observed.ObservedSignal
			}
			return errors.Join(err, tree.err, ctx.Err()), termination
		}
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

func stopGroup(pid int, kind, detail string, tree *unixProcessTree) *Termination {
	termination := &Termination{Kind: kind, Detail: detail}
	tree.refresh() // Capture ownership before terminating parents and reparenting.
	sent := syscall.Kill(-pid, syscall.SIGTERM) == nil
	if tree.signal(syscall.SIGTERM) || sent {
		termination.Signal = "SIGTERM"
	}
	deadline := time.NewTimer(2 * time.Second)
	defer deadline.Stop()
	ticker := time.NewTicker(50 * time.Millisecond)
	defer ticker.Stop()
	for groupAlive(pid) || len(tree.live) > 0 {
		select {
		case <-ticker.C:
			tree.refresh()
			tree.signal(syscall.SIGTERM)
		case <-deadline.C:
			tree.refresh()
			sent := syscall.Kill(-pid, syscall.SIGKILL) == nil
			if tree.signal(syscall.SIGKILL) || sent {
				termination.EscalationSignal, termination.EscalationAfterMS = "SIGKILL", 2000
			}
			// Releasing a listening socket can lag signal delivery. Wait for live
			// owned processes, not orphan zombies, before allowing the next job.
			until := time.Now().Add(time.Second)
			for len(tree.live) > 0 && time.Now().Before(until) {
				time.Sleep(50 * time.Millisecond)
				tree.refresh()
				tree.signal(syscall.SIGKILL)
			}
			if len(tree.live) > 0 && tree.err == nil {
				tree.err = errors.New("owned process cleanup did not finish")
			}
			// Descendants can briefly be zombies while the system reaps them;
			// do not hang a runner forever
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
