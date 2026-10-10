package tier2

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
	"unicode/utf8"
)

// CLI success tests verify transport behavior, not startup latency. Allow
// subprocess scheduling headroom under concurrent race-enabled package tests.
const cliTestTimeout = 10 * time.Second

func TestHTTPProviderUsesBoundedLocalTransport(t *testing.T) {
	t.Setenv("OPENAI_API_KEY", "test-key")
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "POST" {
			t.Error("method")
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte("{\"choices\":[{\"message\":{\"content\":\"```typescript\\nx\\n```\"}}]}"))
	}))
	defer server.Close()
	p := HTTPProvider{name: "openai", baseURL: server.URL, timeout: time.Second}
	got, err := p.Complete(context.Background(), "secret prompt", "model")
	if err != nil || !strings.Contains(got, "typescript") {
		t.Fatalf("got=%q err=%v", got, err)
	}
}

func TestHTTPProviderParsesAnthropicTextBlocks(t *testing.T) {
	t.Setenv("ANTHROPIC_API_KEY", "test-key")
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("anthropic-version"); got != "2023-06-01" {
			t.Errorf("anthropic-version=%q", got)
		}
		_, _ = w.Write([]byte("{\"content\":[{\"type\":\"text\",\"text\":\"```typescript\\nx\\n```\"}]}"))
	}))
	defer server.Close()
	got, err := HTTPProvider{name: "anthropic", baseURL: server.URL, timeout: time.Second}.Complete(context.Background(), "prompt", "model")
	if err != nil || !strings.Contains(got, "typescript") {
		t.Fatalf("got=%q err=%v", got, err)
	}
}

func TestCLIProviderPromptIsStdinAndErrorsAreSanitized(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX shell fixture; Windows process ownership is tested separately")
	}
	dir := t.TempDir()
	binary := filepath.Join(dir, "codex")
	script := "#!/bin/sh\ncat\necho ignored >&2\n"
	if err := os.WriteFile(binary, []byte(script), 0700); err != nil {
		t.Fatal(err)
	}
	old := os.Getenv("PATH")
	t.Setenv("PATH", dir+string(os.PathListSeparator)+old)
	got, err := (CLIProvider{name: "codex", timeout: cliTestTimeout}).Complete(context.Background(), "prompt-in-stdin", "m")
	if err != nil || got != "prompt-in-stdin" {
		t.Fatalf("got=%q err=%v", got, err)
	}
}

func TestResolvePreservesClaudeCodeAliasAndHTTPFallback(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX shell fixture; Windows process ownership is tested separately")
	}
	// Isolate provider choice from any developer credentials inherited by go test.
	t.Setenv("ANTHROPIC_API_KEY", "")
	t.Setenv("NINELIVES_PROVIDER", "")
	t.Setenv("NINELIVES_MODEL", "")
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "claude"), []byte("#!/bin/sh\nexit 1\n"), 0700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("OPENAI_API_KEY", "test-key")
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"ok"}}]}`))
	}))
	defer server.Close()
	provider, err := Resolve(Options{Name: "claude-code", BaseURL: server.URL, Timeout: cliTestTimeout})
	if err != nil || provider.Name() != "claude" {
		t.Fatalf("provider=%v err=%v", provider, err)
	}
	if got, err := provider.Complete(context.Background(), "p", ""); err != nil || got != "ok" {
		t.Fatalf("fallback got=%q err=%v", got, err)
	}
}

func TestHTTPProviderUsesPinnedDefaultModel(t *testing.T) {
	t.Setenv("OPENAI_API_KEY", "test-key")
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		if body["model"] != defaultOpenAIModel {
			t.Errorf("model=%v", body["model"])
		}
		_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"ok"}}]}`))
	}))
	defer server.Close()
	if got, err := httpProvider("openai", server.URL, time.Second).Complete(context.Background(), "p", ""); err != nil || got != "ok" {
		t.Fatalf("got=%q err=%v", got, err)
	}
}

func TestCLIProviderForwardsExplicitModel(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX shell fixture; Windows process ownership is tested separately")
	}
	dir := t.TempDir()
	binary := filepath.Join(dir, "codex")
	script := "#!/bin/sh\n[ \"$1\" = exec ] && [ \"$2\" = --skip-git-repo-check ] && [ \"$3\" = --model ] && [ \"$4\" = chosen ] && [ \"$5\" = - ] || exit 7\ncat\n"
	if err := os.WriteFile(binary, []byte(script), 0700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	if got, err := (CLIProvider{name: "codex", timeout: cliTestTimeout}).Complete(context.Background(), "prompt", "chosen"); err != nil || got != "prompt" {
		t.Fatalf("got=%q err=%v", got, err)
	}
}

type errorProvider struct {
	err   error
	calls int
}

