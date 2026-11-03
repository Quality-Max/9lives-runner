package main

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"
)

type fakeStar struct {
	opened, gh []string
	openErr    error
	ghErr      error
}

func (f *fakeStar) options(interactive bool) starOptions {
	return starOptions{
		interactive: interactive,
		open:        func(url string) error { f.opened = append(f.opened, url); return f.openErr },
		gh: func(_ context.Context, args ...string) error {
			f.gh = append(f.gh, strings.Join(args, " "))
			return f.ghErr
		},
	}
}

func runStar(t *testing.T, fake *fakeStar, interactive bool, args ...string) (int, string, string) {
	t.Helper()
	var out, errOut bytes.Buffer
	code := starContext(context.Background(), args, &out, &errOut, fake.options(interactive))
	return code, out.String(), errOut.String()
}

// Outside a terminal, such as under an agent, star only prints the URL: it
// neither opens a browser nor touches the user's GitHub account.
func TestStarWithoutTerminalOnlyPrintsTheRepository(t *testing.T) {
	fake := &fakeStar{}
	code, out, _ := runStar(t, fake, false)
	if code != 0 || !strings.Contains(out, starURL) || len(fake.opened) != 0 || len(fake.gh) != 0 {
		t.Fatalf("code=%d out=%q opened=%v gh=%v", code, out, fake.opened, fake.gh)
	}
}

func TestStarInTerminalOpensTheRepository(t *testing.T) {
	fake := &fakeStar{}
	code, out, _ := runStar(t, fake, true)
	if code != 0 || len(fake.opened) != 1 || fake.opened[0] != starURL || !strings.Contains(out, "Opened in your browser") || len(fake.gh) != 0 {
		t.Fatalf("code=%d out=%q opened=%v gh=%v", code, out, fake.opened, fake.gh)
	}
	fake = &fakeStar{openErr: errors.New("no opener")}
	if code, out, _ := runStar(t, fake, true); code != 0 || !strings.Contains(out, starURL) || strings.Contains(out, "Opened") {
		t.Fatalf("failed open: code=%d out=%q", code, out)
	}
}

func TestStarWithGHStarsOnlyTheRunnerRepository(t *testing.T) {
	fake := &fakeStar{}
	code, out, _ := runStar(t, fake, false, "--gh")
	if code != 0 || len(fake.gh) != 1 || fake.gh[0] != "repo star Quality-Max/9lives-runner" || !strings.Contains(out, "Starred") || len(fake.opened) != 0 {
		t.Fatalf("code=%d out=%q gh=%v opened=%v", code, out, fake.gh, fake.opened)
	}
	fake = &fakeStar{ghErr: errors.New("gh is not on PATH")}
	if code, out, errOut := runStar(t, fake, false, "--gh"); code != 1 || out != "" || !strings.Contains(errOut, starURL) {
		t.Fatalf("gh failure: code=%d out=%q err=%q", code, out, errOut)
	}
}

func TestStarRejectsArguments(t *testing.T) {
	for _, args := range [][]string{{"other/repo"}, {"--repo", "other/repo"}} {
		fake := &fakeStar{}
		if code, _, _ := runStar(t, fake, true, args...); code != exitUsage || len(fake.gh) != 0 || len(fake.opened) != 0 {
			t.Fatalf("%v: code=%d gh=%v opened=%v", args, code, fake.gh, fake.opened)
		}
	}
}

func TestStarHintOnlyForPeopleAtATerminal(t *testing.T) {
	for _, tc := range []struct {
		terminal     bool
		ci, disabled string
		want         bool
	}{
		{true, "", "", true},
		{false, "", "", false},
		{true, "true", "", false},
		{true, "", "1", false},
		{true, "", "0", true},
	} {
		if got := starHintAllowed(tc.terminal, tc.ci, tc.disabled); got != tc.want {
			t.Fatalf("terminal=%v ci=%q disabled=%q: got %v", tc.terminal, tc.ci, tc.disabled, got)
		}
	}
}

// Help, version and doctor write the hint to stderr only when stderr is a
// terminal, so piped and captured output stays exactly as before.
func TestStarHintStaysOutOfCapturedOutput(t *testing.T) {
	for _, args := range [][]string{{"--help"}, {"version"}, {"version", "--format", "json"}} {
		var out, errOut bytes.Buffer
		if code := run(args, &out, &errOut); code != 0 || strings.Contains(out.String()+errOut.String(), "9l star\n") || errOut.Len() != 0 {
			t.Fatalf("%v: code=%d out=%q err=%q", args, code, out.String(), errOut.String())
		}
	}
	var out bytes.Buffer
	run([]string{"--help"}, &out, &bytes.Buffer{})
	if !strings.Contains(out.String(), "9l star [--gh]") {
		t.Fatal("usage omits 9l star")
	}
}
