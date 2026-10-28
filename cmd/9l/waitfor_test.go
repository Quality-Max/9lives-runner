package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"log"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"syscall"
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

// closedPortReason reports whether reason is what a wait on a closed port
// ends with: "connection refused", or "timed out" when no dial completed
// within the one-second wait. Windows retries a refused connection for about
// two seconds before reporting it, and a loaded CI runner can delay the dial
// past the wait on any platform. Either way the reason is a fixed phrase;
// TestNetReasonNamesErrnoCodes checks the refused mapping itself.
func closedPortReason(reason, prefix string) bool {
	return reason == prefix+"connection refused" || reason == prefix+"timed out"
}

func waitFor(ctx context.Context, t *testing.T, targets []string, timeout time.Duration) error {
	t.Helper()
	probes, err := parseWaitTargets(targets)
	if err != nil {
		t.Fatalf("targets rejected: %v", err)
	}
	return waitForTargets(ctx, probes, timeout)
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
	// An upper-case scheme is still an http(s) target.
	upper := "HTTP://" + strings.TrimPrefix(server.URL, "http://")
	if err := waitFor(context.Background(), t, []string{upper + "/health?token=fixture-secret", listener.Addr().String()}, 10*time.Second); err != nil {
		t.Fatalf("ready targets reported not ready: %v", err)
	}
	if calls.Load() < 3 {
		t.Fatalf("server errors were not retried: %d call(s)", calls.Load())
	}
}

// A redirect is the target's own answer, so it counts as ready and is not
// followed.
func TestWaitForTargetsDoesNotFollowRedirects(t *testing.T) {
	var followed atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/login" {
			followed.Add(1)
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		http.Redirect(w, r, "/login", http.StatusFound)
	}))
	defer server.Close()
	if err := waitFor(context.Background(), t, []string{server.URL + "/"}, 2*time.Second); err != nil || followed.Load() != 0 {
		t.Fatalf("redirecting target: err=%v followed=%d", err, followed.Load())
	}
}

// The display name is scheme://host:port only: a path, matrix parameters,
// query or fragment can hold a token.
func TestWaitForTargetsNamesTheTargetAndReasonWithoutSecrets(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusBadGateway) }))
	defer server.Close()
	started := time.Now()
	err := waitFor(context.Background(), t, []string{server.URL + "/hooks/fixture-path;jsessionid=fixture-session?token=fixture-secret#fixture-fragment"}, time.Second)
	var notReady waitNotReadyError
	if !errors.As(err, &notReady) || err.Error() != "--wait-for target not ready (environment): "+server.URL+" was not ready after 1s: HTTP 502" {
		t.Fatalf("http: %v", err)
	}
	// "after 1s" is reported only once the wait deadline has passed.
	if elapsed := time.Since(started); elapsed < time.Second {
		t.Fatalf("reported not ready after 1s when %s had passed", elapsed)
	}
	closedTarget := closedPort(t)
	err = waitFor(context.Background(), t, []string{closedTarget}, time.Second)
	if err == nil || !closedPortReason(err.Error(), "--wait-for target not ready (environment): "+closedTarget+" was not ready after 1s: dial: ") {
		t.Fatalf("tcp: %v", err)
	}
	for _, good := range []string{"[::1]:8080", "http://[::1]:8080/", "localhost:08080", "Https://example.test", "http://[fe80::1%25en0]:8080/", "[fe80::1%en0]:8080", "[::ffff:127.0.0.1]:80", "http://127.0.0.1:8080"} {
		if _, err := parseWaitTargets([]string{good}); err != nil {
			t.Fatalf("%q rejected: %v", good, err)
		}
	}
	for _, bad := range []string{"localhost", "ftp://x", "http://", "http://:8080", "http://localhost:65536", "http://localhost:", "127.0.0.1:0", "127.0.0.1:+1", "127.0.0.1:-1", "127.0.0.1: 80", "127.0.0.1:080800", "fixture-user@127.0.0.1:1", "a/b:80",
		// A colon in a host must belong to a bracketed IPv6 literal.
		"http://localhost:80:90", "https://localhost:80:90/", "[localhost:80]:90", "[1.2.3.4]:80", "http://::1:8080", "http://[::1%25]:80", "http://[fe80::1%25a/b]:80", "[fe80::1%a b]:80", "local%host:80"} {
		_, err := parseWaitTargets([]string{bad})
		if err == nil || strings.Contains(err.Error(), "fixture-user") {
			t.Fatalf("%q accepted or echoed: %v", bad, err)
		}
	}
}

