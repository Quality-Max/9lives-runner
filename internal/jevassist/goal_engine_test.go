//go:build !windows

package jevassist

import (
	"bytes"
	"context"
	"encoding/json"
	"net"
	"net/http"
	"strings"
	"testing"

	"github.com/Quality-Max/9lives-runner/internal/goals"
	"github.com/Quality-Max/9lives-runner/internal/runner"
)

// engineSession drives the real goal engine over its owned socket, as the
// browser worker does, with Jev bound to the attempt.
type engineSession struct {
	t        *testing.T
	client   *http.Client
	identity runner.AttemptIdentity
	service  runner.AttemptService
	closed   int
}

func startEngine(t *testing.T, fake *scriptedGoalJev, limits goals.Limits) *engineSession {
	t.Helper()
	identity := runner.AttemptIdentity{RunID: "run-1", JobID: "job-001", AttemptID: "job-001-attempt-001"}
	e := &engineSession{t: t, identity: identity}
	provider := &GoalProvider{SourceSHA256: strings.Repeat("b", 64), New: func(_ context.Context, bound runner.AttemptIdentity, _, _ string) (Decider, func(), error) {
		if bound.AttemptID != identity.AttemptID || bound.Target != "web" {
			t.Errorf("bound to %+v", bound)
		}
		return fake, func() { e.closed++ }, nil
	}}
	service, err := goals.Factory{Provider: provider, Limits: limits}.Start(context.Background(), identity)
	if err != nil {
		t.Fatal(err)
	}
	e.service = service
	socket := service.Environment()["NINELIVES_GOAL_SOCKET"]
	e.client = &http.Client{Transport: &http.Transport{DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
		return (&net.Dialer{}).DialContext(ctx, "unix", socket)
	}}}
	return e
}

func (e *engineSession) call(body map[string]any) map[string]any {
	e.t.Helper()
	body["version"], body["runId"], body["jobId"], body["attemptId"] = goals.Version, e.identity.RunID, e.identity.JobID, e.identity.AttemptID
	raw, _ := json.Marshal(body)
	response, err := e.client.Post("http://goal/goal", "application/json", bytes.NewReader(raw))
	if err != nil {
		e.t.Fatal(err)
	}
	defer response.Body.Close()
	var out map[string]any
	if err := json.NewDecoder(response.Body).Decode(&out); err != nil {
		e.t.Fatal(err)
	}
	return out
}

func (e *engineSession) start(parameters ...string) string {
	body := map[string]any{"op": "start", "instruction": "Sign up with the email, then continue"}
	if len(parameters) > 0 {
		body["parameters"] = parameters
	}
	r := e.call(body)
	goal, ok := r["goalId"].(string)
	if !ok {
		e.t.Fatalf("start refused: %v", r)
	}
	return goal
}

func (e *engineSession) decide(goal string, controls []goals.Candidate) map[string]any {
	return e.call(map[string]any{"op": "decide", "goalId": goal, "candidates": controls, "state": []string{"Create your account"}})
}

func (e *engineSession) ack(goal string, r map[string]any, outcome string) map[string]any {
	return e.call(map[string]any{"op": "ack", "goalId": goal, "decisionId": r["decisionId"], "outcome": outcome})
}

func TestJevGoalsRunInsideTheEnginesProtections(t *testing.T) {
	t.Run("multi-step navigation ends completed, never verified", func(t *testing.T) {
		fake := &scriptedGoalJev{choices: map[string][]string{"next": {"o2", "o1", "complete"}}, confident: .9}
		e := startEngine(t, fake, goals.Defaults())
		g := e.start("email")
		fill := e.decide(g, signup[:2])
		if d, _ := fill["decision"].(map[string]any); d["action"] != "fill" || d["targetId"] != "target-2" || d["parameter"] != "email" {
			t.Fatalf("fill: %v", fill)
		}
		e.ack(g, fill, "done")
		next := []goals.Candidate{{ID: "target-1", Role: "button", Label: "Continue", Actions: []string{"click"}}}
		click := e.decide(g, next)
		if d, _ := click["decision"].(map[string]any); d["action"] != "click" || d["targetId"] != "target-1" {
			t.Fatalf("click: %v", click)
		}
		e.ack(g, click, "done")
		if done := e.decide(g, next); done["status"] != "completed" {
			t.Fatalf("complete: %v", done)
		}
		receipts := e.service.Close()
		if len(receipts) != 1 || receipts[0].Status != "completed" || receipts[0].Provider != "jev" || len(receipts[0].Decisions) != 3 || e.closed != 1 {
			t.Fatalf("receipts %+v, released %d", receipts, e.closed)
		}
		if strings.Contains(fake.seen[0], "@") {
			t.Fatal("a parameter value reached Jev")
		}
	})
	for _, tc := range []struct {
		name     string
		choices  []string
		controls []goals.Candidate
		limits   func(*goals.Limits)
		status   string
	}{
		{"forbidden label is blocked by the engine", []string{"o1"}, []goals.Candidate{{ID: "target-1", Role: "button", Label: "Pay now", Actions: []string{"click"}}}, nil, "policy_blocked"},
		{"indistinguishable duplicates stop", []string{"o1"}, []goals.Candidate{{ID: "target-1", Role: "button", Label: "Next", Actions: []string{"click"}}, {ID: "target-2", Role: "button", Label: "Next", Actions: []string{"click"}}}, nil, "ambiguous_target"},
		{"no viable candidate is unresolved", []string{"unresolved"}, signup[:1], nil, "unresolved"},
		{"budget exhaustion stops before a call", []string{"o1"}, signup[:1], func(l *goals.Limits) { l.MaxDecisions = 1 }, "budget_exhausted"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			fake := &scriptedGoalJev{choices: map[string][]string{"next": append(tc.choices, tc.choices...)}, confident: .9}
			limits := goals.Defaults()
			if tc.limits != nil {
				tc.limits(&limits)
			}
			e := startEngine(t, fake, limits)
			g := e.start()
			r := e.decide(g, tc.controls)
			if tc.status == "budget_exhausted" {
				e.ack(g, r, "done")
				r = e.decide(g, tc.controls)
			}
			if r["status"] != tc.status || r["decision"] != nil {
				t.Fatalf("got %v", r)
			}
			e.service.Close()
		})
	}
	t.Run("unknown effect is never retried", func(t *testing.T) {
		fake := &scriptedGoalJev{choices: map[string][]string{"next": {"o1", "o1"}}, confident: .9}
		e := startEngine(t, fake, goals.Defaults())
		g := e.start()
		r := e.decide(g, signup[:1])
		if e.ack(g, r, "unknown")["status"] != "unknown_effect" || e.decide(g, signup[:1])["status"] != "unknown_effect" || len(fake.asked) != 1 {
			t.Fatal("an unknown effect was followed by another decision")
		}
		e.service.Close()
	})
	t.Run("provider outage stops the goal", func(t *testing.T) {
		fake := &scriptedGoalJev{choices: map[string][]string{"next": {"o1"}}, confident: .9, err: context.DeadlineExceeded}
		e := startEngine(t, fake, goals.Defaults())
		g := e.start()
		if r := e.decide(g, signup[:1]); r["status"] != "provider_error" {
			t.Fatalf("got %v", r)
		}
		e.service.Close()
	})
}
