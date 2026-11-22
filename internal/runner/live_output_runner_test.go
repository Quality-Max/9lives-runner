package runner

import (
	"context"
	"encoding/json"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

// runChannel runs the channel fake, which prints a dotenv line on ordinary
// stdout and writes its evidence to the private channel.
func runChannel(t *testing.T, receipts string, live io.Writer) Receipt {
	t.Helper()
	plan := Plan{Version: 1, RunID: "run-live", Jobs: []Job{fakeJob("job-001", "channel", 0)}}
	summary, err := Execute(context.Background(), plan, ExecuteOptions{Workers: 1, Timeout: 20 * time.Second, ReceiptDir: receipts, Adapters: []Adapter{channelAdapter{}}, LiveOutput: live})
	if err != nil || len(summary.Receipts) != 1 {
		t.Fatalf("run: %v %+v", err, summary)
	}
	return summary.Receipts[0]
}

// comparableReceipt reads receipt.json and drops what differs between any two
// runs: timings, and the receipt directory inside paths.
func comparableReceipt(t *testing.T, receipt Receipt, receipts string) map[string]any {
	t.Helper()
	raw, err := os.ReadFile(receipt.ReceiptPath)
	if err != nil {
		t.Fatal(err)
	}
	// Paths inside JSON are escaped: on Windows every backslash is doubled.
	escaped, _ := json.Marshal(receipts)
	raw = []byte(strings.ReplaceAll(string(raw), strings.Trim(string(escaped), `"`), "RECEIPTS"))
	var fields map[string]any
	if err := json.Unmarshal(raw, &fields); err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"startedAt", "finishedAt", "durationMs"} {
		if _, ok := fields[key]; !ok {
			t.Fatalf("receipt has no %s; update the comparison", key)
		}
		delete(fields, key)
	}
	return fields
}

func TestLiveOutputShowsWorkerOutputWithoutPersistingIt(t *testing.T) {
	plainDir, liveDir := t.TempDir(), t.TempDir()
	plain := runChannel(t, plainDir, nil)
	var live recordingWriter
	shown := runChannel(t, liveDir, &live)

	if !strings.Contains(live.text(), "[job-001 stdout] [dotenv@17] injecting env (0) from .env\n") {
		t.Fatalf("worker stdout did not reach live output: %q", live.text())
	}
	for _, receipt := range []Receipt{plain, shown} {
		if receipt.Status != StatusPassed || !receipt.Validated {
			t.Fatalf("receipt %s validated=%v: %s", receipt.Status, receipt.Validated, receipt.Error)
		}
	}
	if a, b := comparableReceipt(t, plain, plainDir), comparableReceipt(t, shown, liveDir); !reflect.DeepEqual(a, b) {
		t.Fatalf("receipts differ with live output:\nwithout: %v\nwith:    %v", a, b)
	}
	// Nothing the worker printed is stored anywhere in the run directory:
	// not stdout.log, stderr.log, receipt.json, events.jsonl or result.json.
	seen := map[string]bool{}
	err := filepath.WalkDir(liveDir, func(path string, entry fs.DirEntry, err error) error {
		if err != nil || entry.IsDir() {
			return err
		}
		raw, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		seen[entry.Name()] = true
		if strings.Contains(string(raw), "dotenv") {
			t.Errorf("%s persisted worker output", path)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"stdout.log", "receipt.json", "events.jsonl", "result.json"} {
		if !seen[name] {
			t.Errorf("%s was not written, so it was not checked", name)
		}
	}
}

type alwaysFailing struct{}

func (alwaysFailing) Write([]byte) (int, error) { return 0, io.ErrClosedPipe }

type foreverBlocking struct{ release chan struct{} }

func (w foreverBlocking) Write(p []byte) (int, error) {
	<-w.release
	return len(p), nil
}

func TestLiveOutputWriterCannotFailOrHoldAnAttempt(t *testing.T) {
	release := make(chan struct{})
	defer close(release)
	for name, writer := range map[string]io.Writer{"failing": alwaysFailing{}, "blocking": foreverBlocking{release}} {
		t.Run(name, func(t *testing.T) {
			started := time.Now()
			receipt := runChannel(t, t.TempDir(), writer)
			if receipt.Status != StatusPassed || !receipt.Validated || receipt.ExitCode != 0 {
				t.Fatalf("a live writer changed the attempt: %s %s", receipt.Status, receipt.Error)
			}
			if elapsed := time.Since(started); elapsed > 15*time.Second {
				t.Fatalf("attempt took %s", elapsed)
			}
		})
	}
}
