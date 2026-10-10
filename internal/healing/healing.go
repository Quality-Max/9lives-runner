// Package healing implements the bounded, offline Tier 1 proposal engine.
package healing

import (
	"fmt"
	"regexp"
	"strings"
	"unicode"
)

const Version = 1

type Request struct {
	Version        int    `json:"version"`
	Framework      string `json:"framework"`
	ErrorMessage   string `json:"errorMessage"`
	StackTrace     string `json:"stackTrace,omitempty"`
	FailureType    string `json:"failureType,omitempty"`
	FailedSelector string `json:"failedSelector,omitempty"`
	TestCode       string `json:"testCode"`
	PageSnapshot   string `json:"pageSnapshot,omitempty"`
	// AriaSnapshot is Playwright's ARIA snapshot of the page at failure
	// (roles and accessible names), used to re-find a getBy* locator.
	AriaSnapshot         string `json:"ariaSnapshot,omitempty"`
	AllowAssertionChange bool   `json:"allowAssertionChange,omitempty"`
}

type Response struct {
	Version          int            `json:"version"`
	FailureType      string         `json:"failureType"`
	FailedSelector   string         `json:"failedSelector,omitempty"`
	SelectedTier     string         `json:"selectedTier"`
	AttemptedTier    string         `json:"attemptedTier"`
	Decision         string         `json:"decision"`
	ProposedCode     string         `json:"proposedCode,omitempty"`
	Changes          []string       `json:"changes"`
	RequiresApproval bool           `json:"requiresApproval"`
	Apply            bool           `json:"apply"`
	Confidence       float64        `json:"confidence"`
	Metadata         map[string]any `json:"metadata"`
	Reason           string         `json:"reason,omitempty"`
}

var (
	quoted       = `(?:'((?:\\.|[^'])*)'|"((?:\\.|[^"])*)")`
	waitingFor   = regexp.MustCompile(`(?i)waiting for (?:locator|selector)\s*\(?` + quoted + `\)?`)
	locatorCall  = regexp.MustCompile(`(?i)(?:locator|page\.locator|getByText|getByTestId|getByLabel|getByPlaceholder|getByTitle)\(` + quoted)
	cyExpected   = regexp.MustCompile("(?i)Expected to find (?:element|content):\\s*`([^`]+)`")
	cyCall       = regexp.MustCompile(`(?i)cy\.(?:get|find|contains)\(` + quoted)
	seleniumJSON = regexp.MustCompile(`"method"\s*:\s*"[^"]+"\s*,\s*"selector"\s*:\s*"((?:\\.|[^"\\])*)"`)
	seleniumBy   = regexp.MustCompile(`By\.\w+\s*,\s*` + quoted)
)

func Classify(errorMessage, stack string) string {
	s := strings.ToLower(errorMessage + " " + stack)
	for _, pattern := range []string{"unexpected.*page", "unexpected.*url", "step.*missing", "element.*removed", "flow.*changed"} {
		if matches(pattern, s) {
			return "flow_changed"
		}
	}
	for _, pattern := range []string{"navigation.*failed", "net::err", "page.*crash", "context.*closed", "target.*closed"} {
		if matches(pattern, s) {
			return "navigation_failed"
		}
	}
	// Assertions are never modified by the native offline tier.  In particular,
	// Playwright call logs often include a "waiting for locator" line after the
	// assertion failure.
	for _, pattern := range []string{"assertionerror", "expect.*to[a-z]+", "expected.*to be", "expected.*to have", "assertion failed"} {
		if matches(pattern, s) {
			return "assertion_failed"
		}
	}
	for _, pattern := range []string{"element.*not visible", "element.*hidden", "display.*none", "visibility.*hidden", "not interactable", "outside viewport"} {
		if matches(pattern, s) {
			return "element_not_visible"
		}
	}
	for _, pattern := range []string{"locator.*not found", "element.*not found", "selector.*not found", "timeouterror.*locator", "waiting for selector", "waiting for locator", "no element matching", "could not find element", "strict mode violation", "expected to find element", "nosuchelementexception"} {
		if matches(pattern, s) {
			return "locator_not_found"
		}
	}
	for _, pattern := range []string{"timeouterror", "timeout.*exceeded", "waiting for.*timed out"} {
		if matches(pattern, s) {
			return "locator_timeout"
		}
	}
	if strings.Contains(s, "syntax") || strings.Contains(s, "parse") {
		return "syntax_error"
	}
	return "unknown"
}

