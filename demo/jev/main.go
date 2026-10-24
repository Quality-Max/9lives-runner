// The illustrative demo uses only its embedded synthetic fixtures.
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
	AssertionPassed bool
	CatBoxes        []rectangle
	OrderCount      *int
	Scenario        string
	ObservedAt      time.Time
}
type report struct {
	Live, Images        bool
	Scenario            string
	Capture             capture
	Observed, Predicted *rectangle
	Declined, Confirmed *float64
	Record              jev.Record
	RecordJSON          string
	Cats                *catResult
}

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	os.Exit(run(ctx, os.Args[1:], os.Stdout, os.Stderr))
}

func run(ctx context.Context, args []string, out, errOut io.Writer) int {
	fs := flag.NewFlagSet("jev-demo", flag.ContinueOnError)
	fs.SetOutput(errOut)
	live := fs.Bool("live", false, "enable real TypeSafe requests (requires TYPESAFE_API_KEY)")
	images := fs.Bool("images", false, "include only the demo's synthetic screenshot and a Box question")
	serve := fs.Bool("serve", false, "open the interactive local demo server")
	port := fs.Int("port", 4175, "loopback port for --serve (0 chooses a free port)")
	open := fs.Bool("open", false, "open the interactive demo in your browser")
	scenario := fs.String("scenario", "declined", "synthetic scene: declined, success, cats or cat-decoys")
	model := fs.String("model", jev.DefaultModel, "explicit Jev model")
	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0
		}
		return 2
	}
	if fs.NArg() != 0 || !validScenario(*scenario) || *port < 0 || *port > 65535 || (*open && !*serve) {
		fmt.Fprintln(errOut, "Use --serve [--open] [--port 4175], --live, --images, --model and --scenario declined|success|cats|cat-decoys.")
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
	if *serve {
		return serveDemo(ctx, config, *port, *open, out, errOut)
	}
	ctx, cancel := context.WithTimeout(ctx, 90*time.Second)
	defer cancel()
	dir, r, err := executeDemo(ctx, config, *scenario)
	if dir == "" {
		fmt.Fprintln(errOut, err)
		return 2
	}
	if r.Cats != nil {
		fmt.Fprintf(out, "Executed Playwright scene checks: %d cats, taps and decoys verified.\n", r.Cats.ActualCount)
	} else if r.Capture.AssertionFailed {
		fmt.Fprintln(out, "Executed Playwright assertion: expected one saved notebook order FAILED, as intended.")
	} else {
		fmt.Fprintln(out, "Executed Playwright assertion: one saved notebook order PASSED.")
	}
	fmt.Fprintf(out, "Jev: %s; requests: %d; elapsed: %d ms\n", r.Record.Status, r.Record.RequestCount, r.Record.DurationMS)
	if r.Declined != nil && r.Confirmed != nil {
		fmt.Fprintf(out, "Typed inference: payment declined %.3f; order confirmed %.3f (probabilities, not test verdicts).\n", *r.Declined, *r.Confirmed)
	}
	fmt.Fprintln(out, "Report:", filepath.Join(dir, "index.html"))
	if err != nil {
		fmt.Fprintln(errOut, "Jev request failed:", err)
		return 2
	}
	return 0
}

