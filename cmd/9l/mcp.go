package main

import (
	"bufio"
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
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/Quality-Max/9lives-runner/internal/adapters/playwright"
	"github.com/Quality-Max/9lives-runner/internal/confirm"
	"github.com/Quality-Max/9lives-runner/internal/healing/tier2"
	"github.com/Quality-Max/9lives-runner/internal/runner"
)

// MCPToolResultVersion is the version of the JSON each MCP tool returns.
const MCPToolResultVersion = 1

const (
	mcpLatestProtocol = "2025-06-18"
	// One JSON-RPC message per line; a longer line is discarded unread.
	mcpMaxMessage = 4 << 20
	// Tool calls beyond this many in flight are refused rather than queued.
	mcpMaxCalls = 4
	// Bounded diagnostics returned to the agent.
	mcpMaxText = 4 << 10
)

var mcpProtocols = map[string]bool{"2025-06-18": true, "2025-03-26": true, "2024-11-05": true}

const mcpInstructions = "9lives Go runner. run_test executes one Playwright spec with bounded time and validated receipts; " +
	"only status passed with complete evidence means the test passed. heal_test re-runs a failing spec and repairs a drifted " +
	"selector (offline Tier 1, then the configured agent CLI or API), verifying the candidate in an isolated copy before " +
	"saving it as <spec>.healed or, with apply=true, writing it in place. needs_human means an assertion failed: a possible " +
	"real bug that healing will not mask. assess_test statically reviews a spec or directory for weak or missing assertions " +
	"and analysis limits without running it. confirm_finding runs a reproduction spec on the revision a finding was reported " +
	"against and on the fixing revision (default: the working tree); only verdict confirmed means its assertions failed before " +
	"and passed after, and even that does not show the spec tests the finding. Paths must be inside the server's working directory."

// mcpTools are advertised by tools/list.
var mcpTools = []map[string]any{
	{
		"name":        "run_test",
		"title":       "Run a Playwright spec",
		"description": "Run one Playwright spec through the installed project with a bounded timeout. Returns status passed, failed or incomplete, test counts, each failed test with its failing line and error, Playwright's error context for the first failure (error, ARIA snapshot of the page, marked source) and the receipt path. incomplete is never a pass.",
		"inputSchema": map[string]any{
			"type": "object",
			"properties": map[string]any{
				"spec":        map[string]any{"type": "string", "description": "Path to the spec file"},
				"run_timeout": map[string]any{"type": "integer", "minimum": 1, "description": "Max seconds for the run (default 300, or NINELIVES_RUN_TIMEOUT)"},
			},
			"required":             []string{"spec"},
			"additionalProperties": false,
		},
	},
	{
		"name":        "heal_test",
		"title":       "Heal a failing Playwright spec",
		"description": "Run a spec and, if a locator action failed, propose a selector-only repair, verify it in an isolated copy and return the diff. state is passed, verified (saved as savedPath), applied, needs_human, unverified, provider_error or canceled. needs_human means a failing assertion: do not force the test green.",
		"inputSchema": map[string]any{
			"type": "object",
			"properties": map[string]any{
				"spec":          map[string]any{"type": "string", "description": "Path to the spec file"},
				"apply":         map[string]any{"type": "boolean", "default": false, "description": "Write the verified candidate to the spec in place (default: save <spec>.healed)"},
				"max_proposals": map[string]any{"type": "integer", "minimum": 1, "maximum": 5, "default": 1, "description": "Maximum Tier 2 proposals"},
				"run_timeout":   map[string]any{"type": "integer", "minimum": 1, "description": "Max seconds for each verification run (default 300, or NINELIVES_RUN_TIMEOUT)"},
			},
			"required":             []string{"spec"},
			"additionalProperties": false,
		},
	},
	{
		"name":        "assess_test",
		"title":       "Assess a spec's assertions",
		"description": "Statically review a spec file, or every spec in a directory, for missing, unawaited or weak assertions, fixed waits, disabled tests and analysis limits. Advisory; nothing is executed.",
		"inputSchema": map[string]any{
			"type": "object",
			"properties": map[string]any{
				"path":         map[string]any{"type": "string", "description": "Spec file or directory"},
				"requirements": map[string]any{"type": "string", "description": "Optional reviewed requirement contract JSON"},
			},
			"required":             []string{"path"},
			"additionalProperties": false,
		},
	},
	{
		"name":        "confirm_finding",
		"title":       "Confirm a reported finding",
		"description": "Run a reproduction spec, which must import test from @9l/playwright, once on the unfixed revision and once on the fixed revision (default: the working tree), each in its own checkout. verdict is confirmed (assertion failed before, passed after), not-reproduced, fix-ineffective, regressed or inconclusive. Write the reproduction before fixing a finding; do not call a finding confirmed without this verdict.",
		"inputSchema": map[string]any{
			"type": "object",
			"properties": map[string]any{
				"spec":        map[string]any{"type": "string", "description": "Path to the reproduction spec in the working tree"},
				"unfixed":     map[string]any{"type": "string", "description": "Git revision the finding was reported against, such as HEAD or main"},
				"fixed":       map[string]any{"type": "string", "description": "Git revision with the fix (default: the working tree)"},
				"finding_id":  map[string]any{"type": "string", "description": "Short label recorded as given, such as an issue key"},
				"finding":     map[string]any{"type": "string", "description": "Finding text; only its SHA-256 is recorded"},
				"run_timeout": map[string]any{"type": "integer", "minimum": 1, "description": "Max seconds for each of the two runs (default 300, or NINELIVES_RUN_TIMEOUT)"},
			},
			"required":             []string{"spec", "unfixed"},
			"additionalProperties": false,
		},
	},
}

