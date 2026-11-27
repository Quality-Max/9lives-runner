package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"syscall"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/Quality-Max/9lives-runner/internal/adapters/playwright"
	"github.com/Quality-Max/9lives-runner/internal/adapters/playwrightsdk"
	"github.com/Quality-Max/9lives-runner/internal/assessment"
	"github.com/Quality-Max/9lives-runner/internal/goals"
	"github.com/Quality-Max/9lives-runner/internal/healing"
	"github.com/Quality-Max/9lives-runner/internal/healing/tier2"
	"github.com/Quality-Max/9lives-runner/internal/jev"
	"github.com/Quality-Max/9lives-runner/internal/jevassist"
	"github.com/Quality-Max/9lives-runner/internal/runner"
)

// Release builds set this with -ldflags "-X main.version=<version>".
var version = "0.7.3"

func main() {
	code := run(os.Args[1:], os.Stdout, os.Stderr)
	offerAutomaticUpdate(os.Args[1:], code)
	os.Exit(code)
}

func run(args []string, out, errOut io.Writer) int {
	if len(args) == 0 || args[0] == "--help" || args[0] == "-h" {
		usage(out)
		starHint(out, errOut)
		return 0
	}
	if args[0] == "--version" || args[0] == "version" {
		return versionCommand(args[1:], out, errOut)
	}

	switch args[0] {
	case "update":
		return updateCommand(args[1:], out, errOut)
	case "doctor":
		return doctorCommand(args[1:], out, errOut)
	case "plan", "run":
		return runCommand(args[0], args[1:], out, errOut)
	case "status", "result", "cancel":
		return stateCommand(args[0], args[1:], out, errOut)
	case "diagnose":
		return diagnoseCommand(args[1:], out, errOut)
	case "heal", "heal-native":
		return healCommand(args[1:], out, errOut)
	case "mcp":
		return mcpCommand(args[1:], os.Stdin, out, errOut)
	case "tier1":
		return tier1Command(args[1:], os.Stdin, out, errOut)
	case "assess":
		return assessCommand(args[1:], out, errOut)
	case "provenance":
		return provenanceCommand(args[1:], out, errOut)
	case "prove":
		return proveCommand(args[1:], out, errOut)
	case "confirm":
		return confirmCommand(args[1:], out, errOut)
	case "scan":
		return scanCommand(args[1:], out, errOut)
	case "inspect":
		return inspectCommand(args[1:], out, errOut)
	case "star":
		return starCommand(args[1:], out, errOut)
	default:
		fmt.Fprintf(errOut, "9l: unknown command %q\n", args[0])
		usage(errOut)
		return 2
	}
}

func usage(w io.Writer) {
	fmt.Fprint(w, `9l — 9lives Go runner

Usage:
  9l plan <spec-or-glob>... [--format text|json] [--max-jobs N]
  9l run  <spec-or-glob|spec:LINE>... [--workers N] [--timeout D] [--deadline D] [--pass-env NAME]...
          [--grep RE] [--grep-invert RE]  # Playwright title filters
          [--config FILE] [--project NAME]...  # Playwright config and projects to run with
          [--reporter html,...]  # also run project reporters beside the evidence reporter
          [--keep-attachments]  # copy failed tests' error context, screenshots, traces into receipts
          [--show-output]  # stream test stdout/stderr to stderr while it runs; never saved, redaction best effort;
          # with --sdk add --reporter list to see a spec's console.log
          [--sdk]  # opt-in @9l/playwright engine protocol
          [--target web:<app-alias>|android:<app-alias>|ios:<app-alias>|android-fake:<app-alias>]...  # with --sdk; one attempt per target project
          [--failure-details]  # with --sdk, record failed test titles and errors
          [--pin-skip "<file> › <title>"]...  # with --sdk, accept a declared skip
          [--headed]  # show the browser, one job at a time unless --workers is set
          [--junit FILE]  # also write a JUnit XML report; an incomplete run is reported as errors
          [--verify CHECKS.json]  # experimental, one spec: after the test process stops, 9l itself probes the declared
                                  # HTTP GET/HEAD postconditions; a contradicted check fails the run (docs/verify.md)
  9l status <run-id> [--receipt-dir DIR]
  9l result <run-id> [--format text|json] [--receipt-dir DIR] [--junit FILE]
  9l cancel <run-id> [--receipt-dir DIR]
  9l heal <spec> [--provider NAME|none] [--model NAME] [--jev [--jev-images off|synthetic]] [--yes] [--run-timeout D] [--pass-env NAME]... [--format json|text]
          # verified locator healing: offline Tier 1, then an agent CLI (its own login) or API (heal-native is an alias)
  9l diagnose <spec> [--jev [--jev-model MODEL] [--jev-images off|synthetic]] [--run-timeout D] [--pass-env NAME]... [--receipt-dir DIR] [--format text|json]
          # experimental: run once; on failure report observed facts and, with --jev, one bounded Jev diagnosis; never changes the result
  9l mcp [--pass-env NAME]... [--provider NAME|none]  # MCP server on stdio: run_test, heal_test, assess_test, confirm_finding,
          # scan_url, inspect_page, inspect_screen, generate_playwright_repro
  9l scan <url> [--checks LIST] [--format markdown|json|issue] [--baseline FILE [--fail-on-new]] [--allow-private-network]
          # one Chromium page load: console, links, accessibility, vitals, SEO, headers, cookies, mixed content, weight
  9l inspect <url> [--format markdown|json] [--allow-private-network]  # ranked locators and testability
  9l inspect --android [--serial SERIAL] [--package PKG] | --ios [--udid UDID] [--wda-products DIR]  # test IDs on a device screen
          # scan and inspect: [--viewport WxH] [--storage-state FILE --acknowledge-private-content]
  9l tier1 --format json  # one offline version:1 JSON proposal request on stdin
  9l assess <spec|dir|'glob'>... [--requirements <contract.json>] [--format text|json] [--titles] [--limits]
          # text groups analysis limits per file by code; --limits lists each location
  9l prove <spec> [--faults KINDS] [--max-faults N] [--paths] [--format text|json]  # experimental: inject network faults
          # needs @9l/playwright installed and a spec importing test from it; faults reach only fetch/XHR
          # made through its page or context fixtures, not Node-side fetch or database clients
  9l confirm <spec> --unfixed REV [--fixed REV] [--finding-id ID] [--format text|json]
          # experimental: does a reproduction spec fail before a fix and pass after it? needs @9l/playwright
  9l provenance <spec> --agent <id>  # creation snapshot JSON
  9l doctor [--sdk [spec]] [--android] [--paths] [--format text|json]  # read-only local prerequisite checks
  9l doctor --jev[=false] [--jev-model MODEL] [--jev-images off|synthetic|masked] [--format text|json]  # local Jev configuration; no API request
  9l version [--format text|json]  # json: contract versions for host integrations
  9l update [--check | --yes]  # check Latest; ask before replacing the runner
  9l star [--gh]  # open the GitHub repository; --gh stars it with your gh login
  # assess/run accept --agent-provenance <snapshot.json> for branch, source and local-import checks

The Go runner currently executes Playwright specs from existing projects.
It never reports an incomplete run green. run exits 0 passed, 1 failed,
2 usage or setup error, 3 incomplete.
`)
}

func assessCommand(args []string, out, errOut io.Writer) int {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	return assessContext(ctx, args, out, errOut)
}

