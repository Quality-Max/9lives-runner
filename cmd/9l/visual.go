package main

import (
	"errors"
	"os"
	"runtime"

	"github.com/Quality-Max/9lives-runner/internal/runner"
)

// visualMode shows the browser during a run. It changes how the browser is
// displayed, never what is asserted or how evidence is validated.
type visualMode struct {
	headed bool
}

func visualOptions(headed, executing bool) (visualMode, error) {
	// A headed browser without a display fails inside the worker, after the
	// run has started and with its error text withheld from the receipt;
	// refuse before planning instead.
	if headed && executing && !displayAvailable(runtime.GOOS, os.Getenv) {
		return visualMode{}, errors.New("--headed needs a display; on a headless machine run it under a virtual one, e.g. xvfb-run -a 9l run --headed ...")
	}
	return visualMode{headed: headed}, nil
}

// macOS and Windows always have a window server for a logged-in user; other
// Unix systems need an X11 or Wayland display, which the runner already passes
// through to test processes.
func displayAvailable(goos string, getenv func(string) string) bool {
	if goos == "darwin" || goos == "windows" {
		return true
	}
	return getenv("DISPLAY") != "" || getenv("WAYLAND_DISPLAY") != ""
}

func (mode visualMode) apply(plan *runner.Plan) {
	if !mode.headed {
		return
	}
	for index := range plan.Jobs {
		// Playwright's own flag; it overrides `headless` from the config.
		plan.Jobs[index].Command = append(plan.Jobs[index].Command, "--headed")
	}
}
