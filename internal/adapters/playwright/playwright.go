// Package playwright implements the initial local Playwright runner adapter.
package playwright

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/qualitymax/9lives-runner/internal/runner"
)

type Adapter struct{}

func New() Adapter           { return Adapter{} }
func (Adapter) Name() string { return "playwright" }

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
	return runner.Job{
		ID: fmt.Sprintf("job-%03d", index), Input: input, Spec: path,
		WorkDir: project, Adapter: adapter.Name(), Command: []string{binary, "test", relative, "--reporter=json"}, DependsOn: []string{},
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
	unsupported, incomplete, flaky := "", "", 0
	var walk func(suite)
	walk = func(current suite) {
		for _, spec := range current.Specs {
			for _, test := range spec.Tests {
				// Counts are per test, not per retry attempt. A test that failed
				// and then passed on retry is flaky, not failed.
				outcome := test.Status
				if outcome == "" && len(test.Results) > 0 {
					outcome = legacyOutcome(test.Results[len(test.Results)-1].Status)
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

// legacyOutcome maps a final attempt status for reports without a per-test
// status field. It cannot see test.fail() expectations, so it is conservative.
func legacyOutcome(status string) string {
	switch status {
	case "passed":
		return "expected"
	case "failed", "timedOut":
		return "unexpected"
	case "skipped":
		return "skipped"
	}
	return status
}
