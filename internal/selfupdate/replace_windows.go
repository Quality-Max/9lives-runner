//go:build windows

package selfupdate

import (
	"errors"
	"fmt"
	"os"
)

// Windows cannot overwrite a running image. Move it into the private release
// directory first, then put the verified candidate at the original path. The
// old image remains usable by this process and by other already-running runs.
func replaceExecutable(candidate, target, backup string) (bool, error) {
	if err := os.Rename(target, backup); err != nil {
		return false, errors.New("could not move the running executable aside; the runner is unchanged")
	}
	if err := os.Rename(candidate, target); err != nil {
		if rollbackErr := os.Rename(backup, target); rollbackErr != nil {
			return true, fmt.Errorf("update failed; recover the previous executable from %q", backup)
		}
		return false, errors.New("could not install the update; the previous executable was restored")
	}
	// A running image may remain locked until it exits. Retain that recovery
	// copy beside the notices if Windows cannot remove it yet.
	_ = os.Remove(backup)
	return false, nil
}
