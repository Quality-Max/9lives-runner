package playwrightsdk

import (
	"fmt"
	"strings"
	"testing"

	"github.com/Quality-Max/9lives-runner/internal/adapters/playwright"
	"github.com/Quality-Max/9lives-runner/internal/runner"
)

// outcomeFrames is an engine stream of one test per terminal outcome: a
// pass, a flaky pass, a failed assertion, a timeout and a pinned skip.
func outcomeFrames() ([]map[string]any, map[string]string) {
	id := map[string]string{}
	for index, name := range []string{"pass", "flaky", "fail", "timeout", "skip"} {
		id[name] = strings.Repeat(fmt.Sprint(index+1), 64)
	}
	begin := func(name string, retry int, expected string) map[string]any {
		return map[string]any{"type": "test_begin", "testId": id[name], "retry": retry, "expectedStatus": expected}
	}
	end := func(name string, retry int, status, expected string) map[string]any {
		return map[string]any{"type": "test_end", "testId": id[name], "retry": retry, "status": status, "expectedStatus": expected, "artifacts": []any{}}
	}
	step := func(name string, retry, number int, status string) map[string]any {
		return map[string]any{"type": "step_end", "testId": id[name], "retry": retry, "stepId": fmt.Sprintf("step-%d", number), "category": "assertion", "status": status}
	}
	result := func(name, outcome string) map[string]any {
		return map[string]any{"type": "test_result", "testId": id[name], "outcome": outcome}
	}
	skipped := result("skip", "skipped")
	skipped["skipKey"] = SkipPinKey(skipPin)
	return []map[string]any{
		{"type": "hello", "capabilities": []string{"steps", "artifact-metadata", "terminal-outcomes", "declared-outcomes"}, "totalTests": 5},
		begin("pass", 0, "passed"), step("pass", 0, 1, "passed"), end("pass", 0, "passed", "passed"),
		begin("flaky", 0, "passed"), step("flaky", 0, 2, "failed"), end("flaky", 0, "failed", "passed"),
		begin("flaky", 1, "passed"), step("flaky", 1, 3, "passed"), end("flaky", 1, "passed", "passed"),
		begin("fail", 0, "passed"), step("fail", 0, 4, "failed"), end("fail", 0, "failed", "passed"),
		begin("timeout", 0, "passed"), end("timeout", 0, "timedOut", "passed"),
		begin("skip", 0, "skipped"), end("skip", 0, "skipped", "skipped"),
		result("pass", "expected"), result("flaky", "flaky"), result("fail", "unexpected"), result("timeout", "unexpected"), skipped,
		{"type": "end", "status": "failed"},
	}, id
}

// The same five tests as Playwright's own JSON reporter describes them.
const outcomeReport = `{"stats":{"duration":1},"suites":[{"title":"tests/outcomes.spec.ts","specs":[
	{"title":"pass","tests":[{"status":"expected","expectedStatus":"passed","results":[{"status":"passed","duration":10}]}]},
	{"title":"flaky","tests":[{"status":"flaky","expectedStatus":"passed","results":[{"status":"failed","duration":10},{"status":"passed","duration":10}]}]},
	{"title":"fail","tests":[{"status":"unexpected","expectedStatus":"passed","results":[{"status":"failed","duration":10,"errors":[{"message":"Error: expect(received).toBe(expected)"}]}]}]},
	{"title":"timeout","tests":[{"status":"unexpected","expectedStatus":"passed","results":[{"status":"timedOut","duration":10}]}]},
	{"title":"skip","tests":[{"status":"skipped","expectedStatus":"skipped","results":[{"status":"skipped","duration":0}]}]}
]}]}`

func TestEngineStreamListsEveryTestByDigestOnly(t *testing.T) {
	adapter, err := New().WithSkipPins([]string{skipPin})
	if err != nil {
		t.Fatal(err)
	}
	frames, id := outcomeFrames()
	validation, err := adapter.ValidateAttempt(stream(frames), owner)
	if err != nil {
		t.Fatal(err)
	}
	want := []runner.TestOutcome{
		{TestID: id["pass"], Status: runner.TestPassed},
		{TestID: id["flaky"], Status: runner.TestFlaky},
		{TestID: id["fail"], Status: runner.TestFailed, Class: runner.FailureAssertion},
		{TestID: id["timeout"], Status: runner.TestFailed, Class: runner.FailureTimeout},
		{TestID: id["skip"], Status: runner.TestSkipped},
	}
	if len(validation.Tests) != len(want) {
		t.Fatalf("listed %+v", validation.Tests)
	}
	for index, test := range validation.Tests {
		// No title crosses the bridge and no duration is measured, so the
		// whole entry is the digest and closed tokens.
		if test != want[index] || test.Validate() != nil {
			t.Errorf("test %d: %+v, want %+v", index, test, want[index])
		}
	}
	// Each failed entry's class is the one its failure summary carries.
	for _, failed := range validation.FailedTests {
		for _, test := range validation.Tests {
			if test.TestID == failed.TestID && test.Class != failed.Class {
				t.Errorf("%s: list says %s, summary says %s", test.TestID[:8], test.Class, failed.Class)
			}
		}
	}
}

// A JUnit report must not depend on which adapter ran the tests: the engine
// stream and Playwright's report of the same tests list the same outcomes.
func TestEngineAndPlainListsAgree(t *testing.T) {
	adapter, err := New().WithSkipPins([]string{skipPin})
	if err != nil {
		t.Fatal(err)
	}
	frames, _ := outcomeFrames()
	engine, err := adapter.ValidateAttempt(stream(frames), owner)
	if err != nil {
		t.Fatal(err)
	}
	plain, err := playwright.New().Validate([]byte(outcomeReport))
	if err != nil {
		t.Fatal(err)
	}
	outcomes := func(validation runner.Validation) string {
		var each []string
		for _, test := range validation.Tests {
			each = append(each, test.Status+"/"+test.Class)
		}
		return strings.Join(each, " ")
	}
	if len(engine.Tests) != len(plain.Tests) || outcomes(engine) != outcomes(plain) {
		t.Fatalf("engine lists %q, plain lists %q", outcomes(engine), outcomes(plain))
	}
	for _, validation := range []runner.Validation{engine, plain} {
		if len(validation.Tests) != validation.ExecutedTests+validation.SkippedTests || validation.FailureCount != 2 || validation.FlakyTests != 1 || validation.SkippedTests != 1 {
			t.Fatalf("list disagrees with counts: %+v", validation)
		}
	}
}

// An invalid stream lists nothing: its tests have no outcome to report.
func TestRejectedStreamListsNoTests(t *testing.T) {
	frames, _ := outcomeFrames()
	validation, err := New().ValidateAttempt(stream(frames), owner) // the skip is not pinned
	if err == nil || len(validation.Tests) != 0 {
		t.Fatalf("a rejected stream listed tests: %+v, %v", validation.Tests, err)
	}
}

func TestTitlesComeFromTheDiagnosticsReportByDigest(t *testing.T) {
	titles := New().TestTitles([]byte(`{"suites":[{"title":"a.spec.ts","specs":[{"id":"abc","title":"adds","tests":[{"status":"expected","results":[]}]}]}]}`))
	// sha256("abc"), as the reporter derives a test's digest from its ID.
	if len(titles) != 1 || titles["ba7816bf8f01cfea414140de5dae2223b00361a396177a9cb410ff61f20015ad"] != "adds" {
		t.Fatalf("titles %v", titles)
	}
}
