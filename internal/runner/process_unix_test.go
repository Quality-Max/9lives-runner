//go:build unix

package runner

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
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
	shortenOutputDrain(t, 200*time.Millisecond)
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
	// Both writers' lines are kept; their order depends on when perl starts,
	// which on a loaded machine can be after the leader has printed.
	var pid int
	leader := false
	for _, line := range strings.Fields(stdout.String()) {
		if line == "leader" {
			leader = true
		} else if n, err := strconv.Atoi(line); err == nil {
			pid = n
		}
	}
	if pid > 0 {
		_ = syscall.Kill(pid, syscall.SIGKILL)
	}
	if len(strings.Fields(stdout.String())) != 2 || !leader || pid == 0 {
		t.Fatalf("stdout=%q", stdout.String())
	}
}

// slowSink accepts every write but takes delay per call, as a sink behind a
// slow consumer or a copy goroutine starved on a loaded machine does.
type slowSink struct {
	stalledSink
	delay time.Duration
}

func (s *slowSink) Write(p []byte) (int, error) {
	time.Sleep(s.delay)
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.data.Write(p)
}

// The drain window bounds only a pipe a writer outside the group still holds
// open. When every writer has exited, bytes already in the pipe must reach a
// slow sink however long it takes; cutting them and reporting exit 0 as
// success returned truncated output as complete.
func TestOwnedCommandKeepsPendingOutputPastTheDrainWindow(t *testing.T) {
	shortenOutputDrain(t, 100*time.Millisecond)
	stdout := &slowSink{delay: 400 * time.Millisecond}
	command := exec.Command("/bin/sh", "-c", "printf 'out-1\\n'; sleep 0.05; printf 'out-2\\n'; sleep 0.05; printf 'out-3\\n'")
	command.Stdout = stdout
	if err := RunOwnedCommand(context.Background(), command); err != nil {
		t.Fatal(err)
	}
	if stdout.String() != "out-1\nout-2\nout-3\n" {
		t.Fatalf("pending output was cut: stdout=%q", stdout.String())
	}
}

var errSinkRejected = errors.New("sink rejected output")

type failingSink struct{}

func (failingSink) Write([]byte) (int, error) { return 0, errSinkRejected }

