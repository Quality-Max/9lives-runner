package assessment

import (
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"sort"
)

// Screens are saved `9l inspect --android|--ios --format json` results: what
// the test IDs of an app's screens were when they were inspected. They are
// evidence like a requirement contract: the report names each by path and
// digest, and only test IDs and counts are read from them.
type Screens struct {
	screens []screen
}

type screen struct {
	evidence ScreenEvidence
	ids      map[string]int
}

// ScreenEvidence names one inspected screen a report was checked against.
type ScreenEvidence struct {
	Path     string   `json:"path"`
	SHA256   string   `json:"sha256"`
	Platform string   `json:"platform"`
	Apps     []string `json:"apps"`
	// TestIDs is the number of distinct test IDs the screen holds.
	TestIDs int `json:"testIds"`
	// TestIDsTruncated marks a screen with more test IDs than inspect keeps,
	// so an ID missing from it may still be on that screen.
	TestIDsTruncated bool `json:"testIdsTruncated,omitempty"`
}

// MaxScreens and MaxScreenBytes bound the inspected screens one assessment reads.
const (
	MaxScreens     = 64
	MaxScreenBytes = 4 << 20
)

var screenTestID = regexp.MustCompile(`^[A-Za-z0-9_][A-Za-z0-9_.-]{0,127}$`)

// LoadScreens validates saved inspect results, read by path through read.
func LoadScreens(paths []string, read func(string, int64) ([]byte, error)) (*Screens, error) {
	if len(paths) == 0 || len(paths) > MaxScreens {
		return nil, fmt.Errorf("screens: give 1 to %d saved inspect results", MaxScreens)
	}
	seen := map[string]bool{}
	s := &Screens{}
	for _, path := range paths {
		if seen[path] {
			continue
		}
		seen[path] = true
		data, err := read(path, MaxScreenBytes)
		if err != nil {
			return nil, fmt.Errorf("screen %s: %w", path, err)
		}
		var raw struct {
			Platform         string          `json:"platform"`
			Apps             []string        `json:"apps"`
			TestIDs          map[string]int  `json:"testIds"`
			TestIDsTruncated bool            `json:"testIdsTruncated"`
			Controls         json.RawMessage `json:"controls"`
		}
		if json.Unmarshal(data, &raw) != nil || (raw.Platform != "android" && raw.Platform != "ios") || raw.Controls == nil {
			return nil, fmt.Errorf("screen %s: not a `9l inspect --android` or `--ios` JSON result", path)
		}
		if raw.TestIDs == nil {
			return nil, fmt.Errorf("screen %s: has no testIds; inspect it again with this 9l", path)
		}
		if len(raw.Apps) > 8 || len(raw.TestIDs) > 2000 {
			return nil, fmt.Errorf("screen %s: exceeds the inspect result limits", path)
		}
		for _, app := range raw.Apps {
			if len(app) == 0 || len(app) > 200 {
				return nil, fmt.Errorf("screen %s: invalid app name", path)
			}
		}
		for id, count := range raw.TestIDs {
			if !screenTestID.MatchString(id) || count < 1 || count > 100000 {
				return nil, fmt.Errorf("screen %s: invalid test ID entry", path)
			}
		}
		apps := raw.Apps
		if apps == nil {
			apps = []string{}
		}
		s.screens = append(s.screens, screen{
			evidence: ScreenEvidence{Path: path, SHA256: digest(data), Platform: raw.Platform, Apps: apps, TestIDs: len(raw.TestIDs), TestIDsTruncated: raw.TestIDsTruncated},
			ids:      raw.TestIDs,
		})
	}
	if len(s.screens) == 0 {
		return nil, errors.New("screens: no saved inspect results")
	}
	return s, nil
}

// Evidence lists the screens in the order they were given.
func (s *Screens) Evidence() []ScreenEvidence {
	out := make([]ScreenEvidence, len(s.screens))
	for i, sc := range s.screens {
		out[i] = sc.evidence
	}
	return out
}

// observed reports whether any screen holds id, and the largest number of
// elements one screen carries it on.
func (s *Screens) observed(id string) (found bool, most int) {
	for _, sc := range s.screens {
		if n := sc.ids[id]; n > 0 {
			found = true
			if n > most {
				most = n
			}
		}
	}
	return found, most
}

func (s *Screens) truncated() bool {
	for _, sc := range s.screens {
		if sc.evidence.TestIDsTruncated {
			return true
		}
	}
	return false
}

// applyScreens checks each target test's test IDs against the screens. An ID
// no screen holds is informational: it may be on a screen that was not
// inspected. An ID a screen holds more than once is suspected: getByTestId
// refuses it as ambiguous while those elements are shown.
func applyScreens(report *Report, facts Facts, s *Screens) {
	report.Screens = s.Evidence()
	if s.truncated() {
		report.Limits = append(report.Limits, "screens: an inspected screen held more test IDs than inspect keeps; an ID reported as unobserved may still be on it.")
	}
	for i := range report.Tests {
		if i >= len(facts.Tests) {
			break
		}
		t := &report.Tests[i]
		ids := append([]TestID(nil), facts.Tests[i].TestIDs...)
		sort.SliceStable(ids, func(a, b int) bool { return ids[a].Line < ids[b].Line })
		for _, id := range ids {
			found, most := s.observed(id.ID)
			switch {
			case !found:
				t.Findings = append(t.Findings, Finding{Rule: "screen-test-id", Classification: "informational", Dimension: "engineeringQuality", Location: id.Location, Site: id.Site, Code: "unobserved-test-id",
					Message:    "No inspected screen shows this test ID; it may be on a screen that was not inspected, or missing from the app.",
					Suggestion: "Inspect the screen this step runs on with 9l inspect, or check the ID against the app's code."})
			case most > 1:
				t.Findings = append(t.Findings, Finding{Rule: "screen-test-id", Classification: "suspected", Dimension: "engineeringQuality", Location: id.Location, Site: id.Site, Code: "repeated-test-id",
					Message:    fmt.Sprintf("An inspected screen shows this test ID on %d elements; getByTestId refuses it as ambiguous while they are shown.", most),
					Suggestion: "Give each instance its own test ID, for example with the item's key as a suffix."})
				t.Dimensions["engineeringQuality"] = "concern"
			}
		}
	}
}