// assessContext runs `9l assess` until ctx ends, which stops the analysis
// helper; the MCP assess_test tool passes its request context.
func assessContext(ctx context.Context, args []string, out, errOut io.Writer) int {
	fs := flag.NewFlagSet("assess", flag.ContinueOnError)
	fs.SetOutput(errOut)
	requirements := fs.String("requirements", "", "shared reviewed requirement contract; without it, requirement checks are not run")
	format := fs.String("format", "text", "text or json advisory report")
	agentRecord := fs.String("agent-provenance", "", "agent creation snapshot JSON")
	titles := fs.Bool("titles", false, "include literal test titles (source text) for local use")
	limits := fs.Bool("limits", false, "text: list every analysis-limit location instead of one count per code and file")
	var screenArgs stringList
	fs.Var(&screenArgs, "screens", "saved `9l inspect --android|--ios --format json` result, or a glob of them; check target tests' test IDs against these screens (repeatable)")
	if fs.Parse(flagsFirst(args, map[string]bool{"--requirements": true, "--format": true, "--agent-provenance": true, "--screens": true})) != nil {
		return 2
	}
	if fs.NArg() == 0 || (*format != "json" && *format != "text") {
		fmt.Fprintln(errOut, "9l: assess requires a spec, directory or pattern and text or json format")
		return 2
	}
	// An empty path, e.g. from an unset variable, must not silently drop the
	// requirement checks. Omit the flag to assess without a contract.
	emptyRequirements := false
	fs.Visit(func(f *flag.Flag) {
		emptyRequirements = emptyRequirements || (f.Name == "requirements" && *requirements == "")
	})
	if emptyRequirements {
		fmt.Fprintln(errOut, "9l: --requirements needs a contract path; omit it to assess without one")
		return 2
	}
	read := func(path string, limit int64) ([]byte, error) {
		info, err := os.Stat(path)
		if err != nil || !info.Mode().IsRegular() || info.Size() > limit {
			return nil, errors.New("assessment input unavailable, non-regular or exceeds limit")
		}
		file, err := os.Open(path)
		if err != nil {
			return nil, errors.New("assessment input unavailable")
		}
		defer file.Close()
		data, err := io.ReadAll(io.LimitReader(file, limit+1))
		if err != nil || int64(len(data)) > limit {
			return nil, errors.New("assessment input exceeds limit or cannot be read")
		}
		return data, nil
	}
	var screens *assessment.Screens
	if len(screenArgs) > 0 {
		var paths []string
		for _, arg := range screenArgs {
			if arg == "" {
				fmt.Fprintln(errOut, "9l: --screens needs a path or glob; omit it to assess without screens")
				return 2
			}
			matches, err := filepath.Glob(arg)
			if err != nil || len(matches) == 0 {
				fmt.Fprintln(errOut, "9l: --screens matched no file:", arg)
				return 2
			}
			sort.Strings(matches)
			paths = append(paths, matches...)
		}
		var err error
		if screens, err = assessment.LoadScreens(paths, read); err != nil {
			fmt.Fprintln(errOut, "9l:", err)
			return 2
		}
	}
	var contract []byte
	if *requirements != "" {
		var err error
		if contract, err = read(*requirements, 256<<10); err != nil {
			fmt.Fprintln(errOut, "9l:", err)
			return 2
		}
	}
	// One regular file keeps the single-file report. Several inputs, a
	// directory or a pattern give one combined suite report.
	if info, err := os.Stat(fs.Arg(0)); fs.NArg() > 1 || assessment.IsPattern(fs.Arg(0)) || (err == nil && info.IsDir()) {
		if *agentRecord != "" {
			fmt.Fprintln(errOut, "9l: --agent-provenance applies to a single spec")
			return 2
		}
		return assessSuite(ctx, fs.Args(), contract, screens, *format, *titles, *limits, read, out, errOut)
	}
	source, err := read(fs.Arg(0), assessment.MaxSource)
	if err != nil {
		fmt.Fprintln(errOut, "9l:", err)
		return 2
	}
	report, err := assessment.Assess(ctx, source, contract, assessment.Options{Titles: *titles, Path: fs.Arg(0), Screens: screens})
	if err != nil {
		fmt.Fprintln(errOut, "9l:", err)
		return 2
	}
	if *agentRecord != "" {
		expected, err := loadAgentProvenance(*agentRecord)
		if err != nil {
			fmt.Fprintln(errOut, "9l:", err)
			return 2
		}
		actual, err := runner.CaptureAgentProvenanceFor(ctx, fs.Arg(0), expected)
		if graph := (runner.ImportGraphError{}); errors.As(err, &graph) {
			// Our own bounded message naming repo-relative paths; raw Git
			// errors are never emitted.
			fmt.Fprintf(errOut, "9l: warning: requested agent provenance could not be captured (%s); provenance remains unknown\n", graph.Error())
		} else if err != nil {
			fmt.Fprintln(errOut, "9l: warning: requested agent provenance could not be captured; provenance remains unknown")
		} else {
			if actual.Source != report.SourceSHA256 {
				fmt.Fprintln(errOut, "9l: source changed during assessment")
				return 2
			}
			report.AgentProvenance = runner.CompareAgentProvenance(expected, actual)
		}
	}
	if *format == "json" {
		if json.NewEncoder(out).Encode(report) != nil {
			return 2
		}
	} else {
		writeAssessmentText(out, report, *limits)
	}
	// Advice does not gate execution. Invalid or unavailable analysis exits 2.
	return 0
}

// assessSuite reads and assesses every discovered file. It writes the combined
// report even when some files could not be assessed, then exits 2 for them.
func assessSuite(ctx context.Context, inputs []string, contract []byte, screens *assessment.Screens, format string, titles, limits bool, read func(string, int64) ([]byte, error), out, errOut io.Writer) int {
	paths, err := assessment.DiscoverSpecs(inputs)
	if err != nil {
		fmt.Fprintln(errOut, "9l:", err)
		return 2
	}
	var total int64
	for _, path := range paths {
		if info, err := os.Stat(path); err == nil {
			total += info.Size()
		}
	}
	if total > assessment.MaxSuiteSource {
		fmt.Fprintln(errOut, "9l: assessment input exceeds the 64 MiB suite source limit; narrow the directory or pattern")
		return 2
	}
	files := make([]assessment.SuiteInput, len(paths))
	for index, path := range paths {
		files[index].Path = filepath.ToSlash(path)
		if files[index].Source, err = read(path, assessment.MaxSource); err != nil {
			files[index].Error = "source-limit"
		}
	}
	suite, err := assessment.AssessSuite(ctx, files, contract, assessment.Options{Titles: titles, Screens: screens})
	if err != nil {
		fmt.Fprintln(errOut, "9l:", err)
		return 2
	}
	if format == "json" {
		if json.NewEncoder(out).Encode(suite) != nil {
			return 2
		}
	} else {
		writeSuiteText(out, suite, limits)
	}
	if suite.Summary.Failed > 0 {
		fmt.Fprintf(errOut, "9l: %d of %d files could not be assessed\n", suite.Summary.Failed, suite.Summary.Files)
		return 2
	}
	return 0
}

// writeSuiteText writes the combined report. limits lists every
// analysis-limit location instead of one count per code and file.
func writeSuiteText(out io.Writer, suite assessment.SuiteReport, limits bool) {
	fmt.Fprintln(out, "Advisory assessment; execution not run; analysis partial.")
	for _, file := range suite.Files {
		if file.Report == nil {
			fmt.Fprintf(out, "file %s: not assessed: %s\n", strconv.Quote(file.Path), file.Error)
			continue
		}
		if len(file.Report.Tests) == 0 && len(file.Report.UnrecognizedTestImports) == 0 {
			fmt.Fprintf(out, "file %s: no tests\n", strconv.Quote(file.Path))
			continue
		}
		fmt.Fprintf(out, "file %s:\n", strconv.Quote(file.Path))
		writeAssessmentTests(out, *file.Report, limits)
	}
	for _, limit := range suite.Limits {
		fmt.Fprintln(out, "Limit:", limit)
	}
	writeScreens(out, suite.Screens)
	fmt.Fprintf(out, "Summary: %d files (%d assessed, ", suite.Summary.Files, suite.Summary.Assessed)
	if suite.Summary.NoTests > 0 {
		fmt.Fprintf(out, "%d with no tests, ", suite.Summary.NoTests)
	}
	fmt.Fprintf(out, "%d not assessed", suite.Summary.Failed+suite.Summary.Unrecognized)
	if suite.Summary.Unrecognized > 0 {
		fmt.Fprintf(out, ", %d of them for an unrecognized test import", suite.Summary.Unrecognized)
	}
	fmt.Fprintf(out, "), %d tests, %d findings\n", suite.Summary.Tests, suite.Summary.Findings)
	writeRuleCounts(out, suite.Summary.Rules)
}

// writeScreens names the inspected screens test IDs were checked against.
func writeScreens(out io.Writer, screens []assessment.ScreenEvidence) {
	for _, screen := range screens {
		fmt.Fprintf(out, "Screen %s: %s, %d test IDs, sha256 %s\n", strconv.Quote(screen.Path), screen.Platform, screen.TestIDs, screen.SHA256)
	}
}

func writeAssessmentText(out io.Writer, report assessment.Report, limits bool) {
	fmt.Fprintln(out, "Advisory assessment; execution not run; analysis partial.")
	fmt.Fprint(out, "Agent branch/source provenance: ", report.AgentProvenance.Status)
	// "matched" is easy to over-trust: name what was compared.
	switch report.AgentProvenance.Scope {
	case runner.ProvenanceScopeSpec:
		fmt.Fprint(out, " (the selected spec, its path, branch, commit and workspace root; imported modules are not compared; recapture the snapshot to compare them)")
	case runner.ProvenanceScopeImports:
		fmt.Fprint(out, " (the selected spec, its path, its relative imports, the nearest lockfile, branch, commit and workspace root; package and path-alias imports are not followed)")
	}
	fmt.Fprintln(out)
	for _, change := range report.AgentProvenance.DependencyChanges {
		fmt.Fprintf(out, "Agent provenance %s %s: %s\n", change.Kind, change.Change, strconv.Quote(change.Path))
	}
	writeAssessmentTests(out, report, limits)
	for _, limit := range report.Limits {
		fmt.Fprintln(out, "Limit:", limit)
	}
	writeScreens(out, report.Screens)
	counts, total := assessment.CountFindings(report)
	fmt.Fprintf(out, "Summary: %d tests, %d findings\n", len(report.Tests), total)
	writeRuleCounts(out, counts)
}

