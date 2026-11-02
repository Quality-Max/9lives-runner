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
	// Target is an explicitly selected SDK target (kind:app alias).
	Target    string            `json:"target,omitempty"`
	ID        string            `json:"id"`
	Input     string            `json:"input"`
	Spec      string            `json:"spec"`
	WorkDir   string            `json:"workDir"`
	Adapter   string            `json:"adapter"`
	Command   []string          `json:"command"`
	Env       map[string]string `json:"-"`
	DependsOn []string          `json:"dependsOn"`
	// Selection describes a test filter narrowing the spec, such as
	// `line 12` or `--grep login`; empty runs every test in it.
	Selection string `json:"selection,omitempty"`
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
	AndroidRuntimes []AndroidRuntime `json:"androidRuntimes,omitempty"`
	// TargetSetup lists the native target tests whose driver classified the
	// device or app state before, or after a failed, application assertion:
	// blocked (locked device), dirty (state left by a previous test),
	// mismatch (declared build or posture), observer (the accessibility
	// snapshot disagreed with the window manager) or unknown. A test with a record
	// whose recovery did not restore the baseline cannot pass; its failure is
	// inconclusive setup evidence, not an application defect.
	TargetSetup []TargetSetup `json:"targetSetup,omitempty"`
	// TargetDiagnostics lists bounded, metadata-only records a native driver
	// captured when a target operation failed and the test opted in: timing,
	// requested coordinates, viewport and app window counts before and after.
	// They describe the failure without claiming its cause.
	TargetDiagnostics   []TargetDiagnostic    `json:"targetDiagnostics,omitempty"`
	Target              string                `json:"target,omitempty"`
	AgentProvenance     *ProvenanceAssessment `json:"agentProvenance,omitempty"`
	Goals               []GoalReceipt         `json:"goals,omitempty"`
	Version             int                   `json:"version"`
	RunID               string                `json:"runId"`
	JobID               string                `json:"jobId"`
	AttemptID           string                `json:"attemptId"`
	Attempt             int                   `json:"attempt"`
	Spec                string                `json:"spec"`
	Adapter             string                `json:"adapter"`
	Status              ReceiptStatus         `json:"status"`
	StartedAt           time.Time             `json:"startedAt"`
	FinishedAt          time.Time             `json:"finishedAt"`
	DurationMS          int64                 `json:"durationMs"`
	ExitCode            int                   `json:"exitCode"`
	Executed            bool                  `json:"executed"`
	Validated           bool                  `json:"validated"`
	Validation          string                `json:"validation,omitempty"`
	GoalFailed          bool                  `json:"goalFailed,omitempty"`
	NonGoalFailureCount int                   `json:"nonGoalFailureCount,omitempty"`
	FailureCount        int                   `json:"failureCount"`
	ExecutedTests       int                   `json:"executedTests"`
	SkippedTests        int                   `json:"skippedTests"`
	// FlakyTests counts executed tests that failed and then passed on a
	// Playwright retry inside this attempt. They count as passed, so this is
	// what tells a clean pass from one that needed a second try.
	FlakyTests         int `json:"flakyTests,omitempty"`
	VerifiedAssertions int `json:"verifiedAssertions"`
	// TargetAssertions counts successful SDK-instrumented target checks the
	// worker reported. It is not independent verification or coverage.
	TargetAssertions  int    `json:"targetAssertions,omitempty"`
	AssertionCoverage string `json:"assertionCoverage"`
	// ReportedAssertions counts the assertion steps the test worker reported
	// as passed in each executed test's last completed attempt, and
	// PassedWithoutAssertions the tests that passed while reporting none.
	// They say whether a green test asserted anything; they are the worker's
	// report, not verified coverage, so assertionCoverage stays unknown.
	ReportedAssertions      int `json:"reportedAssertions,omitempty"`
	PassedWithoutAssertions int `json:"passedWithoutAssertions,omitempty"`
	// RuntimeExpectedFailures counts failed tests that count as expected only
	// because test.fail() ran during the attempt, and SkippedAfterAssertions
	// skipped tests that had reported a passed assertion step first. Both
	// come from the SDK evidence stream and mark results the framework
	// relabeled while the test ran; they do not change the status.
	RuntimeExpectedFailures int    `json:"runtimeExpectedFailures,omitempty"`
	SkippedAfterAssertions  int    `json:"skippedAfterAssertions,omitempty"`
	Error                   string `json:"error,omitempty"`
	// Failures describes each failed test, from the validated structured
	// report, when the adapter can attribute them. Messages are bounded and
	// redacted; attachments are referenced and copied only on request.
	Failures []TestFailure `json:"failures,omitempty"`
	// KeepAttachments records that retention was requested. FailureAttachments
	// counts the attachment files the listed failures reference (inline
	// bodies and files outside the project are never referenced) and
	// RetainedAttachments those copied into the receipt directory, so a
	// request that found nothing to copy is distinguishable from one that
	// was never made.
	KeepAttachments     bool `json:"keepAttachments,omitempty"`
	FailureAttachments  int  `json:"failureAttachments,omitempty"`
	RetainedAttachments int  `json:"retainedAttachments,omitempty"`
	// Annotations are the tests' own annotations from the validated report,
	// such as a test recording why it returned early. A passed receipt with
	// annotations may not have exercised everything its tests can check.
	Annotations []TestAnnotation `json:"annotations,omitempty"`
	// FailureSummary says why an attempt that did not pass ended, on every
	// such receipt, with or without --failure-details. It holds closed
	// tokens, counts and test digests only; see FailureSummary.
	FailureSummary *FailureSummary `json:"failureSummary,omitempty"`
	Termination    *Termination    `json:"termination,omitempty"`
	Evidence       Evidence        `json:"evidence"`
	ReceiptPath    string          `json:"receiptPath,omitempty"`
}

