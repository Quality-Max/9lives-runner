package runner

import (
	"context"
	"errors"
	"regexp"
	"slices"
	"strings"
)

// VerificationResult is what the engine established about one independent
// postcondition check.
type VerificationResult string

const (
	// VerificationVerified: the engine observed the declared postcondition.
	VerificationVerified VerificationResult = "verified"
	// VerificationContradicted: the application answered and the answer is
	// not the declared postcondition.
	VerificationContradicted VerificationResult = "contradicted"
	// VerificationUnreachable: the engine could not obtain an answer to
	// compare, so the check says nothing about the application.
	VerificationUnreachable VerificationResult = "unreachable"
	// VerificationNotRun: the engine made no observation for this check.
	VerificationNotRun VerificationResult = "not_run"
)

// Verification reasons: closed tokens saying why a check has its result.
const (
	// Contradicted.
	VerificationReasonStatus      = "status"
	VerificationReasonJSONValue   = "json_value"
	VerificationReasonJSONPointer = "json_pointer_missing"
	VerificationReasonNotJSON     = "not_json"
	// Unreachable.
	VerificationReasonConnection = "connection"
	VerificationReasonTimeout    = "timeout"
	VerificationReasonTLS        = "tls"
	VerificationReasonBodyLimit  = "body_too_large"
	// Not run.
	VerificationReasonNotValidated = "attempt_not_validated"
	VerificationReasonFileChanged  = "checks_file_changed"
	VerificationReasonInterrupted  = "interrupted"
	VerificationReasonBudget       = "time_budget"
)

// VerificationObserver is the only value of Verification.ObservedBy: the
// record was produced by the engine's own probe, never reported by a worker.
const VerificationObserver = "engine"

// MaxVerifications bounds the checks of one attempt.
const MaxVerifications = 16

// Verification is the engine's record of one independent postcondition
// check, declared in a checks file outside the spec and probed by the engine
// after the attempt's process tree stopped. It is a closed record: the
// check's identifiers, its method, the result, a reason token and the HTTP
// status the engine observed. It never holds a URL, a header, an expected
// value or any part of a response body.
type Verification struct {
	ID string `json:"id"`
	// OutcomeID is the expectedOutcome identifier of a requirements contract
	// the checks file linked this check to. It is recorded as declared; the
	// engine does not resolve it against a contract.
	OutcomeID string             `json:"outcomeId,omitempty"`
	Method    string             `json:"method"`
	Result    VerificationResult `json:"result"`
	Reason    string             `json:"reason,omitempty"`
	// HTTPStatus is the status of the last answer the engine received, and
	// zero when it received none.
	HTTPStatus int    `json:"httpStatus,omitempty"`
	ObservedBy string `json:"observedBy"`
}

