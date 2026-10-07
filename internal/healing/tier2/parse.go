package tier2

import (
	"errors"
	"regexp"
	"strings"

	"github.com/qualitymax/9lives-runner/internal/healing"
)

var codeMarker = regexp.MustCompile(`(?im)(?:^|\n)CODE:[ \t]*(?:\r?\n)?`)

// ParseCandidate accepts the pinned REASONING/CHANGES/CODE response contract
// and a legacy single complete fence. CHANGES is bounded to five nonblank
// entries; this is intentionally stricter than the Python display-only parser
// because native healing records a reviewable proposal.
func ParseCandidate(response, original string) (string, error) {
	body := strings.TrimSpace(response)
	if section, envelope := codeEnvelopeBody(body); envelope {
		changes, ok := changesBeforeCode(body)
		if !ok || changeCount(changes) == 0 || changeCount(changes) > 5 {
			return "", errors.New("provider returned invalid CHANGES section")
		}
		body = section
	}
	blocks, err := structuralFences(body)
	if err != nil || len(blocks) != 1 {
		return "", errors.New("provider must return exactly one fenced complete file")
	}
	candidate := blocks[0]
	if candidate == "" || candidate == original {
		return "", errors.New("provider returned no source change")
	}
	if strings.Contains(candidate, "\x00") {
		return "", errors.New("provider returned invalid source")
	}
	return candidate, nil
}

// codeEnvelopeBody admits CODE only before the first structural fence and only
// when CODE itself introduces a fence. A literal CODE: in a bare source file
// therefore remains source text.
func codeEnvelopeBody(body string) (string, bool) {
	first := firstFenceOffset(body)
	for _, loc := range codeMarker.FindAllStringIndex(body, -1) {
		if first >= 0 && loc[0] > first {
			continue
		}
		rest := strings.TrimLeft(body[loc[1]:], " \t\r\n")
		if _, ok := openingFence(firstLine(rest)); ok {
			return rest, true
		}
	}
	return body, false
}

func firstFenceOffset(body string) int {
	for offset := 0; offset < len(body); {
		line, next := lineAt(body, offset)
		if _, ok := openingFence(line); ok {
			return offset
		}
		offset = next
	}
	return -1
}

// structuralFences recognizes only whole-line, matching backtick delimiters.
// Inline markdown and a shorter delimiter inside a complete source file cannot
// terminate the enclosing response fence.
func structuralFences(body string) ([]string, error) {
	var blocks []string
	for offset := 0; offset < len(body); {
		line, next := lineAt(body, offset)
		delimiter, opening := openingFence(line)
		if !opening {
			offset = next
			continue
		}
		contentStart, cursor := next, next
		closed := false
		for cursor < len(body) {
			closeLine, closeNext := lineAt(body, cursor)
			if closingFence(closeLine, delimiter) {
				blocks = append(blocks, body[contentStart:cursor])
				offset, closed = closeNext, true
				break
			}
			cursor = closeNext
		}
		if !closed {
			return nil, errors.New("provider returned an unterminated fence")
		}
	}
	return blocks, nil
}

func lineAt(body string, offset int) (string, int) {
	end := strings.IndexByte(body[offset:], '\n')
	if end < 0 {
		return strings.TrimSuffix(body[offset:], "\r"), len(body)
	}
	end += offset
	return strings.TrimSuffix(body[offset:end], "\r"), end + 1
}

func firstLine(body string) string { line, _ := lineAt(body, 0); return line }
func openingFence(line string) (string, bool) {
	n := 0
	for n < len(line) && line[n] == '`' {
		n++
	}
	if n < 3 {
		return "", false
	}
	language := strings.ToLower(strings.TrimSpace(line[n:]))
	switch language {
	case "", "javascript", "typescript", "python", "js", "ts", "py":
		return strings.Repeat("`", n), true
	}
	return "", false
}
func closingFence(line, delimiter string) bool { return strings.TrimSpace(line) == delimiter }

func changesBeforeCode(body string) (string, bool) {
	upper := strings.ToUpper(body)
	changes := strings.Index(upper, "CHANGES:")
	code := strings.Index(upper, "CODE:")
	if changes < 0 || code < 0 {
		return "", false
	}
	if changes < code {
		return body[changes+len("CHANGES:") : code], true
	}
	return body[changes+len("CHANGES:"):], true
}
func changeCount(section string) int {
	count := 0
	for _, line := range strings.Split(strings.ReplaceAll(section, "\r\n", "\n"), "\n") {
		if strings.TrimSpace(line) != "" {
			count++
		}
	}
	return count
}

func SafeCandidate(source, candidate, failedSelector, framework string) error {
	if failedSelector == "" || !healing.ExactLocatorSelectorReplacement(source, candidate, failedSelector, framework) {
		return errors.New("candidate is not an exact failed-locator selector replacement")
	}
	return nil
}