// AndroidRuntime is sanitized, worker-reported app/build/runtime attribution.
// It does not establish independent behavioral verification.
type AndroidRuntime struct {
	TestID       string `json:"testId"`
	AppPackage   string `json:"appPackage"`
	APKSHA256    string `json:"apkSHA256"`
	API          string `json:"api"`
	Release      string `json:"release"`
	ABI          string `json:"abi"`
	Emulator     string `json:"emulator,omitempty"`
	Device       string `json:"device,omitempty"`
	DeviceHash   string `json:"deviceHash,omitempty"`
	Appium       string `json:"appium"`
	UIAutomator2 string `json:"uiautomator2"`
	Worker       string `json:"worker"`
	// Display is the physical display the session targeted (its id or
	// "unknown"), with its pixel size and density, and Posture the fold
	// posture the device reported: opened, half-opened, closed, flat (no
	// fold) or unknown.
	Display       string `json:"display"`
	DisplayWidth  string `json:"displayWidth"`
	DisplayHeight string `json:"displayHeight"`
	Density       string `json:"density"`
	Posture       string `json:"posture"`
	// VersionCode, VersionName and Signer identify the installed build as
	// the device reported it: the package manager's versions and the
	// SHA-256 of the first signing certificate, or "unknown" when not read.
	VersionCode string `json:"versionCode"`
	VersionName string `json:"versionName"`
	Signer      string `json:"signer"`
}

// TargetSetup is one worker-reported setup classification of a native target
// test. Classification and reason are closed enums; stage says whether the
// driver found the state before any application operation ("open") or when
// it rechecked after a failed assertion ("recheck"). Recovery, when present,
// records a user-declared non-destructive step the driver took and whether
// it restored the declared baseline.
type TargetSetup struct {
	TestID         string         `json:"testId"`
	Classification string         `json:"classification"`
	Reason         string         `json:"reason"`
	Stage          string         `json:"stage"`
	Recovery       *SetupRecovery `json:"recovery,omitempty"`
}

// TargetDiagnostic is one opt-in failure diagnostic of a native target
// operation; see Receipt.TargetDiagnostics. Windows and focus are the app's
// window count and focus class at the last mutation and after the failure;
// -1 and "" mean not observed.
type TargetDiagnostic struct {
	TestID    string `json:"testId"`
	Operation string `json:"operation"`
	// Reason is the stop reason the operation failed with.
	Reason    string `json:"reason"`
	ElapsedMS int    `json:"elapsedMs"`
	X         int    `json:"x,omitempty"`
	Y         int    `json:"y,omitempty"`
	// Point says X and Y are the requested coordinates (0,0 is a valid point).
	Point          bool   `json:"point,omitempty"`
	ViewportWidth  int    `json:"viewportWidth,omitempty"`
	ViewportHeight int    `json:"viewportHeight,omitempty"`
	WindowsBefore  int    `json:"windowsBefore"`
	WindowsAfter   int    `json:"windowsAfter"`
	FocusBefore    string `json:"focusBefore,omitempty"`
	FocusAfter     string `json:"focusAfter,omitempty"`
}

// SetupRecovery is a recorded, user-declared recovery step; see TargetSetup.
type SetupRecovery struct {
	Method   string `json:"method"`
	Presses  int    `json:"presses"`
	Restored bool   `json:"restored"`
}

