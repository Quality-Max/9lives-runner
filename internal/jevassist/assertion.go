package jevassist

import (
	"regexp"
	"strconv"
	"strings"
)

// AssertionComparison describes a failed Playwright comparison without
// carrying arbitrary assertion operands, selectors, or call-log text.
type AssertionComparison struct {
	Matcher  string `json:"matcher"`
	Kind     string `json:"kind"`
	Expected string `json:"expected,omitempty"`
	Observed string `json:"observed,omitempty"`
	Relation string `json:"relation"`
}

var (
	comparisonHeader = regexp.MustCompile(`^Error: expect(?:\.soft|\.poll)?\((received|locator)\)\.(toBe|toHaveText|toHaveCount|toBeVisible|toBeHidden)\((expected)?\)(?: failed| // Object\.is equality)?$`)
	comparisonNumber = regexp.MustCompile(`^-?(?:0|[1-9][0-9]*)(?:\.[0-9]+)?(?:[eE][+-]?[0-9]+)?$`)
	comparisonCount  = regexp.MustCompile(`^(?:0|[1-9][0-9]{0,8})$`)
)

func assertionComparison(message string) *AssertionComparison {
	lines := strings.SplitN(message, "\n", 7)
	if len(lines) < 4 || lines[1] != "" {
		return nil
	}
	header := comparisonHeader.FindStringSubmatch(lines[0])
	if header == nil {
		return nil
	}
	matcher := header[2]
	operands := lines[2:]
	if header[1] == "locator" {
		if len(operands) < 3 || !strings.HasPrefix(operands[0], "Locator:") {
			return nil
		}
		operands = operands[1:]
	} else if matcher != "toBe" {
		return nil
	}
	expected, expectOK := strings.CutPrefix(operands[0], "Expected:")
	observed, receiveOK := strings.CutPrefix(operands[1], "Received:")
	if !expectOK || !receiveOK {
		return nil
	}
	expected, observed = strings.TrimSpace(expected), strings.TrimSpace(observed)
	if expected == observed {
		return nil
	}
	result := &AssertionComparison{Matcher: matcher, Relation: "different"}
	switch {
	case matcher == "toBeVisible" && expected == "visible" && observed == "hidden",
		matcher == "toBeHidden" && expected == "hidden" && observed == "visible":
		result.Kind, result.Expected, result.Observed = "visibility", expected, observed
	case matcher == "toHaveCount" && comparisonCount.MatchString(expected) && comparisonCount.MatchString(observed):
		result.Kind, result.Expected, result.Observed = "element-count", expected, observed
	case matcher == "toBe" && booleanLiteral(expected) && booleanLiteral(observed):
		result.Kind, result.Expected, result.Observed = "boolean", expected, observed
	case matcher == "toBe" && comparisonNumber.MatchString(expected) && comparisonNumber.MatchString(observed):
		result.Kind = "number"
	case (matcher == "toBe" || matcher == "toHaveText") && quotedString(expected) && quotedString(observed):
		result.Kind = "text"
	default:
		return nil
	}
	return result
}

func booleanLiteral(value string) bool {
	return value == "true" || value == "false"
}

func quotedString(value string) bool {
	if !strings.HasPrefix(value, `"`) {
		return false
	}
	_, err := strconv.Unquote(value)
	return err == nil
}
