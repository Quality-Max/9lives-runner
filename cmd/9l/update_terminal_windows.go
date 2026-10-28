package main

import (
	"golang.org/x/sys/windows"
	"os"
)

func updateTerminal(file *os.File) bool {
	var mode uint32
	return windows.GetConsoleMode(windows.Handle(file.Fd()), &mode) == nil
}
