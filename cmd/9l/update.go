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
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/Quality-Max/9lives-runner/internal/selfupdate"
)

type updateService interface {
	Check(context.Context) (selfupdate.Release, error)
	Install(context.Context, selfupdate.Release, string) (selfupdate.Installation, error)
}

type updateOptions struct {
	service     updateService
	input       io.Reader
	interactive bool
	executable  func() (string, error)
}

func updateCommand(args []string, out, errOut io.Writer) int {
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	return updateContext(ctx, args, out, errOut, updateOptions{
		service: selfupdate.New(version), input: os.Stdin,
		interactive: updateTerminal(os.Stdin) && updateTerminal(os.Stderr), executable: os.Executable,
	})
}

func updateContext(ctx context.Context, args []string, out, errOut io.Writer, options updateOptions) int {
	fs := flag.NewFlagSet("update", flag.ContinueOnError)
	fs.SetOutput(errOut)
	check := fs.Bool("check", false, "check for a newer stable release without installing")
	yes := fs.Bool("yes", false, "approve installation without an interactive prompt")
	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0
		}
		return exitUsage
	}
	if fs.NArg() != 0 || (*check && *yes) {
		fmt.Fprintln(errOut, "9l: use 9l update [--check | --yes]")
		return exitUsage
	}
	release, err := options.service.Check(ctx)
	if err != nil {
		fmt.Fprintf(errOut, "9l: %v\n", err)
		return 1
	}
	if !release.Available {
		fmt.Fprintf(out, "9l %s; latest stable release is %s. No update needed.\n", release.Current, release.Latest)
		return 0
	}
	fmt.Fprintf(out, "9l update available: %s → %s\n%s\n", release.Current, release.Latest, selfupdate.ReleaseURL(release))
	if *check {
		return 0
	}
	if !*yes {
		if !options.interactive {
			fmt.Fprintln(errOut, "9l: installation needs approval; use 9l update in a terminal or 9l update --yes")
			return exitUsage
		}
		question := fmt.Sprintf("Install 9l %s? [y/N]: ", release.Latest)
		if !readApproval(ctx, options.input, errOut, question) {
			fmt.Fprintln(out, "No update installed.")
			if ctx.Err() != nil {
				return 1
			}
			return 0
		}
	}
	return installUpdate(ctx, release, options, out, errOut)
}

func installUpdate(ctx context.Context, release selfupdate.Release, options updateOptions, out, errOut io.Writer) int {
	if ctx.Err() != nil {
		fmt.Fprintln(errOut, "9l: update canceled")
		return 1
	}
	executable, err := options.executable()
	if err != nil {
		fmt.Fprintln(errOut, "9l: could not locate the installed runner")
		return 1
	}
	installed, err := options.service.Install(ctx, release, executable)
	if err != nil {
		fmt.Fprintf(errOut, "9l: %v\n", err)
		return 1
	}
	fmt.Fprintf(out, "Installed 9l %s. The next invocation uses the new version.\n", installed.Version)
	fmt.Fprintf(out, "License and dependency notices: %s\n", installed.NoticesDirectory)
	fmt.Fprintf(out, "For projects using the SDK: npm install --save-dev @9l/playwright@%s\n", installed.Version)
	return 0
}

// Automatic checks are a terminal-only follow-up, after owned test processes
// have stopped. They cannot change run receipts, exit codes or machine output.
func automaticUpdateAllowed(args []string, code int, inputTerminal, outputTerminal bool, ci, disabled string) bool {
	if len(args) == 0 || args[0] != "run" || (code != 0 && code != 1) || !inputTerminal || !outputTerminal || ci != "" || disabled == "1" {
		return false
	}
	for i, arg := range args[1:] {
		switch arg {
		case "--help", "-h", "-help", "--dry-run", "-dry-run", "--dry-run=true", "-dry-run=true":
			return false
		case "--format", "-format":
			if i+2 < len(args) && args[i+2] != "text" {
				return false
			}
		}
		if strings.HasPrefix(arg, "--format=") || strings.HasPrefix(arg, "-format=") {
			if strings.SplitN(arg, "=", 2)[1] != "text" {
				return false
			}
		}
	}
	return true
}

func offerAutomaticUpdate(args []string, code int) {
	if !automaticUpdateAllowed(args, code, updateTerminal(os.Stdin), updateTerminal(os.Stdout) && updateTerminal(os.Stderr), os.Getenv("CI"), os.Getenv("NINELIVES_NO_UPDATE_CHECK")) {
		return
	}
	cache, err := os.UserCacheDir()
	if err != nil {
		return
	}
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	offerUpdate(ctx, filepath.Join(cache, "9lives-runner", "update-check.json"), time.Now(), updateOptions{
		service: selfupdate.New(version), input: os.Stdin, interactive: true, executable: os.Executable,
	}, os.Stderr)
}

type updateCheckRecord struct {
	Version   int       `json:"version"`
	CheckedAt time.Time `json:"checkedAt"`
}

func offerUpdate(ctx context.Context, cache string, now time.Time, options updateOptions, out io.Writer) {
	if err := os.MkdirAll(filepath.Dir(cache), 0o700); err != nil {
		return
	}
	lease, err := os.OpenFile(cache+".lock", os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		return
	}
	defer func() { _ = lease.Close(); _ = os.Remove(cache + ".lock") }()
	if info, err := os.Lstat(cache); err == nil && info.Mode().IsRegular() && info.Size() <= 4096 {
		file, err := os.Open(cache)
		var data []byte
		if err == nil {
			data, err = io.ReadAll(io.LimitReader(file, 4097))
			_ = file.Close()
		}
		var record updateCheckRecord
		if err == nil && len(data) <= 4096 && json.Unmarshal(data, &record) == nil && record.Version == 1 &&
			!record.CheckedAt.After(now.Add(time.Minute)) && now.Sub(record.CheckedAt) < 24*time.Hour {
			return
		}
	}
	// The cache contains only a check time, not an install decision or URL.
	// A cached response can never authorize or select an executable update.
	file, err := os.CreateTemp(filepath.Dir(cache), ".update-check-")
	if err != nil {
		return
	}
	name := file.Name()
	encodeErr := json.NewEncoder(file).Encode(updateCheckRecord{Version: 1, CheckedAt: now})
	closeErr := file.Close()
	defer os.Remove(name)
	if encodeErr != nil || closeErr != nil {
		return
	}
	if err := os.Rename(name, cache); err != nil {
		return
	}
	release, err := options.service.Check(ctx)
	if err != nil || ctx.Err() != nil {
		return
	}
	if !release.Available || !options.interactive {
		return
	}
	fmt.Fprintf(out, "\n9l update available: %s → %s\n", release.Current, release.Latest)
	if !readApproval(ctx, options.input, out, fmt.Sprintf("Install 9l %s for future runs? [y/N]: ", release.Latest)) {
		return
	}
	_ = installUpdate(ctx, release, options, out, out)
}
