package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
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
	report := func(protocol string) (int, string, struct {
		Ready  bool
		Checks []struct{ Name, Status, Message, Fix string }
	}) {
		t.Helper()
		manifest, _ := json.Marshal(map[string]any{"name": "@9l/playwright", "version": "0.0.1", "main": "index.js",
			"9l": map[string]any{"protocol": protocol, "capabilities": playwrightsdk.Capabilities, "faults": prove.Kinds}})
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
	code, _, foreign := report("9l.engine/0")
	if sdk := find(foreign.Checks, "sdk"); code != 2 || foreign.Ready || sdk.Status != "missing" || !strings.Contains(sdk.Message, "Incompatible") {
		t.Fatalf("foreign protocol: exit %d %+v", code, sdk)
	}
}

// Doctor compares the 9l first on PATH with its own executable without
// running either, and names the two paths only under --paths.
func TestDoctorWarnsAboutAnotherNineLOnPathWithoutRunningIt(t *testing.T) {
	if _, err := exec.LookPath("node"); err != nil {
		t.Skip("Node not installed")
	}
	bin := t.TempDir()
	marker := filepath.Join(bin, "fixture-ran-marker")
	name, script := "9l", "#!/bin/sh\necho ran > \""+marker+"\"\n"
	if runtime.GOOS == "windows" {
		name, script = "9l.cmd", "@echo ran > \""+marker+"\"\r\n"
	}
	if err := os.WriteFile(filepath.Join(bin, name), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Chdir(t.TempDir())
	find := func(args ...string) (string, struct{ Name, Status, Message, Fix string }) {
		t.Helper()
		var out, stderr bytes.Buffer
		run(append([]string{"doctor"}, args...), &out, &stderr)
		var report struct {
			Checks []struct{ Name, Status, Message, Fix string }
		}
		if err := json.Unmarshal(out.Bytes(), &report); err != nil {
			t.Fatalf("%s %s", out.String(), stderr.String())
		}
		for _, check := range report.Checks {
			if check.Name == "executable" {
				return out.String(), check
			}
		}
		t.Fatalf("no executable check: %s", out.String())
		return "", struct{ Name, Status, Message, Fix string }{}
	}
	raw, check := find("--format", "json")
	if check.Status != "warning" || !strings.Contains(check.Message, "is not this executable") || !strings.Contains(check.Fix, "full path") {
		t.Fatalf("stale PATH: %+v", check)
	}
	if strings.Contains(raw, bin) || strings.Contains(check.Message, "This executable:") {
		t.Fatal("doctor printed executable paths without --paths")
	}
	var text, stderr bytes.Buffer
	run([]string{"doctor"}, &text, &stderr)
	if !strings.Contains(text.String(), "executable [warning]") || strings.Contains(text.String(), bin) {
		t.Fatalf("text report: %s", text.String())
	}
	other, err := filepath.EvalSymlinks(filepath.Join(bin, name))
	if err != nil {
		t.Fatal(err)
	}
	// Windows may hand the test an 8.3 short name for the temp directory
	// while doctor reports the long one, so there the fake binary is matched
	// by its file name at the end of the reported path.
	raw, check = find("--format", "json", "--paths")
	if !strings.Contains(check.Message, "First on PATH: ") || !strings.Contains(check.Message, string(filepath.Separator)+name+".") || (runtime.GOOS != "windows" && !strings.Contains(raw, other)) {
		t.Fatalf("--paths omits the PATH binary: %+v", check)
	}
	if _, err := os.Stat(marker); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("doctor ran the other 9l")
	}
}

