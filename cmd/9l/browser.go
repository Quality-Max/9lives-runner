package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/url"
	"os"
	"os/signal"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/Quality-Max/9lives-runner/internal/browsercheck"
	"github.com/Quality-Max/9lives-runner/internal/repro"
)

var severities = []string{"critical", "high", "medium", "low", "info"}

type browserViewport struct {
	Width  int `json:"width"`
	Height int `json:"height"`
}

type weightBudget struct {
	TotalBytes     *int `json:"total_bytes,omitempty"`
	RequestCount   *int `json:"request_count,omitempty"`
	RenderBlocking *int `json:"render_blocking,omitempty"`
	ImageBytes     *int `json:"image_bytes,omitempty"`
	ScriptBytes    *int `json:"script_bytes,omitempty"`
}

// pageArguments are shared by scan_url and inspect_page.
type pageArguments struct {
	URL                       string           `json:"url"`
	AllowPrivateNetwork       bool             `json:"allow_private_network"`
	StorageStatePath          string           `json:"storage_state_path"`
	AcknowledgePrivateContent *bool            `json:"acknowledge_private_content"`
	Viewport                  *browserViewport `json:"viewport"`
	Timeout                   *int             `json:"timeout"`
}

type scanArguments struct {
	pageArguments
	Checks       []string        `json:"checks"`
	Baseline     json.RawMessage `json:"baseline"`
	MaxLinks     *int            `json:"max_links"`
	Screenshot   bool            `json:"screenshot"`
	WeightBudget *weightBudget   `json:"weight_budget"`
	Format       string          `json:"format"`
	MinSeverity  string          `json:"min_severity"`
}

type inspectArguments struct {
	pageArguments
	IncludeAccessibilityTree *bool  `json:"include_accessibility_tree"`
	IncludeForms             *bool  `json:"include_forms"`
	Format                   string `json:"format"`
}

// helperArgs validates the arguments both tools share and converts them to
// the helper's names. Files are resolved inside root and passed relative to
// it; the helper confines them to its working directory again.
func (a pageArguments) helperArgs(root string) (map[string]any, error) {
	parsed, err := url.Parse(a.URL)
	if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Host == "" {
		return nil, toolError{"url must be an absolute http or https URL"}
	}
	if parsed.User != nil {
		return nil, toolError{"URLs containing credentials are not permitted"}
	}
	args := map[string]any{"url": a.URL}
	if a.AllowPrivateNetwork {
		args["allowPrivateNetwork"] = true
	}
	if a.AcknowledgePrivateContent != nil && !*a.AcknowledgePrivateContent {
		return nil, toolError{"acknowledge_private_content can only be true; omit it otherwise"}
	}
	if a.StorageStatePath != "" {
		if a.AcknowledgePrivateContent == nil {
			return nil, toolError{"storage_state_path requires acknowledge_private_content: true, because the result may contain private page content"}
		}
		relative, err := workspaceFile(root, a.StorageStatePath, "storage_state_path")
		if err != nil {
			return nil, err
		}
		args["storageStatePath"], args["acknowledgePrivateContent"] = relative, true
	}
	if a.Viewport != nil {
		if a.Viewport.Width < 320 || a.Viewport.Width > 3840 || a.Viewport.Height < 320 || a.Viewport.Height > 2400 {
			return nil, toolError{"viewport width must be 320 to 3840 and height 320 to 2400"}
		}
		args["viewport"] = map[string]int{"width": a.Viewport.Width, "height": a.Viewport.Height}
	}
	return args, nil
}

func (a pageArguments) timeout(fallback time.Duration) (time.Duration, error) {
	if a.Timeout == nil {
		return fallback, nil
	}
	if *a.Timeout < 1 {
		return 0, toolError{"timeout must be at least 1 second"}
	}
	return time.Duration(*a.Timeout) * time.Second, nil
}

