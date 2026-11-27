package tier2

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

type fakeGrounded struct {
	candidate string
	calls     int
	got       RunResult
}

func (g *fakeGrounded) Name() string { return "fake-grounded" }
func (g *fakeGrounded) Propose(_ context.Context, _, _ string, original RunResult) (string, any, error) {
	g.calls++
	g.got = original
	return g.candidate, map[string]string{"status": "proposed"}, nil
}

const groundedSource = "import { test, expect } from '@playwright/test';\ntest('pay', async ({ page }) => {\n  await page.getByRole('button', { name: 'Checkout' }).click();\n  await expect(page.getByText('Payment form')).toBeVisible();\n});\n"
const groundedFailure = "TimeoutError: locator.click: Timeout 1000ms exceeded.\nCall log:\n  - waiting for getByRole('button', { name: 'Checkout' })\n"

func groundedSpec(t *testing.T) string {
	spec := filepath.Join(t.TempDir(), "pay.spec.ts")
	if err := os.WriteFile(spec, []byte(groundedSource), 0o600); err != nil {
		t.Fatal(err)
	}
	return spec
}

func TestGroundedCandidateIsVerifiedInIsolationBeforeSaving(t *testing.T) {
	spec := groundedSpec(t)
	candidate := strings.Replace(groundedSource, "'Checkout'", "'Continue to payment'", 1)
	grounded := &fakeGrounded{candidate: candidate}
	var labels []string
	run := func(_ context.Context, path, label string) RunResult {
		labels = append(labels, label)
		data, _ := os.ReadFile(path)
		if label != "original" && string(data) == candidate && path != spec {
			return RunResult{Passed: true, ExecutedTests: 1}
		}
		return RunResult{ExecutedTests: 1, Failure: groundedFailure, Snapshot: "- button \"Continue to payment\""}
	}
	result, err := Heal(context.Background(), SessionOptions{Spec: spec, Framework: "playwright", Run: run, Grounded: grounded}, func(string, string, string) (string, bool) { return "", false })
	if err != nil || result.State != "verified" || result.SavedPath != spec+".healed" || result.Grounded == nil || !result.Grounded.Passed || result.GroundedBy != "fake-grounded" || result.GroundedDecision == nil {
		t.Fatalf("result=%+v err=%v", result, err)
	}
	if strings.Join(labels, ",") != "original,grounded" || grounded.got.Failure != groundedFailure {
		t.Fatalf("runs %v", labels)
	}
	if source, _ := os.ReadFile(spec); string(source) != groundedSource {
		t.Fatal("source changed without approval")
	}
}

func TestGroundedProposalsKeepEveryBoundary(t *testing.T) {
	widened := strings.Replace(groundedSource, "await page.getByRole('button', { name: 'Checkout' }).click();", "await page.getByRole('link', { name: 'Checkout' }).click();", 1)
	assertionEdit := strings.Replace(groundedSource, "'Payment form'", "'Anything'", 1)
	renamed := strings.Replace(groundedSource, "'Checkout'", "'Back'", 1)
	for _, tc := range []struct {
		name, candidate, originalFailure string
		verify                           RunResult
		state                            string
		calls                            int
		runs                             string
	}{
		{"abstention changes nothing", "", groundedFailure, RunResult{}, "unverified", 1, "original"},
		{"a role change is refused before running", widened, groundedFailure, RunResult{}, "unverified", 1, "original"},
		{"an assertion edit is refused before running", assertionEdit, groundedFailure, RunResult{}, "unverified", 1, "original"},
		{"a wrong choice that reaches the assertion stays failing", renamed, groundedFailure, RunResult{ExecutedTests: 1, Failure: "Error: expect(locator).toBeVisible() failed"}, "needs_human", 1, "original,grounded"},
		{"zero tests never verify", renamed, groundedFailure, RunResult{Passed: true}, "unverified", 1, "original,grounded"},
		{"an assertion failure is never grounded", renamed, "Error: expect(received).toBe(expected)", RunResult{}, "needs_human", 0, "original"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			spec := groundedSpec(t)
			grounded := &fakeGrounded{candidate: tc.candidate}
			var runs []string
			run := func(_ context.Context, _ string, label string) RunResult {
				runs = append(runs, label)
				if label == "original" {
					return RunResult{ExecutedTests: 1, Failure: tc.originalFailure}
				}
				return tc.verify
			}
			result, err := Heal(context.Background(), SessionOptions{Spec: spec, Framework: "playwright", Run: run, Grounded: grounded}, func(string, string, string) (string, bool) { return "", false })
			if err != nil || result.State != tc.state || result.Applied || result.SavedPath != "" || grounded.calls != tc.calls || strings.Join(runs, ",") != tc.runs {
				t.Fatalf("state %s, calls %d, runs %v, err %v", result.State, grounded.calls, runs, err)
			}
			if source, _ := os.ReadFile(spec); string(source) != groundedSource {
				t.Fatal("source changed")
			}
		})
	}
}

func TestVerifiedTier1SkipsTheGroundedProposal(t *testing.T) {
	spec := groundedSpec(t)
	candidate := strings.Replace(groundedSource, "'Checkout'", "'Continue to payment'", 1)
	grounded := &fakeGrounded{candidate: candidate}
	run := func(_ context.Context, _ string, label string) RunResult {
		if label == "tier1" {
			return RunResult{Passed: true, ExecutedTests: 1}
		}
		return RunResult{ExecutedTests: 1, Failure: groundedFailure}
	}
	result, err := Heal(context.Background(), SessionOptions{Spec: spec, Framework: "playwright", Run: run, Grounded: grounded}, func(string, string, string) (string, bool) { return candidate, true })
	if err != nil || result.State != "verified" || grounded.calls != 0 || result.GroundedBy != "" {
		t.Fatalf("result=%+v calls=%d", result, grounded.calls)
	}
}
