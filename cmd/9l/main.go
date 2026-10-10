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
	"github.com/Quality-Max/9lives-runner/internal/runner"
)

// Release builds set this with -ldflags "-X main.version=<version>".
var version = "0.2.0"

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}

func run(args []string, out, errOut io.Writer) int {
	if len(args) == 0 || args[0] == "--help" || args[0] == "-h" {
		usage(out)
		return 0
	}
	if args[0] == "--version" || args[0] == "version" {
		return versionCommand(args[1:], out, errOut)
	}

	switch args[0] {
	case "plan", "run":
		return runCommand(args[0], args[1:], out, errOut)
	case "status", "result", "cancel":
		return stateCommand(args[0], args[1:], out, errOut)
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
  9l run  <spec-or-glob>... [--workers N] [--timeout D] [--deadline D] [--pass-env NAME]...
          [--sdk]  # opt-in @9l/playwright engine protocol
          [--pin-skip "<file> › <title>"]...  # with --sdk, accept a declared skip
          [--headed]  # show the browser, one job at a time unless --workers is set
  9l status <run-id> [--receipt-dir DIR]
  9l result <run-id> [--format text|json] [--receipt-dir DIR]
  9l cancel <run-id> [--receipt-dir DIR]
  9l heal <spec> [--provider NAME] [--model NAME] [--yes] [--run-timeout D] [--pass-env NAME]...
          # verified selector healing: offline Tier 1, then an agent CLI or API (heal-native is an alias)
  9l mcp [--pass-env NAME]... [--provider NAME]  # MCP server on stdio: run_test, heal_test, assess_test
  9l tier1 --format json  # one offline version:1 JSON proposal request on stdin
  9l assess <spec|dir|'glob'>... [--requirements <contract.json>] [--format text|json] [--titles]
  9l prove <spec> [--faults KINDS] [--max-faults N] [--paths] [--format text|json]  # experimental: inject network faults
  9l confirm <spec> --unfixed REV [--fixed REV] [--finding-id ID] [--format text|json]
          # experimental: does a reproduction spec fail before a fix and pass after it?
  9l provenance <spec> --agent <id>  # creation snapshot JSON
  9l version [--format text|json]  # json: contract versions for host integrations
  # assess/run accept --agent-provenance <snapshot.json> for branch/source checks

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
	if fs.Parse(flagsFirst(args, map[string]bool{"--requirements": true, "--format": true, "--agent-provenance": true})) != nil {
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
		return assessSuite(ctx, fs.Args(), contract, *format, *titles, read, out, errOut)
	}
	source, err := read(fs.Arg(0), assessment.MaxSource)
	if err != nil {
		fmt.Fprintln(errOut, "9l:", err)
		return 2
	}
	report, err := assessment.Assess(ctx, source, contract, assessment.Options{Titles: *titles})
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
		actual, err := runner.CaptureAgentProvenance(ctx, fs.Arg(0), expected.Agent)
		if err != nil {
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
		writeAssessmentText(out, report)
	}
	// Advice does not gate execution. Invalid or unavailable analysis exits 2.
	return 0
}

// assessSuite reads and assesses every discovered file. It writes the combined
// report even when some files could not be assessed, then exits 2 for them.
func assessSuite(ctx context.Context, inputs []string, contract []byte, format string, titles bool, read func(string, int64) ([]byte, error), out, errOut io.Writer) int {
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
	suite, err := assessment.AssessSuite(ctx, files, contract, assessment.Options{Titles: titles})
	if err != nil {
		fmt.Fprintln(errOut, "9l:", err)
		return 2
	}
	if format == "json" {
		if json.NewEncoder(out).Encode(suite) != nil {
			return 2
		}
	} else {
		fmt.Fprintln(out, "Advisory assessment; execution not run; analysis partial.")
		for _, file := range suite.Files {
			if file.Report == nil {
				fmt.Fprintf(out, "file %s: not assessed: %s\n", strconv.Quote(file.Path), file.Error)
				continue
			}
			fmt.Fprintf(out, "file %s:\n", strconv.Quote(file.Path))
			writeAssessmentTests(out, *file.Report)
		}
		for _, limit := range suite.Limits {
			fmt.Fprintln(out, "Limit:", limit)
		}
		fmt.Fprintf(out, "Summary: %d files (%d assessed, %d not assessed), %d tests, %d findings\n", suite.Summary.Files, suite.Summary.Assessed, suite.Summary.Failed, suite.Summary.Tests, suite.Summary.Findings)
		writeRuleCounts(out, suite.Summary.Rules)
	}
	if suite.Summary.Failed > 0 {
		fmt.Fprintf(errOut, "9l: %d of %d files could not be assessed\n", suite.Summary.Failed, suite.Summary.Files)
		return 2
	}
	return 0
}

func writeAssessmentText(out io.Writer, report assessment.Report) {
	fmt.Fprintln(out, "Advisory assessment; execution not run; analysis partial.")
	fmt.Fprintln(out, "Agent branch/source provenance:", report.AgentProvenance.Status)
	writeAssessmentTests(out, report)
	for _, limit := range report.Limits {
		fmt.Fprintln(out, "Limit:", limit)
	}
	counts, total := assessment.CountFindings(report)
	fmt.Fprintf(out, "Summary: %d tests, %d findings\n", len(report.Tests), total)
	writeRuleCounts(out, counts)
}

func writeAssessmentTests(out io.Writer, report assessment.Report) {
	for _, test := range report.Tests {
		title := ""
		if test.Title != "" {
			// Quoting keeps control characters in source titles off the terminal.
			title = " " + strconv.Quote(test.Title)
		}
		fmt.Fprintf(out, "test at %d:%d%s: purpose=%s alignment=%s assertions=%s runtime=%s quality=%s\n", test.Line, test.Column, title, test.Dimensions["purpose"], test.Dimensions["intentAlignment"], test.Dimensions["assertionAdequacy"], test.Dimensions["runtimeEvidence"], test.Dimensions["engineeringQuality"])
		for _, finding := range test.Findings {
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
	fmt.Fprint(out, "Apply this verified candidate? [y/yes]: ")
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
	maxAttempts := fs.Int("attempts", 1, "maximum attempts per job")
	maxOutputBytes := fs.Int("max-output-bytes", 4<<20, "captured bytes per output stream")
	dryRun := fs.Bool("dry-run", false, "print the plan without executing it")
	grep := fs.String("grep", "", "run only tests whose title matches this regular expression (Playwright --grep)")
	grepInvert := fs.String("grep-invert", "", "skip tests whose title matches this regular expression (Playwright --grep-invert)")
	keepAttachments := fs.Bool("keep-attachments", false, "copy each failed test's attachments (error context, screenshot, trace) into its receipt directory; they can hold page content")
	sdk := fs.Bool("sdk", false, "use the installed @9l/playwright engine bridge")
	failureDetails := fs.Bool("failure-details", false, "with --sdk, record each failed test's title, failing line, error and attachments, from Playwright's JSON reporter beside the evidence stream")
	headed := fs.Bool("headed", false, "show the browser: run Playwright headed, one job at a time unless --workers is set")
	agentRecord := fs.String("agent-provenance", "", "require the agent creation branch, commit and source")
	goalProvider := fs.String("goal-provider", "", "explicit goal provider: openai or anthropic")
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
	var passEnv envNames
	fs.Var(&passEnv, "pass-env", "forward this environment variable to test processes (repeatable or comma-separated)")
	var skipPins skipPinList
	fs.Var(&skipPins, "pin-skip", "with --sdk, accept this skipped test: \"<file> › <title>\" (repeatable)")
	normalized := flagsFirst(args, map[string]bool{
		"-pass-env": true, "--pass-env": true,
		"-agent-provenance": true, "--agent-provenance": true,
		"-pin-skip": true, "--pin-skip": true,
		"-grep": true, "--grep": true, "-grep-invert": true, "--grep-invert": true,
		"-format": true, "--format": true,
		"-max-jobs": true, "--max-jobs": true,
		"-workers": true, "--workers": true,
		"-timeout": true, "--timeout": true,
		"-deadline": true, "--deadline": true,
		"-attempts": true, "--attempts": true,
		"-max-output-bytes": true, "--max-output-bytes": true,
		"-receipt-dir": true, "--receipt-dir": true,
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
			if *goalProvider != "openai" && *goalProvider != "anthropic" {
				fmt.Fprintln(errOut, "9l: goal provider must be openai or anthropic")
				return 2
			}
			resolved, err := tier2.Resolve(tier2.Options{Name: *goalProvider, Timeout: time.Duration(goalLimits.TimeoutMS) * time.Millisecond})
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
		}
		factory.Provider = provider
		services = factory
	}

	if *failureDetails && !*sdk {
		fmt.Fprintln(errOut, "9l: --failure-details requires --sdk; plain runs always report failed tests")
		return 2
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
	var extraArgs, selection []string
	if *grep != "" {
		extraArgs, selection = append(extraArgs, "--grep", *grep), append(selection, "--grep "+strconv.Quote(*grep))
	}
	if *grepInvert != "" {
		extraArgs, selection = append(extraArgs, "--grep-invert", *grepInvert), append(selection, "--grep-invert "+strconv.Quote(*grepInvert))
	}
	plan, err := runner.BuildPlan(fs.Args(), runner.PlanOptions{MaxJobs: *maxJobs, MaxParallel: *workers, MaxAttempts: *maxAttempts, MaxOutputBytes: *maxOutputBytes, Deadline: *deadline, Adapters: availableAdapters, ExtraArgs: extraArgs, Selection: strings.Join(selection, " ")})
	if err != nil {
		fmt.Fprintf(errOut, "9l: plan: %v\n", err)
		return 2
	}
	visual.apply(&plan)
	if command == "plan" || *dryRun {
		return printPlan(out, plan, *format)
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	result, err := runner.Execute(ctx, plan, runner.ExecuteOptions{
		OnStarted: func() {
			if *format == "json" {
				fmt.Fprintf(errOut, "run %s started; use `9l cancel %s --receipt-dir %s` to cancel\n", plan.RunID, plan.RunID, *receiptDir)
			} else {
				fmt.Fprintf(out, "run %s started; use `9l cancel %s --receipt-dir %s` to cancel\n", plan.RunID, plan.RunID, *receiptDir)
			}
		},
		AgentProvenance: agentProvenance,
		Workers:         *workers, Timeout: *timeout, RunDeadline: *deadline, MaxAttempts: *maxAttempts, MaxOutputBytes: *maxOutputBytes, PassEnv: passEnv, KeepAttachments: *keepAttachments, ReceiptDir: *receiptDir, Adapters: availableAdapters, Services: services,
	})
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
	return runExitCode(result, executionFailed)
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
	normalized := flagsFirst(args, map[string]bool{"-format": true, "--format": true, "-receipt-dir": true, "--receipt-dir": true})
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
	return 0
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
	switch {
	case result.Outcome == runner.OutcomeFailed:
		fmt.Fprintln(w, "  FAILED: every planned job ran and at least one test failed")
	case !result.Complete && result.SkippedInputs > 0 && result.Passed == result.PlannedJobs && len(result.Receipts) == result.PlannedJobs:
		fmt.Fprintf(w, "  INCOMPLETE: %d input(s) were skipped and not run; a skipped input is never a pass\n", result.SkippedInputs)
	case !result.Complete:
		fmt.Fprintln(w, "  INCOMPLETE: one or more planned jobs did not finish successfully")
	}
}

// printFailure shows which test failed, where and why, and the files that
// explain it, below its receipt line.
func printFailure(w io.Writer, failure runner.TestFailure) {
	fmt.Fprintf(w, "           ✗ %s", receiptReason(failure.Title))
	if failure.Location != "" {
		fmt.Fprintf(w, "  %s", receiptReason(failure.Location))
	}
	fmt.Fprintln(w)
	shown := 0
	for _, line := range strings.Split(failure.Message, "\n") {
		if line = strings.TrimSpace(line); line == "" || strings.HasPrefix(line, "Call log:") {
			continue
		}
		if shown == 3 {
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