// assessmentDimensions are the text names of a test's dimensions, in order.
var assessmentDimensions = []struct{ key, label string }{
	{"purpose", "purpose"}, {"intentAlignment", "alignment"}, {"assertionAdequacy", "assertions"},
	{"runtimeEvidence", "runtime"}, {"engineeringQuality", "quality"},
}

// knownDimensions is ": label=value ..." for the dimensions the assessment
// could judge, or "" when every one is unknown. Without a requirements
// contract, provenance or a run, most are unknown for every test, and a line
// of five unknowns per test hides the findings. JSON output keeps all five.
func knownDimensions(dimensions map[string]string) string {
	var known []string
	for _, dimension := range assessmentDimensions {
		if value := dimensions[dimension.key]; value != "" && value != "unknown" {
			known = append(known, dimension.label+"="+value)
		}
	}
	if len(known) == 0 {
		return ""
	}
	return ": " + strings.Join(known, " ")
}

// writeAssessmentTests lists each test with its findings. Analysis limits
// repeat for every test that calls the same unresolved helper or passes the
// same callback, so unless limits is set they are left out of the test lines
// and summed in one unindented line after the file's tests: one count per
// limit code, counted under assessment.FindingCountKey so that the line agrees
// with the per-rule summary.
func writeAssessmentTests(out io.Writer, report assessment.Report, limits bool) {
	seen := map[assessment.CountKey]bool{}
	grouped := map[string]int{}
	for _, test := range report.Tests {
		title := ""
		if test.Title != "" {
			// Quoting keeps control characters in source titles off the terminal.
			title = " " + strconv.Quote(test.Title)
		}
		fmt.Fprintf(out, "test at %d:%d%s%s\n", test.Line, test.Column, title, knownDimensions(test.Dimensions))
		if test.Assertions.Count > 0 {
			// What was recognized, beside any limit on what was not.
			fmt.Fprintf(out, "  recognized assertions: %d (%s)\n", test.Assertions.Count, strings.Join(test.Assertions.Matchers, ", "))
		}
		for _, finding := range test.Findings {
			if finding.Rule == "analysis-limit" && !limits {
				if k := assessment.FindingCountKey(finding); !seen[k] {
					seen[k] = true
					grouped[finding.Code]++
				}
				continue
			}
			fields := ""
			if finding.Requirement != "" {
				fields += " requirement=" + finding.Requirement
			}
			if finding.Outcome != "" {
				fields += " outcome=" + finding.Outcome
			}
			if finding.Site != nil {
				// The finding is inside a helper; the site is the test's call.
				fields += fmt.Sprintf(" via %d:%d", finding.Site.Line, finding.Site.Column)
			}
			fmt.Fprintf(out, "  %s [%s] at %d:%d%s: %s\n", assessment.FindingRule(finding), finding.Classification, finding.Line, finding.Column, fields, finding.Message)
		}
	}
	if len(grouped) > 0 {
		fmt.Fprintf(out, "analysis limits in this file: %s (--limits lists them)\n", strings.Join(countedCodes(grouped), ", "))
	}
}

// countedCodes renders codes as "code ×N" by descending count, then name.
func countedCodes(counts map[string]int) []string {
	codes := make([]string, 0, len(counts))
	for code := range counts {
		codes = append(codes, code)
	}
	sort.Slice(codes, func(i, j int) bool {
		if counts[codes[i]] != counts[codes[j]] {
			return counts[codes[i]] > counts[codes[j]]
		}
		return codes[i] < codes[j]
	})
	for i, code := range codes {
		codes[i] = fmt.Sprintf("%s ×%d", code, counts[code])
	}
	return codes
}

// writeRuleCounts lists rules by descending count, then name.
func writeRuleCounts(out io.Writer, counts map[string]int) {
	rules := make([]string, 0, len(counts))
	for rule := range counts {
		rules = append(rules, rule)
	}
	sort.Slice(rules, func(i, j int) bool {
		if counts[rules[i]] != counts[rules[j]] {
			return counts[rules[i]] > counts[rules[j]]
		}
		return rules[i] < rules[j]
	})
	for _, rule := range rules {
		fmt.Fprintf(out, "  %s: %d\n", rule, counts[rule])
	}
}

func nativeRunTimeout() time.Duration {
	if raw := strings.TrimSpace(os.Getenv("NINELIVES_RUN_TIMEOUT")); raw != "" {
		if seconds, err := strconv.Atoi(raw); err == nil && seconds > 0 {
			return time.Duration(seconds) * time.Second
		}
	}
	return 5 * time.Minute
}

func terminalApproval(in *os.File, out io.Writer) func(context.Context) bool {
	if info, err := in.Stat(); err != nil || info.Mode()&os.ModeCharDevice == 0 {
		return nil
	}
	return func(ctx context.Context) bool { return readTerminalApproval(ctx, in, out) }
}

// readTerminalApproval returns as soon as its session is canceled, even if a
// terminal's blocking read ignores Close. The reader goroutine is command-owned:
// cancellation causes nativeHealCommand to return and process teardown reclaims
// it; it never performs approval or source mutation after the select returns.
func readTerminalApproval(ctx context.Context, in io.Reader, out io.Writer) bool {
	return readApproval(ctx, in, out, "Apply this verified candidate? [y/yes]: ")
}

func readApproval(ctx context.Context, in io.Reader, out io.Writer, question string) bool {
	fmt.Fprint(out, question)
	type response struct {
		answer string
		err    error
	}
	read := make(chan response, 1)
	go func() {
		var answer string
		_, err := fmt.Fscanln(in, &answer)
		read <- response{answer: answer, err: err}
	}()
	select {
	case <-ctx.Done():
		return false
	case result := <-read:
		if result.err != nil {
			return false
		}
		answer := strings.ToLower(strings.TrimSpace(result.answer))
		return answer == "y" || answer == "yes"
	}
}

func previewDiff(out io.Writer) func(string, string) {
	return func(original, candidate string) {
		fmt.Fprintln(out, "--- original")
		fmt.Fprintln(out, "+++ verified candidate")
		oldLines, newLines := strings.Split(original, "\n"), strings.Split(candidate, "\n")
		for i := 0; i < len(oldLines) || i < len(newLines); i++ {
			var old, next string
			if i < len(oldLines) {
				old = oldLines[i]
			}
			if i < len(newLines) {
				next = newLines[i]
			}
			if old != next {
				fmt.Fprintln(out, "-"+old)
				fmt.Fprintln(out, "+"+next)
			}
		}
	}
}

func tier1Command(args []string, in io.Reader, out, errOut io.Writer) int {
	fs := flag.NewFlagSet("tier1", flag.ContinueOnError)
	fs.SetOutput(errOut)
	format := fs.String("format", "", "must be json")
	if err := fs.Parse(args); err != nil || fs.NArg() != 0 || *format != "json" {
		fmt.Fprintln(errOut, "9l: tier1 requires --format json and no positional arguments")
		return 2
	}
	const maxInput = 4 << 20
	limited := io.LimitReader(in, maxInput+1)
	raw, err := io.ReadAll(limited)
	if err != nil || len(raw) > maxInput {
		fmt.Fprintln(errOut, "9l: tier1 request is too large")
		return 2
	}
	request, err := decodeTier1Request(raw)
	if err != nil {
		fmt.Fprintln(errOut, "9l: tier1 invalid request")
		return 2
	}
	if err := healing.Validate(request); err != nil {
		fmt.Fprintln(errOut, "9l: tier1 invalid request")
		return 2
	}
	if err := json.NewEncoder(out).Encode(healing.Heal(request)); err != nil {
		fmt.Fprintln(errOut, "9l: tier1 response failed")
		return 1
	}
	return 0
}

func decodeTier1Request(raw []byte) (healing.Request, error) {
	if !utf8.Valid(raw) || !validJSONUnicodeEscapes(raw) {
		return healing.Request{}, errors.New("invalid request encoding")
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	first, err := decoder.Token()
	if err != nil || first != json.Delim('{') {
		return healing.Request{}, errors.New("request must be an object")
	}
	allowed := map[string]bool{"version": true, "framework": true, "errorMessage": true, "stackTrace": true, "failureType": true, "failedSelector": true, "testCode": true, "pageSnapshot": true, "allowAssertionChange": true}
	values := make(map[string]json.RawMessage)
	for decoder.More() {
		token, err := decoder.Token()
		key, ok := token.(string)
		if err != nil || !ok || !allowed[key] || values[key] != nil {
			return healing.Request{}, errors.New("invalid request member")
		}
		var value json.RawMessage
		if err := decoder.Decode(&value); err != nil || bytes.Equal(value, []byte("null")) {
			return healing.Request{}, errors.New("invalid request value")
		}
		values[key] = value
	}
	last, err := decoder.Token()
	if err != nil || last != json.Delim('}') {
		return healing.Request{}, errors.New("unterminated request")
	}
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		return healing.Request{}, errors.New("multiple JSON values")
	}
	encoded, err := json.Marshal(values)
	if err != nil {
		return healing.Request{}, err
	}
	var request healing.Request
	if err := json.Unmarshal(encoded, &request); err != nil {
		return healing.Request{}, err
	}
	return request, nil
}

