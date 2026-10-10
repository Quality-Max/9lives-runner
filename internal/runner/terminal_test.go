package runner

import (
	"strings"
	"testing"
)

func TestStripTerminalEscapesRemovesCSIAndOSCSequences(t *testing.T) {
	raw := "\x1b[31mred\x1b[39m plain \x1b]8;;https://evil.example\x1b\\link\x1b]8;;\x1b\\ tail"
	if got := StripTerminalEscapes(raw); got != "red plain link tail" {
		t.Fatalf("stripped=%q", got)
	}
	if got := StripTerminalEscapes("no escapes"); got != "no escapes" {
		t.Fatalf("changed plain text: %q", got)
	}
	// An unterminated OSC sequence must not swallow the rest of the text.
	unterminated := "keep \x1b]8;;no-terminator"
	if got := StripTerminalEscapes(unterminated); strings.Contains(got, "\x1b]8;;") || !strings.HasPrefix(got, "keep ") {
		t.Fatalf("unterminated osc: %q", got)
	}
}

func TestRedactTextCoversAuthorizationHeaders(t *testing.T) {
	for raw, want := range map[string]string{
		"Authorization: Bearer sk-live-abcdef123456":        "Authorization: Bearer [REDACTED]",
		"authorization=Basic dXNlcjpwYXNzd29yZA==":          "authorization=Basic [REDACTED]",
		"request sent with bearer eyJhbGciOiJIUzI1NiJ9.e30": "request sent with bearer [REDACTED]",
		"the bearer token expired":                          "the bearer token expired",
	} {
		if got := RedactText(raw); got != want {
			t.Errorf("RedactText(%q) = %q, want %q", raw, got, want)
		}
	}
}
