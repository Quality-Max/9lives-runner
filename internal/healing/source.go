package healing

import "strings"

// The source reader is deliberately a conservative lexical subset, not a JS or
// Python compiler. It reads the WHOLE source before returning any editable span.
// Unsupported/interpolated literals and unbalanced input fail closed. In
// particular, no locator text inside a comment/string can become source code.
type sourceToken struct {
	text, value string
	start, end  int
	quote       byte
	pair        int
}

func readSource(code, framework string) ([]sourceToken, bool) {
	var tokens []sourceToken
	var stack []int
	python := framework == "selenium"
	for i := 0; i < len(code); {
		c := code[i]
		if strings.ContainsRune(" \t\r\n", rune(c)) {
			i++
			continue
		}
		if python && c == '#' || !python && strings.HasPrefix(code[i:], "//") {
			if n := strings.IndexByte(code[i:], '\n'); n >= 0 {
				i += n + 1
			} else {
				i = len(code)
			}
			continue
		}
		if !python && strings.HasPrefix(code[i:], "/*") {
			n := strings.Index(code[i+2:], "*/")
			if n < 0 {
				return nil, false
			}
			i += n + 4
			continue
		}
		if c == '\\' {
			return nil, false
		} // Escaped JS identifiers and Python continuations can hide assertions.
		if !python && c == '<' && !comparisonOperand(tokens) {
			return nil, false // JSX/type-angle syntax is outside this source subset.
		}
		start := i
		if c == '\'' || c == '"' || !python && c == '`' {
			triple := python && strings.HasPrefix(code[i:], strings.Repeat(string(c), 3))
			width := 1
			if triple {
				width = 3
			}
			i += width
			var value strings.Builder
			closed := false
			for i < len(code) {
				if strings.HasPrefix(code[i:], strings.Repeat(string(c), width)) {
					i += width
					closed = true
					break
				}
				if c == '`' && strings.HasPrefix(code[i:], "${") {
					return nil, false
				}
				if !triple && c != '`' && (code[i] == '\n' || code[i] == '\r') {
					return nil, false
				}
				if code[i] == '\\' {
					i++
					if i >= len(code) {
						return nil, false
					}
					switch code[i] {
					case '\\', '\'', '"', '`':
						value.WriteByte(code[i])
					case 'n':
						value.WriteByte('\n')
					case 'r':
						value.WriteByte('\r')
					case 't':
						value.WriteByte('\t')
					default:
						return nil, false // Unknown runtime escape semantics.
					}
					i++
					continue
				}
				value.WriteByte(code[i])
				i++
			}
			if !closed {
				return nil, false
			}
			quote := c
			if triple {
				quote = 0
			}
			tokens = append(tokens, sourceToken{text: "literal", value: value.String(), start: start, end: i, quote: quote, pair: -1})
			continue
		}
		if !python && c == '/' {
			// A slash token outside a comment is accepted only as a bounded regex
			// literal. Division and ambiguous slash grammar are refused.
			i++
			class, closed := false, false
			for i < len(code) && code[i] != '\n' && code[i] != '\r' {
				if code[i] == '\\' {
					i += 2
					continue
				}
				if code[i] == '[' {
					class = true
				}
				if code[i] == ']' {
					class = false
				}
				if code[i] == '/' && !class {
					i++
					closed = true
					break
				}
				i++
			}
			if !closed {
				return nil, false
			}
			for i < len(code) && isIdentifierByte(code[i]) {
				i++
			}
			tokens = append(tokens, sourceToken{text: "regex", start: start, end: i, pair: -1})
			continue
		}
		if isIdentifierByte(c) {
			for i < len(code) && isIdentifierByte(code[i]) {
				i++
			}
			// Python prefixed strings are opaque; refusing the whole file avoids
			// treating f-string interpolation or raw escapes as an executable call.
			if python && i < len(code) && (code[i] == '\'' || code[i] == '"') {
				return nil, false
			}
			tokens = append(tokens, sourceToken{text: code[start:i], start: start, end: i, pair: -1})
			continue
		}
		i++
		text := code[start:i]
		if c == '=' && i < len(code) && code[i] == '>' {
			i++
			text = "=>"
		}
		tokens = append(tokens, sourceToken{text: text, start: start, end: i, pair: -1})
		index := len(tokens) - 1
		if strings.Contains("([{", text) {
			stack = append(stack, index)
		}
		if strings.Contains(")]}", text) {
			if len(stack) == 0 {
				return nil, false
			}
			open := stack[len(stack)-1]
			if !((tokens[open].text == "(" && text == ")") || (tokens[open].text == "[" && text == "]") || (tokens[open].text == "{" && text == "}")) {
				return nil, false
			}
			tokens[open].pair = index
			tokens[index].pair = open
			stack = stack[:len(stack)-1]
		}
	}
	return tokens, len(stack) == 0
}

