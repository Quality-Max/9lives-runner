package main

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/Quality-Max/9lives-runner/internal/adapters/playwrightsdk"
	"github.com/Quality-Max/9lives-runner/internal/prove"
)

func TestDoctorEmptyProjectAndMissingNode(t *testing.T) {
	if _, err := exec.LookPath("node"); err != nil {
		t.Skip("Node not installed")
	}
	t.Chdir(t.TempDir())
	var out, stderr bytes.Buffer
	if code := run([]string{"doctor", "--sdk", "--format", "json"}, &out, &stderr); code != 2 {
		t.Fatalf("doctor exit %d: %s", code, stderr.String())
	}
	if !strings.Contains(out.String(), "npm install --save-dev @playwright/test@1.64.0") || !strings.Contains(out.String(), `"ready":false`) {
		t.Fatalf("missing repair: %s", out.String())
	}
	files, err := os.ReadDir(".")
	if err != nil || len(files) != 0 {
		t.Fatal("doctor wrote project files")
	}
	t.Setenv("PATH", t.TempDir())
	out.Reset()
	stderr.Reset()
	if code := run([]string{"doctor"}, &out, &stderr); code != 2 || !strings.Contains(stderr.String(), "Install Node 24 LTS") {
		t.Fatalf("missing Node: exit %d %s", code, stderr.String())
	}
}

func TestDoctorFindsInstalledAndMissingChromium(t *testing.T) {
	root := browserProject(t)
	if err := os.WriteFile(filepath.Join(root, "package.json"), []byte(`{"private":true}`), 0600); err != nil {
		t.Fatal(err)
	}
	t.Chdir(root)
	var out, stderr bytes.Buffer
	if code := run([]string{"doctor", "--format", "json"}, &out, &stderr); code != 0 {
		t.Fatalf("installed: exit %d %s %s", code, out.String(), stderr.String())
	}
	var report struct {
		Ready  bool
		Checks []struct{ Name, Status, Fix string }
	}
	if err := json.Unmarshal(out.Bytes(), &report); err != nil || !report.Ready {
		t.Fatal("invalid installed report")
	}
	found := 0
	for _, check := range report.Checks {
		if strings.HasSuffix(check.Name, "/chromium-headless-shell") && check.Status == "ok" {
			found++
		}
	}
	if found != 2 {
		t.Fatalf("run and scan headless checks: %d", found)
	}
	t.Setenv("PLAYWRIGHT_BROWSERS_PATH", t.TempDir())
	out.Reset()
	stderr.Reset()
	if code := run([]string{"doctor", "--format", "json"}, &out, &stderr); code != 2 {
		t.Fatalf("empty cache: exit %d", code)
	}
	if err := json.Unmarshal(out.Bytes(), &report); err != nil || report.Ready {
		t.Fatal("missing browser reported ready")
	}
	for _, check := range report.Checks {
		if strings.HasSuffix(check.Name, "/chromium-headless-shell") && (check.Status != "missing" || !strings.Contains(check.Fix, "playwright@")) {
			t.Fatal("missing exact build repair")
		}
	}
}

func TestUnreachableBrowserTargetHasSharedRemediation(t *testing.T) {
	root := browserProject(t)
	server := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	target := server.URL + "/fixture-query-canary?private=fixture-url-canary"
	server.Close()
	t.Chdir(root)
	var out, stderr bytes.Buffer
	if code := run([]string{"scan", target, "--allow-private-network", "--checks", "console", "--format", "json"}, &out, &stderr); code == 0 {
		t.Fatal("unreachable scan passed")
	}
	if !strings.Contains(stderr.String(), "Start the application") || strings.Contains(stderr.String()+out.String(), "fixture-url-canary") {
		t.Fatalf("unsafe or unactionable CLI error: %s %s", out.String(), stderr.String())
	}
	h := startMCP(t, root)
	payload, isError := h.toolCall(t, 1, "inspect_page", map[string]any{"url": target, "allow_private_network": true})
	message, _ := payload["error"].(string)
	if !isError || !strings.Contains(message, "Start the application") || strings.Contains(message, "fixture-url-canary") {
		t.Fatalf("MCP error %v", payload)
	}
}

func TestVersionAdvertisesSetupAndBrowserCommands(t *testing.T) {
	for _, name := range []string{"doctor", "scan", "inspect"} {
		found := false
		for _, command := range versionInfo().Commands {
			found = found || command == name
		}
		if !found {
			t.Fatalf("version omits %s", name)
		}
	}
}