func matches(pattern, text string) bool {
	return regexp.MustCompile(`(?i)` + pattern).MatchString(text)
}

func ExtractSelector(errorMessage, stack string) string {
	s := errorMessage + "\n" + stack
	for _, re := range []*regexp.Regexp{waitingFor, cyExpected, seleniumJSON, locatorCall, cyCall, seleniumBy} {
		m := re.FindStringSubmatch(s)
		if len(m) == 0 {
			continue
		}
		if re == cyExpected || re == seleniumJSON {
			return unescape(m[1])
		}
		if m[1] != "" {
			return unescape(m[1])
		}
		return unescape(m[2])
	}
	return ""
}

func unescape(value string) string {
	return strings.ReplaceAll(strings.ReplaceAll(strings.ReplaceAll(value, `\'`, `'`), `\"`, `"`), `\\`, `\`)
}

func selectedTier(failure string, request Request) string {
	switch failure {
	case "assertion_failed", "navigation_failed", "flow_changed", "network_error":
		return "tier3_human"
	case "syntax_error":
		return "no_healing"
	case "element_not_visible":
		if strings.Contains(strings.ToLower(request.ErrorMessage), "scroll") || strings.Contains(strings.ToLower(request.ErrorMessage), "viewport") {
			return "tier1_auto"
		}
		return "tier2_ai_suggest"
	case "locator_not_found", "locator_timeout":
		selector := request.FailedSelector
		if selector == "" {
			selector = ExtractSelector(request.ErrorMessage, request.StackTrace)
		}
		if selector != "" && (strings.Contains(strings.ToLower(request.TestCode), "// fallback:") || strings.Contains(selector, "data-testid") || strategyCanFindAlternative(selector, request.PageSnapshot)) {
			return "tier1_auto"
		}
		return "tier2_ai_suggest"
	default:
		return "tier2_ai_suggest"
	}
}

func Heal(request Request) Response {
	selector := request.FailedSelector
	if selector == "" {
		selector = ExtractSelector(request.ErrorMessage, request.StackTrace)
	}
	classified := Classify(request.ErrorMessage, request.StackTrace)
	failure := request.FailureType
	if failure == "" || classified == "assertion_failed" {
		failure = classified
	}
	if matches, ok := sourceLocators(request.TestCode, selector, request.Framework); ok {
		for _, match := range matches {
			if assertionOwnsMatch(request.TestCode, match, request.Framework) {
				failure = "assertion_failed"
			}
		}
	}
	response := Response{Version: Version, FailureType: failure, FailedSelector: selector, SelectedTier: selectedTier(failure, Request{FailedSelector: selector, ErrorMessage: request.ErrorMessage, StackTrace: request.StackTrace, TestCode: request.TestCode, PageSnapshot: request.PageSnapshot}), AttemptedTier: "tier1_auto", Decision: "refuse", Changes: []string{}, Metadata: map[string]any{"engine": "native-tier1", "provenance": "unverified"}}
	if failure == "assertion_failed" {
		response.Reason = "assertion failures require human review"
		return response
	}
	if failure != "locator_not_found" && failure != "locator_timeout" && failure != "element_not_visible" {
		response.Reason = "failure is outside offline Tier 1"
		return response
	}
	if selector == "" || request.TestCode == "" {
		response.Reason = "missing selector or test code"
		return response
	}
	if call, ok := parseLocatorExpression(selector); ok && call.method != "locator" {
		// CSS strategies do not apply to a getBy* locator; only its name or
		// text can be re-found, from the ARIA snapshot.
		if name := ariaAlternative(call, request.AriaSnapshot); name != "" {
			if code, ok := replaceSelector(request.TestCode, selector, name, request.Framework); ok {
				return proposal(response, code, selector, name, "aria-snapshot", .8, "re-found accessible name")
			}
			response.Reason = "locator is absent or ambiguous in the source"
			return response
		}
		if request.AriaSnapshot == "" {
			response.Reason = "re-finding a getBy locator offline needs the page's ARIA snapshot"
		} else {
			response.Reason = "the ARIA snapshot has no single renamed candidate"
		}
		return response
	}
	if strings.HasPrefix(selector, "text=") {
		if request.Framework != "playwright" {
			response.Reason = "exact text selectors require Playwright"
			return response
		}
		text := identifier(selector, `text=['"]([^'"]+)['"]`, "text").value
		if text == "" || htmlExactText(request.PageSnapshot, text) == "" {
			response.Reason = "no unique supported exact text anchor"
			return response
		}
	}
	if replacement, anchor := alternative(selector, request.PageSnapshot); replacement != "" && replacement != selector {
		if code, ok := replaceSelector(request.TestCode, selector, replacement, request.Framework); ok {
			return proposal(response, code, selector, replacement, anchor, .85, "re-found selector")
		}
		response.Reason = "selector literal is absent or ambiguous"
		return response
	}
	if unsafeAlternative(selector, request.PageSnapshot) {
		response.Reason = "snapshot alternative is not a safe CSS selector"
		return response
	}
	if request.PageSnapshot == "" {
		if transformed := transform(selector); transformed != "" {
			if code, ok := replaceSelector(request.TestCode, selector, transformed, request.Framework); ok {
				return proposal(response, code, selector, transformed, "transformation", .7, "transformed selector")
			}
			response.Reason = "selector literal is absent or ambiguous"
			return response
		}
	}
	if request.Framework == "playwright" && timing(request.ErrorMessage) {
		if code, ok := addWait(request.TestCode, selector); ok {
			return proposal(response, code, selector, selector, "timing", .75, "added visible wait")
		}
	}
	response.Reason = "no safe Tier 1 repair found"
	return response
}

func strategyCanFindAlternative(selector, snapshot string) bool {
	if snapshot == "" {
		return false
	}
	for _, pattern := range []string{`id=["']([^"']+)["']`, `text=["']([^"']+)["']`} {
		m := regexp.MustCompile(pattern).FindStringSubmatch(selector)
		if len(m) == 2 && strings.Contains(strings.ToLower(snapshot), strings.ToLower(m[1])) {
			return true
		}
	}
	return false
}

func proposal(r Response, code, old, next, anchor string, confidence float64, change string) Response {
	if code == "" {
		r.Reason = "empty repair refused"
		return r
	}
	r.Decision, r.ProposedCode, r.RequiresApproval, r.Apply, r.Confidence = "propose", code, true, false, confidence
	r.Changes = []string{change}
	r.Metadata["anchor"], r.Metadata["oldSelector"], r.Metadata["newSelector"] = anchor, old, next
	return r
}

func alternative(selector, html string) (string, string) {
	if html == "" {
		return "", ""
	}
	for _, item := range []struct{ kind, value string }{identifier(selector, `\[data-testid=['"]([^'"]+)['"]\]`, "testid"), identifier(selector, `#([A-Za-z][\w-]*)`, "id"), identifier(selector, `\[aria-label=['"]([^'"]+)['"]\]`, "aria-label"), identifier(selector, `text=['"]([^'"]+)['"]`, "text"), identifier(selector, `\.([A-Za-z][\w-]*)`, "class")} {
		if item.value == "" {
			continue
		}
		switch item.kind {
		case "testid":
			if v := attribute(html, "data-testid", item.value); v != "" {
				return "[data-testid='" + cssString(v) + "']", item.kind
			}
		case "id":
			if v := attribute(html, "id", item.value); v != "" {
				return "#" + v, item.kind
			}
		case "aria-label":
			if v := attribute(html, "aria-label", item.value); v != "" {
				return "[aria-label='" + cssString(v) + "']", item.kind
			}
		case "text":
			if v := htmlExactText(html, item.value); v != "" {
				return "text='" + cssString(v) + "'", item.kind
			}
		case "class":
			if v := className(html, item.value); v != "" {
				return "." + v, item.kind
			}
		}
	}
	return "", ""
}

