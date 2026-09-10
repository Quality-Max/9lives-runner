package healingbridge

import (
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestPythonCompatibilityFixtures(t *testing.T) {
	_, source, _, _ := runtime.Caller(0)
	path := filepath.Join(filepath.Dir(source), "testdata", "healing-v1.json")
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var fixtures []Fixture
	if err := json.Unmarshal(raw, &fixtures); err != nil {
		t.Fatal(err)
	}
	if len(fixtures) < 3 {
		t.Fatal("expected representative compatibility fixtures")
	}
	for _, fixture := range fixtures {
		t.Run(fixture.Name, func(t *testing.T) {
			if err := Validate(fixture.Request, fixture.Expected); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestAssertionRefusalCannotRegress(t *testing.T) {
	request := Request{Version: Version, AllowAssertionChange: false}
	response := Response{Version: Version, FailureType: "assertion_failed", Decision: "propose", ProposedCode: "expect(4).toBe(4)", RequiresApproval: true}
	if err := Validate(request, response); err == nil {
		t.Fatal("expected assertion-refusal validation failure")
	}
}
