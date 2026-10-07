package assessment

import (
	"context"
	"io"
	"strings"
	"testing"
)

const contractJSON = `{"version":1,"requirements":[{"id":"checkout-order","reference":"requirements/checkout","revision":"1","expectedOutcomes":[{"id":"order-count","description":"Exactly one order"},{"id":"order-items","description":"Selected items"}]},{"id":"confirmation","reference":"requirements/confirmation","revision":"1","expectedOutcomes":[{"id":"confirmation","description":"Confirmation is displayed"}]}]}`

func TestRequirementChangesBannerAssessment(t *testing.T) {
	c, err := ParseContract([]byte(contractJSON))
	if err != nil {
		t.Fatal(err)
	}
	fact := Fact{Location: Location{1, 1}, Requirements: []string{"checkout-order"}, Assertions: []Assertion{{Location: Location{2, 1}, Outcomes: []string{"confirmation"}}}}
	report := Build([]byte("source"), []byte(contractJSON), c, Facts{Compiler: "5.9.3", Tests: []Fact{fact}})
	if report.Tests[0].Dimensions["intentAlignment"] != "concern" || len(report.Tests[0].Findings) != 2 {
		t.Fatalf("missing requirement gaps: %+v", report.Tests[0])
	}
	for _, f := range report.Tests[0].Findings {
		if f.Classification != "suspected" {
			t.Fatal("static gap claimed as proof")
		}
	}
	fact.Requirements = []string{"confirmation"}
	report = Build([]byte("source"), []byte(contractJSON), c, Facts{Compiler: "5.9.3", Tests: []Fact{fact}})
	if len(report.Tests[0].Findings) != 0 || report.Tests[0].Dimensions["purpose"] != "supported" || report.Tests[0].Dimensions["assertionAdequacy"] != "unknown" || report.Tests[0].Dimensions["intentAlignment"] != "unknown" {
		t.Fatal("declared mapping promoted to proof")
	}
	if report.Execution != "not_run" || report.Tests[0].Dimensions["runtimeEvidence"] != "unknown" {
		t.Fatal("invented execution")
	}
}

func TestAnalyzerOutputLimitCannotBeBypassedByCopy(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	output := &boundedOutput{cancel: cancel}
	if _, err := io.Copy(output, strings.NewReader(strings.Repeat("x", MaxSource+1))); err == nil || !output.overflow || ctx.Err() == nil || len(output.Bytes()) > MaxSource {
		t.Fatal("helper output escaped its bound or cancellation")
	}
}

func TestMissingIntentAndSourceProvenance(t *testing.T) {
	c, _ := ParseContract([]byte(contractJSON))
	a := Build([]byte("a"), []byte(contractJSON), c, Facts{Tests: []Fact{{Location: Location{1, 1}}}})
	b := Build([]byte("b"), []byte(contractJSON), c, Facts{})
	if a.SourceSHA256 == b.SourceSHA256 || a.Tests[0].Dimensions["purpose"] != "unknown" || a.Tests[0].Findings[0].Classification != "suspected" {
		t.Fatal("missing intent or provenance lost")
	}
}

func TestInvalidContracts(t *testing.T) {
	for _, raw := range []string{
		strings.Replace(contractJSON, `"version":1`, `"version":1,"version":1`, 1),
		contractJSON + ` {}`, strings.Replace(contractJSON, `"version":1`, `"version":2`, 1),
		strings.Replace(contractJSON, `"revision":"1"`, `"revision":null`, 1),
		strings.Replace(contractJSON, `"order-items"`, `"order-count"`, 1),
		strings.Replace(contractJSON, `"id":"confirmation","description"`, `"id":"order-count","description"`, 1),
		strings.Replace(contractJSON, `"reference":`, `"extra":`, 1),
		`{"version":1,"requirements":[]}`,
	} {
		if _, err := ParseContract([]byte(raw)); err == nil {
			t.Fatal("accepted malformed contract")
		}
	}
}

func TestDisabledTestIsAnEngineeringConcernWithoutRuntimeProof(t *testing.T) {
	c, _ := ParseContract([]byte(contractJSON))
	disabled := true
	fact := Fact{Location: Location{1, 1}, Requirements: []string{"confirmation"}, Disabled: &disabled, Assertions: []Assertion{{Location: Location{2, 1}, Outcomes: []string{"confirmation"}}}}
	report := Build([]byte("source"), []byte(contractJSON), c, Facts{Tests: []Fact{fact}})
	test := report.Tests[0]
	if len(test.Findings) != 1 || test.Findings[0].Rule != "disabled-test" || test.Findings[0].Classification != "demonstrated" || test.Dimensions["engineeringQuality"] != "concern" {
		t.Fatalf("disabled test lost: %+v", test)
	}
	if report.Execution != "not_run" || test.Dimensions["runtimeEvidence"] != "unknown" || test.Dimensions["intentAlignment"] != "unknown" {
		t.Fatal("disabled syntax invented behavioral proof")
	}
}

func TestRejectForeignAndIncompleteFacts(t *testing.T) {
	f := Facts{Version: 1, Compiler: "5.9.3", Tests: []Fact{{Location: Location{9, 1}, Requirements: []string{}, Assertions: []Assertion{}, Sleeps: []Location{}, Disabled: new(bool)}}}
	if validFacts(f, []byte("x")) {
		t.Fatal("foreign location accepted")
	}
	f.Tests[0].Location = Location{1, 1}
	if !validFacts(f, []byte("x")) {
		t.Fatal("valid facts rejected")
	}
	f.Tests[0].Disabled = nil
	if validFacts(f, []byte("x")) {
		t.Fatal("missing disabled state accepted")
	}
	f.Tests[0].Disabled = new(bool)
	f.Tests = append(f.Tests, f.Tests[0])
	if validFacts(f, []byte("x")) {
		t.Fatal("duplicate test accepted")
	}
	if validFacts(Facts{Version: 1, Compiler: "5.9.3"}, []byte("x")) {
		t.Fatal("missing inventory accepted")
	}
}
