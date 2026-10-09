package runner

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
)

const pythonInstallHint = "install it with `pip install 9lives` or `uv tool install 9lives`, or set NINELIVES_PYTHON"

// RunPython runs a command of the installed Python 9lives package, such as
// heal or mcp, behind an explicit compatibility boundary. No shell is
// involved; stdin, stdout and stderr are passed through unchanged, so an MCP
// host speaks to the Python server directly.
func RunPython(ctx context.Context, command string, args []string, in io.Reader, out, errOut io.Writer) (int, error) {
	argv, err := pythonCommand(ctx, command)
	if err != nil {
		return 0, err
	}
	process := exec.CommandContext(ctx, argv[0], append(argv[1:], args...)...)
	process.Stdin, process.Stdout, process.Stderr = in, out, errOut
	err = process.Run()
	if err == nil {
		return 0, nil
	}
	if exit, ok := err.(*exec.ExitError); ok {
		return exit.ExitCode(), nil
	}
	return 0, err
}

// pythonCommand selects NINELIVES_PYTHON when set. Otherwise it prefers
// Python's own `9lives` entry point, because a uv or pipx installation is not
// importable from python3, and falls back to `python3 -m ninelives.cli`.
func pythonCommand(ctx context.Context, command string) ([]string, error) {
	python := os.Getenv("NINELIVES_PYTHON")
	if python == "" {
		if entry, err := exec.LookPath("9lives"); err == nil {
			return []string{entry, command}, nil
		}
		python = "python3"
	}
	binary, err := exec.LookPath(python)
	if err != nil {
		return nil, fmt.Errorf("%s not found; %s", python, pythonInstallHint)
	}
	// Without the package the interpreter exits 1, which a caller could not
	// tell from a failed heal.
	if exec.CommandContext(ctx, binary, "-c", "import ninelives.cli").Run() != nil {
		return nil, fmt.Errorf("Python 9lives is not installed for %s; %s", python, pythonInstallHint)
	}
	return []string{binary, "-m", "ninelives.cli", command}, nil
}
