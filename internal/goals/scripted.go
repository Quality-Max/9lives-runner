package goals

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"slices"
	"sync"

	"github.com/qualitymax/9lives-runner/internal/healing/tier2"
)

// Scripted is an explicit offline qualification provider, never a claimed LLM.
// Labels match only current candidates; ambiguity abstains instead of guessing.
type Scripted struct {
	mu    sync.Mutex
	steps []scriptStep
	index int
}
type scriptStep struct {
	Action    string `json:"action"`
	Label     string `json:"label,omitempty"`
	Parameter string `json:"parameter,omitempty"`
}

func LoadScript(path string) (*Scripted, error) {
	info, err := os.Stat(path)
	if err != nil || !info.Mode().IsRegular() || info.Size() > 16000 {
		return nil, errors.New("invalid goal script")
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, errors.New("invalid goal script")
	}
	var script struct {
		Version string       `json:"version"`
		Steps   []scriptStep `json:"steps"`
	}
	if decode(raw, &script) != nil || script.Version != Version || len(script.Steps) == 0 || len(script.Steps) > 100 {
		return nil, errors.New("invalid goal script")
	}
	for _, s := range script.Steps {
		if !slices.Contains([]string{"click", "fill", "select", "check", "wait", "complete", "unresolved"}, s.Action) || len(s.Label) > 160 {
			return nil, errors.New("invalid goal script")
		}
	}
	return &Scripted{steps: script.Steps}, nil
}
func (*Scripted) Name() string       { return "scripted" }
func (s *Scripted) Clone() *Scripted { return &Scripted{steps: s.steps} }
func (s *Scripted) CompleteDecision(ctx context.Context, prompt, model string, maxTokens int) (tier2.Completion, error) {
	if ctx.Err() != nil {
		return tier2.Completion{}, ctx.Err()
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	d := Decision{Action: "unresolved"}
	if s.index < len(s.steps) {
		step := s.steps[s.index]
		d = Decision{Action: step.Action, Parameter: step.Parameter}
		if step.Label != "" {
			var state struct {
				Controls []Candidate `json:"observedControls"`
			}
			if json.Unmarshal([]byte(prompt), &state) != nil {
				return tier2.Completion{}, errors.New("invalid script state")
			}
			matches := 0
			for _, c := range state.Controls {
				if c.Label == step.Label && slices.Contains(c.Actions, step.Action) {
					d.TargetID = c.ID
					matches++
				}
			}
			if matches == 0 {
				d = Decision{Action: "wait"}
			} else if matches != 1 {
				d = Decision{Action: "unresolved"}
			} else {
				s.index++
			}
		} else {
			s.index++
		}
	}
	raw, _ := json.Marshal(d)
	return tier2.Completion{Text: string(raw)}, nil
}