type mcpMessage struct {
	ID     json.RawMessage `json:"id,omitempty"`
	Method string          `json:"method"`
	Params json.RawMessage `json:"params,omitempty"`
}

type mcpServer struct {
	// root contains every tool path; empty when NINELIVES_MCP_UNRESTRICTED=1.
	root        string
	passEnv     []string
	provider    tier2.Provider
	model       string
	runTimeout  time.Duration
	receiptDir  string
	healReceipt string
	out         io.Writer
	log         io.Writer

	writeMu sync.Mutex
	// Heals run one at a time. A queued heal waits on this one-slot channel
	// rather than a mutex, so cancelling it frees its request slot at once.
	healSlot chan struct{}
	mu       sync.Mutex
	calls    map[string]context.CancelFunc
	wg       sync.WaitGroup
}

// toolError is returned to the agent as an isError tool result.
type toolError struct{ message string }

func (e toolError) Error() string { return e.message }

// failedResult is a complete tool result reported with isError, so a failed
// call still returns its evidence.
type failedResult struct{ payload any }

func (failedResult) Error() string { return "tool failed" }

func mcpCommand(args []string, in io.Reader, out, errOut io.Writer) int {
	fs := flag.NewFlagSet("mcp", flag.ContinueOnError)
	fs.SetOutput(errOut)
	providerName := fs.String("provider", "", "heal_test Tier 2 provider; default: an installed agent CLI or configured API key, else offline Tier 1 only")
	model := fs.String("model", os.Getenv("NINELIVES_MODEL"), "provider model")
	providerURL := fs.String("provider-url", "", "local/provider HTTP URL")
	receiptDir := fs.String("receipt-dir", ".9lives/receipts", "run_test receipts")
	healReceipts := fs.String("heal-receipt-dir", ".9lives/healing-receipts", "heal_test verification receipts")
	var passEnv envNames
	fs.Var(&passEnv, "pass-env", "forward this environment variable to test processes (repeatable or comma-separated)")
	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0
		}
		return exitUsage
	}
	if fs.NArg() != 0 {
		fmt.Fprintln(errOut, "9l: mcp takes no arguments; start it in the project directory")
		return exitUsage
	}
	provider, err := resolveHealProvider(*providerName, *model, *providerURL)
	if err != nil {
		fmt.Fprintln(errOut, "9l: heal provider unavailable")
		return exitUsage
	}
	server := &mcpServer{passEnv: passEnv, provider: provider, model: *model, runTimeout: nativeRunTimeout(),
		receiptDir: *receiptDir, healReceipt: *healReceipts, out: out, log: errOut, calls: map[string]context.CancelFunc{}, healSlot: make(chan struct{}, 1)}
	if os.Getenv("NINELIVES_MCP_UNRESTRICTED") != "1" {
		cwd, err := os.Getwd()
		if err == nil {
			cwd, err = filepath.EvalSymlinks(cwd)
		}
		if err != nil {
			fmt.Fprintln(errOut, "9l: mcp cannot resolve its working directory")
			return exitUsage
		}
		server.root = cwd
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	server.serve(ctx, in)
	return 0
}

