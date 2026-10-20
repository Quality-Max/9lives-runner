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
	"testing"
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
