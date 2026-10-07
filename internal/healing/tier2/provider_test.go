package tier2

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

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
	dir := t.TempDir()
	binary := filepath.Join(dir, "codex")
	script := "#!/bin/sh\ncat\necho ignored >&2\n"
	if err := os.WriteFile(binary, []byte(script), 0700); err != nil {
		t.Fatal(err)
	}
	old := os.Getenv("PATH")
	t.Setenv("PATH", dir+string(os.PathListSeparator)+old)
	got, err := (CLIProvider{name: "codex", timeout: time.Second}).Complete(context.Background(), "prompt-in-stdin", "m")
	if err != nil || got != "prompt-in-stdin" {
		t.Fatalf("got=%q err=%v", got, err)
	}
}

func TestResolvePreservesClaudeCodeAliasAndHTTPFallback(t *testing.T) {
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
	provider, err := Resolve(Options{Name: "claude-code", BaseURL: server.URL, Timeout: time.Second})
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
	dir := t.TempDir()
	binary := filepath.Join(dir, "codex")
	script := "#!/bin/sh\n[ \"$1\" = exec ] && [ \"$2\" = --skip-git-repo-check ] && [ \"$3\" = --model ] && [ \"$4\" = chosen ] && [ \"$5\" = - ] || exit 7\ncat\n"
	if err := os.WriteFile(binary, []byte(script), 0700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	if got, err := (CLIProvider{name: "codex", timeout: time.Second}).Complete(context.Background(), "prompt", "chosen"); err != nil || got != "prompt" {
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
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil || len(payload.Messages) != 1 || len(payload.Messages[0].Content) != maxProviderPromptBytes || payload.MaxTokens != maxProviderTokens {
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
	dir := t.TempDir()
	binary := filepath.Join(dir, "opencode")
	record := filepath.Join(dir, "bytes")
	if err := os.WriteFile(binary, []byte("#!/bin/sh\nprintf '%s' \"$2\" | wc -c > '"+record+"'\nprintf ok\n"), 0700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	if got, err := (CLIProvider{name: "opencode", timeout: time.Second}).Complete(context.Background(), prompt, ""); err != nil || got != "ok" {
		t.Fatalf("got=%q err=%v", got, err)
	}
	bytes, _ := os.ReadFile(record)
	if strings.TrimSpace(string(bytes)) != "32768" {
		t.Fatalf("argv bytes=%q", bytes)
	}
}
