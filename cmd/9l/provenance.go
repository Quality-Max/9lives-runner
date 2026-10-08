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

	"github.com/Quality-Max/9lives-runner/internal/runner"
)

func loadAgentProvenance(path string) (runner.AgentProvenance, error) {
	info, err := os.Stat(path)
	if err != nil || !info.Mode().IsRegular() || info.Size() > 4096 {
		return runner.AgentProvenance{}, errors.New("agent creation snapshot unavailable or exceeds limit")
	}
	file, err := os.Open(path)
	if err != nil {
		return runner.AgentProvenance{}, errors.New("agent creation snapshot unavailable")
	}
	defer file.Close()
	raw, err := io.ReadAll(io.LimitReader(file, 4097))
	if err != nil {
		return runner.AgentProvenance{}, errors.New("agent creation snapshot unavailable")
	}
	return runner.ParseAgentProvenance(raw)
}

func provenanceCommand(args []string, out, errOut io.Writer) int {
	fs := flag.NewFlagSet("provenance", flag.ContinueOnError)
	fs.SetOutput(errOut)
	agent := fs.String("agent", "", "declared agent identifier (not authenticated authorship)")
	if fs.Parse(flagsFirst(args, map[string]bool{"--agent": true})) != nil {
		return 2
	}
	if fs.NArg() != 1 || *agent == "" {
		fmt.Fprintln(errOut, "9l: provenance requires one spec and --agent")
		return 2
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	record, err := runner.CaptureAgentProvenance(ctx, fs.Arg(0), *agent)
	if err != nil {
		fmt.Fprintln(errOut, "9l:", err)
		return 2
	}
	if json.NewEncoder(out).Encode(record) != nil {
		return 2
	}
	return 0
}
