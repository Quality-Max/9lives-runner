//go:build !windows

package selfupdate

import (
	"errors"
	"os"
)

func replaceExecutable(candidate, target, backup string) (bool, error) {
	if err := os.Rename(candidate, target); err != nil {
		return false, errors.New("could not replace the runner; the existing executable is unchanged")
	}
	return false, nil
}