// serve answers newline-delimited JSON-RPC until stdin closes or ctx ends,
// then cancels tool calls still running and waits for them.
func (s *mcpServer) serve(ctx context.Context, in io.Reader) {
	lines := make(chan []byte)
	go func() {
		defer close(lines)
		reader := bufio.NewReaderSize(in, 64<<10)
		for {
			line, err := readMessage(reader)
			if line != nil {
				select {
				case lines <- line:
				case <-ctx.Done():
					return
				}
			}
			if err != nil {
				return
			}
		}
	}()
	defer func() {
		s.mu.Lock()
		for _, cancel := range s.calls {
			cancel()
		}
		s.mu.Unlock()
		s.wg.Wait()
	}()
	for {
		select {
		case <-ctx.Done():
			return
		case line, ok := <-lines:
			if !ok {
				return
			}
			s.handle(ctx, line)
		}
	}
}

// readMessage returns the next line. A line over mcpMaxMessage is discarded
// through its newline and returned as oversizedLine, which is answered as a
// parse error.
func readMessage(reader *bufio.Reader) ([]byte, error) {
	var line []byte
	oversized := false
	for {
		chunk, err := reader.ReadSlice('\n')
		if !oversized && len(line)+len(chunk) > mcpMaxMessage {
			oversized, line = true, nil
		}
		if !oversized {
			line = append(line, chunk...)
		}
		if errors.Is(err, bufio.ErrBufferFull) {
			continue
		}
		if oversized {
			return oversizedLine, err
		}
		return line, err
	}
}

var oversizedLine = []byte{0}

func (s *mcpServer) handle(ctx context.Context, line []byte) {
	line = bytes.TrimSpace(line)
	if len(line) == 0 {
		return
	}
	var message mcpMessage
	if line[0] != '{' {
		code, text := -32700, "Parse error"
		if json.Valid(line) {
			code, text = -32600, "Invalid Request"
		}
		s.send(map[string]any{"jsonrpc": "2.0", "id": nil, "error": map[string]any{"code": code, "message": text}})
		return
	}
	if err := json.Unmarshal(line, &message); err != nil {
		s.send(map[string]any{"jsonrpc": "2.0", "id": nil, "error": map[string]any{"code": -32700, "message": "Parse error"}})
		return
	}
	notification := len(message.ID) == 0
	switch message.Method {
	case "initialize":
		var params struct {
			ProtocolVersion string `json:"protocolVersion"`
		}
		_ = json.Unmarshal(message.Params, &params)
		protocol := mcpLatestProtocol
		if mcpProtocols[params.ProtocolVersion] {
			protocol = params.ProtocolVersion
		}
		s.result(message.ID, map[string]any{
			"protocolVersion": protocol,
			"capabilities":    map[string]any{"tools": map[string]any{}},
			"serverInfo":      map[string]any{"name": "9lives", "title": "9lives Go runner", "version": version},
			"instructions":    mcpInstructions,
		})
	case "ping":
		s.result(message.ID, map[string]any{})
	case "tools/list":
		s.result(message.ID, map[string]any{"tools": mcpTools})
	case "tools/call":
		if notification {
			return
		}
		s.call(ctx, message)
	case "notifications/cancelled":
		var params struct {
			RequestID json.RawMessage `json:"requestId"`
		}
		if json.Unmarshal(message.Params, &params) == nil {
			s.mu.Lock()
			if cancel := s.calls[string(params.RequestID)]; cancel != nil {
				cancel()
			}
			s.mu.Unlock()
		}
	default:
		if !notification {
			s.send(map[string]any{"jsonrpc": "2.0", "id": message.ID, "error": map[string]any{"code": -32601, "message": "Method not found"}})
		}
	}
}