// encoding/json replaces malformed UTF-8 and lone UTF-16 surrogates. Tier 1's
// strict request schema rejects them before decoding so a caller cannot change
// request meaning through replacement characters.
func validJSONUnicodeEscapes(raw []byte) bool {
	inString := false
	for i := 0; i < len(raw); i++ {
		if raw[i] == '"' && !inString {
			inString = !inString
			continue
		}
		if !inString {
			continue
		}
		if raw[i] == '"' {
			inString = false
			continue
		}
		if raw[i] != '\\' || i+1 >= len(raw) {
			continue
		}
		if raw[i+1] != 'u' {
			i++
			continue
		}
		if i+5 >= len(raw) {
			return false
		}
		unit, ok := hexUnit(raw[i+2 : i+6])
		if !ok {
			return false
		}
		if unit >= 0xD800 && unit <= 0xDBFF {
			if i+11 >= len(raw) || raw[i+6] != '\\' || raw[i+7] != 'u' {
				return false
			}
			low, ok := hexUnit(raw[i+8 : i+12])
			if !ok || low < 0xDC00 || low > 0xDFFF {
				return false
			}
			i += 11
			continue
		}
		if unit >= 0xDC00 && unit <= 0xDFFF {
			return false
		}
		i += 5
	}
	return !inString
}

func hexUnit(value []byte) (rune, bool) {
	var unit rune
	for _, digit := range value {
		unit <<= 4
		switch {
		case digit >= '0' && digit <= '9':
			unit += rune(digit - '0')
		case digit >= 'a' && digit <= 'f':
			unit += rune(digit-'a') + 10
		case digit >= 'A' && digit <= 'F':
			unit += rune(digit-'A') + 10
		default:
			return 0, false
		}
	}
	return unit, true
}

