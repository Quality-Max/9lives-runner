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
	"strings"
	"time"

	"github.com/Quality-Max/9lives-runner/internal/strictjson"
)

// AgentProvenance records a local snapshot, not authenticated agent authorship.
// Repository is a hash of the repository root, never a remote URL or config.
type AgentProvenance struct {
	Version    int    `json:"version"`
	Agent      string `json:"agent"`
	Repository string `json:"repositorySHA256"`
	Branch     string `json:"branch"`
	Commit     string `json:"commit"`
	Source     string `json:"sourceSHA256"`
	SourcePath string `json:"sourcePathSHA256"`
}
type ProvenanceAssessment struct {
	Status     string           `json:"status"`
	Branch     string           `json:"branch"`
	Commit     string           `json:"commit"`
	Source     string           `json:"source"`
	Repository string           `json:"repository"`
	Expected   *AgentProvenance `json:"expected,omitempty"`
	Before     *AgentProvenance `json:"before,omitempty"`
	After      *AgentProvenance `json:"after,omitempty"`
}

func provenanceHash(raw []byte) string { sum := sha256.Sum256(raw); return hex.EncodeToString(sum[:]) }

var provenanceID = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9_.-]{0,79}$`)
var provenanceSHA = regexp.MustCompile(`^[a-f0-9]{64}$`)
var provenanceCommit = regexp.MustCompile(`^(?:[a-f0-9]{40}|[a-f0-9]{64})$`)
var provenanceBranch = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9_./-]{0,254}$`)

func ParseAgentProvenance(raw []byte) (AgentProvenance, error) {
	var p AgentProvenance
	d := json.NewDecoder(bytes.NewReader(raw))
	if len(raw) > 4096 || strictjson.Value(d) != nil {
		return p, errors.New("invalid agent provenance")
	}
	if _, err := d.Token(); err != io.EOF {
		return p, errors.New("invalid agent provenance")
	}
	d = json.NewDecoder(bytes.NewReader(raw))
	d.DisallowUnknownFields()
	if d.Decode(&p) != nil || p.Version != 1 || !provenanceID.MatchString(p.Agent) || !provenanceSHA.MatchString(p.Repository) || !provenanceSHA.MatchString(p.Source) || !provenanceSHA.MatchString(p.SourcePath) || !provenanceCommit.MatchString(p.Commit) || !provenanceBranch.MatchString(p.Branch) || p.Branch == "HEAD" {
		return p, errors.New("invalid agent provenance or detached branch")
	}
	return p, nil
}

// CaptureAgentProvenance requests only worktree identity and the selected
// regular source file; it never requests configuration, remotes or credentials.
func CaptureAgentProvenance(ctx context.Context, spec, agent string) (AgentProvenance, error) {
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
	if err != nil || !info.Mode().IsRegular() || info.Size() > 1<<20 {
		return empty, errors.New("source unavailable or too large")
	}
	file, err := os.Open(abs)
	if err != nil {
		return empty, errors.New("source unavailable")
	}
	defer file.Close()
	info, err = file.Stat()
	if err != nil || !info.Mode().IsRegular() || info.Size() > 1<<20 {
		return empty, errors.New("source unavailable or too large")
	}
	data, err := io.ReadAll(io.LimitReader(file, (1<<20)+1))
	if err != nil || len(data) > 1<<20 {
		return empty, errors.New("source unavailable or too large")
	}
	p := AgentProvenance{1, agent, provenanceHash([]byte(root)), branch, commit, provenanceHash(data), provenanceHash([]byte(filepath.ToSlash(rel)))}
	raw, _ := json.Marshal(p)
	return ParseAgentProvenance(raw)
}

type limitedProvenanceOutput struct{ buffer bytes.Buffer }

func (b *limitedProvenanceOutput) Write(p []byte) (int, error) {
	if b.buffer.Len()+len(p) > 4096 {
		return 0, errors.New("Git provenance output limit")
	}
	return b.buffer.Write(p)
}

func (b *limitedProvenanceOutput) String() string { return b.buffer.String() }

func CompareAgentProvenance(expected, actual AgentProvenance) ProvenanceAssessment {
	compare := func(a, b string) string {
		if a == b {
			return "matched"
		}
		return "mismatch"
	}
	r := ProvenanceAssessment{Status: "matched", Branch: compare(expected.Branch, actual.Branch), Commit: compare(expected.Commit, actual.Commit), Source: compare(expected.Source, actual.Source), Repository: compare(expected.Repository, actual.Repository), Expected: &expected, Before: &actual}
	if expected.SourcePath != actual.SourcePath {
		r.Source = "mismatch"
	}
	if r.Branch != "matched" || r.Commit != "matched" || r.Source != "matched" || r.Repository != "matched" {
		r.Status = "mismatch"
	}
	return r
}

func UnknownAgentProvenance() ProvenanceAssessment {
	return ProvenanceAssessment{Status: "unknown", Branch: "unknown", Commit: "unknown", Source: "unknown", Repository: "unknown"}
}
