package runner

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
)

// RunPythonHealer preserves the existing healing implementation behind an
// explicit compatibility boundary. No shell is involved.
func RunPythonHealer(ctx context.Context, args []string, out, errOut io.Writer) (int, error) {
	python := os.Getenv("NINELIVES_PYTHON")
	if python == "" {
		python = "python3"
	}
	binary, err := exec.LookPath(python)
	if err != nil {
		return 0, fmt.Errorf("%s not found; install Python 9lives or set NINELIVES_PYTHON", python)
	}
	commandArgs := append([]string{"-m", "ninelives.cli", "heal"}, args...)
	command := exec.CommandContext(ctx, binary, commandArgs...)
	command.Stdout, command.Stderr = out, errOut
	err = command.Run()
	if err == nil {
		return 0, nil
	}
	if exit, ok := err.(*exec.ExitError); ok {
		return exit.ExitCode(), nil
	}
	return 0, err
}
