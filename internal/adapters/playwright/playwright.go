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
	var walk func(suite)
	walk = func(current suite) {
		for _, spec := range current.Specs {
			for _, test := range spec.Tests {
				for _, result := range test.Results {
					validation.ExecutedTests++
					if result.Status == "failed" || result.Status == "timedOut" {
						validation.FailureCount++
					}
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
	return validation, nil
}