func (a scanArguments) request(root string) (browsercheck.Request, error) {
	args, err := a.helperArgs(root)
	if err != nil {
		return browsercheck.Request{}, err
	}
	if a.Checks != nil {
		if len(a.Checks) == 0 {
			return browsercheck.Request{}, toolError{"checks must name at least one check; omit it to run all"}
		}
		for _, check := range a.Checks {
			if !slices.Contains(browsercheck.Checks, check) {
				return browsercheck.Request{}, toolError{fmt.Sprintf("unknown check %q; supported: %s", check, strings.Join(browsercheck.Checks, ", "))}
			}
		}
		args["checks"] = a.Checks
	}
	if a.MaxLinks != nil {
		if *a.MaxLinks < 0 || *a.MaxLinks > 250 {
			return browsercheck.Request{}, toolError{"max_links must be 0 to 250"}
		}
		args["maxLinks"] = *a.MaxLinks
	}
	if a.Screenshot {
		args["screenshot"] = true
	}
	if a.WeightBudget != nil {
		budget := map[string]int{}
		for _, field := range []struct {
			name  string
			value *int
			min   int
		}{{"totalBytes", a.WeightBudget.TotalBytes, 1}, {"requestCount", a.WeightBudget.RequestCount, 1}, {"renderBlocking", a.WeightBudget.RenderBlocking, 0}, {"imageBytes", a.WeightBudget.ImageBytes, 1}, {"scriptBytes", a.WeightBudget.ScriptBytes, 1}} {
			if field.value == nil {
				continue
			}
			if *field.value < field.min {
				return browsercheck.Request{}, toolError{"weight_budget values must be positive (render_blocking may be 0)"}
			}
			budget[field.name] = *field.value
		}
		args["weightBudget"] = budget
	}
	if baseline := bytes.TrimSpace(a.Baseline); len(baseline) > 0 && !bytes.Equal(baseline, []byte("null")) {
		switch baseline[0] {
		case '"':
			var path string
			if json.Unmarshal(baseline, &path) != nil || path == "" {
				return browsercheck.Request{}, toolError{"baseline must be a previous scan result or a path to one"}
			}
			relative, err := workspaceFile(root, path, "baseline")
			if err != nil {
				return browsercheck.Request{}, err
			}
			args["baseline"] = relative
		case '{':
			args["baseline"] = json.RawMessage(baseline)
		default:
			return browsercheck.Request{}, toolError{"baseline must be a previous scan result or a path to one"}
		}
	}
	switch a.Format {
	case "", "json", "markdown", "issue":
	default:
		return browsercheck.Request{}, toolError{"format must be json, markdown or issue"}
	}
	if a.MinSeverity != "" && !slices.Contains(severities, a.MinSeverity) {
		return browsercheck.Request{}, toolError{"min_severity must be one of " + strings.Join(severities, ", ")}
	}
	return browsercheck.Request{Tool: "scan_url", Args: args, Format: a.Format, MinSeverity: a.MinSeverity}, nil
}

func (a inspectArguments) request(root string) (browsercheck.Request, error) {
	args, err := a.helperArgs(root)
	if err != nil {
		return browsercheck.Request{}, err
	}
	if a.IncludeAccessibilityTree != nil {
		args["includeAccessibilityTree"] = *a.IncludeAccessibilityTree
	}
	if a.IncludeForms != nil {
		args["includeForms"] = *a.IncludeForms
	}
	if a.Format != "" && a.Format != "json" && a.Format != "markdown" {
		return browsercheck.Request{}, toolError{"format must be json or markdown"}
	}
	return browsercheck.Request{Tool: "inspect_page", Args: args, Format: a.Format}, nil
}

// workspaceFile resolves a file the helper will read, which must be a
// regular file inside root after symbolic links, and returns it relative to root.
func workspaceFile(root, raw, label string) (string, error) {
	absolute := raw
	if !filepath.IsAbs(absolute) {
		absolute = filepath.Join(root, raw)
	}
	resolved, err := filepath.EvalSymlinks(absolute)
	if err != nil {
		return "", toolError{label + " must name an existing file in the working directory"}
	}
	info, err := os.Stat(resolved)
	if err != nil || !info.Mode().IsRegular() {
		return "", toolError{label + " must name a regular file"}
	}
	relative, err := filepath.Rel(root, resolved)
	if err != nil || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) || filepath.IsAbs(relative) {
		return "", toolError{label + " is outside the working directory"}
	}
	return filepath.ToSlash(relative), nil
}

// workingRoot is the directory browser checks and reproductions are
// confined to: where 9l was started, after symbolic links.
func workingRoot() (string, error) {
	cwd, err := os.Getwd()
	if err == nil {
		cwd, err = filepath.EvalSymlinks(cwd)
	}
	return cwd, err
}