func identifier(selector, pattern, kind string) struct{ kind, value string } {
	m := regexp.MustCompile(`^(?:` + pattern + `)$`).FindStringSubmatch(selector)
	if len(m) == 2 {
		return struct{ kind, value string }{kind, m[1]}
	}
	return struct{ kind, value string }{}
}
func attribute(html, name, contains string) string {
	var candidate string
	count := 0
	for _, attrs := range htmlElementAttributes(html) {
		if value, ok := attrs[strings.ToLower(name)]; ok && strings.Contains(strings.ToLower(value), strings.ToLower(contains)) && (name != "id" || validCSSIdentifier(value)) && safeAttributeValue(value) {
			candidate = value
			count++
		}
	}
	if count == 1 {
		return candidate
	}
	return ""
}

func safeAttributeValue(value string) bool {
	if value == "" {
		return false
	}
	for _, r := range value {
		if unicode.IsControl(r) {
			return false
		}
	}
	return true
}
func cssString(value string) string {
	return strings.ReplaceAll(strings.ReplaceAll(value, "\\", "\\\\"), "'", "\\'")
}

func validCSSIdentifier(value string) bool {
	if value == "" {
		return false
	}
	for index, r := range value {
		if index == 0 && !(unicode.IsLetter(r) || r == '_') {
			return false
		}
		if unicode.IsControl(r) || unicode.IsSpace(r) || !(unicode.IsLetter(r) || unicode.IsDigit(r) || r == '_' || r == '-') {
			return false
		}
	}
	return true
}

