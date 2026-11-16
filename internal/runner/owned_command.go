package runner

import (
	"context"
	"os/exec"
)

// RunOwnedCommand runs command with the runner's process ownership cleanup. It is for
// local helper processes whose descendants must be stopped on cancellation or
// after the leader exits. Callers must not use exec.CommandContext: this
// function owns cancellation and cleanup of the group and observed descendants.
func RunOwnedCommand(ctx context.Context, command *exec.Cmd) error {
	err, _ := startAndWait(ctx, command)
	return err
}
