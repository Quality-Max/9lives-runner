package main

import (
	"bytes"
	"strings"
	"testing"

	"github.com/Quality-Max/9lives-runner/internal/assessment"
	"github.com/Quality-Max/9lives-runner/internal/runner"
)

// The text output says what a spec-scoped status compared, so "matched" is
// not read as unchanged dependencies; an unknown status compared nothing.
func TestAssessmentTextNamesProvenanceScope(t *testing.T) {
	expected := runner.AgentProvenance{Version: 1, Agent: "fixture-agent", Repository: "r", Branch: "b", Commit: "c", Source: "s", SourcePath: "p"}
	var out bytes.Buffer
	writeAssessmentText(&out, assessment.Report{AgentProvenance: runner.CompareAgentProvenance(expected, expected)}, false)
	if !strings.Contains(out.String(), "Agent branch/source provenance: matched (the selected spec, its path, branch, commit and workspace root; imported modules are not compared)\n") {
		t.Fatalf("matched: %s", out.String())
	}
	out.Reset()
	writeAssessmentText(&out, assessment.Report{AgentProvenance: runner.UnknownAgentProvenance()}, false)
	if !strings.Contains(out.String(), "Agent branch/source provenance: unknown\n") {
		t.Fatalf("unknown: %s", out.String())
	}
}
