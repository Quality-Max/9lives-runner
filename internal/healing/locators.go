package healing

import (
	"regexp"
	"strings"
	"unicode"
)

// editableLocator is one Playwright locator a native heal may edit: the CSS
// selector of page.locator(...), or the text argument of a single
// page.getBy*(...) call. For getByRole only the accessible name is editable;
// the role must stay the same.
type editableLocator struct {
	method string // "locator", or getByRole, getByText, ...
	role   string // getByRole only
	value  string // CSS selector, accessible name, text, label, ...
	exact  string // "", "true" or "false"
}

// getByMethods take one string, optionally followed by { exact: bool }.
var getByMethods = map[string]bool{"getByText": true, "getByLabel": true, "getByPlaceholder": true, "getByAltText": true, "getByTitle": true, "getByTestId": true}

var (
	terminalSequence = regexp.MustCompile(`\x1b\[[0-9;?]*[A-Za-z]`)
	waitingForGetBy  = regexp.MustCompile(`(?m)waiting for (getBy[A-Za-z]+\(.*\))[ \t]*$`)
)

// FailedLocator names the locator a failed Playwright action waited for. A
// single getBy* call in the call log is returned in canonical form, such as
// getByRole('button', { name: 'Sign in' }); otherwise it is ExtractSelector's
// CSS selector. Native healing uses this; the Python-parity Tier 1 request
// path keeps ExtractSelector.
func FailedLocator(errorMessage string) string {
	clean := terminalSequence.ReplaceAllString(errorMessage, "")
	if m := waitingForGetBy.FindStringSubmatch(clean); len(m) == 2 {
		if call, ok := parseLocatorExpression(m[1]); ok {
			return call.String()
		}
		return "" // a chained or regex getBy locator is not editable
	}
	return ExtractSelector(errorMessage, "")
}

// parseLocatorExpression reads a canonical getBy* expression; anything else is
// a CSS selector.
func parseLocatorExpression(expression string) (editableLocator, bool) {
	if !strings.HasPrefix(expression, "getBy") {
		return editableLocator{method: "locator", value: expression}, true
	}
	tokens, ok := readSource(expression, "playwright")
	if !ok || len(tokens) < 3 || tokens[1].text != "(" || tokens[1].pair != len(tokens)-1 {
		return editableLocator{}, false
	}
	return parseGetByArguments(tokens, 0)
}

// parseGetByArguments reads tokens[method] '(' ... ')' as a supported getBy*
// call with literal arguments only.
func parseGetByArguments(tokens []sourceToken, method int) (editableLocator, bool) {
	name, open := tokens[method].text, method+1
	if open >= len(tokens) || tokens[open].text != "(" || tokens[open].pair < 0 {
		return editableLocator{}, false
	}
	args := tokens[open+1 : tokens[open].pair]
	call := editableLocator{method: name}
	switch {
	case name == "getByRole":
		// 'role', { name: 'Name' [, exact: bool] } with properties in any order
		if len(args) < 3 || args[0].quote == 0 || args[1].text != "," || args[2].text != "{" {
			return editableLocator{}, false
		}
		call.role = args[0].value
		properties, ok := literalProperties(args[2:])
		if !ok || properties["name"] == nil || len(properties) > 2 || (len(properties) == 2 && properties["exact"] == nil) {
			return editableLocator{}, false
		}
		call.value = properties["name"].value
		if exact := properties["exact"]; exact != nil {
			call.exact = exact.text
		}
	case getByMethods[name]:
		if len(args) == 0 || args[0].quote == 0 {
			return editableLocator{}, false
		}
		call.value = args[0].value
		if len(args) > 1 {
			if args[1].text != "," || len(args) < 3 || args[2].text != "{" {
				return editableLocator{}, false
			}
			properties, ok := literalProperties(args[2:])
			if !ok || len(properties) != 1 || properties["exact"] == nil {
				return editableLocator{}, false
			}
			call.exact = properties["exact"].text
		}
	default:
		return editableLocator{}, false
	}
	if call.value == "" {
		return editableLocator{}, false
	}
	return call, true
}

