// The illustrative demo uses only its embedded synthetic checkout fixture.
package main

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	_ "embed"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"html/template"
	"io"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/Quality-Max/9lives-runner/internal/jev"
	"github.com/Quality-Max/9lives-runner/internal/runner"
)

//go:embed checkout.html
var checkout []byte

//go:embed report.html
var reportHTML string

type rectangle struct{ X, Y, Width, Height float64 }
type capture struct {
	Snapshot        string
	Bounds          *rectangle
	AssertionFailed bool
	ObservedAt      time.Time
}
type report struct {
	Live, Images        bool
	Capture             capture
	Observed, Predicted *rectangle
	Declined, Confirmed *float64
	Record              jev.Record
	RecordJSON          string
}

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	os.Exit(run(ctx, os.Args[1:], os.Stdout, os.Stderr))
}

func run(ctx context.Context, args []string, out, errOut io.Writer) int {
	fs := flag.NewFlagSet("jev-demo", flag.ContinueOnError)
	fs.SetOutput(errOut)
	live := fs.Bool("live", false, "make one real TypeSafe request (requires TYPESAFE_API_KEY)")
	images := fs.Bool("images", false, "include only the demo's synthetic screenshot and a Box question")
	model := fs.String("model", jev.DefaultModel, "explicit Jev model")
	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0
		}
		return 2
	}
	if fs.NArg() != 0 {
		fmt.Fprintln(errOut, "Demo takes --live, --images and --model only.")
		return 2
	}
	config := jev.Config{Enabled: *live, Model: *model, Images: "off"}
	if *images {
		config.Images = "synthetic"
	}
	preflight, err := jev.CheckConfig(config)
	if err != nil {
		fmt.Fprintln(errOut, "Invalid demo model configuration.")
		return 2
	}
	if *live && !preflight.CredentialPresent {
		fmt.Fprintln(errOut, "Set TYPESAFE_API_KEY in the Go process environment before using --live. No API request made.")
		return 2
	}
	ctx, cancel := context.WithTimeout(ctx, 90*time.Second)
	defer cancel()
	revisionCmd := exec.CommandContext(ctx, "git", "rev-parse", "HEAD")
	revision, err := revisionCmd.Output()
	if err != nil {
		fmt.Fprintln(errOut, "Run this demo from the repository root with Git available.")
		return 2
	}
	if err := os.MkdirAll(".context", 0700); err != nil {
		fmt.Fprintln(errOut, "Could not prepare demo output.")
		return 2
	}
	dir, err := os.MkdirTemp(".context", "jev-demo-")
	if err != nil {
		fmt.Fprintln(errOut, "Could not prepare demo output.")
		return 2
	}
	if !*live {
		fmt.Fprintln(out, "Offline mode: no API requests. Capturing the synthetic failed checkout.")
	}
	var captured capture
	if err := captureFixture(ctx, dir, &captured); err != nil {
		fmt.Fprintln(errOut, err)
		return 2
	}
	var nonce [16]byte
	if _, err := rand.Read(nonce[:]); err != nil {
		fmt.Fprintln(errOut, "Could not create demo identity.")
		return 2
	}
	source := sha256.Sum256(checkout)
	binding := jev.Binding{RunID: "demo-" + hex.EncodeToString(nonce[:]), JobID: "checkout", AttemptID: "attempt-1", Target: "web", Revision: strings.TrimSpace(string(revision)), SourceSHA256: hex.EncodeToString(source[:]), TestID: "checkout-confirmation", StepID: "place-order", ObservationID: "declined-checkout"}
	observation := jev.Observation{Binding: binding, ObservedAt: captured.ObservedAt, Snapshot: captured.Snapshot, FailureClass: runner.FailureAssertion}
	contract := jev.Contract{Version: jev.Version, Task: "synthetic-checkout", QuestionVersion: "v1", CriteriaVersion: "v1", Questions: map[string]jev.Question{
		"declined":  {Type: "noul", Instructions: "Does the observed checkout explicitly show that payment was declined? Use `accessible_observation` and the screenshot when present."},
		"confirmed": {Type: "noul", Instructions: "Does the observed checkout explicitly confirm that an order was placed successfully? Use `accessible_observation` and the screenshot when present."},
	}}
	if *images {
		raw, err := os.ReadFile(filepath.Join(dir, "checkout.png"))
		if err != nil {
			fmt.Fprintln(errOut, "Could not read synthetic screenshot.")
			return 2
		}
		observation.Image, err = jev.PrepareImage(binding, captured.ObservedAt, raw, jev.Transform{ScaleX: 1, ScaleY: 1}, "synthetic", nil)
		if err != nil {
			fmt.Fprintln(errOut, "Synthetic screenshot did not satisfy image policy.")
			return 2
		}
		contract.Questions["retry"] = jev.Question{Type: "box", Instructions: "Locate the button labeled Try again in `screenshot`. Return its bounding box."}
	}
	limits := jev.DefaultLimits()
	limits.MaxRequests, limits.MaxQuestions, limits.MaxBytes, limits.Timeout = 1, 3, 2<<20, 20*time.Second
	client, err := jev.New(ctx, config, runner.AttemptIdentity{RunID: binding.RunID, JobID: binding.JobID, AttemptID: binding.AttemptID, Target: binding.Target}, binding.Revision, binding.SourceSHA256, limits)
	if err != nil {
		fmt.Fprintln(errOut, "Could not create bounded demo client.")
		return 2
	}
	defer client.Close()
	result, decisionErr := client.Decide(ctx, binding, observation, contract)
	r := report{Live: *live, Images: *images, Capture: captured, Record: result.Record, Observed: &rectangle{X: captured.Bounds.X / 850 * 100, Y: captured.Bounds.Y / 600 * 100, Width: captured.Bounds.Width / 850 * 100, Height: captured.Bounds.Height / 600 * 100}}
	if decisionErr == nil {
		r.Declined = result.Answers["declined"].Noul
		r.Confirmed = result.Answers["confirmed"].Noul
		if box := result.Answers["retry"].Box; box != nil {
			r.Predicted = &rectangle{X: box.Left * 100, Y: box.Top * 100, Width: (box.Right - box.Left) * 100, Height: (box.Bottom - box.Top) * 100}
		}
	}
	if err := writeReport(dir, r); err != nil {
		fmt.Fprintln(errOut, "Could not write demo report.")
		return 2
	}
	fmt.Fprintln(out, "Executed Playwright assertion: expected order confirmation FAILED, as intended.")
	fmt.Fprintf(out, "Jev: %s; requests: %d; elapsed: %d ms\n", result.Record.Status, result.Record.RequestCount, result.Record.DurationMS)
	if r.Declined != nil && r.Confirmed != nil {
		fmt.Fprintf(out, "Typed inference: payment declined %.3f; order confirmed %.3f (probabilities, not test verdicts).\n", *r.Declined, *r.Confirmed)
	}
	fmt.Fprintln(out, "Report:", filepath.Join(dir, "index.html"))
	if decisionErr != nil && !errors.Is(decisionErr, jev.ErrDisabled) {
		fmt.Fprintln(errOut, "Jev request failed:", decisionErr)
		return 2
	}
	return 0
}