func runCommand(command string, args []string, out, errOut io.Writer) int {
	fs := flag.NewFlagSet(command, flag.ContinueOnError)
	fs.SetOutput(errOut)
	format := fs.String("format", "text", "text or json")
	maxJobs := fs.Int("max-jobs", 0, "maximum jobs to reserve for this run (0 is unlimited)")
	workers := fs.Int("workers", min(runtime.GOMAXPROCS(0), 8), "maximum concurrent jobs")
	timeout := fs.Duration("timeout", 5*time.Minute, "per-job timeout")
	deadline := fs.Duration("deadline", 0, "shared run deadline (0 is unlimited)")
	maxAttempts := fs.Int("attempts", 1, "maximum attempts per job: 9l's own retry of the whole job; the project's Playwright retries still apply inside each attempt unless --retries overrides them")
	retries := fs.Int("retries", -1, "Playwright retries per test inside each attempt, replacing the project's setting; 0 with --attempts lets 9l own all retries (default: the project's setting)")
	var waitFor waitTargets
	fs.Var(&waitFor, "wait-for", "before any test process starts, wait until this http(s) URL answers without a server error or this host:port accepts connections (repeatable)")
	waitTimeout := fs.Duration("wait-timeout", time.Minute, "how long --wait-for targets may take to become ready, 1s to 30m; a target still not ready leaves the run incomplete without starting a test process")
	maxOutputBytes := fs.Int("max-output-bytes", 4<<20, "captured bytes per output stream")
	dryRun := fs.Bool("dry-run", false, "print the plan without executing it")
	grep := fs.String("grep", "", "run only tests whose title matches this regular expression (Playwright --grep)")
	grepInvert := fs.String("grep-invert", "", "skip tests whose title matches this regular expression (Playwright --grep-invert)")
	configFile := fs.String("config", "", "Playwright config file to run the specs with, instead of the one Playwright finds")
	var projects projectNames
	fs.Var(&projects, "project", "run only this Playwright project (repeatable)")
	reporters := fs.String("reporter", "", "also run these Playwright reporters beside 9l's own, comma-separated (for example html); json is reserved for evidence")
	keepAttachments := fs.Bool("keep-attachments", false, "copy each failed test's attachments (error context, screenshot, trace) into its receipt directory; they can hold page content")
	sdk := fs.Bool("sdk", false, "use the installed @9l/playwright engine bridge")
	var targets projectNames
	fs.Var(&targets, "target", "with --sdk, schedule one job per target project: web:<app-alias>, android:<app-alias>, ios:<app-alias> or android-fake:<app-alias> (repeatable; no automatic retries)")
	// --show-output belongs to run alone; plan has nothing to show.
	showOutput := new(bool)
	if command == "run" {
		fs.BoolVar(showOutput, "show-output", false, "stream each worker's stdout and stderr to stderr while it runs, prefixed by job and stream; never persisted, redaction is best effort, so do not publish it")
	}
	failureDetails := fs.Bool("failure-details", false, "with --sdk, record each failed test's title, failing line, error and attachments, from Playwright's JSON reporter beside the evidence stream")
	headed := fs.Bool("headed", false, "show the browser: run Playwright headed, one job at a time unless --workers is set")
	agentRecord := fs.String("agent-provenance", "", "require the agent creation branch, commit and source")
	goalProvider := fs.String("goal-provider", "", "explicit goal provider: openai or anthropic (API key), claude, codex or opencode (the CLI's own login), or jev (TYPESAFE_API_KEY; experimental)")
	goalModel := fs.String("goal-model", "", "provider model for goal decisions")
	goalScript := fs.String("goal-script", "", "offline scripted goal decisions JSON (qualification only)")
	goalLimits := goals.Defaults()
	fs.IntVar(&goalLimits.MaxActions, "goal-max-actions", goalLimits.MaxActions, "shared attempt goal action budget")
	fs.IntVar(&goalLimits.MaxDecisions, "goal-max-decisions", goalLimits.MaxDecisions, "shared attempt provider decision budget")
	fs.IntVar(&goalLimits.MaxTokens, "goal-max-tokens", goalLimits.MaxTokens, "conservative shared goal token reservation")
	fs.IntVar(&goalLimits.TimeoutMS, "goal-timeout-ms", goalLimits.TimeoutMS, "per-goal time budget in milliseconds")
	goalCost := fs.Int64("goal-max-cost-micros", 0, "optional shared estimated-cost ceiling in millionths of USD")
	goalInputPrice := fs.Int64("goal-input-micros-per-million", 0, "explicit conservative input price, millionths of USD per million tokens")
	goalOutputPrice := fs.Int64("goal-output-micros-per-million", 0, "explicit conservative output price, millionths of USD per million tokens")
	receiptDir := fs.String("receipt-dir", ".9lives/receipts", "directory for evidence receipts")
	junitPath := fs.String("junit", "", "also write a JUnit XML report of the run to this file, for CI dashboards; an incomplete run is reported as errors, never as passed tests")
	verifyPath := fs.String("verify", "", "experimental: after the test process tree has stopped, probe the HTTP postconditions declared in this checks file from the 9l process; needs one exact spec, and a contradicted check fails the run")
	var passEnv envNames
	fs.Var(&passEnv, "pass-env", "forward this environment variable to test processes (repeatable or comma-separated)")
	var skipPins skipPinList
	fs.Var(&skipPins, "pin-skip", "with --sdk, accept this skipped test: \"<file> › <title>\" (repeatable)")
	repeat := new(int)
	label := new(string)
	if command == "run" {
		fs.IntVar(repeat, "repeat", 1, "run the same job this many times, one run after another, and report the pass rate and failure categories; needs --deadline for the whole batch, runs Playwright with --retries 0 unless --retries is given, and never retries a member")
		fs.StringVar(label, "label", "", "with --repeat, a name for this batch, to compare experiment arms (one line, at most 128 bytes)")
	}
	normalized := flagsFirst(args, map[string]bool{
		"-repeat": true, "--repeat": true, "-label": true, "--label": true,
		"-pass-env": true, "--pass-env": true,
		"-agent-provenance": true, "--agent-provenance": true,
		"-pin-skip": true, "--pin-skip": true,
		"-reporter": true, "--reporter": true,
		"-config": true, "--config": true, "-project": true, "--project": true,
		"-target": true, "--target": true,
		"-grep": true, "--grep": true, "-grep-invert": true, "--grep-invert": true,
		"-format": true, "--format": true,
		"-max-jobs": true, "--max-jobs": true,
		"-workers": true, "--workers": true,
		"-timeout": true, "--timeout": true,
		"-deadline": true, "--deadline": true,
		"-attempts": true, "--attempts": true, "-retries": true, "--retries": true,
		"-wait-for": true, "--wait-for": true, "-wait-timeout": true, "--wait-timeout": true,
		"-max-output-bytes": true, "--max-output-bytes": true,
		"-receipt-dir": true, "--receipt-dir": true, "-junit": true, "--junit": true, "-verify": true, "--verify": true,
		"-goal-provider": true, "--goal-provider": true, "-goal-model": true, "--goal-model": true, "-goal-script": true, "--goal-script": true,
		"-goal-max-actions": true, "--goal-max-actions": true, "-goal-max-decisions": true, "--goal-max-decisions": true, "-goal-max-tokens": true, "--goal-max-tokens": true, "-goal-timeout-ms": true, "--goal-timeout-ms": true,
		"-goal-max-cost-micros": true, "--goal-max-cost-micros": true, "-goal-input-micros-per-million": true, "--goal-input-micros-per-million": true, "-goal-output-micros-per-million": true, "--goal-output-micros-per-million": true,
	})
	if err := fs.Parse(normalized); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0
		}
		return 2
	}
	if *format != "text" && *format != "json" {
		fmt.Fprintln(errOut, "9l: --format must be text or json")
		return 2
	}
	if *workers < 1 || *maxJobs < 0 || *timeout <= 0 || *deadline < 0 || *maxAttempts < 1 || *maxOutputBytes < 1024 {
		fmt.Fprintln(errOut, "9l: invalid limits; workers, timeout, attempts, and max-output-bytes must be positive")
		return 2
	}
	if fs.NArg() == 0 {
		fmt.Fprintln(errOut, "9l: at least one spec, glob, or directory is required")
		return 2
	}
	if *repeat == 0 && command == "plan" {
		*repeat = 1
	}
	switch {
	case *repeat < 1 || *repeat > MaxRepeat:
		fmt.Fprintf(errOut, "9l: --repeat must be 1 to %d\n", MaxRepeat)
		return exitUsage
	case *label != "" && *repeat == 1:
		fmt.Fprintln(errOut, "9l: --label applies only with --repeat")
		return exitUsage
	case len(*label) > 128 || strings.ContainsAny(*label, "\r\n\x00"):
		fmt.Fprintln(errOut, "9l: --label is one line of at most 128 bytes")
		return exitUsage
	case *repeat > 1 && *deadline <= 0:
		fmt.Fprintln(errOut, "9l: --repeat needs --deadline, the bound for the whole batch")
		return exitUsage
	case *repeat > 1 && *maxAttempts > 1:
		fmt.Fprintln(errOut, "9l: --repeat measures single attempts; drop --attempts, since a retry would hide the failures it counts")
		return exitUsage
	case *repeat > 1 && (flagWasSet(fs, "junit") || flagWasSet(fs, "verify")):
		fmt.Fprintln(errOut, "9l: --junit and --verify describe one run; they cannot be combined with --repeat")
		return exitUsage
	}
	// Checked here so plan and --dry-run reject a path run would reject.
	var report *junitReport
	if flagWasSet(fs, "junit") {
		checked, err := newJUnitReport(*junitPath)
		if err != nil {
			fmt.Fprintln(errOut, "9l:", err)
			return exitUsage
		}
		report = checked
	}
	// The checks file is read once, here, before any process starts: this
	// content and its digest are what the attempt is held to.
	var postconditions *verifier
	if flagWasSet(fs, "verify") {
		loaded, err := loadVerifier(*verifyPath, passEnv)
		if err != nil {
			fmt.Fprintln(errOut, "9l: --verify:", err)
			return exitUsage
		}
		postconditions = loaded
	}
	// --wait-for is checked here so plan and --dry-run reject what run would.
	if waitFor.invalid {
		fmt.Fprintln(errOut, "9l: --wait-for: each target is one http(s) URL or host:port of at most 512 bytes, at most 16 targets")
		return exitUsage
	}
	if *waitTimeout < minWaitTimeout || *waitTimeout > maxWaitTimeout {
		fmt.Fprintln(errOut, "9l: --wait-timeout must be between 1s and 30m")
		return exitUsage
	}
	waitProbes, err := parseWaitTargets(waitFor.values)
	if err != nil {
		fmt.Fprintf(errOut, "9l: --wait-for: %v\n", err)
		return exitUsage
	}
	explicitWorkers := false
	fs.Visit(func(f *flag.Flag) { explicitWorkers = explicitWorkers || f.Name == "workers" })
	visual, err := visualOptions(*headed, command == "run" && !*dryRun)
	if err != nil {
		fmt.Fprintln(errOut, "9l:", err)
		return 2
	}
	if *headed && !explicitWorkers {
		*workers = 1
	}
	var agentProvenance *runner.AgentProvenance
	if *agentRecord != "" {
		record, err := loadAgentProvenance(*agentRecord)
		if err != nil {
			fmt.Fprintln(errOut, "9l:", err)
			return 2
		}
		agentProvenance = &record
	}
	if *goalModel != "" && *goalProvider == "" {
		fmt.Fprintln(errOut, "9l: --goal-model requires --goal-provider")
		return 2
	}
	var services runner.AttemptServiceFactory
	var jevGoals *jevassist.GoalProvider
	if *goalProvider != "" || *goalScript != "" {
		factory := goals.Factory{Model: *goalModel, Limits: goalLimits, MaxCostMicros: *goalCost, InputMicrosPerMillion: *goalInputPrice, OutputMicrosPerMillion: *goalOutputPrice}
		if !*sdk || (*goalProvider != "" && *goalScript != "") || factory.ValidateBudget() != nil {
			fmt.Fprintln(errOut, "9l: goals require --sdk, one explicit provider, and valid budgets; cost ceilings require both conservative token prices")
			return 2
		}
		var provider goals.Provider
		if *goalScript != "" {
			script, err := goals.LoadScript(*goalScript)
			if err != nil {
				fmt.Fprintln(errOut, "9l: invalid goal script")
				return 2
			}
			provider = script
		} else {
			timeout := time.Duration(goalLimits.TimeoutMS) * time.Millisecond
			switch strings.ToLower(*goalProvider) {
			case "openai", "anthropic":
				resolved, err := tier2.Resolve(tier2.Options{Name: *goalProvider, Timeout: timeout})
				if err != nil {
					fmt.Fprintln(errOut, "9l: goal provider unavailable")
					return 2
				}
				var ok bool
				provider, ok = resolved.(goals.Provider)
				if !ok {
					fmt.Fprintln(errOut, "9l: goal provider lacks bounded decision transport")
					return 2
				}
			case "claude", "claude-code", "codex", "opencode":
				// The CLI's own login pays for decisions; no API fallback.
				cli, err := tier2.DecisionCLI(*goalProvider, timeout)
				if err != nil {
					fmt.Fprintln(errOut, "9l: goal provider unavailable:", err)
					return 2
				}
				provider = cli
			case "jev":
				config := jev.Config{Enabled: true, Model: firstNonEmptyString(*goalModel, jev.DefaultModel)}
				if preflight, err := jev.CheckConfig(config); err != nil || preflight.Status != "configured" {
					fmt.Fprintln(errOut, "9l: goal provider jev needs a valid --goal-model and "+jev.CredentialName+" in this process")
					return 2
				}
				jevGoals = &jevassist.GoalProvider{New: newGoalDecider(config, timeout)}
				provider = jevGoals
			default:
				fmt.Fprintln(errOut, "9l: goal provider must be openai, anthropic, claude, codex, opencode or jev")
				return 2
			}
		}
		factory.Provider = provider
		services = factory
	}

	if *failureDetails && !*sdk {
		fmt.Fprintln(errOut, "9l: --failure-details requires --sdk; plain runs always report failed tests")
		return 2
	}
	if len(targets) > 0 {
		if !*sdk || len(projects) > 0 {
			fmt.Fprintln(errOut, "9l: --target requires --sdk and cannot be combined with --project")
			return 2
		}
		seen := map[string]bool{}
		for _, target := range targets {
			if !playwrightsdk.ValidTarget(target) || seen[target] {
				fmt.Fprintln(errOut, "9l: targets must be distinct web:<app-alias>, android:<app-alias>, ios:<app-alias> or android-fake:<app-alias> projects")
				return 2
			}
			seen[target] = true
		}
	}
	if len(skipPins) > 0 && !*sdk {
		fmt.Fprintln(errOut, "9l: --pin-skip requires --sdk")
		return 2
	}
	availableAdapters := []runner.Adapter{playwright.New()}
	if *sdk {
		adapter, err := playwrightsdk.New().WithSkipPins(skipPins)
		if err != nil {
			fmt.Fprintf(errOut, "9l: --pin-skip: %v\n", err)
			return 2
		}
		availableAdapters = []runner.Adapter{adapter.WithFailureDetails(*failureDetails)}
	}
	config := ""
	if *configFile != "" {
		absolute, err := filepath.Abs(*configFile)
		if err != nil {
			fmt.Fprintln(errOut, "9l: --config must name an existing Playwright config file")
			return 2
		}
		if info, err := os.Stat(absolute); err != nil || !info.Mode().IsRegular() {
			fmt.Fprintln(errOut, "9l: --config must name an existing Playwright config file")
			return 2
		}
		config = absolute
	}
	extraReporters, err := reporterList(*reporters)
	if err != nil {
		fmt.Fprintln(errOut, "9l: --reporter:", err)
		return 2
	}
	var extraArgs, selection []string
	if *grep != "" {
		// One argument each, so a pattern starting with "-" is never an option.
		extraArgs, selection = append(extraArgs, "--grep="+*grep), append(selection, "--grep "+strconv.Quote(*grep))
	}
	if *grepInvert != "" {
		extraArgs, selection = append(extraArgs, "--grep-invert="+*grepInvert), append(selection, "--grep-invert "+strconv.Quote(*grepInvert))
	}
	// Only an omitted --retries keeps the project's setting; an explicit
	// value, negative included, goes to the planner, which bounds it.
	var playwrightRetries *int
	fs.Visit(func(f *flag.Flag) {
		if f.Name == "retries" {
			playwrightRetries = retries
		}
	})
	// A repeat measures how often a job passes on its own: a Playwright retry
	// inside a member would hide the failure it is there to count. Targets
	// already run without automatic retries and refuse --retries.
	if *repeat > 1 && playwrightRetries == nil && len(targets) == 0 {
		zero := 0
		playwrightRetries = &zero
	}
	build := func(runDeadline time.Duration) (runner.Plan, error) {
		plan, err := runner.BuildPlan(fs.Args(), runner.PlanOptions{MaxJobs: *maxJobs, MaxParallel: *workers, MaxAttempts: *maxAttempts, MaxOutputBytes: *maxOutputBytes, Deadline: runDeadline, Adapters: availableAdapters, ExtraArgs: extraArgs, Selection: strings.Join(selection, " "), Reporters: extraReporters, Config: config, Projects: projects, Targets: targets, Retries: playwrightRetries})
		if err == nil {
			visual.apply(&plan)
		}
		return plan, err
	}
	plan, err := build(*deadline)
	if err != nil {
		fmt.Fprintf(errOut, "9l: plan: %v\n", err)
		return 2
	}
	// Another job running beside the checked one could change the state the
	// checks observe, so plan and --dry-run reject what run would.
	if postconditions != nil && (len(plan.Jobs) != 1 || len(plan.Skipped) != 0) {
		fmt.Fprintln(errOut, "9l: --verify: independent verification requires one exact spec planned as one job")
		return exitUsage
	}
	if jevGoals != nil {
		// Jev decisions are bound to the exact spec bytes that run.
		digest, err := planSourceDigest(plan)
		if err != nil {
			fmt.Fprintln(errOut, "9l: goal provider jev could not read the planned specs")
			return 2
		}
		jevGoals.SourceSHA256 = digest
		jevGoals.Revision, _ = sourceRevision(plan.Jobs[0].Spec, digest)
	}
	if command == "plan" || *dryRun {
		return printPlan(out, plan, *format)
	}
	if report != nil {
		if err := report.prepare(); err != nil {
			fmt.Fprintln(errOut, "9l:", err)
			return exitUsage
		}
	}

	// Live output goes to stderr, so --format json keeps stdout parseable.
	var liveOutput io.Writer
	if *showOutput {
		liveOutput = errOut
		if os.Getenv("CI") != "" {
			fmt.Fprintln(errOut, "9l: --show-output streams test output into the CI log; redaction is best effort, so the log may hold secrets or application data")
		}
		// The engine reporter keeps worker output out of Playwright's own
		// printing, so a spec's console reaches the stream only through a
		// terminal reporter.
		if *sdk && !printingReporter(extraReporters) {
			fmt.Fprintln(errOut, "9l: with --sdk, a spec's console output reaches --show-output only through a terminal reporter; add --reporter list")
		}
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	// A target that never comes up is an environment failure, not a test
	// failure. The wait runs inside the stored run, so `9l cancel`, the run
	// deadline and `9l result` cover it; a target still not ready leaves an
	// error receipt per job and no test process started.
	var beforeJobs func(context.Context) error
	if len(waitProbes) > 0 {
		beforeJobs = func(ctx context.Context) error {
			err := waitForTargets(ctx, waitProbes, *waitTimeout)
			var notReady waitNotReadyError
			if errors.As(err, &notReady) {
				fmt.Fprintf(errOut, "9l: %v; no test process was started\n", err)
			}
			return err
		}
	}
	execute := func(plan runner.Plan, runDeadline time.Duration) (runner.RunSummary, error) {
		return runner.Execute(ctx, plan, runner.ExecuteOptions{
			OnStarted: func() {
				if *format == "json" {
					fmt.Fprintf(errOut, "run %s started; use `9l cancel %s --receipt-dir %s` to cancel\n", plan.RunID, plan.RunID, *receiptDir)
				} else {
					fmt.Fprintf(out, "run %s started; use `9l cancel %s --receipt-dir %s` to cancel\n", plan.RunID, plan.RunID, *receiptDir)
				}
			},
			BeforeJobs:      beforeJobs,
			AgentProvenance: agentProvenance,
			TrackJevUsage:   jevGoals != nil,
			Verifier:        verifierOption(postconditions),
			Workers:         *workers, Timeout: *timeout, RunDeadline: runDeadline, MaxAttempts: *maxAttempts, MaxOutputBytes: *maxOutputBytes, PassEnv: passEnv, KeepAttachments: *keepAttachments, LiveOutput: liveOutput, ReceiptDir: *receiptDir, Adapters: availableAdapters, Services: services,
		})
	}
	if *repeat > 1 {
		return runRepeat(ctx, *repeat, *label, *deadline, playwrightRetries, plan, build, execute, *format, *receiptDir, out, errOut)
	}
	result, err := execute(plan, *deadline)
	// A setup error is found before any process starts: no run exists to
	// report, cancel or retry, so it is a usage error without a result.
	var setup runner.SetupError
	if errors.As(err, &setup) {
		fmt.Fprintf(errOut, "9l: %v\n", err)
		return exitUsage
	}
	executionFailed := err != nil
	if err != nil && !errors.Is(err, context.Canceled) && !errors.Is(err, context.DeadlineExceeded) {
		fmt.Fprintf(errOut, "9l: execution: %v\n", err)
	}
	if *format == "json" {
		_ = json.NewEncoder(out).Encode(result)
	} else {
		printResult(out, result)
	}
	// Written on every outcome of a run that started, so CI shows a
	// canceled or timed-out run instead of a missing report.
	return report.finish(result, runExitCode(result, executionFailed), errOut)
}

