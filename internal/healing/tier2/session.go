package tier2

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/Quality-Max/9lives-runner/internal/healing"
)

// RunFunc executes exactly the supplied physical spec and returns a verified
// result. The caller supplies runner-backed execution so Tier 2 cannot invent
// receipts or accidentally reuse an earlier run's artifacts.
type RunFunc func(context.Context, string, string) RunResult
type RunResult struct {
	Passed        bool
	ExecutedTests int
	Failure       string
	Receipt       string
}

type SessionOptions struct {
	Spec, Framework, Model string
	MaxProposals           int
	Apply                  bool
	Interactive            func(context.Context) bool
	Preview                func(original, candidate string)
	Provider               Provider
	Run                    RunFunc
}

const (
	// Original and Tier 1 retain the broad local-file limit. Tier 2 additionally
	// limits provider admission so an argv/API prompt is predictably bounded.
	maxPromptSourceBytes = 1 << 20
	maxTier2SourceBytes  = 8 << 10
	maxTier2PromptBytes  = 32 << 10
	maxFailureBytes      = 4 << 10
	maxPageHTMLBytes     = 3000
	maxConsoleEntries    = 10
	maxConsoleEntryBytes = 1 << 10
	maxConsoleBytes      = 4 << 10
)

type Session struct {
	OriginalHash  string    `json:"originalHash"`
	CandidateHash string    `json:"candidateHash,omitempty"`
	Original      RunResult `json:"original"`
	Tier1         RunResult `json:"tier1"`
	Verified      RunResult `json:"verified"`
	State         string    `json:"state"`
	Applied       bool      `json:"applied"`
	SavedPath     string    `json:"savedPath,omitempty"`
	Reason        string    `json:"reason,omitempty"`
}

// Heal runs original, then at most one caller-provided Tier 1 verification,
// then each Tier 2 candidate in a fresh owned copy. A candidate is verified
// only when a non-zero test count passed in its own run.
func Heal(ctx context.Context, opts SessionOptions, tier1 func(string, string) (string, bool)) (result Session, err error) {
	if opts.Run == nil || opts.Provider == nil || opts.Spec == "" {
		return result, errors.New("native healing requires spec, provider, and runner")
	}
	if opts.MaxProposals <= 0 {
		opts.MaxProposals = 1
	}
	original, err := os.ReadFile(opts.Spec)
	if err != nil {
		return result, err
	}
	if len(original) > maxPromptSourceBytes {
		return result, errors.New("source file exceeds native healing input limit")
	}
	cleanupStaleOwnedCopies(opts.Spec)
	result.OriginalHash = hash(original)
	result.Original = opts.Run(ctx, opts.Spec, "original")
	if err := ctx.Err(); err != nil {
		result.State = "canceled"
		return result, err
	}
	if result.Original.Passed && result.Original.ExecutedTests > 0 {
		result.State = "passed"
		return result, nil
	}
	if result.Original.ExecutedTests == 0 {
		result.State = "unverified"
		result.Reason = "original run completed zero tests"
		return result, nil
	}
	if healing.Classify(result.Original.Failure, "") == "assertion_failed" {
		result.State = "needs_human"
		result.Reason = "assertion failure is outside native healing"
		return result, nil
	}
	if _, editable := editableFailure(result.Original.Failure); !editable {
		result.State = "unverified"
		result.Reason = "original failure is not an editable locator action"
		return result, nil
	}
	// Tier 1 is one attempt only. Its broader heuristic proposal must satisfy the
	// same exact source boundary before it is allowed to execute.
	if proposal, ok := tier1(string(original), result.Original.Failure); ok {
		selector, editable := editableFailure(result.Original.Failure)
		if editable && SafeCandidate(string(original), preserveNewlines(string(original), proposal), selector, opts.Framework) == nil {
			proposal = preserveNewlines(string(original), proposal)
			if verified := verify(ctx, opts, original, proposal, "tier1"); verified.Passed && verified.ExecutedTests == result.Original.ExecutedTests {
				result.Tier1 = verified
				return finish(ctx, opts, original, proposal, verified, result)
			} else {
				result.Tier1 = verified
				if ctx.Err() != nil {
					result.State = "canceled"
					return result, ctx.Err()
				}
				if result.stopAfterFailedVerification(verified, result.Original.ExecutedTests) {
					return result, nil
				}
			}
		}
	}
	if ctx.Err() != nil {
		result.State = "canceled"
		return result, ctx.Err()
	}
	latestSource, latestFailure := string(original), result.Original.Failure
	for attempt := 0; attempt < opts.MaxProposals; attempt++ {
		if len(latestSource) > maxTier2SourceBytes {
			result.State = "unverified"
			result.Reason = "source file exceeds native Tier 2 provider limit"
			return result, nil
		}
		failedSelector, editable := editableFailure(latestFailure)
		if !editable {
			if healing.Classify(latestFailure, "") == "assertion_failed" {
				result.State = "needs_human"
				result.Reason = "candidate verification reached an assertion failure"
			} else {
				result.State = "unverified"
				result.Reason = "failed run is not an editable locator action"
			}
			return result, nil
		}
		prompt := Prompt(opts.Framework, latestSource, latestFailure)
		if len(prompt) > maxTier2PromptBytes {
			result.State = "unverified"
			result.Reason = "native Tier 2 prompt exceeds provider limit"
			return result, nil
		}
		response, callErr := opts.Provider.Complete(ctx, prompt, opts.Model)
		if callErr != nil {
			if errors.Is(callErr, context.Canceled) || errors.Is(callErr, context.DeadlineExceeded) || ctx.Err() != nil {
				result.State = "canceled"
				return result, callErr
			}
			result.State = "provider_error"
			result.Reason = "provider did not return a usable candidate"
			return result, callErr
		}
		candidate, parseErr := ParseCandidate(response, latestSource)
		if parseErr != nil {
			result.Reason = "provider candidate refused"
			continue
		}
		candidate = preserveNewlines(string(original), candidate)
		if SafeCandidate(latestSource, candidate, failedSelector, opts.Framework) != nil {
			result.Reason = "provider candidate is not an exact failed-locator selector replacement"
			continue
		}
		verified := verify(ctx, opts, original, candidate, fmt.Sprintf("tier2-%d", attempt+1))
		if verified.Passed && verified.ExecutedTests == result.Original.ExecutedTests {
			return finish(ctx, opts, original, candidate, verified, result)
		}
		result.Verified = verified
		latestSource, latestFailure = candidate, verified.Failure
		if ctx.Err() != nil {
			result.State = "canceled"
			return result, ctx.Err()
		}
		if result.stopAfterFailedVerification(verified, result.Original.ExecutedTests) {
			return result, nil
		}
	}
	result.State = "unverified"
	return result, nil
}

