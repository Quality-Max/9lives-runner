package main

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/Quality-Max/9lives-runner/internal/jev"
)

func TestLiveRequiresCredentialBeforeCapture(t *testing.T) {
	t.Setenv(jev.CredentialName, "")
	t.Setenv("PATH", t.TempDir())
	var out, stderr bytes.Buffer
	if run(context.Background(), []string{"--live", "--images"}, &out, &stderr) != 2 || out.Len() != 0 || !strings.Contains(stderr.String(), "No API request made") {
		t.Fatal("live request did not fail safely before starting capture")
	}
}

func TestBrowserHelperCannotInheritProviderCredential(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX executable fixture")
	}
	bin := t.TempDir()
	// Presence alone is enough to detect a leak; no credential value is printed.
	script := `#!/bin/sh
if [ "${TYPESAFE_API_KEY+x}" = x ]; then exit 1; fi
printf '%s' '{"snapshot":"button Try again","bounds":{"x":20,"y":30,"width":100,"height":40},"assertionFailed":true,"observedAt":"2026-10-24T10:00:00Z"}'
`
	if err := os.WriteFile(filepath.Join(bin, "node"), []byte(script), 0700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin)
	t.Setenv(jev.CredentialName, "present") // non-credential presence sentinel
	var c capture
	if err := captureFixture(context.Background(), t.TempDir(), &c); err != nil || !c.AssertionFailed {
		t.Fatal("helper inherited provider credential or failed capture")
	}
}

func TestIllustrativeReportExcludesRawObservation(t *testing.T) {
	dir := t.TempDir()
	p := 0.98
	r := report{Capture: capture{Snapshot: "private-observation-sentinel", AssertionFailed: true}, Declined: &p, Record: jev.Record{Version: jev.Version, Status: "answered", RequestCount: 1}}
	if err := writeReport(dir, r); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"index.html", "record.json"} {
		raw, err := os.ReadFile(filepath.Join(dir, name))
		if err != nil || bytes.Contains(raw, []byte("private-observation-sentinel")) {
			t.Fatal("report retained raw observation")
		}
	}
	raw, _ := os.ReadFile(filepath.Join(dir, "index.html"))
	if !bytes.Contains(raw, []byte("98.0%")) {
		t.Fatal("synthetic typed probability missing from illustrative report")
	}
}