// call runs a tool in its own goroutine so cancellation and other requests
// are still read while it runs. A canceled call gets no response.
func (s *mcpServer) call(ctx context.Context, message mcpMessage) {
	var params struct {
		Name      string          `json:"name"`
		Arguments json.RawMessage `json:"arguments"`
	}
	if json.Unmarshal(message.Params, &params) != nil || (params.Name != "run_test" && params.Name != "heal_test" && params.Name != "assess_test" && params.Name != "confirm_finding") {
		s.send(map[string]any{"jsonrpc": "2.0", "id": message.ID, "error": map[string]any{"code": -32602, "message": "Unknown tool"}})
		return
	}
	key := string(message.ID)
	callCtx, cancel := context.WithCancel(ctx)
	s.mu.Lock()
	if _, duplicate := s.calls[key]; duplicate || len(s.calls) >= mcpMaxCalls {
		s.mu.Unlock()
		cancel()
		s.toolResult(message.ID, map[string]any{"version": MCPToolResultVersion, "error": "too many tool calls in progress or duplicate request id"}, true)
		return
	}
	s.calls[key] = cancel
	s.wg.Add(1)
	s.mu.Unlock()
	go func() {
		defer s.wg.Done()
		defer func() {
			s.mu.Lock()
			delete(s.calls, key)
			s.mu.Unlock()
			cancel()
		}()
		payload, err := s.runTool(callCtx, params.Name, params.Arguments)
		if callCtx.Err() != nil {
			return
		}
		var failed failedResult
		if errors.As(err, &failed) {
			s.toolResult(message.ID, failed.payload, true)
			return
		}
		if err != nil {
			var tool toolError
			text := "tool failed"
			if errors.As(err, &tool) {
				text = tool.message
			}
			s.toolResult(message.ID, map[string]any{"version": MCPToolResultVersion, "error": text}, true)
			return
		}
		s.toolResult(message.ID, payload, false)
	}()
}