// browserFailure describes why a check produced no result.
func browserFailure(err error) string {
	var check browsercheck.CheckError
	switch {
	case errors.As(err, &check):
		return bounded(check.Message)
	case errors.Is(err, browsercheck.ErrNodeUnavailable), errors.Is(err, browsercheck.ErrTimeout), errors.Is(err, browsercheck.ErrOutputLimit):
		return err.Error()
	}
	return "browser check failed"
}

// withVersion adds the MCP result version to a helper result object
// without reordering its fields.
func withVersion(result json.RawMessage) json.RawMessage {
	trimmed := bytes.TrimSpace(result)
	if len(trimmed) < 2 || trimmed[0] != '{' {
		return result
	}
	prefix := fmt.Sprintf(`{"version":%d`, MCPToolResultVersion)
	if rest := bytes.TrimSpace(trimmed[1:]); len(rest) > 0 && rest[0] != '}' {
		prefix += ","
	}
	return json.RawMessage(prefix + string(trimmed[1:]))
}

// pageFlags are the scan and inspect options that choose and load the page.
type pageFlags struct {
	allowPrivate, acknowledge *bool
	storageState, viewport    *string
	timeout                   *time.Duration
}

func addPageFlags(fs *flag.FlagSet, valued map[string]bool) pageFlags {
	for _, name := range []string{"--storage-state", "--viewport", "--timeout"} {
		valued[name] = true
	}
	return pageFlags{
		allowPrivate: fs.Bool("allow-private-network", false, "permit a loopback target such as a locally started app"),
		storageState: fs.String("storage-state", "", "Playwright storage-state file inside the working directory, for an authenticated page"),
		acknowledge:  fs.Bool("acknowledge-private-content", false, "required with --storage-state: the report may contain private page content"),
		viewport:     fs.String("viewport", "", "browser viewport as WIDTHxHEIGHT, such as 390x844 (default 1366x900)"),
		timeout:      fs.Duration("timeout", nativeRunTimeout(), "maximum time for the check"),
	}
}

func (f pageFlags) arguments(url string) (pageArguments, error) {
	page := pageArguments{URL: url, AllowPrivateNetwork: *f.allowPrivate, StorageStatePath: *f.storageState}
	switch {
	case *f.storageState != "" && !*f.acknowledge:
		return page, errors.New("--storage-state requires --acknowledge-private-content, because the report may contain private page content")
	case *f.storageState == "" && *f.acknowledge:
		return page, errors.New("--acknowledge-private-content applies only with --storage-state")
	case *f.acknowledge:
		page.AcknowledgePrivateContent = f.acknowledge
	}
	if *f.viewport != "" {
		var width, height int
		if n, err := fmt.Sscanf(*f.viewport, "%dx%d", &width, &height); n != 2 || err != nil || fmt.Sprintf("%dx%d", width, height) != *f.viewport {
			return page, errors.New("--viewport must be WIDTHxHEIGHT, such as 390x844")
		}
		page.Viewport = &browserViewport{Width: width, Height: height}
	}
	return page, nil
}

// parseWeightBudget reads --weight-budget total_bytes=500000,request_count=40.
func parseWeightBudget(raw string) (*weightBudget, error) {
	budget := &weightBudget{}
	fields := map[string]**int{"total_bytes": &budget.TotalBytes, "request_count": &budget.RequestCount, "render_blocking": &budget.RenderBlocking, "image_bytes": &budget.ImageBytes, "script_bytes": &budget.ScriptBytes}
	for _, item := range strings.Split(raw, ",") {
		key, value, ok := strings.Cut(strings.TrimSpace(item), "=")
		target := fields[key]
		number, err := strconv.Atoi(value)
		if !ok || target == nil || err != nil {
			return nil, errors.New("--weight-budget takes NAME=VALUE pairs for total_bytes, request_count, render_blocking, image_bytes and script_bytes")
		}
		*target = &number
	}
	return budget, nil
}