// A dev server with a self-signed certificate, or https against plain http,
// gets a fixed TLS reason rather than "request failed".
func TestWaitForTargetsNamesTLSFailures(t *testing.T) {
	secure := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	secure.Config.ErrorLog = log.New(io.Discard, "", 0)
	secure.StartTLS()
	defer secure.Close()
	if err := waitFor(context.Background(), t, []string{secure.URL}, time.Second); err == nil || !strings.HasSuffix(err.Error(), ": certificate not trusted") {
		t.Fatalf("self-signed: %v", err)
	}
	plain := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	defer plain.Close()
	if err := waitFor(context.Background(), t, []string{"https://" + plain.Listener.Addr().String()}, time.Second); err == nil || !strings.HasSuffix(err.Error(), ": tls handshake failed") {
		t.Fatalf("https to http: %v", err)
	}
}

// Cancellation is returned as such, not as a target that was not ready.
func TestWaitForTargetsReturnsCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	time.AfterFunc(300*time.Millisecond, cancel)
	started := time.Now()
	err := waitFor(ctx, t, []string{closedPort(t)}, 30*time.Second)
	var notReady waitNotReadyError
	if !errors.Is(err, context.Canceled) || errors.As(err, &notReady) || time.Since(started) > 5*time.Second {
		t.Fatalf("err=%v after %s", err, time.Since(started))
	}
}

func TestNetReasonNamesErrnoCodes(t *testing.T) {
	for errno, want := range map[syscall.Errno]string{syscall.ECONNREFUSED: "connection refused", syscall.ECONNRESET: "connection reset", syscall.EHOSTUNREACH: "host unreachable", syscall.ENETUNREACH: "network unreachable"} {
		if got := netReason(os.NewSyscallError("read", errno)); got != want {
			t.Fatalf("%v: %q, want %q", errno, got, want)
		}
	}
	for _, dns := range []struct {
		err  *net.DNSError
		want string
	}{
		{&net.DNSError{Err: "no such host", Name: "fixture-name.invalid", IsNotFound: true}, "no such host"},
		{&net.DNSError{Err: "i/o timeout", Name: "fixture-name.invalid", IsTimeout: true}, "dns lookup timed out"},
		{&net.DNSError{Err: "server misbehaving", Name: "fixture-name.invalid", IsTemporary: true}, "dns lookup failed"},
	} {
		if got := netReason(&net.OpError{Op: "dial", Net: "tcp", Err: dns.err}); got != dns.want {
			t.Fatalf("%v: %q, want %q", dns.err, got, dns.want)
		}
	}
	if got := netReason(errors.New("fixture-secret text")); got != "unreachable" {
		t.Fatalf("unknown error: %q", got)
	}
}

