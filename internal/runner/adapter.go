package runner

import "context"

// AttemptService is an optional, owned in-memory engine service. Environment
// contains only its local transport address; provider credentials stay here.
// CredentialNames lists the environment variables the service itself uses;
// the runner withholds them from the worker even when passed explicitly.
type AttemptServiceFactory interface {
	Start(context.Context, AttemptIdentity) (AttemptService, error)
	CredentialNames() []string
}
type AttemptService interface {
	Environment() map[string]string
	Close() []GoalReceipt
}

type GoalDecisionReceipt struct {
	ID             string `json:"id"`
	Action         string `json:"action"`
	TargetID       string `json:"targetId,omitempty"`
	Outcome        string `json:"outcome"`
	InputTokens    int    `json:"inputTokens"`
	OutputTokens   int    `json:"outputTokens"`
	UsageAvailable bool   `json:"usageAvailable"`
}
type GoalReceipt struct {
	ID                  string                `json:"id"`
	Status              string                `json:"status"`
	Provider            string                `json:"provider"`
	DurationMS          int64                 `json:"durationMs"`
	ReservedTokens      int                   `json:"reservedTokens"`
	EstimatedCostMicros int64                 `json:"estimatedCostMicros"`
	CostAvailable       bool                  `json:"costAvailable"`
	Decisions           []GoalDecisionReceipt `json:"decisions"`
}

type Adapter interface {
	Name() string
	Supports(path string) bool
	Plan(path, input string, index int) (Job, error)
	Validate(stdout []byte) (Validation, error)
}

// FailureReporter adapters describe each failed test in a report that
// already validated. workDir is the job's working directory; attachment
// paths outside it are not reported.
type FailureReporter interface {
	Failures(report []byte, workDir string) []TestFailure
}

type Validation struct {
	GoalFailed bool
	// NonGoalFailureCount counts unexpected test failures in tests with no
	// failed goal steps. Legacy adapters may leave this zero; it is consulted
	// only when GoalFailed is true.
	NonGoalFailureCount int
	FailureCount        int
	ExecutedTests       int
	SkippedTests        int
	VerifiedAssertions  int
	AssertionCoverage   string
	Description         string
}

// AttemptValidator binds structured worker evidence to the attempt that owns it.
// Legacy report adapters remain source-compatible with Adapter.
type AttemptIdentity struct {
	RunID, JobID, AttemptID string
}

type AttemptValidator interface {
	ValidateAttempt(stdout []byte, identity AttemptIdentity) (Validation, error)
}

// EvidenceChannel adapters receive structured evidence through a private
// per-attempt file named by the returned environment variable, instead of the
// worker's stdout, which configuration, hooks and dependencies also write to.
// The file replaces stdout for validation and persistence.
type EvidenceChannel interface {
	EvidenceEnv() string
}

// EvidenceSanitizer can omit unsafe worker payloads before the store receives
// them. Validation is already complete; filtering cannot change the verdict.
type EvidenceSanitizer interface {
	SanitizeEvidence(stdout, stderr []byte, validated bool) ([]byte, []byte)
}

func adapterFor(adapters []Adapter, path string) Adapter {
	for _, candidate := range adapters {
		if candidate.Supports(path) {
			return candidate
		}
	}
	return nil
}

func adapterNamed(adapters []Adapter, name string) Adapter {
	for _, candidate := range adapters {
		if candidate.Name() == name {
			return candidate
		}
	}
	return nil
}