// decodeArguments decodes a tool's arguments into its own struct, so an
// argument another tool takes is refused like any unknown one, as each
// schema's additionalProperties: false declares.
func decodeArguments(name string, raw json.RawMessage, target any) error {
	if len(raw) == 0 {
		return nil
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if decoder.Decode(target) != nil {
		return toolError{"invalid arguments for " + name}
	}
	return nil
}

func (s *mcpServer) timeoutArgument(seconds *int) (time.Duration, error) {
	if seconds == nil {
		return s.runTimeout, nil
	}
	if *seconds < 1 {
		return 0, toolError{"run_timeout must be at least 1 second"}
	}
	return time.Duration(*seconds) * time.Second, nil
}

func (s *mcpServer) runTool(ctx context.Context, name string, raw json.RawMessage) (any, error) {
	switch name {
	case "run_test":
		var args struct {
			Spec       string `json:"spec"`
			RunTimeout *int   `json:"run_timeout"`
		}
		if err := decodeArguments(name, raw, &args); err != nil {
			return nil, err
		}
		timeout, err := s.timeoutArgument(args.RunTimeout)
		if err != nil {
			return nil, err
		}
		spec, err := s.contained(args.Spec, false)
		if err != nil {
			return nil, err
		}
		return s.runTest(ctx, spec, timeout)
	case "heal_test":
		var args struct {
			Spec         string `json:"spec"`
			Apply        bool   `json:"apply"`
			MaxProposals *int   `json:"max_proposals"`
			RunTimeout   *int   `json:"run_timeout"`
		}
		if err := decodeArguments(name, raw, &args); err != nil {
			return nil, err
		}
		timeout, err := s.timeoutArgument(args.RunTimeout)
		if err != nil {
			return nil, err
		}
		spec, err := s.contained(args.Spec, false)
		if err != nil {
			return nil, err
		}
		proposals := 1
		if args.MaxProposals != nil {
			if *args.MaxProposals < 1 || *args.MaxProposals > 5 {
				return nil, toolError{"max_proposals must be 1 to 5"}
			}
			proposals = *args.MaxProposals
		}
		return s.healTest(ctx, spec, args.Apply, proposals, timeout)
	case "confirm_finding":
		var args struct {
			Spec       string `json:"spec"`
			Unfixed    string `json:"unfixed"`
			Fixed      string `json:"fixed"`
			FindingID  string `json:"finding_id"`
			Finding    string `json:"finding"`
			RunTimeout *int   `json:"run_timeout"`
		}
		if err := decodeArguments(name, raw, &args); err != nil {
			return nil, err
		}
		timeout, err := s.timeoutArgument(args.RunTimeout)
		if err != nil {
			return nil, err
		}
		spec, err := s.contained(args.Spec, false)
		if err != nil {
			return nil, err
		}
		if args.Unfixed == "" {
			return nil, toolError{"unfixed is required"}
		}
		return s.confirmFinding(ctx, confirmOptions{
			spec: spec, unfixed: args.Unfixed, fixed: args.Fixed, finding: args.Finding, findingID: args.FindingID,
			workers: 1, timeout: timeout, maxOutputBytes: 4 << 20, passEnv: s.passEnv, receiptDir: s.receiptDir,
		})
	default:
		var args struct {
			Path         string `json:"path"`
			Requirements string `json:"requirements"`
		}
		if err := decodeArguments(name, raw, &args); err != nil {
			return nil, err
		}
		path, err := s.contained(args.Path, true)
		if err != nil {
			return nil, err
		}
		requirements := ""
		if args.Requirements != "" {
			if requirements, err = s.contained(args.Requirements, false); err != nil {
				return nil, err
			}
		}
		return s.assessTest(ctx, path, requirements)
	}
}

// contained resolves a tool path, which must exist inside the server's root.
// Running or healing a spec executes it, so an agent cannot point the server
// at files elsewhere unless NINELIVES_MCP_UNRESTRICTED=1.
func (s *mcpServer) contained(raw string, allowDir bool) (string, error) {
	if strings.TrimSpace(raw) == "" {
		return "", toolError{"a path is required"}
	}
	absolute, err := filepath.Abs(raw)
	if err == nil {
		absolute, err = filepath.EvalSymlinks(absolute)
	}
	if err != nil {
		return "", toolError{"path not found: " + raw}
	}
	info, err := os.Stat(absolute)
	if err != nil || !(info.Mode().IsRegular() || (allowDir && info.IsDir())) {
		return "", toolError{"not a regular file: " + raw}
	}
	if s.root != "" {
		relative, err := filepath.Rel(s.root, absolute)
		if err != nil || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
			return "", toolError{"path is outside the server's working directory: " + raw + "; start `9l mcp` in the project root or set NINELIVES_MCP_UNRESTRICTED=1"}
		}
	}
	return absolute, nil
}

type mcpRunResult struct {
	Version       int    `json:"version"`
	Spec          string `json:"spec"`
	Status        string `json:"status"`
	RunID         string `json:"runId"`
	ExecutedTests int    `json:"executedTests"`
	FailureCount  int    `json:"failureCount"`
	SkippedTests  int    `json:"skippedTests"`
	Reason        string `json:"reason,omitempty"`
	Failure       string `json:"failure,omitempty"`
	// Failures names each failed test with its failing line, error start and
	// attachments; FailureContext is the first failure's Playwright
	// error-context.md (error, ARIA snapshot of the page, marked source).
	Failures       []runner.TestFailure `json:"failures,omitempty"`
	FailureContext string               `json:"failureContext,omitempty"`
	Receipt        string               `json:"receipt,omitempty"`
}

const mcpMaxFailureContext = 8 << 10

