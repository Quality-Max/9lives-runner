package runner

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

type PlanOptions struct {
	MaxJobs        int
	MaxParallel    int
	MaxAttempts    int
	MaxOutputBytes int
	Deadline       time.Duration
	Adapters       []Adapter
}

// BuildPlan resolves every candidate before execution and reserves the
// run-wide job budget up front.
func BuildPlan(inputs []string, opts PlanOptions) (Plan, error) {
	plan := Plan{
		Version: PlanVersion, RunID: newRunID(), CreatedAt: time.Now().UTC(), Jobs: []Job{}, Skipped: []Skipped{},
		Limits: Limits{MaxJobs: opts.MaxJobs, MaxParallel: opts.MaxParallel, MaxAttempts: opts.MaxAttempts, MaxOutputBytes: opts.MaxOutputBytes, DeadlineMS: opts.Deadline.Milliseconds()},
	}
	seen := map[string]bool{}
	for _, input := range inputs {
		matches, err := expand(input, opts.Adapters)
		if err != nil {
			return Plan{}, fmt.Errorf("%q: %w", input, err)
		}
		if len(matches) == 0 {
			plan.Skipped = append(plan.Skipped, Skipped{Input: input, Reason: "no matching files"})
			continue
		}
		for _, path := range matches {
			absolute, err := filepath.Abs(path)
			if err != nil {
				return Plan{}, err
			}
			if seen[absolute] {
				continue
			}
			seen[absolute] = true
			candidate := adapterFor(opts.Adapters, absolute)
			if candidate == nil {
				plan.Skipped = append(plan.Skipped, Skipped{Input: path, Reason: "unsupported spec; currently supported: Playwright .spec/.test JS, JSX, TS, and TSX files"})
				continue
			}
			if opts.MaxJobs > 0 && len(plan.Jobs) >= opts.MaxJobs {
				plan.Skipped = append(plan.Skipped, Skipped{Input: path, Reason: "run-wide job budget exhausted"})
				continue
			}
			job, err := candidate.Plan(absolute, input, len(plan.Jobs)+1)
			if err != nil {
				plan.Skipped = append(plan.Skipped, Skipped{Input: path, Reason: err.Error()})
				continue
			}
			plan.Jobs = append(plan.Jobs, job)
		}
	}
	return plan, nil
}

func expand(input string, adapters []Adapter) ([]string, error) {
	if info, err := os.Stat(input); err == nil {
		if !info.IsDir() {
			return []string{input}, nil
		}
		found := []string{}
		err := filepath.WalkDir(input, func(path string, entry os.DirEntry, walkErr error) error {
			if walkErr != nil {
				return walkErr
			}
			if entry.IsDir() && path != input && ignoredDirectory(entry.Name()) {
				return filepath.SkipDir
			}
			if !entry.IsDir() && adapterFor(adapters, path) != nil {
				found = append(found, path)
			}
			return nil
		})
		sort.Strings(found)
		return found, err
	} else if !os.IsNotExist(err) {
		return nil, err
	}
	matches, err := filepath.Glob(input)
	if err != nil {
		return nil, err
	}
	sort.Strings(matches)
	return matches, nil
}

func ignoredDirectory(name string) bool {
	return name == "node_modules" || name == ".git" || name == ".9lives" || strings.HasPrefix(name, ".")
}

func newRunID() string {
	random := make([]byte, 6)
	if _, err := rand.Read(random); err != nil {
		return fmt.Sprintf("run-%d", time.Now().UnixNano())
	}
	return "run-" + time.Now().UTC().Format("20060102T150405Z") + "-" + hex.EncodeToString(random)
}
