package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Quality-Max/9lives-runner/internal/selfupdate"
)

type fakeUpdater struct {
	checks, installs     atomic.Int32
	checkErr, installErr error
	available            bool
	entered, wait        chan struct{}
}

func (u *fakeUpdater) Check(context.Context) (selfupdate.Release, error) {
	u.checks.Add(1)
	if u.entered != nil {
		close(u.entered)
		<-u.wait
	}
	return selfupdate.Release{Current: "0.5.3", Latest: "0.5.4", Available: u.available}, u.checkErr
}
func (u *fakeUpdater) Install(_ context.Context, _ selfupdate.Release, target string) (selfupdate.Installation, error) {
	u.installs.Add(1)
	return selfupdate.Installation{Version: "0.5.4", NoticesDirectory: "release-notices"}, u.installErr
}

func TestUpdateCommandRequiresConsentAndCheckNeverInstalls(t *testing.T) {
	for _, tc := range []struct {
		name           string
		args           []string
		input          string
		terminal       bool
		code, installs int
	}{
		{"check", []string{"--check"}, "yes\n", true, 0, 0},
		{"nonterminal", nil, "yes\n", false, 2, 0},
		{"decline", nil, "n\n", true, 0, 0},
		{"default-no", nil, "\n", true, 0, 0},
		{"not-an-approval", nil, "yes extra\n", true, 0, 0},
		{"approve", nil, "yes\n", true, 0, 1},
		{"explicit-yes", []string{"--yes"}, "", false, 0, 1},
		{"conflicting", []string{"--yes", "--check"}, "", true, 2, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			u := &fakeUpdater{available: true}
			var stdout, stderr bytes.Buffer
			located := 0
			code := updateContext(context.Background(), tc.args, &stdout, &stderr, updateOptions{service: u,
				input: strings.NewReader(tc.input), interactive: tc.terminal,
				executable: func() (string, error) { located++; return "installed-runner", nil },
			})
			if code != tc.code || int(u.installs.Load()) != tc.installs || located != tc.installs {
				t.Fatalf("code=%d installs=%d located=%d", code, u.installs.Load(), located)
			}
			if tc.installs == 1 && !strings.Contains(stdout.String(), "npm install --save-dev @9l/playwright@0.5.4") {
				t.Fatal("separate SDK upgrade command missing")
			}
			if len(tc.args) > 0 && tc.args[0] == "--check" && strings.Contains(stderr.String(), "[y/N]") {
				t.Fatal("check prompted")
			}
		})
	}
}

func TestUpdateFailureAndCancellationCannotInstall(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	u := &fakeUpdater{available: true}
	var out bytes.Buffer
	code := updateContext(ctx, []string{"--yes"}, &out, &out, updateOptions{service: u,
		executable: func() (string, error) { t.Fatal("canceled update located executable"); return "", nil },
	})
	if code != 1 || u.installs.Load() != 0 {
		t.Fatal("canceled update installed")
	}
	u = &fakeUpdater{checkErr: errors.New("release unavailable")}
	code = updateContext(context.Background(), []string{"--yes"}, &out, &out, updateOptions{service: u})
	if code != 1 || u.installs.Load() != 0 {
		t.Fatal("failed release check installed")
	}
	u = &fakeUpdater{available: false}
	code = updateContext(context.Background(), nil, &out, &out, updateOptions{service: u})
	if code != 0 || u.installs.Load() != 0 {
		t.Fatal("current version installed again")
	}
}