func (p *errorProvider) Name() string { return "error" }
func (p *errorProvider) Complete(context.Context, string, string) (string, error) {
	p.calls++
	return "", p.err
}

func TestFallbackDoesNotCrossCancellationOrDeadline(t *testing.T) {
	for _, err := range []error{context.Canceled, context.DeadlineExceeded} {
		primary, fallback := &errorProvider{err: err}, &errorProvider{}
		_, got := (fallbackProvider{primary: primary, fallback: fallback}).Complete(context.Background(), "p", "m")
		if !errors.Is(got, err) || fallback.calls != 0 {
			t.Fatalf("err=%v fallback=%d", got, fallback.calls)
		}
	}
}

func TestHTTPProviderRejectsRedirectStatusMalformedAndOversizedResponses(t *testing.T) {
	t.Setenv("OPENAI_API_KEY", "test-key")
	for name, handler := range map[string]http.HandlerFunc{
		"redirect":  func(w http.ResponseWriter, r *http.Request) { http.Redirect(w, r, "/next", http.StatusFound) },
		"status":    func(w http.ResponseWriter, r *http.Request) { http.Error(w, "untrusted body", http.StatusBadGateway) },
		"malformed": func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write([]byte("{")) },
		"oversized": func(w http.ResponseWriter, r *http.Request) {
			_, _ = w.Write([]byte(strings.Repeat("x", maxResponseBytes+1)))
		},
	} {
		t.Run(name, func(t *testing.T) {
			server := httptest.NewServer(handler)
			defer server.Close()
			_, err := httpProvider("openai", server.URL, time.Second).Complete(context.Background(), "p", "")
			if err == nil {
				t.Fatal("accepted unsafe provider response")
			}
			if strings.Contains(err.Error(), "untrusted body") {
				t.Fatalf("leaked provider response: %v", err)
			}
		})
	}
}

func TestResolveHonorsExplicitProviderOverEnvironmentAndKeys(t *testing.T) {
	t.Setenv("NINELIVES_PROVIDER", "openai")
	t.Setenv("ANTHROPIC_API_KEY", "test-key")
	t.Setenv("OPENAI_API_KEY", "test-key")
	p, err := Resolve(Options{Name: "anthropic", Timeout: time.Second})
	if err != nil || p.Name() != "anthropic" {
		t.Fatalf("provider=%v err=%v", p, err)
	}
}

