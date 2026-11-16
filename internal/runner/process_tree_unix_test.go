//go:build unix

package runner

import (
	"context"
	"os"
	"os/exec"
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

// A successful command must survive a single failed observation. The next
// snapshot still has to prove that owned children have stopped.
func TestUnixOwnershipToleratesTransientSnapshotFailure(t *testing.T) {
	ps, err := exec.LookPath("ps")
	if err != nil {
		t.Fatal(err)
	}
	for _, failAt := range []string{"1", "2"} {
		t.Run("snapshot-"+failAt, func(t *testing.T) {
			dir := t.TempDir()
			wrapper := `#!/bin/sh
count=0
[ ! -f "$NINELIVES_PS_COUNT" ] || read -r count < "$NINELIVES_PS_COUNT"
count=$((count + 1))
printf '%s\n' "$count" > "$NINELIVES_PS_COUNT"
[ "$count" != "$NINELIVES_PS_FAIL_AT" ] || exit 1
exec "$NINELIVES_REAL_PS" "$@"
`
			if err := os.WriteFile(filepath.Join(dir, "ps"), []byte(wrapper), 0700); err != nil {
				t.Fatal(err)
			}
			t.Setenv("PATH", dir)
			t.Setenv("NINELIVES_PS_COUNT", filepath.Join(dir, "count"))
			t.Setenv("NINELIVES_PS_FAIL_AT", failAt)
			t.Setenv("NINELIVES_REAL_PS", ps)
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			if err := RunOwnedCommand(ctx, exec.Command("/bin/sleep", "1.2")); err != nil {
				t.Fatalf("one failed snapshot rejected a successful command: %v", err)
			}
			data, err := os.ReadFile(filepath.Join(dir, "count"))
			count, _ := strconv.Atoi(strings.TrimSpace(string(data)))
			if err != nil || count < 3 {
				t.Fatal("did not exercise a failed snapshot followed by recovery")
			}
		})
	}
}

func TestUnixOwnershipRequiresFinalSnapshot(t *testing.T) {
	ps, err := exec.LookPath("ps")
	if err != nil {
		t.Fatal(err)
	}
	for _, mode := range []string{"recover", "unavailable"} {
		t.Run(mode, func(t *testing.T) {
			dir := t.TempDir()
			wrapper := `#!/bin/sh
if [ -f "$NINELIVES_FINISHED" ]; then
  if [ "$NINELIVES_PS_MODE" = unavailable ] || [ ! -f "$NINELIVES_PS_FAILED" ]; then
    : > "$NINELIVES_PS_FAILED"
    exit 1
  fi
fi
: > "$NINELIVES_PS_READY"
exec "$NINELIVES_REAL_PS" "$@"
`
			if err := os.WriteFile(filepath.Join(dir, "ps"), []byte(wrapper), 0700); err != nil {
				t.Fatal(err)
			}
			t.Setenv("PATH", dir)
			t.Setenv("NINELIVES_REAL_PS", ps)
			t.Setenv("NINELIVES_PS_MODE", mode)
			t.Setenv("NINELIVES_PS_READY", filepath.Join(dir, "ready"))
			t.Setenv("NINELIVES_PS_FAILED", filepath.Join(dir, "failed"))
			t.Setenv("NINELIVES_FINISHED", filepath.Join(dir, "finished"))
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			command := exec.Command("/bin/sh", "-c", `while [ ! -f "$NINELIVES_PS_READY" ]; do /bin/sleep 0.01; done; /bin/sleep 0.1; : > "$NINELIVES_FINISHED"`)
			err := RunOwnedCommand(ctx, command)
			if mode == "recover" && err != nil {
				t.Fatalf("final snapshot did not recover from one failure: %v", err)
			}
			if mode == "unavailable" && (err == nil || !strings.Contains(err.Error(), "inspect owned process ancestry")) {
				t.Fatalf("successful exit must not hide failed final inspection: %v", err)
			}
			if _, err := os.Stat(filepath.Join(dir, "failed")); err != nil {
				t.Fatal("did not exercise final snapshot failure")
			}
		})
	}
}
