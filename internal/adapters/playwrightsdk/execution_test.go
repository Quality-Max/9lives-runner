package playwrightsdk

import (
	"bytes"
	"context"
	"os"
	"strings"
	"testing"

	"github.com/Quality-Max/9lives-runner/internal/runner"
)

type protocolExecutor struct {
	t                              *testing.T
	foreign, truncated, stdoutOnly bool
}

func (executor protocolExecutor) Run(_ context.Context, job runner.Job, _ int) runner.ProcessOutput {
	if job.Env["NINELIVES_RUN_ID"] != "run-bound" || job.Env["NINELIVES_JOB_ID"] != "job-001" || job.Env["NINELIVES_ATTEMPT_ID"] != "job-001-attempt-001" {
		executor.t.Fatal("caller environment replaced core attempt identity")
	}
	frames := passingFrames()
	for _, frame := range frames {
		frame["runId"] = job.Env["NINELIVES_RUN_ID"]
		frame["jobId"] = job.Env["NINELIVES_JOB_ID"]
		frame["attemptId"] = job.Env["NINELIVES_ATTEMPT_ID"]
	}
	if executor.foreign {
		frames[2]["attemptId"] = "another-attempt"
	}
	evidence := stream(frames)
	if executor.truncated {
		evidence = append(evidence, bytes.Repeat([]byte("x"), 4096)...) // beyond the 4 KiB capture limit
	}
	// The protocol belongs on the private channel; stdout is ordinary output.
	if !executor.stdoutOnly && os.WriteFile(job.Env[New().EvidenceEnv()], evidence, 0600) != nil {
		executor.t.Fatal("evidence channel unavailable")
	}
	return runner.ProcessOutput{Executed: true, ExitCode: 0, Stdout: stream(frames), Stderr: []byte("unstructured private diagnostic fixture")}
}

func TestCoreBindsEvidenceAndRejectsTruncatedOrForeignOutput(t *testing.T) {
	for _, mode := range []string{"valid", "foreign", "truncated", "stdout-only"} {
		t.Run(mode, func(t *testing.T) {
			plan := runner.Plan{Version: 1, RunID: "run-bound", Jobs: []runner.Job{{
				ID: "job-001", Spec: "fixture.spec.ts", Adapter: New().Name(), Command: []string{"unused-by-test-executor"},
				Env: map[string]string{"NINELIVES_RUN_ID": "spoofed-run", "NINELIVES_JOB_ID": "spoofed-job", "NINELIVES_ATTEMPT_ID": "spoofed-attempt"},
			}}}
			summary, err := runner.Execute(context.Background(), plan, runner.ExecuteOptions{
				ReceiptDir: t.TempDir(), Adapters: []runner.Adapter{New()}, MaxOutputBytes: 4096,
				Executor: protocolExecutor{t: t, foreign: mode == "foreign", truncated: mode == "truncated", stdoutOnly: mode == "stdout-only"},
			})
			if err != nil {
				t.Fatal(err)
			}
			if len(summary.Receipts) != 1 {
				t.Fatal("missing terminal receipt")
			}
			receipt := summary.Receipts[0]
			if mode == "valid" {
				if !summary.Complete || summary.Passed != 1 || !receipt.Validated {
					t.Fatalf("valid evidence rejected: %+v", receipt)
				}
			} else if summary.Complete || summary.Passed != 0 || receipt.Validated || receipt.Status != runner.StatusError {
				t.Fatalf("untrusted evidence made a successful receipt: %+v", receipt)
			}
			if receipt.RunID != "run-bound" || receipt.AttemptID != "job-001-attempt-001" {
				t.Fatal("evidence mutated receipt owner")
			}
			if receipt.Evidence.StderrPath != "" {
				t.Fatal("arbitrary diagnostics were persisted")
			}
			if mode != "valid" && receipt.Evidence.StdoutPath != "" {
				t.Fatal("unvalidated worker payload was persisted")
			}
			if mode == "valid" {
				raw, err := os.ReadFile(receipt.Evidence.StdoutPath)
				if err != nil || !strings.Contains(string(raw), Version) {
					t.Fatal("validated evidence was not persisted")
				}
			}
		})
	}
}
