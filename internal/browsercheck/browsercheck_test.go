package browsercheck

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
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

// A check's scratch lives under the directory Run owns, and is gone after Run
// returns: when the helper answered, and when it was stopped at its timeout
// with its own cleanup never run.
func TestRunRemovesTheHelperScratchEvenWhenTheHelperIsStopped(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("the fake node is a shell script")
	}
	bin, record := t.TempDir(), filepath.Join(t.TempDir(), "scratch-path")
	script := "#!/bin/sh\nprintf '%s' \"$NINELIVES_HELPER_SCRATCH\" > " + record + "\n" +
		"[ -d \"$NINELIVES_HELPER_SCRATCH\" ] || exit 7\nmkdir \"$NINELIVES_HELPER_SCRATCH/wda\" && touch \"$NINELIVES_HELPER_SCRATCH/wda/products\"\n" +
		"[ \"$FAKE_HANG\" = 1 ] && sleep 30\ncat >/dev/null\nprintf '{\"version\":1,\"result\":{\"ok\":true}}\\n'\n"
	if err := os.WriteFile(filepath.Join(bin, "node"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	for _, hang := range []bool{false, true} {
		value := "0"
		if hang {
			value = "1"
		}
		t.Setenv("FAKE_HANG", value)
		// The answering helper gets a generous bound: on a loaded host
		// materializing the helper and starting the shell can take seconds.
		// The hanging one is stopped at a short bound on purpose.
		timeout := time.Minute
		if hang {
			timeout = 2 * time.Second
		}
		_, err := Run(context.Background(), Request{Tool: "inspect_ios"}, Options{Root: t.TempDir(), Timeout: timeout, Env: []string{"FAKE_HANG"}})
		if hang != errors.Is(err, ErrTimeout) || (!hang && err != nil) {
			t.Fatalf("hang %v: %v", hang, err)
		}
		scratch, readErr := os.ReadFile(record)
		if readErr != nil || !filepath.IsAbs(string(scratch)) {
			t.Fatalf("hang %v: no absolute scratch recorded: %q %v", hang, scratch, readErr)
		}
		if _, statErr := os.Stat(string(scratch)); !os.IsNotExist(statErr) {
			t.Fatalf("hang %v: scratch %s survived Run: %v", hang, scratch, statErr)
		}
	}
}
