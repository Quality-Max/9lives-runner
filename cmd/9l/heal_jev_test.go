package main

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Quality-Max/9lives-runner/internal/healing/tier2"
	"github.com/Quality-Max/9lives-runner/internal/jev"
	"github.com/Quality-Max/9lives-runner/internal/jevassist"
	"github.com/Quality-Max/9lives-runner/internal/jevcost"
	"github.com/Quality-Max/9lives-runner/internal/runner"
)

const groundedPaySpec = `import {test, expect} from '@playwright/test';
test('pay', async ({page}) => {
  await page.setContent('<h1>Your cart</h1><button>Back</button><button onclick="document.body.insertAdjacentHTML(\'beforeend\', \'<p>Payment form</p>\')">Continue to payment</button><button>Help</button>');
  await page.getByRole('button', { name: 'Checkout' }).click();
  await expect(page.getByText('Payment form')).toBeVisible();
});
`

// Tier 1 picks "Order help", the only control sharing a word with the old
// name; its run then fails on the next locator, so grounding runs after that
// run cleared Playwright's output directory.
const tier1TrapSpec = `import {test, expect} from '@playwright/test';
test('pay', async ({page}) => {
  await page.setContent('<h1>Your cart</h1><button>Order help</button><button id="go">Continue to payment</button><script>document.getElementById("go").onclick = () => { const b = document.createElement("button"); b.textContent = "Confirm"; b.onclick = () => { const p = document.createElement("p"); p.textContent = "Payment form"; document.body.append(p); }; document.body.append(b); };</script>');
  await page.getByRole('button', { name: 'Pay order' }).click();
  await page.getByRole('button', { name: 'Confirm', exact: true }).click();
  await expect(page.getByText('Payment form')).toBeVisible();
});
`

type scriptedGrounding struct {
	choice string
	calls  int
	usage  *jevcost.Tracker
}

func (s *scriptedGrounding) Decide(_ context.Context, b jev.Binding, _ jev.Observation, c jev.Contract) (jev.Result, error) {
	s.calls++
	s.usage.Request()
	s.usage.Usage(122, 8)
	one := func(v float64) *float64 { return &v }
	probabilities := map[string]float64{}
	for id := range c.Questions["target"].Criteria {
		probabilities[id] = 0
	}
	probabilities[s.choice] = 1
	return jev.Result{Record: jev.Record{Version: jev.Version, Binding: b, Status: "answered", RequestCount: 1}, Answers: map[string]jev.Answer{
		"target":    {Type: "choice", Choice: s.choice, Confidence: one(.9), Probabilities: probabilities},
		"present":   {Type: "noul", Noul: one(.95)},
		"ambiguous": {Type: "noul", Noul: one(.05)},
	}}, nil
}

func useScriptedGrounding(t *testing.T, choice string) *scriptedGrounding {
	fake := &scriptedGrounding{choice: choice}
	previous := newRepairDecider
	newRepairDecider = func(jev.Config) jevassist.NewDecider {
		return func(ctx context.Context, _ runner.AttemptIdentity, _, _ string) (jevassist.Decider, func(), error) {
			fake.usage = jevcost.FromContext(ctx)
			return fake, func() {}, nil
		}
	}
	t.Cleanup(func() { newRepairDecider = previous })
	return fake
}

func healJSON(t *testing.T, args ...string) (tier2.Session, int) {
	t.Helper()
	var out, errOut bytes.Buffer
	code := run(append([]string{"heal", "pay.spec.ts", "--provider", "none", "--jev", "--run-timeout", "40s"}, args...), &out, &errOut)
	var session tier2.Session
	if err := json.Unmarshal(out.Bytes(), &session); err != nil {
		t.Fatalf("exit %d: %s %s", code, out.String(), errOut.String())
	}
	return session, code
}

