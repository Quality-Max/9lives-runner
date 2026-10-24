package main

import (
	"encoding/json"
	"fmt"
	"math"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/Quality-Max/9lives-runner/internal/jev"
)

func TestHuntRequiresEveryConfidentSpotBeforeAnyAction(t *testing.T) {
	yes, no := .97, .03
	answers := map[string]jev.Answer{}
	for id := 0; id < 9; id++ {
		answers[fmt.Sprintf("spot%d", id)] = jev.Answer{Type: "noul", Noul: &no}
	}
	for _, id := range []int{0, 2, 4} {
		answers[fmt.Sprintf("spot%d", id)] = jev.Answer{Type: "noul", Noul: &yes}
	}
	if got := chooseHunt(answers); !got.Resolved || !reflect.DeepEqual(got.IDs, []int{0, 2, 4}) {
		t.Fatal("independent judgments did not select the observed controls")
	}
	for _, probability := range []float64{.5, .699, .301, -1, 2, math.NaN(), math.Inf(1)} {
		answers["spot8"] = jev.Answer{Type: "noul", Noul: &probability}
		if got := chooseHunt(answers); got.Resolved || len(got.IDs) != 0 {
			t.Fatal("partial or uncertain judgments permitted clicks")
		}
	}
	answers["spot8"] = jev.Answer{Type: "noul"}
	if chooseHunt(answers).Resolved {
		t.Fatal("missing probability permitted clicks")
	}
	delete(answers, "spot8")
	if chooseHunt(answers).Resolved {
		t.Fatal("missing spot permitted clicks")
	}
}

// Regression: a real synthetic cats request returned .81–.93. Those are yes
// probabilities, not Choice confidence values to normalize a second time.
func TestHuntUsesBinaryProbabilityForVerifiedGameActions(t *testing.T) {
	yes, no := .7, .3
	answers := map[string]jev.Answer{}
	for id := 0; id < 9; id++ {
		answers[fmt.Sprintf("spot%d", id)] = jev.Answer{Type: "noul", Noul: &no}
	}
	for _, id := range []int{0, 2, 4} {
		answers[fmt.Sprintf("spot%d", id)] = jev.Answer{Type: "noul", Noul: &yes}
	}
	if got := chooseHunt(answers); !got.Resolved || !reflect.DeepEqual(got.IDs, []int{0, 2, 4}) {
		t.Fatal("clear binary judgments were rejected before independent verification")
	}
}

func TestHuntGroundingAndSubjectDoNotIncludeTheAnswerMap(t *testing.T) {
	candidates := make([]huntCandidate, 9)
	for i := range candidates {
		candidates[i] = huntCandidate{i, rectangle{float64(i * 10), 20, 4, 5}}
	}
	if !validHuntCandidates(candidates) {
		t.Fatal("valid observed controls rejected")
	}
	for _, bad := range []huntCandidate{{0, rectangle{math.NaN(), 20, 4, 5}}, {0, rectangle{99, 20, 4, 5}}, {2, rectangle{0, 20, 4, 5}}} {
		changed := append([]huntCandidate(nil), candidates...)
		changed[0] = bad
		if validHuntCandidates(changed) {
			t.Fatal("invalid control geometry or identity accepted")
		}
	}
	for _, scenario := range []string{"cats", "dogs", "mice", "striped"} {
		c := huntContract(scenario, candidates)
		if len(c.Questions) != 9 {
			t.Fatal("one bounded request must cover all spots")
		}
		for _, q := range c.Questions {
			if q.Type != "noul" || len(q.Criteria) != 0 || !strings.Contains(q.Instructions, huntSubject(scenario)) {
				t.Fatal("independent visual question is missing")
			}
		}
		raw, _ := json.Marshal(c)
		for _, private := range []string{"data-broken", "fixtureSHA256", "catGame", "decoyIDs", "actualCount"} {
			if strings.Contains(string(raw), private) {
				t.Fatal("model contract contains an answer oracle")
			}
		}
	}
}

func TestHuntRequestsCannotBypassOwnershipLiveGateOrBudget(t *testing.T) {
	s := testDemoServer(t, false, nil)
	valid := `{"id":"0123456789abcdef0123456789abcdef","scenario":"cats","live":false,"broken":false}`
	for _, mutate := range []func(*http.Request){
		func(r *http.Request) { r.Host = "foreign.example" },
		func(r *http.Request) { r.Header.Del("Origin") },
		func(r *http.Request) { r.Header.Del("X-Demo-Session") },
		func(r *http.Request) { r.Header.Set("Content-Type", "text/plain") },
	} {
		r := demoRequest(s, "POST", "/api/hunt", valid)
		mutate(r)
		w := httptest.NewRecorder()
		s.ServeHTTP(w, r)
		if w.Code != 403 {
			t.Fatal("unowned request admitted")
		}
	}
	for _, raw := range []string{
		strings.Replace(valid, `"live":false`, `"live":null`, 1),
		strings.Replace(valid, `"live":false`, `"Live":false`, 1),
		strings.Replace(valid, `"broken":false`, `"broken":false,"broken":true`, 1),
		strings.Replace(valid, `"scenario":"cats"`, `"scenario":"https://foreign.example"`, 1),
		strings.Replace(valid, `0123456789abcdef0123456789abcdef`, `../outside`, 1),
		valid + `{}`, strings.Repeat("x", 513),
	} {
		w := httptest.NewRecorder()
		s.ServeHTTP(w, demoRequest(s, "POST", "/api/hunt", raw))
		if w.Code != 400 {
			t.Fatal("malformed request admitted")
		}
	}
	w := httptest.NewRecorder()
	s.ServeHTTP(w, demoRequest(s, "POST", "/api/hunt", strings.Replace(valid, `"live":false`, `"live":true`, 1)))
	if w.Code != 409 || s.runs != 0 || s.liveRuns != 0 {
		t.Fatal("disabled live request reached execution or consumed budget")
	}
	s.runs = maxDemoRuns
	w = httptest.NewRecorder()
	s.ServeHTTP(w, demoRequest(s, "POST", "/api/hunt", valid))
	if w.Code != 429 {
		t.Fatal("total request budget bypassed")
	}
}

func TestHuntProgressCannotReadAnotherRunOrArbitraryImage(t *testing.T) {
	s := testDemoServer(t, false, nil)
	dir := t.TempDir()
	s.hunt = huntState{"0123456789abcdef0123456789abcdef", dir, "/runs/owned/"}
	for _, tc := range []struct {
		id, image string
		wantImage bool
	}{
		{s.hunt.id, "frame-2.png", true}, {"foreign", "frame-2.png", false}, {s.hunt.id, "../../outside.png", false},
	} {
		raw, _ := json.Marshal(map[string]string{"image": tc.image, "message": "Checked"})
		if err := os.WriteFile(filepath.Join(dir, "progress.json"), raw, 0600); err != nil {
			t.Fatal(err)
		}
		w := httptest.NewRecorder()
		s.ServeHTTP(w, demoRequest(s, "GET", "/api/hunt/progress?id="+tc.id, ""))
		if w.Code != 200 || strings.Contains(w.Body.String(), "/runs/owned/") != tc.wantImage {
			t.Fatal("progress escaped its run or image allowlist")
		}
	}
	r := demoRequest(s, "GET", "/api/hunt/progress?id="+s.hunt.id, "")
	r.Header.Del("X-Demo-Session")
	w := httptest.NewRecorder()
	s.ServeHTTP(w, r)
	if w.Code != 404 {
		t.Fatal("progress exposed without owning session")
	}
}
