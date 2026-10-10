package main

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/Quality-Max/9lives-runner/internal/healing/tier2"
)

type mcpHarness struct {
	in    *io.PipeWriter
	lines chan map[string]any
	done  chan struct{}
}

// startMCP serves from root, which is also the working directory.
func startMCP(t *testing.T, root string, configure ...func(*mcpServer)) *mcpHarness {
	t.Helper()
	root, err := filepath.EvalSymlinks(root)
	if err != nil {
		t.Fatal(err)
	}
	t.Chdir(root)
	inR, inW := io.Pipe()
	outR, outW := io.Pipe()
	h := &mcpHarness{in: inW, lines: make(chan map[string]any, 16), done: make(chan struct{})}
	server := &mcpServer{root: root, runTimeout: time.Minute, receiptDir: ".9lives/receipts", healReceipt: ".9lives/healing-receipts",
		out: outW, log: io.Discard, calls: map[string]context.CancelFunc{}, healSlot: make(chan struct{}, 1)}
	for _, apply := range configure {
		apply(server)
	}
	go func() {
		server.serve(context.Background(), inR)
		outW.Close()
		close(h.done)
	}()
	go func() {
		scanner := bufio.NewScanner(outR)
		scanner.Buffer(make([]byte, 1<<20), 1<<20)
		for scanner.Scan() {
			var message map[string]any
			if err := json.Unmarshal(scanner.Bytes(), &message); err != nil {
				message = map[string]any{"invalid": scanner.Text()}
			}
			h.lines <- message
		}
		close(h.lines)
	}()
	t.Cleanup(func() { inW.Close(); <-h.done })
	return h
}

func (h *mcpHarness) send(t *testing.T, line string) {
	t.Helper()
	if _, err := io.WriteString(h.in, line+"\n"); err != nil {
		t.Fatal(err)
	}
}

func (h *mcpHarness) next(t *testing.T) map[string]any {
	t.Helper()
	select {
	case message, ok := <-h.lines:
		if !ok {
			t.Fatal("server closed stdout")
		}
		return message
	case <-time.After(60 * time.Second):
		t.Fatal("no response")
	}
	return nil
}

// toolCall returns a tool result's decoded payload and isError.
func (h *mcpHarness) toolCall(t *testing.T, id int, name string, arguments any) (map[string]any, bool) {
	t.Helper()
	params, _ := json.Marshal(map[string]any{"name": name, "arguments": arguments})
	h.send(t, `{"jsonrpc":"2.0","id":`+jsonInt(id)+`,"method":"tools/call","params":`+string(params)+`}`)
	response := h.next(t)
	result, _ := response["result"].(map[string]any)
	if result == nil || response["id"] != float64(id) {
		t.Fatalf("tool response %v", response)
	}
	content := result["content"].([]any)[0].(map[string]any)
	var payload map[string]any
	if err := json.Unmarshal([]byte(content["text"].(string)), &payload); err != nil {
		t.Fatalf("tool text is not JSON: %v", content)
	}
	return payload, result["isError"].(bool)
}

func jsonInt(n int) string { data, _ := json.Marshal(n); return string(data) }

func errorCode(message map[string]any) float64 {
	if failure, ok := message["error"].(map[string]any); ok {
		return failure["code"].(float64)
	}
	return 0
}

