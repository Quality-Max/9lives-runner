package runner

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
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
	// ExtraArgs are appended to every job's command, such as Playwright's
	// --grep filter, and so recorded in its receipt's evidence command.
	ExtraArgs []string
	// Selection describes ExtraArgs for the plan and diagnostics.
	Selection string
	// Reporters are added after the adapter's own reporter in its
	// --reporter= argument, such as Playwright's html reporter.
	Reporters []string
	// Config replaces the configuration file the adapter found, as an
	// absolute path; Projects narrows the run to these configured projects.
	Config   string
	Projects []string
}

// lineInput matches `<spec>:<line>`.
var lineInput = regexp.MustCompile(`^(.+):([1-9][0-9]{0,6})$`)

// BuildPlan resolves every candidate before execution and reserves the
// run-wide job budget up front.
func BuildPlan(inputs []string, opts PlanOptions) (Plan, error) {
	plan := Plan{
		Version: PlanVersion, RunID: newRunID(), CreatedAt: time.Now().UTC(), Jobs: []Job{}, Skipped: []Skipped{},
		Limits: Limits{MaxJobs: opts.MaxJobs, MaxParallel: opts.MaxParallel, MaxAttempts: opts.MaxAttempts, MaxOutputBytes: opts.MaxOutputBytes, DeadlineMS: opts.Deadline.Milliseconds()},
	}
	seen := map[string]bool{}
	for _, input := range inputs {
		pattern, line := input, 0
		if m := lineInput.FindStringSubmatch(input); m != nil {
			if _, err := os.Stat(input); os.IsNotExist(err) {
				if info, err := os.Stat(m[1]); err == nil && info.Mode().IsRegular() {
					pattern, line = m[1], atoi(m[2])
				}
			}
		}
		matches, err := expand(pattern, opts.Adapters)
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
			key := absolute
			if line > 0 {
				key = fmt.Sprintf("%s:%d", absolute, line)
			}
			if seen[key] {
				continue
			}
			seen[key] = true
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
			var selections []string
			if line > 0 {
				selector, ok := candidate.(LineSelector)
				if !ok {
					plan.Skipped = append(plan.Skipped, Skipped{Input: input, Reason: "this adapter cannot select a test by line"})
					continue
				}
				if job, err = selector.SelectLine(job, line); err != nil {
					plan.Skipped = append(plan.Skipped, Skipped{Input: input, Reason: err.Error()})
					continue
				}
				selections = append(selections, fmt.Sprintf("line %d", line))
			}
			if len(opts.Reporters) > 0 {
				added := false
				for i, argument := range job.Command {
					if strings.HasPrefix(argument, "--reporter=") {
						job.Command = append([]string{}, job.Command...)
						job.Command[i] = argument + "," + strings.Join(opts.Reporters, ",")
						added = true
						break
					}
				}
				if !added {
					plan.Skipped = append(plan.Skipped, Skipped{Input: input, Reason: "this adapter cannot add reporters"})
					continue
				}
			}
			if opts.Config != "" || len(opts.Projects) > 0 {
				var command []string
				for _, argument := range job.Command {
					if opts.Config == "" || !strings.HasPrefix(argument, "--config=") {
						command = append(command, argument)
					}
				}
				if opts.Config != "" {
					command = append(command, "--config="+opts.Config)
					selections = append(selections, "config "+filepath.Base(opts.Config))
				}
				for _, project := range opts.Projects {
					command = append(command, "--project="+project)
				}
				if len(opts.Projects) > 0 {
					selections = append(selections, "project "+strings.Join(opts.Projects, ", "))
				}
				job.Command = command
			}
			if len(opts.ExtraArgs) > 0 {
				job.Command = append(job.Command, opts.ExtraArgs...)
				selections = append(selections, opts.Selection)
			}
			job.Selection = strings.Join(selections, ", ")
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
	} else if !os.IsNotExist(err) && !strings.ContainsAny(input, "*?[") {
		return nil, err
	}
	matches, err := filepath.Glob(input)
	if err != nil {
		return nil, err
	}
	sort.Strings(matches)
	return matches, nil
}

func atoi(digits string) int {
	value := 0
	for _, digit := range digits {
		value = value*10 + int(digit-'0')
	}
	return value
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
