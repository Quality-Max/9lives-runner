package playwrightsdk

import "testing"

func TestTargetOperationsAreTypedAndClosed(t *testing.T) {
	identity := owner
	identity.Target = "android-fake:finance"
	for _, operation := range append(append([]string{}, targetActions...), targetAssertions...) {
		f := targetFrames()
		for _, frame := range f {
			frame["targetId"] = TargetID(identity.Target)
		}
		assertion := oneOf(operation, targetAssertions...)
		f[3]["operation"] = operation
		if assertion {
			f[3]["category"] = "assertion"
		} else {
			f[3]["category"] = "action"
			// An action alone cannot pass a target test.
			f = append(f[:4], append([]map[string]any{{"type": "step_end", "testId": testKey, "retry": 0, "stepId": "step-2", "category": "assertion", "status": "passed", "operation": "assert-hidden", "targetId": TargetID(identity.Target)}}, f[4:]...)...)
		}
		if _, err := New().ValidateAttempt(stream(f), identity); err != nil {
			t.Fatalf("%s rejected: %v", operation, err)
		}
		f[3]["category"] = map[bool]string{true: "action", false: "assertion"}[assertion]
		if _, err := New().ValidateAttempt(stream(f), identity); err == nil {
			t.Fatalf("%s accepted with the wrong category", operation)
		}
	}
	for _, operation := range []string{"swipe", "assert-screenshot", "unlock", "clear-data"} {
		f := targetFrames()
		for _, frame := range f {
			frame["targetId"] = TargetID(identity.Target)
		}
		f[3]["operation"] = operation
		if _, err := New().ValidateAttempt(stream(f), identity); err == nil {
			t.Fatalf("unknown operation %s accepted", operation)
		}
	}
}
