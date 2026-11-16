package runner

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"time"

	"github.com/Quality-Max/9lives-runner/internal/strictjson"
)

// AgentProvenance records a local snapshot, not authenticated agent authorship.
// Repository is a hash of the repository root, never a remote URL or config.
// Version 2 adds Dependencies: the spec's transitive local imports and the
// nearest lockfile, as repo-relative paths with content digests (never
// contents), and DependenciesSHA256 over the sorted entries. An empty list is
// omitted; the digest is always present in version 2.
type AgentProvenance struct {
	Version            int                    `json:"version"`
	Agent              string                 `json:"agent"`
	Repository         string                 `json:"repositorySHA256"`
	Branch             string                 `json:"branch"`
	Commit             string                 `json:"commit"`
	Source             string                 `json:"sourceSHA256"`
	SourcePath         string                 `json:"sourcePathSHA256"`
	Dependencies       []ProvenanceDependency `json:"dependencies,omitempty"`
	DependenciesSHA256 string                 `json:"dependenciesSHA256,omitempty"`
}

// ProvenanceDependency is one local module the spec imports (kind "import")
// or the nearest lockfile (kind "lockfile").
type ProvenanceDependency struct {
	Path   string `json:"path"`
	Kind   string `json:"kind"`
	SHA256 string `json:"sha256"`
}

// ProvenanceChange names one dependency that differs from the creation
// snapshot: "modified", "added" or "removed".
type ProvenanceChange struct {
	Path   string `json:"path"`
	Kind   string `json:"kind"`
	Change string `json:"change"`
}

// Provenance scopes: what a compared assessment covered.
const (
	ProvenanceScopeSpec    = "spec"
	ProvenanceScopeImports = "spec-and-local-imports"
)

// AgentProvenanceVersion is the snapshot version that capture produces.
// Version 1 snapshots are still accepted and compared with scope "spec".
const AgentProvenanceVersion = 2

// AgentProvenanceLimit bounds a serialized snapshot.
const AgentProvenanceLimit = 256 << 10

type ProvenanceAssessment struct {
	Status string `json:"status"`
	// Scope names what a matched status covers. "spec" (a version 1
	// snapshot) means the selected spec's bytes, its relative path, the
	// branch, commit and workspace root; imported modules are not compared.
	// "spec-and-local-imports" (version 2) adds the spec's transitive
	// relative imports and the nearest lockfile. Package and path-alias
	// imports and installed packages are not compared in either scope.
	Scope      string `json:"scope,omitempty"`
	Branch     string `json:"branch"`
	Commit     string `json:"commit"`
	Source     string `json:"source"`
	Repository string `json:"repository"`
	// Dependencies is "matched", "mismatch" or "unknown" for a version 2
	// snapshot and absent when they were not compared.
	Dependencies      string             `json:"dependencies,omitempty"`
	DependencyChanges []ProvenanceChange `json:"dependencyChanges,omitempty"`
	Expected          *AgentProvenance   `json:"expected,omitempty"`
	Before            *AgentProvenance   `json:"before,omitempty"`
	After             *AgentProvenance   `json:"after,omitempty"`
}

func provenanceHash(raw []byte) string { sum := sha256.Sum256(raw); return hex.EncodeToString(sum[:]) }

var provenanceID = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9_.-]{0,79}$`)
var provenanceSHA = regexp.MustCompile(`^[a-f0-9]{64}$`)
var provenanceCommit = regexp.MustCompile(`^(?:[a-f0-9]{40}|[a-f0-9]{64})$`)
var provenanceBranch = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9_./-]{0,254}$`)

func ParseAgentProvenance(raw []byte) (AgentProvenance, error) {
	var p AgentProvenance
	d := json.NewDecoder(bytes.NewReader(raw))
	if len(raw) > AgentProvenanceLimit || strictjson.Value(d) != nil {
		return p, errors.New("invalid agent provenance")
	}
	if _, err := d.Token(); err != io.EOF {
		return p, errors.New("invalid agent provenance")
	}
	d = json.NewDecoder(bytes.NewReader(raw))
	d.DisallowUnknownFields()
	if d.Decode(&p) != nil || (p.Version != 1 && p.Version != AgentProvenanceVersion) || !provenanceID.MatchString(p.Agent) || !provenanceSHA.MatchString(p.Repository) || !provenanceSHA.MatchString(p.Source) || !provenanceSHA.MatchString(p.SourcePath) || !provenanceCommit.MatchString(p.Commit) || !provenanceBranch.MatchString(p.Branch) || p.Branch == "HEAD" {
		return p, errors.New("invalid agent provenance or detached branch")
	}
	if !validDependencies(p) {
		return p, errors.New("invalid agent provenance dependencies")
	}
	return p, nil
}