func captureFixture(ctx context.Context, dir string, captured *capture) error {
	cmd := exec.Command("node", "demo/jev/capture.cjs", dir)
	cmd.Env = []string{}
	// Neither the Node helper nor its browser inherits provider credentials.
	for _, key := range []string{"PATH", "HOME", "TMPDIR", "TEMP", "TMP", "SYSTEMROOT", "WINDIR", "LOCALAPPDATA", "PLAYWRIGHT_BROWSERS_PATH", "XDG_CACHE_HOME"} {
		if value, ok := os.LookupEnv(key); ok {
			cmd.Env = append(cmd.Env, key+"="+value)
		}
	}
	cmd.Stdin = bytes.NewReader(checkout)
	var stdout cappedBuffer
	cmd.Stdout, cmd.Stderr = &stdout, io.Discard
	if runner.RunOwnedCommand(ctx, cmd) != nil || stdout.overflow || json.Unmarshal(stdout.Bytes(), captured) != nil || !captured.AssertionFailed || captured.Bounds == nil || captured.Bounds.Width <= 0 || captured.Bounds.Height <= 0 || captured.ObservedAt.IsZero() {
		return errors.New("Synthetic capture failed. Run npm ci and npm exec playwright install chromium from the repository root.")
	}
	return nil
}

type cappedBuffer struct {
	bytes.Buffer
	overflow bool
}

func (b *cappedBuffer) Write(p []byte) (int, error) {
	if b.Len()+len(p) > 32<<10 {
		b.overflow = true
		return len(p), nil
	}
	return b.Buffer.Write(p)
}

func writeReport(dir string, r report) error {
	raw, err := json.MarshalIndent(r.Record, "", "  ")
	if err != nil {
		return err
	}
	if err = os.WriteFile(filepath.Join(dir, "record.json"), append(raw, '\n'), 0600); err != nil {
		return err
	}
	r.RecordJSON = string(raw)
	t, err := template.New("demo").Funcs(template.FuncMap{"prob": func(p *float64) string {
		if p == nil {
			return "No inference"
		}
		return fmt.Sprintf("%.1f%%", *p*100)
	}}).Parse(reportHTML)
	if err != nil {
		return err
	}
	f, err := os.OpenFile(filepath.Join(dir, "index.html"), os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0600)
	if err != nil {
		return err
	}
	defer f.Close()
	return t.Execute(f, r)
}
