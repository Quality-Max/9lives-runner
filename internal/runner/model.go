// Package runner implements the framework-neutral execution core for 9lives.
package runner

import "time"

type Plan struct {
	Version   int       `json:"version"`
	RunID     string    `json:"runId"`
	CreatedAt time.Time `json:"createdAt"`
	Jobs      []Job     `json:"jobs"`
	Skipped   []Skipped `json:"skipped"`
	Limits    Limits    `json:"limits"`
}

type Limits struct {
	MaxJobs        int   `json:"maxJobs"`
	MaxParallel    int   `json:"maxParallel"`
	MaxAttempts    int   `json:"maxAttempts"`
	MaxOutputBytes int   `json:"maxOutputBytes"`
	DeadlineMS     int64 `json:"deadlineMs"`
}

type Job struct {
	ID        string            `json:"id"`
	Input     string            `json:"input"`
	Spec      string            `json:"spec"`
	WorkDir   string            `json:"workDir"`
	Adapter   string            `json:"adapter"`
	Command   []string          `json:"command"`
	Env       map[string]string `json:"-"`
	DependsOn []string          `json:"dependsOn"`
}

type Skipped struct {
	Input  string `json:"input"`
	Reason string `json:"reason"`
}

type ReceiptStatus string

const (
	StatusPassed   ReceiptStatus = "passed"
	StatusFailed   ReceiptStatus = "failed"
	StatusCanceled ReceiptStatus = "canceled"
	StatusTimedOut ReceiptStatus = "timed_out"
	StatusError    ReceiptStatus = "error"
)

// Receipt is one job's immutable execution record. Executed and Validated are
// separate so a launched process with missing evidence cannot be called green.
type Receipt struct {
	Goals              []GoalReceipt `json:"goals,omitempty"`
	Version            int           `json:"version"`
	RunID              string        `json:"runId"`
	JobID              string        `json:"jobId"`
	AttemptID          string        `json:"attemptId"`
	Attempt            int           `json:"attempt"`
	Spec               string        `json:"spec"`
	Adapter            string        `json:"adapter"`
	Status             ReceiptStatus `json:"status"`
	StartedAt          time.Time     `json:"startedAt"`
	FinishedAt         time.Time     `json:"finishedAt"`
	DurationMS         int64         `json:"durationMs"`
	ExitCode           int           `json:"exitCode"`
	Executed           bool          `json:"executed"`
	Validated          bool          `json:"validated"`
	Validation         string        `json:"validation,omitempty"`
	GoalFailed         bool          `json:"goalFailed,omitempty"`
	FailureCount       int           `json:"failureCount"`
	ExecutedTests      int           `json:"executedTests"`
	SkippedTests       int           `json:"skippedTests"`
	VerifiedAssertions int           `json:"verifiedAssertions"`
	AssertionCoverage  string        `json:"assertionCoverage"`
	Error              string        `json:"error,omitempty"`
	Termination        *Termination  `json:"termination,omitempty"`
	Evidence           Evidence      `json:"evidence"`
	ReceiptPath        string        `json:"receiptPath,omitempty"`
}

type Termination struct {
	Kind              string `json:"kind"`
	Detail            string `json:"detail,omitempty"`
	ObservedSignal    int    `json:"observedSignal,omitempty"`
	Signal            string `json:"signal,omitempty"`
	EscalationSignal  string `json:"escalationSignal,omitempty"`
	EscalationAfterMS int64  `json:"escalationAfterMs,omitempty"`
}

type Evidence struct {
	Command         []string            `json:"command"`
	StdoutPath      string              `json:"stdoutPath,omitempty"`
	StderrPath      string              `json:"stderrPath,omitempty"`
	StdoutSHA256    string              `json:"stdoutSha256,omitempty"`
	StderrSHA256    string              `json:"stderrSha256,omitempty"`
	StdoutTruncated bool                `json:"stdoutTruncated,omitempty"`
	StderrTruncated bool                `json:"stderrTruncated,omitempty"`
	Artifacts       []ArtifactReference `json:"artifacts"`
}

type ArtifactReference struct {
	Kind     string `json:"kind"`
	Path     string `json:"path,omitempty"`
	SHA256   string `json:"sha256,omitempty"`
	Bytes    int64  `json:"bytes"`
	Required bool   `json:"required"`
	Present  bool   `json:"present"`
}

type ProgressEvent struct {
	Version   int       `json:"version"`
	Sequence  int64     `json:"sequence"`
	RunID     string    `json:"runId"`
	JobID     string    `json:"jobId,omitempty"`
	AttemptID string    `json:"attemptId,omitempty"`
	Type      string    `json:"type"`
	Timestamp time.Time `json:"timestamp"`
	Detail    string    `json:"detail,omitempty"`
}

type RunSummary struct {
	RunID      string    `json:"runId"`
	StartedAt  time.Time `json:"startedAt"`
	FinishedAt time.Time `json:"finishedAt"`
	DurationMS int64     `json:"durationMs"`
	Complete   bool      `json:"complete"`
	Passed     int       `json:"passed"`
	Failed     int       `json:"failed"`
	Canceled   int       `json:"canceled"`
	TimedOut   int       `json:"timedOut"`
	Errors     int       `json:"errors"`
	Receipts   []Receipt `json:"receipts"`
}

type RunStatus struct {
	RunID     string         `json:"runId"`
	State     string         `json:"state"`
	Plan      *Plan          `json:"plan,omitempty"`
	Result    *RunSummary    `json:"result,omitempty"`
	LastEvent *ProgressEvent `json:"lastEvent,omitempty"`
}
