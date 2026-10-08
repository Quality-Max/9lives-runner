package tier2

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Quality-Max/9lives-runner/internal/healing"
)

func TestHealGatesEveryCandidateVerification(t *testing.T) {
	for _, stage := range []string{"tier1", "tier2"} {
		for _, tc := range []struct {
			name     string
			verified RunResult
			state    string
		}{
			{"assertion", RunResult{ExecutedTests: 1, Failure: "AssertionError: expected result"}, "needs_human"},
			{"network", RunResult{ExecutedTests: 1, Failure: "network request failed waiting for locator('#old')"}, "unverified"},
			{"syntax", RunResult{ExecutedTests: 1, Failure: "syntax error waiting for locator('#old')"}, "unverified"},
			{"navigation", RunResult{ExecutedTests: 1, Failure: "navigation failed waiting for locator('#old')"}, "unverified"},
			{"unknown", RunResult{ExecutedTests: 1, Failure: "runner failed"}, "unverified"},
			{"zero tests", RunResult{Failure: "waiting for locator('#old')"}, "unverified"},
			{"different count", RunResult{ExecutedTests: 2, Failure: "waiting for locator('#old')"}, "unverified"},
		} {
			t.Run(stage+"/"+tc.name, func(t *testing.T) {
				spec := filepath.Join(t.TempDir(), "login.spec.ts")
				original := "test('login', async ({ page }) => {\n  await page.locator('[data-testid=\"old\"]').click();\n  await expect(page.locator('#result')).toBeVisible();\n});\n"
				if err := os.WriteFile(spec, []byte(original), 0600); err != nil {
					t.Fatal(err)
				}
				candidate := strings.Replace(original, "[data-testid=\"old\"]", "#new", 1)
				provider := &fakeProvider{responses: []string{"```ts\n" + candidate + "```", "unexpected"}}
				var labels []string
				result, err := Heal(context.Background(), SessionOptions{Spec: spec, Framework: "playwright", Provider: provider, Apply: true, MaxProposals: 1, Run: func(_ context.Context, _ string, label string) RunResult {
					labels = append(labels, label)
					if label == "original" {
						return RunResult{ExecutedTests: 1, Failure: "waiting for locator('[data-testid=\"old\"]')"}
					}
					return tc.verified
				}}, func(source, failure string) (string, bool) {
					if stage != "tier1" {
						return "", false
					}
					p := healing.Heal(healing.Request{Version: healing.Version, Framework: "playwright", TestCode: source, ErrorMessage: failure})
					return p.ProposedCode, p.Decision == "propose"
				})
				expectedCalls := 0
				expectedLabel := "tier1"
				if stage == "tier2" {
					expectedCalls = 1
					expectedLabel = "tier2-1"
				}
				if err != nil || len(labels) != 2 || labels[1] != expectedLabel || provider.calls != expectedCalls || result.Applied || result.SavedPath != "" || result.State != tc.state {
					t.Fatalf("verification bypassed: result=%+v labels=%v calls=%d err=%v", result, labels, provider.calls, err)
				}
				source, err := os.ReadFile(spec)
				if err != nil || string(source) != original {
					t.Fatalf("original changed: err=%v", err)
				}
				if _, err := os.Stat(spec + ".healed"); !os.IsNotExist(err) {
					t.Fatalf("unverified candidate saved: err=%v", err)
				}
			})
		}
	}
}

func TestHealEscalatesEditableTier1Failure(t *testing.T) {
	spec := filepath.Join(t.TempDir(), "login.spec.ts")
	original := "test('login', async ({ page }) => {\n  await page.locator('#old').click();\n});\n"
	if err := os.WriteFile(spec, []byte(original), 0600); err != nil {
		t.Fatal(err)
	}
	candidate := strings.Replace(original, "#old", "#new", 1)
	provider := &fakeProvider{responses: []string{"```ts\n" + candidate + "```"}}
	var labels []string
	result, err := Heal(context.Background(), SessionOptions{Spec: spec, Framework: "playwright", Provider: provider, Apply: true, Run: func(_ context.Context, _ string, label string) RunResult {
		labels = append(labels, label)
		if label == "original" || label == "tier1" {
			return RunResult{ExecutedTests: 1, Failure: "waiting for locator('#old')"}
		}
		return RunResult{Passed: true, ExecutedTests: 1}
	}}, func(source, _ string) (string, bool) { return strings.Replace(source, "#old", "#tier1", 1), true })
	if err != nil || len(labels) != 3 || labels[1] != "tier1" || provider.calls != 1 || !result.Applied || result.State != "applied" {
		t.Fatalf("editable Tier1 did not escalate: result=%+v labels=%v calls=%d err=%v", result, labels, provider.calls, err)
	}
	source, err := os.ReadFile(spec)
	if err != nil || string(source) != candidate {
		t.Fatalf("wrong candidate applied: err=%v", err)
	}
}
