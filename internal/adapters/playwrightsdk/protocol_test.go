package playwrightsdk

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"

	"github.com/Quality-Max/9lives-runner/internal/runner"
)

var owner = runner.AttemptIdentity{RunID: "run-1", JobID: "job-001", AttemptID: "job-001-attempt-001"}
var testKey = strings.Repeat("a", 64)

func passingFrames() []map[string]any {
	return []map[string]any{
		{"type": "hello", "capabilities": []string{"steps", "artifact-metadata", "terminal-outcomes"}, "totalTests": 1},
		{"type": "test_begin", "testId": testKey, "retry": 0},
		{"type": "step_end", "testId": testKey, "retry": 0, "stepId": "step-1", "category": "assertion", "status": "passed"},
		{"type": "test_end", "testId": testKey, "retry": 0, "status": "passed", "expectedStatus": "passed", "artifacts": []any{}},
		{"type": "test_result", "testId": testKey, "outcome": "expected"},
		{"type": "end", "status": "passed"},
	}
}

func stream(frames []map[string]any) []byte {
	var buffer bytes.Buffer
	for i, frame := range frames {
		for key, value := range map[string]any{"version": Version, "runId": owner.RunID, "jobId": owner.JobID, "attemptId": owner.AttemptID, "seq": i + 1} {
			if _, exists := frame[key]; !exists {
				frame[key] = value
			}
		}
		if err := json.NewEncoder(&buffer).Encode(frame); err != nil {
			panic(err)
		}
	}
	return buffer.Bytes()
}

func TestAttemptBoundEvidence(t *testing.T) {
	validation, err := New().ValidateAttempt(stream(passingFrames()), owner)
	if err != nil || validation.ExecutedTests != 1 || validation.FailureCount != 0 || validation.AssertionCoverage != "unknown" || validation.VerifiedAssertions != 0 {
		t.Fatalf("expected complete evidence without inventing assertion coverage: %+v, %v", validation, err)
	}
	if _, err := New().Validate(stream(passingFrames())); err == nil {
		t.Fatal("unbound validation must fail")
	}
	if _, err := New().ValidateAttempt(stream(passingFrames()), runner.AttemptIdentity{}); err == nil {
		t.Fatal("missing owner must fail")
	}
}