type literalMatch struct {
	start, end    int
	quote         byte
	lineStart     int
	awaitPrefix   bool
	call, literal int
	// callEnd is the locator call's closing parenthesis; an action follows it.
	callEnd int
	// css is false for a getBy* locator, whose editable literal is a name or
	// text rather than a selector.
	css bool
	// shorthand marks page.<action>('<selector>', ...), where the locator call
	// is the action itself.
	shorthand bool
	tokens    []sourceToken
}

// directActions are the Playwright actions a native heal may repair.
var directActions = map[string]bool{"click": true, "fill": true, "check": true, "uncheck": true, "hover": true, "press": true, "focus": true, "dblclick": true, "selectOption": true}

// actionCall returns the action's name and the index of its opening
// parenthesis: `.action(` after the locator call, or the page shorthand.
func actionCall(t []sourceToken, m literalMatch) (string, int, bool) {
	if m.shorthand {
		return t[m.call+2].text, m.call + 3, true
	}
	a := m.callEnd + 1
	if a+2 >= len(t) || t[a].text != "." || t[a+2].text != "(" || t[a+2].pair < 0 {
		return "", 0, false
	}
	return t[a+1].text, a + 2, true
}

func sourceLocators(code, old, framework string) ([]literalMatch, bool) {
	tokens, ok := readSource(code, framework)
	if !ok {
		return nil, false
	}
	var want editableLocator
	if framework != "selenium" && framework != "cypress" {
		parsed, ok := parseLocatorExpression(old)
		if !ok {
			return nil, true
		}
		want = parsed
	}
	var found []literalMatch
	for i := 0; i < len(tokens); i++ {
		if i > 0 && (tokens[i-1].text == "." || tokens[i-1].text == "function") {
			continue
		}
		if want.method != "" && want.method != "locator" {
			// page.getBy*(...) with the same method, role and options; only
			// its name or text literal is the editable span.
			if tokens[i].text != "page" || i+3 >= len(tokens) || tokens[i+1].text != "." || tokens[i+2].text != want.method {
				continue
			}
			call, ok := parseGetByArguments(tokens, i+2)
			if !ok || call.role != want.role || call.exact != want.exact || call.value != want.value {
				continue
			}
			literal := i + 4 // getByText('value' ...
			if want.method == "getByRole" {
				for j := i + 4; j < tokens[i+3].pair; j++ {
					if tokens[j].text == "name" && tokens[j+1].text == ":" {
						literal = j + 2
						break
					}
				}
			}
			arg := tokens[literal]
			lineStart := strings.LastIndex(code[:tokens[i].start], "\n") + 1
			found = append(found, literalMatch{start: arg.start + 1, end: arg.end - 1, quote: arg.quote, lineStart: lineStart, awaitPrefix: i > 0 && tokens[i-1].text == "await", call: i, literal: literal, callEnd: tokens[i+3].pair, tokens: tokens})
			continue
		}
		var pattern []string
		switch {
		case framework == "selenium":
			pattern = []string{"driver", ".", "find_element", "(", "By", ".", "CSS_SELECTOR", ","}
		case framework == "cypress":
			if i+2 < len(tokens) && (tokens[i+2].text == "get" || tokens[i+2].text == "find") {
				pattern = []string{"cy", ".", tokens[i+2].text, "("}
			}
		default:
			if tokens[i].text == "locator" {
				pattern = []string{"locator", "("}
			} else if tokens[i].text == "page" && i+4 < len(tokens) && tokens[i+1].text == "." && directActions[tokens[i+2].text] && tokens[i+3].text == "(" && tokens[i+3].pair > i+4 {
				// page.fill('<selector>', value): the selector is the first argument.
				arg := tokens[i+4]
				if arg.quote == 0 || arg.value != old || (tokens[i+5].text != "," && tokens[i+5].text != ")") {
					continue
				}
				lineStart := strings.LastIndex(code[:tokens[i].start], "\n") + 1
				found = append(found, literalMatch{start: arg.start + 1, end: arg.end - 1, quote: arg.quote, lineStart: lineStart, awaitPrefix: i > 0 && tokens[i-1].text == "await", call: i, literal: i + 4, callEnd: tokens[i+3].pair, css: true, shorthand: true, tokens: tokens})
				continue
			} else {
				pattern = []string{"page", ".", "locator", "("}
			}
		}
		if len(pattern) == 0 || i+len(pattern)+1 >= len(tokens) {
			continue
		}
		matches := true
		for j, text := range pattern {
			if tokens[i+j].text != text {
				matches = false
				break
			}
		}
		if !matches {
			continue
		}
		literal := i + len(pattern)
		arg := tokens[literal]
		if arg.quote == 0 || arg.value != old || tokens[literal+1].text != ")" {
			continue
		}
		lineStart := strings.LastIndex(code[:tokens[i].start], "\n") + 1
		found = append(found, literalMatch{start: arg.start + 1, end: arg.end - 1, quote: arg.quote, lineStart: lineStart, awaitPrefix: i > 0 && tokens[i-1].text == "await", call: i, literal: literal, callEnd: literal + 1, css: true, tokens: tokens})
	}
	return found, true
}