// A spec is read as text: taking test from @playwright/test is a missing
// prerequisite for prove, so the report is not ready and the command exits 2.
func TestDoctorSpecImportingPlaywrightTestExitsTwo(t *testing.T) {
	if _, err := exec.LookPath("node"); err != nil {
		t.Skip("Node not installed")
	}
	root := t.TempDir()
	files := map[string]string{
		"package.json":          "{}",
		"playwright.config.ts":  "throw new Error(\"fixture-config-must-not-run\");\nexport default { testDir: './tests' };\n",
		"tests/fixture.spec.ts": "throw new Error(\"fixture-spec-must-not-run\");\nimport { test } from '@playwright/test';\ntest('x', async ({ page }) => { await page.goto('/'); });\n",
	}
	for name, content := range files {
		if err := os.MkdirAll(filepath.Dir(filepath.Join(root, name)), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(root, name), []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	t.Chdir(root)
	var out, stderr bytes.Buffer
	code := run([]string{"doctor", "--sdk", "tests/fixture.spec.ts", "--format", "json"}, &out, &stderr)
	var report struct {
		Ready      bool
		Checks     []struct{ Name, Status, Message, Fix string }
		Unverified []string
	}
	if err := json.Unmarshal(out.Bytes(), &report); err != nil || code != 2 || report.Ready {
		t.Fatalf("exit %d: %s %s", code, out.String(), stderr.String())
	}
	found := map[string]struct{ Name, Status, Message, Fix string }{}
	for _, check := range report.Checks {
		found[check.Name] = check
	}
	if check := found["spec/import"]; check.Status != "missing" || !strings.Contains(check.Fix, "import test from @9l/playwright") {
		t.Fatalf("spec/import: %+v", check)
	}
	if check := found["spec/fixtures"]; check.Status != "ok" {
		t.Fatalf("spec/fixtures: %+v", check)
	}
	if check := found["spec/config"]; check.Status != "ok" || !strings.Contains(check.Message, "config found: playwright.config.ts") {
		t.Fatalf("spec/config: %+v", check)
	}
	if !slices.Contains(report.Unverified, "testDir and testMatch unless literal") {
		t.Fatalf("unverified: %v", report.Unverified)
	}
	if strings.Contains(out.String(), "fixture-") || strings.Contains(out.String(), root) {
		t.Fatal("doctor executed or disclosed the fixture project")
	}
	for _, args := range [][]string{{"doctor", "tests/fixture.spec.ts"}, {"doctor", "--sdk", "tests/absent.spec.ts"}, {"doctor", "--sdk", "tests"}} {
		out.Reset()
		stderr.Reset()
		if code := run(args, &out, &stderr); code != 2 || out.Len() != 0 || !strings.Contains(stderr.String(), "9l: doctor") {
			t.Fatalf("%v: exit %d %s %s", args, code, out.String(), stderr.String())
		}
	}
}

// pathWithOnlyNode returns a PATH holding just a link to Node, so a test can
// control which 9l, if any, PATH finds.
func pathWithOnlyNode(t *testing.T) string {
	t.Helper()
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("Node not installed")
	}
	dir := t.TempDir()
	if err := os.Symlink(node, filepath.Join(dir, filepath.Base(node))); err != nil {
		t.Skipf("cannot link Node: %v", err)
	}
	return dir
}

type doctorCheck struct{ Name, Status, Message, Fix string }

func doctorChecks(t *testing.T, args ...string) (int, string, map[string]doctorCheck) {
	t.Helper()
	var out, stderr bytes.Buffer
	code := run(append([]string{"doctor", "--format", "json"}, args...), &out, &stderr)
	var report struct{ Checks []doctorCheck }
	if err := json.Unmarshal(out.Bytes(), &report); err != nil {
		t.Fatalf("exit %d: %s %s", code, out.String(), stderr.String())
	}
	found := map[string]doctorCheck{}
	for _, check := range report.Checks {
		found[check.Name] = check
	}
	return code, out.String(), found
}

// A 9l missing from PATH is a warning: a host configured with plain 9l
// cannot start the executable doctor runs from.
func TestDoctorWarnsWhenNoNineLIsOnPath(t *testing.T) {
	t.Setenv("PATH", pathWithOnlyNode(t))
	t.Chdir(t.TempDir())
	if facts := executableFacts(false); facts["otherOnPath"] != false || facts["self"] != nil {
		t.Fatalf("facts: %v", facts)
	}
	_, _, found := doctorChecks(t)
	if check := found["executable"]; check.Status != "warning" || !strings.Contains(check.Message, "no 9l on PATH") || !strings.Contains(check.Fix, "full path") {
		t.Fatalf("executable: %+v", check)
	}
}

// A relative PATH entry resolves against the current directory, so doctor
// reports the absolute file it found and warns that hosts started elsewhere
// find another 9l or none.
func TestDoctorReportsARelativePathEntry(t *testing.T) {
	nodeDir := pathWithOnlyNode(t)
	work := t.TempDir()
	name, script := "9l", "#!/bin/sh\nexit 0\n"
	if runtime.GOOS == "windows" {
		name, script = "9l.cmd", "@exit /b 0\r\n"
	}
	if err := os.MkdirAll(filepath.Join(work, "rel"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(work, "rel", name), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", "rel"+string(os.PathListSeparator)+nodeDir)
	t.Chdir(work)
	facts := executableFacts(true)
	other, _ := facts["other"].(string)
	if facts["relative"] != true || facts["otherOnPath"] != true || !filepath.IsAbs(other) || filepath.Base(other) != name {
		t.Fatalf("facts: %v", facts)
	}
	_, raw, found := doctorChecks(t)
	if check := found["executable"]; check.Status != "warning" || !strings.Contains(check.Message, "relative PATH entry") || strings.Contains(check.Message, "First on PATH") {
		t.Fatalf("executable: %+v", check)
	}
	if strings.Contains(raw, work) {
		t.Fatal("doctor printed paths without --paths")
	}
}

func writeFiles(t *testing.T, root string, files map[string]string) {
	t.Helper()
	for name, content := range files {
		if err := os.MkdirAll(filepath.Dir(filepath.Join(root, name)), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(root, name), []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
	}
}

// With a spec, the workspace and package checks describe the project prove
// would use for it, found by walking up from the spec, not the directory
// doctor runs in. Without a spec they still describe the current directory.
func TestDoctorJudgesTheSpecProjectNotTheCurrentDirectory(t *testing.T) {
	if _, err := exec.LookPath("node"); err != nil {
		t.Skip("Node not installed")
	}
	project := t.TempDir()
	writeFiles(t, project, map[string]string{
		"package.json": `{"devDependencies":{"@playwright/test":"1.64.0","@9l/playwright":"0.5.0"}}`,
		"node_modules/@playwright/test/package.json": `{"name":"@playwright/test","version":"1.64.0"}`,
		"node_modules/@9l/playwright/package.json":   `{"name":"@9l/playwright","version":"0.5.0"}`,
		"node_modules/@playwright/test/index.js":     "throw new Error('fixture-entry-must-not-run');\n",
		"node_modules/@9l/playwright/index.js":       "throw new Error('fixture-entry-must-not-run');\n",
		"tests/a.spec.ts":                            "import { test } from './fixtures';\nimport { test as pwTest } from '@playwright/test';\ntest('x', async ({ page }, testInfo) => {});\n",
	})
	t.Chdir(t.TempDir())
	_, raw, found := doctorChecks(t, "--sdk", filepath.Join(project, "tests", "a.spec.ts"))
	for _, name := range []string{"workspace", "playwright-test"} {
		if found[name].Status != "ok" {
			t.Fatalf("%s: %+v", name, found[name])
		}
	}
	if found["sdk"].Status == "missing" {
		t.Fatalf("sdk: %+v", found["sdk"])
	}
	// A test from a local fixtures module is not followed: a warning, never
	// missing, even beside an aliased @playwright/test import.
	if check := found["spec/import"]; check.Status != "warning" || !strings.Contains(check.Message, "another module") {
		t.Fatalf("spec/import: %+v", check)
	}
	if check := found["spec/fixtures"]; check.Status != "ok" {
		t.Fatalf("spec/fixtures: %+v", check)
	}
	if strings.Contains(raw, project) {
		t.Fatal("doctor printed the project path without --paths")
	}
	_, _, cwd := doctorChecks(t)
	if cwd["workspace"].Status != "missing" || !strings.Contains(cwd["workspace"].Message, "current directory") {
		t.Fatalf("without a spec: %+v", cwd["workspace"])
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
