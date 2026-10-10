// Package assessment provides advisory source assessment, separate from execution.
package assessment

import (
	"bytes"
	"context"
	"crypto/sha256"
	_ "embed"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/Quality-Max/9lives-runner/internal/runner"
	"github.com/Quality-Max/9lives-runner/internal/strictjson"
)

//go:embed analyzer.cjs
var analyzer string

const Policy = "assessment-source-v10"

// unmapped-outcome codes: a test that maps none of its requirement's
// outcomes is its own defect; an outcome mapped nowhere in the file is one
// gap however many tests reference the requirement.
const (
	CodeMapsNoOutcome  = "maps-no-outcome"
	CodeUnmappedInFile = "unmapped-in-file"
)

// ReportVersion is the version of a single-file assess report.
const ReportVersion = 3
const MaxSource = 1 << 20

var identifier = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9_.-]{0,79}$`)

type Outcome struct {
	ID          string `json:"id"`
	Description string `json:"description"`
}
type Requirement struct {
	ID               string    `json:"id"`
	Reference        string    `json:"reference"`
	Revision         string    `json:"revision"`
	ExpectedOutcomes []Outcome `json:"expectedOutcomes"`
}
type Contract struct {
	Version      int           `json:"version"`
	Requirements []Requirement `json:"requirements"`
}
type Location struct {
	Line   int `json:"line"`
	Column int `json:"column"`
}
type Assertion struct {
	Location
	// Site is the test's call site when the assertion is inside an inlined
	// helper; nil for one in the test body.
	Site      *Location `json:"site,omitempty"`
	Outcomes  []string  `json:"outcomes"`
	Unawaited bool      `json:"unawaited"`
	// Absence marks an assertion that something is absent or did not happen.
	Absence bool `json:"absence"`
	// AfterWait marks the first recognized assertion after a fixed wait, in
	// execution order through resolved helpers.
	AfterWait bool `json:"afterWait"`
}
type AnalysisLimit struct {
	Location
	Code string `json:"code"`
}

// Sleep is a fixed wait; Site is the test's call site when it is inside an
// inlined helper.
type Sleep struct {
	Location
	Site *Location `json:"site,omitempty"`
}

// ConditionalSkip is a suite modifier that may skip a test. Environment marks a
// condition that reads process.env. AfterExecution marks modifiers in after hooks.
type ConditionalSkip struct {
	Location
	Environment    bool `json:"environment"`
	AfterExecution bool `json:"afterExecution"`
}
type Fact struct {
	Location
	Title            string            `json:"title,omitempty"`
	Requirements     []string          `json:"requirements"`
	Assertions       []Assertion       `json:"assertions"`
	Sleeps           []Sleep           `json:"sleeps"`
	ConditionalSkips []ConditionalSkip `json:"conditionalSkips"`
	Disabled         *bool             `json:"disabled"`
	Exclusive        bool              `json:"exclusive"`
	Unsupported      bool              `json:"unsupported"`
	Limits           []AnalysisLimit   `json:"limits"`
}
type Facts struct {
	Version  int    `json:"version"`
	Compiler string `json:"compiler"`
	Tests    []Fact `json:"tests"`
	// UnrecognizedTestImports names modules a `test` binding was imported
	// from when none came from @playwright/test or @9l/playwright.
	UnrecognizedTestImports []string `json:"unrecognizedTestImports,omitempty"`
}
type Finding struct {
	Rule           string `json:"rule"`
	Classification string `json:"classification"`
	Dimension      string `json:"dimension"`
	Location
	// Site is the test's call site when the finding is inside a helper the
	// test calls; the location is then the helper's own line.
	Site        *Location `json:"site,omitempty"`
	Requirement string    `json:"requirement,omitempty"`
	Outcome     string    `json:"outcome,omitempty"`
	Message     string    `json:"message"`
	Suggestion  string    `json:"suggestion"`
	// Code is the specific reason: the limit code for analysis limits and the
	// rule itself for every other finding.
	Code string `json:"code"`
}
type Test struct {
	Location
	Title        string            `json:"title,omitempty"`
	Requirements []string          `json:"requirements"`
	Dimensions   map[string]string `json:"dimensions"`
	Findings     []Finding         `json:"findings"`
}
type Report struct {
	AgentProvenance    runner.ProvenanceAssessment `json:"agentProvenance"`
	Version            int                         `json:"version"`
	Policy             string                      `json:"policy"`
	SourceSHA256       string                      `json:"sourceSHA256"`
	RequirementsSHA256 string                      `json:"requirementsSHA256,omitempty"`
	Compiler           string                      `json:"compiler"`
	Execution          string                      `json:"execution"`
	Completeness       string                      `json:"completeness"`
	Limits             []string                    `json:"limits"`
	// UnrecognizedTestImports names the modules this file imports `test`
	// from that assess does not recognise; its tests were not assessed.
	UnrecognizedTestImports []string `json:"unrecognizedTestImports,omitempty"`
	Tests                   []Test   `json:"tests"`
}

func Decode(data []byte, target any) error {
	d := json.NewDecoder(bytes.NewReader(data))
	if err := strictjson.Value(d); err != nil {
		return errors.New("invalid assessment JSON")
	}
	if _, err := d.Token(); err != io.EOF {
		return errors.New("trailing assessment JSON")
	}
	d = json.NewDecoder(bytes.NewReader(data))
	d.DisallowUnknownFields()
	if err := d.Decode(target); err != nil {
		return errors.New("invalid assessment fields")
	}
	return nil
}

func ParseContract(data []byte) (Contract, error) {
	var c Contract
	if len(data) > 256<<10 || Decode(data, &c) != nil || c.Version != 1 || len(c.Requirements) == 0 || len(c.Requirements) > 128 {
		return c, errors.New("invalid requirement contract")
	}
	seen := map[string]bool{}
	outcomes := map[string]bool{}
	for _, r := range c.Requirements {
		if !identifier.MatchString(r.ID) || seen[r.ID] || len(r.Reference) == 0 || len(r.Reference) > 512 || len(r.Revision) == 0 || len(r.Revision) > 128 || len(r.ExpectedOutcomes) == 0 || len(r.ExpectedOutcomes) > 32 {
			return c, errors.New("invalid requirement definition")
		}
		seen[r.ID] = true
		for _, o := range r.ExpectedOutcomes {
			if !identifier.MatchString(o.ID) || outcomes[o.ID] || len(o.Description) == 0 || len(o.Description) > 1024 {
				return c, errors.New("invalid expected outcome")
			}
			outcomes[o.ID] = true
		}
	}
	return c, nil
}

func digest(data []byte) string { sum := sha256.Sum256(data); return hex.EncodeToString(sum[:]) }

// Options adjust what a report contains, not how source is analyzed.
type Options struct {
	// Titles includes literal test titles, which are source text, for local use.
	Titles bool
}

// Assess uses a bounded owned helper, forwarding only PATH. Source and contract
// prose remain in memory; output contains hashes, identifiers and locations,
// plus literal test titles only when requested. A nil contract skips the
// requirement and outcome mapping checks.
func Assess(ctx context.Context, source, contract []byte, opts Options) (Report, error) {
	var empty Report
	var c Contract
	if contract != nil {
		var err error
		if c, err = ParseContract(contract); err != nil {
			return empty, err
		}
	}
	if len(source) == 0 || len(source) > MaxSource || !utf8.Valid(source) {
		return empty, errors.New("source exceeds assessment limit or is empty")
	}
	parser, cleanup, err := materializeParser()
	if err != nil {
		return empty, diagnostic("parser-unavailable")
	}
	defer cleanup()
	return assessWithHelper(ctx, source, contract, c, opts, parser, 10*time.Second)
}

func assessWithHelper(parent context.Context, source, contract []byte, c Contract, opts Options, parser string, timeout time.Duration) (Report, error) {
	var empty Report
	ctx, cancel := context.WithTimeout(parent, timeout)
	defer cancel()
	input, _ := json.Marshal(struct {
		Source string `json:"source"`
		Titles bool   `json:"titles"`
	}{string(source), opts.Titles})
	// Windows command lines are limited to 32,767 UTF-16 code units. Keep
	// the embedded analyzer in an owned file instead of passing its source as
	// a large -e argument; only trusted code is materialized or executed.
	helperDir, err := os.MkdirTemp("", "9lives-assessment-helper-")
	if err != nil {
		return empty, diagnostic("parser-unavailable")
	}
	defer os.RemoveAll(helperDir)
	helper := filepath.Join(helperDir, "analyzer.cjs")
	if err := os.WriteFile(helper, []byte(analyzer+"\nmain(process.argv[2]);"), 0600); err != nil {
		return empty, diagnostic("parser-unavailable")
	}
	command := exec.Command("node", helper, parser)
	command.Env = []string{}
	if path, ok := os.LookupEnv("PATH"); ok {
		command.Env = append(command.Env, "PATH="+path)
	}
	command.Stdin = bytes.NewReader(input)
	output := &boundedOutput{cancel: cancel}
	command.Stdout = output
	command.Stderr = io.Discard
	err = runner.RunOwnedCommand(ctx, command)
	if output.overflow {
		return empty, diagnostic("output-limit")
	}
	if parent.Err() != nil {
		if errors.Is(parent.Err(), context.DeadlineExceeded) {
			return empty, diagnostic("timeout")
		}
		return empty, diagnostic("cancelled")
	}
	if ctx.Err() != nil {
		return empty, diagnostic("timeout")
	}
	if errors.Is(err, exec.ErrNotFound) {
		return empty, diagnostic("node-unavailable")
	}
	if err != nil {
		var failure struct {
			Version int    `json:"version"`
			Error   string `json:"error"`
		}
		if Decode(output.Bytes(), &failure) == nil && failure.Version == 7 {
			switch failure.Error {
			case "parser-unavailable", "syntax", "annotation", "limit", "helper-failed":
				return empty, diagnostic(failure.Error)
			}
		}
		return empty, diagnostic("helper-failed")
	}
	var facts Facts
	if Decode(output.Bytes(), &facts) != nil || !validFacts(facts, source, opts.Titles) {
		return empty, diagnostic("invalid-evidence")
	}
	return Build(source, contract, c, facts), nil
}

type boundedOutput struct {
	buffer   bytes.Buffer
	cancel   context.CancelFunc
	overflow bool
}

func (b *boundedOutput) Write(p []byte) (int, error) {
	if b.buffer.Len()+len(p) > 1<<20 {
		b.overflow = true
		b.cancel()
		return 0, errors.New("assessment output limit")
	}
	return b.buffer.Write(p)
}

func (b *boundedOutput) Bytes() []byte { return b.buffer.Bytes() }

var moduleName = regexp.MustCompile(`^(?:[@A-Za-z0-9._/-]{1,214}|\(unnamed module\))$`)

func validFacts(f Facts, source []byte, titles bool) bool {
	if f.Version != 7 || f.Compiler != parserVersion || f.Tests == nil || len(f.Tests) > 256 || len(f.UnrecognizedTestImports) > 8 || len(f.UnrecognizedTestImports) > 0 && len(f.Tests) > 0 {
		return false
	}
	for _, module := range f.UnrecognizedTestImports {
		if !moduleName.MatchString(module) {
			return false
		}
	}
	lines := bytes.Split(source, []byte("\n"))
	valid := func(p Location) bool {
		return p.Line > 0 && p.Line <= len(lines) && p.Column > 0 && p.Column <= len(lines[p.Line-1])+1
	}
	ids := func(values []string) bool {
		seen := map[string]bool{}
		for _, id := range values {
			if !identifier.MatchString(id) || seen[id] {
				return false
			}
			seen[id] = true
		}
		return len(values) <= 128
	}
	seen, modifiers := map[Location]bool{}, map[Location]bool{}
	total := 0
	for _, t := range f.Tests {
		if !valid(t.Location) || seen[t.Location] || t.Requirements == nil || t.Assertions == nil || t.Sleeps == nil || t.ConditionalSkips == nil || len(t.ConditionalSkips) > 16 || t.Disabled == nil ||
			(!titles && t.Title != "") || len(t.Title) > 1024 || !utf8.ValidString(t.Title) || !ids(t.Requirements) || t.Limits == nil || len(t.Limits) > len(analysisLimits) || t.Unsupported != (len(t.Limits) > 0) {
			return false
		}
		seen[t.Location] = true
		codes := map[string]bool{}
		for _, limit := range t.Limits {
			if !valid(limit.Location) || analysisLimits[limit.Code] == "" || codes[limit.Code] {
				return false
			}
			codes[limit.Code] = true
		}
		for _, a := range t.Assertions {
			if !valid(a.Location) || (a.Site != nil && !valid(*a.Site)) || !ids(a.Outcomes) {
				return false
			}
		}
		for _, p := range t.Sleeps {
			if !valid(p.Location) || (p.Site != nil && !valid(*p.Site)) {
				return false
			}
		}
		applied := map[Location]bool{}
		for _, p := range t.ConditionalSkips {
			if !valid(p.Location) || applied[p.Location] {
				return false
			}
			applied[p.Location] = true
			modifiers[p.Location] = true
		}
		total += len(t.Assertions) + len(t.Sleeps) + len(t.Limits)
	}
	// A shared suite modifier counts once, however many tests it applies to.
	return total+len(modifiers) <= 2048
}

// Build derives findings from parsed facts. A nil contract means none was
// supplied: requirement references stay unchecked and purpose stays unknown.
func Build(source, contract []byte, c Contract, facts Facts) Report {
	report := Report{AgentProvenance: runner.UnknownAgentProvenance(), Version: ReportVersion, Policy: Policy, SourceSHA256: digest(source), Compiler: facts.Compiler, Execution: "not_run", Completeness: "partial", Tests: []Test{}, Limits: []string{
		"Named test/expect imports and inline tests only; dynamic generation, custom fixtures and helper assertions may be omitted.",
		"Outcome annotations are reviewed coverage claims, not semantic or behavioral proof. Source-only assessment cannot establish correctness.",
		"No test, configuration or application module is executed. Runtime evidence requires a separate attributed control experiment.",
	}}
	if len(facts.UnrecognizedTestImports) > 0 {
		report.UnrecognizedTestImports = facts.UnrecognizedTestImports
		report.Limits = append(report.Limits, "unrecognized-test-import "+strings.Join(facts.UnrecognizedTestImports, ", ")+": this file imports `test` from a module assess does not recognise (only @playwright/test and @9l/playwright), so its tests were not assessed.")
	}
	checked := contract != nil
	if checked {
		report.RequirementsSHA256 = digest(contract)
	} else {
		report.Limits = append(report.Limits, "No requirement contract was supplied; requirement and outcome mapping checks were not run.")
	}
	requirements := map[string]Requirement{}
	for _, r := range c.Requirements {
		requirements[r.ID] = r
	}
	// A requirement's outcomes may be spread over sibling tests: an outcome
	// mapped by a test that references the same requirement and is not disabled
	// or conditionally skipped before execution covers it for other tests that
	// reference the requirement. After-hook modifiers do not exclude coverage
	// because the test body has already run. A disabled test, a pre-execution
	// conditional skip, or a test that does not reference the requirement never
	// lends coverage; its own mapping still counts for itself. A test that maps
	// none of the outcomes claims a requirement it does not check and is reported
	// for every outcome. Mapping in other files is not considered.
	mapped := map[string]map[string]bool{}
	for _, fact := range facts.Tests {
		if fact.Disabled != nil && *fact.Disabled {
			continue
		}
		preExecutionSkip := false
		for _, skip := range fact.ConditionalSkips {
			if !skip.AfterExecution {
				preExecutionSkip = true
				break
			}
		}
		if preExecutionSkip {
			continue
		}
		for _, requirement := range fact.Requirements {
			if mapped[requirement] == nil {
				mapped[requirement] = map[string]bool{}
			}
			for _, a := range fact.Assertions {
				for _, id := range a.Outcomes {
					mapped[requirement][id] = true
				}
			}
		}
	}
	for _, fact := range facts.Tests {
		t := Test{Location: fact.Location, Title: fact.Title, Requirements: fact.Requirements, Dimensions: map[string]string{"purpose": "unknown", "intentAlignment": "unknown", "assertionAdequacy": "unknown", "runtimeEvidence": "unknown", "engineeringQuality": "unknown"}, Findings: []Finding{}}
		add := func(rule, classification, dimension, requirement, outcome, message, suggestion string, loc Location) {
			t.Findings = append(t.Findings, Finding{Rule: rule, Classification: classification, Dimension: dimension, Location: loc, Requirement: requirement, Outcome: outcome, Message: message, Suggestion: suggestion, Code: rule})
			if classification == "demonstrated" || classification == "suspected" {
				t.Dimensions[dimension] = "concern"
			}
		}
		for _, limit := range fact.Limits {
			add("analysis-limit", "unsupported", "assertionAdequacy", "", "", analysisLimits[limit.Code], "Review the unsupported syntax and related helpers manually.", limit.Location)
			t.Findings[len(t.Findings)-1].Code = limit.Code
		}
		// Without a contract, references are reported but cannot be checked.
		references := fact.Requirements
		if !checked {
			references = nil
		}
		known := len(references) > 0
		for _, id := range references {
			r, ok := requirements[id]
			if !ok {
				known = false
				add("unknown-requirement", "unsupported", "purpose", id, "", "The requirement reference is absent from the supplied contract.", "Supply the independently reviewed requirement.", fact.Location)
				continue
			}
			own := map[string]bool{}
			for _, a := range fact.Assertions {
				for _, outcome := range a.Outcomes {
					own[outcome] = true
				}
			}
			checks := false
			for _, o := range r.ExpectedOutcomes {
				checks = checks || own[o.ID]
			}
			for _, o := range r.ExpectedOutcomes {
				if !checks {
					add("unmapped-outcome", "suspected", "intentAlignment", id, o.ID, "The test references the requirement but maps none of its outcomes; helpers may protect it.", "Map an assertion in this test to an outcome of the requirement, or reference the requirement it checks.", fact.Location)
					t.Findings[len(t.Findings)-1].Code = CodeMapsNoOutcome
				} else if !mapped[id][o.ID] && !own[o.ID] {
					add("unmapped-outcome", "suspected", "intentAlignment", id, o.ID, "A required outcome has no declared direct assertion mapping in an enabled test of this file that references the requirement; helpers or other files may protect it.", "Review the gap and map an assertion in this file that checks this outcome.", fact.Location)
					t.Findings[len(t.Findings)-1].Code = CodeUnmappedInFile
				}
			}
		}
		if known {
			t.Dimensions["purpose"] = "supported"
		}
		// Even complete declared mappings leave semantic alignment unknown.
		if len(fact.Assertions) == 0 && !fact.Unsupported {
			add("no-direct-assertion", "suspected", "assertionAdequacy", "", "", "No direct expect matcher was recognized; helper assertions remain unknown.", "Inspect helpers and add an attributable assertion for the requirement.", fact.Location)
		}
		for _, a := range fact.Assertions {
			if a.Unawaited {
				add("unawaited-assertion", "suspected", "engineeringQuality", "", "", "A recognized asynchronous assertion is neither directly awaited nor returned.", "Await or return the assertion, or verify how its promise is consumed.", a.Location)
				t.Findings[len(t.Findings)-1].Site = a.Site
			}
		}
		for _, p := range fact.Sleeps {
			add("fixed-wait", "demonstrated", "engineeringQuality", "", "", "A waitForTimeout call is present; timing alone does not prove readiness.", "Prefer an observable readiness condition where applicable.", p.Location)
			t.Findings[len(t.Findings)-1].Site = p.Site
		}
		// A fixed wait directly followed by an absence assertion passes when the
		// application is merely slow, so it can hide the defect it should catch.
		for _, next := range fact.Assertions {
			if next.Absence && next.AfterWait {
				add("absence-after-wait", "suspected", "assertionAdequacy", "", "", "The first recognized assertion after a fixed wait checks that something is absent or did not happen; if the application is merely slow, it passes without the outcome having occurred.", "Assert a positive completion signal first, such as the response, a confirmation or the final URL, then check the absence.", next.Location)
				t.Findings[len(t.Findings)-1].Site = next.Site
			}
		}
		if fact.Exclusive {
			add("exclusive-test", "demonstrated", "engineeringQuality", "", "", "A syntactic test.only call is present; check whether it limits suite execution.", "Remove an active only before running the complete suite.", fact.Location)
		}
		if fact.Disabled != nil && *fact.Disabled {
			add("disabled-test", "demonstrated", "engineeringQuality", "", "", "A syntactic test.skip/test.fixme declaration, unconditional suite modifier or enclosing skipped/fixme suite is present.", "Review why the test is disabled before relying on it to protect the requirement.", fact.Location)
		}
		for _, p := range fact.ConditionalSkips {
			if p.Environment {
				add("environment-skip", "suspected", "engineeringQuality", "", "", "A test.skip/test.fixme modifier applying to this test's suite depends on an environment variable; the test may not run in some environments, such as CI.", "Confirm which environments skip it and that at least one required run still executes this test.", p.Location)
				continue
			}
			add("conditional-skip", "informational", "engineeringQuality", "", "", "A conditional or after-hook test.skip/test.fixme modifier applies to this test's suite; whether it skips this test is not evaluated.", "Confirm the condition and reason; while it holds, this test does not protect the requirement.", p.Location)
		}
		report.Tests = append(report.Tests, t)
	}
	return report
}
