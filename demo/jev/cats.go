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

func isCatScenario(s string) bool {
	return s == "cats" || s == "cat-decoys" || s == "dogs" || s == "mice" || s == "striped"
}
func validScenario(s string) bool { return s == "declined" || s == "success" || isCatScenario(s) }

func catFixture(scenario string) []byte {
	return bytes.Replace(catRoom, []byte(`data-scenario="cats"`), []byte(`data-scenario="`+scenario+`"`), 1)
}

func huntSubject(scenario string) string {
	switch scenario {
	case "dogs":
		return "living cartoon dogs with floppy ears; exclude toys and other objects"
	case "mice":
		return "living cartoon mice with round ears and tails; exclude wedges of cheese"
	case "striped":
		return "people wearing a red-and-white striped shirt AND a red-and-white hat, round glasses and blue trousers; exclude people with green hats or other shirt colours"
	default:
		return "living cartoon cats with whiskers and curved sleepy eyes; exclude cat-shaped cushions with X-shaped button eyes, stitched seams and fabric tags"
	}
}

func catContract(scenario string, images bool) jev.Contract {
	subject := huntSubject(scenario)
	criteria := map[string]string{"uncertain": "The requested figures cannot be counted reliably from this picture."}
	for i := 0; i <= 12; i++ {
		criteria[fmt.Sprintf("n%d", i)] = fmt.Sprintf("Exactly %d matching figures are visible: %s.", i, subject)
	}
	c := jev.Contract{Version: jev.Version, Task: "synthetic-picture-hunt", QuestionVersion: "v2", CriteriaVersion: "v2", Questions: map[string]jev.Question{
		"count":   {Type: "choice", Instructions: "How many matching figures can you see in `screenshot`? Find " + subject + ". Look across the entire picture. Count each matching figure once. Select uncertain if you cannot tell.", Criteria: criteria},
		"present": {Type: "noul", Instructions: "Is at least one matching figure visible in `screenshot`? Find " + subject + "."},
	}}
	if images {
		c.Questions["cat"] = jev.Question{Type: "box", Instructions: "Find one matching figure anywhere in `screenshot`: " + subject + ". Return the smallest box around its visible body and head. This is a proposed location, not an action."}
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
	if c.Scenario == "cat-decoys" || c.Scenario == "mice" {
		expected = 6
	}
	if c.Scenario == "striped" {
		expected = 1
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
