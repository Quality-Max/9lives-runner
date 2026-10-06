// Package contracttest locates the optional toolchain used by offline
// contract tests: a Python interpreter, its validator modules, and pinned
// upstream sources. Contributors without it skip those tests; CI sets
// NINELIVES_REQUIRE_CONTRACT=1 so a missing toolchain fails there instead.
package contracttest

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// Unavailable fails the test when the contract toolchain is required and
// skips it otherwise.
func Unavailable(t testing.TB, format string, args ...any) {
	t.Helper()
	if os.Getenv("NINELIVES_REQUIRE_CONTRACT") == "1" {
		t.Fatalf(format, args...)
	}
	t.Skipf(format+" (set NINELIVES_REQUIRE_CONTRACT=1 to make this a failure)", args...)
}

// Python returns the interpreter named by NINELIVES_CONTRACT_PYTHON (default
// python3) after checking that it can import every module listed.
func Python(t testing.TB, modules ...string) string {
	t.Helper()
	python := os.Getenv("NINELIVES_CONTRACT_PYTHON")
	if python == "" {
		python = "python3"
	}
	if _, err := exec.LookPath(python); err != nil {
		Unavailable(t, "contract Python %q is unavailable: %v", python, err)
	}
	if len(modules) > 0 {
		probe := exec.Command(python, "-c", "import "+strings.Join(modules, ", "))
		if output, err := probe.CombinedOutput(); err != nil {
			Unavailable(t, "contract Python cannot import %s: %v: %s", strings.Join(modules, ", "), err, strings.TrimSpace(string(output)))
		}
	}
	return python
}

// RunnerRoot is the repository root, independent of the test's directory.
func RunnerRoot() string {
	_, source, _, _ := runtime.Caller(0)
	return filepath.Clean(filepath.Join(filepath.Dir(source), "../.."))
}

// UpstreamSource returns <dir>/<checkout>/src for a pinned upstream checkout.
// dir is NINELIVES_UPSTREAM_DIR, defaulting to testdata/upstream where CI
// checks out the pinned revisions.
func UpstreamSource(t testing.TB, checkout string) string {
	t.Helper()
	directory := os.Getenv("NINELIVES_UPSTREAM_DIR")
	if directory == "" {
		directory = filepath.Join(RunnerRoot(), "testdata", "upstream")
	}
	source := filepath.Join(directory, checkout, "src")
	if info, err := os.Stat(source); err != nil || !info.IsDir() {
		Unavailable(t, "pinned upstream source %s is unavailable: %v", source, err)
	}
	return source
}