// verifierOption keeps a nil *verifier from becoming a non-nil interface.
func verifierOption(postconditions *verifier) runner.PostconditionVerifier {
	if postconditions == nil {
		return nil
	}
	return postconditions
}

func flagWasSet(fs *flag.FlagSet, name string) bool {
	set := false
	fs.Visit(func(f *flag.Flag) { set = set || f.Name == name })
	return set
}

// projectNames collects repeatable --project values: one Playwright project
// name each, on one line.
type projectNames []string

func (p *projectNames) String() string { return strings.Join(*p, ",") }
func (p *projectNames) Set(name string) error {
	if strings.TrimSpace(name) == "" || len(name) > 128 || strings.ContainsAny(name, "\r\n\x00") {
		return errors.New("a project name is one non-empty line of at most 128 bytes")
	}
	if len(*p) == 16 {
		return errors.New("at most 16 projects")
	}
	*p = append(*p, name)
	return nil
}

var reporterName = regexp.MustCompile(`^[A-Za-z0-9@./][A-Za-z0-9@/._-]{0,127}$`)

// reporterList validates --reporter: names or module paths, comma-separated.
// json is the evidence channel and cannot be added twice.
func reporterList(raw string) ([]string, error) {
	if strings.TrimSpace(raw) == "" {
		return nil, nil
	}
	var names []string
	for _, name := range strings.Split(raw, ",") {
		name = strings.TrimSpace(name)
		switch {
		case name == "json":
			return nil, errors.New("json is 9l's evidence reporter and is always on")
		case !reporterName.MatchString(name) || strings.Contains(name, ".."):
			return nil, fmt.Errorf("%q is not a reporter name or module path", name)
		}
		names = append(names, name)
	}
	if len(names) > 8 {
		return nil, errors.New("at most 8 reporters")
	}
	return names, nil
}

// printingReporter reports whether a Playwright terminal reporter, which
// prints the tests' own stdout and stderr, is among the extra reporters.
func printingReporter(names []string) bool {
	for _, name := range names {
		if name == "list" || name == "line" || name == "dot" {
			return true
		}
	}
	return false
}

// Exit codes of `9l run`, part of the host contract in docs/contracts.md.
const (
	exitPassed     = 0
	exitFailed     = 1
	exitUsage      = 2
	exitIncomplete = 3
)

// runExitCode never reports more than the run proved: an execution error
// makes even an all-passed or failed result incomplete.
func runExitCode(result runner.RunSummary, executionFailed bool) int {
	switch {
	case executionFailed:
		return exitIncomplete
	case result.Outcome == runner.OutcomePassed && result.Complete:
		return exitPassed
	case result.Outcome == runner.OutcomeFailed:
		return exitFailed
	default:
		return exitIncomplete
	}
}