func TestRejectUntrustedEvidence(t *testing.T) {
	cases := map[string]func([]map[string]any) []map[string]any{
		"foreign run":         func(f []map[string]any) []map[string]any { f[2]["runId"] = "run-other"; return f },
		"foreign job":         func(f []map[string]any) []map[string]any { f[2]["jobId"] = "job-other"; return f },
		"foreign attempt":     func(f []map[string]any) []map[string]any { f[2]["attemptId"] = "old-attempt"; return f },
		"unknown version":     func(f []map[string]any) []map[string]any { f[0]["version"] = "9l.engine/99"; return f },
		"unknown capability":  func(f []map[string]any) []map[string]any { f[0]["capabilities"] = []string{"guess"}; return f },
		"zero tests":          func(f []map[string]any) []map[string]any { f[0]["totalTests"] = 0; return f },
		"missing terminal":    func(f []map[string]any) []map[string]any { return f[:5] },
		"missing test result": func(f []map[string]any) []map[string]any { return append(f[:4], f[5]) },
		"duplicate sequence":  func(f []map[string]any) []map[string]any { f[2]["seq"] = 2; return f },
		"duplicate test begin": func(f []map[string]any) []map[string]any {
			return append(f[:2], append([]map[string]any{f[1]}, f[2:]...)...)
		},
		"duplicate step": func(f []map[string]any) []map[string]any {
			return append(f[:3], append([]map[string]any{f[2]}, f[3:]...)...)
		},
		"duplicate final":           func(f []map[string]any) []map[string]any { return append(f, f[5]) },
		"late step":                 func(f []map[string]any) []map[string]any { f[2], f[3] = f[3], f[2]; return f },
		"foreign test":              func(f []map[string]any) []map[string]any { f[2]["testId"] = strings.Repeat("b", 64); return f },
		"changed assertion outcome": func(f []map[string]any) []map[string]any { f[3]["status"] = "failed"; return f },
		"unfinished test":           func(f []map[string]any) []map[string]any { f[3]["status"] = "interrupted"; return f },
		"terminal contradicts failure": func(f []map[string]any) []map[string]any {
			f[3]["status"] = "failed"
			f[4]["outcome"] = "unexpected"
			return f
		},
		"worker error": func(f []map[string]any) []map[string]any {
			return append(f[:1], map[string]any{"type": "engine_error"})
		},
		"unsafe extra field": func(f []map[string]any) []map[string]any { f[2]["pageContent"] = "omitted fixture data"; return f },
		"missing retry":      func(f []map[string]any) []map[string]any { delete(f[1], "retry"); return f },
		"unsafe attachment": func(f []map[string]any) []map[string]any {
			f[3]["artifacts"] = []any{map[string]any{"id": "artifact-1", "kind": "json", "retained": false, "path": "outside"}}
			return f
		},
	}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			if _, err := New().ValidateAttempt(stream(mutate(passingFrames())), owner); err == nil {
				t.Fatal("untrusted evidence accepted")
			}
		})
	}
	for name, raw := range map[string][]byte{
		"empty": nil, "garbage": []byte("ordinary console log\n"),
		"truncated":            bytes.TrimSuffix(stream(passingFrames()), []byte("\n")),
		"oversized":            bytes.Repeat([]byte("a"), maxProtocolBytes+1),
		"duplicate object key": bytes.Replace(stream(passingFrames()), []byte(`"type":"hello"`), []byte(`"type":"hello","type":"hello"`), 1),
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := New().ValidateAttempt(raw, owner); err == nil {
				t.Fatal("malformed stream accepted")
			}
		})
	}
}

func TestFailureExpectedFailureAndRetryOutcomes(t *testing.T) {
	for _, expectedFailure := range []bool{false, true} {
		frames := passingFrames()
		frames[3]["status"] = "failed"
		if expectedFailure {
			frames[3]["expectedStatus"] = "failed"
		} else {
			frames[4]["outcome"] = "unexpected"
			frames[5]["status"] = "failed"
		}
		validation, err := New().ValidateAttempt(stream(frames), owner)
		want := 1
		if expectedFailure {
			want = 0
		}
		if err != nil || validation.ExecutedTests != 1 || validation.FailureCount != want {
			t.Fatalf("unexpected outcome: %+v %v", validation, err)
		}
	}
	frames := passingFrames()
	frames[3]["status"] = "failed"
	frames[4]["outcome"] = "flaky"
	frames = append(frames[:4], append([]map[string]any{
		{"type": "test_begin", "testId": testKey, "retry": 1},
		{"type": "test_end", "testId": testKey, "retry": 1, "status": "passed", "expectedStatus": "passed", "artifacts": []any{}},
	}, frames[4:]...)...)
	validation, err := New().ValidateAttempt(stream(frames), owner)
	if err != nil || validation.ExecutedTests != 1 || validation.FailureCount != 0 {
		t.Fatalf("retry must count one test: %+v %v", validation, err)
	}
}

func TestSerialGroupCanRetryExpectedAndSkippedAttempts(t *testing.T) {
	for name, test := range map[string]struct {
		status         string
		outcome        string
		terminalStatus string
	}{
		"expected": {"passed", "expected", "passed"},
		"skipped":  {"skipped", "expected", "passed"},
	} {
		t.Run(name, func(t *testing.T) {
			frames := passingFrames()
			frames[3]["status"] = test.status
			frames[4]["outcome"] = test.outcome
			frames[5]["status"] = test.terminalStatus
			frames = append(frames[:4], append([]map[string]any{
				{"type": "test_begin", "testId": testKey, "retry": 1},
				{"type": "test_end", "testId": testKey, "retry": 1, "status": "passed", "expectedStatus": "passed", "artifacts": []any{}},
			}, frames[4:]...)...)
			if validation, err := New().ValidateAttempt(stream(frames), owner); err != nil || validation.ExecutedTests != 1 || validation.FailureCount != 0 {
				t.Fatalf("serial retry must retain one successful test: %+v %v", validation, err)
			}
		})
	}
}