// Every executed candidate must remain an editable locator failure before
// another proposal is attempted, including Tier 1 and the last Tier 2 attempt.
func (result *Session) stopAfterFailedVerification(verified RunResult, expectedTests int) bool {
	if healing.Classify(verified.Failure, "") == "assertion_failed" {
		result.State = "needs_human"
		result.Reason = "candidate verification reached an assertion failure"
		return true
	}
	if verified.ExecutedTests != expectedTests {
		result.State = "unverified"
		result.Reason = "candidate verification did not execute the original test count"
		return true
	}
	if _, editable := editableFailure(verified.Failure); !editable {
		result.State = "unverified"
		result.Reason = "failed run is not an editable locator action"
		return true
	}
	return false
}

func verify(ctx context.Context, opts SessionOptions, original []byte, candidate, label string) RunResult {
	copyPath, err := ownedCopy(opts.Spec, []byte(candidate))
	if err != nil {
		return RunResult{Failure: "could not create isolated candidate"}
	}
	defer removeOwnedCopy(copyPath)
	return opts.Run(ctx, copyPath, label)
}
func finish(ctx context.Context, opts SessionOptions, original []byte, candidate string, verified RunResult, result Session) (Session, error) {
	result.Verified = verified
	result.CandidateHash = hash([]byte(candidate))
	result.State = "verified"
	if opts.Preview != nil && !opts.Apply {
		opts.Preview(string(original), candidate)
	}
	approved := opts.Apply || (opts.Interactive != nil && opts.Interactive(ctx))
	if err := ctx.Err(); err != nil {
		result.State = "canceled"
		return result, err
	}
	if !approved {
		path := opts.Spec + ".healed"
		if err := writeAtomically(path, []byte(candidate), 0600); err != nil {
			return result, err
		}
		result.SavedPath = path
		return result, nil
	}
	current, err := os.ReadFile(opts.Spec)
	if err != nil {
		return result, err
	}
	if hash(current) != hash(original) {
		result.State = "concurrent_edit"
		return result, errors.New("source changed during healing; refusing apply")
	}
	info, err := os.Stat(opts.Spec)
	if err != nil {
		return result, err
	}
	if err := ctx.Err(); err != nil {
		result.State = "canceled"
		return result, err
	}
	if err := writeAtomically(opts.Spec, []byte(candidate), info.Mode()); err != nil {
		return result, err
	}
	result.Applied = true
	result.State = "applied"
	return result, nil
}