// A sink that could not accept output fails an otherwise successful command,
// even when a writer outside the group keeps the other stream open until the
// drain window ends.
func TestOwnedCommandReportsSinkFailureWhileAnotherStreamIsHeld(t *testing.T) {
	if _, err := exec.LookPath("perl"); err != nil {
		t.Skip("perl provides setsid for the escaped writer")
	}
	shortenOutputDrain(t, 200*time.Millisecond)
	stderr := &slowSink{}
	command := exec.Command("/bin/sh", "-c", `perl -MPOSIX -e 'POSIX::setsid(); print STDERR "$$\n"; STDERR->flush; sleep 30' >/dev/null & sleep 0.3; echo leader`)
	command.Stdout, command.Stderr = failingSink{}, stderr
	err := RunOwnedCommand(context.Background(), command)
	if pid, convErr := strconv.Atoi(strings.TrimSpace(stderr.String())); convErr == nil {
		_ = syscall.Kill(pid, syscall.SIGKILL)
	}
	if !errors.Is(err, errSinkRejected) {
		t.Fatalf("sink failure was not reported: err=%v stderr=%q", err, stderr.String())
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

// Model Playwright's webServer launcher: a server creates a separate session,
// so killing the adapter's process group cannot reach it.
func TestUnixDetachedProcessHelper(t *testing.T) {
	if os.Getenv("NINELIVES_UNIX_PROCESS_HELPER") != "1" {
		return
	}
	mode, dir := os.Args[len(os.Args)-2], os.Args[len(os.Args)-1]
	if mode == "server" {
		terminated := make(chan os.Signal, 1)
		signal.Notify(terminated, syscall.SIGTERM)
		go func() {
			for range terminated {
				_ = os.WriteFile(filepath.Join(dir, "term"), nil, 0600)
			}
		}()
		listener, err := net.Listen("tcp", "127.0.0.1:0")
		if err != nil {
			os.Exit(7)
		}
		data, _ := json.Marshal(struct {
			PID     int
			Address string
		}{os.Getpid(), listener.Addr().String()})
		if os.WriteFile(filepath.Join(dir, "server.json"), data, 0600) != nil {
			os.Exit(8)
		}
		for {
			connection, err := listener.Accept()
			if err != nil {
				os.Exit(9)
			}
			connection.Close()
		}
	}
	child := unixDetachedHelper("server", dir)
	child.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	if child.Start() != nil {
		os.Exit(10)
	}
	for {
		if mode == "exit-leader" {
			if _, err := os.Stat(filepath.Join(dir, "exit")); err == nil {
				os.Exit(0)
			}
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func unixDetachedHelper(mode, dir string) *exec.Cmd {
	cmd := exec.Command(os.Args[0], "-test.run=^TestUnixDetachedProcessHelper$", "--", mode, dir)
	cmd.Env = append(os.Environ(), "NINELIVES_UNIX_PROCESS_HELPER=1")
	return cmd
}

func TestUnixOwnedDetachedServer(t *testing.T) {
	for _, mode := range []string{"cancel", "timeout", "normal-exit", "cancel-during-cleanup"} {
		t.Run(mode, func(t *testing.T) {
			dir := t.TempDir()
			// An unrelated listener must survive cleanup.
			sentinel, err := net.Listen("tcp", "127.0.0.1:0")
			if err != nil {
				t.Fatal(err)
			}
			defer sentinel.Close()
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			if mode == "timeout" {
				var deadlineCancel context.CancelFunc
				ctx, deadlineCancel = context.WithTimeout(ctx, 4*time.Second)
				defer deadlineCancel()
			}
			leader := "leader"
			if mode == "normal-exit" || mode == "cancel-during-cleanup" {
				leader = "exit-leader"
			}
			type result struct {
				err         error
				termination *Termination
			}
			done := make(chan result, 1)
			go func() {
				err, termination := startAndWait(ctx, unixDetachedHelper(leader, dir))
				done <- result{err, termination}
			}()
			var server struct {
				PID     int
				Address string
			}
			readyBy := time.Now().Add(3 * time.Second)
			for server.PID == 0 {
				if data, err := os.ReadFile(filepath.Join(dir, "server.json")); err == nil {
					_ = json.Unmarshal(data, &server)
				}
				if time.Now().After(readyBy) {
					t.Fatal("detached server did not become ready")
				}
				time.Sleep(10 * time.Millisecond)
			}
			defer syscall.Kill(server.PID, syscall.SIGKILL)
			connection, err := net.DialTimeout("tcp", server.Address, time.Second)
			if err != nil {
				t.Fatal(err)
			}
			connection.Close()
			// Keep the server observable before the launcher exits or is canceled.
			time.Sleep(750 * time.Millisecond)
			if mode == "cancel" {
				cancel()
			} else if mode == "normal-exit" || mode == "cancel-during-cleanup" {
				if err := os.WriteFile(filepath.Join(dir, "exit"), nil, 0600); err != nil {
					t.Fatal(err)
				}
				if mode == "cancel-during-cleanup" {
					// TERM proves the successful leader's completion branch is
					// already cleaning up the detached server before cancellation.
					readyBy := time.Now().Add(3 * time.Second)
					for {
						if _, err := os.Stat(filepath.Join(dir, "term")); err == nil {
							break
						}
						if time.Now().After(readyBy) {
							t.Fatal("successful launcher did not begin detached server cleanup")
						}
						time.Sleep(10 * time.Millisecond)
					}
					cancel()
				}
			}
			select {
			case result := <-done:
				kind := map[string]string{"cancel": "canceled", "timeout": "timeout", "normal-exit": "cleanup", "cancel-during-cleanup": "cleanup"}[mode]
				if result.termination == nil || result.termination.Kind != kind || result.termination.EscalationSignal != "SIGKILL" {
					t.Errorf("expected owned detached server cleanup for %s: %+v", mode, result.termination)
				}
				if mode == "normal-exit" && result.err != nil {
					t.Errorf("successful launcher exit: %v", result.err)
				}
				if mode == "cancel-during-cleanup" && !errors.Is(result.err, context.Canceled) {
					t.Errorf("cancellation during completion cleanup must not report success: %v", result.err)
				}
			case <-time.After(10 * time.Second):
				t.Fatal("owned command did not finish")
			}
			replacement, err := net.Listen("tcp", server.Address)
			if err != nil {
				t.Fatalf("detached server left its port occupied: %v", err)
			}
			replacement.Close()
			connection, err = net.DialTimeout("tcp", sentinel.Addr().String(), time.Second)
			if err != nil {
				t.Fatalf("unrelated listener was stopped: %v", err)
			}
			connection.Close()
		})
	}
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

func TestUnixOwnershipInspectionFailureCannotPass(t *testing.T) {
	t.Setenv("PATH", t.TempDir()) // The command exists, but ps is unavailable.
	err, _ := startAndWait(context.Background(), exec.Command("/bin/sh", "-c", "exit 0"))
	if err == nil || !strings.Contains(err.Error(), "inspect owned process ancestry") {
		t.Fatalf("successful child must not hide failed ownership inspection: %v", err)
	}
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

// shortenOutputDrain sets the drain window for one test and restores it.
func shortenOutputDrain(t *testing.T, window time.Duration) {
	t.Helper()
	previous := outputDrainTimeout.Swap(int64(window))
	t.Cleanup(func() { outputDrainTimeout.Store(previous) })
}

// A command that exits non-zero keeps its exit status for callers and also
// reports a sink that rejected its output, instead of dropping the drain error.
func TestOwnedCommandJoinsDrainErrorsWithAFailedExit(t *testing.T) {
	command := exec.Command("/bin/sh", "-c", "echo out; exit 3")
	command.Stdout = failingSink{}
	err := RunOwnedCommand(context.Background(), command)
	var exitErr *exec.ExitError
	if !errors.As(err, &exitErr) || exitErr.ExitCode() != 3 || !errors.Is(err, errSinkRejected) {
		t.Fatalf("err=%v, want exit status 3 joined with the sink failure", err)
	}
}