// A target that never comes up leaves a stored, incomplete run before any
// test process starts: exit 3, an error receipt per job naming the target,
// and `9l result` reads it back.
func TestRunWaitForLeavesAStoredIncompleteRunWithoutStarting(t *testing.T) {
	project := proveProject(t)
	receipts := t.TempDir()
	target := closedPort(t)
	var stdout, stderr bytes.Buffer
	code := run([]string{"run", "--wait-for", "http://" + target + "/private?token=fixture-secret", "--wait-timeout", "1s", "--format", "json", "--receipt-dir", receipts, filepath.Join(project, "tests/shop.spec.ts")}, &stdout, &stderr)
	reasonPrefix := "not started: --wait-for target not ready (environment): http://" + target + " was not ready after 1s: dial: "
	if code != exitIncomplete || !strings.Contains(stderr.String(), "target not ready (environment)") || strings.Contains(stderr.String()+stdout.String(), "fixture-secret") {
		t.Fatalf("code=%d stderr=%s", code, stderr.String())
	}
	var result runner.RunSummary
	if err := json.Unmarshal(stdout.Bytes(), &result); err != nil || result.Outcome != runner.OutcomeIncomplete || result.Complete || result.PlannedJobs != 1 || result.Errors != 1 || len(result.Receipts) != 1 || !closedPortReason(result.Receipts[0].Error, reasonPrefix) {
		t.Fatalf("result=%+v err=%v", result, err)
	}
	stored, err := runner.LoadResult(receipts, result.RunID)
	if err != nil || stored.Outcome != runner.OutcomeIncomplete || len(stored.Receipts) != 1 || stored.Receipts[0].Error != result.Receipts[0].Error {
		t.Fatalf("stored=%+v err=%v", stored, err)
	}
	stdout.Reset()
	stderr.Reset()
	if code := run([]string{"result", result.RunID, "--receipt-dir", receipts}, &stdout, &stderr); code != 0 || !strings.Contains(stdout.String(), `"outcome":"incomplete"`) {
		t.Fatalf("result: code=%d stdout=%s stderr=%s", code, stdout.String(), stderr.String())
	}
}

// The run deadline bounds the wait, and a run that hits it is reported as
// timed out, not as a target that was not ready.
func TestRunDeadlineBoundsWaitFor(t *testing.T) {
	project := proveProject(t)
	var stdout, stderr bytes.Buffer
	started := time.Now()
	code := run([]string{"run", "--deadline", "1s", "--wait-for", closedPort(t), "--wait-timeout", "30s", "--format", "json", "--receipt-dir", t.TempDir(), filepath.Join(project, "tests/shop.spec.ts")}, &stdout, &stderr)
	var result runner.RunSummary
	if err := json.Unmarshal(stdout.Bytes(), &result); err != nil || code != exitIncomplete || result.TimedOut != 1 || time.Since(started) > 15*time.Second || strings.Contains(stderr.String(), "not ready") {
		t.Fatalf("code=%d after %s result=%+v stderr=%s", code, time.Since(started), result, stderr.String())
	}
}

func TestRunWaitForUsageErrors(t *testing.T) {
	project := proveProject(t)
	spec := filepath.Join(project, "tests/shop.spec.ts")
	for name, args := range map[string][]string{
		"malformed":         {"run", "--wait-for", "nonsense", spec},
		"short timeout":     {"run", "--wait-for", "127.0.0.1:9", "--wait-timeout", "500ms", spec},
		"dry run":           {"run", "--dry-run", "--wait-for", "nonsense", spec},
		"plan":              {"plan", "--wait-for", "127.0.0.1:+1", spec},
		"user info in tcp":  {"run", "--wait-for", "fixture-user@127.0.0.1:1", spec},
		"rejected by Set()": {"run", "--wait-for", "http://user:fixture-secret@localhost:3000/" + strings.Repeat("x", 600), spec},
	} {
		var stdout, stderr bytes.Buffer
		receipts := t.TempDir()
		code := run(append(args, "--receipt-dir", receipts), &stdout, &stderr)
		output := stdout.String() + stderr.String()
		if code != exitUsage || strings.Contains(output, "fixture-") || !strings.Contains(stderr.String(), "--wait-") {
			t.Fatalf("%s: code=%d output=%s", name, code, output)
		}
		if entries, _ := os.ReadDir(receipts); len(entries) != 0 {
			t.Fatalf("%s: a run was started", name)
		}
	}
}