func unsafeAlternative(selector, html string) bool {
	match := regexp.MustCompile(`^#([A-Za-z][\w-]*)$`).FindStringSubmatch(selector)
	if len(match) != 2 {
		return false
	}
	for _, attrs := range htmlElementAttributes(html) {
		if candidate, ok := attrs["id"]; ok && strings.Contains(candidate, match[1]) && !validCSSIdentifier(candidate) {
			return true
		}
	}
	return false
}

// A class candidate must resolve to exactly one element, like the attribute
// and exact-text paths. The first case-insensitive token match otherwise
// proposes a selector that still violates Playwright strict mode.
func className(html, expected string) string {
	var candidate string
	count := 0
	for _, attrs := range htmlElementAttributes(html) {
		for _, name := range strings.Fields(attrs["class"]) {
			if strings.EqualFold(name, expected) {
				if count == 0 {
					candidate = name
				}
				count++
				break // Duplicate tokens on one element count once.
			}
		}
	}
	if count == 1 {
		return candidate
	}
	return ""
}
func transform(selector string) string {
	if m := regexp.MustCompile(`^#([A-Za-z][\w-]*)$`).FindStringSubmatch(selector); len(m) == 2 {
		return `[id="` + m[1] + `"]`
	}
	if m := regexp.MustCompile(`^\.([A-Za-z][\w]*)-([A-Za-z][\w]*)$`).FindStringSubmatch(selector); len(m) == 3 {
		return `[class*="` + m[1] + `"]`
	}
	if m := regexp.MustCompile(`^\[data-testid="([^"]+)"\]$`).FindStringSubmatch(selector); len(m) == 2 {
		if validCSSIdentifier(m[1]) {
			return `[data-testid*="` + m[1] + `"]`
		}
	}
	return ""
}

// EquivalentSelectors reports a Tier 1 transformation that selects exactly
// the same elements, such as #save and [id="save"] for a valid CSS
// identifier. Verifying it can never fix a missing element, so native healing
// skips it; the Python-parity tier1 response still reports it.
func EquivalentSelectors(old, next string) bool {
	m := regexp.MustCompile(`^#([A-Za-z][\w-]*)$`).FindStringSubmatch(old)
	return len(m) == 2 && validCSSIdentifier(m[1]) && next == `[id="`+m[1]+`"]`
}

func timing(message string) bool {
	s := strings.ToLower(message)
	return strings.Contains(s, "timeout") || strings.Contains(s, "waiting") || strings.Contains(s, "not visible") || strings.Contains(s, "viewport")
}

func Validate(request Request) error {
	if request.Version != Version {
		return fmt.Errorf("unsupported version")
	}
	if request.Framework == "" {
		return fmt.Errorf("framework is required")
	}
	if len(request.TestCode) > 1<<20 || len(request.PageSnapshot) > 1<<20 || len(request.AriaSnapshot) > 1<<20 || len(request.ErrorMessage) > 1<<20 || len(request.StackTrace) > 1<<20 {
		return fmt.Errorf("request exceeds size limit")
	}
	switch request.Framework {
	case "playwright", "cypress", "selenium":
	default:
		return fmt.Errorf("unsupported framework")
	}
	if request.FailureType != "" {
		switch request.FailureType {
		case "locator_not_found", "locator_timeout", "element_not_visible", "assertion_failed", "navigation_failed", "flow_changed", "network_error", "syntax_error", "unknown":
		default:
			return fmt.Errorf("unsupported failure type")
		}
	}
	return nil
}
