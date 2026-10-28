//go:build !darwin && !linux && !windows

package main

import "os"

func updateTerminal(*os.File) bool { return false }
