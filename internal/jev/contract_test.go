package jev

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"slices"
	"strings"
	"testing"
)

func TestChoiceAndFractionalScoreOverHTTP(t *testing.T) {
	c, b, o, q := fixture(t, func(w http.ResponseWriter, r *http.Request) {
		var request struct {
			Questions map[string]struct {
				Criteria json.RawMessage
			}
		}
		if json.NewDecoder(r.Body).Decode(&request) != nil || string(request.Questions["severity"].Criteria) != `["clear","degraded","blocked"]` {
			t.Error("ordered score levels not sent")
		}
		_, _ = io.WriteString(w, `{"model":"jev-1.14.0","answers":{"action":{"type":"choice","choice":"inspect","confidence":0.8,"probabilities":{"inspect":0.9,"abstain":0.1}},"severity":{"type":"score","score":1.43,"confidence":0.35,"legend":{"0":"clear","1":"degraded","2":"blocked"},"probabilities":{"0":0,"1":0.57,"2":0.43}}}}`)
	}, Config{Enabled: true}, DefaultLimits())
	q.Questions = map[string]Question{
		"action":   {Type: "choice", Instructions: "Choose an advisory next step.", Criteria: map[string]string{"inspect": "Inspect current controls", "abstain": "Insufficient evidence"}},
		"severity": {Type: "score", Instructions: "Rate the observed failure.", Levels: []string{"clear", "degraded", "blocked"}},
	}
	result, err := c.Decide(context.Background(), b, o, q)
	if err != nil || result.Answers["action"].Choice != "inspect" || result.Answers["severity"].Score == nil || *result.Answers["severity"].Score != 1.43 {
		t.Fatal("typed choice or fractional score rejected", err)
	}
}

func TestRejectsInvalidTypedAnswers(t *testing.T) {
	cases := []struct {
		name   string
		q      Question
		answer string
	}{
		{"foreign choice", Question{Type: "choice", Criteria: map[string]string{"a": "A", "b": "B"}}, `{"type":"choice","choice":"execute","confidence":0.9,"probabilities":{"a":0.9,"b":0.1}}`},
		{"foreign probability", Question{Type: "choice", Criteria: map[string]string{"a": "A", "b": "B"}}, `{"type":"choice","choice":"a","confidence":0.9,"probabilities":{"a":0.9,"execute":0.1}}`},
		{"distribution sum", Question{Type: "choice", Criteria: map[string]string{"a": "A", "b": "B"}}, `{"type":"choice","choice":"a","confidence":0.9,"probabilities":{"a":0.9,"b":0.9}}`},
		{"confidence range", Question{Type: "choice", Criteria: map[string]string{"a": "A", "b": "B"}}, `{"type":"choice","choice":"a","confidence":1.1,"probabilities":{"a":0.9,"b":0.1}}`},
		{"missing box edge", Question{Type: "box"}, `{"type":"box","confidence":0.9,"box":{"top":0.1,"right":0.8,"bottom":0.8}}`},
		{"box alias hides missing top", Question{Type: "box"}, `{"type":"box","confidence":0.9,"box":{"left":0.1,"Left":0.2,"right":0.8,"bottom":0.8}}`},
		{"reversed box", Question{Type: "box"}, `{"type":"box","confidence":0.9,"box":{"left":0.9,"top":0.1,"right":0.8,"bottom":0.8}}`},
		{"zero box", Question{Type: "box"}, `{"type":"box","confidence":0.9,"box":{"left":0.1,"top":0.1,"right":0.1,"bottom":0.8}}`},
		{"out of image", Question{Type: "box"}, `{"type":"box","confidence":0.9,"box":{"left":-0.1,"top":0.1,"right":0.8,"bottom":0.8}}`},
		{"unknown coordinate", Question{Type: "box"}, `{"type":"box","confidence":0.9,"box":{"left":0.1,"top":0.1,"right":0.8,"bottom":0.8,"x":0.2}}`},
		{"changed legend", Question{Type: "score", Levels: []string{"clear", "blocked"}}, `{"type":"score","score":0.2,"confidence":0.8,"legend":{"0":"clear","1":"execute"},"probabilities":{"0":0.8,"1":0.2}}`},
		{"score inconsistent", Question{Type: "score", Levels: []string{"clear", "blocked"}}, `{"type":"score","score":0.8,"confidence":0.8,"legend":{"0":"clear","1":"blocked"},"probabilities":{"0":0.8,"1":0.2}}`},
		{"score out of range", Question{Type: "score", Levels: []string{"clear", "blocked"}}, `{"type":"score","score":2,"confidence":0.8,"legend":{"0":"clear","1":"blocked"},"probabilities":{"0":0.8,"1":0.2}}`},
		{"score missing probability", Question{Type: "score", Levels: []string{"clear", "blocked"}}, `{"type":"score","score":0,"confidence":0.8,"legend":{"0":"clear","1":"blocked"},"probabilities":{"0":1}}`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			contract := Contract{Questions: map[string]Question{"answer": tc.q}}
			raw := []byte(`{"model":"jev-1.14.0","answers":{"answer":` + tc.answer + `}}`)
			if answers, _, _, err := parseResponse(raw, contract); err == nil || answers != nil {
				t.Fatal("invalid provider answer accepted")
			}
		})
	}
}

