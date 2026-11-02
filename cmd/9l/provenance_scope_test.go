package main

import (
	"bytes"
	"strings"
	"testing"

	"github.com/Quality-Max/9lives-runner/internal/assessment"
	"github.com/Quality-Max/9lives-runner/internal/runner"
)

// The text output says what a compared status covered, so "matched" is not
// read as unchanged packages or, for a version 1 snapshot, unchanged imports;
// changed dependencies are named; an unknown status compared nothing.
func TestAssessmentTextNamesProvenanceScope(t *testing.T) {
	expected := runner.AgentProvenance{Version: 1, Agent: "fixture-agent", Repository: "r", Branch: "b", Commit: "c", Source: "s", SourcePath: "p"}
	var out bytes.Buffer
	writeAssessmentText(&out, assessment.Report{AgentProvenance: runner.CompareAgentProvenance(expected, expected)}, false)
	if !strings.Contains(out.String(), "Agent branch/source provenance: matched (the selected spec, its path, branch, commit and workspace root; imported modules are not compared; recapture the snapshot to compare them)\n") {
		t.Fatalf("version 1 matched: %s", out.String())
	}

	helper := runner.ProvenanceDependency{Path: "e2e/support.mjs", Kind: runner.DependencyImport, SHA256: strings.Repeat("a", 64)}
	v2 := expected
	v2.Version, v2.Dependencies = runner.AgentProvenanceVersion, []runner.ProvenanceDependency{helper}
	edited := v2
	helper.SHA256 = strings.Repeat("b", 64)
	edited.Dependencies = []runner.ProvenanceDependency{helper}
	out.Reset()
	writeAssessmentText(&out, assessment.Report{AgentProvenance: runner.CompareAgentProvenance(v2, edited)}, false)
	if !strings.Contains(out.String(), "Agent branch/source provenance: mismatch (the selected spec, its path, its relative imports, the nearest lockfile, branch, commit and workspace root; package and path-alias imports are not followed)\nAgent provenance import modified: \"e2e/support.mjs\"\n") {
		t.Fatalf("version 2 mismatch: %s", out.String())
	}

	out.Reset()
	writeAssessmentText(&out, assessment.Report{AgentProvenance: runner.UnknownAgentProvenance()}, false)
	if !strings.Contains(out.String(), "Agent branch/source provenance: unknown\n") {
		t.Fatalf("unknown: %s", out.String())
	}
}