func executeDemo(ctx context.Context, config jev.Config, scenario string) (string, report, error) {
	if !validScenario(scenario) || (isCatScenario(scenario) && config.Enabled && config.Images != "synthetic") {
		return "", report{}, errors.New("Cat scenes require --images for live Jev requests.")
	}
	revisionCmd := exec.CommandContext(ctx, "git", "rev-parse", "HEAD")
	revision, err := revisionCmd.Output()
	if err != nil {
		return "", report{}, errors.New("Run this demo from the repository root with Git available.")
	}
	if err := os.MkdirAll(".context", 0700); err != nil {
		return "", report{}, errors.New("Could not prepare demo output.")
	}
	dir, err := os.MkdirTemp(".context", "jev-demo-")
	if err != nil {
		return "", report{}, errors.New("Could not prepare demo output.")
	}
	var captured capture
	if err := captureFixture(ctx, dir, &captured, scenario); err != nil {
		return "", report{}, err
	}
	var nonce [16]byte
	if _, err := rand.Read(nonce[:]); err != nil {
		return "", report{}, errors.New("Could not create demo identity.")
	}
	source := sha256.Sum256(fixtureHTML(scenario))
	binding := jev.Binding{RunID: "demo-" + hex.EncodeToString(nonce[:]), JobID: "checkout", AttemptID: "attempt-1", Target: "web", Revision: strings.TrimSpace(string(revision)), SourceSHA256: hex.EncodeToString(source[:]), TestID: "checkout-confirmation", StepID: "place-order", ObservationID: scenario + "-checkout"}
	if isCatScenario(scenario) {
		binding.JobID, binding.TestID, binding.StepID, binding.ObservationID = "cat-hunt", "cat-targets", "inspect-room", scenario+"-room"
	}
	// Keep the ledger and assertion verdict out of the model's state. Jev judges
	// only customer-facing clues, rather than receiving the answer from the test.
	observation := jev.Observation{Binding: binding, ObservedAt: captured.ObservedAt, Snapshot: captured.Snapshot}
	contract := jev.Contract{Version: jev.Version, Task: "synthetic-checkout", QuestionVersion: "v3", CriteriaVersion: "v1", Questions: map[string]jev.Question{
		"declined":  {Type: "noul", Instructions: "Within this pretend shop, does the customer-facing checkout indicate that the payment failed? Interpret the meaning of `accessible_observation` and the screenshot when present. Judge the customer's payment outcome from the messages, not merely the button label or color."},
		"confirmed": {Type: "noul", Instructions: "Within this pretend shop, does the customer-facing checkout indicate that the notebook purchase completed successfully? Interpret the meaning of `accessible_observation` and the screenshot when present. A thank-you message about delivery can confirm success without a literal status label. A button or color alone does not confirm success."},
	}}
	if isCatScenario(scenario) {
		contract = catContract(config.Images == "synthetic")
	}
	if config.Images == "synthetic" {
		raw, err := os.ReadFile(filepath.Join(dir, "checkout.png"))
		if err != nil {
			return "", report{}, errors.New("Could not read synthetic screenshot.")
		}
		observation.Image, err = jev.PrepareImage(binding, captured.ObservedAt, raw, jev.Transform{ScaleX: 1, ScaleY: 1}, "synthetic", nil)
		if err != nil {
			return "", report{}, errors.New("Synthetic screenshot did not satisfy image policy.")
		}
		if !isCatScenario(scenario) {
			contract.Questions["retry"] = jev.Question{Type: "box", Instructions: "Locate the checkout action button labeled Try again or Start again in `screenshot`. Return its bounding box."}
		}
	}
	limits := jev.DefaultLimits()
	limits.MaxRequests, limits.MaxQuestions, limits.MaxBytes, limits.Timeout = 1, 3, 2<<20, 20*time.Second
	client, err := jev.New(ctx, config, runner.AttemptIdentity{RunID: binding.RunID, JobID: binding.JobID, AttemptID: binding.AttemptID, Target: binding.Target}, binding.Revision, binding.SourceSHA256, limits)
	if err != nil {
		return "", report{}, errors.New("Could not create bounded demo client.")
	}
	defer client.Close()
	result, decisionErr := client.Decide(ctx, binding, observation, contract)
	r := report{Live: config.Enabled, Images: config.Images == "synthetic", Scenario: scenario, Capture: captured, Record: result.Record, Observed: &rectangle{X: captured.Bounds.X / 850 * 100, Y: captured.Bounds.Y / 600 * 100, Width: captured.Bounds.Width / 850 * 100, Height: captured.Bounds.Height / 600 * 100}}
	if decisionErr == nil {
		r.Declined = result.Answers["declined"].Noul
		r.Confirmed = result.Answers["confirmed"].Noul
		boxQuestion := "retry"
		if isCatScenario(scenario) {
			boxQuestion = "cat"
		}
		if box := result.Answers[boxQuestion].Box; box != nil {
			r.Predicted = &rectangle{X: box.Left * 100, Y: box.Top * 100, Width: (box.Right - box.Left) * 100, Height: (box.Bottom - box.Top) * 100}
		}
	}
	if isCatScenario(scenario) {
		r.Observed = nil
		r.Cats = compareCats(captured, result.Answers, r.Predicted)
	}
	if err := writeReport(dir, r); err != nil {
		return "", report{}, errors.New("Could not write demo report.")
	}
	if errors.Is(decisionErr, jev.ErrDisabled) {
		decisionErr = nil
	}
	return dir, r, decisionErr
}

func captureFixture(ctx context.Context, dir string, captured *capture, scenario string) error {
	script := "demo/jev/capture.cjs"
	if isCatScenario(scenario) {
		script = "demo/jev/capture-cats.cjs"
	}
	cmd := exec.Command("node", script, dir)
	cmd.Env = demoEnvironment()
	cmd.Stdin = bytes.NewReader(fixtureHTML(scenario))
	var stdout cappedBuffer
	cmd.Stdout, cmd.Stderr = &stdout, io.Discard
	if runner.RunOwnedCommand(ctx, cmd) != nil || stdout.overflow || json.Unmarshal(stdout.Bytes(), captured) != nil || captured.Scenario != scenario || captured.Bounds == nil || captured.Bounds.Width <= 0 || captured.Bounds.Height <= 0 || captured.ObservedAt.IsZero() {
		return captureError()
	}
	if isCatScenario(scenario) {
		if !validCatCapture(*captured) {
			return captureError()
		}
	} else {
		expectedOrders := 0
		if scenario == "success" {
			expectedOrders = 1
		}
		if captured.AssertionFailed != (scenario == "declined") || captured.AssertionPassed != (scenario == "success") || captured.OrderCount == nil || *captured.OrderCount != expectedOrders {
			return captureError()
		}
	}
	return nil
}

func captureError() error {
	return errors.New("Synthetic capture failed. Run npm ci and npm exec playwright install chromium from the repository root.")
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
	markup := reportHTML
	if r.Cats != nil {
		markup = catReportHTML
	}
	t, err := template.New("demo").Funcs(template.FuncMap{"prob": func(p *float64) string {
		if p == nil {
			return "No inference"
		}
		return fmt.Sprintf("%.1f%%", *p*100)
	}}).Parse(markup)
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

func fixtureHTML(scenario string) []byte {
	if isCatScenario(scenario) {
		return catFixture(scenario)
	}
	return bytes.Replace(checkout, []byte(`data-scenario="declined"`), []byte(`data-scenario="`+scenario+`"`), 1)
}

func demoEnvironment() []string {
	allowed := []string{}
	for _, key := range []string{"PATH", "HOME", "TMPDIR", "TEMP", "TMP", "SYSTEMROOT", "WINDIR", "LOCALAPPDATA", "PLAYWRIGHT_BROWSERS_PATH", "XDG_CACHE_HOME", "DISPLAY", "WAYLAND_DISPLAY", "XDG_RUNTIME_DIR"} {
		if value, ok := os.LookupEnv(key); ok {
			allowed = append(allowed, key+"="+value)
		}
	}
	return allowed
}
