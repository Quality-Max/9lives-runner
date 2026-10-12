package main

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

func TestMCPListsTheBrowserTools(t *testing.T) {
	h := startMCP(t, t.TempDir())
	h.send(t, `{"jsonrpc":"2.0","id":1,"method":"tools/list"}`)
	tools := h.next(t)["result"].(map[string]any)["tools"].([]any)
	names := map[string]map[string]any{}
	for _, tool := range tools {
		tool := tool.(map[string]any)
		names[tool["name"].(string)] = tool
	}
	for _, name := range []string{"run_test", "heal_test", "assess_test", "confirm_finding", "scan_url", "inspect_page", "generate_playwright_repro"} {
		if names[name] == nil {
			t.Fatalf("tools/list lacks %s", name)
		}
	}
	if names["inspect_page"]["annotations"].(map[string]any)["readOnlyHint"] != true || names["generate_playwright_repro"]["annotations"].(map[string]any)["destructiveHint"] != true {
		t.Fatal("browser tool annotations changed")
	}
}

func TestMCPBrowserToolArgumentsAreRefusedBeforeABrowserStarts(t *testing.T) {
	root := t.TempDir()
	outside := filepath.Join(t.TempDir(), "state.json")
	if err := os.WriteFile(outside, []byte("{}"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "state.json"), []byte("{}"), 0o600); err != nil {
		t.Fatal(err)
	}
	h := startMCP(t, root)
	page := "https://example.com/"
	for i, call := range []struct {
		name string
		args map[string]any
		want string
	}{
		{"scan_url", map[string]any{"url": "file:///etc/passwd"}, "http or https"},
		{"scan_url", map[string]any{"url": "https://user:secret@example.com/"}, "credentials"},
		{"scan_url", map[string]any{"url": page, "checks": []string{"security-headers"}}, `unknown check "security-headers"`},
		{"scan_url", map[string]any{"url": page, "checks": []string{}}, "at least one check"},
		{"scan_url", map[string]any{"url": page, "max_links": 251}, "max_links must be 0 to 250"},
		{"scan_url", map[string]any{"url": page, "format": "html"}, "format must be"},
		{"scan_url", map[string]any{"url": page, "min_severity": "urgent"}, "min_severity"},
		{"scan_url", map[string]any{"url": page, "weight_budget": map[string]any{"total_bytes": 0}}, "weight_budget"},
		{"scan_url", map[string]any{"url": page, "baseline": outside}, "baseline is outside the working directory"},
		{"scan_url", map[string]any{"url": page, "baseline": []any{}}, "previous scan result"},
		{"scan_url", map[string]any{"url": page, "storageStatePath": "state.json"}, "invalid arguments for scan_url"},
		{"scan_url", map[string]any{"url": page, "timeout": 0}, "timeout must be at least 1 second"},
		{"inspect_page", map[string]any{"url": page, "storage_state_path": "state.json"}, "requires acknowledge_private_content"},
		{"inspect_page", map[string]any{"url": page, "storage_state_path": "state.json", "acknowledge_private_content": false}, "can only be true"},
		{"inspect_page", map[string]any{"url": page, "storage_state_path": outside, "acknowledge_private_content": true}, "outside the working directory"},
		{"inspect_page", map[string]any{"url": page, "storage_state_path": "missing.json", "acknowledge_private_content": true}, "existing file"},
		{"inspect_page", map[string]any{"url": page, "viewport": map[string]any{"width": 100, "height": 800}}, "viewport width"},
		{"inspect_page", map[string]any{"url": page, "format": "issue"}, "format must be json or markdown"},
		{"generate_playwright_repro", map[string]any{"url": page, "output_path": "../x.spec.ts"}, "escapes the project root"},
	} {
		payload, isError := h.toolCall(t, i+1, call.name, call.args)
		if !isError || !strings.Contains(payload["error"].(string), call.want) {
			t.Fatalf("%s %v: %v", call.name, call.args, payload)
		}
	}
}

func TestMCPGenerateReproWritesInsideTheWorkingDirectory(t *testing.T) {
	root := t.TempDir()
	h := startMCP(t, root)
	payload, isError := h.toolCall(t, 1, "generate_playwright_repro", map[string]any{
		"url": "https://example.com/", "finding": map[string]any{"category": "console", "message": "boom"}, "output_path": "tests/boom.spec.ts", "sdk": true,
	})
	if isError || payload["filePath"] != "tests/boom.spec.ts" || payload["version"] != float64(MCPToolResultVersion) {
		t.Fatalf("payload %v", payload)
	}
	written, err := os.ReadFile(filepath.Join(root, "tests", "boom.spec.ts"))
	if err != nil || !strings.Contains(string(written), "from '@9l/playwright'") || string(written) != payload["code"] {
		t.Fatalf("written %q, %v", written, err)
	}
	if payload, isError = h.toolCall(t, 2, "generate_playwright_repro", map[string]any{"url": "https://example.com/", "output_path": "tests/boom.spec.ts"}); !isError || !strings.Contains(payload["error"].(string), "overwrite") {
		t.Fatalf("second write: %v", payload)
	}
}