var (
	verificationID      = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9_.-]{0,79}$`)
	verificationReasons = map[VerificationResult][]string{
		VerificationVerified:     {""},
		VerificationContradicted: {VerificationReasonStatus, VerificationReasonJSONValue, VerificationReasonJSONPointer, VerificationReasonNotJSON},
		VerificationUnreachable:  {VerificationReasonConnection, VerificationReasonTimeout, VerificationReasonTLS, VerificationReasonBodyLimit},
		VerificationNotRun:       {VerificationReasonNotValidated, VerificationReasonFileChanged, VerificationReasonInterrupted, VerificationReasonBudget},
	}
)

// ValidVerificationID reports whether id is a check or outcome identifier:
// the character rules of requirement identifiers.
func ValidVerificationID(id string) bool { return verificationID.MatchString(id) }

// Validate checks one record against its closed form.
func (record Verification) Validate() error {
	reasons, known := verificationReasons[record.Result]
	valid := known && slices.Contains(reasons, record.Reason) &&
		verificationID.MatchString(record.ID) && (record.OutcomeID == "" || verificationID.MatchString(record.OutcomeID)) &&
		(record.Method == "GET" || record.Method == "HEAD") && record.ObservedBy == VerificationObserver &&
		(record.HTTPStatus == 0 || (record.HTTPStatus >= 100 && record.HTTPStatus <= 599)) &&
		// An answer was compared for a verified or contradicted check, and
		// none was requested for one that did not run.
		((record.Result != VerificationVerified && record.Result != VerificationContradicted) || record.HTTPStatus != 0) &&
		(record.Result != VerificationNotRun || record.HTTPStatus == 0)
	if !valid {
		return errors.New("verification list has a malformed entry")
	}
	return nil
}

// ValidVerifications reports whether the receipt's verification list is the
// closed record the runner writes: bounded, with distinct identifiers. A
// receipt without a list is valid.
func (receipt Receipt) ValidVerifications() bool {
	if len(receipt.Verifications) > MaxVerifications {
		return false
	}
	seen := map[string]bool{}
	for _, record := range receipt.Verifications {
		if record.Validate() != nil || seen[record.ID] {
			return false
		}
		seen[record.ID] = true
	}
	return true
}

// VerificationContradicted reports whether the engine demonstrated a
// mismatch: a well-formed list with at least one contradicted check.
func (receipt Receipt) VerificationContradicted() bool {
	return receipt.ValidVerifications() && slices.ContainsFunc(receipt.Verifications, func(record Verification) bool {
		return record.Result == VerificationContradicted
	})
}

// verificationsHold reports whether nothing in the receipt's verification
// list stands against a pass: no list, or a well-formed list in which every
// check was verified.
func verificationsHold(receipt Receipt) bool {
	return receipt.ValidVerifications() && !slices.ContainsFunc(receipt.Verifications, func(record Verification) bool {
		return record.Result != VerificationVerified
	})
}

// contradictedPass reports whether the receipt failed only because the
// engine contradicted an attempt whose own evidence validated without a
// failed test or goal: a demonstrated mismatch, not missing evidence.
func contradictedPass(receipt Receipt) bool {
	return receipt.Status == StatusFailed && receipt.Validated && receipt.Executed && receipt.ExitCode == 0 &&
		receipt.FailureCount == 0 && !receipt.GoalFailed && receipt.VerificationContradicted()
}

// PostconditionVerifier performs independent postcondition checks for one
// attempt. The engine calls it only after the attempt's process tree has
// stopped, and passes nothing about it to the worker.
type PostconditionVerifier interface {
	// Planned lists one not-run record per check, in checks-file order. Its
	// identifiers, outcome identifiers and methods are the authority for the
	// records an attempt may carry.
	Planned() []Verification
	// SourceSHA256 is the digest of the checks file as it was read before
	// any worker started.
	SourceSHA256() string
	// Unchanged re-reads the checks file and reports whether it still has
	// that digest. An unreadable file has changed.
	Unchanged() bool
	// Verify probes every check, within its own time bounds and ctx, and
	// returns one record per planned check in the same order.
	Verify(ctx context.Context) []Verification
}

// plannedVerifications returns the verifier's planned records marked not run
// for reason, or an error when they are not a closed, bounded, distinct list.
func plannedVerifications(verifier PostconditionVerifier, reason string) ([]Verification, error) {
	planned := slices.Clone(verifier.Planned())
	for index := range planned {
		planned[index].Result, planned[index].Reason, planned[index].HTTPStatus, planned[index].ObservedBy = VerificationNotRun, reason, 0, VerificationObserver
	}
	if len(planned) == 0 || !(Receipt{Verifications: planned}).ValidVerifications() {
		return nil, errors.New("independent verification needs 1 to 16 well-formed checks with distinct identifiers")
	}
	return planned, nil
}

// applyVerification runs the independent checks of an attempt whose worker
// has stopped and folds their results into the receipt. Checks run only for
// an attempt whose own evidence validated and whose checks file is unchanged.
// A worker-reported pass never survives a check that was not verified: a
// contradicted check fails the attempt, and a check the engine could not
// complete leaves it an error, which the run reports as incomplete. An
// attempt that already failed, was canceled or timed out keeps its status.
func applyVerification(ctx context.Context, receipt Receipt, verifier PostconditionVerifier) Receipt {
	receipt.VerificationSHA256 = verifier.SourceSHA256()
	reason := ""
	switch {
	case !receipt.Validated:
		reason = VerificationReasonNotValidated
	case !verifier.Unchanged():
		reason = VerificationReasonFileChanged
	}
	// Execute already refused a verifier without a well-formed plan.
	records, _ := plannedVerifications(verifier, VerificationReasonInterrupted)
	if reason != "" {
		records, _ = plannedVerifications(verifier, reason)
	} else if observed := verifier.Verify(ctx); sameChecks(records, observed) {
		records = observed
	}
	receipt.Verifications = records
	var contradicted, unproven []string
	for _, record := range records {
		switch record.Result {
		case VerificationVerified:
			receipt.VerifiedAssertions++
		case VerificationContradicted:
			contradicted = append(contradicted, record.ID)
		default:
			unproven = append(unproven, record.ID)
		}
	}
	switch {
	case reason == VerificationReasonNotValidated:
		// The attempt already says why it did not validate.
	case reason == VerificationReasonFileChanged:
		if receipt.Status == StatusPassed {
			receipt.Status = StatusError
		}
		receipt.Error = joinError(receipt.Error, "the verification checks file changed during the attempt; its checks were not run")
	case len(contradicted) > 0:
		if receipt.Status == StatusPassed {
			receipt.Status = StatusFailed
		}
		receipt.Error = joinError(receipt.Error, "independent verification contradicted the result: "+strings.Join(contradicted, ", "))
	case len(unproven) > 0:
		if receipt.Status == StatusPassed {
			receipt.Status = StatusError
		}
		receipt.Error = joinError(receipt.Error, "independent verification could not be completed: "+strings.Join(unproven, ", "))
	}
	return receipt
}

// sameChecks reports whether observed is a well-formed result for exactly
// the planned checks, in order. Anything else is discarded, so a verifier
// cannot add, drop or rename a check after the fact.
func sameChecks(planned, observed []Verification) bool {
	if len(observed) != len(planned) || !(Receipt{Verifications: observed}).ValidVerifications() {
		return false
	}
	for index, record := range observed {
		if record.ID != planned[index].ID || record.OutcomeID != planned[index].OutcomeID || record.Method != planned[index].Method {
			return false
		}
	}
	return true
}

// verificationDecided reports whether the engine probed the application or
// found the checks file changed in this attempt. Such an attempt is never
// retried: the worker's own result is established, and running it again
// would change the state the checks observe.
func verificationDecided(receipt Receipt) bool {
	return slices.ContainsFunc(receipt.Verifications, func(record Verification) bool {
		return record.Result != VerificationNotRun || record.Reason != VerificationReasonNotValidated
	})
}
