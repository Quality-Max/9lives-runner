package runner

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// fakeExecutables puts shell scripts on an otherwise empty PATH, so neither a
// real 9lives entry point nor a real python3 can be selected.
func fakeExecutables(t *testing.T, scripts map[string]string) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("shell script executables")
	}
	dir := t.TempDir()
	for name, body := range scripts {
		if err := os.WriteFile(filepath.Join(dir, name), []byte("#!/bin/sh\n"+body), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("PATH", dir)
	t.Setenv("NINELIVES_PYTHON", "")
}

func TestRunPythonPrefersTheEntryPointAndPassesStdioThrough(t *testing.T) {
	fakeExecutables(t, map[string]string{
		"9lives":  "printf '%s|' \"$@\"; read line; printf '%s' \"$line\" >&2; exit 7\n",
		"python3": "printf 'python3 selected'; exit 1\n",
	})
	var out, errOut bytes.Buffer
	code, err := RunPython(context.Background(), "mcp", []string{"--flag"}, strings.NewReader("ping\n"), &out, &errOut)
	if err != nil || code != 7 || out.String() != "mcp|--flag|" || errOut.String() != "ping" {
		t.Fatalf("code=%d err=%v stdout=%q stderr=%q", code, err, out.String(), errOut.String())
	}
}

func TestRunPythonReportsAMissingPackageInsteadOfRunning(t *testing.T) {
	// The interpreter cannot import ninelives.cli; running it would exit 1.
	fakeExecutables(t, map[string]string{"python3": "[ \"$1\" = -c ] && exit 1; printf ran\n"})
	var out bytes.Buffer
	_, err := RunPython(context.Background(), "heal", nil, strings.NewReader(""), &out, &out)
	if err == nil || !strings.Contains(err.Error(), "Python 9lives is not installed") || !strings.Contains(err.Error(), "pip install 9lives") || out.Len() != 0 {
		t.Fatalf("err=%v output=%q", err, out.String())
	}
	t.Setenv("PATH", t.TempDir())
	if _, err := RunPython(context.Background(), "heal", nil, strings.NewReader(""), &out, &out); err == nil || !strings.Contains(err.Error(), "python3 not found") {
		t.Fatalf("missing interpreter: %v", err)
	}
}

func TestRunPythonUsesTheSelectedInterpreterModule(t *testing.T) {
	fakeExecutables(t, map[string]string{
		"9lives":      "printf 'entry point selected'\n",
		"venv-python": "[ \"$1\" = -c ] && exit 0; printf '%s|' \"$@\"\n",
	})
	t.Setenv("NINELIVES_PYTHON", "venv-python")
	var out, errOut bytes.Buffer
	code, err := RunPython(context.Background(), "heal", []string{"tests/a.spec.ts", "--yes"}, strings.NewReader(""), &out, &errOut)
	if err != nil || code != 0 || out.String() != "-m|ninelives.cli|heal|tests/a.spec.ts|--yes|" {
		t.Fatalf("code=%d err=%v stdout=%q stderr=%q", code, err, out.String(), errOut.String())
	}
}