func writeAtomically(path string, contents []byte, mode os.FileMode) error {
	temporary, err := os.CreateTemp(filepath.Dir(path), ".9lives-heal-write-*")
	if err != nil {
		return err
	}
	temporaryPath := temporary.Name()
	defer os.Remove(temporaryPath)
	if _, err := temporary.Write(contents); err != nil {
		temporary.Close()
		return err
	}
	if err := temporary.Chmod(mode.Perm()); err != nil {
		temporary.Close()
		return err
	}
	if err := temporary.Close(); err != nil {
		return err
	}
	return os.Rename(temporaryPath, path)
}
func ownedCopy(spec string, source []byte) (string, error) {
	random := make([]byte, 6)
	if _, err := rand.Read(random); err != nil {
		return "", err
	}
	nonce := hex.EncodeToString(random)
	path := ownedCopyPath(spec, nonce)
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return "", err
	}
	_, writeErr := file.Write(source)
	closeErr := file.Close()
	if writeErr != nil {
		_ = os.Remove(path)
		return "", writeErr
	}
	if closeErr != nil {
		_ = os.Remove(path)
		return "", closeErr
	}
	metadata, err := json.Marshal(ownedCopyMetadata{Version: ownedCopyMetadataVersion, Spec: filepath.Clean(spec), PID: os.Getpid(), Nonce: nonce})
	if err != nil {
		_ = os.Remove(path)
		return "", err
	}
	ownerPath := path + ".owner"
	owner, err := os.OpenFile(ownerPath, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		_ = os.Remove(path)
		return "", err
	}
	if _, err := owner.Write(metadata); err != nil {
		_ = owner.Close()
		_ = os.Remove(ownerPath)
		_ = os.Remove(path)
		return "", err
	}
	if err := owner.Close(); err != nil {
		_ = os.Remove(ownerPath)
		_ = os.Remove(path)
		return "", err
	}
	return path, nil
}

const ownedCopyMetadataVersion = 1

type ownedCopyMetadata struct {
	Version int
	Spec    string
	PID     int
	Nonce   string
}

func ownedCopyPath(spec, nonce string) string {
	base := filepath.Base(spec)
	ext := filepath.Ext(base)
	stem := strings.TrimSuffix(base, ext)
	owned := ".9lives-heal-" + nonce
	// Keep Playwright's recognised .spec/.test suffix after our owned marker.
	if i := strings.LastIndex(stem, ".spec"); i >= 0 {
		base = stem[:i] + owned + stem[i:] + ext
	} else if i := strings.LastIndex(stem, ".test"); i >= 0 {
		base = stem[:i] + owned + stem[i:] + ext
	} else {
		base = "." + stem + owned + ext
	}
	return filepath.Join(filepath.Dir(spec), base)
}

func validOwnedNonce(nonce string) bool {
	if len(nonce) != 12 {
		return false
	}
	for _, character := range nonce {
		if !(character >= '0' && character <= '9' || character >= 'a' && character <= 'f') {
			return false
		}
	}
	return true
}

func removeOwnedCopy(path string) {
	_ = os.Remove(path)
	_ = os.Remove(path + ".owner")
}

// cleanupStaleOwnedCopies removes only a recognized, same-spec copy whose
// recorded process no longer exists. Unknown metadata and live/reused PIDs are
// deliberately preserved to avoid touching another user's working file.
func cleanupStaleOwnedCopies(spec string) {
	dir := filepath.Dir(spec)
	entries, err := filepath.Glob(filepath.Join(dir, "*.9lives-heal-*.owner"))
	if err != nil {
		return
	}
	cleanSpec := filepath.Clean(spec)
	for _, ownerPath := range entries {
		copyPath := strings.TrimSuffix(ownerPath, ".owner")
		ownerInfo, ownerErr := os.Lstat(ownerPath)
		copyInfo, copyErr := os.Lstat(copyPath)
		if ownerErr != nil || copyErr != nil || !ownerInfo.Mode().IsRegular() || !copyInfo.Mode().IsRegular() {
			continue
		}
		metadataBytes, err := os.ReadFile(ownerPath)
		if err != nil {
			continue
		}
		var metadata ownedCopyMetadata
		if json.Unmarshal(metadataBytes, &metadata) != nil || metadata.Version != ownedCopyMetadataVersion || metadata.Spec != cleanSpec || metadata.PID <= 0 || !validOwnedNonce(metadata.Nonce) || copyPath != ownedCopyPath(cleanSpec, metadata.Nonce) {
			continue
		}
		if pidMayBeAlive(metadata.PID) {
			continue
		}
		removeOwnedCopy(copyPath)
	}
}
func hash(value []byte) string { sum := sha256.Sum256(value); return hex.EncodeToString(sum[:]) }
func preserveNewlines(original, candidate string) string {
	candidate = strings.ReplaceAll(candidate, "\r\n", "\n")
	if !strings.HasSuffix(original, "\n") {
		// A closing Markdown fence conventionally starts after one newline. Remove
		// only that delimiter newline when the executed source had none.
		candidate = strings.TrimSuffix(candidate, "\n")
	}
	if strings.Contains(original, "\r\n") {
		return strings.ReplaceAll(candidate, "\n", "\r\n")
	}
	return candidate
}

