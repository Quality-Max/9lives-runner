package playwright

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"testing"

	"github.com/Quality-Max/9lives-runner/internal/runner"
)

func statuses(tests []runner.TestOutcome) map[string]int {
	counts := map[string]int{}
	for _, test := range tests {
		counts[test.Status]++
	}
	return counts
}

func TestValidateListsEveryTestWithItsOutcome(t *testing.T) {
	raw := []byte(`{"stats":{"duration":10},"suites":[{"title":"tests/cart.spec.ts","specs":[
		{"title":"adds an item","tests":[{"status":"expected","expectedStatus":"passed","projectName":"chromium","results":[{"status":"passed","duration":120.7}]}]},
		{"title":"pending","tests":[{"status":"skipped","expectedStatus":"skipped","projectName":"chromium","results":[{"status":"skipped","duration":0}]}]}
	],"suites":[{"title":"totals","specs":[
		{"title":"retries","tests":[{"status":"flaky","expectedStatus":"passed","projectName":"chromium","results":[{"status":"failed","duration":40,"errors":[{"message":"Error: expect(received).toBe(expected)"}]},{"status":"passed","duration":25}]}]},
		{"title":"sums","tests":[{"status":"unexpected","expectedStatus":"passed","projectName":"chromium","results":[{"status":"failed","duration":300,"errors":[{"message":"Error: expect(received).toBe(expected)\n\nExpected: 3"}]}]}]},
		{"title":"hangs","tests":[{"status":"unexpected","expectedStatus":"passed","results":[{"status":"timedOut","duration":30000,"errors":[{"message":"Test timeout of 30000ms exceeded."}]}]}]},
		{"title":"pinned defect was fixed","tests":[{"status":"unexpected","expectedStatus":"failed","results":[{"status":"passed","duration":5}]}]},
		{"title":"known defect","tests":[{"status":"expected","expectedStatus":"failed","results":[{"status":"failed","duration":9}]}]}
	]}]}]}`)
	validation, err := New().Validate(raw)
	if err != nil {
		t.Fatal(err)
	}
	want := []runner.TestOutcome{
		{Title: "[chromium] adds an item", Status: runner.TestPassed, DurationMS: 120},
		{Title: "[chromium] pending", Status: runner.TestSkipped},
		// A flaky test's time covers both attempts; its first failure has no class.
		{Title: "[chromium] totals › retries", Status: runner.TestFlaky, DurationMS: 65},
		{Title: "[chromium] totals › sums", Status: runner.TestFailed, Class: runner.FailureAssertion, DurationMS: 300},
		{Title: "totals › hangs", Status: runner.TestFailed, Class: runner.FailureTimeout, DurationMS: 30000},
		{Title: "totals › pinned defect was fixed", Status: runner.TestFailed, Class: runner.FailureUnexpectedPass, DurationMS: 5},
		// A failure test.fail() expects is the test passing.
		{Title: "totals › known defect", Status: runner.TestPassed, DurationMS: 9},
	}
	if len(validation.Tests) != len(want) {
		t.Fatalf("listed %d tests, want %d: %+v", len(validation.Tests), len(want), validation.Tests)
	}
	for index, test := range validation.Tests {
		if test != want[index] {
			t.Errorf("test %d: %+v, want %+v", index, test, want[index])
		}
	}
	// The list and the counts are two views of the same report.
	counts := statuses(validation.Tests)
	if counts[runner.TestFailed] != validation.FailureCount || counts[runner.TestFlaky] != validation.FlakyTests || counts[runner.TestSkipped] != validation.SkippedTests ||
		len(validation.Tests) != validation.ExecutedTests+validation.SkippedTests {
		t.Fatalf("list %v disagrees with counts %+v", counts, validation)
	}
	// The classes are the ones Failures reports for the same tests.
	failures := New().Failures(raw, t.TempDir())
	var listed []string
	for _, test := range validation.Tests {
		if test.Status == runner.TestFailed {
			listed = append(listed, test.Title+"="+test.Class)
		}
	}
	var reported []string
	for _, failure := range failures {
		reported = append(reported, failure.Title+"="+failure.Class)
	}
	if strings.Join(listed, "|") != strings.Join(reported, "|") {
		t.Fatalf("list %v and failures %v disagree", listed, reported)
	}
}

