//go:build unix

package runner

import (
	"context"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

func childPID(t *testing.T, output ProcessOutput) int {
	t.Helper()
	pid, err := strconv.Atoi(strings.TrimSpace(string(output.Stdout)))
	if err != nil {
		t.Fatalf("child PID output %q: %v", output.Stdout, err)
	}
	return pid
}

func requireGone(t *testing.T, pid int) {
	t.Helper()
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		if err := syscall.Kill(pid, 0); err == syscall.ESRCH {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	_ = syscall.Kill(pid, syscall.SIGKILL)
	t.Fatalf("owned child %d survived runner cleanup", pid)
}

func TestProcessExecutorCleansChildAfterLeaderSuccess(t *testing.T) {
	output := (localProcessExecutor{}).Run(context.Background(), Job{Command: []string{"/bin/sh", "-c", "sleep 30 & echo $!"}}, 1024)
	if output.Err != nil {
		t.Fatal(output.Err)
	}
	requireGone(t, childPID(t, output))
}

func TestProcessExecutorEscalatesWhenTerminatedLeaderLeavesChild(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
	defer cancel()
	output := (localProcessExecutor{}).Run(ctx, Job{Command: []string{"/bin/sh", "-c", "trap 'exit 0' TERM; (trap '' TERM; exec sleep 30) & echo $!; while :; do sleep 1; done"}}, 1024)
	if output.Termination == nil || output.Termination.Signal != "SIGTERM" || output.Termination.EscalationSignal != "SIGKILL" {
		t.Fatalf("expected actual TERM then KILL evidence: %#v", output.Termination)
	}
	requireGone(t, childPID(t, output))
}
