package playwright

import (
	"testing"

	"github.com/Quality-Max/9lives-runner/internal/runner"
)

func TestClassifyErrorReadsTargetSetupReasons(t *testing.T) {
	for message, want := range map[string]string{
		"Error: 9lives target stopped: setup_blocked\n    at app.open (targets.js:1:1)": runner.FailureSetup,
		"Error: 9lives target stopped: setup_dirty":                                     runner.FailureSetup,
		"Error: 9lives target stopped: setup_mismatch":                                  runner.FailureSetup,
		"Error: 9lives target stopped: setup_unknown":                                   runner.FailureSetup,
		"Error: 9lives target stopped: assertion_failed":                                runner.FailureOther,
		"Error: 9lives target stopped: setup_blocked_by_me":                             runner.FailureOther,
		"Error: the device said 9lives target stopped: setup_blocked":                   runner.FailureOther,
		"Error: expect(received).toBe(expected)":                                        runner.FailureAssertion,
	} {
		if got := classifyError(message); got != want {
			t.Errorf("%q: got %q, want %q", message, got, want)
		}
	}
}
