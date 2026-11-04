package playwrightsdk

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/Quality-Max/9lives-runner/internal/runner"
)

// failedTargetFrames is a web target test whose typed assert-text operation
// failed with the given stop reason ("" sends none).
func failedTargetFrames(reason string) []map[string]any {
	f := targetFrames()
	// hello, required_test, test_begin, step_end, test_end, test_result, end
	f[3]["status"] = "failed"
	if reason != "" {
		f[3]["reason"] = reason
	}
	f[4]["status"] = "failed"
	f[5]["outcome"] = "unexpected"
	f[6]["status"] = "failed"
	return f
}

func webTarget() runner.AttemptIdentity {
	identity := owner
	identity.Target = "web:finance"
	return identity
}

func TestFailedTestSummaryCarriesTheTargetStopReason(t *testing.T) {
	for reason, class := range map[string]string{
		"assertion_failed":  runner.FailureAssertion,
		"deadline":          runner.FailureActionTimeout,
		"operation_timeout": runner.FailureActionTimeout,
		"operation_failed":  runner.FailureOther,
		"setup_blocked":     runner.FailureSetup,
		"":                  runner.FailureAssertion, // an older SDK sends no reason
	} {
		v, err := New().ValidateAttempt(stream(failedTargetFrames(reason)), webTarget())
		if err != nil || v.FailureCount != 1 || len(v.FailedTests) != 1 {
			t.Fatalf("%q: %+v %v", reason, v, err)
		}
		want := runner.FailedTestSummary{TestID: testKey, Class: class, Status: "failed", Step: "assertion", Operation: "assert-text", Reason: reason, FailedSteps: 1, Attempts: 1}
		if got := v.FailedTests[0]; got != want {
			t.Fatalf("%q: summary %+v, want %+v", reason, got, want)
		}
		if err := v.FailedTests[0].Validate(); err != nil {
			t.Fatalf("%q: %v", reason, err)
		}
	}
}

