//go:build unix

package runner

import (
	"bytes"
	"context"
	"os/exec"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"
)

// stalledSink holds its first write long enough that the process has exited
// before the copy reads the rest of the pipe, as a descheduled copying
// goroutine on a loaded machine does.
type stalledSink struct {
	mu     sync.Mutex
	data   bytes.Buffer
	writes int
}

func (s *stalledSink) Write(p []byte) (int, error) {
	s.mu.Lock()
	s.writes++
	first := s.writes == 1
	s.mu.Unlock()
	if first {
		time.Sleep(600 * time.Millisecond)
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

// Output an exited process already wrote must reach the caller even when the
// reader falls behind; exec.Cmd.WaitDelay used to discard it 50ms after exit,
// which lost provider diagnostics and reported truncated stdout as success.
func TestOwnedCommandKeepsOutputWrittenBeforeExit(t *testing.T) {
	for _, status := range []string{"0", "1"} {
		stdout, stderr := &stalledSink{}, &stalledSink{}
		command := exec.Command("/bin/sh", "-c", "printf 'out-1\\n'; printf 'err-1\\n' >&2; sleep 0.2; printf 'out-2\\n'; printf 'err-2\\n' >&2; exit "+status)
		command.Stdout, command.Stderr = stdout, stderr
		err := RunOwnedCommand(context.Background(), command)
		if status == "0" && err != nil || status == "1" && (err == nil || !strings.Contains(err.Error(), "exit status 1")) {
			t.Fatalf("exit %s: err=%v", status, err)
		}
		if stdout.String() != "out-1\nout-2\n" || stderr.String() != "err-1\nerr-2\n" {
			t.Fatalf("exit %s lost output: stdout=%q stderr=%q", status, stdout.String(), stderr.String())
		}
	}
}

// A writer that left the owned process group can hold the pipe open forever;
// the drain stays bounded and keeps what the leader wrote.
func TestOwnedCommandBoundsOutputHeldOutsideTheGroup(t *testing.T) {
	if _, err := exec.LookPath("perl"); err != nil {
		t.Skip("perl provides setsid for the escaped writer")
	}
	previous := outputDrainTimeout
	outputDrainTimeout = 200 * time.Millisecond
	t.Cleanup(func() { outputDrainTimeout = previous })
	var stdout bytes.Buffer
	command := exec.Command("/bin/sh", "-c", `perl -MPOSIX -e 'POSIX::setsid(); print "$$\n"; STDOUT->flush; sleep 30' & sleep 0.3; echo leader`)
	command.Stdout = &stdout
	started := time.Now()
	if err := RunOwnedCommand(context.Background(), command); err != nil {
		t.Fatal(err)
	}
	if elapsed := time.Since(started); elapsed > 5*time.Second {
		t.Fatalf("drain was not bounded: %s", elapsed)
	}
	lines := strings.Fields(stdout.String())
	if len(lines) != 2 || lines[1] != "leader" {
		t.Fatalf("stdout=%q", stdout.String())
	}
	if pid, err := strconv.Atoi(lines[0]); err == nil {
		_ = syscall.Kill(pid, syscall.SIGKILL)
	}
}

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
