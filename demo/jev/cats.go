package main

import (
	"bytes"
	_ "embed"
	"fmt"
	"math"
	"strconv"
	"strings"

	"github.com/Quality-Max/9lives-runner/internal/jev"
)

//go:embed cats.html
var catsHTML string

//go:embed cat-room.html
var catRoom []byte

type catResult struct {
	ActualCount     int      `json:"actualCount"`
	GuessedCount    *int     `json:"guessedCount"`
	CountConfidence *float64 `json:"countConfidence"`
	HasCats         *float64 `json:"hasCats"`
	CountMatches    *bool    `json:"countMatches"`
	BoxHitsCat      *bool    `json:"boxHitsCat"`
}

func isCatScenario(s string) bool { return s == "cats" || s == "cat-decoys" }
func validScenario(s string) bool { return s == "declined" || s == "success" || isCatScenario(s) }

func catFixture(scenario string) []byte {
	return bytes.Replace(catRoom, []byte(`data-scenario="cats"`), []byte(`data-scenario="`+scenario+`"`), 1)
}

func catContract(images bool) jev.Contract {
	criteria := map[string]string{"uncertain": "The visible cats cannot be counted reliably from this picture."}
	for i := 0; i <= 12; i++ {
		criteria[fmt.Sprintf("n%d", i)] = fmt.Sprintf("Exactly %d living cartoon cats are visible, including partly hidden cats. Exclude cat-shaped cushions, toys and pictures.", i)
	}
	c := jev.Contract{Version: jev.Version, Task: "synthetic-cat-hunt", QuestionVersion: "v1", CriteriaVersion: "v1", Questions: map[string]jev.Question{
		"count":   {Type: "choice", Instructions: "How many living cartoon cats can you see in `screenshot`? Look across the entire room. Count each animal once, including partly hidden ones. Cat-shaped cushions have stitched seams and X-shaped button eyes: these are objects, not animals. Select uncertain if you cannot tell.", Criteria: criteria},
		"present": {Type: "noul", Instructions: "Is at least one living cartoon cat visible in `screenshot`? Exclude cat-shaped cushions with stitched seams and X-shaped button eyes."},
	}}
	if images {
		c.Questions["cat"] = jev.Question{Type: "box", Instructions: "Find one living cartoon cat anywhere in `screenshot`. Return the smallest box around its visible body, head and ears. Exclude furniture and cat-shaped cushions with stitched seams and X-shaped button eyes. This is a proposed location, not an action."}
	}
	return c
}

func compareCats(c capture, answers map[string]jev.Answer, predicted *rectangle) *catResult {
	r := &catResult{ActualCount: len(c.CatBoxes), CountConfidence: answers["count"].Confidence, HasCats: answers["present"].Noul}
	if count, err := strconv.Atoi(strings.TrimPrefix(answers["count"].Choice, "n")); err == nil && count >= 0 && count <= 12 {
		r.GuessedCount = &count
		matches := count == r.ActualCount
		r.CountMatches = &matches
	}
	if predicted != nil {
		hit := false
		for _, b := range c.CatBoxes {
			// Compare geometry only. This synthetic threshold is not a claim of
			// model accuracy, target identity or permission to click a location.
			x := math.Max(0, math.Min(predicted.X+predicted.Width, b.X+b.Width)-math.Max(predicted.X, b.X))
			y := math.Max(0, math.Min(predicted.Y+predicted.Height, b.Y+b.Height)-math.Max(predicted.Y, b.Y))
			intersection := x * y
			union := predicted.Width*predicted.Height + b.Width*b.Height - intersection
			if union > 0 && intersection/union >= 0.35 {
				hit = true
			}
		}
		r.BoxHitsCat = &hit
	}
	return r
}

func validCatCapture(c capture) bool {
	expected := 9
	if c.Scenario == "cat-decoys" {
		expected = 6
	}
	if !c.AssertionPassed || c.AssertionFailed || len(c.CatBoxes) != expected {
		return false
	}
	seen := map[rectangle]bool{}
	for _, b := range c.CatBoxes {
		if seen[b] {
			return false
		}
		seen[b] = true
		if math.IsNaN(b.X+b.Y+b.Width+b.Height) || math.IsInf(b.X+b.Y+b.Width+b.Height, 0) || b.X < 0 || b.Y < 0 || b.Width <= 0 || b.Height <= 0 || b.X+b.Width > 100 || b.Y+b.Height > 100 {
			return false
		}
	}
	return true
}

//go:embed cat-report.html
var catReportHTML string