func TestWithVersionKeepsTheResultFields(t *testing.T) {
	for raw, want := range map[string]string{
		`{"url":"u","score":1}`: `{"version":1,"url":"u","score":1}`,
		` {} `:                  `{"version":1}`,
		`[1]`:                   `[1]`,
	} {
		if got := string(withVersion(json.RawMessage(raw))); got != want {
			t.Fatalf("withVersion(%s) = %s", raw, got)
		}
	}
}

// browserProject returns a working directory that resolves the repository's
// Playwright, or skips when it or Chromium is not installed; CI sets
// NINELIVES_REQUIRE_BROWSER=1 where they are.
func browserProject(t *testing.T) string {
	t.Helper()
	modules, err := filepath.Abs(filepath.Join("..", "..", "node_modules"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(modules, "playwright", "package.json")); err != nil {
		if os.Getenv("NINELIVES_REQUIRE_BROWSER") == "1" {
			t.Fatal("NINELIVES_REQUIRE_BROWSER=1 but the repository's Playwright is not installed; run npm ci")
		}
		t.Skip("npm ci and npm exec playwright install chromium to run browser checks")
	}
	browserOnce.Do(func() {
		probe := exec.Command("node", "-e", "require('playwright').chromium.launch().then(b => b.close(), () => process.exit(1))")
		probe.Dir = filepath.Dir(modules)
		browserInstalled = probe.Run() == nil
	})
	if !browserInstalled {
		if os.Getenv("NINELIVES_REQUIRE_BROWSER") == "1" {
			t.Fatal("NINELIVES_REQUIRE_BROWSER=1 but Chromium cannot launch; run npm exec playwright install chromium")
		}
		t.Skip("npm exec playwright install chromium to run browser checks")
	}
	root := t.TempDir()
	if err := os.Symlink(modules, filepath.Join(root, "node_modules")); err != nil {
		t.Skip("symlinks unavailable")
	}
	return root
}

var (
	browserOnce      sync.Once
	browserInstalled bool
)

const browserFixture = `<!doctype html><html lang="en"><head><title>Fixture</title></head><body>
<h1>Checkout</h1><img src="data:image/gif;base64,R0lGODlhAQABAAAAACw=">
<button data-testid="pay">Pay</button><input placeholder="Email">
<script>console.error('checkout calculation failed')</script></body></html>`

func browserServer(t *testing.T) string {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write([]byte(browserFixture))
	}))
	t.Cleanup(server.Close)
	return server.URL + "/"
}

func TestBrowserToolsScanAndInspectALocalPage(t *testing.T) {
	root := browserProject(t)
	page := browserServer(t)
	h := startMCP(t, root)

	payload, isError := h.toolCall(t, 1, "scan_url", map[string]any{"url": page})
	if !isError || !strings.Contains(payload["error"].(string), "not permitted by the local safety policy") {
		t.Fatalf("loopback without opt-in: %v", payload)
	}
	payload, isError = h.toolCall(t, 2, "scan_url", map[string]any{"url": page, "allow_private_network": true, "checks": []string{"console", "accessibility"}})
	if isError || payload["version"] != float64(MCPToolResultVersion) {
		t.Fatalf("scan: %v", payload)
	}
	findings, _ := json.Marshal(payload["findings"])
	if !strings.Contains(string(findings), "checkout calculation failed") || !strings.Contains(string(findings), `"category":"accessibility"`) {
		t.Fatalf("scan findings %s", findings)
	}

	params, _ := json.Marshal(map[string]any{"name": "scan_url", "arguments": map[string]any{"url": page, "allow_private_network": true, "checks": []string{"console"}, "format": "markdown"}})
	h.send(t, `{"jsonrpc":"2.0","id":3,"method":"tools/call","params":`+string(params)+`}`)
	text := h.next(t)["result"].(map[string]any)["content"].([]any)[0].(map[string]any)["text"].(string)
	if !strings.HasPrefix(text, "# QA Scan") || !strings.Contains(text, "9l scan <url>") {
		t.Fatalf("markdown report %q", text)
	}

	payload, isError = h.toolCall(t, 4, "inspect_page", map[string]any{"url": page, "allow_private_network": true})
	locators, _ := json.Marshal(payload["interactive"])
	if isError || !strings.Contains(string(locators), `getByTestId(\"pay\")`) {
		t.Fatalf("inspect: %v", payload)
	}
}