func (s *mcpServer) runTest(ctx context.Context, spec string, timeout time.Duration) (any, error) {
	adapters := []runner.Adapter{playwright.New()}
	plan, err := runner.BuildPlan([]string{spec}, runner.PlanOptions{MaxJobs: 1, MaxParallel: 1, MaxAttempts: 1, MaxOutputBytes: 4 << 20, Adapters: adapters})
	if err != nil {
		return nil, toolError{"plan: " + err.Error()}
	}
	if len(plan.Jobs) != 1 {
		return nil, toolError{"not a Playwright spec this runner can execute: " + spec}
	}
	summary, err := runner.Execute(ctx, plan, runner.ExecuteOptions{Workers: 1, Timeout: timeout, MaxAttempts: 1, MaxOutputBytes: 4 << 20, PassEnv: s.passEnv, ReceiptDir: s.receiptDir, Adapters: adapters})
	var setup runner.SetupError
	if errors.As(err, &setup) {
		return nil, toolError{setup.Error()}
	}
	result := mcpRunResult{Version: MCPToolResultVersion, Spec: spec, RunID: summary.RunID}
	switch runExitCode(summary, err != nil) {
	case exitPassed:
		result.Status = "passed"
	case exitFailed:
		result.Status = "failed"
	default:
		result.Status = "incomplete"
	}
	if len(summary.Receipts) > 0 {
		receipt := summary.Receipts[len(summary.Receipts)-1]
		result.ExecutedTests, result.FailureCount, result.SkippedTests = receipt.ExecutedTests, receipt.FailureCount, receipt.SkippedTests
		result.Reason, result.Receipt = bounded(receipt.Error), receipt.ReceiptPath
		if receipt.Status != runner.StatusPassed && receipt.Evidence.StdoutPath != "" {
			if raw, readErr := os.ReadFile(receipt.Evidence.StdoutPath); readErr == nil {
				result.Failure = bounded(playwright.FailureContext(raw))
			}
		}
		result.Failures = receipt.Failures
		result.FailureContext = firstFailureContext(receipt.Failures)
	} else if err != nil {
		result.Reason = "execution did not complete"
	}
	return result, nil
}

type mcpHealResult struct {
	Version int    `json:"version"`
	Spec    string `json:"spec"`
	tier2.Session
	Diff  string `json:"diff,omitempty"`
	Note  string `json:"note,omitempty"`
	Error string `json:"error,omitempty"`
}

func (s *mcpServer) healTest(ctx context.Context, spec string, apply bool, proposals int, timeout time.Duration) (any, error) {
	select {
	case s.healSlot <- struct{}{}:
		defer func() { <-s.healSlot }()
	case <-ctx.Done():
		return nil, ctx.Err()
	}
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}
	// Native healing reads at most 1 MiB of source; refuse a larger file
	// before reading it.
	if info, err := os.Stat(spec); err != nil || info.Size() > tier2.MaxSourceBytes {
		return nil, toolError{"spec cannot be read or exceeds the 1 MiB healing input limit"}
	}
	original, err := os.ReadFile(spec)
	if err != nil {
		return nil, toolError{"spec cannot be read"}
	}
	// apply=true is the caller's explicit approval; nothing is applied without it.
	session, err := healSpec(ctx, healOptions{Spec: spec, Provider: s.provider, Model: s.model, MaxProposals: proposals, RunTimeout: timeout, ReceiptDir: s.healReceipt, PassEnv: s.passEnv, Apply: apply})
	if err != nil && session.State == "" {
		return nil, toolError{"healing did not start: " + bounded(err.Error())}
	}
	result := mcpHealResult{Version: MCPToolResultVersion, Spec: spec, Session: session}
	result.Original.Failure, result.Tier1.Failure, result.Verified.Failure = bounded(session.Original.Failure), bounded(session.Tier1.Failure), bounded(session.Verified.Failure)
	candidatePath := session.SavedPath
	if session.Applied {
		candidatePath = spec
	}
	if candidatePath != "" {
		if candidate, readErr := os.ReadFile(candidatePath); readErr == nil {
			result.Diff = unifiedLines(string(original), string(candidate))
		}
	}
	if session.State == "needs_human" {
		result.Note = "possible real bug: an assertion failed, and healing does not rewrite assertions to force a pass"
	}
	if err != nil {
		// The session's evidence stays in the result; nothing was saved or
		// applied unless savedPath or applied says so.
		result.Error = healFailure(session, apply)
		return nil, failedResult{result}
	}
	return result, nil
}