// Setup classifications and the stages at which they are recorded.
const (
	SetupBlocked  = "blocked"
	SetupDirty    = "dirty"
	SetupMismatch = "mismatch"
	SetupUnknown  = "unknown"
	// SetupObserver: the accessibility snapshot disagreed with the window
	// manager after a failed assertion (an element reported displayed in a
	// window that no longer exists), so the failure is an observer
	// staleness, not an application or gesture failure.
	SetupObserver     = "observer"
	SetupStageOpen    = "open"
	SetupStageRecheck = "recheck"
)

// TestFailure is one failed test in a validated report. At most
// MaxReportedFailures are kept per attempt.
type TestFailure struct {
	// Title is the test's describe path and name, prefixed by its project in
	// brackets when the report names one.
	Title string `json:"title"`
	// Location is the failing line, or the test's declaration, relative to the
	// project directory, as file:line.
	Location string `json:"location,omitempty"`
	// Message is the start of the first error, without terminal escapes, at
	// most MaxFailureMessageBytes and redacted. A test that was expected to
	// fail and passed has no error; its message says so.
	Message string `json:"message,omitempty"`
	// ExpectedStatus and Status are the framework's expected status for the
	// test and the status of its last completed attempt. A failure with
	// expectedStatus "failed" and status "passed" is a test.fail() pin whose
	// expectation no longer holds, not a broken test.
	ExpectedStatus string `json:"expectedStatus,omitempty"`
	Status         string `json:"status,omitempty"`
	// Class says what kind of failure the framework reported, from its
	// structured status and its own error prefixes, never from message
	// prose: "assertion" (an expect failed), "timeout" (the test's time
	// budget ran out), "action-timeout" (one action or navigation timed
	// out), "environment" (the browser could not reach the target: a
	// connection, name-resolution or certificate error on navigation),
	// "unexpected-pass", "setup" (a native target driver classified the
	// device or app state as blocked, dirty, mismatched or unknown before or
	// after the assertion, so the result is inconclusive), or "other" when
	// the error carries no such mark.
	Class       string           `json:"class,omitempty"`
	Attachments []TestAttachment `json:"attachments,omitempty"`
}

// Failure classes; see TestFailure.Class.
const (
	FailureAssertion      = "assertion"
	FailureTimeout        = "timeout"
	FailureActionTimeout  = "action-timeout"
	FailureEnvironment    = "environment"
	FailureUnexpectedPass = "unexpected-pass"
	FailureSetup          = "setup"
	FailureOther          = "other"
)

// UnexpectedPass reports a test listed as failed because it passed while
// the framework expected it to fail.
func (failure TestFailure) UnexpectedPass() bool {
	return failure.Status == "passed" && failure.ExpectedStatus != "" && failure.ExpectedStatus != "passed"
}

// TestAnnotation is one annotation of one test, as the test framework
// reported it for the test's last attempt. Type and description are bounded,
// redacted and free of terminal escapes.
type TestAnnotation struct {
	Test        string `json:"test"`
	Type        string `json:"type"`
	Description string `json:"description,omitempty"`
}

const (
	MaxReportedAnnotations        = 100
	MaxAnnotationTypeBytes        = 64
	MaxAnnotationDescriptionBytes = 256
)

// TestAttachment is a file the test framework attached to a failed test,
// such as Playwright's error-context.md, a screenshot or a trace.
type TestAttachment struct {
	Name        string `json:"name"`
	ContentType string `json:"contentType,omitempty"`
	// Path is the retained copy when Retained, otherwise the framework's own
	// file, which the project's next run may delete.
	Path   string `json:"path,omitempty"`
	Bytes  int64  `json:"bytes"`
	SHA256 string `json:"sha256,omitempty"`
	// Retained means 9l copied the file into the attempt's receipt directory.
	Retained bool `json:"retained"`
}

const (
	MaxReportedFailures    = 20
	MaxFailureMessageBytes = 1 << 10
)

// FailureSummary is the bounded, metadata-only account of an attempt that did
// not pass. It is built from the receipt's own status and from validated
// structured evidence, never from message prose, so it carries no error
// messages, stack frames, test titles, page or app text, URLs, selector
// values, environment values or credentials, and it is kept without
// --failure-details. It explains a failure; it does not change the status.
type FailureSummary struct {
	// Stop is why the attempt ended without a pass, one of the Stop*
	// tokens: test_failed, goal_failed, deadline, canceled,
	// invalid_evidence, evidence_truncated, exit_without_failure,
	// provenance_changed, not_started or infrastructure.
	Stop string `json:"stop"`
	// FailedTests counts the failed tests in validated evidence (zero when
	// the evidence did not validate). Tests describes at most
	// MaxFailureSummaryTests of them, and TestsTruncated says that fewer
	// are described than failed.
	FailedTests    int                 `json:"failedTests"`
	Tests          []FailedTestSummary `json:"tests,omitempty"`
	TestsTruncated bool                `json:"testsTruncated,omitempty"`
}