func assertionOwnsMatch(code string, m literalMatch, framework string) bool {
	for i, token := range m.tokens {
		if token.text == "(" && token.pair > m.literal && i < m.call && assertionCallee(m.tokens, i) {
			return true
		}
	}
	if framework == "selenium" && pythonAssertionOwnsMatch(code, m) {
		return true
	}
	// Cypress assertions in the same chain are behavioral assertions too.
	if framework == "cypress" {
		for i := m.literal + 1; i < len(m.tokens); i++ {
			if m.tokens[i].text == ";" || m.tokens[i].text == "}" {
				break
			}
			if m.tokens[i].text == "should" || m.tokens[i].text == "and" {
				return true
			}
		}
	}
	return false
}

func assertionCallee(tokens []sourceToken, open int) bool {
	// A callee can end in a name, grouped expression, call result, or computed
	// property. Walk these balanced spans and member/optional access backwards.
	// A grouped/dynamic callee mentioning an assertion is conservatively owned
	// by it; proving the runtime result of a conditional/comma/call is outside
	// this subset. This examines only the callee, never unrelated statements.
	for i := open - 1; i >= 0; {
		if tokens[i].text == "." {
			i--
			if i >= 0 && tokens[i].text == "?" {
				i--
			}
			continue
		}
		if tokens[i].text == ")" || tokens[i].text == "]" {
			start := tokens[i].pair
			for j := start + 1; j < i; j++ {
				if assertionName(tokens[j].text) || tokens[j].quote != 0 && assertionName(tokens[j].value) {
					return true
				}
			}
			i = start - 1
			continue
		}
		if assertionName(tokens[i].text) {
			return true
		}
		if i > 0 && tokens[i-1].text == "." {
			i--
			continue
		}
		return false
	}
	return false
}

func assertionName(name string) bool {
	return name == "expect" || strings.HasPrefix(strings.ToLower(name), "assert")
}

func pythonAssertionOwnsMatch(code string, m literalMatch) bool {
	// Python physical newlines inside balanced delimiters continue the current
	// logical statement. Only depth-zero newlines/semicolons end an assert;
	// comparisons, boolean operands and the comma-separated message all belong
	// to that statement. Comments/opaque strings do not supply keyword tokens.
	depth := 0
	assertion := false
	for i := 0; i <= m.call; i++ {
		token := m.tokens[i]
		if depth == 0 && i > 0 && strings.ContainsAny(code[m.tokens[i-1].end:token.start], "\r\n") {
			assertion = false
		}
		if depth == 0 {
			if token.text == ";" {
				assertion = false
			} else if token.text == "assert" {
				assertion = true
			}
		}
		switch token.text {
		case "(", "[", "{":
			depth++
		case ")", "]", "}":
			depth--
		}
	}
	return assertion
}

