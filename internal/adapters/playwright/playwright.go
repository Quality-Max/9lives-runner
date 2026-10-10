// Package playwright implements the initial local Playwright runner adapter.
package playwright

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"

	"github.com/Quality-Max/9lives-runner/internal/healing"
	"github.com/Quality-Max/9lives-runner/internal/runner"
)

// FailureContext extracts bounded failed-test diagnostic text from Playwright's
// structured reporter output. It deliberately excludes source-code frames: a
// locator timeout may be followed by an unrelated `expect(...)` line in the
// stack, which must not turn a locator failure into an assertion failure.
func FailureContext(raw []byte) string {
	var payload any
	if json.Unmarshal(raw, &payload) != nil {
		return ""
	}
	var values []string
	var visit func(any)
	visit = func(value any) {
		switch node := value.(type) {
		case map[string]any:
			if message, ok := node["message"].(string); ok && message != "" {
				values = append(values, withoutSourceFrames(message))
			}
			for _, child := range node {
				visit(child)
			}
		case []any:
			for _, child := range node {
				visit(child)
			}
		}
	}
	visit(payload)
	context := strings.TrimSpace(strings.Join(values, "\n"))
	if len(context) > 3000 {
		// Classify the complete diagnostics before clipping. Otherwise a long
		// locator call log can hide a later assertion or infrastructure failure.
		var summary string
		switch kind := healing.Classify(context, ""); kind {
		case "assertion_failed", "navigation_failed", "flow_changed", "syntax_error":
			summary = "Reported failure: " + strings.ReplaceAll(kind, "_", " ") + "\n"
		}
		// Native healing also rejects these even when a locator is mentioned.
		lower := strings.ToLower(context)
		for _, unsafe := range []string{"network", "syntax", "navigation", "flow changed"} {
			if strings.Contains(lower, unsafe) {
				summary += "Reported failure: " + unsafe + "\n"
			}
		}
		return summary + context[:3000-len(summary)]
	}
	return context
}

var sourceFrame = regexp.MustCompile(`(?m)^\s*(?:>|\|)?\s*\d+\s*\|`)

func withoutSourceFrames(message string) string {
	if location := sourceFrame.FindStringIndex(message); location != nil {
		return strings.TrimSpace(message[:location[0]])
	}
	return strings.TrimSpace(message)
}

type Adapter struct{}

func New() Adapter           { return Adapter{} }
func (Adapter) Name() string { return "playwright" }

// EvidenceEnv points Playwright's JSON reporter at the runner's private
// per-attempt file. The report on stdout would share it with configuration,
// globalSetup and dependencies, and one stray line invalidates the run.
func (Adapter) EvidenceEnv() string { return "PLAYWRIGHT_JSON_OUTPUT_FILE" }

func (Adapter) Supports(path string) bool {
	lower := strings.ToLower(filepath.Base(path))
	for _, suffix := range []string{".spec.js", ".spec.jsx", ".spec.mjs", ".spec.cjs", ".spec.ts", ".spec.tsx", ".test.js", ".test.jsx", ".test.mjs", ".test.cjs", ".test.ts", ".test.tsx"} {
		if strings.HasSuffix(lower, suffix) {
			return true
		}
	}
	return false
}

func (adapter Adapter) Plan(path, input string, index int) (runner.Job, error) {
	project, binary, err := findProject(filepath.Dir(path))
	if err != nil {
		return runner.Job{}, err
	}
	relative, err := filepath.Rel(project, path)
	if err != nil || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
		return runner.Job{}, fmt.Errorf("spec is outside its Playwright project")
	}
	command, err := InstalledCommand(binary, "test", TestFileFilter(project, relative), "--reporter=json")
	if err != nil {
		return runner.Job{}, err
	}
	return runner.Job{
		ID: fmt.Sprintf("job-%03d", index), Input: input, Spec: path,
		WorkDir: project, Adapter: adapter.Name(), Command: command, DependsOn: []string{},
	}, nil
}

func findProject(start string) (string, string, error) {
	for directory := start; ; directory = filepath.Dir(directory) {
		packagePath := filepath.Join(directory, "package.json")
		if data, err := os.ReadFile(packagePath); err == nil {
			var manifest struct {
				Dependencies    map[string]string `json:"dependencies"`
				DevDependencies map[string]string `json:"devDependencies"`
			}
			if json.Unmarshal(data, &manifest) == nil && (manifest.Dependencies["@playwright/test"] != "" || manifest.DevDependencies["@playwright/test"] != "") {
				binary := filepath.Join(directory, "node_modules", ".bin", "playwright")
				if runtime.GOOS == "windows" {
					binary += ".cmd"
				}
				if info, statErr := os.Stat(binary); statErr != nil || info.IsDir() {
					return "", "", fmt.Errorf("Playwright is declared in %s but its local binary is missing; install project dependencies first", packagePath)
				}
				return directory, binary, nil
			}
		}
		parent := filepath.Dir(directory)
		if parent == directory {
			break
		}
	}
	return "", "", fmt.Errorf("no package.json with @playwright/test found; standalone scaffolding is not implemented yet")
}

type report struct {
	Stats *struct {
		Duration float64 `json:"duration"`
	} `json:"stats"`
	Suites []suite `json:"suites"`
	// Errors are reported outside any test, such as a spec that fails to load.
	// Only whether they exist is used; their messages are not retained.
	Errors []struct {
		Message string `json:"message"`
	} `json:"errors"`
}
type suite struct {
	Suites []suite `json:"suites"`
	Specs  []struct {
		Tests []struct {
			// Status is Playwright's per-test outcome after retries and
			// test.fail() expectations: expected, unexpected, flaky, skipped.
			Status  string `json:"status"`
			Results []struct {
				Status string `json:"status"`
			} `json:"results"`
		} `json:"tests"`
	} `json:"specs"`
}

