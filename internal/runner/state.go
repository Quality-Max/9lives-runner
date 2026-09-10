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
var validRunID = regexp.MustCompile(`^run-[A-Za-z0-9][A-Za-z0-9._-]*$`)
var validComponent = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]*$`)

func validateRunID(runID string) error {
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
	event.Version, event.Sequence, event.RunID, event.Timestamp = 1, store.seq, store.runID, time.Now().UTC()
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

func (store *runStore) PersistAttempt(receipt Receipt, stdout, stderr []byte) (Receipt, error) {
	evidence, err := writeEvidence(store.root, receipt, stdout, stderr)
	if err != nil {
		return receipt, err
	}
	receipt.Evidence = evidence
	path, err := writeReceipt(store.root, receipt)
	if err != nil {
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
	status := RunStatus{RunID: runID, State: "running", Plan: &plan}
	var result RunSummary
	if err := readJSON(filepath.Join(directory, "result.json"), &result); err == nil {
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
		}
	}
	return status, nil
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
	return result, err
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
