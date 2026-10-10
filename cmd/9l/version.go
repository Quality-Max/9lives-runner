package main

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"

	"github.com/Quality-Max/9lives-runner/internal/adapters/playwrightsdk"
	"github.com/Quality-Max/9lives-runner/internal/assessment"
	"github.com/Quality-Max/9lives-runner/internal/runner"
)

// VersionInfoVersion is the version of the `9l version --format json` output.
const VersionInfoVersion = 1

// VersionInfo lets a host confirm it found the Go runner, not the Python
// 9lives `9l` command, and that the output contracts it decodes match.
type VersionInfo struct {
	Version        int       `json:"version"`
	Name           string    `json:"name"`
	Implementation string    `json:"implementation"`
	CLIVersion     string    `json:"cliVersion"`
	Contracts      Contracts `json:"contracts"`
	Commands       []string  `json:"commands"`
}

// Contracts lists the version of each machine-readable output.
type Contracts struct {
	Plan             int    `json:"plan"`
	RunResult        int    `json:"runResult"`
	RunStatus        int    `json:"runStatus"`
	Receipt          int    `json:"receipt"`
	ProgressEvent    int    `json:"progressEvent"`
	Assess           int    `json:"assess"`
	AssessSuite      int    `json:"assessSuite"`
	ExecutionReceipt string `json:"executionReceipt"`
	Engine           string `json:"engine"`
}

var commands = []string{"plan", "run", "status", "result", "cancel", "assess", "provenance", "prove", "confirm", "tier1", "heal-native", "heal", "mcp", "version"}

func versionInfo() VersionInfo {
	return VersionInfo{
		Version: VersionInfoVersion, Name: "9l", Implementation: "go-runner", CLIVersion: version,
		Contracts: Contracts{
			Plan: runner.PlanVersion, RunResult: runner.RunSummaryVersion, RunStatus: runner.RunStatusVersion,
			Receipt: runner.ReceiptVersion, ProgressEvent: runner.ProgressEventVersion,
			Assess: assessment.ReportVersion, AssessSuite: assessment.SuiteVersion,
			ExecutionReceipt: runner.CanonicalReceiptSchema, Engine: playwrightsdk.Version,
		},
		Commands: commands,
	}
}

func versionCommand(args []string, out, errOut io.Writer) int {
	fs := flag.NewFlagSet("version", flag.ContinueOnError)
	fs.SetOutput(errOut)
	format := fs.String("format", "text", "text or json")
	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0
		}
		return exitUsage
	}
	if fs.NArg() != 0 || (*format != "text" && *format != "json") {
		fmt.Fprintln(errOut, "9l: version takes only --format text|json")
		return exitUsage
	}
	if *format == "json" {
		if err := json.NewEncoder(out).Encode(versionInfo()); err != nil {
			return 1
		}
		return 0
	}
	fmt.Fprintf(out, "9l %s (Go runner)\n", version)
	return 0
}
