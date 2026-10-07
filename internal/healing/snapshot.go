package healing

import (
	"io"
	"strings"
	"unicode/utf8"

	"golang.org/x/net/html"
)

// Candidates and text come only from the HTML5 tree, never from the lexical
// guard's tokens. In particular, script escaping and tree construction can
// turn an apparent start tag into text or discard it altogether.
func snapshotTokens(snapshot string) ([]map[string]string, string) {
	doc := snapshotDocument(snapshot)
	if doc == nil {
		return nil, ""
	}
	var elements []map[string]string
	var visible strings.Builder
	// Walk iteratively. Both the request size and total nodes visited are
	// bounded; nested input cannot exhaust the Go stack during extraction.
	stack := []*html.Node{doc}
	visited := 0
	for len(stack) > 0 {
		n := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		visited++
		if visited > 1<<20 {
			return nil, ""
		}
		if n.Type == html.ElementNode {
			if n.Namespace != "" || n.Data == "svg" || n.Data == "math" {
				return nil, ""
			}
			if inertSnapshotElement(n.Data) {
				continue // Exclude its own attributes and every descendant.
			}
			attrs := make(map[string]string, len(n.Attr))
			for _, a := range n.Attr {
				if a.Namespace == "" {
					key := asciiLower(a.Key)
					if _, seen := attrs[key]; !seen {
						attrs[key] = a.Val // The first duplicate attribute wins.
					}
				}
			}
			elements = append(elements, attrs)
		} else if n.Type == html.TextNode {
			visible.WriteString(n.Data) // Already decoded by the DOM parser.
		}
		for child := n.LastChild; child != nil; child = child.PrevSibling {
			stack = append(stack, child)
		}
	}
	return elements, visible.String()
}

func snapshotDocument(snapshot string) *html.Node {
	if !validSnapshotHTML(snapshot) {
		return nil
	}
	doc, err := html.ParseWithOptions(strings.NewReader(snapshot), html.ParseOptionEnableScripting(true))
	if err != nil {
		return nil
	}
	return doc
}

// Quoted legacy Playwright text matches normalized immediate text runs, not
// the concatenated document or even all descendant text of an element. We
// propose only ordinary leaf elements, while counting other matching elements
// (including input values) so unsupported matches cannot hide ambiguity.
func htmlExactText(snapshot, expected string) string {
	if strings.ContainsAny(expected, `\'"`) || strings.Contains(expected, ">>") {
		return ""
	}
	expected = normalizeExactText(expected)
	if expected == "" {
		return ""
	}
	doc := snapshotDocument(snapshot)
	if doc == nil || !supportedExactTextTree(doc) {
		return ""
	}
	stack := []*html.Node{doc}
	visited, matches := 0, 0
	candidate := ""
	for len(stack) > 0 {
		n := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		visited++
		if visited > 1<<20 {
			return ""
		}
		if n.Type == html.ElementNode {
			if n.Namespace != "" || n.Data == "svg" || n.Data == "math" {
				return ""
			}
			if n.Data == "template" {
				for _, a := range n.Attr {
					if a.Key == "shadowrootmode" {
						return "" // A declarative shadow tree is outside this proof.
					}
				}
				continue
			}
			if n.Data == "noscript" {
				return "" // Its DOM differs with the browser scripting setting.
			}
			if n.Data == "head" || n.Data == "script" || n.Data == "style" {
				continue
			}
			runs, leaf := directSnapshotText(n)
			if n.Data == "input" {
				kind := asciiLower(firstSnapshotAttribute(n, "type"))
				if kind == "button" || kind == "submit" {
					runs = []string{firstSnapshotAttribute(n, "value")}
				}
			}
			for _, run := range runs {
				value := normalizeExactText(run)
				if strings.EqualFold(value, expected) {
					matches++
					if matches > 1 {
						return ""
					}
					if leaf && n.Data != "html" && n.Data != "body" && n.Data != "input" && !inertSnapshotElement(n.Data) && safeAttributeValue(value) {
						candidate = value
					}
					break // A selector counts elements, not matching runs.
				}
			}
		}
		for child := n.LastChild; child != nil; child = child.PrevSibling {
			stack = append(stack, child)
		}
	}
	if matches == 1 {
		return candidate
	}
	return ""
}

// Establish whole-tree restrictions before filtering candidate subtrees. For
// example, an implicit head may contain noscript; skipping head first would
// conceal the scripting-dependent tree and permit a false uniqueness proof.
func supportedExactTextTree(doc *html.Node) bool {
	stack := []*html.Node{doc}
	visited := 0
	for len(stack) > 0 {
		n := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		visited++
		if visited > 1<<20 {
			return false
		}
		if n.Type == html.ElementNode {
			if n.Namespace != "" || n.Data == "svg" || n.Data == "math" || n.Data == "noscript" {
				return false
			}
			if n.Data == "template" {
				for _, a := range n.Attr {
					if a.Key == "shadowrootmode" {
						return false
					}
				}
			}
		}
		for child := n.LastChild; child != nil; child = child.PrevSibling {
			stack = append(stack, child)
		}
	}
	return true
}

func firstSnapshotAttribute(n *html.Node, key string) string {
	for _, a := range n.Attr {
		if a.Namespace == "" && a.Key == key {
			return a.Val
		}
	}
	return ""
}

func directSnapshotText(n *html.Node) ([]string, bool) {
	var runs []string
	var current strings.Builder
	leaf := true
	flush := func() {
		if current.Len() > 0 {
			runs = append(runs, current.String())
			current.Reset()
		}
	}
	for child := n.FirstChild; child != nil; child = child.NextSibling {
		switch child.Type {
		case html.TextNode:
			current.WriteString(child.Data)
		case html.CommentNode:
			// Playwright ignores comments without separating adjacent text.
		default:
			flush()
			if child.Type == html.ElementNode {
				leaf = false
			}
		}
	}
	flush()
	return runs, leaf
}