func TestTargetStopReasonIsAClosedTokenOnAFailedOperation(t *testing.T) {
	cases := map[string]func([]map[string]any) []map[string]any{
		"free text":        func(f []map[string]any) []map[string]any { f[3]["reason"] = "Close picker still displayed"; return f },
		"uppercase":        func(f []map[string]any) []map[string]any { f[3]["reason"] = "Assertion_failed"; return f },
		"too long":         func(f []map[string]any) []map[string]any { f[3]["reason"] = strings.Repeat("a", 65); return f },
		"empty":            func(f []map[string]any) []map[string]any { f[3]["reason"] = ""; return f },
		"not a string":     func(f []map[string]any) []map[string]any { f[3]["reason"] = 7; return f },
		"on a passed step": func(f []map[string]any) []map[string]any { f[3]["status"] = "passed"; return f },
		"without an operation": func(f []map[string]any) []map[string]any {
			delete(f[3], "operation")
			f[3]["category"] = "assertion"
			return f
		},
	}
	for name, mutate := range cases {
		if _, err := New().ValidateAttempt(stream(mutate(failedTargetFrames("assertion_failed"))), webTarget()); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
	// An untargeted stream has no typed operations, so it can carry no reason.
	frames := passingFrames()
	frames[2]["status"], frames[2]["reason"] = "failed", "assertion_failed"
	if _, err := New().ValidateAttempt(stream(frames), owner); err == nil {
		t.Fatal("untargeted step accepted a stop reason")
	}
}

func TestFailedTestSummaryClassifiesFromStructuredFactsOnly(t *testing.T) {
	step := func(id int, category, status string) map[string]any {
		return map[string]any{"type": "step_end", "testId": testKey, "retry": 0, "stepId": fmt.Sprintf("step-%d", id), "category": category, "status": status}
	}
	failed := func(status string, steps ...map[string]any) []map[string]any {
		f := passingFrames()
		f[3]["status"] = status
		f[4]["outcome"] = "unexpected"
		f[5]["status"] = "failed"
		return append(f[:2], append(steps, f[3:]...)...)
	}
	for name, test := range map[string]struct {
		frames []map[string]any
		want   runner.FailedTestSummary
	}{
		// An expect inside a test.step ends first and both fail: the
		// assertion is the cause, not its wrapper.
		"assertion in a step": {failed("failed", step(1, "assertion", "failed"), step(2, "action", "failed")),
			runner.FailedTestSummary{Class: runner.FailureAssertion, Status: "failed", Step: "assertion", FailedSteps: 2}},
		"browser call": {failed("failed", step(1, "browser", "failed"), step(2, "fixture", "failed")),
			runner.FailedTestSummary{Class: runner.FailureOther, Status: "failed", Step: "browser", FailedSteps: 2}},
		"test timeout": {failed("timedOut", step(1, "assertion", "failed")),
			runner.FailedTestSummary{Class: runner.FailureTimeout, Status: "timedOut", Step: "assertion", FailedSteps: 1}},
		"no failed step": {failed("failed", step(1, "assertion", "passed")),
			runner.FailedTestSummary{Class: runner.FailureOther, Status: "failed"}},
	} {
		v, err := New().ValidateAttempt(stream(test.frames), owner)
		if err != nil || len(v.FailedTests) != 1 {
			t.Fatalf("%s: %+v %v", name, v, err)
		}
		test.want.TestID, test.want.Attempts = testKey, 1
		if got := v.FailedTests[0]; got != test.want {
			t.Errorf("%s: %+v, want %+v", name, got, test.want)
		}
	}
	// A test.fail() test that passed is an unexpected pass, whatever failed.
	f := passingFrames()
	f[1]["expectedStatus"], f[3]["expectedStatus"], f[4]["outcome"], f[5]["status"] = "failed", "failed", "unexpected", "failed"
	v, err := New().ValidateAttempt(stream(f), owner)
	if err != nil || len(v.FailedTests) != 1 || v.FailedTests[0].Class != runner.FailureUnexpectedPass || v.FailedTests[0].Status != "passed" {
		t.Fatalf("unexpected pass: %+v %v", v, err)
	}
}

func TestFailedTestSummaryKeepsTheLastCompletedAttempt(t *testing.T) {
	f := passingFrames()
	// Attempt 0 failed an assertion; attempt 1 timed out in a browser call.
	f[2]["status"] = "failed"
	f[3]["status"] = "failed"
	f[4]["outcome"] = "unexpected"
	f[5]["status"] = "failed"
	f = append(f[:4], append([]map[string]any{
		{"type": "test_begin", "testId": testKey, "retry": 1, "expectedStatus": "passed"},
		{"type": "step_end", "testId": testKey, "retry": 1, "stepId": "step-2", "category": "browser", "status": "failed"},
		{"type": "test_end", "testId": testKey, "retry": 1, "status": "timedOut", "expectedStatus": "passed", "artifacts": []any{}},
	}, f[4:]...)...)
	v, err := New().ValidateAttempt(stream(f), owner)
	want := runner.FailedTestSummary{TestID: testKey, Class: runner.FailureTimeout, Status: "timedOut", Step: "browser", FailedSteps: 1, Attempts: 2}
	if err != nil || len(v.FailedTests) != 1 || v.FailedTests[0] != want {
		t.Fatalf("last attempt: %+v %v", v.FailedTests, err)
	}
}

func TestFailedTestSummaryNamesAnUnrecoveredSetup(t *testing.T) {
	identity := owner
	identity.Target = "android-fake:finance"
	f := setupFrames("android-fake:finance", blocked())
	f[4]["reason"] = "setup_blocked"
	v, err := New().ValidateAttempt(stream(f), identity)
	want := runner.FailedTestSummary{TestID: testKey, Class: runner.FailureSetup, Status: "failed", Step: "assertion", Operation: "assert-text", Reason: "setup_blocked", Setup: runner.SetupBlocked, FailedSteps: 1, Attempts: 1}
	if err != nil || len(v.FailedTests) != 1 || v.FailedTests[0] != want {
		t.Fatalf("setup: %+v %v", v.FailedTests, err)
	}
	// A recovered baseline is not a setup failure of the test.
	dirty := map[string]any{"classification": "dirty", "reason": "extra-window", "stage": "open", "recovery": map[string]any{"method": "back", "presses": 2, "restored": true}}
	f = setupFrames("android-fake:finance", dirty)
	f[4]["reason"] = "assertion_failed"
	v, err = New().ValidateAttempt(stream(f), identity)
	if err != nil || len(v.FailedTests) != 1 || v.FailedTests[0].Setup != "" || v.FailedTests[0].Class != runner.FailureAssertion {
		t.Fatalf("recovered: %+v %v", v.FailedTests, err)
	}
}

func TestFailedTestSummaryIsBounded(t *testing.T) {
	total := runner.MaxFailureSummaryTests + 5
	frames := []map[string]any{{"type": "hello", "capabilities": Capabilities, "totalTests": total}}
	for i := range total {
		id := fmt.Sprintf("%064x", i+1)
		frames = append(frames,
			map[string]any{"type": "test_begin", "testId": id, "retry": 0, "expectedStatus": "passed"},
			map[string]any{"type": "step_end", "testId": id, "retry": 0, "stepId": fmt.Sprintf("step-%d", i+1), "category": "assertion", "status": "failed"},
			map[string]any{"type": "test_end", "testId": id, "retry": 0, "status": "failed", "expectedStatus": "passed", "artifacts": []any{}})
	}
	for i := range total {
		frames = append(frames, map[string]any{"type": "test_result", "testId": fmt.Sprintf("%064x", i+1), "outcome": "unexpected"})
	}
	frames = append(frames, map[string]any{"type": "end", "status": "failed"})
	v, err := New().ValidateAttempt(stream(frames), owner)
	if err != nil || v.FailureCount != total || len(v.FailedTests) != runner.MaxFailureSummaryTests {
		t.Fatalf("bounded: %d of %d, %v", len(v.FailedTests), v.FailureCount, err)
	}
	// The summary is metadata: nothing in it is longer than a digest.
	raw, _ := json.Marshal(v.FailedTests)
	var entries []map[string]any
	if err := json.Unmarshal(raw, &entries); err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		for key, value := range entry {
			if text, ok := value.(string); ok && len(text) > 64 {
				t.Fatalf("%s is %d bytes", key, len(text))
			}
		}
	}
}