func TestMissingInstalledSDKFailsPlanning(t *testing.T) {
	path := filepath.Join(t.TempDir(), "checkout.spec.ts")
	if err := os.WriteFile(path, []byte("// spec"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := New().Plan(path, path, 1); err == nil {
		t.Fatal("missing SDK accepted")
	}
}

const skipPin = "tests/skipped.spec.ts › selected test has no execution evidence"

// One passed test plus one skipped test whose result carries the given key.
func mixedSkipFrames(skipKey string) []map[string]any {
	frames := passingFrames()
	frames[0]["totalTests"] = 2
	other := strings.Repeat("b", 64)
	frames = append(frames[:4], append([]map[string]any{
		{"type": "test_begin", "testId": other, "retry": 0},
		{"type": "test_end", "testId": other, "retry": 0, "status": "skipped", "expectedStatus": "skipped", "artifacts": []any{}},
	}, frames[4:]...)...)
	return append(frames[:7], append([]map[string]any{{"type": "test_result", "testId": other, "outcome": "skipped", "skipKey": skipKey}}, frames[7:]...)...)
}

func TestMixedPassedAndSkippedIsIncomplete(t *testing.T) {
	if _, err := New().ValidateAttempt(stream(mixedSkipFrames(SkipPinKey(skipPin))), owner); err == nil || !strings.Contains(err.Error(), "skipped") {
		t.Fatalf("selected skipped test must prevent completeness: %v", err)
	}
	other, err := New().WithSkipPins([]string{"tests/skipped.spec.ts › another title"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := other.ValidateAttempt(stream(mixedSkipFrames(SkipPinKey(skipPin))), owner); err == nil || !strings.Contains(err.Error(), "without a pin") {
		t.Fatalf("a pin for another test must not accept this skip: %v", err)
	}
}

func TestPinnedSkipCompletesWithoutCountingAsExecuted(t *testing.T) {
	adapter, err := New().WithSkipPins([]string{skipPin})
	if err != nil {
		t.Fatal(err)
	}
	validation, err := adapter.ValidateAttempt(stream(mixedSkipFrames(SkipPinKey(skipPin))), owner)
	if err != nil || validation.ExecutedTests != 1 || validation.SkippedTests != 1 || validation.FailureCount != 0 || !strings.Contains(validation.Description, "1 pinned skip") {
		t.Fatalf("pinned skip must complete as skipped: %+v %v", validation, err)
	}
}

func TestPinnedSkipsAloneAreNotExecution(t *testing.T) {
	adapter, err := New().WithSkipPins([]string{skipPin})
	if err != nil {
		t.Fatal(err)
	}
	frames := passingFrames()
	frames[3]["status"], frames[3]["expectedStatus"] = "skipped", "skipped"
	frames[4]["outcome"], frames[4]["skipKey"] = "skipped", SkipPinKey(skipPin)
	frames = append(frames[:2], frames[3:]...) // skipped tests report no steps
	if _, err := adapter.ValidateAttempt(stream(frames), owner); err == nil || !strings.Contains(err.Error(), "no completed tests") {
		t.Fatalf("only pinned skips must not establish execution: %v", err)
	}
}

func TestSkipKeyBelongsOnlyToSkippedResults(t *testing.T) {
	adapter, err := New().WithSkipPins([]string{skipPin})
	if err != nil {
		t.Fatal(err)
	}
	missing := mixedSkipFrames("")
	delete(missing[7], "skipKey")
	malformed := mixedSkipFrames("not-a-digest")
	extra := passingFrames()
	extra[4]["skipKey"] = SkipPinKey(skipPin)
	for name, frames := range map[string][]map[string]any{"missing": missing, "malformed": malformed, "on executed test": extra} {
		if _, err := adapter.ValidateAttempt(stream(frames), owner); err == nil {
			t.Fatalf("%s skip key accepted", name)
		}
	}
}

func TestSkipPinKeyMatchesSDK(t *testing.T) {
	// Same digest as skipPinKey in packages/playwright/test/protocol.test.cjs.
	if got := SkipPinKey(skipPin); got != "65ff0be8a3e9f3cb35403edfbc5b19fc47af011c6764abf4b42abead846caedc" {
		t.Fatalf("skip pin digest drifted from the SDK: %s", got)
	}
}

func TestInvalidSkipPinsAreRejected(t *testing.T) {
	tooMany := make([]string, maxSkipPins+1)
	for i := range tooMany {
		tooMany[i] = fmt.Sprintf("tests/a.spec.ts › test %d", i)
	}
	for name, pins := range map[string][]string{
		"no separator": {"skipped title"},
		"newline":      {"tests/a.spec.ts › two\nlines"},
		"padded":       {"tests/a.spec.ts › title "},
		"invalid utf8": {"tests/a.spec.ts › \xff"},
		"oversized":    {"tests/a.spec.ts › " + strings.Repeat("x", maxSkipPinBytes)},
		"too many":     tooMany,
	} {
		if _, err := New().WithSkipPins(pins); err == nil {
			t.Fatalf("%s pin accepted", name)
		}
	}
}

func TestNestedArtifactJSONRejectsAmbiguity(t *testing.T) {
	frames := passingFrames()
	frames[3]["artifacts"] = []any{map[string]any{"id": "artifact-1", "kind": "json", "retained": false}}
	raw := stream(frames)
	for _, mutation := range []struct{ old, replacement string }{
		{`"retained":false`, `"retained":false,"retained":false`},
		{`"retained":false`, `"retained":null`},
	} {
		changed := bytes.Replace(raw, []byte(mutation.old), []byte(mutation.replacement), 1)
		if _, err := New().ValidateAttempt(changed, owner); err == nil {
			t.Fatal("ambiguous nested artifact accepted")
		}
	}
}

func TestNestedPackageKeepsOwningConfigWithHoistedRuntime(t *testing.T) {
	root := t.TempDir()
	write := func(relative, content string) string {
		t.Helper()
		path := filepath.Join(root, relative)
		if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(content), 0600); err != nil {
			t.Fatal(err)
		}
		return path
	}
	config := write("playwright.config.ts", "export default {}")
	write("package.json", `{"devDependencies":{"@playwright/test":"1.61.1"}}`)
	write("packages/app/package.json", `{"name":"nested-app"}`)
	spec := write("packages/app/tests/checkout.spec.ts", "// spec")
	binaryName := "node_modules/.bin/playwright"
	if runtime.GOOS == "windows" {
		binaryName += ".cmd"
	}
	binary := write(binaryName, "placeholder")
	reporter := write("node_modules/@9l/playwright/dist/reporter.js", "placeholder")
	job, err := New().Plan(spec, spec, 1)
	if err != nil {
		t.Fatal(err)
	}
	if job.WorkDir != root || job.Command[0] != binary || job.Command[2] != filepath.Join("packages", "app", "tests", "checkout.spec.ts") || !slices.Contains(job.Command, "--config="+config) || !slices.Contains(job.Command, "--reporter="+reporter) {
		t.Fatalf("owning config/runtime lost: %+v", job)
	}
}

func TestCaughtGoalFailurePreservesObservedTestCounts(t *testing.T) {
	frames := passingFrames()
	frames[2]["category"], frames[2]["status"] = "goal", "failed"
	validation, err := New().ValidateAttempt(stream(frames), owner)
	if err != nil || !validation.GoalFailed || validation.FailureCount != 0 || validation.ExecutedTests != 1 {
		t.Fatalf("caught goal evidence lost: %+v %v", validation, err)
	}
	frames = passingFrames()
	frames[2]["category"], frames[2]["status"] = "action", "failed"
	validation, err = New().ValidateAttempt(stream(frames), owner)
	if err != nil || validation.GoalFailed {
		t.Fatal("ordinary caught steps are not goal failures")
	}
}

func TestAssertionsCountsFailedTestsWithAFailedAssertion(t *testing.T) {
	other := strings.Repeat("b", 64)
	frames := []map[string]any{
		{"type": "hello", "capabilities": []string{"steps", "artifact-metadata", "terminal-outcomes"}, "totalTests": 2},
		{"type": "test_begin", "testId": testKey, "retry": 0},
		{"type": "step_end", "testId": testKey, "retry": 0, "stepId": "step-1", "category": "assertion", "status": "failed"},
		{"type": "test_end", "testId": testKey, "retry": 0, "status": "failed", "expectedStatus": "passed", "artifacts": []any{}},
		{"type": "test_begin", "testId": other, "retry": 0},
		{"type": "step_end", "testId": other, "retry": 0, "stepId": "step-2", "category": "browser", "status": "failed"},
		{"type": "test_end", "testId": other, "retry": 0, "status": "failed", "expectedStatus": "passed", "artifacts": []any{}},
		{"type": "test_result", "testId": testKey, "outcome": "unexpected"},
		{"type": "test_result", "testId": other, "outcome": "unexpected"},
		{"type": "end", "status": "failed"},
	}
	raw := stream(frames)
	if _, err := New().ValidateAttempt(raw, owner); err != nil {
		t.Fatal(err)
	}
	facts, err := Assertions(raw)
	if err != nil || facts.Unexpected != 2 || facts.UnexpectedWithAssertion != 1 {
		t.Fatalf("facts %+v, %v", facts, err)
	}
	if facts.Tests[testKey] != (TestAssertionFacts{Outcome: "unexpected", Attempts: 1, AssertionFailed: true}) || facts.Tests[other] != (TestAssertionFacts{Outcome: "unexpected", Attempts: 1}) {
		t.Fatalf("per-test facts %+v", facts.Tests)
	}
	// A passing assertion step inside a passing test is not a detection.
	if facts, err := Assertions(stream(passingFrames())); err != nil || facts.Unexpected != 0 || facts.UnexpectedWithAssertion != 0 || facts.Tests[testKey].Outcome != "expected" || facts.Tests[testKey].Attempts != 1 {
		t.Fatalf("passing facts %+v, %v", facts, err)
	}
	// A retried test is visible as more than one attempt, whatever its outcome.
	retried := stream([]map[string]any{
		{"type": "hello", "capabilities": []string{"steps", "artifact-metadata", "terminal-outcomes"}, "totalTests": 1},
		{"type": "test_begin", "testId": testKey, "retry": 0},
		{"type": "step_end", "testId": testKey, "retry": 0, "stepId": "step-1", "category": "assertion", "status": "failed"},
		{"type": "test_end", "testId": testKey, "retry": 0, "status": "failed", "expectedStatus": "passed", "artifacts": []any{}},
		{"type": "test_begin", "testId": testKey, "retry": 1},
		{"type": "step_end", "testId": testKey, "retry": 1, "stepId": "step-2", "category": "assertion", "status": "passed"},
		{"type": "test_end", "testId": testKey, "retry": 1, "status": "passed", "expectedStatus": "passed", "artifacts": []any{}},
		{"type": "test_result", "testId": testKey, "outcome": "flaky"},
		{"type": "end", "status": "passed"},
	})
	if _, err := New().ValidateAttempt(retried, owner); err != nil {
		t.Fatal(err)
	}
	if facts, err := Assertions(retried); err != nil || facts.Tests[testKey] != (TestAssertionFacts{Outcome: "flaky", Attempts: 2, AssertionFailed: true}) {
		t.Fatalf("retried facts %+v, %v", facts.Tests, err)
	}
	if _, err := Assertions([]byte("{\"type\":\"end\"}")); err == nil {
		t.Fatal("accepted unfinished evidence")
	}
}