func TestResponseRequiresExactFieldNames(t *testing.T) {
	noul := `{"model":"jev-1.14.0","answers":{"answer":{"type":"noul","noul":0.97}},"usage":{"input_tokens":0,"output_tokens":0}}`
	box := `{"model":"jev-1.14.0","answers":{"answer":{"type":"box","confidence":0.9,"box":{"left":0,"top":0,"right":0.8,"bottom":0.8}}}}`
	cases := []struct {
		name, body string
		q          Question
		path       []string
		required   []string
	}{
		{"envelope", noul, Question{Type: "noul"}, nil, []string{"model", "answers"}},
		{"usage", noul, Question{Type: "noul"}, []string{"usage"}, []string{"input_tokens", "output_tokens"}},
		{"noul", noul, Question{Type: "noul"}, []string{"answers", "answer"}, []string{"type", "noul"}},
		{"choice", `{"model":"jev-1.14.0","answers":{"answer":{"type":"choice","choice":"a","confidence":0.9,"probabilities":{"a":0.9,"A":0.1}}}}`, Question{Type: "choice", Criteria: map[string]string{"a": "A", "A": "Other"}}, []string{"answers", "answer"}, []string{"type", "choice", "confidence", "probabilities"}},
		{"score", `{"model":"jev-1.14.0","answers":{"answer":{"type":"score","score":0.2,"confidence":0.8,"legend":{"0":"clear","1":"blocked"},"probabilities":{"0":0.8,"1":0.2}}}}`, Question{Type: "score", Levels: []string{"clear", "blocked"}}, []string{"answers", "answer"}, []string{"type", "score", "confidence", "legend", "probabilities"}},
		{"box", box, Question{Type: "box"}, []string{"answers", "answer"}, []string{"type", "confidence", "box"}},
		{"edges", box, Question{Type: "box"}, []string{"answers", "answer", "box"}, []string{"left", "top", "right", "bottom"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			contract := Contract{Questions: map[string]Question{"answer": tc.q}}
			if _, _, _, err := parseResponse([]byte(tc.body), contract); err != nil {
				t.Fatal("valid response rejected", err)
			}
			var original map[string]any
			if err := json.Unmarshal([]byte(tc.body), &original); err != nil {
				t.Fatal(err)
			}
			object := original
			for _, key := range tc.path {
				object = object[key].(map[string]any)
			}
			for field := range object {
				for _, mutation := range []string{"alias", "duplicate alias", "missing"} {
					if mutation == "missing" && !slices.Contains(tc.required, field) {
						continue
					}
					t.Run(field+"/"+mutation, func(t *testing.T) {
						var body map[string]any
						if err := json.Unmarshal([]byte(tc.body), &body); err != nil {
							t.Fatal(err)
						}
						fields := body
						for _, key := range tc.path {
							fields = fields[key].(map[string]any)
						}
						if mutation != "missing" {
							fields[strings.ToUpper(field)] = fields[field]
						}
						if mutation != "duplicate alias" {
							delete(fields, field)
						}
						raw, err := json.Marshal(body)
						if err != nil {
							t.Fatal(err)
						}
						answers, model, usage, err := parseResponse(raw, contract)
						if err == nil || answers != nil || model != "" || usage != (Usage{}) {
							t.Fatal("malformed field names produced a response")
						}
					})
				}
			}
		})
	}
}

func TestInvalidContractMakesNoRequest(t *testing.T) {
	c, b, o, base := fixture(t, func(http.ResponseWriter, *http.Request) { t.Error("invalid contract reached provider") }, Config{Enabled: true}, DefaultLimits())
	cases := []Contract{{}, {Version: Version, Task: "task", QuestionVersion: "v1", CriteriaVersion: "v1", Questions: map[string]Question{"q": {Type: "tools", Instructions: "Execute code"}}}}
	for _, q := range []Question{
		{Type: "noul", Instructions: strings.Repeat("x", 8001)},
		{Type: "noul", Instructions: "Question", Criteria: map[string]string{"yes": "yes", "no": "no"}},
		{Type: "box", Instructions: "Question", Criteria: map[string]string{"a": "A"}},
		{Type: "choice", Instructions: "Question", Criteria: map[string]string{"a": "A"}},
		{Type: "score", Instructions: "Question", Levels: []string{"only one"}},
		{Type: "noul", Instructions: "Question", Levels: []string{"extra"}},
	} {
		cc := base
		cc.Questions = map[string]Question{"q": q}
		cases = append(cases, cc)
	}
	for _, contract := range cases {
		if result, err := c.Decide(context.Background(), b, o, contract); err == nil || result.Record.RequestCount != 0 || result.Answers != nil {
			t.Fatal("invalid contract accepted")
		}
	}
}
