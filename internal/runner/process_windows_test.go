//go:build windows

package runner

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"golang.org/x/sys/windows"
)

func windowsHelper(mode, directory string) *exec.Cmd {
	cmd := exec.Command(os.Args[0], "-test.run=^TestWindowsProcessHelper$", "--", mode, directory)
	cmd.Env = append(os.Environ(), "NINELIVES_WINDOWS_HELPER=1")
	return cmd
}

func TestWindowsProcessHelper(t *testing.T) {
	if os.Getenv("NINELIVES_WINDOWS_HELPER") != "1" {
		return
	}
	mode, dir := os.Args[len(os.Args)-2], os.Args[len(os.Args)-1]
	if err := os.WriteFile(filepath.Join(dir, mode), []byte(strconv.Itoa(os.Getpid())), 0600); err != nil {
		os.Exit(7)
	}
	switch mode {
	case "leader", "exit-leader":
		if err := windowsHelper("child", dir).Start(); err != nil {
			os.Exit(8)
		}
	case "child":
		if err := windowsHelper("grandchild", dir).Start(); err != nil {
			os.Exit(9)
		}
	}
	if mode == "exit-leader" {
		deadline := time.Now().Add(10 * time.Second)
		for {
			if _, err := os.Stat(filepath.Join(dir, "grandchild")); err == nil {
				os.Exit(0)
			}
			if time.Now().After(deadline) {
				os.Exit(10)
			}
			time.Sleep(10 * time.Millisecond)
		}
	}
	for {
		time.Sleep(time.Second)
	}
}

func markerPID(t *testing.T, directory, name string) uint32 {
	t.Helper()
	deadline := time.Now().Add(15 * time.Second)
	for time.Now().Before(deadline) {
		if data, err := os.ReadFile(filepath.Join(directory, name)); err == nil {
			pid, err := strconv.ParseUint(string(data), 10, 32)
			if err == nil {
				return uint32(pid)
			}
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("owned process %s never became ready", name)
	return 0
}

func windowsProcessAlive(pid uint32) bool {
	handle, err := windows.OpenProcess(windows.PROCESS_QUERY_LIMITED_INFORMATION, false, pid)
	if err != nil {
		return false
	}
	defer windows.CloseHandle(handle)
	var code uint32
	return windows.GetExitCodeProcess(handle, &code) == nil && code == 259 // STILL_ACTIVE
}

func TestWindowsOwnedProcessTree(t *testing.T) {
	for _, mode := range []string{"cancel", "timeout", "normal-exit"} {
		t.Run(mode, func(t *testing.T) {
			dir := t.TempDir()
			unrelated := windowsHelper("sentinel", dir)
			if err := unrelated.Start(); err != nil {
				t.Fatal(err)
			}
			defer func() { _ = unrelated.Process.Kill(); _ = unrelated.Wait() }()
			sentinel := markerPID(t, dir, "sentinel")
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			if mode == "timeout" {
				var deadlineCancel context.CancelFunc
				ctx, deadlineCancel = context.WithTimeout(ctx, 8*time.Second)
				defer deadlineCancel()
			}
			leader := "leader"
			if mode == "normal-exit" {
				leader = "exit-leader"
			}
			type result struct {
				err         error
				termination *Termination
			}
			done := make(chan result, 1)
			go func() {
				err, termination := startAndWait(ctx, windowsHelper(leader, dir))
				done <- result{err, termination}
			}()
			pids := []uint32{markerPID(t, dir, leader), markerPID(t, dir, "child"), markerPID(t, dir, "grandchild")}
			if mode == "cancel" {
				cancel()
			}
			select {
			case result := <-done:
				kind := map[string]string{"cancel": "canceled", "timeout": "timeout", "normal-exit": "cleanup"}[mode]
				if result.termination == nil || result.termination.Kind != kind || result.termination.Signal != "TerminateJobObject" {
					t.Fatalf("missing tree termination for %s: %+v", mode, result.termination)
				}
				if mode == "normal-exit" && result.err != nil {
					t.Fatalf("normal leader exit failed: %v", result.err)
				}
				if mode != "normal-exit" && result.err == nil {
					t.Fatal("interrupted command passed")
				}
			case <-time.After(20 * time.Second):
				t.Fatal("owned command did not finish")
			}
			for _, pid := range pids {
				if windowsProcessAlive(pid) {
					t.Error(fmt.Sprintf("owned process %d survived", pid))
				}
			}
			if !windowsProcessAlive(sentinel) {
				t.Fatal("unrelated process was terminated")
			}
		})
	}
}