func editableFailure(failure string) (string, bool) {
	// A locator quoted in a network/syntax/navigation failure is contextual
	// evidence, not an editable locator action. Fail closed before escalation.
	lower := strings.ToLower(failure)
	for _, unsafe := range []string{"network", "syntax", "navigation", "flow changed"} {
		if strings.Contains(lower, unsafe) {
			return "", false
		}
	}
	selector := healing.ExtractSelector(failure, "")
	if selector == "" {
		return "", false
	}
	switch healing.Classify(failure, "") {
	case "locator_not_found", "locator_timeout", "element_not_visible":
		return selector, true
	default:
		return "", false
	}
}

// Prompt carries the native framework contract. Page and console fields are
// optional because the current Playwright adapter has no durable page snapshot
// channel yet; callers may provide bounded diagnostic context without changing
// the complete source supplied for a repair.
func Prompt(framework, source, failure string) string {
	return PromptWithContext(framework, source, failure, "", nil)
}

func PromptWithContext(framework, source, failure, pageHTML string, console []string) string {
	label, language := "Playwright", "javascript"
	switch strings.ToLower(framework) {
	case "cypress":
		label = "Cypress"
	case "selenium", "python", "pytest":
		label, language = "Selenium (Python + pytest)", "python"
	}
	codeFence := fenceDelimiter(source)
	var prompt strings.Builder
	fmt.Fprintf(&prompt, "A %s test is failing. Please suggest a fix.\n\n## Error Details\nError Message: %s\n\n## Test Code\n%s%s\n%s\n%s\n", label, truncate(failure, maxFailureBytes), codeFence, language, source, codeFence)
	if pageHTML != "" {
		pageHTML = truncate(pageHTML, maxPageHTMLBytes)
		pageFence := fenceDelimiter(pageHTML)
		fmt.Fprintf(&prompt, "\n## Page state at failure (snippet)\n%s\n%s\n%s\n", pageFence, pageHTML, pageFence)
	}
	if logs := boundedConsole(console); logs != "" {
		fmt.Fprintf(&prompt, "\n## Console Logs\n%s\n", logs)
	}
	fmt.Fprintf(&prompt, "\n## Instructions\n1. Analyze why the test is failing\n2. Suggest a minimal fix: change only the line(s) that cause THIS failure\n3. Do not edit comments, imports, or other tests; do not reformat or add advice\n4. Do not describe anything as tested or verified — 9lives re-runs the test to verify\n5. Provide the corrected code (the COMPLETE test file)\n6. Keep REASONING and CHANGES concise\n\n## Output Format\nREASONING: <brief analysis>\nCHANGES: <brief list>\nCODE:\n%s%s\n<corrected code>\n%s", codeFence, language, codeFence)
	// The independently bounded fields total well below the provider's 32 KiB
	// admission limit while preserving the complete Tier 2 source.
	return prompt.String()
}

func boundedConsole(console []string) string {
	var b strings.Builder
	for i, entry := range console {
		if i == maxConsoleEntries || b.Len() == maxConsoleBytes {
			break
		}
		entry = truncate(entry, maxConsoleEntryBytes)
		if b.Len() > 0 {
			b.WriteByte('\n')
		}
		remaining := maxConsoleBytes - b.Len()
		b.WriteString(truncate(entry, remaining))
	}
	return b.String()
}

func fenceDelimiter(value string) string {
	longest, run := 0, 0
	for _, ch := range value {
		if ch == '`' {
			run++
			if run > longest {
				longest = run
			}
		} else {
			run = 0
		}
	}
	if longest < 2 {
		return "```"
	}
	return strings.Repeat("`", longest+1)
}

func truncate(value string, limit int) string {
	if len(value) > limit {
		return value[:limit]
	}
	return value
}