func comparisonOperand(tokens []sourceToken) bool {
	if len(tokens) == 0 {
		return false
	}
	last := tokens[len(tokens)-1]
	if last.quote != 0 || last.text == ")" || last.text == "]" {
		return true
	}
	if last.text == "" || !isIdentifierByte(last.text[0]) {
		return false
	}
	switch last.text {
	case "return", "throw", "yield", "await", "new", "delete", "void", "typeof", "instanceof", "in", "else", "case":
		return false
	}
	return true
}

func replaceSelector(code, old, next, framework string) (string, bool) {
	matches, ok := sourceLocators(code, old, framework)
	if !ok || len(matches) != 1 || assertionOwnsMatch(code, matches[0], framework) || !sourceArgumentContextSafe(matches[0]) {
		return "", false
	}
	m := matches[0]
	return code[:m.start] + escapeJSString(next, m.quote) + code[m.end:], true
}

// ExactLocatorSelectorReplacement proves that candidate differs from source
// only by the literal selector of one safe failed-locator action. Tier 2 uses
// this before it executes provider output.
func ExactLocatorSelectorReplacement(source, candidate, old, framework string) bool {
	matches, ok := sourceLocators(source, old, framework)
	if !ok || len(matches) != 1 || assertionOwnsMatch(source, matches[0], framework) || !sourceArgumentContextSafe(matches[0]) || !directActionMatch(source, matches[0]) {
		return false
	}
	m := matches[0]
	if !strings.HasPrefix(candidate, source[:m.start]) || !strings.HasSuffix(candidate, source[m.end:]) || len(candidate) <= m.start+len(source)-m.end {
		return false
	}
	replacement := candidate[m.start : len(candidate)-len(source[m.end:])]
	// A different, non-empty value only: an empty getBy name or text would
	// match every element instead of re-finding one.
	if replacement == source[m.start:m.end] || strings.ContainsAny(replacement, "\\\\\r\n") || strings.ContainsRune(replacement, rune(m.quote)) {
		return false
	}
	expected, ok := replaceSelector(source, old, replacement, framework)
	return ok && expected == candidate
}

// EditableLocatorAction reports whether ExactLocatorSelectorReplacement can
// accept any candidate for this failed selector: the source must hold exactly
// one direct `await page.locator('<selector>').<action>(…)` statement outside
// an assertion. Tier 2 checks this before asking a provider, so a proposal
// that can never be admitted is not requested. The reason names the boundary.
func EditableLocatorAction(source, selector, framework string) (bool, string) {
	if selector == "" {
		return false, "the failure names no locator selector"
	}
	locator, parsed := parseLocatorExpression(selector)
	if !parsed {
		return false, "the failed locator is not a page.locator(...) or single page.getBy*(...) call with literal arguments"
	}
	form := locator.sourceForm()
	matches, ok := sourceLocators(source, selector, framework)
	switch {
	case !ok:
		return false, "the spec uses syntax outside the editable source subset (escaped identifiers, template interpolation, JSX or unbalanced brackets)"
	case len(matches) == 0:
		if locator.method == "locator" {
			form = "page.locator(" + quoteSelector(locator.value) + ") or page.<action>(" + quoteSelector(locator.value) + ", ...)"
		}
		return false, "no " + form + " call in the spec matches the failed locator"
	case len(matches) > 1:
		return false, "the failed locator appears more than once as " + form
	case assertionOwnsMatch(source, matches[0], framework):
		return false, "the failed locator is inside an assertion, which healing does not edit"
	case !sourceArgumentContextSafe(matches[0]) || !directActionMatch(source, matches[0]):
		return false, "the failed locator is not a direct `await " + form + ".<action>(...)` statement on one line"
	}
	return true, ""
}

func quoteSelector(selector string) string {
	if len(selector) > 120 {
		selector = selector[:120] + "…"
	}
	return "'" + selector + "'"
}

