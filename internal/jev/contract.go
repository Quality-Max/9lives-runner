// Package jev supplies optional typed judgments, never actions or test verdicts.
package jev

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"math"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/Quality-Max/9lives-runner/internal/strictjson"
)

const Version = "9l.jev/1"

var identifier = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9_.:-]{0,127}$`)
var revision = regexp.MustCompile(`^(?:[a-f0-9]{40}|[a-f0-9]{64})$`)
var modelName = regexp.MustCompile(`^jev-[a-zA-Z0-9_.-]{1,64}$`)

// Contract is built by Go callers. Page observations cannot supply questions,
// criteria, model options, credentials or policy. Versions name their meaning.
type Contract struct {
	Version, Task, QuestionVersion, CriteriaVersion string
	Questions                                       map[string]Question
}

type Question struct {
	Type         string            `json:"type"`
	Instructions string            `json:"instructions"`
	Criteria     map[string]string `json:"criteria,omitempty"`
	Levels       []string          `json:"-"` // ordered Score criteria
}

func (q Question) MarshalJSON() ([]byte, error) {
	var criteria any
	if len(q.Criteria) != 0 {
		criteria = q.Criteria
	}
	if q.Type == "score" {
		criteria = q.Levels
	}
	return json.Marshal(struct {
		Type         string `json:"type"`
		Instructions string `json:"instructions"`
		Criteria     any    `json:"criteria,omitempty"`
	}{q.Type, q.Instructions, criteria})
}

func (c Contract) validate() error {
	if c.Version != Version || !identifier.MatchString(c.Task) || !identifier.MatchString(c.QuestionVersion) || !identifier.MatchString(c.CriteriaVersion) || len(c.Questions) == 0 || len(c.Questions) > 8 {
		return errors.New("invalid Jev contract")
	}
	for id, q := range c.Questions {
		if !identifier.MatchString(id) || !boundedText(q.Instructions, 8000) {
			return errors.New("invalid Jev question")
		}
		if q.Type != "score" && len(q.Levels) != 0 {
			return errors.New("unexpected Jev score levels")
		}
		switch q.Type {
		case "score":
			if len(q.Criteria) != 0 || len(q.Levels) < 2 || len(q.Levels) > 10 {
				return errors.New("Jev score needs 2-10 ordered levels")
			}
			for _, level := range q.Levels {
				if !boundedText(level, 2000) {
					return errors.New("invalid Jev score level")
				}
			}
		case "choice":
			if len(q.Criteria) < 2 || len(q.Criteria) > 64 {
				return errors.New("Jev choice needs 2-64 owned candidates")
			}
		case "noul":
			if len(q.Criteria) != 0 && (len(q.Criteria) != 2 || q.Criteria["true"] == "" || q.Criteria["false"] == "") {
				return errors.New("invalid Jev noul criteria")
			}
		case "box":
			if len(q.Criteria) != 0 {
				return errors.New("Jev Box does not accept criteria")
			}
		default:
			return errors.New("unsupported Jev question type")
		}
		for key, value := range q.Criteria {
			if !identifier.MatchString(key) || !boundedText(value, 2000) {
				return errors.New("invalid Jev criterion")
			}
		}
	}
	return nil
}

func boundedText(s string, max int) bool {
	return len(s) <= max && utf8.ValidString(s) && strings.TrimSpace(s) != "" && !strings.ContainsRune(s, '\x00')
}

type Box struct {
	Left   float64 `json:"left"`
	Top    float64 `json:"top"`
	Right  float64 `json:"right"`
	Bottom float64 `json:"bottom"`
}

// Answer is transient. Even a valid Box does not prove a target exists. The
// caller must ask presence/ambiguity questions and verify observed identity.
type Answer struct {
	Type          string             `json:"type"`
	Noul          *float64           `json:"noul,omitempty"`
	Choice        string             `json:"choice,omitempty"`
	Confidence    *float64           `json:"confidence,omitempty"`
	Probabilities map[string]float64 `json:"probabilities,omitempty"`
	Box           *Box               `json:"box,omitempty"`
	Score         *float64           `json:"score,omitempty"`
	Legend        map[string]string  `json:"legend,omitempty"`
}

func probability(v float64) bool { return !math.IsNaN(v) && !math.IsInf(v, 0) && v >= 0 && v <= 1 }

func decode(raw []byte, dst any) error {
	d := json.NewDecoder(bytes.NewReader(raw))
	if strictjson.Value(d) != nil {
		return errors.New("invalid Jev response schema")
	}
	if _, err := d.Token(); err != io.EOF {
		return errors.New("invalid Jev response schema")
	}
	d = json.NewDecoder(bytes.NewReader(raw))
	d.DisallowUnknownFields()
	if d.Decode(dst) != nil {
		return errors.New("invalid Jev response schema")
	}
	return nil
}

type response struct {
	Model   string                     `json:"model"`
	Answers map[string]json.RawMessage `json:"answers"`
	Usage   *struct {
		Input  *int `json:"input_tokens"`
		Output *int `json:"output_tokens"`
	} `json:"usage,omitempty"`
}

// Struct decoding accepts case-insensitive field aliases. Check the exact
// required/optional names before decoding so aliases cannot hide missing data.
func responseFields(raw []byte, required []string, optional ...string) (map[string]json.RawMessage, error) {
	var fields map[string]json.RawMessage
	invalid := errors.New("invalid Jev response schema")
	if decode(raw, &fields) != nil {
		return nil, invalid
	}
	for _, key := range required {
		if _, ok := fields[key]; !ok {
			return nil, invalid
		}
	}
	for key := range fields {
		if !slices.Contains(required, key) && !slices.Contains(optional, key) {
			return nil, invalid
		}
	}
	return fields, nil
}

func parseResponse(raw []byte, contract Contract) (map[string]Answer, string, Usage, error) {
	var r response
	invalid := errors.New("invalid Jev response schema")
	fields, err := responseFields(raw, []string{"model", "answers"}, "usage")
	if err != nil {
		return nil, "", Usage{}, invalid
	}
	if usage, ok := fields["usage"]; ok {
		if _, err := responseFields(usage, []string{"input_tokens", "output_tokens"}); err != nil {
			return nil, "", Usage{}, invalid
		}
	}
	if decode(raw, &r) != nil || !modelName.MatchString(r.Model) || len(r.Answers) != len(contract.Questions) {
		return nil, "", Usage{}, invalid
	}
	usage := Usage{}
	if r.Usage != nil {
		if r.Usage.Input == nil || r.Usage.Output == nil || *r.Usage.Input < 0 || *r.Usage.Output < 0 || *r.Usage.Input > 10000000 || *r.Usage.Output > 10000000 {
			return nil, "", Usage{}, invalid
		}
		usage = Usage{InputTokens: *r.Usage.Input, OutputTokens: *r.Usage.Output, Available: true}
	}
	answers := make(map[string]Answer, len(r.Answers))
	for id, q := range contract.Questions {
		var required []string
		switch q.Type {
		case "noul":
			required = []string{"type", "noul"}
		case "choice":
			required = []string{"type", "choice", "confidence", "probabilities"}
		case "box":
			required = []string{"type", "confidence", "box"}
		case "score":
			required = []string{"type", "score", "confidence", "legend", "probabilities"}
		default:
			return nil, "", Usage{}, invalid
		}
		fields, err := responseFields(r.Answers[id], required)
		if err != nil {
			return nil, "", Usage{}, invalid
		}
		if q.Type == "box" {
			if _, err := responseFields(fields["box"], []string{"left", "top", "right", "bottom"}); err != nil {
				return nil, "", Usage{}, invalid
			}
		}
		var a Answer
		if decode(r.Answers[id], &a) != nil || a.Type != q.Type {
			return nil, "", Usage{}, invalid
		}
		switch q.Type {
		case "noul":
			if a.Noul == nil || !probability(*a.Noul) {
				return nil, "", Usage{}, invalid
			}
		case "choice":
			if a.Confidence == nil || !probability(*a.Confidence) || q.Criteria[a.Choice] == "" || len(a.Probabilities) != len(q.Criteria) {
				return nil, "", Usage{}, invalid
			}
			sum := 0.0
			for key := range q.Criteria {
				p, ok := a.Probabilities[key]
				if !ok || !probability(p) {
					return nil, "", Usage{}, invalid
				}
				sum += p
			}
			if math.Abs(sum-1) > 0.001 {
				return nil, "", Usage{}, invalid
			}
		case "box":
			b := a.Box
			if a.Confidence == nil || !probability(*a.Confidence) || b == nil || !probability(b.Left) || !probability(b.Top) || !probability(b.Right) || !probability(b.Bottom) || b.Left >= b.Right || b.Top >= b.Bottom {
				return nil, "", Usage{}, invalid
			}
		case "score":
			if a.Score == nil || math.IsNaN(*a.Score) || math.IsInf(*a.Score, 0) || *a.Score < 0 || *a.Score > float64(len(q.Levels)-1) || a.Confidence == nil || !probability(*a.Confidence) || len(a.Legend) != len(q.Levels) || len(a.Probabilities) != len(q.Levels) {
				return nil, "", Usage{}, invalid
			}
			sum, weighted := 0.0, 0.0
			for i, level := range q.Levels {
				key := strconv.Itoa(i)
				p, ok := a.Probabilities[key]
				if !ok || !probability(p) || a.Legend[key] != level {
					return nil, "", Usage{}, invalid
				}
				sum += p
				weighted += float64(i) * p
			}
			if math.Abs(sum-1) > 0.001 || math.Abs(weighted-*a.Score) > 0.02 {
				return nil, "", Usage{}, invalid
			}
		}
		answers[id] = a
	}
	return answers, r.Model, usage, nil
}