func TestAutomaticUpdateEligibilityProtectsNoninteractiveProtocols(t *testing.T) {
	for _, args := range [][]string{
		{"run", "tests/a.spec.ts", "--format", "json"}, {"run", "tests/a.spec.ts", "--format=json"},
		{"mcp"}, {"version"}, {"plan", "tests/a.spec.ts"}, {"run", "--help"}, {"run", "tests/a.spec.ts", "--dry-run"},
		{"run", "tests/a.spec.ts", "--dry-run=1"}, {"run", "tests/a.spec.ts", "--dry-run=TRUE"},
		{"run", "--help=true"}, {"run", "-h=false"},
	} {
		if automaticUpdateAllowed(args, 0, true, true, "", "") {
			t.Fatalf("automatic update allowed for %v", args)
		}
	}
	args := []string{"run", "tests/a.spec.ts"}
	for _, tc := range []struct {
		code          int
		input, output bool
		ci, disabled  string
	}{
		{0, false, true, "", ""}, {0, true, false, "", ""}, {0, true, true, "true", ""},
		{0, true, true, "", "1"}, {2, true, true, "", ""}, {3, true, true, "", ""},
	} {
		if automaticUpdateAllowed(args, tc.code, tc.input, tc.output, tc.ci, tc.disabled) {
			t.Fatalf("automatic update accepted %+v", tc)
		}
	}
	for _, code := range []int{0, 1} {
		if !automaticUpdateAllowed(args, code, true, true, "", "") {
			t.Fatal("interactive completed run was not eligible")
		}
	}
}

func TestAutomaticCheckCacheDoesNotAuthorizeAnInstall(t *testing.T) {
	cache := filepath.Join(t.TempDir(), "update-check.json")
	now := time.Now()
	u := &fakeUpdater{available: true}
	var out bytes.Buffer
	options := updateOptions{service: u, input: strings.NewReader("no\n"), interactive: true,
		executable: func() (string, error) { t.Fatal("declined offer located runner"); return "", nil }}
	offerUpdate(context.Background(), cache, now, options, &out)
	options.input = strings.NewReader("yes\n")
	offerUpdate(context.Background(), cache, now.Add(time.Hour), options, &out)
	if u.checks.Load() != 1 || u.installs.Load() != 0 {
		t.Fatal("cache caused repeated check or unauthorized install")
	}
	if strings.Count(out.String(), "[y/N]") != 1 {
		t.Fatal("offer repeated within a day")
	}
	options.input = strings.NewReader("no\n")
	offerUpdate(context.Background(), cache, now.Add(25*time.Hour), options, &out)
	if u.checks.Load() != 2 || u.installs.Load() != 0 {
		t.Fatal("expired cache did not recheck")
	}
	data, _ := os.ReadFile(cache)
	var record updateCheckRecord
	if json.Unmarshal(data, &record) != nil || record.Version != 1 || strings.Contains(string(data), "0.5.4") {
		t.Fatal("cache retained a release decision")
	}
}

func TestAutomaticConcurrentRunsCheckOnlyOnce(t *testing.T) {
	cache := filepath.Join(t.TempDir(), "check.json")
	u := &fakeUpdater{entered: make(chan struct{}), wait: make(chan struct{})}
	options := updateOptions{service: u}
	done := make(chan struct{})
	go func() { offerUpdate(context.Background(), cache, time.Now(), options, &bytes.Buffer{}); close(done) }()
	<-u.entered
	offerUpdate(context.Background(), cache, time.Now(), options, &bytes.Buffer{})
	close(u.wait)
	<-done
	if u.checks.Load() != 1 {
		t.Fatal("concurrent runs both checked")
	}
}

func TestOfflineAutomaticCheckIsSilentAndRateLimited(t *testing.T) {
	cache := filepath.Join(t.TempDir(), "check.json")
	u := &fakeUpdater{checkErr: errors.New("offline")}
	var out bytes.Buffer
	options := updateOptions{service: u, input: strings.NewReader("yes\n"), interactive: true}
	offerUpdate(context.Background(), cache, time.Now(), options, &out)
	offerUpdate(context.Background(), cache, time.Now().Add(time.Minute), options, &out)
	if out.Len() != 0 || u.checks.Load() != 1 || u.installs.Load() != 0 {
		t.Fatal("offline check disrupted a run")
	}
}