func scanCommand(args []string, out, errOut io.Writer) int {
	fs := flag.NewFlagSet("scan", flag.ContinueOnError)
	fs.SetOutput(errOut)
	checks := fs.String("checks", "", "comma-separated checks to run: "+strings.Join(browsercheck.Checks, ", "))
	maxLinks := fs.Int("max-links", 50, "maximum links to check (0 to 250)")
	screenshot := fs.Bool("screenshot", false, "capture a screenshot to a temporary file")
	format := fs.String("format", "markdown", "markdown, json or issue")
	minSeverity := fs.String("min-severity", "", "with --format issue, export only findings at or above this severity")
	outPath := fs.String("out", "", "write the report to this file instead of stdout")
	baseline := fs.String("baseline", "", "previous --format json result to report new and fixed findings against")
	failOnNew := fs.Bool("fail-on-new", false, "exit 1 when the scan finds anything absent from --baseline")
	budget := fs.String("weight-budget", "", "page-weight budget as NAME=VALUE pairs: total_bytes, request_count, render_blocking, image_bytes, script_bytes")
	valued := map[string]bool{"--checks": true, "--max-links": true, "--format": true, "--min-severity": true, "--out": true, "--baseline": true, "--weight-budget": true}
	page := addPageFlags(fs, valued)
	timeout := page.timeout
	if fs.Parse(flagsFirst(args, valued)) != nil {
		return exitUsage
	}
	if fs.NArg() != 1 || *timeout <= 0 {
		fmt.Fprintln(errOut, "9l: scan takes one URL and a positive --timeout")
		return exitUsage
	}
	if *failOnNew && *baseline == "" {
		fmt.Fprintln(errOut, "9l: --fail-on-new needs --baseline to compare against")
		return exitUsage
	}
	root, err := workingRoot()
	if err != nil {
		fmt.Fprintln(errOut, "9l: scan cannot resolve its working directory")
		return exitUsage
	}
	pageArgs, err := page.arguments(fs.Arg(0))
	if err != nil {
		fmt.Fprintln(errOut, "9l:", err)
		return exitUsage
	}
	scan := scanArguments{pageArguments: pageArgs, MaxLinks: maxLinks, Screenshot: *screenshot, Format: *format, MinSeverity: *minSeverity}
	if *budget != "" {
		if scan.WeightBudget, err = parseWeightBudget(*budget); err != nil {
			fmt.Fprintln(errOut, "9l:", err)
			return exitUsage
		}
	}
	if *checks != "" {
		scan.Checks = []string{}
		for _, check := range strings.Split(*checks, ",") {
			if check = strings.TrimSpace(check); check != "" {
				scan.Checks = append(scan.Checks, check)
			}
		}
	}
	if *baseline != "" {
		scan.Baseline, _ = json.Marshal(*baseline)
	}
	request, err := scan.request(root)
	if err != nil {
		fmt.Fprintln(errOut, "9l:", err)
		return exitUsage
	}
	response, code := runBrowserCheck(request, root, *timeout, errOut)
	if code != 0 {
		return code
	}
	if writeReport(response, *format == "json", *outPath, out, errOut) != nil {
		return exitIncomplete
	}
	if *failOnNew {
		var result struct {
			Delta *struct {
				New []json.RawMessage `json:"new"`
			} `json:"delta"`
		}
		if json.Unmarshal(response.Result, &result) != nil || result.Delta == nil {
			fmt.Fprintln(errOut, "9l: the scan result has no baseline comparison")
			return exitIncomplete
		}
		if fresh := len(result.Delta.New); fresh > 0 {
			fmt.Fprintf(errOut, "9l: %d new finding(s) since baseline\n", fresh)
			return exitFailed
		}
	}
	return exitPassed
}

func inspectCommand(args []string, out, errOut io.Writer) int {
	fs := flag.NewFlagSet("inspect", flag.ContinueOnError)
	fs.SetOutput(errOut)
	format := fs.String("format", "markdown", "markdown or json")
	outPath := fs.String("out", "", "write the report to this file instead of stdout")
	tree := fs.Bool("accessibility-tree", false, "include Chromium's accessibility tree")
	valued := map[string]bool{"--format": true, "--out": true}
	page := addPageFlags(fs, valued)
	timeout := page.timeout
	if fs.Parse(flagsFirst(args, valued)) != nil {
		return exitUsage
	}
	if fs.NArg() != 1 || *timeout <= 0 {
		fmt.Fprintln(errOut, "9l: inspect takes one URL and a positive --timeout")
		return exitUsage
	}
	root, err := workingRoot()
	if err != nil {
		fmt.Fprintln(errOut, "9l: inspect cannot resolve its working directory")
		return exitUsage
	}
	pageArgs, err := page.arguments(fs.Arg(0))
	if err != nil {
		fmt.Fprintln(errOut, "9l:", err)
		return exitUsage
	}
	inspect := inspectArguments{pageArguments: pageArgs, Format: *format}
	if *tree {
		inspect.IncludeAccessibilityTree = tree
	}
	request, err := inspect.request(root)
	if err != nil {
		fmt.Fprintln(errOut, "9l:", err)
		return exitUsage
	}
	response, code := runBrowserCheck(request, root, *timeout, errOut)
	if code != 0 {
		return code
	}
	if writeReport(response, *format == "json", *outPath, out, errOut) != nil {
		return exitIncomplete
	}
	return exitPassed
}

