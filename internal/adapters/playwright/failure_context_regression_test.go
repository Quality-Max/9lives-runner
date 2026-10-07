package playwright

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/qualitymax/9lives-runner/internal/healing"
)

func TestFailureContextPreservesLateUnsafeDiagnostics(t *testing.T) {
	first := "TimeoutError: locator.click: Timeout exceeded.\nCall log:\n  - waiting for locator('#old')\n" + strings.Repeat("  - element is not visible\n", 150)
	for _, tc := range []struct{ name, message, classification, marker string }{
		{"assertion", "Error: expect(received).toBe(expected)\nExpected: 1\nReceived: 2", "assertion_failed", ""},
		{"navigation", "page.goto: net::ERR_CONNECTION_REFUSED", "navigation_failed", ""},
		{"flow", "unexpected page after checkout", "flow_changed", ""},
		{"network", "network request failed", "", "network"},
		{"syntax", "SyntaxError: unexpected token", "", "syntax"},
	} {
		for _, layout := range []string{"separate tests", "same message"} {
			t.Run(tc.name+"/"+layout, func(t *testing.T) {
				messages := []string{first, tc.message}
				if layout == "same message" {
					messages = []string{first + tc.message}
				}
				var specs []any
				for _, message := range messages {
					specs = append(specs, map[string]any{"tests": []any{map[string]any{"results": []any{map[string]any{"errors": []any{map[string]any{"message": message}}}}}}})
				}
				raw, err := json.Marshal(map[string]any{"suites": []any{map[string]any{"specs": specs}}})
				if err != nil {
					t.Fatal(err)
				}
				diagnostic := FailureContext(raw)
				if len(diagnostic) > 3000 {
					t.Fatalf("unbounded diagnostics: %d bytes", len(diagnostic))
				}
				if tc.classification != "" && healing.Classify(diagnostic, "") != tc.classification {
					t.Fatalf("lost failure classification: got=%s want=%s", healing.Classify(diagnostic, ""), tc.classification)
				}
				if tc.marker != "" && !strings.Contains(strings.ToLower(diagnostic), tc.marker) {
					t.Fatalf("lost noneditable failure marker: %s", tc.marker)
				}
			})
		}
	}
}

func TestFailureContextDoesNotClassifySourceFramesAsFailures(t *testing.T) {
	message := "waiting for locator('#old')\n" + strings.Repeat("call log detail\n", 220) + "  12 | await expect(page.locator('#result')).toBeVisible();"
	raw, err := json.Marshal(map[string]any{"errors": []any{map[string]any{"message": message}}})
	if err != nil {
		t.Fatal(err)
	}
	if got := healing.Classify(FailureContext(raw), ""); got != "locator_not_found" {
		t.Fatalf("source frame classified as failure: %s", got)
	}
}
