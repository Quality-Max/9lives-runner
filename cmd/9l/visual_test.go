package main

import (
	"bytes"
	"runtime"
	"strings"
	"testing"

	"github.com/Quality-Max/9lives-runner/internal/runner"
)

func TestDisplayAvailable(t *testing.T) {
	env := func(values map[string]string) func(string) string {
		return func(key string) string { return values[key] }
	}
	cases := []struct {
		goos   string
		values map[string]string
		want   bool
	}{
		{"darwin", nil, true},
		{"windows", nil, true},
		{"linux", nil, false},
		{"linux", map[string]string{"DISPLAY": ":99"}, true},
		{"linux", map[string]string{"WAYLAND_DISPLAY": "wayland-0"}, true},
		{"freebsd", nil, false},
	}
	for _, test := range cases {
		if got := displayAvailable(test.goos, env(test.values)); got != test.want {
			t.Errorf("%s %v: got %v", test.goos, test.values, got)
		}
	}
}

func TestHeadedAppendsPlaywrightFlagToEveryJob(t *testing.T) {
	plan := runner.Plan{Jobs: []runner.Job{{Command: []string{"playwright", "test", "a.spec.ts"}}, {Command: []string{"playwright", "test", "b.spec.ts"}}}}
	visualMode{}.apply(&plan)
	if strings.Contains(strings.Join(plan.Jobs[0].Command, " "), "--headed") {
		t.Fatal("headless mode changed the command")
	}
	visualMode{headed: true}.apply(&plan)
	for _, job := range plan.Jobs {
		if job.Command[len(job.Command)-1] != "--headed" {
			t.Fatalf("command %q", job.Command)
		}
	}
}

func TestHeadedRunWithoutDisplayIsRefusedBeforePlanning(t *testing.T) {
	if runtime.GOOS == "darwin" || runtime.GOOS == "windows" {
		t.Skip("macOS and Windows always have a display")
	}
	t.Setenv("DISPLAY", "")
	t.Setenv("WAYLAND_DISPLAY", "")
	var stdout, stderr bytes.Buffer
	if code := run([]string{"run", "missing.spec.ts", "--headed"}, &stdout, &stderr); code != 2 || !strings.Contains(stderr.String(), "xvfb-run") {
		t.Fatalf("code=%d stderr=%s", code, stderr.String())
	}
	// Planning and dry runs launch no browser, so they need no display.
	if _, err := visualOptions(true, false); err != nil {
		t.Fatal(err)
	}
}