func runBrowserCheck(request browsercheck.Request, root string, timeout time.Duration, errOut io.Writer) (browsercheck.Response, int) {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	response, err := browsercheck.Run(ctx, request, browsercheck.Options{Root: root, Timeout: timeout})
	if err != nil {
		fmt.Fprintln(errOut, "9l:", browserFailure(err))
		return response, exitIncomplete
	}
	return response, 0
}

func writeReport(response browsercheck.Response, asJSON bool, outPath string, out, errOut io.Writer) error {
	text := response.Text
	if asJSON {
		var indented bytes.Buffer
		if err := json.Indent(&indented, response.Result, "", "  "); err != nil {
			fmt.Fprintln(errOut, "9l: the browser check returned an unreadable result")
			return err
		}
		text = indented.String()
	}
	if !strings.HasSuffix(text, "\n") {
		text += "\n"
	}
	if outPath == "" {
		_, err := io.WriteString(out, text)
		return err
	}
	if err := os.WriteFile(outPath, []byte(text), 0o644); err != nil {
		fmt.Fprintln(errOut, "9l: report could not be written:", outPath)
		return err
	}
	fmt.Fprintln(errOut, "Report written to", outPath)
	return nil
}

// browserTool runs scan_url, inspect_page or generate_playwright_repro.
// Files they read or write stay inside the working directory, even with
// NINELIVES_MCP_UNRESTRICTED=1, because the helper confines them there too.
func (s *mcpServer) browserTool(ctx context.Context, name string, raw json.RawMessage) (any, error) {
	root, err := workingRoot()
	if err != nil {
		return nil, toolError{"the server cannot resolve its working directory"}
	}
	if name == "generate_playwright_repro" {
		var args struct {
			URL       string         `json:"url"`
			Goal      string         `json:"goal"`
			Finding   map[string]any `json:"finding"`
			TestName  string         `json:"test_name"`
			Output    string         `json:"output_path"`
			Overwrite bool           `json:"overwrite"`
			SDK       bool           `json:"sdk"`
		}
		if err := decodeArguments(name, raw, &args); err != nil {
			return nil, err
		}
		result, err := repro.Generate(root, repro.Options{URL: args.URL, Goal: args.Goal, Finding: args.Finding, Name: args.TestName, Output: args.Output, Overwrite: args.Overwrite, SDK: args.SDK})
		var refused repro.Error
		if errors.As(err, &refused) {
			return nil, toolError{refused.Message}
		}
		if err != nil {
			return nil, toolError{"reproduction could not be written"}
		}
		return map[string]any{"version": MCPToolResultVersion, "filePath": result.FilePath, "code": result.Code, "assumptions": result.Assumptions}, nil
	}
	var request browsercheck.Request
	var timeout time.Duration
	if name == "scan_url" {
		var args scanArguments
		if err := decodeArguments(name, raw, &args); err != nil {
			return nil, err
		}
		if timeout, err = args.timeout(s.runTimeout); err != nil {
			return nil, err
		}
		request, err = args.request(root)
	} else {
		var args inspectArguments
		if err := decodeArguments(name, raw, &args); err != nil {
			return nil, err
		}
		if timeout, err = args.timeout(s.runTimeout); err != nil {
			return nil, err
		}
		request, err = args.request(root)
	}
	if err != nil {
		return nil, err
	}
	response, err := browsercheck.Run(ctx, request, browsercheck.Options{Root: root, Timeout: timeout})
	if err != nil {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		return nil, toolError{browserFailure(err)}
	}
	if request.Format == "markdown" || request.Format == "issue" {
		return textResult(response.Text), nil
	}
	return withVersion(response.Result), nil
}
