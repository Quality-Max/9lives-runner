package main

import (
	"bytes"
	"context"
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Quality-Max/9lives-runner/internal/runner"
)

// closedPort is an address nothing listens on.
func closedPort(t *testing.T) string {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	address := listener.Addr().String()
	listener.Close()
	return address
}

func TestWaitForTargetsWaitsForServerErrorsToClearAndForPortsToOpen(t *testing.T) {
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if calls.Add(1) < 3 {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	if err := waitForTargets(context.Background(), []string{server.URL + "/health?token=fixture-secret", listener.Addr().String()}, 10*time.Second); err != nil {
		t.Fatalf("ready targets reported not ready: %v", err)
	}
	if calls.Load() < 3 {
		t.Fatalf("server errors were not retried: %d call(s)", calls.Load())
	}
}

func TestWaitForTargetsNamesTheTargetAndReasonWithoutSecrets(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusBadGateway) }))
	defer server.Close()
	err := waitForTargets(context.Background(), []string{server.URL + "/api?token=fixture-secret"}, time.Second)
	if err == nil || !strings.Contains(err.Error(), server.URL+"/api was not ready after 1s: HTTP 502") || strings.Contains(err.Error(), "fixture-secret") {
		t.Fatalf("http: %v", err)
	}
	err = waitForTargets(context.Background(), []string{closedPort(t)}, time.Second)
	if err == nil || !strings.Contains(err.Error(), "was not ready after 1s: dial: ") {
		t.Fatalf("tcp: %v", err)
	}
	for _, bad := range []string{"localhost", "ftp://x", "http://"} {
		var usage waitUsageError
		if err := waitForTargets(context.Background(), []string{bad}, time.Second); err == nil || !errorsAs(err, &usage) {
			t.Fatalf("%q accepted as a target: %v", bad, err)
		}
	}
}

// A target that never comes up leaves the run incomplete before any test
// process starts: exit 3, an environment reason, no receipts.
func TestRunWaitForLeavesTheRunIncompleteWithoutStarting(t *testing.T) {
	project := proveProject(t)
	receipts := t.TempDir()
	var stdout, stderr bytes.Buffer
	code := run([]string{"run", "--wait-for", "http://" + closedPort(t) + "/", "--wait-timeout", "1s", "--format", "json", "--receipt-dir", receipts, filepath.Join(project, "tests/shop.spec.ts")}, &stdout, &stderr)
	if code != exitIncomplete || !strings.Contains(stderr.String(), "target not ready (environment)") || !strings.Contains(stderr.String(), "nothing ran") {
		t.Fatalf("code=%d stderr=%s", code, stderr.String())
	}
	var result runner.RunSummary
	if err := json.Unmarshal(stdout.Bytes(), &result); err != nil || result.Outcome != runner.OutcomeIncomplete || result.Complete || result.PlannedJobs != 1 || len(result.Receipts) != 0 {
		t.Fatalf("result=%+v err=%v", result, err)
	}
	if entries, _ := os.ReadDir(receipts); len(entries) != 0 {
		t.Fatalf("a run was started: %v", entries)
	}
	stdout.Reset()
	stderr.Reset()
	if code := run([]string{"run", "--wait-for", "nonsense", "--receipt-dir", receipts, filepath.Join(project, "tests/shop.spec.ts")}, &stdout, &stderr); code != exitUsage || !strings.Contains(stderr.String(), "--wait-for: a target is an http(s) URL or host:port") {
		t.Fatalf("usage: code=%d stderr=%s", code, stderr.String())
	}
}

func errorsAs(err error, target *waitUsageError) bool {
	for err != nil {
		if usage, ok := err.(waitUsageError); ok {
			*target = usage
			return true
		}
		unwrapper, ok := err.(interface{ Unwrap() error })
		if !ok {
			return false
		}
		err = unwrapper.Unwrap()
	}
	return false
}
