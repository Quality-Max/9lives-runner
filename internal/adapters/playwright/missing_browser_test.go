package playwright

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestMissingBrowserIsNotAValidatedTestFailure(t *testing.T) {
	launch := "Error: browserType.launch: Executable doesn't exist at /cache/chromium_headless_shell-1248/chrome\n╔══╗\n║ npx playwright install ║"
	for _, tc := range []struct {
		name, outcome string
		results       []testResult
		incomplete    bool
		failures      int
	}{
		{"launch error", "unexpected", []testResult{{Status: "failed", Error: reportError{launch}}}, true, 0},
		{"errors array", "unexpected", []testResult{{Status: "failed", Errors: []reportError{{launch}}}}, true, 0},
		{"expected failure cannot hide setup", "expected", []testResult{{Status: "failed", Error: reportError{launch}}}, true, 0},
		{"interrupted retry cannot hide setup", "unexpected", []testResult{{Status: "failed", Error: reportError{launch}}, {Status: "interrupted"}}, true, 0},
		{"recovered retry", "flaky", []testResult{{Status: "failed", Error: reportError{launch}}, {Status: "passed"}}, false, 0},
		{"later assertion supersedes setup", "unexpected", []testResult{{Status: "failed", Error: reportError{launch}}, {Status: "failed", Error: reportError{"Error: expect(received).toBe(expected)"}}}, false, 1},
		{"assertion quoting launch error", "unexpected", []testResult{{Status: "failed", Error: reportError{"Error: expect(received).toBe(expected)\nReceived: " + launch}}}, false, 1},
		{"custom executable", "unexpected", []testResult{{Status: "failed", Error: reportError{"Error: browserType.launch: Executable doesn't exist at /my/custom-browser"}}}, false, 1},
		{"ordinary timeout", "unexpected", []testResult{{Status: "timedOut", Error: reportError{"Test timeout exceeded"}}}, false, 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			raw, err := json.Marshal(map[string]any{
				"stats": map[string]any{"duration": 1},
				"suites": []any{map[string]any{"specs": []any{map[string]any{"tests": []any{
					map[string]any{"status": tc.outcome, "results": tc.results},
				}}}}},
			})
			if err != nil {
				t.Fatal(err)
			}
			validation, err := New().Validate(raw)
			if tc.incomplete {
				if err == nil || !strings.Contains(err.Error(), "chromium_headless_shell-1248") || !strings.Contains(err.Error(), "npx playwright install chromium") || strings.ContainsAny(err.Error(), "╔║") {
					t.Fatalf("missing browser must give bounded installation guidance: %v", err)
				}
				if validation.ExecutedTests != 0 || validation.FailureCount != 0 {
					t.Fatalf("setup failure claimed test execution: %+v", validation)
				}
			} else if err != nil || validation.ExecutedTests != 1 || validation.FailureCount != tc.failures {
				t.Fatalf("ordinary outcome changed: %+v, %v", validation, err)
			}
		})
	}
}

func TestMissingBrowserGuidanceNamesOnlyTheBuildAndInstallCommand(t *testing.T) {
	for _, tc := range []struct{ path, build, browser string }{
		{"/private/user/cache/chromium_headless_shell-1248/chrome", "chromium_headless_shell-1248", "chromium"},
		{`C:\Users\private\cache\chromium-1248\chrome.exe`, "chromium-1248", "chromium"},
		{"/private/cache/firefox-1500/firefox", "firefox-1500", "firefox"},
		{"/private/cache/webkit-2200/pw_run.sh", "webkit-2200", "webkit"},
	} {
		t.Run(tc.build, func(t *testing.T) {
			message := "Error: browserType.launch: Executable doesn't exist at " + tc.path + "\n╔══╗\n║ install browsers ║"
			raw, _ := json.Marshal(map[string]any{"suites": []any{map[string]any{"specs": []any{map[string]any{"tests": []any{map[string]any{
				"status": "unexpected", "results": []testResult{{Status: "failed", Error: reportError{message}, Errors: []reportError{{message}}}},
			}}}}}}})
			context := FailureContext(raw)
			if !strings.Contains(context, tc.build) || !strings.Contains(context, "npx playwright install "+tc.browser) || !strings.Contains(context, "npx playwright install --with-deps "+tc.browser) || strings.ContainsAny(context, "╔║") || strings.Contains(context, "private") || strings.Count(context, "is not installed") != 1 {
				t.Fatalf("unsafe or unhelpful diagnostic: %q", context)
			}
		})
	}
}
