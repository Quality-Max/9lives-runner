package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/Quality-Max/9lives-runner/internal/browsercheck"
)

func doctorCommand(args []string, out, errOut io.Writer) int {
	fs := flag.NewFlagSet("doctor", flag.ContinueOnError)
	fs.SetOutput(errOut)
	format := fs.String("format", "text", "text or json")
	sdk := fs.Bool("sdk", false, "require the optional @9l/playwright SDK")
	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0
		}
		return 2
	}
	if fs.NArg() != 0 || (*format != "text" && *format != "json") {
		fmt.Fprintln(errOut, "9l: doctor takes only --sdk and --format text|json")
		return 2
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	response, err := browsercheck.Run(ctx, browsercheck.Request{Tool: "doctor", Args: map[string]any{"cliVersion": version, "sdk": *sdk}}, browsercheck.Options{Timeout: 10 * time.Second})
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
