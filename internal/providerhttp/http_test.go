package providerhttp

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func request(t *testing.T, method, url string) *http.Request {
	t.Helper()
	req, err := http.NewRequestWithContext(context.Background(), method, url, strings.NewReader("{}"))
	if err != nil {
		t.Fatal(err)
	}
	return req
}

func TestDoReturnsBodyOnSuccess(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"ok":true}`)
	}))
	t.Cleanup(server.Close)
	raw, err := Do(request(t, http.MethodPost, server.URL), time.Second, 1<<20)
	if err != nil || string(raw) != `{"ok":true}` {
		t.Fatal("successful response not returned verbatim", err)
	}
}

func TestDoExposesStatusOnlyNeverTheBody(t *testing.T) {
	for _, code := range []int{400, 401, 429, 500, 503} {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(code)
			_, _ = io.WriteString(w, "secret-provider-diagnostic "+strings.Repeat("x", 512))
		}))
		t.Cleanup(server.Close)
		_, err := Do(request(t, http.MethodPost, server.URL), time.Second, 1<<20)
		var status *StatusError
		if !errors.As(err, &status) || status.Code != code {
			t.Fatal("HTTP failure did not expose its status code")
		}
		if err.Error() != "provider returned HTTP "+strconv.Itoa(code) || strings.Contains(err.Error(), "secret-provider-diagnostic") {
			t.Fatal("HTTP failure leaked its body")
		}
	}
}

func TestDoDoesNotFollowRedirects(t *testing.T) {
	var followed atomic.Int32
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		followed.Add(1)
		_, _ = io.WriteString(w, `{"ok":true}`)
	}))
	t.Cleanup(target.Close)
	source := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Location", target.URL)
		w.WriteHeader(http.StatusFound)
	}))
	t.Cleanup(source.Close)
	_, err := Do(request(t, http.MethodPost, source.URL), time.Second, 1<<20)
	var status *StatusError
	if !errors.As(err, &status) || status.Code != http.StatusFound {
		t.Fatal("redirect was followed instead of reported")
	}
	if followed.Load() != 0 {
		t.Fatal("redirect target was contacted")
	}
}

func TestDoRejectsResponsesBeyondTheByteLimit(t *testing.T) {
	const limit = 16
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, strings.Repeat("a", limit))
	}))
	t.Cleanup(server.Close)
	if raw, err := Do(request(t, http.MethodPost, server.URL), time.Second, limit); err != nil || len(raw) != limit {
		t.Fatal("response at the limit was rejected", err)
	}
	server.Config.Handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, strings.Repeat("a", limit+1))
	})
	if _, err := Do(request(t, http.MethodPost, server.URL), time.Second, limit); err == nil || err.Error() != "provider response is too large" {
		t.Fatal("response beyond the limit was accepted")
	}
}

func TestDoReturnsTheContextErrorWhenCancelled(t *testing.T) {
	release := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		<-release
	}))
	t.Cleanup(func() { close(release) })
	t.Cleanup(server.Close)
	ctx, cancel := context.WithCancel(context.Background())
	req := request(t, http.MethodPost, server.URL).WithContext(ctx)
	started := make(chan struct{})
	done := make(chan error, 1)
	go func() {
		close(started)
		_, err := Do(req, 30*time.Second, 1<<20)
		done <- err
	}()
	<-started
	cancel()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatal("cancelled transport did not return the context error", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("cancelled transport did not stop")
	}
}
