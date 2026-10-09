package runner

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sync"
	"time"
)

var ErrRunNotFound = errors.New("run not found")

// execution-receipt/1.0 identifiers are capped at 160 characters. Rejecting
// rather than truncating preserves identity binding across local and portable receipts.
const canonicalIdentifierLimit = 160

// A live runner rewrites its heartbeat every heartbeatInterval, including
// while it terminates process groups after a deadline or cancellation. The
// stale threshold leaves room for a loaded host (many browsers) to delay a
// beat without reporting a live run as interrupted.
const (
	heartbeatInterval   = time.Second
	heartbeatStaleAfter = 10 * heartbeatInterval
)

// heartbeatStore is optional: a Store that cannot prove liveness is simply
// judged by its last progress event.
type heartbeatStore interface {
	Heartbeat() error
}

var validRunID = regexp.MustCompile(`^run-[A-Za-z0-9][A-Za-z0-9._-]*$`)
var validComponent = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]*$`)

func validateRunID(runID string) error {
	if len(runID) > canonicalIdentifierLimit {
		return fmt.Errorf("run ID exceeds canonical identifier limit (%d): %q", canonicalIdentifierLimit, runID)
	}
	if !validRunID.MatchString(runID) {
		return fmt.Errorf("invalid run ID %q", runID)
	}
	return nil
}

type runStore struct {
	root  string
	runID string
	mu    sync.Mutex
	seq   int64
}

func newRunStore(root, runID string) *runStore { return &runStore{root: root, runID: runID} }

func (store *runStore) directory() string { return filepath.Join(store.root, store.runID) }

func (store *runStore) Initialize(plan Plan) error {
	if err := os.MkdirAll(store.root, 0o700); err != nil {
		return err
	}
	if err := os.Mkdir(store.directory(), 0o700); err != nil {
		if os.IsExist(err) {
			return fmt.Errorf("run %s already exists", store.runID)
		}
		return err
	}
	return writeJSONAtomic(store.directory(), "plan.json", plan)
}

func (store *runStore) AppendEvent(event ProgressEvent) error {
	store.mu.Lock()
	defer store.mu.Unlock()
	store.seq++
	event.Version, event.Sequence, event.RunID, event.Timestamp = ProgressEventVersion, store.seq, store.runID, time.Now().UTC()
	raw, err := json.Marshal(event)
	if err != nil {
		return err
	}
	path := filepath.Join(store.directory(), "events.jsonl")
	file, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	if _, err = file.Write(append(raw, '\n')); err != nil {
		_ = file.Close()
		return err
	}
	return file.Close()
}

// Heartbeat overwrites a single liveness file instead of appending events, so
// a long run does not grow events.jsonl by one line per second.
func (store *runStore) Heartbeat() error {
	_, _, err := writeAtomic(store.directory(), "heartbeat", []byte(time.Now().UTC().Format(time.RFC3339Nano)+"\n"))
	return err
}

// PersistAttempt writes the canonical export before the legacy receipt.
// receipt.json is the commit point for an attempt: if it cannot be written the
// canonical export is removed, so the two files never disagree on disk.
func (store *runStore) PersistAttempt(receipt Receipt, stdout, stderr []byte) (Receipt, error) {
	evidence, err := writeEvidence(store.root, receipt, stdout, stderr)
	if err != nil {
		return receipt, err
	}
	receipt.Evidence = evidence
	canonical, err := writeCanonicalReceipt(store.root, receipt)
	if err != nil {
		return receipt, err
	}
	path, err := writeReceipt(store.root, receipt)
	if err != nil {
		_ = os.Remove(canonical)
		return receipt, err
	}
	receipt.ReceiptPath = path
	return receipt, nil
}

func (store *runStore) Finalize(summary RunSummary) error {
	return writeJSONAtomic(store.directory(), "result.json", summary)
}

func (store *runStore) CancellationRequested() bool {
	_, err := os.Stat(filepath.Join(store.directory(), "cancel"))
	return err == nil
}

func RequestCancel(root, runID string) error {
	if err := validateRunID(runID); err != nil {
		return err
	}
	directory := filepath.Join(root, runID)
	if _, err := os.Stat(filepath.Join(directory, "plan.json")); err != nil {
		if os.IsNotExist(err) {
			return ErrRunNotFound
		}
		return err
	}
	_, _, err := writeAtomic(directory, "cancel", []byte(time.Now().UTC().Format(time.RFC3339Nano)+"\n"))
	return err
}

func LoadStatus(root, runID string) (RunStatus, error) {
	if err := validateRunID(runID); err != nil {
		return RunStatus{}, err
	}
	directory := filepath.Join(root, runID)
	var plan Plan
	if err := readJSON(filepath.Join(directory, "plan.json"), &plan); err != nil {
		if os.IsNotExist(err) {
			return RunStatus{}, ErrRunNotFound
		}
		return RunStatus{}, err
	}
	status := RunStatus{Version: RunStatusVersion, RunID: runID, State: "running", Plan: &plan}
	var result RunSummary
	if err := readJSON(filepath.Join(directory, "result.json"), &result); err == nil {
		upgradeResult(&result, plan)
		status.Result = &result
		if result.Complete {
			status.State = "completed"
		} else {
			status.State = "finished"
		}
	} else if !os.IsNotExist(err) {
		return RunStatus{}, err
	}
	events, err := readEvents(filepath.Join(directory, "events.jsonl"))
	if err != nil && !os.IsNotExist(err) {
		return RunStatus{}, err
	}
	if len(events) > 0 {
		status.LastEvent = &events[len(events)-1]
	}
	if status.Result == nil {
		if _, err := os.Stat(filepath.Join(directory, "cancel")); err == nil {
			status.State = "canceling"
		} else if time.Since(lastSignOfLife(directory, status.LastEvent)) > heartbeatStaleAfter {
			// There is no recovery protocol for a local process. A stale
			// heartbeat is therefore reported honestly instead of "running" forever.
			status.State = "interrupted"
		}
	}
	return status, nil
}

func lastSignOfLife(directory string, lastEvent *ProgressEvent) time.Time {
	var latest time.Time
	if lastEvent != nil {
		latest = lastEvent.Timestamp
	}
	// plan.json covers the instant between Initialize and the first beat.
	for _, name := range []string{"heartbeat", "plan.json"} {
		if info, err := os.Stat(filepath.Join(directory, name)); err == nil && info.ModTime().After(latest) {
			latest = info.ModTime()
		}
	}
	return latest
}

func LoadResult(root, runID string) (RunSummary, error) {
	if err := validateRunID(runID); err != nil {
		return RunSummary{}, err
	}
	var result RunSummary
	err := readJSON(filepath.Join(root, runID, "result.json"), &result)
	if os.IsNotExist(err) {
		return RunSummary{}, ErrRunNotFound
	}
	if err == nil && result.Version == 0 {
		var plan Plan
		if err := readJSON(filepath.Join(root, runID, "plan.json"), &plan); err != nil {
			return RunSummary{}, err
		}
		upgradeResult(&result, plan)
	}
	return result, err
}

// upgradeResult fills the version 1 fields of a result written by CLI 0.1.1
// or earlier. Those runs did not record interruption, so a run is classified
// from its plan and receipts alone; interrupted jobs still have canceled or
// timed-out receipts.
func upgradeResult(result *RunSummary, plan Plan) {
	if result.Version != 0 {
		return
	}
	result.Version, result.PlannedJobs, result.SkippedInputs = RunSummaryVersion, len(plan.Jobs), len(plan.Skipped)
	result.Outcome = classifyRun(*result, false)
}

func readEvents(path string) ([]ProgressEvent, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	events := []ProgressEvent{}
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		var event ProgressEvent
		if err := json.Unmarshal(scanner.Bytes(), &event); err != nil {
			return nil, fmt.Errorf("decode event: %w", err)
		}
		events = append(events, event)
	}
	return events, scanner.Err()
}

func readJSON(path string, target any) error {
	raw, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	return json.Unmarshal(raw, target)
}

func writeJSONAtomic(directory, name string, value any) error {
	raw, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return err
	}
	_, _, err = writeAtomic(directory, name, append(raw, '\n'))
	return err
}
