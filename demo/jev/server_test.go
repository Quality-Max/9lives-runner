package main

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Quality-Max/9lives-runner/internal/jev"
)

func demoRequest(s *demoServer, method, path, body string) *http.Request {
	r := httptest.NewRequest(method, s.origin+path, strings.NewReader(body))
	r.Header.Set("Origin", s.origin)
	r.Header.Set("X-Demo-Session", s.session)
	r.Header.Set("Content-Type", "application/json")
	return r
}

func testDemoServer(t *testing.T, live bool, execute demoExecutor) *demoServer {
	t.Helper()
	s, err := newDemoServer(jev.Config{Enabled: live}, "http://127.0.0.1:4175", execute)
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func TestInteractiveDemoRequiresExplicitOwnedRequest(t *testing.T) {
	calls := 0
	s := testDemoServer(t, false, func(context.Context, jev.Config, string) (string, report, error) { calls++; return "", report{}, nil })
	for _, path := range []string{"/", "/api/config", "/shop?scenario=success"} {
		w := httptest.NewRecorder()
		s.ServeHTTP(w, demoRequest(s, "GET", path, ""))
		if w.Code != 200 || strings.Contains(w.Body.String(), "__DEMO_SESSION__") {
			t.Fatalf("page unavailable: %s", path)
		}
	}
	for _, mutate := range []func(*http.Request){
		func(r *http.Request) { r.Host = "foreign.example" },
		func(r *http.Request) { r.Header.Set("Origin", "https://foreign.example") },
		func(r *http.Request) { r.Header.Del("Origin") },
		func(r *http.Request) { r.Header.Del("X-Demo-Session") },
		func(r *http.Request) { r.Header.Set("Content-Type", "text/plain") },
	} {
		r := demoRequest(s, "POST", "/api/run", `{"scenario":"declined","live":false}`)
		mutate(r)
		w := httptest.NewRecorder()
		s.ServeHTTP(w, r)
		if w.Code != 403 {
			t.Fatal("foreign or unowned request admitted")
		}
	}
	for _, raw := range []string{
		`{"scenario":"success","live":true}`, // Live mode was not enabled at startup.
		`{"scenario":"success","Live":true}`, `{"scenario":"success","live":false,"live":true}`,
		`{"scenario":"success","live":null}`, `{"scenario":"success"}`, `{"scenario":"other","live":false}`,
		`{"scenario":"success","live":"true"}`, `{"scenario":"success","live":false} {}`,
		`{"scenario":"success","live":false,"prompt":"arbitrary"}`, strings.Repeat(" ", 257),
	} {
		w := httptest.NewRecorder()
		s.ServeHTTP(w, demoRequest(s, "POST", "/api/run", raw))
		if w.Code < 400 {
			t.Fatal("invalid or disabled request admitted")
		}
	}
	if calls != 0 {
		t.Fatal("opening pages or rejected requests invoked executor")
	}
}

func TestInteractiveDemoProjectsSafeEvidenceAndBoundsCalls(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "record.json"), []byte(`{"status":"disabled"}`), 0600); err != nil {
		t.Fatal(err)
	}
	var modes []bool
	s := testDemoServer(t, true, func(_ context.Context, c jev.Config, scenario string) (string, report, error) {
		modes = append(modes, c.Enabled)
		return dir, report{Capture: capture{AssertionPassed: scenario == "success", Snapshot: "raw-observation-sentinel"}, Record: jev.Record{Status: "disabled"}}, nil
	})
	for i := 0; i < maxDemoRuns; i++ {
		live := i < maxLiveRuns
		body := `{"scenario":"success","live":false}`
		if live {
			body = `{"scenario":"success","live":true}`
		}
		w := httptest.NewRecorder()
		s.ServeHTTP(w, demoRequest(s, "POST", "/api/run", body))
		if w.Code != 200 || !strings.Contains(w.Body.String(), `"passed":true`) || strings.Contains(w.Body.String(), "raw-observation-sentinel") {
			t.Fatal("safe executed result missing")
		}
		if i == maxLiveRuns-1 {
			w = httptest.NewRecorder()
			s.ServeHTTP(w, demoRequest(s, "POST", "/api/run", body))
			if w.Code != 429 {
				t.Fatal("live request budget exceeded")
			}
		}
	}
	w := httptest.NewRecorder()
	s.ServeHTTP(w, demoRequest(s, "POST", "/api/run", `{"scenario":"success","live":false}`))
	if w.Code != 429 || len(modes) != maxDemoRuns {
		t.Fatal("session budget exceeded")
	}
	for i, live := range modes {
		if live != (i < maxLiveRuns) {
			t.Fatal("offline click inherited live startup mode")
		}
	}
	if err := os.WriteFile(filepath.Join(dir, "index.html"), []byte("<h1>Saved report</h1>"), 0600); err != nil {
		t.Fatal(err)
	}
	for path, want := range map[string]int{
		"/runs/" + filepath.Base(dir) + "/index.html": 200, "/runs/" + filepath.Base(dir) + "/record.json": 200, "/runs/" + filepath.Base(dir) + "/private.txt": 404, "/.env": 404} {
		w = httptest.NewRecorder()
		s.ServeHTTP(w, demoRequest(s, "GET", path, ""))
		if w.Code != want {
			t.Fatal("artifact allowlist not enforced")
		}
	}
}

func TestInteractiveDemoCancellationAndConcurrentRequest(t *testing.T) {
	entered := make(chan struct{})
	stopped := make(chan struct{})
	var calls atomic.Int32
	s := testDemoServer(t, true, func(ctx context.Context, _ jev.Config, _ string) (string, report, error) {
		calls.Add(1)
		close(entered)
		<-ctx.Done()
		close(stopped)
		return "", report{}, ctx.Err()
	})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan struct{})
	go func() {
		defer close(done)
		s.ServeHTTP(httptest.NewRecorder(), demoRequest(s, "POST", "/api/run", `{"scenario":"declined","live":true}`).WithContext(ctx))
	}()
	select {
	case <-entered:
	case <-time.After(time.Second):
		t.Fatal("check did not start")
	}
	w := httptest.NewRecorder()
	s.ServeHTTP(w, demoRequest(s, "POST", "/api/run", `{"scenario":"declined","live":true}`))
	if w.Code != 409 || calls.Load() != 1 {
		t.Fatal("concurrent provider work admitted")
	}
	cancel()
	select {
	case <-stopped:
	case <-time.After(time.Second):
		t.Fatal("browser request cancellation did not reach executor")
	}
	<-done
}

func TestInteractiveDemoProviderFailureKeepsBrowserResult(t *testing.T) {
	s := testDemoServer(t, true, func(context.Context, jev.Config, string) (string, report, error) {
		return t.TempDir(), report{Capture: capture{AssertionPassed: true}, Record: jev.Record{Status: "provider_error"}}, errors.New("raw-provider-error-sentinel")
	})
	w := httptest.NewRecorder()
	s.ServeHTTP(w, demoRequest(s, "POST", "/api/run", `{"scenario":"success","live":true}`))
	raw, _ := io.ReadAll(w.Result().Body)
	if w.Code != 200 || !strings.Contains(string(raw), `"passed":true`) || !strings.Contains(string(raw), "Jev could not answer") || strings.Contains(string(raw), "raw-provider-error-sentinel") {
		t.Fatal("provider failure hid browser result or leaked raw error")
	}
}