func directActionMatch(code string, m literalMatch) bool {
	tokens := m.tokens
	if m.call == 0 || tokens[m.call].text != "page" || tokens[m.call-1].text != "await" {
		return false
	}
	// Only an await expression statement can be edited. The preceding token
	// proves this is not a conditional, return, assignment, or argument.
	await := m.call - 1
	if await == 0 || (tokens[await-1].text != "{" && tokens[await-1].text != ";") {
		return false
	}
	// A direct statement cannot be inside a control/grouping parenthesis. This
	// rejects multi-line `if`/`for` predicates even when semicolons surround it.
	for i := 0; i < await; i++ {
		if tokens[i].text == "(" && tokens[i].pair >= await && !functionBodyBetween(tokens, i, await) {
			return false
		}
	}
	name, open, ok := actionCall(tokens, m)
	if !ok || !directActions[name] {
		return false
	}
	end := tokens[open].pair + 1
	if end < len(tokens) && tokens[end].text == ";" {
		end++
	}
	// ASI can continue an expression on the next physical line (for example
	// `await action()\n&& assertion`). Require an explicit semicolon or EOF/}
	// after whitespace, so a replacement cannot short-circuit later behavior.
	terminated := end > 0 && tokens[end-1].text == ";"
	if !terminated && (end >= len(tokens) || tokens[end].text != "}") {
		return false
	}
	lineEnd := strings.IndexByte(code[m.lineStart:], '\n')
	if lineEnd < 0 {
		lineEnd = len(code)
	} else {
		lineEnd += m.lineStart
	}
	return end > 0 && tokens[end-1].end <= lineEnd && strings.TrimSpace(code[tokens[end-1].end:lineEnd]) == ""
}

// functionBodyBetween proves that an enclosing call/group reaches this action
// through a callback/function body, rather than consuming the action as a
// predicate, argument, or grouped expression.
func functionBodyBetween(tokens []sourceToken, open, position int) bool {
	for i := open + 1; i < position; i++ {
		if tokens[i].text != "{" || tokens[i].pair < position || i == 0 {
			continue
		}
		if tokens[i-1].text == "=>" {
			return true
		}
		if tokens[i-1].text == ")" {
			before := tokens[i-1].pair - 1
			if before > open && tokens[before].text != "function" {
				before--
			}
			if before > open && tokens[before].text == "function" {
				return true
			}
		}
	}
	return false
}

func sourceArgumentContextSafe(m literalMatch) bool {
	// A locator passed to an arbitrary callee could belong to an assertion
	// alias. Do not infer runtime identities. An enclosing proven function body
	// is a separate execution scope, so test(..., async () => { action }) remains
	// supported without granting argument edits to unknown calls.
	t := m.tokens
	for i, token := range t {
		if token.text != "(" || i >= m.call || token.pair < m.literal || i == 0 {
			continue
		}
		callee := t[i-1].text
		switch callee {
		case "if", "elif", "while", "for", "switch", "catch", "with", "assert", "and", "or", "not", "return", "throw", "yield", "await":
			continue // Grouping/control syntax, not arbitrary call arguments.
		}
		if callee != ")" && callee != "]" && callee != "." && (callee == "" || !isIdentifierByte(callee[0])) {
			continue
		}
		body := false
		for j := i + 1; j < m.call; j++ {
			if t[j].text != "{" || t[j].pair < m.literal {
				continue
			}
			if t[j-1].text == "=>" {
				body = true
			} else if t[j-1].text == ")" {
				before := t[j-1].pair - 1
				if before > i && t[before].text != "function" {
					before-- // Optional function name.
				}
				body = body || before > i && t[before].text == "function"
			}
		}
		if !body {
			return false
		}
	}
	return true
}