func TestScanFailsOnNewFindingsSinceItsBaseline(t *testing.T) {
	root := browserProject(t)
	page := browserServer(t)
	t.Chdir(root)
	var out, errOut bytes.Buffer
	if code := run([]string{"scan", page, "--allow-private-network", "--checks", "seo", "--format", "json", "--out", "baseline.json"}, &out, &errOut); code != exitPassed {
		t.Fatalf("baseline scan exit %d: %s", code, errOut.String())
	}
	errOut.Reset()
	if code := run([]string{"scan", page, "--allow-private-network", "--checks", "seo", "--baseline", "baseline.json", "--fail-on-new", "--format", "json"}, &out, &errOut); code != exitPassed {
		t.Fatalf("unchanged scan exit %d: %s", code, errOut.String())
	}
	errOut.Reset()
	if code := run([]string{"scan", page, "--allow-private-network", "--checks", "seo,console", "--baseline", "baseline.json", "--fail-on-new"}, &out, &errOut); code != exitFailed || !strings.Contains(errOut.String(), "new finding") {
		t.Fatalf("regressed scan exit %d: %s", code, errOut.String())
	}
	if code := run([]string{"scan", page, "--checks", "seo"}, &out, &errOut); code != exitIncomplete {
		t.Fatalf("blocked scan exit %d", code)
	}
}

func TestScanAndInspectFlagsAreRefusedBeforeABrowserStarts(t *testing.T) {
	t.Chdir(t.TempDir())
	if err := os.WriteFile("state.json", []byte("{}"), 0o600); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		args []string
		want string
	}{
		{[]string{"scan", "https://example.com/", "--storage-state", "state.json"}, "requires --acknowledge-private-content"},
		{[]string{"inspect", "https://example.com/", "--acknowledge-private-content"}, "only with --storage-state"},
		{[]string{"inspect", "https://example.com/", "--viewport", "390"}, "WIDTHxHEIGHT"},
		{[]string{"scan", "https://example.com/", "--viewport", "390x100"}, "viewport width"},
		{[]string{"scan", "https://example.com/", "--weight-budget", "total=1"}, "NAME=VALUE"},
		{[]string{"scan", "https://example.com/", "--weight-budget", "total_bytes=0"}, "weight_budget"},
		{[]string{"scan", "https://example.com/", "--fail-on-new"}, "needs --baseline"},
	} {
		var out, errOut bytes.Buffer
		if code := run(tc.args, &out, &errOut); code != exitUsage || !strings.Contains(errOut.String(), tc.want) {
			t.Fatalf("%v: exit %d, %q", tc.args, code, errOut.String())
		}
	}
}

func TestInspectLoadsAStorageStateForAnAuthenticatedPage(t *testing.T) {
	root := browserProject(t)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		title := "Sign in"
		if cookie, err := r.Cookie("session"); err == nil && cookie.Value == "granted" {
			title = "Account"
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write([]byte(`<!doctype html><html lang="en"><head><title>` + title + `</title></head><body><h1>` + title + `</h1></body></html>`))
	}))
	t.Cleanup(server.Close)
	host := strings.TrimPrefix(server.URL, "http://")
	host = host[:strings.LastIndex(host, ":")]
	state := `{"cookies":[{"name":"session","value":"granted","domain":"` + host + `","path":"/","expires":-1,"httpOnly":true,"secure":false,"sameSite":"Lax"}],"origins":[]}`
	t.Chdir(root)
	if err := os.WriteFile("state.json", []byte(state), 0o600); err != nil {
		t.Fatal(err)
	}
	var out, errOut bytes.Buffer
	if code := run([]string{"inspect", server.URL + "/", "--allow-private-network", "--format", "json", "--viewport", "390x844"}, &out, &errOut); code != exitPassed || !strings.Contains(out.String(), `"title": "Sign in"`) {
		t.Fatalf("anonymous inspect exit %d: %s %s", code, out.String(), errOut.String())
	}
	out.Reset()
	if code := run([]string{"inspect", server.URL + "/", "--allow-private-network", "--format", "json", "--storage-state", "state.json", "--acknowledge-private-content"}, &out, &errOut); code != exitPassed || !strings.Contains(out.String(), `"title": "Account"`) {
		t.Fatalf("authenticated inspect exit %d: %s %s", code, out.String(), errOut.String())
	}
	if strings.Contains(out.String(), "granted") {
		t.Fatal("the storage-state credential reached the report")
	}
}

func TestScanNamesTheInstallCommandForAMissingBrowser(t *testing.T) {
	root := browserProject(t)
	t.Setenv("PLAYWRIGHT_BROWSERS_PATH", t.TempDir())
	t.Chdir(root)
	var out, errOut bytes.Buffer
	code := run([]string{"inspect", browserServer(t), "--allow-private-network"}, &out, &errOut)
	if code != exitIncomplete || !strings.Contains(errOut.String(), "npx playwright install chromium") || strings.Contains(errOut.String(), "╔") {
		t.Fatalf("exit %d: %q", code, errOut.String())
	}
}