// Match Playwright's JavaScript whitespace normalization. Unicode IsSpace
// would also collapse characters such as NEL that JavaScript does not treat
// as whitespace, inventing exact matches that the browser cannot find.
func normalizeExactText(value string) string {
	var out strings.Builder
	space := false
	for _, r := range value {
		if r == '\u200b' || r == '\u00ad' {
			continue
		}
		if r == '\t' || r == '\n' || r == '\v' || r == '\f' || r == '\r' || r == ' ' || r == '\u00a0' || r == '\u1680' || (r >= '\u2000' && r <= '\u200a') || r == '\u2028' || r == '\u2029' || r == '\u202f' || r == '\u205f' || r == '\u3000' || r == '\ufeff' {
			space = out.Len() > 0
			continue
		}
		if space {
			out.WriteByte(' ')
			space = false
		}
		out.WriteRune(r)
	}
	return out.String()
}

func inertSnapshotElement(name string) bool {
	return name == "template" || name == "plaintext" || rawSnapshotElement(name)
}

func rawSnapshotElement(name string) bool {
	switch name {
	case "script", "style", "textarea", "title", "xmp", "iframe", "noembed", "noframes", "noscript":
		return true
	}
	return false
}

// HTML parsing intentionally recovers malformed input. This refusal-only
// guard restricts that behavior to our supported complete markup. The mature
// tokenizer, including its script escaped/double-escaped states, establishes
// raw-text boundaries. These tokens never supply attributes or visible text.
func validSnapshotHTML(snapshot string) bool {
	if len(snapshot) > 1<<20 || !utf8.ValidString(snapshot) || strings.ContainsRune(snapshot, '\x00') {
		return false
	}
	z := html.NewTokenizer(strings.NewReader(snapshot))
	pendingRaw := ""
	templates := 0
	for {
		kind := z.Next()
		raw := string(z.Raw()) // Token() can mutate Raw's backing buffer.
		if kind == html.ErrorToken {
			return z.Err() == io.EOF && len(raw) == 0 && pendingRaw == "" && templates == 0
		}
		switch kind {
		case html.StartTagToken, html.SelfClosingTagToken, html.EndTagToken:
			if !completeSnapshotTag(raw) {
				return false
			}
			token := z.Token()
			if token.Data == "svg" || token.Data == "math" {
				return false // Foreign-content snapshots remain unsupported.
			}
			if kind == html.EndTagToken {
				if token.Data == pendingRaw {
					pendingRaw = ""
				}
				if token.Data == "template" {
					if templates == 0 {
						return false
					}
					templates--
				}
			} else {
				if rawSnapshotElement(token.Data) {
					pendingRaw = token.Data
				}
				if token.Data == "template" {
					templates++
				}
			}
		case html.CommentToken:
			if !strings.HasPrefix(raw, "<!--") || !strings.HasSuffix(raw, "-->") {
				return false // Bogus/unterminated comments and declarations.
			}
		case html.DoctypeToken:
			if !strings.HasSuffix(raw, ">") || strings.Contains(raw[2:], "<") {
				return false
			}
		}
	}
}

// Check complete ordinary tag/attribute syntax without interpreting a token as
// a DOM element. HTML names fold ASCII only; quoted attribute values keep all
// Unicode bytes. Unsupported malformed attributes fail the whole snapshot.
func completeSnapshotTag(raw string) bool {
	if len(raw) < 3 || raw[0] != '<' || raw[len(raw)-1] != '>' {
		return false
	}
	i := 1
	if raw[i] == '/' {
		i++
	}
	start := i
	for i < len(raw) && htmlNameByte(raw[i]) {
		i++
	}
	if i == start {
		return false
	}
	for i < len(raw) {
		for i < len(raw) && htmlSpace(raw[i]) {
			i++
		}
		if i == len(raw)-1 {
			return true
		}
		if i+1 == len(raw)-1 && raw[i] == '/' {
			return true
		}
		start = i
		for i < len(raw) && htmlNameByte(raw[i]) {
			i++
		}
		if i == start {
			return false
		}
		for i < len(raw) && htmlSpace(raw[i]) {
			i++
		}
		if i < len(raw) && raw[i] == '=' {
			i++
			for i < len(raw) && htmlSpace(raw[i]) {
				i++
			}
			if i >= len(raw)-1 {
				return false
			}
			if raw[i] == '\'' || raw[i] == '"' {
				quote := raw[i]
				i++
				for i < len(raw) && raw[i] != quote {
					i++
				}
				if i >= len(raw)-1 {
					return false
				}
				i++
			} else {
				start = i
				for i < len(raw)-1 && !htmlSpace(raw[i]) {
					if strings.ContainsRune("'\"<>=`", rune(raw[i])) {
						return false
					}
					i++
				}
				if i == start {
					return false
				}
			}
		}
	}
	return false
}

func asciiLower(value string) string {
	bytes := []byte(value)
	for i, c := range bytes {
		if c >= 'A' && c <= 'Z' {
			bytes[i] = c + ('a' - 'A')
		}
	}
	return string(bytes)
}

func htmlNameByte(c byte) bool {
	return c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '-' || c == ':' || c == '_'
}
func htmlSpace(c byte) bool { return c == ' ' || c == '\t' || c == '\n' || c == '\r' || c == '\f' }

func htmlElementAttributes(snapshot string) []map[string]string {
	elements, _ := snapshotTokens(snapshot)
	return elements
}
func htmlVisibleText(snapshot string) string { _, text := snapshotTokens(snapshot); return text }
