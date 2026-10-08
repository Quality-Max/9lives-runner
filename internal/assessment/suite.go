package assessment

import (
	"context"
	"errors"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"
	"unicode/utf8"
)

// Suite budgets. Each file keeps the single-file source, test and helper
// budgets; these bound the suite as a whole.
const (
	MaxSuiteFiles  = 2048
	MaxSuiteSource = 64 << 20
	SuiteTimeout   = 10 * time.Minute
	suiteWorkers   = 4
)

const SuiteVersion = 1

// Playwright's default testMatch: **/*.@(spec|test).?(c|m)[jt]s?(x).
var specName = regexp.MustCompile(`\.(spec|test)\.[cm]?[jt]sx?$`)

// IsPattern reports whether an assess input is a glob rather than a path.
func IsPattern(input string) bool { return strings.ContainsAny(input, "*?[") }

// DiscoverSpecs expands assess inputs into a sorted, de-duplicated list of
// files. A file is taken as given. A directory contributes files named like
// Playwright specs. A pattern matches path segments with path.Match, where a
// "**" segment matches any number of directories. Walks skip node_modules,
// hidden directories and symbolic links, so discovery stays inside the tree.
func DiscoverSpecs(inputs []string) ([]string, error) {
	seen := map[string]bool{}
	var files []string
	add := func(file string) error {
		clean := filepath.Clean(file)
		if seen[clean] {
			return nil
		}
		if len(files) == MaxSuiteFiles {
			return errors.New("assessment input matches more than 2048 files; narrow the directory or pattern")
		}
		seen[clean] = true
		files = append(files, clean)
		return nil
	}
	for _, input := range inputs {
		if IsPattern(input) {
			if err := discoverPattern(input, add); err != nil {
				return nil, err
			}
			continue
		}
		info, err := os.Stat(input)
		if err != nil {
			return nil, errors.New("assessment input unavailable")
		}
		if !info.IsDir() {
			if err := add(input); err != nil {
				return nil, err
			}
			continue
		}
		if err := walk(input, func(file string) error {
			if specName.MatchString(file) {
				return add(file)
			}
			return nil
		}); err != nil {
			return nil, err
		}
	}
	if len(files) == 0 {
		return nil, errors.New("assessment input matched no spec files")
	}
	sort.Strings(files)
	return files, nil
}

func discoverPattern(pattern string, add func(string) error) error {
	segments := strings.Split(filepath.ToSlash(filepath.Clean(pattern)), "/")
	fixed := 0
	for fixed < len(segments) && !IsPattern(segments[fixed]) {
		fixed++
	}
	for _, segment := range segments[fixed:] {
		if _, err := path.Match(segment, ""); err != nil {
			return errors.New("assessment input pattern is malformed")
		}
	}
	root := strings.Join(segments[:fixed], "/")
	if root == "" {
		root = "."
		if strings.HasPrefix(pattern, "/") {
			root = "/"
		}
	}
	info, err := os.Stat(root)
	if err != nil || !info.IsDir() {
		return nil // A pattern under a missing directory matches nothing.
	}
	return walk(root, func(file string) error {
		relative, err := filepath.Rel(root, file)
		if err != nil {
			return nil
		}
		if matchSegments(segments[fixed:], strings.Split(filepath.ToSlash(relative), "/")) {
			return add(file)
		}
		return nil
	})
}

func walk(root string, visit func(string) error) error {
	return filepath.WalkDir(root, func(file string, entry fs.DirEntry, err error) error {
		if err != nil {
			return errors.New("assessment input directory cannot be read")
		}
		if entry.IsDir() {
			if file != root && (entry.Name() == "node_modules" || strings.HasPrefix(entry.Name(), ".")) {
				return filepath.SkipDir
			}
			return nil
		}
		if !entry.Type().IsRegular() {
			return nil
		}
		return visit(file)
	})
}

func matchSegments(pattern, name []string) bool {
	if len(pattern) == 0 {
		return len(name) == 0
	}
	if pattern[0] == "**" {
		for skip := 0; skip <= len(name); skip++ {
			if matchSegments(pattern[1:], name[skip:]) {
				return true
			}
		}
		return false
	}
	if len(name) == 0 {
		return false
	}
	if ok, err := path.Match(pattern[0], name[0]); err != nil || !ok {
		return false
	}
	return matchSegments(pattern[1:], name[1:])
}

// SuiteInput is one discovered file. Error carries a stable code when the
// file could not be read, and the file is then not analyzed.
type SuiteInput struct {
	Path   string
	Source []byte
	Error  string
}

// SuiteFile is one file's report, or the stable code of the reason it has none.
type SuiteFile struct {
	Path   string  `json:"path"`
	Report *Report `json:"report,omitempty"`
	Error  string  `json:"error,omitempty"`
}

type SuiteSummary struct {
	Files    int            `json:"files"`
	Assessed int            `json:"assessed"`
	Failed   int            `json:"failed"`
	Tests    int            `json:"tests"`
	Findings int            `json:"findings"`
	Rules    map[string]int `json:"rules"`
}