func TestListedTestsAreClosedRecordsWhateverTheSpecNamesThem(t *testing.T) {
	title := "\x1b[31mred\x1b[0m\nsecond line\ttab \x1b]0;window\x07 password=hunter2secret " + strings.Repeat("é", 2000)
	report := map[string]any{"stats": map[string]any{"duration": 1}, "suites": []any{map[string]any{"title": "a.spec.ts", "specs": []any{
		map[string]any{"title": title, "tests": []any{map[string]any{"status": "expected", "projectName": title, "results": []any{map[string]any{"status": "passed", "duration": 1e300}}}}},
		map[string]any{"title": "", "tests": []any{map[string]any{"status": "unexpected", "results": []any{map[string]any{"status": "failed", "duration": -5, "errors": []any{map[string]any{"message": title}}}}}}},
	}}}}
	raw, err := json.Marshal(report)
	if err != nil {
		t.Fatal(err)
	}
	validation, err := New().Validate(raw)
	if err != nil || len(validation.Tests) != 2 {
		t.Fatalf("validation %+v: %v", validation, err)
	}
	for _, test := range validation.Tests {
		if err := test.Validate(); err != nil {
			t.Errorf("%+v is not a closed record: %v", test, err)
		}
		if strings.Contains(test.Title, "hunter2secret") || strings.Contains(test.Title, "window") || len(test.Title) > runner.MaxTestTitleBytes || test.DurationMS != 0 {
			t.Errorf("unsanitized or unbounded entry: %+v", test)
		}
	}
}

// The real retries report lists one entry per test, never per attempt.
func TestRetriesReportListsTestsNotAttempts(t *testing.T) {
	raw, err := os.ReadFile("testdata/retries-report.json")
	if err != nil {
		t.Fatal(err)
	}
	validation, err := New().Validate(raw)
	if err != nil {
		t.Fatal(err)
	}
	counts := statuses(validation.Tests)
	if len(validation.Tests) != 3 || counts[runner.TestFlaky] != 1 || counts[runner.TestPassed] != 1 || counts[runner.TestSkipped] != 1 {
		t.Fatalf("list %+v", validation.Tests)
	}
}

func TestTestTitlesNamesOnlyTestsTheReportIdentifies(t *testing.T) {
	digest := func(id string) string {
		sum := sha256.Sum256([]byte(id))
		return hex.EncodeToString(sum[:])
	}
	test := func(project string) string {
		return fmt.Sprintf(`{"status":"expected","projectName":%q,"results":[{"status":"passed"}]}`, project)
	}
	raw := []byte(`{"suites":[{"title":"tests/cart.spec.ts","specs":[
		{"id":"id-one","title":"adds an item","tests":[` + test("chromium") + `]},
		{"id":"id-merged","title":"runs in two projects","tests":[` + test("chromium") + `,` + test("firefox") + `]},
		{"title":"has no id","tests":[` + test("") + `]},
		{"id":"id-twice","title":"first","tests":[` + test("") + `]},
		{"id":"id-twice","title":"second","tests":[` + test("") + `]}
	],"suites":[{"title":"totals","specs":[{"id":"id-nested","title":"sums\nup","tests":[` + test("") + `]}]}]}]}`)
	titles := TestTitles(raw)
	if len(titles) != 2 || titles[digest("id-one")] != "[chromium] adds an item" || titles[digest("id-nested")] != "totals › sums up" {
		t.Fatalf("titles %v", titles)
	}
	if TestTitles([]byte("not json")) != nil {
		t.Fatal("an unreadable report named tests")
	}
}
