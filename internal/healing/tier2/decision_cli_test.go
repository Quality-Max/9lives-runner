package tier2

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestDecisionParsersReadAnswerAndUsage(t *testing.T) {
	claude, err := parseClaudeDecision([]byte(`{"type":"result","is_error":false,"result":"{\"action\":\"wait\"}","usage":{"input_tokens":2,"cache_creation_input_tokens":730,"cache_read_input_tokens":8,"output_tokens":26}}`))
	if err != nil || claude.Text != `{"action":"wait"}` || claude.InputTokens != 740 || claude.OutputTokens != 26 || !claude.UsageAvailable {
		t.Fatalf("claude %+v %v", claude, err)
	}
	if _, err := parseClaudeDecision([]byte(`{"is_error":true,"result":"Not logged in · Please run /login"}`)); err == nil || !strings.Contains(err.Error(), "Not logged in") {
		t.Fatalf("claude error %v", err)
	}
	codex, err := parseCodexDecision([]byte("{\"type\":\"thread.started\"}\n{\"type\":\"item.completed\",\"item\":{\"type\":\"reasoning\",\"text\":\"thinking\"}}\n{\"type\":\"item.completed\",\"item\":{\"type\":\"agent_message\",\"text\":\"{\\\"action\\\":\\\"complete\\\",\\\"targetId\\\":\\\"\\\",\\\"parameter\\\":\\\"\\\"}\"}}\n{\"type\":\"turn.completed\",\"usage\":{\"input_tokens\":19161,\"cached_input_tokens\":0,\"output_tokens\":25,\"reasoning_output_tokens\":40}}\n"))
	if err != nil || !strings.Contains(codex.Text, `"complete"`) || codex.InputTokens != 19161 || codex.OutputTokens != 65 || !codex.UsageAvailable {
		t.Fatalf("codex %+v %v", codex, err)
	}
	if _, err := parseCodexDecision([]byte(`{"type":"turn.failed","error":{"message":"unexpected status 401 Unauthorized"}}`)); err == nil || !strings.Contains(err.Error(), "401") {
		t.Fatalf("codex failure %v", err)
	}
	opencode, err := parseOpenCodeDecision([]byte("{\"type\":\"step_start\"}\n{\"type\":\"text\",\"part\":{\"text\":\"{\\\"action\\\":\"}}\n{\"type\":\"text\",\"part\":{\"text\":\"\\\"wait\\\"}\"}}\n{\"type\":\"step_finish\",\"part\":{\"tokens\":{\"input\":14929,\"output\":16,\"reasoning\":151,\"cache\":{\"read\":1792,\"write\":0}}}}\n"))
	if err != nil || opencode.Text != `{"action":"wait"}` || opencode.InputTokens != 16721 || opencode.OutputTokens != 167 || !opencode.UsageAvailable {
		t.Fatalf("opencode %+v %v", opencode, err)
	}
}

func TestDecisionCLIRunsDecisionOnlyAndWithoutCredentials(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX shell fixture")
	}
	dir := t.TempDir()
	// The fake fails unless it got the decision-only flags, and echoes whether
	// an API key reached it.
	script := "#!/bin/sh\ncase \"$*\" in *'--tools  --strict-mcp-config --setting-sources  --no-session-persistence'*) ;; *) echo \"bad args: $*\" >&2; exit 9;; esac\n" +
		"cat >/dev/null\nprintf '{\"is_error\":false,\"result\":\"{\\\\\"action\\\\\":\\\\\"wait\\\\\"}\",\"usage\":{\"input_tokens\":5,\"output_tokens\":7}}'\n" +
		"[ -z \"$ANTHROPIC_API_KEY\" ] || exit 8\n"
	if err := os.WriteFile(filepath.Join(dir, "claude"), []byte(script), 0700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("ANTHROPIC_API_KEY", "must-not-reach-the-cli")
	provider, err := DecisionCLI("claude-code", cliTestTimeout)
	if err != nil {
		t.Fatal(err)
	}
	completion, err := provider.CompleteDecision(context.Background(), `{"contract":"9l.goal/1"}`, "", 512)
	if err != nil || completion.Text != `{"action":"wait"}` || completion.InputTokens != 5 || completion.OutputTokens != 7 || provider.DecisionInputOverhead() <= 0 {
		t.Fatalf("completion %+v err %v", completion, err)
	}
	var callErr *CallError
	if _, err := (CLIProvider{name: "claude", timeout: cliTestTimeout}).CompleteDecision(context.Background(), "p", "", 0); err == nil || errors.As(err, &callErr) {
		t.Fatalf("invalid token limit: %v", err)
	}
	if _, err := DecisionCLI("anthropic", cliTestTimeout); err == nil {
		t.Fatal("API provider accepted as a CLI")
	}
	t.Setenv("PATH", t.TempDir())
	if _, err := DecisionCLI("codex", cliTestTimeout); err == nil || !strings.Contains(err.Error(), "not installed") {
		t.Fatalf("missing CLI: %v", err)
	}
}
