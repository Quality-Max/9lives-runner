// Package playwright implements the initial local Playwright runner adapter.
package playwright

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strings"
	"unicode/utf8"

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
	seen := map[string]bool{}
	var visit func(any)
	visit = func(value any) {
		switch node := value.(type) {
		case map[string]any:
			if message, ok := node["message"].(string); ok && message != "" {
				// Playwright reports each error as result.error and in
				// result.errors; keep one copy, without terminal escapes.
				message = withoutSourceFrames(runner.StripTerminalEscapes(message))
				if !seen[message] {
					seen[message] = true
					values = append(values, message)
				}
			}
			keys := make([]string, 0, len(node))
			for key := range node {
				keys = append(keys, key)
			}
			sort.Strings(keys) // a stable order keeps prompts and reasons reproducible
			for _, key := range keys {
				visit(node[key])
			}
		case []any:
			for _, child := range node {
				visit(child)
			}
		}
	}
	visit(payload)
	// Redact before clipping: a clip through a key/value pair would dodge the
	// redaction patterns.
	context := runner.RedactText(strings.TrimSpace(strings.Join(values, "\n")))
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
		return summary + boundText(context, 3000-len(summary))
	}
	return context
}

// boundText cuts text at limit bytes on a rune boundary, so bounded evidence
// stays valid UTF-8.
func boundText(text string, limit int) string {
	if len(text) <= limit {
		return text
	}
	cut := limit
	for cut > 0 && !utf8.RuneStart(text[cut]) {
		cut--
	}
	return text[:cut]
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

// SelectLine narrows a job to the test at line with Playwright's own
// `file:line` filter.
func (Adapter) SelectLine(job runner.Job, line int) (runner.Job, error) {
	return SelectLine(job, line)
}

// SelectLine appends :line to the job's file filter argument.
func SelectLine(job runner.Job, line int) (runner.Job, error) {
	relative, err := filepath.Rel(job.WorkDir, job.Spec)
	if err != nil {
		return job, fmt.Errorf("cannot resolve the spec for line selection")
	}
	filter := TestFileFilter(job.WorkDir, relative)
	command := append([]string{}, job.Command...)
	for i, argument := range command {
		if argument == filter {
			command[i] = fmt.Sprintf("%s:%d", filter, line)
			job.Command = command
			return job, nil
		}
	}
	return job, fmt.Errorf("cannot select a line: the planned command has no file filter")
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
				binary := installedBinary(directory)
				if binary == "" {
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

// installedBinary finds node_modules/.bin/playwright in the project or, for
// a workspace package whose dependencies are hoisted, in an ancestor
// directory. It never downloads anything.
func installedBinary(project string) string {
	name := "playwright"
	if runtime.GOOS == "windows" {
		name += ".cmd"
	}
	for directory := project; ; directory = filepath.Dir(directory) {
		binary := filepath.Join(directory, "node_modules", ".bin", name)
		if info, err := os.Stat(binary); err == nil && !info.IsDir() {
			return binary
		}
		if filepath.Dir(directory) == directory {
			return ""
		}
	}
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
	// Config names the configuration Playwright used, so a spec it found no
	// tests in can be explained.
	Config *struct {
		ConfigFile string `json:"configFile"`
		Projects   []struct {
			Name    string `json:"name"`
			TestDir string `json:"testDir"`
		} `json:"projects"`
	} `json:"config"`
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
		return runner.Validation{}, fmt.Errorf("Playwright report contains no completed tests: Playwright found no tests in this spec%s; check that the config includes it (testDir, testMatch, testIgnore), or choose another config or project with --config or --project", configSummary(parsed))
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

// configSummary names the config file and its projects' test directories,
// relative to the config, as " under playwright.config.ts (testDir tests)".
func configSummary(parsed report) string {
	if parsed.Config == nil || parsed.Config.ConfigFile == "" {
		return ""
	}
	base := filepath.Dir(parsed.Config.ConfigFile)
	var dirs []string
	seen := map[string]bool{}
	for _, project := range parsed.Config.Projects {
		dir := project.TestDir
		if relative, err := filepath.Rel(base, dir); err == nil && !strings.HasPrefix(relative, "..") {
			dir = relative
		}
		dir = filepath.ToSlash(dir)
		if dir != "" && !seen[dir] && len(dirs) < 4 {
			seen[dir] = true
			dirs = append(dirs, dir)
		}
	}
	summary := " under " + filepath.Base(parsed.Config.ConfigFile)
	if len(dirs) > 0 {
		summary += " (testDir " + strings.Join(dirs, ", ") + ")"
	}
	return summary
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

type failureReport struct {
	Suites []failureSuite `json:"suites"`
}

type failureSuite struct {
	Title  string         `json:"title"`
	Suites []failureSuite `json:"suites"`
	Specs  []struct {
		Title string `json:"title"`
		File  string `json:"file"`
		Line  int    `json:"line"`
		Tests []struct {
			Status      string             `json:"status"`
			ProjectName string             `json:"projectName"`
			Annotations []reportAnnotation `json:"annotations"`
			Results     []struct {
				Status      string             `json:"status"`
				Annotations []reportAnnotation `json:"annotations"`
				Errors      []struct {
					Message  string `json:"message"`
					Location *struct {
						File string `json:"file"`
						Line int    `json:"line"`
					} `json:"location"`
				} `json:"errors"`
				Attachments []struct {
					Name        string `json:"name"`
					ContentType string `json:"contentType"`
					Path        string `json:"path"`
				} `json:"attachments"`
			} `json:"results"`
		} `json:"tests"`
	} `json:"specs"`
}

type reportAnnotation struct {
	Type        string `json:"type"`
	Description string `json:"description"`
}

// Annotations lists every test's annotations from its last attempt, which
// includes ones the test pushed at run time, such as a note that it returned
// early. Static annotations stand in for a test without attempts.
func (Adapter) Annotations(raw []byte) []runner.TestAnnotation {
	var parsed failureReport
	if json.Unmarshal(raw, &parsed) != nil {
		return nil
	}
	annotations := []runner.TestAnnotation{}
	var walk func(failureSuite, []string)
	walk = func(current failureSuite, titles []string) {
		for _, spec := range current.Specs {
			for _, test := range spec.Tests {
				reported := test.Annotations
				if n := len(test.Results); n > 0 && test.Results[n-1].Annotations != nil {
					reported = test.Results[n-1].Annotations
				}
				title := strings.Join(append(append([]string{}, titles...), spec.Title), " › ")
				if test.ProjectName != "" {
					title = "[" + test.ProjectName + "] " + title
				}
				for _, annotation := range reported {
					if len(annotations) == runner.MaxReportedAnnotations || annotation.Type == "" {
						continue
					}
					annotations = append(annotations, runner.TestAnnotation{
						Test:        boundedText(title, 512),
						Type:        boundedText(annotation.Type, runner.MaxAnnotationTypeBytes),
						Description: boundedText(annotation.Description, runner.MaxAnnotationDescriptionBytes),
					})
				}
			}
		}
		for _, child := range current.Suites {
			walk(child, append(append([]string{}, titles...), child.Title))
		}
	}
	for _, file := range parsed.Suites {
		walk(file, nil)
	}
	return annotations
}

// boundedText is text without terminal escapes or line breaks, redacted
// and cut on a rune boundary.
func boundedText(text string, limit int) string {
	text = runner.RedactText(strings.Join(strings.Fields(runner.StripTerminalEscapes(text)), " "))
	if len(text) <= limit {
		return text
	}
	cut := limit
	for cut > 0 && !utf8.RuneStart(text[cut]) {
		cut--
	}
	return text[:cut] + "…"
}

// Failures lists each unexpected test of a validated report with its failing
// line, the start of its error and the files Playwright attached, such as
// error-context.md with the page's ARIA snapshot. It reads the last failed
// attempt, so retries report the final failure.
func (Adapter) Failures(raw []byte, workDir string) []runner.TestFailure {
	var parsed failureReport
	if json.Unmarshal(raw, &parsed) != nil {
		return nil
	}
	failures := []runner.TestFailure{}
	var walk func(failureSuite, []string)
	walk = func(current failureSuite, titles []string) {
		for _, spec := range current.Specs {
			for _, test := range spec.Tests {
				if test.Status != "unexpected" || len(failures) == runner.MaxReportedFailures {
					continue
				}
				failure := runner.TestFailure{Title: strings.Join(append(append([]string{}, titles...), spec.Title), " › ")}
				if test.ProjectName != "" {
					failure.Title = "[" + test.ProjectName + "] " + failure.Title
				}
				// A spec controls its own titles; keep the bound on evidence.
				failure.Title = boundText(failure.Title, 512)
				if spec.File != "" && spec.Line > 0 {
					failure.Location = fmt.Sprintf("%s:%d", filepath.ToSlash(spec.File), spec.Line)
				}
				for index := len(test.Results) - 1; index >= 0; index-- {
					result := test.Results[index]
					if result.Status != "failed" && result.Status != "timedOut" {
						continue
					}
					if len(result.Errors) > 0 {
						first := result.Errors[0]
						failure.Message = failureMessage(first.Message)
						if first.Location != nil && first.Location.Line > 0 {
							if relative, ok := withinDirectory(workDir, first.Location.File); ok {
								failure.Location = fmt.Sprintf("%s:%d", filepath.ToSlash(relative), first.Location.Line)
							}
						}
					}
					for _, attachment := range result.Attachments {
						if attachment.Path == "" {
							continue // inline bodies are not files
						}
						path := attachment.Path
						if !filepath.IsAbs(path) {
							path = filepath.Join(workDir, path)
						}
						if !attachmentInProject(workDir, path) {
							continue
						}
						reference := runner.TestAttachment{Name: attachment.Name, ContentType: attachment.ContentType, Path: path}
						if info, err := os.Lstat(path); err == nil && info.Mode().IsRegular() {
							reference.Bytes = info.Size()
						}
						failure.Attachments = append(failure.Attachments, reference)
					}
					break
				}
				failures = append(failures, failure)
			}
		}
		for _, child := range current.Suites {
			walk(child, append(append([]string{}, titles...), child.Title))
		}
	}
	// The top-level suite is the file; its title repeats the location.
	for _, file := range parsed.Suites {
		walk(file, nil)
	}
	return failures
}

// failureMessage is the start of an error without terminal escapes or source
// frames, redacted and cut on a rune boundary.
func failureMessage(message string) string {
	message = runner.RedactText(withoutSourceFrames(runner.StripTerminalEscapes(message)))
	if len(message) <= runner.MaxFailureMessageBytes {
		return message
	}
	return boundText(message, runner.MaxFailureMessageBytes) + "…"
}

// attachmentInProject keeps attachment references inside the project, also
// through symlinked directories, so a report cannot point 9l at other files.
func attachmentInProject(workDir, path string) bool {
	if _, ok := withinDirectory(workDir, path); !ok {
		return false
	}
	realDir, dirErr := filepath.EvalSymlinks(workDir)
	realPath, pathErr := filepath.EvalSymlinks(path)
	if dirErr != nil || pathErr != nil {
		return pathErr != nil && os.IsNotExist(pathErr) // a missing file is only a reference
	}
	_, ok := withinDirectory(realDir, realPath)
	return ok
}

// withinDirectory returns path relative to directory when it is inside it.
func withinDirectory(directory, path string) (string, bool) {
	if directory == "" || path == "" {
		return "", false
	}
	relative, err := filepath.Rel(directory, path)
	if err != nil || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) || filepath.IsAbs(relative) {
		return "", false
	}
	return relative, true
}

// maxPageSnapshotBytes bounds the ARIA snapshot read for healing.
const maxPageSnapshotBytes = 64 << 10

// typedValue matches the value Playwright prints after an input's name, as
// in `- textbox "Token": s3cr3t`.
var typedValue = regexp.MustCompile(`(?m)^(\s*- (?:textbox|searchbox|combobox|spinbutton|slider)\b[^:"\n]*(?:"(?:\\.|[^"\\])*")?(?: \[[^\]\n]*\])*):[ \t].*$`)

// ReadAttachment reads at most limit bytes of a regular attachment file.
// FIFOs, devices and directories are refused, so a report cannot block or
// flood the reader.
func ReadAttachment(path string, limit int64) ([]byte, error) {
	info, err := os.Lstat(path)
	if err != nil || !info.Mode().IsRegular() {
		return nil, fmt.Errorf("attachment is not a regular file")
	}
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	raw, err := io.ReadAll(io.LimitReader(file, limit+1))
	if err != nil || int64(len(raw)) > limit {
		return nil, fmt.Errorf("attachment unreadable or too large")
	}
	return raw, nil
}

var pageSnapshotBlock = regexp.MustCompile("(?s)(?:^|\n)# Page snapshot\\s*\n```yaml\n(.*?)\n```")

// SanitizeAttachmentText prepares page-snapshot text taken from an attachment
// to leave the machine: values typed into fields are dropped and the rest is
// redacted like evidence. Callers bound the result.
func SanitizeAttachmentText(text string) string {
	return runner.RedactText(typedValue.ReplaceAllString(text, "$1"))
}

// PageSnapshot returns the ARIA snapshot of the page from a failed test's
// error-context.md, at most 64 KiB, or "" when it has none.
func PageSnapshot(failures []runner.TestFailure) string {
	for _, failure := range failures {
		for _, attachment := range failure.Attachments {
			if attachment.Name != "error-context" || attachment.Bytes > 1<<20 {
				continue
			}
			raw, err := ReadAttachment(attachment.Path, 1<<20)
			if err != nil {
				return ""
			}
			m := pageSnapshotBlock.FindSubmatch(raw)
			if m == nil || len(m[1]) > maxPageSnapshotBytes {
				return ""
			}
			// The snapshot leaves the machine in a provider prompt: drop values
			// typed into fields and redact what looks like a secret.
			return SanitizeAttachmentText(string(m[1]))
		}
	}
	return ""
}