func TestMCPProtocolFraming(t *testing.T) {
	h := startMCP(t, t.TempDir())
	h.send(t, `{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-03-26"}}`)
	init := h.next(t)["result"].(map[string]any)
	if init["protocolVersion"] != "2025-03-26" || init["serverInfo"].(map[string]any)["name"] != "9lives" {
		t.Fatalf("initialize %v", init)
	}
	h.send(t, `{"jsonrpc":"2.0","id":2,"method":"initialize","params":{"protocolVersion":"1999-01-01"}}`)
	if got := h.next(t)["result"].(map[string]any)["protocolVersion"]; got != mcpLatestProtocol {
		t.Fatalf("unsupported version answered with %v", got)
	}
	// Notifications and blank lines get no response; the next response is ping's.
	h.send(t, `{"jsonrpc":"2.0","method":"notifications/initialized"}`)
	h.send(t, ``)
	h.send(t, `{"jsonrpc":"2.0","method":"tools/call","params":{"name":"run_test","arguments":{"spec":"x"}}}`)
	h.send(t, `{"jsonrpc":"2.0","id":"p","method":"ping"}`)
	if ping := h.next(t); ping["id"] != "p" || ping["result"] == nil {
		t.Fatalf("ping %v", ping)
	}
	h.send(t, `{"jsonrpc":"2.0","id":3,"method":"tools/list"}`)
	var names []string
	for _, tool := range h.next(t)["result"].(map[string]any)["tools"].([]any) {
		names = append(names, tool.(map[string]any)["name"].(string))
	}
	if strings.Join(names, ",") != "run_test,heal_test,assess_test,confirm_finding" {
		t.Fatalf("tools %v", names)
	}
	for line, code := range map[string]float64{
		`{"jsonrpc":"2.0","id":4,"method":"resources/list"}`:                    -32601,
		`{"jsonrpc":"2.0","id":5,"method":"tools/call","params":{"name":"rm"}}`: -32602,
		`not json`: -32700,
		`[{"jsonrpc":"2.0","id":6,"method":"ping"}]`:                                          -32600,
		`{"jsonrpc":"2.0","id":7,"method":` + `"` + strings.Repeat("x", mcpMaxMessage) + `"}`: -32700,
	} {
		h.send(t, line)
		if got := errorCode(h.next(t)); got != code {
			t.Fatalf("%.40s: code %v, want %v", line, got, code)
		}
	}
	// Each tool takes only its own arguments, including ones another tool takes.
	for i, call := range []struct {
		name      string
		arguments map[string]any
	}{
		{"run_test", map[string]any{"spec": "a.spec.ts", "unknown": true}},
		{"run_test", map[string]any{"spec": "a.spec.ts", "apply": true}},
		{"assess_test", map[string]any{"spec": "a.spec.ts"}},
		{"heal_test", map[string]any{"spec": "a.spec.ts", "path": "a.spec.ts"}},
		{"confirm_finding", map[string]any{"spec": "a.spec.ts", "unfixed": "HEAD", "apply": true}},
	} {
		if payload, isError := h.toolCall(t, 8+i, call.name, call.arguments); !isError || !strings.Contains(payload["error"].(string), "invalid arguments") {
			t.Fatalf("%s accepted %v: %v", call.name, call.arguments, payload)
		}
	}
}

