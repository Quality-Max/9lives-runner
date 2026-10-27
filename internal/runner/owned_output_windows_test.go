//go:build windows

package runner

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"testing"
	"time"
)

// windowsOutputHelper runs this test binary as a command that writes to
// stdout and stderr, pauses so the runner's copy falls behind, writes again
// and exits with status. The pipes it writes to are the runner's owned pipes,
// so the drain inspects them with PeekNamedPipe.
func windowsOutputHelper(status int) *exec.Cmd {
	cmd := exec.Command(os.Args[0], "-test.run=^TestWindowsOutputHelper$", "--", strconv.Itoa(status))
	cmd.Env = append(os.Environ(), "NINELIVES_WINDOWS_OUTPUT_HELPER=1")
	return cmd
}

func TestWindowsOutputHelper(t *testing.T) {
	if os.Getenv("NINELIVES_WINDOWS_OUTPUT_HELPER") != "1" {
		return
	}
	status, err := strconv.Atoi(os.Args[len(os.Args)-1])
	if err != nil {
		os.Exit(20)
	}
	fmt.Fprint(os.Stdout, "out-1\n")
	fmt.Fprint(os.Stderr, "err-1\n")
	time.Sleep(200 * time.Millisecond)
	fmt.Fprint(os.Stdout, "out-2\n")
	fmt.Fprint(os.Stderr, "err-2\n")
	os.Exit(status)
}

// Output a process wrote before it exited reaches a sink that stalls past the
// exit, for a successful and a failing exit; exec.Cmd.WaitDelay used to drop
// it 50ms after the exit.
func TestWindowsOwnedCommandKeepsOutputWrittenBeforeExit(t *testing.T) {
	for _, status := range []int{0, 1} {
		stdout, stderr := &stalledSink{}, &stalledSink{}
		command := windowsOutputHelper(status)
		command.Stdout, command.Stderr = stdout, stderr
		err := RunOwnedCommand(context.Background(), command)
		var exitErr *exec.ExitError
		if status == 0 && err != nil || status == 1 && (!errors.As(err, &exitErr) || exitErr.ExitCode() != 1) {
			t.Fatalf("exit %d: err=%v", status, err)
		}
		if stdout.String() != "out-1\nout-2\n" || stderr.String() != "err-1\nerr-2\n" {
			t.Fatalf("exit %d lost output: stdout=%q stderr=%q", status, stdout.String(), stderr.String())
		}
	}
}

// Every writer has exited, so bytes left in the pipe after the drain window
// still reach a slow sink: the drain keeps reading until end of file.
func TestWindowsOwnedCommandKeepsPendingOutputPastTheDrainWindow(t *testing.T) {
	shortenOutputDrain(t, 100*time.Millisecond)
	stdout := &slowSink{delay: 400 * time.Millisecond}
	command := windowsOutputHelper(0)
	command.Stdout = stdout
	if err := RunOwnedCommand(context.Background(), command); err != nil {
		t.Fatal(err)
	}
	if stdout.String() != "out-1\nout-2\n" {
		t.Fatalf("pending output was cut: stdout=%q", stdout.String())
	}
}

// A sink that rejects output fails a command that exited 0, and keeps the exit
// status of one that failed.
func TestWindowsOwnedCommandReportsSinkFailure(t *testing.T) {
	for _, status := range []int{0, 3} {
		command := windowsOutputHelper(status)
		command.Stdout = failingSink{}
		err := RunOwnedCommand(context.Background(), command)
		if !errors.Is(err, errSinkRejected) {
			t.Fatalf("exit %d: sink failure was not reported: err=%v", status, err)
		}
		var exitErr *exec.ExitError
		if status != 0 && (!errors.As(err, &exitErr) || exitErr.ExitCode() != status) {
			t.Fatalf("exit %d: exit status lost: err=%v", status, err)
		}
	}
}