func replaceLiteral(code, old, next string) (string, bool) {
	return replaceSelector(code, old, next, "playwright")
}
func isIdentifierByte(value byte) bool {
	return value == '_' || value == '$' || value >= 'a' && value <= 'z' || value >= 'A' && value <= 'Z' || value >= '0' && value <= '9'
}
func escapeJSString(value string, quote byte) string {
	value = strings.ReplaceAll(value, "\\", "\\\\")
	value = strings.ReplaceAll(value, string(quote), "\\"+string(quote))
	value = strings.ReplaceAll(value, "\n", "\\n")
	value = strings.ReplaceAll(value, "\r", "\\r")
	value = strings.ReplaceAll(value, "\t", "\\t")
	if quote == '`' {
		value = strings.ReplaceAll(value, "${", "\\${")
	}
	return value
}

// Wait insertion needs more proof than a literal replacement: both statements
// must remain in the same execution scope. Accept only a complete expression
// statement after { or ; (or module start), with no enclosing sync function.
func addWait(code, selector string) (string, bool) {
	matches, ok := sourceLocators(code, selector, "playwright")
	if !ok || len(matches) != 1 {
		return "", false
	}
	m := matches[0]
	t := m.tokens
	if !m.css || !m.awaitPrefix || assertionOwnsMatch(code, m, "playwright") || t[m.call].text != "page" {
		return "", false
	}
	await := m.call - 1
	if await > 0 && t[await-1].text != "{" && t[await-1].text != ";" && t[await-1].text != "}" {
		return "", false
	}
	if strings.TrimSpace(code[m.lineStart:t[await].start]) != "" {
		return "", false
	}
	// A semicolon within a for header is not a statement boundary. Function
	// callback parentheses are permitted only when their function body brace
	// encloses the statement inside those parentheses.
	for i, token := range t {
		if token.text != "(" || i >= await || token.pair < await {
			continue
		}
		body := false
		for j := i + 1; j < await; j++ {
			if t[j].text == "{" && t[j].pair > await {
				body = true
			}
		}
		if !body {
			return "", false
		}
	}
	// Complete chain action with zero/opaque arguments, terminating at the end
	// of this physical line. Never split a multiline expression or ASI hazard.
	name, open, ok := actionCall(t, m)
	if !ok || !directActions[name] || open+1 >= len(t) {
		return "", false
	}
	end := t[open].pair + 1
	if end < len(t) && t[end].text == ";" {
		end++
	}
	lineEnd := strings.IndexByte(code[m.lineStart:], '\n')
	if lineEnd < 0 {
		lineEnd = len(code)
	} else {
		lineEnd += m.lineStart
	}
	if t[end-1].end > lineEnd || strings.TrimSpace(code[t[end-1].end:lineEnd]) != "" {
		return "", false
	}
	if end < len(t) && t[end].start < lineEnd {
		return "", false
	}
	// Identify every enclosing brace's function header. A nearest synchronous
	// function blocks an outer async function. Unknown method/function syntax
	// is refused. Plain blocks inherit their enclosing function's async state.
	async := true // standalone module snippets already use top-level await
	for i, token := range t {
		if token.text != "{" || i >= await || token.pair < await {
			continue
		}
		j := i - 1
		if j >= 0 && t[j].text == "=>" {
			j--
			if j >= 0 && t[j].text == ")" {
				j = t[j].pair - 1
			}
			async = j >= 0 && t[j].text == "async"
			continue
		}
		if j >= 0 && t[j].text == ")" {
			open := t[j].pair
			k := open - 1
			if k >= 0 && (t[k].text == "if" || t[k].text == "for" || t[k].text == "while" || t[k].text == "switch" || t[k].text == "catch" || t[k].text == "with") {
				continue
			}
			if k >= 0 && t[k].text != "function" {
				k--
			}
			if k < 0 || t[k].text != "function" {
				return "", false
			}
			async = k > 0 && t[k-1].text == "async"
		} else if j >= 0 && t[j].text != "else" && t[j].text != "try" && t[j].text != "finally" && t[j].text != "do" && t[j].text != ";" && t[j].text != "{" && t[j].text != "}" {
			return "", false
		}
	}
	if !async {
		return "", false
	}
	indent := code[m.lineStart:t[await].start]
	q := string(m.quote)
	wait := indent + "await page.locator(" + q + escapeJSString(selector, m.quote) + q + ").waitFor({ state: 'visible', timeout: 10000 });\n"
	return code[:m.lineStart] + wait + code[m.lineStart:], true
}