func (s *mcpServer) assessTest(ctx context.Context, path, requirements string) (any, error) {
	args := []string{path, "--format", "json"}
	if requirements != "" {
		args = append(args, "--requirements", requirements)
	}
	var out, errOut bytes.Buffer
	code := assessContext(ctx, args, &out, &errOut)
	var report json.RawMessage
	if json.Unmarshal(out.Bytes(), &report) != nil {
		return nil, toolError{"assessment failed: " + bounded(strings.TrimSpace(strings.TrimPrefix(errOut.String(), "9l: ")))}
	}
	if code != 0 {
		// A suite report is still written when some files could not be assessed.
		return map[string]any{"version": MCPToolResultVersion, "report": report, "error": bounded(strings.TrimSpace(errOut.String()))}, nil
	}
	return map[string]any{"version": MCPToolResultVersion, "report": report}, nil
}

func (s *mcpServer) confirmFinding(ctx context.Context, options confirmOptions) (any, error) {
	report, saved, err := confirmFinding(ctx, options, io.Discard)
	var setup confirm.SetupError
	if errors.As(err, &setup) {
		return nil, toolError{bounded(setup.Message)}
	}
	if err != nil {
		return nil, toolError{"confirmation failed: " + bounded(err.Error())}
	}
	if saved == "" {
		return nil, failedResult{map[string]any{"version": MCPToolResultVersion, "report": report, "error": "the confirmation report could not be saved"}}
	}
	return map[string]any{"version": MCPToolResultVersion, "report": report, "reportPath": saved}, nil
}

// terminalEscape matches the ANSI color and style sequences Playwright
// writes into its error messages.
var terminalEscape = regexp.MustCompile(`\x1b\[[0-9;]*[A-Za-z]`)

// firstFailureContext reads the first error-context attachment, redacted
// and bounded. The file holds page content, so it is returned to the caller
// and never persisted by this path.
func firstFailureContext(failures []runner.TestFailure) string {
	for _, failure := range failures {
		for _, attachment := range failure.Attachments {
			if attachment.Name != "error-context" || attachment.Bytes > 1<<20 {
				continue
			}
			raw, err := os.ReadFile(attachment.Path)
			if err != nil {
				return ""
			}
			return boundedTo(runner.RedactText(string(raw)), mcpMaxFailureContext)
		}
	}
	return ""
}

// bounded keeps returned diagnostics small, free of terminal escapes and on
// valid UTF-8 boundaries.
func bounded(text string) string { return boundedTo(text, mcpMaxText) }

func boundedTo(text string, limit int) string {
	text = terminalEscape.ReplaceAllString(text, "")
	if len(text) <= limit {
		return text
	}
	cut := limit
	for cut > 0 && !utf8RuneStart(text[cut]) {
		cut--
	}
	return text[:cut] + "…"
}

func utf8RuneStart(b byte) bool { return b&0xC0 != 0x80 }

func (s *mcpServer) result(id json.RawMessage, result any) {
	s.send(map[string]any{"jsonrpc": "2.0", "id": id, "result": result})
}

func (s *mcpServer) toolResult(id json.RawMessage, payload any, isError bool) {
	text, err := json.MarshalIndent(payload, "", "  ")
	if err != nil {
		text, isError = []byte(`{"error":"result could not be encoded"}`), true
	}
	s.result(id, map[string]any{"content": []map[string]any{{"type": "text", "text": string(text)}}, "isError": isError})
}

// send writes one message per line; stdout carries nothing else.
func (s *mcpServer) send(message any) {
	data, err := json.Marshal(message)
	if err != nil {
		return
	}
	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	_, _ = s.out.Write(append(data, '\n'))
}