func stateCommand(command string, args []string, out, errOut io.Writer) int {
	fs := flag.NewFlagSet(command, flag.ContinueOnError)
	fs.SetOutput(errOut)
	format := fs.String("format", "json", "text or json")
	receiptDir := fs.String("receipt-dir", ".9lives/receipts", "directory containing run state")
	valueFlags := map[string]bool{"-format": true, "--format": true, "-receipt-dir": true, "--receipt-dir": true}
	junitPath := new(string)
	if command == "result" {
		junitPath = fs.String("junit", "", "also write a JUnit XML report of the saved result to this file")
		valueFlags["-junit"], valueFlags["--junit"] = true, true
	}
	normalized := flagsFirst(args, valueFlags)
	if err := fs.Parse(normalized); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0
		}
		return 2
	}
	if fs.NArg() != 1 {
		fmt.Fprintf(errOut, "9l: %s requires exactly one run ID\n", command)
		return 2
	}
	if *format != "text" && *format != "json" {
		fmt.Fprintln(errOut, "9l: --format must be text or json")
		return 2
	}
	runID := fs.Arg(0)
	if command == "cancel" {
		if err := runner.RequestCancel(*receiptDir, runID); err != nil {
			fmt.Fprintf(errOut, "9l: cancel: %v\n", err)
			return 2
		}
		fmt.Fprintf(out, "cancellation requested for %s\n", runID)
		return 0
	}
	if command == "status" {
		status, err := runner.LoadStatus(*receiptDir, runID)
		if err != nil {
			fmt.Fprintf(errOut, "9l: status: %v\n", err)
			return 2
		}
		if *format == "json" {
			_ = json.NewEncoder(out).Encode(status)
		} else {
			fmt.Fprintf(out, "%s: %s\n", runID, status.State)
		}
		return 0
	}
	var report *junitReport
	if flagWasSet(fs, "junit") {
		checked, err := newJUnitReport(*junitPath)
		if err == nil {
			err = checked.prepare()
		}
		if err != nil {
			fmt.Fprintln(errOut, "9l:", err)
			return exitUsage
		}
		report = checked
	}
	result, err := runner.LoadResult(*receiptDir, runID)
	if err != nil {
		fmt.Fprintf(errOut, "9l: result: %v\n", err)
		return 2
	}
	if *format == "json" {
		_ = json.NewEncoder(out).Encode(result)
	} else {
		printResult(out, result)
	}
	return report.finish(result, 0, errOut)
}

func printPlan(w io.Writer, plan runner.Plan, format string) int {
	if format == "json" {
		if err := json.NewEncoder(w).Encode(plan); err != nil {
			return 1
		}
		return 0
	}
	fmt.Fprintf(w, "run %s: %d job(s), %d skipped\n", plan.RunID, len(plan.Jobs), len(plan.Skipped))
	for _, job := range plan.Jobs {
		if job.Selection != "" {
			fmt.Fprintf(w, "  RUN  %s [%s] (%s)\n", job.Spec, job.Adapter, job.Selection)
		} else {
			fmt.Fprintf(w, "  RUN  %s [%s]\n", job.Spec, job.Adapter)
		}
	}
	for _, skip := range plan.Skipped {
		fmt.Fprintf(w, "  SKIP %s — %s\n", skip.Input, skip.Reason)
	}
	return 0
}

func printResult(w io.Writer, result runner.RunSummary) {
	clearable := false
	fmt.Fprintf(w, "run %s: %d passed, %d failed, %d canceled, %d timed out, %d errors (%s)\n", result.RunID, result.Passed, result.Failed, result.Canceled, result.TimedOut, result.Errors, time.Duration(result.DurationMS)*time.Millisecond)
	for _, receipt := range result.Receipts {
		fmt.Fprintf(w, "  %-8s %s", strings.ToUpper(string(receipt.Status)), receipt.Spec)
		if receipt.ReceiptPath != "" {
			fmt.Fprintf(w, "  %s", filepath.Clean(receipt.ReceiptPath))
		}
		fmt.Fprintln(w)
		if receipt.Error != "" {
			fmt.Fprintf(w, "           %s\n", receiptReason(receipt.Error))
		}
		for _, failure := range receipt.Failures {
			printFailure(w, failure)
			for _, attachment := range failure.Attachments {
				clearable = clearable || !attachment.Retained
			}
		}
		// Without per-test details (an engine run without --failure-details)
		// the summary still says which tests failed and how.
		if receipt.FailureSummary != nil && len(receipt.Failures) == 0 {
			printFailureSummary(w, *receipt.FailureSummary)
		}
		printAnnotations(w, receipt.Annotations)
		printVerifications(w, receipt)
		if receipt.FlakyTests > 0 {
			fmt.Fprintf(w, "           %d test(s) passed only on a Playwright retry inside this attempt\n", receipt.FlakyTests)
		}
		// Only when the failures are known: an engine run without
		// --failure-details lists none, so nothing can be said about them.
		// The count is of the files the receipt references; inline bodies and
		// files outside the project are never referenced, so it says
		// "referenced", not "produced".
		if receipt.KeepAttachments && len(receipt.Failures) > 0 && receipt.FailureAttachments == 0 {
			fmt.Fprintln(w, "           --keep-attachments: the listed failures referenced no attachment files")
		}
		if receipt.PassedWithoutAssertions > 0 {
			fmt.Fprintf(w, "           %d passed test(s) reported no assertion step; a green test that asserts nothing proves nothing\n", receipt.PassedWithoutAssertions)
		}
		if receipt.RuntimeExpectedFailures > 0 {
			fmt.Fprintf(w, "           %d failed test(s) count as expected only because test.fail() ran during the test; Playwright reports them green\n", receipt.RuntimeExpectedFailures)
		}
		if receipt.SkippedAfterAssertions > 0 {
			fmt.Fprintf(w, "           %d skipped test(s) had already reported a passed assertion step; the body ran before the skip\n", receipt.SkippedAfterAssertions)
		}
		for _, setup := range receipt.TargetSetup {
			fmt.Fprintf(w, "           %s\n", setupSummary(setup))
		}
		for _, diagnostic := range receipt.TargetDiagnostics {
			fmt.Fprintf(w, "           %s\n", diagnosticSummary(diagnostic))
		}
		if len(receipt.Failures) == runner.MaxReportedFailures && receipt.FailureCount > len(receipt.Failures) {
			fmt.Fprintf(w, "           … %d more failed test(s) in the structured report\n", receipt.FailureCount-len(receipt.Failures))
		}
	}
	for _, skip := range result.Skipped {
		fmt.Fprintf(w, "  SKIP     %s — %s\n", receiptReason(skip.Input), receiptReason(skip.Reason))
	}
	if clearable {
		fmt.Fprintln(w, "  Attachments are Playwright's own files; the project's next run may delete them. --keep-attachments copies them into the receipt.")
	}
	printJevUsage(w, result.JevUsage)
	failedTests, contradicted := false, false
	for _, receipt := range result.Receipts {
		failedTests = failedTests || receipt.FailureCount > 0
		contradicted = contradicted || receipt.VerificationContradicted()
	}
	switch {
	case result.Outcome == runner.OutcomeFailed && contradicted && !failedTests:
		fmt.Fprintln(w, "  FAILED: every planned job ran and an independent check contradicted the result")
	case result.Outcome == runner.OutcomeFailed:
		fmt.Fprintln(w, "  FAILED: every planned job ran and at least one test failed")
	case !result.Complete && result.SkippedInputs > 0 && result.Passed == result.PlannedJobs && len(result.Receipts) == result.PlannedJobs:
		fmt.Fprintf(w, "  INCOMPLETE: %d input(s) were skipped and not run; a skipped input is never a pass\n", result.SkippedInputs)
	case !result.Complete:
		fmt.Fprintln(w, "  INCOMPLETE: one or more planned jobs did not finish successfully")
	}
}

// printVerifications names each independent check and what the engine
// established about it. The records hold identifiers, closed tokens and an
// HTTP status only; a list that is not the closed record is withheld.
func printVerifications(w io.Writer, receipt runner.Receipt) {
	if len(receipt.Verifications) == 0 {
		return
	}
	if !receipt.ValidVerifications() {
		fmt.Fprintln(w, "           verify  the receipt's verification records are malformed and are not shown")
		return
	}
	for _, record := range receipt.Verifications {
		marker := map[runner.VerificationResult]string{runner.VerificationVerified: "✓", runner.VerificationContradicted: "✗"}[record.Result]
		if marker == "" {
			marker = "?"
		}
		line := fmt.Sprintf("%s verify %s: %s", marker, record.ID, record.Result)
		if record.Reason != "" {
			line += " (" + record.Reason + ")"
		}
		line += ", " + record.Method
		if record.HTTPStatus != 0 {
			line += fmt.Sprintf(" answered HTTP %d", record.HTTPStatus)
		}
		if record.OutcomeID != "" {
			line += ", outcome " + record.OutcomeID
		}
		fmt.Fprintf(w, "           %s\n", receiptReason(line))
	}
	fmt.Fprintln(w, "           verify checks are engine-observed HTTP postconditions; they do not measure assertion coverage")
}

// builtinAnnotations are Playwright's own modifiers; a suite can hold many
// test.fail() pins, so they stay in the receipt but not in the text summary.
var builtinAnnotations = map[string]bool{"skip": true, "fixme": true, "fail": true, "slow": true}

