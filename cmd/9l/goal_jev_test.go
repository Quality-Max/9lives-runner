package main

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Quality-Max/9lives-runner/internal/jev"
	"github.com/Quality-Max/9lives-runner/internal/jevassist"
	"github.com/Quality-Max/9lives-runner/internal/runner"
)

// observingJev plays Jev offline: it reads the observed controls and picks the
// option for the next scripted step, or wait while its control is absent.
type observingJev struct {
	mu    sync.Mutex
	steps []struct{ action, label string }
	asked int
	texts []string
}

var observedControl = regexp.MustCompile(`(?m)^(target-\d+): \S+ "([^"]*)"`)

func (o *observingJev) Decide(_ context.Context, b jev.Binding, obs jev.Observation, c jev.Contract) (jev.Result, error) {
	o.mu.Lock()
	defer o.mu.Unlock()
	o.asked++
	o.texts = append(o.texts, obs.Snapshot)
	criteria := c.Questions["next"].Criteria
	choice := "unresolved"
	if len(o.steps) > 0 {
		step := o.steps[0]
		if step.action == "complete" {
			choice = "complete"
		} else {
			choice = "wait"
			for _, m := range observedControl.FindAllStringSubmatch(obs.Snapshot, -1) {
				if m[2] != step.label {
					continue
				}
				want := strings.ToUpper(step.action[:1]) + step.action[1:] + " the observed control " + m[1] + "."
				for id, text := range criteria {
					if text == want {
						choice = id
					}
				}
			}
		}
		if choice != "wait" {
			o.steps = o.steps[1:]
		}
	}
	probabilities := map[string]float64{}
	for id := range criteria {
		probabilities[id] = 0
	}
	probabilities[choice] = 1
	confidence := .95
	// Like the real client: one request per answered call, and the model.
	return jev.Result{Record: jev.Record{Version: jev.Version, Binding: b, Status: "answered", RequestCount: 1, Model: "jev-1.14.0"}, Answers: map[string]jev.Answer{"next": {Type: "choice", Choice: choice, Confidence: &confidence, Probabilities: probabilities}}}, nil
}

func TestRunSDKGoalWithJevKeepsIndependentAssertions(t *testing.T) {
	repo, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(repo, "node_modules", "@9l", "playwright", "dist", "reporter.js")); err != nil {
		if os.Getenv("NINELIVES_REQUIRE_BROWSER") == "1" {
			t.Fatal("NINELIVES_REQUIRE_BROWSER=1 but @9l/playwright is not built")
		}
		t.Skip("npm ci and npm run build to run SDK goals")
	}
	browserProject(t) // skips when Chromium cannot launch
	t.Chdir(repo)
	t.Setenv(jev.CredentialName, "fixture-presence-only")
	for _, tc := range []struct {
		name   string
		defect bool
		code   int
	}{{"healthy order", false, 0}, {"wrong business outcome", true, 1}} {
		t.Run(tc.name, func(t *testing.T) {
			fake := &observingJev{steps: []struct{ action, label string }{{"fill", "Name"}, {"click", "Continue"}, {"click", "Confirm cart"}, {"complete", ""}}}
			var bound runner.AttemptIdentity
			previous := newGoalDecider
			newGoalDecider = func(jev.Config, time.Duration) jevassist.NewDecider {
				return func(_ context.Context, identity runner.AttemptIdentity, _, source string) (jevassist.Decider, func(), error) {
					bound = identity
					if len(source) != 64 {
						t.Errorf("not bound to the spec bytes: %q", source)
					}
					return fake, func() {}, nil
				}
			}
			t.Cleanup(func() { newGoalDecider = previous })
			args := []string{"run", "testdata/sdk/tests/goal.spec.ts", "--sdk", "--goal-provider", "jev", "--format", "json", "--receipt-dir", t.TempDir()}
			if tc.defect {
				t.Setenv("NINELIVES_SMOKE_DEFECT", "1")
				args = append(args, "--pass-env", "NINELIVES_SMOKE_DEFECT")
			}
			var out, errOut bytes.Buffer
			code := run(args, &out, &errOut)
			var summary runner.RunSummary
			if err := json.Unmarshal(out.Bytes(), &summary); err != nil || code != tc.code || len(summary.Receipts) != 1 || len(summary.Receipts[0].Goals) != 1 {
				t.Fatalf("exit %d: %s %s", code, out.String(), errOut.String())
			}
			goal := summary.Receipts[0].Goals[0]
			var actions []string
			for _, d := range goal.Decisions {
				actions = append(actions, d.Action)
			}
			if goal.Status != "completed" || goal.Provider != "jev" || !strings.Contains(strings.Join(actions, ","), "fill,click") || bound.AttemptID != summary.Receipts[0].AttemptID {
				t.Fatalf("goal %s by %s, actions %v, bound %+v", goal.Status, goal.Provider, actions, bound)
			}
			// The receipt counts exactly the Jev requests that were answered.
			requests := 0
			for _, d := range goal.Decisions {
				if d.Requests == nil || d.Model != "jev-1.14.0" {
					t.Fatalf("decision %s did not record its Jev requests and model: %+v", d.ID, d)
				}
				requests += *d.Requests
			}
			if requests != fake.asked {
				t.Fatalf("the receipt counts %d Jev requests; %d were answered", requests, fake.asked)
			}
			wantStatus := runner.StatusPassed
			if tc.defect {
				wantStatus = runner.StatusFailed // the actions completed; the assertion still decides
			}
			if summary.Receipts[0].Status != wantStatus {
				t.Fatalf("receipt %s", summary.Receipts[0].Status)
			}
			if strings.Contains(out.String(), "Fixture Person") || strings.Contains(strings.Join(fake.texts, "\n"), "Fixture Person") {
				t.Fatal("a parameter value left the browser worker")
			}
		})
	}
}

func TestRunRefusesJevGoalsWithoutCredential(t *testing.T) {
	t.Setenv(jev.CredentialName, "")
	var out, errOut bytes.Buffer
	if code := run([]string{"run", "missing.spec.ts", "--sdk", "--goal-provider", "jev"}, &out, &errOut); code != 2 || !strings.Contains(errOut.String(), jev.CredentialName) {
		t.Fatalf("exit %d: %s", code, errOut.String())
	}
}