// literalProperties reads `{ key: value, ... }` (optionally trailing commas)
// spanning all of tokens, where name is a string literal and exact a boolean.
func literalProperties(tokens []sourceToken) (map[string]*sourceToken, bool) {
	if len(tokens) < 2 || tokens[0].text != "{" || tokens[len(tokens)-1].text != "}" {
		return nil, false
	}
	properties := map[string]*sourceToken{}
	for i := 1; i < len(tokens)-1; {
		if i+2 >= len(tokens) || tokens[i+1].text != ":" {
			return nil, false
		}
		key, value := tokens[i].text, &tokens[i+2]
		switch {
		case key == "name" && value.quote != 0:
		case key == "exact" && (value.text == "true" || value.text == "false"):
		default:
			return nil, false
		}
		if properties[key] != nil {
			return nil, false
		}
		properties[key] = value
		i += 3
		if i < len(tokens)-1 {
			if tokens[i].text != "," {
				return nil, false
			}
			i++
		}
	}
	return properties, true
}

func jsQuote(value string) string { return "'" + escapeJSString(value, '\'') + "'" }

// String is the canonical form, which matches Playwright's call-log text.
func (call editableLocator) String() string {
	switch {
	case call.method == "locator":
		return call.value
	case call.method == "getByRole":
		options := "name: " + jsQuote(call.value)
		if call.exact != "" {
			options += ", exact: " + call.exact
		}
		return "getByRole(" + jsQuote(call.role) + ", { " + options + " })"
	default:
		suffix := ""
		if call.exact != "" {
			suffix = ", { exact: " + call.exact + " }"
		}
		return call.method + "(" + jsQuote(call.value) + suffix + ")"
	}
}

// sourceForm is how the locator reads in a spec, for refusal reasons.
func (call editableLocator) sourceForm() string {
	if call.method == "locator" {
		return "page.locator(" + quoteSelector(call.value) + ")"
	}
	return "page." + call.String()
}

// ariaLine reads one line of a Playwright ARIA snapshot: `- role "name" [...]`.
var ariaLine = regexp.MustCompile(`^\s*- ([a-z]+)(?: "((?:\\.|[^"\\])*)")?`)

// labelledRoles are the controls getByLabel finds by their accessible name.
var labelledRoles = map[string]bool{"textbox": true, "searchbox": true, "combobox": true, "checkbox": true, "radio": true, "spinbutton": true, "slider": true, "switch": true, "listbox": true}

// ariaAlternative re-finds a getByRole or getByLabel locator in an ARIA
// snapshot: the only candidate element of that role (or labelled control),
// else the only candidate whose name shares a word with the old name. It
// returns "" while the old name still matches, or when no single candidate
// is left. The candidate is unverified until the spec runs with it.
func ariaAlternative(call editableLocator, snapshot string) string {
	if call.method != "getByRole" && call.method != "getByLabel" {
		return ""
	}
	var names []string
	for _, line := range strings.Split(snapshot, "\n") {
		m := ariaLine.FindStringSubmatch(line)
		if len(m) != 3 || m[2] == "" {
			continue
		}
		role, name := m[1], strings.ReplaceAll(m[2], `\"`, `"`)
		if call.method == "getByRole" && role != call.role || call.method == "getByLabel" && !labelledRoles[role] {
			continue
		}
		if strings.Contains(strings.ToLower(name), strings.ToLower(call.value)) {
			return "" // the old name still matches; this is not a rename
		}
		if safeAttributeValue(name) && len(name) <= 200 && !strings.Contains(name, "\\") {
			names = append(names, name)
		}
	}
	if len(names) == 1 {
		return names[0]
	}
	var sharing []string
	for _, name := range names {
		if sharesWord(name, call.value) {
			sharing = append(sharing, name)
		}
	}
	if len(sharing) == 1 {
		return sharing[0]
	}
	return ""
}

func sharesWord(a, b string) bool {
	words := map[string]bool{}
	split := func(s string) []string {
		return strings.FieldsFunc(strings.ToLower(s), func(r rune) bool { return !unicode.IsLetter(r) && !unicode.IsDigit(r) })
	}
	for _, word := range split(a) {
		if len([]rune(word)) > 2 {
			words[word] = true
		}
	}
	for _, word := range split(b) {
		if words[word] {
			return true
		}
	}
	return false
}
