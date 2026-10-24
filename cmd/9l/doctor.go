package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"github.com/Quality-Max/9lives-runner/internal/adapters/playwrightsdk"
	"github.com/Quality-Max/9lives-runner/internal/browsercheck"
	"github.com/Quality-Max/9lives-runner/internal/prove"
)

func doctorCommand(args []string, out, errOut io.Writer) int {
	fs := flag.NewFlagSet("doctor", flag.ContinueOnError)
	fs.SetOutput(errOut)
	format := fs.String("format", "text", "text or json")
	sdk := fs.Bool("sdk", false, "require the optional @9l/playwright SDK; with a spec path, also check that spec for prove")
	paths := fs.Bool("paths", false, "print executable and config paths for local use; never persisted")
	// Flags may follow the spec, as with run and prove.
	if err := fs.Parse(flagsFirst(args, map[string]bool{"-format": true, "--format": true})); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0
		}
		return 2
	}
	if fs.NArg() > 1 || (fs.NArg() == 1 && !*sdk) || (*format != "text" && *format != "json") {
		fmt.Fprintln(errOut, "9l: doctor takes only --sdk [spec], --paths and --format text|json")
		return 2
	}
	helperArgs := map[string]any{"cliVersion": version, "sdk": *sdk, "paths": *paths, "executable": executableFacts(*paths)}
	if fs.NArg() == 1 {
		// The helper reads the spec as text and never executes it; refuse
		// anything but a regular file here so the report names a real spec.
		spec, err := filepath.Abs(fs.Arg(0))
		if info, statErr := os.Stat(spec); err != nil || statErr != nil || !info.Mode().IsRegular() {
			fmt.Fprintln(errOut, "9l: doctor: the spec must be an existing file")
			return 2
		}
		helperArgs["spec"] = spec
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	// What this engine admits and can request, so the helper can judge an
	// installed SDK by its declared protocol rather than by version string.
	helperArgs["engine"] = map[string]any{"protocol": playwrightsdk.Version, "capabilities": playwrightsdk.Capabilities, "faults": prove.Kinds}
	response, err := browsercheck.Run(ctx, browsercheck.Request{Tool: "doctor", Args: helperArgs}, browsercheck.Options{Timeout: 10 * time.Second})
	if err != nil {
		// Never echo a helper's arbitrary stderr or project paths from diagnostics.
		if errors.Is(err, browsercheck.ErrNodeUnavailable) {
			fmt.Fprintln(errOut, "9l: Node is missing from PATH. Install Node 24 LTS, then restart your terminal and MCP host.")
		} else {
			fmt.Fprintln(errOut, "9l: diagnostics could not complete; check Node 24 and run from your project directory.")
		}
		return 2
	}
	var report struct {
		Ready      bool                                          `json:"ready"`
		Checks     []struct{ Name, Status, Message, Fix string } `json:"checks"`
		Unverified []string                                      `json:"unverified"`
		Next       string                                        `json:"next"`
	}
	if json.Unmarshal(response.Result, &report) != nil {
		fmt.Fprintln(errOut, "9l: invalid diagnostic result")
		return 2
	}
	if *format == "json" {
		if _, err := fmt.Fprintln(out, string(response.Result)); err != nil {
			return 2
		}
	} else {
		fmt.Fprintf(out, "9l %s — local prerequisite diagnostics\n", version)
		for _, check := range report.Checks {
			fmt.Fprintf(out, "%s [%s]: %s\n", check.Name, check.Status, check.Message)
			if check.Fix != "" {
				fmt.Fprintf(out, "  Next: %s\n", check.Fix)
			}
		}
		for _, check := range report.Unverified {
			fmt.Fprintf(out, "Unverified: %s\n", check)
		}
		fmt.Fprintln(out, report.Next)
	}
	if !report.Ready {
		return 2
	}
	return 0
}

// executableFacts compares the file doctor runs from with the 9l that PATH
// resolves to, without executing either. The paths travel to the helper only
// under --paths; otherwise the report says just whether the two match.
func executableFacts(paths bool) map[string]any {
	facts := map[string]any{"samePath": false, "otherOnPath": false}
	self, err := os.Executable()
	if err != nil {
		return facts
	}
	other, err := exec.LookPath("9l")
	if err != nil && !errors.Is(err, exec.ErrDot) {
		return facts
	}
	facts["otherOnPath"] = true
	facts["samePath"] = sameFile(self, other)
	if paths {
		facts["self"], facts["other"] = realPath(self), realPath(other)
	}
	return facts
}

func realPath(path string) string {
	if resolved, err := filepath.EvalSymlinks(path); err == nil {
		return resolved
	}
	return path
}

func sameFile(a, b string) bool {
	infoA, errA := os.Stat(a)
	infoB, errB := os.Stat(b)
	if errA == nil && errB == nil {
		return os.SameFile(infoA, infoB)
	}
	return realPath(a) == realPath(b)
}