// The CLI tells the helper what it admits, so an installed SDK is judged by
// the protocol it declares; a different protocol is a missing prerequisite.
func TestDoctorJudgesTheSDKPairByDeclaredProtocol(t *testing.T) {
	if _, err := exec.LookPath("node"); err != nil {
		t.Skip("Node not installed")
	}
	root := t.TempDir()
	pkg := filepath.Join(root, "node_modules", "@9l", "playwright")
	if err := os.MkdirAll(pkg, 0o755); err != nil {
		t.Fatal(err)
	}
	for name, content := range map[string]string{"package.json": "{}", "node_modules/@9l/playwright/index.js": `throw new Error("fixture-sdk-entry-must-not-run")`} {
		if err := os.WriteFile(filepath.Join(root, name), []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	t.Chdir(root)
	report := func(protocol string, faults ...string) (int, string, struct {
		Ready  bool
		Checks []struct{ Name, Status, Message, Fix string }
	}) {
		t.Helper()
		if faults == nil {
			faults = prove.Kinds
		}
		manifest, _ := json.Marshal(map[string]any{"name": "@9l/playwright", "version": "0.0.1", "main": "index.js",
			"9l": map[string]any{"protocol": protocol, "capabilities": playwrightsdk.Capabilities, "faults": faults}})
		if err := os.WriteFile(filepath.Join(pkg, "package.json"), manifest, 0o600); err != nil {
			t.Fatal(err)
		}
		var out, stderr bytes.Buffer
		code := run([]string{"doctor", "--sdk", "--format", "json"}, &out, &stderr)
		var parsed struct {
			Ready  bool
			Checks []struct{ Name, Status, Message, Fix string }
		}
		if err := json.Unmarshal(out.Bytes(), &parsed); err != nil {
			t.Fatalf("exit %d: %s %s", code, out.String(), stderr.String())
		}
		return code, out.String(), parsed
	}
	find := func(checks []struct{ Name, Status, Message, Fix string }, name string) struct{ Name, Status, Message, Fix string } {
		for _, check := range checks {
			if check.Name == name {
				return check
			}
		}
		t.Fatalf("no %s check", name)
		return struct{ Name, Status, Message, Fix string }{}
	}
	_, raw, admitted := report(playwrightsdk.Version)
	if sdk := find(admitted.Checks, "sdk"); sdk.Status != "warning" || !strings.Contains(sdk.Message, "Admitted: protocol "+playwrightsdk.Version) || !strings.Contains(sdk.Fix, "@9l/playwright@"+version) {
		t.Fatalf("admitted pair: %+v", sdk)
	}
	if faults := find(admitted.Checks, "sdk/faults"); faults.Status != "ok" || !strings.Contains(faults.Message, strings.Join(prove.Kinds, ", ")) {
		t.Fatalf("fault kinds: %+v", faults)
	}
	if strings.Contains(raw, "fixture-") || strings.Contains(raw, "node_modules") {
		t.Fatal("doctor disclosed fixture paths or ran the SDK entry")
	}
	// An SDK that predates redirect-cross-origin and delay is admitted, and the
	// kinds it cannot apply are named before any prove run.
	_, _, older := report(playwrightsdk.Version, "abort", "http-500", "empty-json", "http-401", "http-403", "http-429", "malformed-json")
	if faults := find(older.Checks, "sdk/faults"); faults.Status != "warning" || !strings.Contains(faults.Message, "Not applied by this SDK: redirect-cross-origin, delay.") {
		t.Fatalf("older fault kinds: %+v", faults)
	}
	code, _, foreign := report("9l.engine/0")
	if sdk := find(foreign.Checks, "sdk"); code != 2 || foreign.Ready || sdk.Status != "missing" || !strings.Contains(sdk.Message, "Incompatible") {
		t.Fatalf("foreign protocol: exit %d %+v", code, sdk)
	}
}

// The SDK's package metadata is what doctor compares against this engine,
// so it must declare exactly what the Go side admits and can request.
func TestSDKPackageDeclaresTheEngineContract(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join("..", "..", "packages", "playwright", "package.json"))
	if err != nil {
		t.Fatal(err)
	}
	var manifest struct {
		Engine struct {
			Protocol     string   `json:"protocol"`
			Capabilities []string `json:"capabilities"`
			Faults       []string `json:"faults"`
		} `json:"9l"`
	}
	if err := json.Unmarshal(raw, &manifest); err != nil {
		t.Fatal(err)
	}
	if manifest.Engine.Protocol != playwrightsdk.Version || !slices.Equal(manifest.Engine.Capabilities, playwrightsdk.Capabilities) || !slices.Equal(manifest.Engine.Faults, prove.Kinds) {
		t.Fatalf("packages/playwright/package.json declares %+v; engine admits %s %v and requests %v", manifest.Engine, playwrightsdk.Version, playwrightsdk.Capabilities, prove.Kinds)
	}
}