func TestProviderPromptBudgetUsesLocalHTTPAndCLI(t *testing.T) {
	t.Setenv("OPENAI_API_KEY", "test-key")
	prompt := strings.Repeat("p", maxProviderPromptBytes)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var payload struct {
			MaxTokens int `json:"max_tokens"`
			Messages  []struct {
				Content string `json:"content"`
			} `json:"messages"`
		}
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil || len(payload.Messages) != 2 || len(payload.Messages[1].Content) != maxProviderPromptBytes || payload.MaxTokens != maxProviderTokens {
			t.Errorf("payload err=%v size=%d tokens=%d", err, len(payload.Messages[0].Content), payload.MaxTokens)
		}
		_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"ok"}}]}`))
	}))
	defer server.Close()
	if _, err := httpProvider("openai", server.URL, time.Second).Complete(context.Background(), prompt, ""); err != nil {
		t.Fatal(err)
	}
	if _, err := httpProvider("openai", server.URL, time.Second).Complete(context.Background(), prompt+"x", ""); err == nil {
		t.Fatal("accepted oversized HTTP prompt")
	}
	if runtime.GOOS == "windows" {
		t.Skip("HTTP prompt budget verified; remaining CLI fixture requires POSIX shell")
	}
	dir := t.TempDir()
	binary := filepath.Join(dir, "opencode")
	record := filepath.Join(dir, "bytes")
	if err := os.WriteFile(binary, []byte("#!/bin/sh\nprintf '%s' \"$2\" | wc -c > '"+record+"'\nprintf ok\n"), 0700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	if got, err := (CLIProvider{name: "opencode", timeout: cliTestTimeout}).Complete(context.Background(), prompt, ""); err != nil || got != "ok" {
		t.Fatalf("got=%q err=%v", got, err)
	}
	bytes, _ := os.ReadFile(record)
	if strings.TrimSpace(string(bytes)) != "32768" {
		t.Fatalf("argv bytes=%q", bytes)
	}
}

func TestDecisionTransportBoundsAndUsage(t *testing.T) {
	t.Setenv("OPENAI_API_KEY", "fixture-key")
	for _, usage := range []bool{false, true} {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			var body map[string]any
			if json.NewDecoder(r.Body).Decode(&body) != nil || body["max_tokens"] != float64(512) {
				t.Error("decision output cap missing")
			}
			messages, ok := body["messages"].([]any)
			if !ok || len(messages) != 2 || messages[0].(map[string]any)["role"] != "system" {
				t.Error("decision system contract missing")
			}
			response := map[string]any{"choices": []any{map[string]any{"message": map[string]any{"content": `{"action":"complete"}`}}}}
			if usage {
				response["usage"] = map[string]int{"prompt_tokens": 31, "completion_tokens": 7}
			}
			_ = json.NewEncoder(w).Encode(response)
		}))
		p := HTTPProvider{name: "openai", baseURL: server.URL, timeout: time.Second}
		got, err := p.CompleteDecision(context.Background(), "finite controls", "fixture-model", 512)
		server.Close()
		if err != nil || got.UsageAvailable != usage || usage && (got.InputTokens != 31 || got.OutputTokens != 7) {
			t.Fatal("decision transport usage mismatch")
		}
	}
}

func TestDecisionTransportCancellation(t *testing.T) {
	t.Setenv("OPENAI_API_KEY", "fixture-key")
	entered, stopped := make(chan struct{}), make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Consume the body so the server can observe client disconnect reliably.
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		close(entered)
		<-r.Context().Done()
		close(stopped)
	}))
	defer server.Close()
	ctx, cancel := context.WithCancel(context.Background())
	finished := make(chan error, 1)
	go func() {
		_, err := (HTTPProvider{name: "openai", baseURL: server.URL, timeout: time.Second}).CompleteDecision(ctx, "controls", "fixture-model", 512)
		finished <- err
	}()
	select {
	case <-entered:
	case <-time.After(time.Second):
		t.Fatal("request not started")
	}
	cancel()
	select {
	case err := <-finished:
		if !errors.Is(err, context.Canceled) {
			t.Fatal("cancellation not propagated")
		}
	case <-time.After(time.Second):
		t.Fatal("provider remained blocked")
	}
	select {
	case <-stopped:
	case <-time.After(time.Second):
		t.Fatal("HTTP request not canceled")
	}
}

// fakeLoginCLI behaves like `claude -p` resolving a subscription login: it
// answers only when USER is present and otherwise reports the logged-out state
// on stdout with exit 1, as Claude Code does.
func fakeLoginCLI(t *testing.T) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("POSIX shell fixture; Windows process ownership is tested separately")
	}
	dir := t.TempDir()
	script := "#!/bin/sh\n[ -n \"$USER\" ] || { echo 'Not logged in · Please run /login'; exit 1; }\necho \"key=${ANTHROPIC_API_KEY:-absent} user=$USER\"\ncat\n"
	if err := os.WriteFile(filepath.Join(dir, "claude"), []byte(script), 0700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
}

func TestCLIProviderFindsSubscriptionLoginWithoutCredentials(t *testing.T) {
	fakeLoginCLI(t)
	t.Setenv("USER", "tester")
	t.Setenv("ANTHROPIC_API_KEY", "must-not-reach-the-cli")
	got, err := (CLIProvider{name: "claude", timeout: cliTestTimeout}).Complete(context.Background(), "prompt", "")
	if err != nil || got != "key=absent user=tester\nprompt" {
		t.Fatalf("got=%q err=%v", got, err)
	}
}

func TestCLIProviderReportsWhyTheCallFailed(t *testing.T) {
	fakeLoginCLI(t)
	t.Setenv("USER", "")
	_, err := (CLIProvider{name: "claude", timeout: cliTestTimeout}).Complete(context.Background(), "secret prompt", "")
	var callErr *CallError
	if !errors.As(err, &callErr) || callErr.ExitCode != 1 || callErr.Diagnostic != "Not logged in · Please run /login" {
		t.Fatalf("err=%#v", err)
	}
	if want := "claude provider failed (exit 1): Not logged in · Please run /login"; err.Error() != want || strings.Contains(err.Error(), "secret") {
		t.Fatalf("message=%q", err.Error())
	}
}

func TestCLIDiagnosticIsBoundedRedactedAndPlain(t *testing.T) {
	raw := "\x1b[31mstarting\x1b[0m\n\nfailed: token=abc123 at https://u:pw@proxy.example\n" + strings.Repeat("é", 400)
	got := cliDiagnostic(raw)
	if strings.Contains(got, "abc123") || strings.Contains(got, "pw@") || strings.Contains(got, "\x1b") || strings.Contains(got, "\n") {
		t.Fatalf("diagnostic leaked or kept formatting: %q", got)
	}
	if len(got) > maxDiagnosticText+len("…") || !strings.HasPrefix(got, "…") || !utf8.ValidString(got) {
		t.Fatalf("diagnostic not bounded on a rune boundary: %d bytes", len(got))
	}
	if short := cliDiagnostic("line one\n  line two  \n"); short != "line one | line two" {
		t.Fatalf("short=%q", short)
	}
}
