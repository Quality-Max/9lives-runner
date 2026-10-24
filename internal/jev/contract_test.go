package jev

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
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