// A failed control operation may name the identifier-shaped test ID it
// addressed, and the failure summary keeps it beside the operation.
func TestFailedTestSummaryNamesTheFailedControl(t *testing.T) {
	f := failedTargetFrames("assertion_failed")
	f[3]["control"] = "login.status-2"
	v, err := New().ValidateAttempt(stream(f), webTarget())
	if err != nil || len(v.FailedTests) != 1 {
		t.Fatalf("%+v %v", v, err)
	}
	if got := v.FailedTests[0]; got.Control != "login.status-2" || got.Operation != "assert-text" || got.Validate() != nil {
		t.Fatalf("summary %+v", got)
	}
}

func TestTargetControlIsAnIdentifierOnAFailedControlOperation(t *testing.T) {
	cases := map[string]func([]map[string]any) []map[string]any{
		"free text":    func(f []map[string]any) []map[string]any { f[3]["control"] = "Pay 4242 now"; return f },
		"an email":     func(f []map[string]any) []map[string]any { f[3]["control"] = "ada@example.test"; return f },
		"too long":     func(f []map[string]any) []map[string]any { f[3]["control"] = strings.Repeat("a", 65); return f },
		"leading dash": func(f []map[string]any) []map[string]any { f[3]["control"] = "-send"; return f },
		"empty":        func(f []map[string]any) []map[string]any { f[3]["control"] = ""; return f },
		"not a string": func(f []map[string]any) []map[string]any { f[3]["control"] = 7; return f },
		"on a passed step": func(f []map[string]any) []map[string]any {
			f[3]["control"], f[3]["status"] = "send", "passed"
			delete(f[3], "reason")
			return f
		},
		"on an app-level operation": func(f []map[string]any) []map[string]any {
			f[3]["control"], f[3]["operation"], f[3]["category"] = "send", "back", "action"
			return f
		},
		"without an operation": func(f []map[string]any) []map[string]any {
			f[3]["control"] = "send"
			delete(f[3], "operation")
			delete(f[3], "reason")
			return f
		},
	}
	for name, mutate := range cases {
		if _, err := New().ValidateAttempt(stream(mutate(failedTargetFrames("assertion_failed"))), webTarget()); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
}
