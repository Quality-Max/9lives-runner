package browsercheck

import (
	"context"
	"errors"
	"regexp"
	"slices"
	"testing"
	"time"
)

// The Go argument check and the helper must agree on the check names.
func TestChecksMatchTheEmbeddedHelper(t *testing.T) {
	source, err := helper.ReadFile("helper/scan-url.js")
	if err != nil {
		t.Fatal(err)
	}
	block := regexp.MustCompile(`(?s)exports\.SUPPORTED_CHECKS = \[(.*?)\]`).FindSubmatch(source)
	if block == nil {
		t.Fatal("SUPPORTED_CHECKS not found in the embedded helper")
	}
	var names []string
	for _, match := range regexp.MustCompile(`'([a-z_]+)'`).FindAllSubmatch(block[1], -1) {
		names = append(names, string(match[1]))
	}
	if !slices.Equal(names, Checks) {
		t.Fatalf("helper checks %v, Go checks %v", names, Checks)
	}
}

func TestRunReportsAMissingNode(t *testing.T) {
	t.Setenv("PATH", t.TempDir())
	_, err := Run(context.Background(), Request{Tool: "scan_url", Args: map[string]any{"url": "https://example.com/"}}, Options{Root: t.TempDir(), Timeout: 10 * time.Second})
	if !errors.Is(err, ErrNodeUnavailable) {
		t.Fatalf("err = %v", err)
	}
}