func TestHealJevGroundsThenVerifiesInIsolation(t *testing.T) {
	root := diagnosisProject(t)
	writeTestFile(t, filepath.Join(root, "pay.spec.ts"), groundedPaySpec)
	t.Setenv(jev.CredentialName, "fixture-presence-only")
	t.Run("a correct choice is verified and saved, not applied", func(t *testing.T) {
		fake := useScriptedGrounding(t, "c2")
		session, code := healJSON(t)
		decision, _ := json.Marshal(session.GroundedDecision)
		if code != 0 || session.State != "verified" || session.Applied || session.SavedPath == "" || session.Grounded == nil || !session.Grounded.Passed || fake.calls != 1 || !strings.Contains(string(decision), `"chosen":"Continue to payment"`) {
			t.Fatalf("exit %d, session %+v, decision %s", code, session, decision)
		}
		saved, _ := os.ReadFile(session.SavedPath)
		if string(saved) != strings.Replace(groundedPaySpec, "name: 'Checkout'", "name: 'Continue to payment'", 1) {
			t.Fatalf("saved candidate changed more than the name literal:\n%s", saved)
		}
		if source, _ := os.ReadFile(filepath.Join(root, "pay.spec.ts")); string(source) != groundedPaySpec {
			t.Fatal("source changed without approval")
		}
		os.Remove(session.SavedPath)
	})
	t.Run("a wrong choice fails its own assertion and is not saved", func(t *testing.T) {
		useScriptedGrounding(t, "c1") // Back: clickable, but no payment form follows
		session, code := healJSON(t)
		if code != 1 || session.State != "needs_human" || session.SavedPath != "" || session.Applied || session.Grounded == nil || session.Grounded.Passed {
			t.Fatalf("exit %d, session %+v", code, session)
		}
		if usage := session.JevUsage; usage == nil || usage.Totals != (jevcost.Totals{Requests: 1, RequestsWithUsage: 1, InputTokens: 122, OutputTokens: 8}) {
			t.Fatalf("unsuccessful healing usage = %+v", usage)
		}
	})
	t.Run("grounding keeps the original evidence after a failed Tier 1 run", func(t *testing.T) {
		writeTestFile(t, filepath.Join(root, "pay.spec.ts"), tier1TrapSpec)
		t.Cleanup(func() { writeTestFile(t, filepath.Join(root, "pay.spec.ts"), groundedPaySpec) })
		fake := useScriptedGrounding(t, "c2")
		session, code := healJSON(t)
		decision, _ := json.Marshal(session.GroundedDecision)
		if session.Tier1 == nil || session.Tier1.Passed {
			t.Fatalf("Tier 1 did not run and fail first: %+v", session.Tier1)
		}
		if code != 0 || session.State != "verified" || fake.calls != 1 || !strings.Contains(string(decision), `"chosen":"Continue to payment"`) {
			t.Fatalf("exit %d, session %+v, decision %s", code, session, decision)
		}
		os.Remove(session.SavedPath)
	})
	t.Run("approval applies a verified choice", func(t *testing.T) {
		useScriptedGrounding(t, "c2")
		session, code := healJSON(t, "--yes")
		if code != 0 || session.State != "applied" || !session.Applied {
			t.Fatalf("exit %d, session %+v", code, session)
		}
		if source, _ := os.ReadFile(filepath.Join(root, "pay.spec.ts")); !strings.Contains(string(source), "name: 'Continue to payment'") {
			t.Fatal("approved candidate not applied")
		}
	})
}

func TestHealJevKeepsTheCredentialOutOfTestProcesses(t *testing.T) {
	dir := t.TempDir()
	spec := filepath.Join(dir, "a.spec.ts")
	writeTestFile(t, spec, "// never runs\n")
	t.Setenv(jev.CredentialName, "fixture-presence-only")
	for _, names := range []string{jev.CredentialName, "BASE_URL," + strings.ToLower(jev.CredentialName)} {
		receipts := filepath.Join(dir, "receipts")
		var out, errOut bytes.Buffer
		code := run([]string{"heal", spec, "--provider", "none", "--jev", "--pass-env", names, "--receipt-dir", receipts}, &out, &errOut)
		if code != 2 || out.Len() != 0 || !strings.Contains(errOut.String(), "--pass-env") {
			t.Fatalf("--pass-env %s: exit %d, stderr %q", names, code, errOut.String())
		}
		if _, err := os.Stat(receipts); !os.IsNotExist(err) {
			t.Fatalf("--pass-env %s ran the spec", names)
		}
	}
}

func TestHealJevRefusesWithoutCredentialOrWithUnsafeOptions(t *testing.T) {
	spec := filepath.Join(t.TempDir(), "a.spec.ts")
	writeTestFile(t, spec, "// never runs\n")
	t.Setenv(jev.CredentialName, "")
	for _, args := range [][]string{{"--jev"}, {"--jev-images", "synthetic"}, {"--jev", "--jev-images", "masked"}} {
		var out, errOut bytes.Buffer
		if code := run(append([]string{"heal", spec, "--provider", "none"}, args...), &out, &errOut); code != 2 || out.Len() != 0 {
			t.Fatalf("%v: exit %d", args, code)
		}
	}
}