// validDependencies accepts no dependency fields in version 1 and, in
// version 2, a bounded list sorted by unique path whose digest matches.
func validDependencies(p AgentProvenance) bool {
	if p.Version == 1 {
		return p.Dependencies == nil && p.DependenciesSHA256 == ""
	}
	if len(p.Dependencies) > provenanceMaxModules+len(provenanceLockfiles) || p.DependenciesSHA256 != dependenciesDigest(p.Dependencies) {
		return false
	}
	for index, dep := range p.Dependencies {
		if !validDependencyPath(dep.Path) || (dep.Kind != DependencyImport && dep.Kind != DependencyLockfile) || !provenanceSHA.MatchString(dep.SHA256) {
			return false
		}
		if index > 0 && p.Dependencies[index-1].Path >= dep.Path {
			return false
		}
	}
	return true
}

// CaptureAgentProvenance requests only worktree identity, the selected regular
// source file, the local modules it imports and the nearest lockfile; it never
// requests configuration, remotes or credentials, and never executes source.
// An import graph that cannot be captured completely returns an
// ImportGraphError, so provenance stays unknown rather than matching.
func CaptureAgentProvenance(ctx context.Context, spec, agent string) (AgentProvenance, error) {
	return captureAgentProvenance(ctx, spec, agent, AgentProvenanceVersion)
}

func captureAgentProvenance(ctx context.Context, spec, agent string, version int) (AgentProvenance, error) {
	var empty AgentProvenance
	if !provenanceID.MatchString(agent) {
		return empty, errors.New("invalid agent identifier")
	}
	abs, err := filepath.Abs(spec)
	if err != nil {
		return empty, errors.New("source unavailable for provenance")
	}
	abs, err = filepath.EvalSymlinks(abs)
	if err != nil {
		return empty, errors.New("source unavailable for provenance")
	}
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	git := func(arguments ...string) (string, error) {
		command := exec.Command("git", append([]string{"-C", filepath.Dir(abs), "rev-parse"}, arguments...)...)
		command.Env = []string{}
		if path, ok := os.LookupEnv("PATH"); ok {
			command.Env = append(command.Env, "PATH="+path)
		}
		out := &limitedProvenanceOutput{}
		command.Stdout = out
		command.Stderr = io.Discard
		if RunOwnedCommand(ctx, command) != nil || ctx.Err() != nil {
			return "", errors.New("Git provenance unavailable")
		}
		return strings.TrimSpace(out.String()), nil
	}
	root, err := git("--show-toplevel")
	if err != nil {
		return empty, err
	}
	root, err = filepath.EvalSymlinks(root)
	if err != nil {
		return empty, errors.New("repository unavailable")
	}
	rel, err := filepath.Rel(root, abs)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return empty, errors.New("source outside repository")
	}
	branch, err := git("--abbrev-ref", "HEAD")
	if err != nil {
		return empty, err
	}
	commit, err := git("HEAD")
	if err != nil {
		return empty, err
	}
	info, err := os.Stat(abs)
	if err != nil || !info.Mode().IsRegular() || info.Size() > provenanceSourceLimit {
		return empty, errors.New("source unavailable or too large")
	}
	file, err := os.Open(abs)
	if err != nil {
		return empty, errors.New("source unavailable")
	}
	defer file.Close()
	info, err = file.Stat()
	if err != nil || !info.Mode().IsRegular() || info.Size() > provenanceSourceLimit {
		return empty, errors.New("source unavailable or too large")
	}
	data, err := io.ReadAll(io.LimitReader(file, provenanceSourceLimit+1))
	if err != nil || len(data) > provenanceSourceLimit {
		return empty, errors.New("source unavailable or too large")
	}
	p := AgentProvenance{Version: version, Agent: agent, Repository: provenanceHash([]byte(root)), Branch: branch, Commit: commit, Source: provenanceHash(data), SourcePath: provenanceHash([]byte(filepath.ToSlash(rel)))}
	if version >= 2 {
		deps, err := captureDependencies(ctx, root, abs, filepath.ToSlash(rel), data)
		if err != nil {
			return empty, err
		}
		p.Dependencies, p.DependenciesSHA256 = deps, dependenciesDigest(deps)
	}
	raw, _ := json.Marshal(p)
	return ParseAgentProvenance(raw)
}

