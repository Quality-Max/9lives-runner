//go:build unix

package runner

import (
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
