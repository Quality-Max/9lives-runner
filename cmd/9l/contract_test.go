package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/Quality-Max/9lives-runner/internal/assessment"
	"github.com/Quality-Max/9lives-runner/internal/contracttest"
	"github.com/Quality-Max/9lives-runner/internal/runner"
)

var contractEnums = map[reflect.Type][]string{
	reflect.TypeFor[runner.ReceiptStatus](): {"passed", "failed", "canceled", "timed_out", "error"},
	reflect.TypeFor[runner.RunOutcome]():    {"passed", "failed", "incomplete"},
}

var publishedContracts = []struct {
	file, title, description string
	root                     reflect.Type
	version                  int
}{
	{"version", "9l version --format json", "Implementation, CLI version and the version of every machine-readable output.", reflect.TypeFor[VersionInfo](), VersionInfoVersion},
	{"plan", "9l plan --format json", "Resolved jobs and explained skipped inputs, before any process starts.", reflect.TypeFor[runner.Plan](), runner.PlanVersion},
	{"run-result", "9l run / 9l result --format json", "Terminal run result. outcome is the normative classification; the run exit code is derived from it.", reflect.TypeFor[runner.RunSummary](), runner.RunSummaryVersion},
	{"run-status", "9l status --format json", "Run state from persisted plan, result and events.", reflect.TypeFor[runner.RunStatus](), runner.RunStatusVersion},
	{"receipt", "receipt.json", "One attempt's execution receipt, also embedded in the run result.", reflect.TypeFor[runner.Receipt](), runner.ReceiptVersion},
	{"progress-event", "events.jsonl line", "One persisted progress event.", reflect.TypeFor[runner.ProgressEvent](), runner.ProgressEventVersion},
	{"assess", "9l assess <one spec> --format json", "Advisory single-file source assessment. Static findings do not establish executed assertion coverage.", reflect.TypeFor[assessment.Report](), assessment.ReportVersion},
	{"assess-suite", "9l assess <several specs|dir|glob> --format json", "Advisory combined assessment of several files.", reflect.TypeFor[assessment.SuiteReport](), assessment.SuiteVersion},
}

// TestPublishedContractSchemasMatchOutputTypes regenerates every schema under
// docs/contracts from the Go types. Set NINELIVES_UPDATE_CONTRACTS=1 to
// rewrite them after an intended change; bump the version first if the change
// is not additive.
func TestPublishedContractSchemasMatchOutputTypes(t *testing.T) {
	directory := filepath.Join(contracttest.RunnerRoot(), "docs", "contracts")
	for _, contract := range publishedContracts {
		t.Run(contract.file, func(t *testing.T) {
			want, err := contracttest.Schema(contract.root, contracttest.SchemaOptions{
				ID:    "https://github.com/Quality-Max/9lives-runner/blob/main/docs/contracts/" + contract.file + ".schema.json",
				Title: contract.title, Description: contract.description, Version: contract.version, Enums: contractEnums,
			})
			if err != nil {
				t.Fatal(err)
			}
			path := filepath.Join(directory, contract.file+".schema.json")
			if os.Getenv("NINELIVES_UPDATE_CONTRACTS") == "1" {
				if err := os.WriteFile(path, want, 0o644); err != nil {
					t.Fatal(err)
				}
				return
			}
			got, err := os.ReadFile(path)
			if err != nil || !bytes.Equal(got, want) {
				t.Fatalf("%s is out of date with %s (err=%v); rerun with NINELIVES_UPDATE_CONTRACTS=1", path, contract.root, err)
			}
		})
	}
}

func TestVersionJSONIdentifiesGoRunnerAndContracts(t *testing.T) {
	var stdout, stderr bytes.Buffer
	if code := run([]string{"version", "--format", "json"}, &stdout, &stderr); code != 0 {
		t.Fatalf("code=%d stderr=%s", code, stderr.String())
	}
	var info VersionInfo
	decoder := json.NewDecoder(&stdout)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&info); err != nil {
		t.Fatal(err)
	}
	if info.Version != VersionInfoVersion || info.Implementation != "go-runner" || info.CLIVersion != version ||
		info.Contracts.RunResult != runner.RunSummaryVersion || info.Contracts.Engine != "9l.engine/1" || info.Contracts.ExecutionReceipt != "execution-receipt/1.0" {
		t.Fatalf("unexpected version info: %+v", info)
	}
	for _, args := range [][]string{{"--version"}, {"version"}, {"version", "--format", "text"}} {
		stdout.Reset()
		if code := run(args, &stdout, &stderr); code != 0 || stdout.String() != "9l "+version+" (Go runner)\n" {
			t.Fatalf("%v: code=%d out=%q", args, code, stdout.String())
		}
	}
	if code := run([]string{"version", "--format", "yaml"}, &stdout, &stderr); code != exitUsage {
		t.Fatalf("invalid format code=%d", code)
	}
}

func TestRunExitCodeSeparatesFailureFromIncomplete(t *testing.T) {
	passed := runner.RunSummary{Outcome: runner.OutcomePassed, Complete: true}
	failed := runner.RunSummary{Outcome: runner.OutcomeFailed}
	incomplete := runner.RunSummary{Outcome: runner.OutcomeIncomplete}
	for _, test := range []struct {
		name            string
		result          runner.RunSummary
		executionFailed bool
		want            int
	}{
		{"passed", passed, false, exitPassed},
		{"failed", failed, false, exitFailed},
		{"incomplete", incomplete, false, exitIncomplete},
		{"passed but interrupted", passed, true, exitIncomplete},
		{"failed but interrupted", failed, true, exitIncomplete},
		{"passed outcome without completeness", runner.RunSummary{Outcome: runner.OutcomePassed}, false, exitIncomplete},
		{"unknown outcome", runner.RunSummary{Outcome: "unknown"}, false, exitIncomplete},
	} {
		if got := runExitCode(test.result, test.executionFailed); got != test.want {
			t.Errorf("%s: exit %d, want %d", test.name, got, test.want)
		}
	}
}
