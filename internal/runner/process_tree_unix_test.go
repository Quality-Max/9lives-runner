//go:build unix

package runner

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

func TestUnixProcessSnapshotBoundsInheritedStdout(t *testing.T) {
	dir := t.TempDir()
	marker := filepath.Join(dir, "child.pid")
	// The wrapper exits successfully with a valid row while its child keeps
	// the stdout pipe open. A context deadline alone cannot bound pipe drain.
	wrapper := "#!/bin/sh\n/bin/sleep 30 &\nprintf '%s\\n' \"$!\" > \"$NINELIVES_PS_PID_FILE\"\nprintf '1 0 1 S Mon Oct 26 00:00:00 2026\\n'\n"
	if err := os.WriteFile(filepath.Join(dir, "ps"), []byte(wrapper), 0700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir)
	t.Setenv("NINELIVES_PS_PID_FILE", marker)
	done := make(chan error, 1)
	go func() {
		_, err := unixProcessTable()
		done <- err
	}()
	var child int
	readyBy := time.Now().Add(3 * time.Second)
	for child == 0 {
		if data, err := os.ReadFile(marker); err == nil {
			child, _ = strconv.Atoi(strings.TrimSpace(string(data)))
		}
		if time.Now().After(readyBy) {
			t.Fatal("ps wrapper did not start its pipe-holding child")
		}
		time.Sleep(10 * time.Millisecond)
	}
	defer syscall.Kill(child, syscall.SIGKILL)
	select {
	case err := <-done:
		if err == nil || !strings.Contains(err.Error(), "WaitDelay") {
			t.Fatalf("incomplete snapshot must fail with bounded pipe-drain error: %v", err)
		}
	case <-time.After(2 * time.Second):
		// Release the pipe before failing so the regression itself leaves no
		// blocked snapshot goroutine or synthetic child process behind.
		_ = syscall.Kill(child, syscall.SIGKILL)
		<-done
		t.Fatal("ps snapshot blocked on inherited stdout beyond its deadline")
	}
}