func (Adapter) Validate(raw []byte) (runner.Validation, error) {
	var parsed report
	if err := json.Unmarshal(raw, &parsed); err != nil {
		return runner.Validation{}, fmt.Errorf("invalid Playwright JSON report: %w", err)
	}
	if parsed.Stats == nil || parsed.Suites == nil {
		return runner.Validation{}, fmt.Errorf("invalid Playwright JSON report: required stats or suites field is missing")
	}
	validation := runner.Validation{AssertionCoverage: "unknown", Description: "Playwright JSON report; assertion count unavailable"}
	unsupported, incomplete, flaky, found := "", "", 0, 0
	var walk func(suite)
	walk = func(current suite) {
		for _, spec := range current.Specs {
			for _, test := range spec.Tests {
				found++
				// Counts are per test, not per retry attempt. A test that failed
				// and then passed on retry is flaky, not failed.
				outcome := test.Status
				if outcome == "" && len(test.Results) > 0 {
					outcome = legacyOutcome(test.Results)
				}
				switch outcome {
				case "expected":
					if !hasCompletedAttempt(test.Results) {
						incomplete = "Playwright report contains an executed test without a completed attempt"
						continue
					}
					validation.ExecutedTests++
				case "flaky":
					if !hasCompletedAttempt(test.Results) {
						incomplete = "Playwright report contains an executed test without a completed attempt"
						continue
					}
					validation.ExecutedTests++
					flaky++
				case "unexpected":
					if !hasCompletedAttempt(test.Results) {
						incomplete = "Playwright report contains an executed test without a completed attempt"
						continue
					}
					validation.ExecutedTests++
					validation.FailureCount++
				case "skipped":
					validation.SkippedTests++
				default:
					unsupported = "unsupported Playwright test outcome: " + outcome
				}
			}
		}
		for _, child := range current.Suites {
			walk(child)
		}
	}
	for _, current := range parsed.Suites {
		walk(current)
	}
	if incomplete != "" {
		return runner.Validation{}, fmt.Errorf("%s", incomplete)
	}
	// Playwright selects files through the project's own configuration, so a
	// spec outside its testDir, or excluded by testMatch or testIgnore, runs
	// nothing rather than failing to plan. A spec that fails to load reports
	// its own error as well as finding no tests.
	if found == 0 {
		for _, reported := range parsed.Errors {
			if !strings.HasPrefix(reported.Message, "Error: No tests found") {
				return runner.Validation{}, fmt.Errorf("Playwright report contains no completed tests: Playwright reported an error before finding any test, such as a spec or import that fails to load; see the attempt's structured report")
			}
		}
		return runner.Validation{}, fmt.Errorf("Playwright report contains no completed tests: Playwright found no tests in this spec; check that the project's Playwright config includes it (testDir, testMatch, testIgnore)")
	}
	if validation.ExecutedTests == 0 {
		return runner.Validation{}, fmt.Errorf("Playwright report contains no completed tests")
	}
	if unsupported != "" {
		return runner.Validation{}, fmt.Errorf("%s", unsupported)
	}
	if flaky > 0 {
		validation.Description += fmt.Sprintf("; %d flaky test(s) passed on retry", flaky)
	}
	return validation, nil
}

func hasCompletedAttempt(results []struct {
	Status string `json:"status"`
}) bool {
	for _, result := range results {
		switch result.Status {
		case "passed", "failed", "timedOut":
			return true
		}
	}
	return false
}

// legacyOutcome derives a per-test outcome from every attempt for reports
// without a per-test status field, with Playwright's own precedence
// (computeTestCaseOutcome): interrupted and skipped attempts do not count,
// any failure without a pass is unexpected and a failure with a pass is
// flaky. The final attempt alone would let a skipped or interrupted retry
// hide an earlier failure. It cannot see test.fail() expectations, so passed
// is taken as the expected status.
func legacyOutcome(results []struct {
	Status string `json:"status"`
}) string {
	passed, failed := 0, 0
	for _, result := range results {
		switch result.Status {
		case "passed":
			passed++
		case "failed", "timedOut":
			failed++
		case "skipped", "interrupted":
		default:
			return result.Status
		}
	}
	switch {
	case passed == 0 && failed == 0:
		return "skipped"
	case failed == 0:
		return "expected"
	case passed == 0:
		return "unexpected"
	}
	return "flaky"
}

// InstalledCommand executes the installed JavaScript CLI directly on Windows.
// exec.Cmd cannot execute npm's .cmd shim; a shell would reinterpret spec paths.
func InstalledCommand(binary string, args ...string) ([]string, error) {
	if runtime.GOOS != "windows" {
		return append([]string{binary}, args...), nil
	}
	cli := filepath.Join(filepath.Dir(filepath.Dir(binary)), "@playwright", "test", "cli.js")
	info, err := os.Stat(cli)
	if err != nil || !info.Mode().IsRegular() {
		return nil, fmt.Errorf("installed Playwright JavaScript CLI is missing; install project dependencies first")
	}
	node, err := exec.LookPath("node")
	if err != nil {
		return nil, fmt.Errorf("Node.js is required to execute Playwright")
	}
	return append([]string{node, cli}, args...), nil
}

// TestFileFilter preserves Windows separators and metacharacters when
// Playwright interprets its file argument as a regular expression.
func TestFileFilter(project, relative string) string {
	if runtime.GOOS == "windows" {
		return "^" + regexp.QuoteMeta(filepath.ToSlash(filepath.Join(project, relative))) + "$"
	}
	return relative
}
