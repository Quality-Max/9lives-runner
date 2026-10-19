package playwrightsdk

import (
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"

	"github.com/Quality-Max/9lives-runner/internal/runner"
)

func targetFrames() []map[string]any {
	f := passingFrames()
	f[0]["capabilities"] = []string{"steps", "artifact-metadata", "terminal-outcomes", TargetVersion}
	f[0]["targetVersion"] = TargetVersion
	f[2]["operation"] = "assert-text"
	f = append(f[:1], append([]map[string]any{{"type": "required_test", "testId": testKey}}, f[1:]...)...)
	for _, frame := range f {
		frame["targetId"] = TargetID("web:finance")
	}
	return f
}

func TestTargetEvidenceRequiresCompleteBoundManifest(t *testing.T) {
	identity := owner
	identity.Target = "web:finance"
	v, err := New().ValidateAttempt(stream(targetFrames()), identity)
	if err != nil || v.ExecutedTests != 1 || v.TargetAssertions != 1 || v.VerifiedAssertions != 0 {
		t.Fatalf("target assertion evidence: %+v %v", v, err)
	}
	if _, err := New().ValidateAttempt(stream(targetFrames()), owner); err == nil {
		t.Fatal("target evidence accepted by ordinary job")
	}
	cases := map[string]func([]map[string]any) []map[string]any{
		"missing manifest":      func(f []map[string]any) []map[string]any { return append(f[:1], f[2:]...) },
		"missing required test": func(f []map[string]any) []map[string]any { f[0]["totalTests"] = 2; return f },
		"foreign target": func(f []map[string]any) []map[string]any {
			f[3]["targetId"] = TargetID("android-fake:finance")
			return f
		},
		"stale attempt": func(f []map[string]any) []map[string]any { f[3]["attemptId"] = "stale"; return f },
		"foreign test":  func(f []map[string]any) []map[string]any { f[2]["testId"] = strings.Repeat("b", 64); return f },
		"duplicate manifest": func(f []map[string]any) []map[string]any {
			return append(f[:2], append([]map[string]any{f[1]}, f[2:]...)...)
		},
		"duplicate step": func(f []map[string]any) []map[string]any {
			return append(f[:4], append([]map[string]any{f[3]}, f[4:]...)...)
		},
		"late manifest":            func(f []map[string]any) []map[string]any { f[1], f[2] = f[2], f[1]; return f },
		"target retry":             func(f []map[string]any) []map[string]any { f[2]["retry"] = 1; return f },
		"no target assertions":     func(f []map[string]any) []map[string]any { delete(f[3], "operation"); return f },
		"caught failed assertion":  func(f []map[string]any) []map[string]any { f[3]["status"] = "failed"; return f },
		"unknown operation":        func(f []map[string]any) []map[string]any { f[3]["operation"] = "swipe"; return f },
		"wrong assertion category": func(f []map[string]any) []map[string]any { f[3]["category"] = "action"; return f },
		"version mismatch":         func(f []map[string]any) []map[string]any { f[0]["targetVersion"] = "9l.target/99"; return f },
		"capability mismatch":      func(f []map[string]any) []map[string]any { f[0]["capabilities"] = []string{"guess"}; return f },
		"missing terminal":         func(f []map[string]any) []map[string]any { return f[:len(f)-1] },
		"content persistence":      func(f []map[string]any) []map[string]any { f[3]["text"] = "synthetic content"; return f },
		"expected failure":         func(f []map[string]any) []map[string]any { f[4]["expectedStatus"] = "failed"; return f },
		"skip hides failed operation": func(f []map[string]any) []map[string]any {
			f[3]["status"], f[4]["status"], f[4]["expectedStatus"] = "failed", "skipped", "skipped"
			f[5]["outcome"], f[5]["skipKey"] = "skipped", SkipPinKey("shared.spec.ts › t")
			return f
		},
	}
	for name, change := range cases {
		t.Run(name, func(t *testing.T) {
			if _, err := New().ValidateAttempt(stream(change(targetFrames())), identity); err == nil {
				t.Fatal("invalid target evidence accepted")
			}
		})
	}
	raw := stream(targetFrames())
	if _, err := New().ValidateAttempt(raw[:len(raw)-1], identity); err == nil {
		t.Fatal("truncation accepted")
	}
}

func TestTargetBusinessFailureIsTerminal(t *testing.T) {
	f := targetFrames()
	f[3]["status"], f[4]["status"], f[5]["outcome"], f[6]["status"] = "failed", "failed", "unexpected", "failed"
	identity := owner
	identity.Target = "web:finance"
	v, err := New().ValidateAttempt(stream(f), identity)
	if err != nil || v.FailureCount != 1 || v.TargetAssertions != 0 {
		t.Fatalf("business failure lost: %+v %v", v, err)
	}
}

func TestPlanSchedulesTargetsWithOneAttemptEach(t *testing.T) {
	root := t.TempDir()
	files := []string{"tests/shared.spec.ts", "node_modules/.bin/playwright", "node_modules/@9l/playwright/dist/reporter.js"}
	if runtime.GOOS == "windows" {
		files[1] += ".cmd"
		files = append(files, "node_modules/@playwright/test/cli.js")
	}
	for _, name := range files {
		file := filepath.Join(root, name)
		if err := os.MkdirAll(filepath.Dir(file), 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(file, []byte("// fixture"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(root, "package.json"), []byte(`{"devDependencies":{"@playwright/test":"1.61.1"}}`), 0600); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(root, files[0])
	opts := runner.PlanOptions{Adapters: []runner.Adapter{New()}, Targets: []string{"web:finance", "android-fake:finance"}}
	plan, err := runner.BuildPlan([]string{path}, opts)
	if err != nil || len(plan.Jobs) != 2 {
		t.Fatalf("target jobs: %+v %v", plan, err)
	}
	for i, job := range plan.Jobs {
		if job.Target != opts.Targets[i] || job.Env["NINELIVES_TARGET"] != job.Target || !slices.Contains(job.Command, "--project="+job.Target) || !slices.Contains(job.Command, "--retries=0") {
			t.Fatalf("unowned target: %+v", job)
		}
	}
	opts.MaxJobs = 1
	plan, err = runner.BuildPlan([]string{path}, opts)
	if err != nil || len(plan.Jobs) != 1 || len(plan.Skipped) != 1 {
		t.Fatalf("target job budget: %+v %v", plan, err)
	}
	for _, targets := range [][]string{{""}, {"web:finance", "web:finance"}} {
		opts.Targets = targets
		if _, err := runner.BuildPlan([]string{path}, opts); err == nil {
			t.Fatal("ambiguous target selection accepted")
		}
	}
	opts.Targets, opts.Projects = []string{"web:finance"}, []string{"other"}
	if _, err := runner.BuildPlan([]string{path}, opts); err == nil {
		t.Fatal("target/project collision accepted")
	}
}

func TestTargetSkipFollowsSkipPins(t *testing.T) {
	f := targetFrames()
	delete(f[3], "operation")
	f[4]["status"], f[4]["expectedStatus"] = "skipped", "skipped"
	f[5]["outcome"], f[5]["skipKey"] = "skipped", SkipPinKey("shared.spec.ts › t")
	identity := owner
	identity.Target = "web:finance"
	// A skip passes the target checks and reaches ordinary skip accounting; a
	// lone skipped test is still not a completed run.
	_, err := New().ValidateAttempt(stream(f), identity)
	if err == nil || !strings.Contains(err.Error(), "no completed tests") {
		t.Fatalf("target skip did not reach skip accounting: %v", err)
	}
}
