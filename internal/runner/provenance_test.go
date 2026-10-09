package runner

import (
	"context"
	"encoding/json"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func provenanceGit(t *testing.T, dir string, args ...string) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	command := exec.Command("git", append([]string{"-C", dir}, args...)...)
	emptyConfig := filepath.Join(dir, ".fixture-git-config")
	if err := os.WriteFile(emptyConfig, nil, 0600); err != nil {
		t.Fatal(err)
	}
	command.Env = []string{"PATH=" + os.Getenv("PATH"), "GIT_CONFIG_NOSYSTEM=1", "GIT_CONFIG_GLOBAL=" + emptyConfig}
	command.Stdout, command.Stderr = io.Discard, io.Discard
	if err := RunOwnedCommand(ctx, command); err != nil {
		t.Fatalf("isolated fixture Git operation failed: %v", err)
	}
}

func provenanceFixture(t *testing.T) (string, AgentProvenance) {
	t.Helper()
	dir := t.TempDir()
	provenanceGit(t, dir, "init", "-b", "fixture-branch")
	spec := filepath.Join(dir, "test.fake")
	mustWrite(t, spec, "original source", 0o600)
	provenanceGit(t, dir, "add", "test.fake")
	provenanceGit(t, dir, "-c", "user.name=Fixture", "-c", "user.email=fixture@example.invalid", "-c", "commit.gpgsign=false", "commit", "-m", "Fixture")
	p, err := CaptureAgentProvenance(context.Background(), spec, "fixture-agent")
	if err != nil {
		t.Fatal(err)
	}
	return spec, p
}

type provenanceExecutor struct {
	calls  int
	mutate func()
}

func (e *provenanceExecutor) Run(context.Context, Job, int) ProcessOutput {
	e.calls++
	if e.mutate != nil {
		e.mutate()
	}
	return ProcessOutput{Executed: true, ExitCode: 0, Stdout: []byte(`{"outcome":"passed","assertions":1,"artifact":true}`)}
}

func TestAgentProvenanceRejectsDriftBeforeExecutionAndDoesNotRetry(t *testing.T) {
	for _, change := range []string{"branch", "commit", "source", "path", "repository", "unavailable"} {
		t.Run(change, func(t *testing.T) {
			spec, p := provenanceFixture(t)
			switch change {
			case "branch":
				provenanceGit(t, filepath.Dir(spec), "checkout", "-b", "different-branch")
			case "commit":
				provenanceGit(t, filepath.Dir(spec), "-c", "user.name=Fixture", "-c", "user.email=fixture@example.invalid", "-c", "commit.gpgsign=false", "commit", "--allow-empty", "-m", "New commit")
			case "source":
				mustWrite(t, spec, "changed source", 0o600)
			case "path":
				spec = filepath.Join(filepath.Dir(spec), "other.fake")
				mustWrite(t, spec, "original source", 0o600)
			case "repository":
				spec, _ = provenanceFixture(t)
			case "unavailable":
				if err := os.Remove(spec); err != nil {
					t.Fatal(err)
				}
			}
			job := fakeJob("job-001", "success", 0)
			job.Spec = spec
			e := &provenanceExecutor{}
			summary, err := Execute(context.Background(), Plan{Version: 1, RunID: "run-provenance", Jobs: []Job{job}}, ExecuteOptions{AgentProvenance: &p, Executor: e, Adapters: []Adapter{fakeAdapter{}}, MaxAttempts: 3, ReceiptDir: t.TempDir()})
			if err != nil || summary.Complete || e.calls != 0 || len(summary.Receipts) != 1 || summary.Receipts[0].Status != StatusError || summary.Receipts[0].Validated {
				t.Fatal("provenance mismatch executed, retried or passed")
			}
		})
	}
}

func TestAgentProvenanceInvalidatesPassingWorkerAfterDrift(t *testing.T) {
	for _, change := range []string{"none", "source", "branch", "unavailable"} {
		t.Run(change, func(t *testing.T) {
			spec, p := provenanceFixture(t)
			job := fakeJob("job-001", "success", 0)
			job.Spec = spec
			e := &provenanceExecutor{mutate: func() {
				switch change {
				case "source":
					mustWrite(t, spec, "changed source", 0o600)
				case "branch":
					provenanceGit(t, filepath.Dir(spec), "checkout", "-b", "different-branch")
				case "unavailable":
					if err := os.Remove(spec); err != nil {
						t.Fatal(err)
					}
				}
			}}
			summary, err := Execute(context.Background(), Plan{Version: 1, RunID: "run-provenance", Jobs: []Job{job}}, ExecuteOptions{AgentProvenance: &p, Executor: e, Adapters: []Adapter{fakeAdapter{}}, MaxAttempts: 3, ReceiptDir: t.TempDir()})
			if err != nil || e.calls != 1 || len(summary.Receipts) != 1 {
				t.Fatal("unexpected execution or retry")
			}
			r := summary.Receipts[0]
			if change == "none" {
				if !summary.Complete || r.Status != StatusPassed || !r.Validated || r.AgentProvenance.Status != "matched" || r.AgentProvenance.After == nil {
					t.Fatal("matched execution refused")
				}
			} else if summary.Complete || r.Status != StatusError || r.Validated || r.AgentProvenance.Status == "matched" {
				t.Fatal("passing report survived provenance drift")
			}
			if change == "unavailable" && r.AgentProvenance.Source != "unknown" {
				t.Fatal("unavailable post-run source claimed matched")
			}
		})
	}
}

func TestAgentProvenanceRejectsMalformedRecordsAndDetachedBranch(t *testing.T) {
	spec, p := provenanceFixture(t)
	raw, _ := json.Marshal(p)
	for _, invalid := range [][]byte{
		[]byte(strings.Replace(string(raw), `"version":1`, `"version":1,"version":1`, 1)),
		append(append([]byte{}, raw...), []byte(` {}`)...),
		[]byte(strings.Replace(string(raw), `"agent":"fixture-agent"`, `"agent":null`, 1)),
		[]byte(strings.Replace(string(raw), `"version":1`, `"version":2`, 1)),
	} {
		if _, err := ParseAgentProvenance(invalid); err == nil {
			t.Fatal("malformed record accepted")
		}
	}
	provenanceGit(t, filepath.Dir(spec), "checkout", "--detach")
	if _, err := CaptureAgentProvenance(context.Background(), spec, "fixture-agent"); err == nil {
		t.Fatal("detached branch accepted")
	}
}
