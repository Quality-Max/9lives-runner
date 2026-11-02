package playwrightsdk

import (
	"strings"
	"testing"

	"github.com/Quality-Max/9lives-runner/internal/runner"
)

// setupFrames is a failed native target test whose driver classified the
// device as locked before any application operation.
func setupFrames(target string, setup map[string]any) []map[string]any {
	f := targetFrames()
	for _, frame := range f {
		frame["targetId"] = TargetID(target)
	}
	// hello, required_test, test_begin, [target_setup], step_end, test_end, test_result, end
	f[3]["status"] = "failed"
	f[4]["status"], f[4]["expectedStatus"] = "failed", "passed"
	f[5]["outcome"] = "unexpected"
	f[6]["status"] = "failed"
	s := map[string]any{"type": "target_setup", "testId": testKey, "retry": 0, "targetId": TargetID(target), "setup": setup}
	return append(f[:3], append([]map[string]any{s}, f[3:]...)...)
}

func blocked() map[string]any {
	return map[string]any{"classification": "blocked", "reason": "device-locked", "stage": "open"}
}

func TestSetupClassificationIsBoundAndKeepsTheTestFailed(t *testing.T) {
	for _, target := range []string{"android:finance", "android-fake:finance"} {
		identity := owner
		identity.Target = target
		v, err := New().ValidateAttempt(stream(setupFrames(target, blocked())), identity)
		if err != nil || len(v.TargetSetup) != 1 || v.TargetSetup[0].TestID != testKey || v.TargetSetup[0].Classification != runner.SetupBlocked || v.TargetSetup[0].Reason != "device-locked" || v.FailureCount != 1 {
			t.Fatalf("%s: setup classification not bound: %+v %v", target, v, err)
		}
		if !strings.Contains(v.Description, "1 test(s) carry a driver setup classification") {
			t.Fatalf("%s: description omits the classification: %s", target, v.Description)
		}
	}
	identity := owner
	identity.Target = "web:finance"
	if _, err := New().ValidateAttempt(stream(setupFrames("web:finance", blocked())), identity); err == nil {
		t.Fatal("web target accepted a native setup classification")
	}
}

func TestSetupClassificationRejectsPassingTestsUnlessRecovered(t *testing.T) {
	identity := owner
	identity.Target = "android-fake:finance"
	passing := func(setup map[string]any) []map[string]any {
		f := setupFrames("android-fake:finance", setup)
		f[4]["status"], f[5]["status"], f[6]["outcome"], f[7]["status"] = "passed", "passed", "expected", "passed"
		return f
	}
	if _, err := New().ValidateAttempt(stream(passing(blocked())), identity); err == nil || !strings.Contains(err.Error(), "unrecovered setup classification") {
		t.Fatalf("a locked device cannot pass: %v", err)
	}
	dirty := map[string]any{"classification": "dirty", "reason": "extra-window", "stage": "open", "recovery": map[string]any{"method": "back", "presses": 2, "restored": true}}
	v, err := New().ValidateAttempt(stream(passing(dirty)), identity)
	if err != nil || v.FailureCount != 0 || len(v.TargetSetup) != 1 || v.TargetSetup[0].Recovery == nil || !v.TargetSetup[0].Recovery.Restored || v.TargetSetup[0].Recovery.Presses != 2 {
		t.Fatalf("recovered baseline must let the test pass with its record: %+v %v", v, err)
	}
	unrestored := map[string]any{"classification": "dirty", "reason": "extra-window", "stage": "open", "recovery": map[string]any{"method": "back", "presses": 3, "restored": false}}
	if _, err := New().ValidateAttempt(stream(passing(unrestored)), identity); err == nil {
		t.Fatal("an unrestored recovery cannot pass")
	}
	if v, err := New().ValidateAttempt(stream(setupFrames("android-fake:finance", unrestored)), identity); err != nil || v.FailureCount != 1 {
		t.Fatalf("an unrestored recovery keeps a failed test failed: %+v %v", v, err)
	}
}

