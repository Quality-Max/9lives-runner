// Package playwrightsdk implements the opt-in, attempt-bound SDK engine bridge.
package playwrightsdk

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"unicode/utf8"

	"github.com/Quality-Max/9lives-runner/internal/adapters/playwright"
	"github.com/Quality-Max/9lives-runner/internal/runner"
)

type Adapter struct {
	skipPins       map[string]bool
	failureDetails bool
}

const maxSkipPins = 256
const maxSkipPinBytes = 2048

func New() Adapter                        { return Adapter{} }
func (Adapter) Name() string              { return "playwright-sdk" }
func (Adapter) Supports(path string) bool { return playwright.New().Supports(path) }

func (adapter Adapter) Plan(path, input string, index int) (runner.Job, error) {
	// Resolve configuration separately from workspace-hoisted dependencies.
	// A nested package manifest must not hide its owning Playwright config.
	project, config, binary, reporter := "", "", "", ""
	for directory := filepath.Dir(path); ; directory = filepath.Dir(directory) {
		if config == "" {
			for _, extension := range []string{"ts", "js", "mts", "mjs", "cts", "cjs"} {
				candidate := filepath.Join(directory, "playwright.config."+extension)
				if regular(candidate) {
					config = candidate
					break
				}
			}
		}
		if project == "" && declaresPlaywright(filepath.Join(directory, "package.json")) {
			project = directory
		}
		candidate := filepath.Join(directory, "node_modules", ".bin", "playwright")
		if runtime.GOOS == "windows" {
			candidate += ".cmd"
		}
		if binary == "" && regular(candidate) {
			binary = candidate
		}
		candidate = filepath.Join(directory, "node_modules", "@9l", "playwright", "dist", "reporter.js")
		if reporter == "" && regular(candidate) {
			reporter = candidate
		}
		if filepath.Dir(directory) == directory {
			break
		}
	}
	if config != "" {
		project = filepath.Dir(config)
	}
	if project == "" || binary == "" || reporter == "" {
		return runner.Job{}, fmt.Errorf("SDK engine unavailable: install @playwright/test and build @9l/playwright before using --sdk")
	}
	relative, err := filepath.Rel(project, path)
	if err != nil {
		return runner.Job{}, fmt.Errorf("cannot resolve SDK spec")
	}
	reporters := reporter
	if adapter.failureDetails {
		// Playwright's own JSON report goes to a private file beside the
		// evidence stream; it explains failures and never validates a run.
		reporters += ",json"
	}
	command, err := playwright.InstalledCommand(binary, "test", playwright.TestFileFilter(project, relative), "--reporter="+reporters)
	if err != nil {
		return runner.Job{}, err
	}
	if config != "" {
		command = append(command, "--config="+config)
	}
	return runner.Job{
		ID: fmt.Sprintf("job-%03d", index), Input: input, Spec: path,
		WorkDir: project, Adapter: adapter.Name(), DependsOn: []string{},
		Command: command, Env: map[string]string{"NINELIVES_ENGINE_PROTOCOL": Version},
	}, nil
}

func declaresPlaywright(path string) bool {
	raw, err := os.ReadFile(path)
	if err != nil {
		return false
	}
	var manifest struct {
		Dependencies    map[string]string `json:"dependencies"`
		DevDependencies map[string]string `json:"devDependencies"`
	}
	return json.Unmarshal(raw, &manifest) == nil && (manifest.Dependencies["@playwright/test"] != "" || manifest.DevDependencies["@playwright/test"] != "")
}

func regular(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.Mode().IsRegular()
}

func (Adapter) Validate([]byte) (runner.Validation, error) {
	return runner.Validation{}, fmt.Errorf("SDK engine evidence requires an owning attempt identity")
}

// EvidenceEnv names the private per-attempt file the reporter writes. The
// worker's stdout belongs to configuration, hooks and dependencies.
func (Adapter) EvidenceEnv() string { return "NINELIVES_ENGINE_EVENTS" }

func (adapter Adapter) ValidateAttempt(raw []byte, identity runner.AttemptIdentity) (runner.Validation, error) {
	return validate(raw, identity, adapter.skipPins)
}

// WithSkipPins declares tests that may be skipped without making the attempt
// incomplete. A pin is Playwright's title path below the project:
// "<file relative to testDir> › <describe>... › <title>", with / separators.
// Unpinned skips still fail validation, and pinned skips stay counted as
// skipped, never as executed.
func (adapter Adapter) WithSkipPins(pins []string) (Adapter, error) {
	if len(pins) > maxSkipPins {
		return adapter, fmt.Errorf("at most %d skip pins are supported", maxSkipPins)
	}
	keys := make(map[string]bool, len(pins))
	for _, pin := range pins {
		if len(pin) > maxSkipPinBytes || !utf8.ValidString(pin) || strings.ContainsAny(pin, "\r\n\x00") || strings.TrimSpace(pin) != pin || !strings.Contains(pin, " › ") {
			return adapter, fmt.Errorf("skip pin must be \"<file> › <title>\" on one line")
		}
		keys[SkipPinKey(pin)] = true
	}
	adapter.skipPins = keys
	return adapter, nil
}

// SkipPinKey is the digest the SDK reporter sends for a skipped test, so test
// titles never cross the bridge. It must match skipPinKey in protocol.ts.
func SkipPinKey(pin string) string {
	sum := sha256.Sum256([]byte(pin))
	return hex.EncodeToString(sum[:])
}

// WithFailureDetails also records each failed test's title, failing line,
// error and attachments in the receipt. The engine protocol carries none of
// them by design, so they come from Playwright's JSON reporter, written to a
// separate private file that never validates the attempt.
func (adapter Adapter) WithFailureDetails(enabled bool) Adapter {
	adapter.failureDetails = enabled
	return adapter
}

// DiagnosticsEnv names Playwright's JSON report file when failure details
// are enabled.
func (adapter Adapter) DiagnosticsEnv() string {
	if !adapter.failureDetails {
		return ""
	}
	return "PLAYWRIGHT_JSON_OUTPUT_FILE"
}

func (Adapter) SelectLine(job runner.Job, line int) (runner.Job, error) {
	return playwright.SelectLine(job, line)
}

func (Adapter) Failures(report []byte, workDir string) []runner.TestFailure {
	return playwright.New().Failures(report, workDir)
}

func (Adapter) SanitizeEvidence(stdout, _ []byte, validated bool) ([]byte, []byte) {
	if !validated {
		// Invalid or interrupted streams may contain arbitrary worker payloads.
		// Keep the bounded reason and termination in the receipt, not the payload.
		return nil, nil
	}
	return stdout, nil
}