func TestMCPToolPathsStayInsideTheWorkingDirectory(t *testing.T) {
	outside := filepath.Join(t.TempDir(), "outside.spec.ts")
	if err := os.WriteFile(outside, []byte("test('x', () => {});\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	if err := os.Mkdir(filepath.Join(root, "tests"), 0o755); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(root, "tests", "link.spec.ts")
	if err := os.Symlink(outside, link); err != nil {
		t.Skip("symlinks unavailable")
	}
	if err := os.WriteFile(filepath.Join(root, "large.spec.ts"), make([]byte, tier2.MaxSourceBytes+1), 0o600); err != nil {
		t.Fatal(err)
	}
	h := startMCP(t, root)
	for i, call := range []struct {
		name, key, path, want string
	}{
		{"run_test", "spec", outside, "outside the server's working directory"},
		{"heal_test", "spec", "tests/link.spec.ts", "outside the server's working directory"},
		{"assess_test", "path", "../", "outside the server's working directory"},
		{"run_test", "spec", "tests", "not a regular file"},
		{"run_test", "spec", "missing.spec.ts", "path not found"},
		{"heal_test", "spec", "", "a path is required"},
		{"heal_test", "spec", "large.spec.ts", "exceeds the 1 MiB healing input limit"},
	} {
		payload, isError := h.toolCall(t, i+1, call.name, map[string]any{call.key: call.path})
		if !isError || !strings.Contains(payload["error"].(string), call.want) {
			t.Fatalf("%s %q: %v", call.name, call.path, payload)
		}
	}
}

func TestMCPAssessTestReturnsTheReport(t *testing.T) {
	if _, err := exec.LookPath("node"); err != nil {
		t.Skip("requires Node")
	}
	root := t.TempDir()
	spec := "import {test, expect} from '@playwright/test';\ntest('t', async ({page}) => { await page.waitForTimeout(100); expect(1).toBe(1); });\n"
	if err := os.WriteFile(filepath.Join(root, "a.spec.ts"), []byte(spec), 0o600); err != nil {
		t.Fatal(err)
	}
	h := startMCP(t, root)
	payload, isError := h.toolCall(t, 1, "assess_test", map[string]any{"path": "a.spec.ts"})
	report, _ := payload["report"].(map[string]any)
	if isError || report == nil || report["policy"] != "assessment-source-v9" || !strings.Contains(mustJSON(t, report), `"fixed-wait"`) {
		t.Fatalf("assess_test %v", payload)
	}
}

// A run in progress keeps the server responsive, a cancelled call gets no
// response and closing stdin stops the run.
func TestMCPRunTestIsCancellable(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell script Playwright stand-in")
	}
	root := t.TempDir()
	bin := filepath.Join(root, "node_modules", ".bin")
	if err := os.MkdirAll(bin, 0o755); err != nil {
		t.Fatal(err)
	}
	files := map[string]string{
		"package.json":                 `{"devDependencies":{"@playwright/test":"1.61.1"}}`,
		"slow.spec.ts":                 "test('slow', async () => {});\n",
		"node_modules/.bin/playwright": "#!/bin/sh\necho $$ > \"$0.pid\"\nexec sleep 30\n",
	}
	for name, body := range files {
		if err := os.WriteFile(filepath.Join(root, name), []byte(body), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	h := startMCP(t, root)
	h.send(t, `{"jsonrpc":"2.0","id":"slow","method":"tools/call","params":{"name":"run_test","arguments":{"spec":"slow.spec.ts"}}}`)
	h.send(t, `{"jsonrpc":"2.0","id":1,"method":"ping"}`)
	if ping := h.next(t); ping["id"] != float64(1) {
		t.Fatalf("server blocked by a running tool: %v", ping)
	}
	pid := waitForPID(t, filepath.Join(bin, "playwright.pid"))
	h.send(t, `{"jsonrpc":"2.0","method":"notifications/cancelled","params":{"requestId":"slow"}}`)
	// The cancellation alone, before stdin closes, stops the test process.
	deadline := time.Now().Add(15 * time.Second)
	for processAlive(pid) {
		if time.Now().After(deadline) {
			t.Fatal("cancelled run still executing")
		}
		time.Sleep(50 * time.Millisecond)
	}
	h.send(t, `{"jsonrpc":"2.0","id":2,"method":"ping"}`)
	if ping := h.next(t); ping["id"] != float64(2) {
		t.Fatalf("cancelled call answered: %v", ping)
	}
	h.in.Close()
	select {
	case <-h.done:
	case <-time.After(20 * time.Second):
		t.Fatal("server did not stop after stdin closed")
	}
	if message, ok := <-h.lines; ok {
		t.Fatalf("unexpected response after cancellation: %v", message)
	}
}

func mustJSON(t *testing.T, value any) string {
	t.Helper()
	data, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func waitForPID(t *testing.T, path string) int {
	t.Helper()
	deadline := time.Now().Add(30 * time.Second)
	for time.Now().Before(deadline) {
		if data, err := os.ReadFile(path); err == nil {
			if pid, err := strconv.Atoi(strings.TrimSpace(string(data))); err == nil {
				return pid
			}
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal("test process did not start")
	return 0
}

func processAlive(pid int) bool {
	process, err := os.FindProcess(pid)
	return err == nil && process.Signal(syscall.Signal(0)) == nil
}

// writeProject writes a Playwright project whose installed CLI is a shell
// script standing in for Playwright.
func writeProject(t *testing.T, playwright string, files map[string]string) string {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("shell script Playwright stand-in")
	}
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "node_modules", ".bin"), 0o755); err != nil {
		t.Fatal(err)
	}
	files["package.json"] = `{"devDependencies":{"@playwright/test":"1.61.1"}}`
	files["node_modules/.bin/playwright"] = "#!/bin/sh\n" + playwright
	for name, body := range files {
		if err := os.WriteFile(filepath.Join(root, name), []byte(body), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	return root
}

type scriptedProvider struct{ response string }

var _ tier2.Provider = scriptedProvider{}

func (scriptedProvider) Name() string { return "scripted" }
func (p scriptedProvider) Complete(context.Context, string, string) (string, error) {
	return p.response, nil
}

// A verified candidate that cannot be saved is an error result that keeps
// the verification evidence, not a successful one.
func TestMCPHealReportsAFailedSave(t *testing.T) {
	original := "import { test, expect } from '@playwright/test';\ntest('save', async ({ page }) => {\n  await page.locator('#old').click();\n  await expect(page.locator('#other')).toBeVisible();\n});\n"
	candidate := strings.Replace(original, "#old", "#new", 1)
	passed := `{"stats":{"duration":1},"suites":[{"specs":[{"tests":[{"status":"expected","results":[{"status":"passed"}]}]}]}]}`
	failed := `{"stats":{"duration":1},"suites":[{"specs":[{"tests":[{"status":"unexpected","results":[{"status":"failed","error":{"message":"TimeoutError: locator.click: Timeout 1000ms exceeded.\nCall log:\n  - waiting for locator(\u0027#old\u0027)"}}]}]}]}]}`
	root := writeProject(t, "if grep -q '#new' \"$2\"; then printf '%s' '"+passed+"' > \"$PLAYWRIGHT_JSON_OUTPUT_FILE\"; exit 0; fi\nprintf '%s' '"+failed+"' > \"$PLAYWRIGHT_JSON_OUTPUT_FILE\"\nexit 1\n",
		map[string]string{"login.spec.ts": original})
	if err := os.Mkdir(filepath.Join(root, "login.spec.ts.healed"), 0o755); err != nil {
		t.Fatal(err)
	}
	h := startMCP(t, root, func(s *mcpServer) { s.provider = scriptedProvider{"```typescript\n" + candidate + "```"} })
	payload, isError := h.toolCall(t, 1, "heal_test", map[string]any{"spec": "login.spec.ts"})
	verified, _ := payload["verified"].(map[string]any)
	if !isError || payload["error"] != "the verified candidate could not be saved" || payload["state"] != "verified" ||
		verified["passed"] != true || payload["savedPath"] != nil || payload["applied"] != false {
		t.Fatalf("failed save reported as %v (isError=%v)", payload, isError)
	}
	if source, _ := os.ReadFile(filepath.Join(root, "login.spec.ts")); string(source) != original {
		t.Fatal("source changed")
	}
	// The same session saves once the path is free.
	if err := os.Remove(filepath.Join(root, "login.spec.ts.healed")); err != nil {
		t.Fatal(err)
	}
	payload, isError = h.toolCall(t, 2, "heal_test", map[string]any{"spec": "login.spec.ts"})
	if isError || payload["error"] != nil || !strings.HasSuffix(payload["savedPath"].(string), "login.spec.ts.healed") || !strings.Contains(payload["diff"].(string), "+  await page.locator('#new')") {
		t.Fatalf("save after fix: %v (isError=%v)", payload, isError)
	}
}

// Cancelling an assessment stops its analysis helper; it does not run to the
// helper's own deadline.
func TestMCPAssessTestIsCancellable(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell script Node stand-in")
	}
	bin := t.TempDir()
	if err := os.WriteFile(filepath.Join(bin, "node"), []byte("#!/bin/sh\necho $$ > \"$0.pid\"\nexec sleep 30\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "a.spec.ts"), []byte("test('t', () => {});\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	h := startMCP(t, root)
	h.send(t, `{"jsonrpc":"2.0","id":"assess","method":"tools/call","params":{"name":"assess_test","arguments":{"path":"a.spec.ts"}}}`)
	pid := waitForPID(t, filepath.Join(bin, "node.pid"))
	h.send(t, `{"jsonrpc":"2.0","method":"notifications/cancelled","params":{"requestId":"assess"}}`)
	// The helper's own deadline is ten seconds.
	deadline := time.Now().Add(5 * time.Second)
	for processAlive(pid) {
		if time.Now().After(deadline) {
			t.Fatal("cancelled assessment helper still running")
		}
		time.Sleep(50 * time.Millisecond)
	}
	h.in.Close()
	select {
	case <-h.done:
	case <-time.After(5 * time.Second):
		t.Fatal("server did not stop promptly")
	}
	if message, ok := <-h.lines; ok {
		t.Fatalf("cancelled assessment answered: %v", message)
	}
}

// Queued heals that are cancelled free their request slots while another
// heal is still running.
func TestMCPCancelledQueuedHealsFreeTheirSlots(t *testing.T) {
	root := writeProject(t, "echo $$ > \"$0.pid\"\nexec sleep 30\n", map[string]string{"slow.spec.ts": "test('slow', async () => {});\n"})
	h := startMCP(t, root)
	call := func(id string) string {
		return `{"jsonrpc":"2.0","id":"` + id + `","method":"tools/call","params":{"name":"heal_test","arguments":{"spec":"slow.spec.ts"}}}`
	}
	h.send(t, call("active"))
	waitForPID(t, filepath.Join(root, "node_modules", ".bin", "playwright.pid"))
	for _, id := range []string{"q1", "q2", "q3"} {
		h.send(t, call(id))
	}
	// All four slots are taken.
	if payload, isError := h.toolCall(t, 1, "run_test", map[string]any{"spec": "missing.spec.ts"}); !isError || !strings.Contains(payload["error"].(string), "too many tool calls") {
		t.Fatalf("slots were not full: %v", payload)
	}
	for _, id := range []string{"q1", "q2", "q3"} {
		h.send(t, `{"jsonrpc":"2.0","method":"notifications/cancelled","params":{"requestId":"`+id+`"}}`)
	}
	deadline := time.Now().Add(5 * time.Second)
	for id := 2; ; id++ {
		payload, _ := h.toolCall(t, id, "run_test", map[string]any{"spec": "missing.spec.ts"})
		if strings.Contains(payload["error"].(string), "path not found") {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("cancelled queued heals still hold their slots: %v", payload)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

func TestReceiptLabelsCannotLeaveTheReceiptDirectory(t *testing.T) {
	for label, ok := range map[string]bool{"original": true, "tier1": true, "tier2-3": true, "../x": false, "a/b": false, "": false, "..": false} {
		if receiptLabel.MatchString(label) != ok {
			t.Errorf("%q accepted=%v", label, !ok)
		}
	}
}

func TestMCPConfirmFindingReturnsTheVerdict(t *testing.T) {
	root, _, _ := confirmRepository(t)
	worker := &fakeConfirmWorker{outcome: shopOutcome}
	confirmExecutor = worker
	t.Cleanup(func() { confirmExecutor = nil })
	h := startMCP(t, root)
	payload, isError := h.toolCall(t, 1, "confirm_finding", map[string]any{"spec": "tests/repro.spec.ts", "unfixed": "HEAD~1", "finding_id": "QUA-14", "finding": "count drops the last item"})
	if isError && strings.Contains(fmt.Sprint(payload["error"]), "could not be linked") && runtime.GOOS == "windows" {
		t.Skip("this Windows account cannot create symbolic links")
	}
	report, _ := payload["report"].(map[string]any)
	path, _ := payload["reportPath"].(string)
	if isError || report == nil || report["verdict"] != "confirmed" || report["findingId"] != "QUA-14" || len(worker.workDirs) != 2 {
		t.Fatalf("confirm_finding %v", payload)
	}
	if _, err := os.Stat(path); err != nil || strings.Contains(mustJSON(t, payload), "drops the last item") {
		t.Fatalf("report path %q: %v", path, err)
	}
	for i, call := range []struct {
		arguments map[string]any
		want      string
	}{
		{map[string]any{"spec": "tests/repro.spec.ts"}, "unfixed is required"},
		{map[string]any{"spec": "tests/repro.spec.ts", "unfixed": "no-such-branch"}, "does not name a commit"},
		{map[string]any{"spec": "tests/repro.spec.ts", "unfixed": "--output=x"}, "invalid revision"},
		{map[string]any{"spec": "../outside.spec.ts", "unfixed": "HEAD~1"}, "path not found"},
	} {
		payload, isError := h.toolCall(t, 2+i, "confirm_finding", call.arguments)
		if !isError || !strings.Contains(fmt.Sprint(payload["error"]), call.want) {
			t.Fatalf("%v: %v", call.arguments, payload)
		}
	}
	if len(worker.workDirs) != 2 {
		t.Fatalf("a refused call ran the spec: %d runs", len(worker.workDirs))
	}
}

func TestMCPRunTestReturnsEachFailureAndItsErrorContext(t *testing.T) {
	report := `{"stats":{"duration":1},"suites":[{"title":"login.spec.ts","specs":[{"title":"signs in","file":"login.spec.ts","line":2,"tests":[{"status":"unexpected","results":[{"status":"failed","errors":[{"message":"TimeoutError: locator.fill: Timeout\nCall log:\n  - waiting for locator('#emailAddress')"}],"attachments":[{"name":"error-context","contentType":"text/markdown","path":"PROJECT/test-results/login/error-context.md"}]}]}]}]}]}`
	script := "mkdir -p test-results/login\nprintf '%s\\n' '- textbox \"E-mail\" [ref=e2]' 'api_key=sk-test' > test-results/login/error-context.md\n" +
		"sed \"s#PROJECT#$PWD#\" report.json > \"$PLAYWRIGHT_JSON_OUTPUT_FILE\"\nexit 1\n"
	root := writeProject(t, script, map[string]string{"login.spec.ts": "test('signs in', async () => {});\n", "report.json": report})
	h := startMCP(t, root)
	payload, isError := h.toolCall(t, 1, "run_test", map[string]any{"spec": "login.spec.ts"})
	failures, _ := payload["failures"].([]any)
	if isError || payload["status"] != "failed" || len(failures) != 1 {
		t.Fatalf("run_test: %v (isError=%v)", payload, isError)
	}
	failure := failures[0].(map[string]any)
	context, _ := payload["failureContext"].(string)
	if failure["title"] != "signs in" || !strings.Contains(failure["message"].(string), "waiting for locator('#emailAddress')") ||
		!strings.Contains(context, `textbox "E-mail"`) || strings.Contains(context, "sk-test") {
		t.Fatalf("failure=%v context=%q", failure, context)
	}
}