func TestSetupClassificationIsClosed(t *testing.T) {
	identity := owner
	identity.Target = "android-fake:finance"
	cases := map[string]map[string]any{
		"unknown classification": {"classification": "flaky", "reason": "device-locked", "stage": "open"},
		"reason outside class":   {"classification": "dirty", "reason": "device-locked", "stage": "open"},
		"mismatch has no reason": {"classification": "mismatch", "reason": "device-locked", "stage": "open"},
		"unknown stage":          {"classification": "blocked", "reason": "device-locked", "stage": "teardown"},
		"content":                {"classification": "blocked", "reason": "PIN 1234 shown on lock screen", "stage": "open"},
		"extra field":            {"classification": "blocked", "reason": "device-locked", "stage": "open", "screen": "content"},
		"bound test id":          {"classification": "blocked", "reason": "device-locked", "stage": "open", "testId": testKey},
		"recovery for blocked":   {"classification": "blocked", "reason": "device-locked", "stage": "open", "recovery": map[string]any{"method": "back", "presses": 1, "restored": true}},
		"recovery on recheck":    {"classification": "dirty", "reason": "extra-window", "stage": "recheck", "recovery": map[string]any{"method": "back", "presses": 1, "restored": true}},
		"unknown recovery":       {"classification": "dirty", "reason": "extra-window", "stage": "open", "recovery": map[string]any{"method": "clear-data", "presses": 1, "restored": true}},
		"unbounded presses":      {"classification": "dirty", "reason": "extra-window", "stage": "open", "recovery": map[string]any{"method": "back", "presses": 17, "restored": true}},
		"open recovery":          {"classification": "dirty", "reason": "extra-window", "stage": "open", "recovery": map[string]any{"method": "back", "presses": 1, "restored": true, "note": "x"}},
	}
	for name, setup := range cases {
		t.Run(name, func(t *testing.T) {
			if _, err := New().ValidateAttempt(stream(setupFrames("android-fake:finance", setup)), identity); err == nil {
				t.Fatal("invalid setup classification accepted")
			}
		})
	}
	for name, change := range map[string]func([]map[string]any) []map[string]any{
		"duplicate": func(f []map[string]any) []map[string]any {
			return append(f[:4], append([]map[string]any{f[3]}, f[4:]...)...)
		},
		"before begin": func(f []map[string]any) []map[string]any { f[2], f[3] = f[3], f[2]; return f },
		"after end":    func(f []map[string]any) []map[string]any { f[3], f[4], f[5] = f[4], f[5], f[3]; return f },
		"foreign test": func(f []map[string]any) []map[string]any { f[3]["testId"] = strings.Repeat("c", 64); return f },
		"wrong retry":  func(f []map[string]any) []map[string]any { f[3]["retry"] = 1; return f },
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := New().ValidateAttempt(stream(change(setupFrames("android-fake:finance", blocked()))), identity); err == nil {
				t.Fatal("misplaced setup classification accepted")
			}
		})
	}
}

func TestSetupRecordsPerStage(t *testing.T) {
	identity := owner
	identity.Target = "android-fake:finance"
	recovered := map[string]any{"classification": "dirty", "reason": "extra-window", "stage": "open", "recovery": map[string]any{"method": "back", "presses": 1, "restored": true}}
	rechecked := map[string]any{"classification": "blocked", "reason": "device-locked", "stage": "recheck"}
	with := func(first, second map[string]any) []map[string]any {
		f := setupFrames("android-fake:finance", first)
		s := map[string]any{"type": "target_setup", "testId": testKey, "retry": 0, "targetId": TargetID("android-fake:finance"), "setup": second}
		return append(f[:4], append([]map[string]any{s}, f[4:]...)...)
	}
	v, err := New().ValidateAttempt(stream(with(recovered, rechecked)), identity)
	if err != nil || len(v.TargetSetup) != 2 || v.TargetSetup[0].Stage != runner.SetupStageOpen || v.TargetSetup[1].Stage != runner.SetupStageRecheck {
		t.Fatalf("recovered baseline then lock not kept: %+v %v", v, err)
	}
	for name, frames := range map[string][]map[string]any{
		"two rechecks":       with(rechecked, rechecked),
		"two open records":   with(recovered, recovered),
		"open after recheck": with(rechecked, recovered),
	} {
		if _, err := New().ValidateAttempt(stream(frames), identity); err == nil {
			t.Fatalf("%s accepted", name)
		}
	}
	passing := with(recovered, rechecked)
	passing[5]["status"], passing[6]["status"], passing[7]["outcome"], passing[8]["status"] = "passed", "passed", "expected", "passed"
	if _, err := New().ValidateAttempt(stream(passing), identity); err == nil {
		t.Fatal("a recheck record cannot accompany a passed test")
	}
}
