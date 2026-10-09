package main

import (
	"bufio"
	"context"
	"encoding/json"
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
)

type mcpHarness struct {
	in    *io.PipeWriter
	lines chan map[string]any
	done  chan struct{}
}

// startMCP serves from root, which is also the working directory.
func startMCP(t *testing.T, root string) *mcpHarness {
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
		out: outW, log: io.Discard, calls: map[string]context.CancelFunc{}}
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
	if strings.Join(names, ",") != "run_test,heal_test,assess_test" {
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
	if payload, isError := h.toolCall(t, 8, "run_test", map[string]any{"spec": "a.spec.ts", "unknown": true}); !isError || !strings.Contains(payload["error"].(string), "invalid arguments") {
		t.Fatalf("unknown argument accepted: %v", payload)
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
	if isError || report == nil || report["policy"] != "assessment-source-v8" || !strings.Contains(mustJSON(t, report), `"fixed-wait"`) {
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
