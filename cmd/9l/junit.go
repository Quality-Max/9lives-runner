package main

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/Quality-Max/9lives-runner/internal/report/junit"
	"github.com/Quality-Max/9lives-runner/internal/runner"
)

// junitReport is the --junit option of `9l run` and `9l result`: a JUnit XML
// file written from the run result.
type junitReport struct {
	path string
}

// newJUnitReport checks a --junit path without touching the file system: it
// names a file, not a directory, below an existing directory or one that
// can be created. A path that fails here is a usage error, found before any
// test process starts.
func newJUnitReport(raw string) (*junitReport, error) {
	if strings.TrimSpace(raw) == "" || strings.ContainsAny(raw, "\x00\r\n") || strings.HasSuffix(raw, "/") || strings.HasSuffix(raw, string(filepath.Separator)) {
		return nil, errors.New("--junit needs the path of the XML file to write")
	}
	path, err := filepath.Abs(raw)
	if err != nil {
		return nil, errors.New("--junit needs the path of the XML file to write")
	}
	if info, err := os.Lstat(path); err == nil && !info.Mode().IsRegular() {
		return nil, errors.New("--junit names a directory or special file, not a report file")
	}
	// The nearest existing ancestor decides whether the directory can exist.
	for directory := filepath.Dir(path); ; directory = filepath.Dir(directory) {
		info, err := os.Stat(directory)
		if err == nil && !info.IsDir() {
			return nil, errors.New("--junit names a file below something that is not a directory")
		}
		if err == nil || filepath.Dir(directory) == directory {
			break
		}
	}
	return &junitReport{path: path}, nil
}

// prepare creates the report's directory and proves a file can be written
// there, so a run does not finish only to find its report has nowhere to go.
func (report *junitReport) prepare() error {
	directory := filepath.Dir(report.path)
	if err := os.MkdirAll(directory, 0o755); err != nil {
		return errors.New("--junit: the report directory cannot be created")
	}
	probe, err := os.CreateTemp(directory, ".junit-*")
	if err != nil {
		return errors.New("--junit: the report directory is not writable")
	}
	_ = probe.Close()
	_ = os.Remove(probe.Name())
	return nil
}

// write replaces the report atomically, readable by whatever uploads it. It
// holds only what the receipts hold, redacted again on the way out.
func (report *junitReport) write(summary runner.RunSummary) error {
	// Suites and classnames are named by the spec. A path below the working
	// directory is written relative to it and with forward slashes, so a
	// dashboard sees the same name on every machine and no home directory.
	receipts := make([]runner.Receipt, len(summary.Receipts))
	for index, receipt := range summary.Receipts {
		receipt.Spec = filepath.ToSlash(displayPath(receipt.Spec))
		receipts[index] = receipt
	}
	summary.Receipts = receipts
	content, err := junit.Render(summary)
	if err != nil {
		return err
	}
	temporary, err := os.CreateTemp(filepath.Dir(report.path), ".junit-*")
	if err != nil {
		return err
	}
	defer os.Remove(temporary.Name())
	if _, err = temporary.Write(content); err == nil {
		err = temporary.Chmod(0o644)
	}
	if closeErr := temporary.Close(); err == nil {
		err = closeErr
	}
	if err == nil {
		err = os.Rename(temporary.Name(), report.path)
	}
	return err
}

// finish writes the report after a run or a loaded result and returns the
// exit code. A report that cannot be written makes an otherwise successful
// command exit incomplete; it never lowers a code that already says the run
// failed, was misused or is incomplete.
func (report *junitReport) finish(summary runner.RunSummary, code int, errOut io.Writer) int {
	if report == nil {
		return code
	}
	if err := report.write(summary); err != nil {
		fmt.Fprintf(errOut, "9l: --junit: the report was not written: %v\n", err)
		if code == exitPassed {
			return exitIncomplete
		}
	}
	return code
}