// printAnnotations shows the tests' own annotations, such as a test noting
// that it returned early, so a green job does not hide what it skipped.
func printAnnotations(w io.Writer, annotations []runner.TestAnnotation) {
	shown, hidden := 0, 0
	for _, annotation := range annotations {
		if builtinAnnotations[annotation.Type] {
			continue
		}
		if shown == 20 {
			hidden++
			continue
		}
		line := receiptReason(annotation.Test) + " — " + receiptReason(annotation.Type)
		if annotation.Description != "" {
			line += ": " + receiptReason(annotation.Description)
		}
		fmt.Fprintf(w, "           note  %s\n", line)
		shown++
	}
	if hidden > 0 {
		fmt.Fprintf(w, "           … %d more annotation(s) in the receipt\n", hidden)
	}
}

// printFailure shows which test failed, where and why, and the files that
// explain it, below its receipt line.
func printFailure(w io.Writer, failure runner.TestFailure) {
	// A test.fail() pin that passed is reported as a failure, but it means
	// the expected failure is gone, so it gets its own marker.
	marker := "✗"
	if failure.UnexpectedPass() {
		marker = "⚠"
	}
	fmt.Fprintf(w, "           %s %s", marker, receiptReason(failure.Title))
	if failure.Location != "" {
		fmt.Fprintf(w, "  %s", receiptReason(failure.Location))
	}
	// The class tells an assertion that failed from a target that never came
	// up before anyone reads the error; "other" says nothing and is omitted.
	if failure.Class != "" && failure.Class != runner.FailureOther && failure.Class != runner.FailureUnexpectedPass {
		fmt.Fprintf(w, "  [%s]", failure.Class)
	}
	fmt.Fprintln(w)
	shown := 0
	for _, line := range strings.Split(failure.Message, "\n") {
		if line = strings.TrimSpace(line); line == "" || strings.HasPrefix(line, "Call log:") {
			continue
		}
		// Three lines name the error. A matcher's message puts what it
		// received on the fourth, after the locator and the expected value,
		// so those two lines are shown wherever they come.
		if shown >= 3 && !strings.HasPrefix(line, "Expected") && !strings.HasPrefix(line, "Received") {
			continue
		}
		if shown == 6 {
			break
		}
		fmt.Fprintf(w, "             %s\n", receiptReason(line))
		shown++
	}
	for _, attachment := range failure.Attachments {
		label := attachment.Name
		if label == "error-context" {
			label = "context"
		}
		fmt.Fprintf(w, "             %s: %s\n", receiptReason(label), receiptReason(displayPath(attachment.Path)))
	}
}

// printFailureSummary shows each summarized failed test as its short engine
// digest, class and attributed step; the summary has no titles or messages.
func printFailureSummary(w io.Writer, summary runner.FailureSummary) {
	// Loaded results drop malformed summaries; never format one regardless.
	if summary.Validate() != nil {
		return
	}
	for _, test := range summary.Tests {
		line := "test"
		if test.TestID != "" {
			line += " " + test.TestID[:min(12, len(test.TestID))]
		}
		line += " [" + test.Class + "]"
		if test.Status == "timedOut" {
			line += " timed out"
		}
		if test.Operation != "" {
			line += " in " + test.Operation
			if test.Control != "" {
				line += " on " + test.Control
			}
		} else if test.Step != "" {
			line += " in a failed " + test.Step + " step"
		}
		if test.Reason != "" {
			line += ": " + test.Reason
		}
		if test.Setup != "" {
			line += " (setup " + test.Setup + ")"
		}
		fmt.Fprintf(w, "           ✗ %s\n", receiptReason(line))
	}
	if summary.TestsTruncated && len(summary.Tests) > 0 {
		fmt.Fprintf(w, "           … %d more failed test(s) not listed\n", summary.FailedTests-len(summary.Tests))
	}
	if len(summary.Tests) > 0 {
		fmt.Fprintln(w, "           --failure-details adds each failed test's title, line and error")
	}
}

// displayPath shortens a path below the working directory.
func displayPath(path string) string {
	if cwd, err := os.Getwd(); err == nil {
		if relative, err := filepath.Rel(cwd, path); err == nil && !strings.HasPrefix(relative, "..") {
			return relative
		}
	}
	return filepath.Clean(path)
}

// receiptReason is the receipt's error on one line of at most 300 characters,
// with control characters replaced, for the text summary.
func receiptReason(reason string) string {
	runes := []rune(strings.Map(func(r rune) rune {
		if unicode.IsControl(r) {
			return ' '
		}
		return r
	}, reason))
	if len(runes) > 300 {
		return string(runes[:299]) + "…"
	}
	return string(runes)
}

// envNames collects --pass-env values. Only names are accepted; values are
// read from the caller's environment so they never appear in argv or receipts.
type envNames []string

func (names *envNames) String() string { return strings.Join(*names, ",") }

func (names *envNames) Set(value string) error {
	for _, name := range strings.Split(value, ",") {
		name = strings.TrimSpace(name)
		if name == "" || strings.ContainsAny(name, "= \t\x00") {
			return fmt.Errorf("invalid environment variable name %q", name)
		}
		*names = append(*names, name)
	}
	return nil
}

// Pins are whole titles, which may contain commas, so each flag is one pin.
type skipPinList []string

func (pins *skipPinList) String() string { return strings.Join(*pins, "\n") }

func (pins *skipPinList) Set(value string) error {
	*pins = append(*pins, value)
	return nil
}

// The standard flag package stops at the first positional argument. Moving
// recognized flags to the front keeps the familiar `9l run spec --workers 2`
// form without adding a CLI dependency.
func flagsFirst(args []string, valueFlags map[string]bool) []string {
	flags := make([]string, 0, len(args))
	positionals := make([]string, 0, len(args))
	for index := 0; index < len(args); index++ {
		argument := args[index]
		if argument == "--" {
			positionals = append(positionals, args[index+1:]...)
			break
		}
		if !strings.HasPrefix(argument, "-") {
			positionals = append(positionals, argument)
			continue
		}
		flags = append(flags, argument)
		name := strings.SplitN(argument, "=", 2)[0]
		if valueFlags[name] && !strings.Contains(argument, "=") && index+1 < len(args) {
			index++
			flags = append(flags, args[index])
		}
	}
	return append(flags, positionals...)
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

// setupSummary describes one native target setup classification: what the
// driver found, when, and whether a declared recovery restored the baseline.
// Such a test's failure is inconclusive setup evidence, not an application
// defect; a recovered test ran from the restored baseline.
func setupSummary(setup runner.TargetSetup) string {
	when := "before any application operation"
	if setup.Stage == runner.SetupStageRecheck {
		when = "rechecked after a failed assertion"
	}
	if setup.Phase != "" {
		when += ", in the open phase " + setup.Phase
	}
	text := fmt.Sprintf("setup %s: %s (%s)", setup.Classification, setup.Reason, when)
	switch {
	case setup.Recovery != nil && setup.Recovery.Restored:
		return text + fmt.Sprintf("; %s recovery restored the baseline after %d press(es), the test ran from it", setup.Recovery.Method, setup.Recovery.Presses)
	case setup.Recovery != nil:
		return text + fmt.Sprintf("; %s recovery did not restore the baseline after %d press(es); inconclusive, not an application failure", setup.Recovery.Method, setup.Recovery.Presses)
	case setup.Classification == runner.SetupUnknown && setup.Phase != "":
		return text + "; the driver could not complete this phase of open, so the failure is inconclusive"
	case setup.Classification == runner.SetupUnknown:
		return text + "; the device state could not be determined, so the failure is inconclusive"
	case setup.Classification == runner.SetupObserver:
		return text + "; the accessibility snapshot disagreed with the window manager, an observer staleness rather than an application or gesture failure"
	}
	return text + "; inconclusive, not an application failure"
}

// diagnosticSummary prints an opt-in failure diagnostic as metadata only.
func diagnosticSummary(d runner.TargetDiagnostic) string {
	text := fmt.Sprintf("diagnostic %s: %s after %d ms", d.Operation, d.Reason, d.ElapsedMS)
	if d.Point {
		text += fmt.Sprintf("; requested point (%d, %d)", d.X, d.Y)
	}
	if d.ViewportWidth != 0 {
		text += fmt.Sprintf(" in a %dx%d viewport", d.ViewportWidth, d.ViewportHeight)
	}
	windows := func(count int, focus string) string {
		if count < 0 {
			return "not observed"
		}
		if focus == "" {
			return fmt.Sprintf("%d", count)
		}
		return fmt.Sprintf("%d (focus %s)", count, focus)
	}
	return text + fmt.Sprintf("; app windows at the last mutation %s, after the failure %s", windows(d.WindowsBefore, d.FocusBefore), windows(d.WindowsAfter, d.FocusAfter))
}

// stringList collects a repeatable string flag.
type stringList []string

func (l *stringList) String() string     { return strings.Join(*l, ",") }
func (l *stringList) Set(v string) error { *l = append(*l, v); return nil }
