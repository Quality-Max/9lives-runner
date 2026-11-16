package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"os/signal"
	"runtime"
	"syscall"
	"time"
)

const (
	starRepository = "Quality-Max/9lives-runner"
	starURL        = "https://github.com/" + starRepository
)

type starOptions struct {
	interactive bool
	open        func(string) error
	gh          func(context.Context, ...string) error
}

func starCommand(args []string, out, errOut io.Writer) int {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	return starContext(ctx, args, out, errOut, starOptions{
		interactive: updateTerminal(os.Stdin) && updateTerminal(os.Stdout),
		open:        openBrowser, gh: runGH,
	})
}

// starContext only ever acts when a person runs `9l star`: it prints the
// repository, opens it in a terminal session, and stars it through the
// user's own gh login only under --gh. Nothing else in 9l stars for them.
func starContext(ctx context.Context, args []string, out, errOut io.Writer, options starOptions) int {
	fs := flag.NewFlagSet("star", flag.ContinueOnError)
	fs.SetOutput(errOut)
	gh := fs.Bool("gh", false, "star the repository with the GitHub CLI, using its login")
	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0
		}
		return exitUsage
	}
	if fs.NArg() != 0 {
		fmt.Fprintln(errOut, "9l: star takes only --gh")
		return exitUsage
	}
	if *gh {
		// gh has no star subcommand; PUT /user/starred is the API operation.
		if err := options.gh(ctx, "api", "--method", "PUT", "--silent", "/user/starred/"+starRepository); err != nil {
			fmt.Fprintf(errOut, "9l: gh could not star %s (%v); star it at %s\n", starRepository, err, starURL)
			return 1
		}
		fmt.Fprintf(out, "Starred %s. Thank you!\n", starRepository)
		return 0
	}
	fmt.Fprintf(out, "Star 9lives on GitHub: %s\n", starURL)
	if options.interactive && options.open(starURL) == nil {
		fmt.Fprintln(out, "Opened in your browser. Or run: 9l star --gh")
	}
	return 0
}

func runGH(ctx context.Context, args ...string) error {
	path, err := exec.LookPath("gh")
	if err != nil {
		return errors.New("gh is not on PATH")
	}
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	command := exec.CommandContext(ctx, path, args...)
	command.Stdout, command.Stderr = io.Discard, os.Stderr
	return command.Run()
}

// openBrowser hands the fixed repository URL to the desktop opener and does
// not wait for it; the browser must outlive this process.
func openBrowser(url string) error {
	var command *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		command = exec.Command("open", url)
	case "windows":
		command = exec.Command("rundll32", "url.dll,FileProtocolHandler", url)
	default:
		command = exec.Command("xdg-open", url)
	}
	if err := command.Start(); err != nil {
		return err
	}
	return command.Process.Release()
}

// starHint is a footer for people at a terminal. It goes to stderr only when
// both the command's output and stderr are terminals, outside CI and unless
// NINELIVES_NO_HINTS=1, so it never accompanies redirected output, receipts,
// MCP responses or agent pipes.
func starHint(out, errOut io.Writer) {
	if !starHintAllowed(isTerminal(out), isTerminal(errOut), os.Getenv("CI"), os.Getenv("NINELIVES_NO_HINTS")) {
		return
	}
	fmt.Fprintln(errOut, "\n★ Like 9l? Star it on GitHub: 9l star")
}

func isTerminal(w io.Writer) bool {
	file, ok := w.(*os.File)
	return ok && updateTerminal(file)
}

func starHintAllowed(outTerminal, errTerminal bool, ci, disabled string) bool {
	return outTerminal && errTerminal && ci == "" && disabled != "1"
}