// CaptureAgentProvenanceFor captures the checkout in the form of expected, so
// the two can be compared: a version 1 creation snapshot is checked against a
// spec-only capture, as before version 2, and an import that a version 2
// capture could not follow does not turn an otherwise valid version 1
// comparison unknown. A version 2 snapshot always captures the import graph.
func CaptureAgentProvenanceFor(ctx context.Context, spec string, expected AgentProvenance) (AgentProvenance, error) {
	version := AgentProvenanceVersion
	if expected.Version == 1 {
		version = 1
	}
	return captureAgentProvenance(ctx, spec, expected.Agent, version)
}

type limitedProvenanceOutput struct{ buffer bytes.Buffer }

func (b *limitedProvenanceOutput) Write(p []byte) (int, error) {
	if b.buffer.Len()+len(p) > 4096 {
		return 0, errors.New("Git provenance output limit")
	}
	return b.buffer.Write(p)
}

func (b *limitedProvenanceOutput) String() string { return b.buffer.String() }

// CompareAgentProvenance compares a creation snapshot with a fresh capture.
// A version 1 snapshot has no dependency list, so only the spec is compared.
func CompareAgentProvenance(expected, actual AgentProvenance) ProvenanceAssessment {
	compare := func(a, b string) string {
		if a == b {
			return "matched"
		}
		return "mismatch"
	}
	r := ProvenanceAssessment{Status: "matched", Scope: ProvenanceScopeSpec, Branch: compare(expected.Branch, actual.Branch), Commit: compare(expected.Commit, actual.Commit), Source: compare(expected.Source, actual.Source), Repository: compare(expected.Repository, actual.Repository), Expected: &expected, Before: &actual}
	if expected.SourcePath != actual.SourcePath {
		r.Source = "mismatch"
	}
	if expected.Version >= 2 {
		r.Scope = ProvenanceScopeImports
		r.Dependencies, r.DependencyChanges = compareDependencies(expected, actual)
	}
	if r.Branch != "matched" || r.Commit != "matched" || r.Source != "matched" || r.Repository != "matched" || (r.Dependencies != "" && r.Dependencies != "matched") {
		r.Status = "mismatch"
	}
	return r
}

// compareDependencies names every dependency that was modified, added or
// removed since the snapshot, sorted by path.
func compareDependencies(expected, actual AgentProvenance) (string, []ProvenanceChange) {
	if actual.Version < 2 {
		return "unknown", nil
	}
	before := make(map[string]ProvenanceDependency, len(expected.Dependencies))
	for _, dep := range expected.Dependencies {
		before[dep.Path] = dep
	}
	var changes []ProvenanceChange
	for _, dep := range actual.Dependencies {
		old, ok := before[dep.Path]
		delete(before, dep.Path)
		if !ok {
			changes = append(changes, ProvenanceChange{dep.Path, dep.Kind, "added"})
		} else if old != dep {
			changes = append(changes, ProvenanceChange{dep.Path, dep.Kind, "modified"})
		}
	}
	for _, dep := range before {
		changes = append(changes, ProvenanceChange{dep.Path, dep.Kind, "removed"})
	}
	if len(changes) == 0 {
		return "matched", nil
	}
	slices.SortFunc(changes, func(a, b ProvenanceChange) int { return strings.Compare(a.Path, b.Path) })
	return "mismatch", changes
}

func UnknownAgentProvenance() ProvenanceAssessment {
	return ProvenanceAssessment{Status: "unknown", Branch: "unknown", Commit: "unknown", Source: "unknown", Repository: "unknown", Dependencies: "unknown"}
}