// SuiteReport combines per-file reports. Completeness is partial whenever any
// file has no report, in addition to the per-file source-only limits.
type SuiteReport struct {
	Version            int          `json:"version"`
	Policy             string       `json:"policy"`
	RequirementsSHA256 string       `json:"requirementsSHA256,omitempty"`
	Execution          string       `json:"execution"`
	Completeness       string       `json:"completeness"`
	Files              []SuiteFile  `json:"files"`
	Summary            SuiteSummary `json:"summary"`
	Limits             []string     `json:"limits"`
}

// AssessSuite assesses files with a bounded worker pool under one suite
// deadline. A failure in one file is recorded for that file; it does not stop
// the others. Files left when the deadline passes are recorded as timed out.
func AssessSuite(parent context.Context, inputs []SuiteInput, contract []byte, opts Options) (SuiteReport, error) {
	var c Contract
	if contract != nil {
		var err error
		if c, err = ParseContract(contract); err != nil {
			return SuiteReport{}, err
		}
	}
	parser, cleanup, err := materializeParser()
	if err != nil {
		return SuiteReport{}, diagnostic("parser-unavailable")
	}
	defer cleanup()
	ctx, cancel := context.WithTimeout(parent, SuiteTimeout)
	defer cancel()
	files := make([]SuiteFile, len(inputs))
	jobs := make(chan int)
	var wg sync.WaitGroup
	for worker := 0; worker < suiteWorkers; worker++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for index := range jobs {
				files[index] = assessSuiteFile(ctx, inputs[index], contract, c, opts, parser)
			}
		}()
	}
	for index := range inputs {
		select {
		case jobs <- index:
		case <-ctx.Done():
			files[index] = SuiteFile{Path: inputs[index].Path, Error: stopped(ctx)}
		}
	}
	close(jobs)
	wg.Wait()
	return buildSuite(files, contract), nil
}

func assessSuiteFile(ctx context.Context, input SuiteInput, contract []byte, c Contract, opts Options, parser string) SuiteFile {
	file := SuiteFile{Path: input.Path, Error: input.Error}
	if file.Error != "" {
		return file
	}
	if len(input.Source) == 0 || len(input.Source) > MaxSource || !utf8.Valid(input.Source) {
		file.Error = "source-limit"
		return file
	}
	if ctx.Err() != nil {
		file.Error = stopped(ctx)
		return file
	}
	report, err := assessWithHelper(ctx, input.Source, contract, c, opts, parser, 10*time.Second)
	var failure *Error
	switch {
	case err == nil:
		file.Report = &report
	case ctx.Err() != nil:
		file.Error = stopped(ctx)
	case errors.As(err, &failure):
		file.Error = failure.Code
	default:
		file.Error = "helper-failed"
	}
	return file
}

// stopped is the code for a file the suite deadline or a cancellation stopped.
func stopped(ctx context.Context) string {
	if errors.Is(ctx.Err(), context.DeadlineExceeded) {
		return "suite-timeout"
	}
	return "cancelled"
}

func buildSuite(files []SuiteFile, contract []byte) SuiteReport {
	suite := SuiteReport{Version: SuiteVersion, Policy: Policy, Execution: "not_run", Completeness: "partial", Files: files,
		Summary: SuiteSummary{Files: len(files), Rules: map[string]int{}}, Limits: []string{}}
	if contract != nil {
		suite.RequirementsSHA256 = digest(contract)
	}
	limits := map[string]bool{}
	for _, file := range files {
		if file.Report == nil {
			suite.Summary.Failed++
			continue
		}
		suite.Summary.Assessed++
		suite.Summary.Tests += len(file.Report.Tests)
		counts, total := CountFindings(*file.Report)
		suite.Summary.Findings += total
		for rule, count := range counts {
			suite.Summary.Rules[rule] += count
		}
		for _, limit := range file.Report.Limits {
			if !limits[limit] {
				limits[limit] = true
				suite.Limits = append(suite.Limits, limit)
			}
		}
	}
	if suite.Summary.Failed > 0 {
		suite.Limits = append(suite.Limits, "Some files could not be assessed; their tests and findings are absent from this report and summary.")
	}
	return suite
}

// CountFindings counts a report's findings per rule, or per rule/code for
// analysis limits. Findings at the same location, such as one suite modifier
// applying to several tests, count once.
func CountFindings(report Report) (map[string]int, int) {
	type key struct {
		rule, requirement, outcome string
		at                         Location
	}
	counted := map[key]bool{}
	counts, total := map[string]int{}, 0
	for _, test := range report.Tests {
		for _, finding := range test.Findings {
			rule := FindingRule(finding)
			if k := (key{rule, finding.Requirement, finding.Outcome, finding.Location}); !counted[k] {
				counted[k] = true
				counts[rule]++
				total++
			}
		}
	}
	return counts, total
}

// FindingRule is the rule, qualified by its code when they differ.
func FindingRule(finding Finding) string {
	if finding.Code != finding.Rule {
		return finding.Rule + "/" + finding.Code
	}
	return finding.Rule
}