// FailedTestSummary is one failed test of a FailureSummary.
type FailedTestSummary struct {
	// TestID is the engine's SHA-256 test digest, the one targetSetup,
	// targetDiagnostics and androidRuntimes carry for the same test. A
	// plain Playwright report has no such digest; see the receipt's
	// failures for its titles.
	TestID string `json:"testId,omitempty"`
	// Class is one of the TestFailure classes. With the engine it is derived
	// from the test's final status, setup records and failed step; with a
	// plain report it is the report failure's class.
	Class string `json:"class"`
	// Status is the status of the test's last completed Playwright attempt:
	// failed, timedOut, or passed for a test that was expected to fail.
	Status string `json:"status,omitempty"`
	// Step is the category of the failed step the failure is attributed to
	// (assertion, action, browser, fixture, goal or other): the first failed
	// target operation, else the first failed assertion, else the first
	// failed step of the last completed attempt. Operation is the target
	// operation of that step, and Reason the stop reason token the target
	// fixture failed it with, such as assertion_failed, deadline,
	// operation_failed or setup_blocked.
	Step      string `json:"step,omitempty"`
	Operation string `json:"operation,omitempty"`
	Reason    string `json:"reason,omitempty"`
	// Setup is the driver's setup classification of the test (blocked,
	// dirty, mismatch, observer or unknown) when one was not recovered.
	Setup string `json:"setup,omitempty"`
	// FailedSteps counts the failed steps of the last completed attempt and
	// Attempts the test's Playwright attempts in this receipt's attempt.
	FailedSteps int `json:"failedSteps,omitempty"`
	Attempts    int `json:"attempts,omitempty"`
}

// Bounds of a FailureSummary.
const (
	MaxFailureSummaryTests = 20
	MaxFailureSummaryCount = 10000
	MaxFailureTokenBytes   = 64
)

// FailureSummary stop tokens; see FailureSummary.Stop.
const (
	StopTestFailed         = "test_failed"
	StopGoalFailed         = "goal_failed"
	StopDeadline           = "deadline"
	StopCanceled           = "canceled"
	StopInvalidEvidence    = "invalid_evidence"
	StopEvidenceTruncated  = "evidence_truncated"
	StopExitWithoutFailure = "exit_without_failure"
	StopProvenanceChanged  = "provenance_changed"
	StopNotStarted         = "not_started"
	StopInfrastructure     = "infrastructure"
)

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

// Output contract versions. Additive fields keep a version; removing,
// renaming or changing the meaning of a field increments it. See
// docs/contracts.md.
const (
	PlanVersion          = 1
	ReceiptVersion       = 1
	ProgressEventVersion = 1
	RunSummaryVersion    = 1
	RunStatusVersion     = 1
)

// RunOutcome is the normative classification of a run for hosts. Complete is
// kept for compatibility: it is true only when the outcome is passed.
type RunOutcome string

const (
	// OutcomePassed: every planned job has a validated passing receipt.
	OutcomePassed RunOutcome = "passed"
	// OutcomeFailed: every planned job ran to a validated receipt and at
	// least one reported a test failure. The change broke something.
	OutcomeFailed RunOutcome = "failed"
	// OutcomeIncomplete: anything else (skipped inputs, cancellation,
	// timeouts, infrastructure errors, missing or invalid evidence). The run
	// proves neither a pass nor a failure.
	OutcomeIncomplete RunOutcome = "incomplete"
)

type RunSummary struct {
	Version    int        `json:"version"`
	Outcome    RunOutcome `json:"outcome"`
	RunID      string     `json:"runId"`
	StartedAt  time.Time  `json:"startedAt"`
	FinishedAt time.Time  `json:"finishedAt"`
	DurationMS int64      `json:"durationMs"`
	Complete   bool       `json:"complete"`
	// PlannedJobs and SkippedInputs come from the plan, so a host can see a
	// dropped spec without reading plan.json.
	PlannedJobs   int `json:"plannedJobs"`
	SkippedInputs int `json:"skippedInputs"`
	// Skipped explains each skipped input, as in the plan.
	Skipped  []Skipped `json:"skipped,omitempty"`
	Passed   int       `json:"passed"`
	Failed   int       `json:"failed"`
	Canceled int       `json:"canceled"`
	TimedOut int       `json:"timedOut"`
	Errors   int       `json:"errors"`
	Receipts []Receipt `json:"receipts"`
}

type RunStatus struct {
	Version   int            `json:"version"`
	RunID     string         `json:"runId"`
	State     string         `json:"state"`
	Plan      *Plan          `json:"plan,omitempty"`
	Result    *RunSummary    `json:"result,omitempty"`
	LastEvent *ProgressEvent `json:"lastEvent,omitempty"`
}
