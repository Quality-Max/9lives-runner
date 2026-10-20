package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const missingBrowserSpec = `import {test, expect} from '@playwright/test';
test('browser is available', async ({page}) => {
  await page.setContent('<button>Pay</button>');
  await expect(page.getByRole('button', {name: 'Pay'})).toBeVisible();
});
`

func TestMissingBrowserCLIAndMCPOutcomes(t *testing.T) {
	root := browserProject(t)
	writeTestFile(t, filepath.Join(root, "package.json"), `{"devDependencies":{"@playwright/test":"1.61.1"}}`)
	writeTestFile(t, filepath.Join(root, "browser.spec.ts"), missingBrowserSpec)
	writeTestFile(t, filepath.Join(root, "persistent.spec.ts"), `import {test, expect, chromium} from '@playwright/test';
test('persistent browser is available', async () => {
  const context = await chromium.launchPersistentContext('');
  try {
    const page = await context.newPage();
    await page.setContent('<button>Pay</button>');
    await expect(page.getByRole('button', {name: 'Pay'})).toBeVisible();
  } finally {
    await context.close();
  }
});
`)
	writeTestFile(t, filepath.Join(root, "global-setup.cjs"), `const {chromium} = require('@playwright/test'); module.exports = async () => {const browser = await chromium.launch(); await browser.close();};`)
	writeTestFile(t, filepath.Join(root, "assertion.spec.ts"), `import {test, expect} from '@playwright/test'; test('assertion', () => {expect(1).toBe(2)});`)
	t.Setenv("PLAYWRIGHT_BROWSERS_PATH", t.TempDir())
	t.Chdir(root)
	h := startMCP(t, root, func(s *mcpServer) { s.provider = scriptedProvider{"must not be called"} })
	for _, tc := range []struct {
		spec, outcome string
		code          int
		globalSetup   bool
	}{
		{"browser.spec.ts", "incomplete", exitIncomplete, false},
		{"persistent.spec.ts", "incomplete", exitIncomplete, false},
		{"browser.spec.ts", "incomplete", exitIncomplete, true},
		{"assertion.spec.ts", "failed", exitFailed, false},
	} {
		setup := ""
		if tc.globalSetup {
			setup = "globalSetup: './global-setup.cjs',"
		}
		writeTestFile(t, filepath.Join(root, "playwright.config.cjs"), "module.exports = {"+setup+" retries: 0, workers: 1, use: {browserName: 'chromium'}};")
		var out, errOut bytes.Buffer
		code := run([]string{"run", tc.spec, "--format", "json", "--timeout", "20s", "--attempts", "1"}, &out, &errOut)
		var result map[string]any
		if err := json.Unmarshal(out.Bytes(), &result); err != nil || code != tc.code || result["outcome"] != tc.outcome {
			t.Fatalf("%s: exit %d, outcome %v, decode %v", tc.spec, code, result["outcome"], err)
		}
		payload, isError := h.toolCall(t, 1, "run_test", map[string]any{"spec": tc.spec})
		if isError || payload["status"] != tc.outcome {
			t.Fatalf("run_test %s (global setup %v) returned %v", tc.spec, tc.globalSetup, payload)
		}
		if tc.outcome == "incomplete" {
			reason, _ := payload["reason"].(string)
			for _, diagnostic := range []string{out.String(), reason} {
				if !strings.Contains(diagnostic, "npx playwright install chromium") || !strings.Contains(diagnostic, "is not installed") || strings.ContainsAny(diagnostic, "╔║") {
					t.Fatalf("%s (global setup %v) lost safe repair guidance", tc.spec, tc.globalSetup)
				}
			}
		}
	}
	payload, _ := h.toolCall(t, 2, "heal_test", map[string]any{"spec": "browser.spec.ts"})
	if payload["state"] != "unverified" || payload["providerCalls"] != float64(0) || payload["applied"] != false || payload["savedPath"] != nil {
		t.Fatalf("heal_test attempted to repair setup: %v", payload)
	}
	if source, err := os.ReadFile(filepath.Join(root, "browser.spec.ts")); err != nil || string(source) != missingBrowserSpec {
		t.Fatal("healing changed the spec")
	}
}

func TestMCPMissingBrowserConfirmationIsInconclusive(t *testing.T) {
	// Use the installed runtime, not the fake assertion events used by the
	// other confirmation tests: a launch failure must reach no assertion.
	project := browserProject(t)
	modules, err := os.Readlink(filepath.Join(project, "node_modules"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(modules, "@9l", "playwright", "dist", "reporter.js")); err != nil {
		if os.Getenv("NINELIVES_REQUIRE_BROWSER") == "1" {
			t.Fatal("NINELIVES_REQUIRE_BROWSER=1 but @9l/playwright is not built")
		}
		t.Skip("build @9l/playwright to exercise real confirmation")
	}
	root, spec, _ := confirmRepository(t)
	if err := os.RemoveAll(filepath.Join(root, "node_modules")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(modules, filepath.Join(root, "node_modules")); err != nil {
		t.Skip("symlinks unavailable")
	}
	writeTestFile(t, spec, missingBrowserSpec)
	t.Setenv("PLAYWRIGHT_BROWSERS_PATH", t.TempDir())
	h := startMCP(t, root)
	payload, isError := h.toolCall(t, 1, "confirm_finding", map[string]any{"spec": "tests/repro.spec.ts", "unfixed": "HEAD~1", "finding_id": "browser-setup"})
	report, _ := payload["report"].(map[string]any)
	if isError || report["verdict"] != "inconclusive" || report["inconclusiveReason"] != "failed-without-assertion" {
		t.Fatalf("confirmation claimed evidence from a missing browser: %v", payload)
	}
}
