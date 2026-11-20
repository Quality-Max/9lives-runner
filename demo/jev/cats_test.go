package main

import (
	"context"
	"math"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Quality-Max/9lives-runner/internal/jev"
)

func TestCatGuessesAreComparedWithoutInventingAnswers(t *testing.T) {
	c := capture{CatBoxes: []rectangle{{10, 20, 5, 8}, {70, 60, 7, 10}}}
	for _, tc := range []struct {
		name, choice string
		box          *rectangle
		count, hit   *bool
	}{
		{"matching", "n2", &rectangle{10, 20, 5, 8}, boolPtr(true), boolPtr(true)},
		{"wrong count and decoy", "n3", &rectangle{40, 40, 5, 8}, boolPtr(false), boolPtr(false)},
		{"whole room is not a cat", "n2", &rectangle{0, 0, 100, 100}, boolPtr(true), boolPtr(false)},
		{"uncertain", "uncertain", nil, nil, nil},
		{"offline", "", nil, nil, nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := compareCats(c, map[string]jev.Answer{"count": {Choice: tc.choice}}, tc.box)
			if r.ActualCount != 2 || !sameBool(r.CountMatches, tc.count) || !sameBool(r.BoxHitsCat, tc.hit) {
				t.Fatal("guess comparison lost uncertainty or accepted wrong geometry")
			}
		})
	}
}

func boolPtr(b bool) *bool     { return &b }
func sameBool(a, b *bool) bool { return a == nil && b == nil || a != nil && b != nil && *a == *b }

func TestCatCaptureRejectsAbsentAndInvalidTargetEvidence(t *testing.T) {
	c := capture{Scenario: "cats", AssertionPassed: true, CatBoxes: make([]rectangle, 9)}
	for i := range c.CatBoxes {
		c.CatBoxes[i] = rectangle{10, float64(i) * 10, 3, 4}
	}
	if !validCatCapture(c) {
		t.Fatal("valid targets rejected")
	}
	c.CatBoxes = c.CatBoxes[:6]
	if validCatCapture(c) {
		t.Fatal("missing targets accepted")
	}
	c.Scenario = "cat-decoys"
	if !validCatCapture(c) {
		t.Fatal("decoy targets rejected")
	}
	saved := c.CatBoxes[0]
	c.CatBoxes[0] = c.CatBoxes[1]
	if validCatCapture(c) {
		t.Fatal("duplicate target evidence accepted")
	}
	c.CatBoxes[0] = saved
	for _, bad := range []rectangle{{99, 20, 5, 8}, {0, 0, 0, 2}, {math.NaN(), 0, 1, 2}} {
		c.CatBoxes[0] = bad
		if validCatCapture(c) {
			t.Fatal("invalid target rectangle accepted")
		}
	}
}

func TestCatRoutesAndImageGate(t *testing.T) {
	calls := 0
	s := testDemoServer(t, true, func(_ context.Context, c jev.Config, scenario string) (string, report, error) {
		calls++
		if c.Enabled || scenario != "cat-decoys" {
			t.Fatal("offline scene inherited live mode")
		}
		return t.TempDir(), report{Cats: &catResult{ActualCount: 6}}, nil
	})
	for _, path := range []string{"/cats", "/shop?scenario=cats", "/shop?scenario=cat-decoys"} {
		w := httptest.NewRecorder()
		s.ServeHTTP(w, demoRequest(s, "GET", path, ""))
		if w.Code != 200 || strings.Contains(w.Body.String(), "__DEMO_SESSION__") {
			t.Fatal("cat page unavailable")
		}
	}
	w := httptest.NewRecorder()
	s.ServeHTTP(w, demoRequest(s, "POST", "/api/run", `{"scenario":"cats","live":true}`))
	if w.Code != 409 || calls != 0 || s.runs != 0 || s.liveRuns != 0 {
		t.Fatal("text-only cat request reached executor or consumed budget")
	}
	w = httptest.NewRecorder()
	s.ServeHTTP(w, demoRequest(s, "POST", "/api/run", `{"scenario":"cat-decoys","live":false}`))
	if w.Code != 200 || calls != 1 || !strings.Contains(w.Body.String(), `"actualCount":6`) {
		t.Fatal("offline cat check unavailable")
	}
}

func TestCatReportExcludesAnswerMapAndRawObservation(t *testing.T) {
	dir := t.TempDir()
	n := 0
	r := report{Cats: &catResult{ActualCount: 9, GuessedCount: &n}, Capture: capture{Snapshot: "private-observation-sentinel", CatBoxes: []rectangle{{12.345, 23.456, 5, 6}}}}
	if err := writeReport(dir, r); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"index.html", "record.json"} {
		raw, err := os.ReadFile(filepath.Join(dir, name))
		if err != nil || strings.Contains(string(raw), "private-observation-sentinel") || strings.Contains(string(raw), "12.345") {
			t.Fatal("report leaked model state or target map")
		}
	}
}
