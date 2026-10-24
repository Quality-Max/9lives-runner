package main

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/Quality-Max/9lives-runner/internal/adapters/playwright"
	"github.com/Quality-Max/9lives-runner/internal/jev"
	"github.com/Quality-Max/9lives-runner/internal/runner"
	"github.com/Quality-Max/9lives-runner/internal/strictjson"
)

// This is one fixed synthetic Playwright example, not the general goal provider.
// Go owns the attempt and decision; Playwright owns handles and assertions.
type huntInput struct {
	ID       string `json:"id"`
	Scenario string `json:"scenario"`
	Live     bool   `json:"live"`
	Broken   bool   `json:"broken"`
}
type huntState struct{ id, dir, base string }

var huntID = regexp.MustCompile(`^[a-f0-9]{32}$`)

type huntCandidate struct {
	ID  int       `json:"id"`
	Box rectangle `json:"box"`
}
type huntDecision struct {
	Resolved bool  `json:"resolved"`
	IDs      []int `json:"ids"`
}

// Each observed spot gets an independent yes/no judgment in one request.
// Only observed crops and visual features are sent, never the answer map.
func huntContract(scenario string, candidates []huntCandidate) jev.Contract {
	c := jev.Contract{Version: jev.Version, Task: "synthetic-picture-actions", QuestionVersion: "v4", CriteriaVersion: "v4", Questions: map[string]jev.Question{}}
	for _, p := range candidates {
		c.Questions[fmt.Sprintf("spot%d", p.ID)] = jev.Question{Type: "noul", Instructions: fmt.Sprintf("In `screenshot`, look only at the enlarged tile labelled Spot %d (row %d, column %d of the 3 by 3 grid). Does the central figure match: %s? Judge the central figure, not other figures in the background. Each tile is a magnified crop from the same fresh game picture. Labels and borders are annotations.", p.ID+1, p.ID/3+1, p.ID%3+1, huntSubject(scenario))}
	}
	return c
}
func chooseHunt(answers map[string]jev.Answer) huntDecision {
	d := huntDecision{IDs: []int{}}
	for id := 0; id < 9; id++ {
		a, ok := answers[fmt.Sprintf("spot%d", id)]
		// Use the documented Noul yes/no starting thresholds for harmless game actions.
		// The complete selection is independently checked before any click.
		if !ok || a.Type != "noul" || a.Noul == nil || math.IsNaN(*a.Noul) || math.IsInf(*a.Noul, 0) || *a.Noul < 0 || *a.Noul > 1 || (*a.Noul > .3 && *a.Noul < .7) {
			return huntDecision{IDs: []int{}}
		}
		if *a.Noul >= .7 {
			d.IDs = append(d.IDs, id)
		}
	}
	d.Resolved = true
	return d
}

// The service lives exactly as long as a runner attempt. Its private address and
// nonce go to the worker; the TypeSafe credential stays in the Go client.
type huntServices struct {
	config                          jev.Config
	dir, revision, source, scenario string
}

func (f huntServices) CredentialNames() []string { return []string{jev.CredentialName} }
func (f huntServices) Start(ctx context.Context, identity runner.AttemptIdentity) (runner.AttemptService, error) {
	service := &huntService{env: map[string]string{"CAT_HUNT_DIR": f.dir}}
	if !f.config.Enabled {
		return service, nil
	}
	identity.Target = "web"
	limits := jev.DefaultLimits()
	limits.MaxRequests = 1
	limits.MaxQuestions = 9
	limits.MaxBytes = 2 << 20
	limits.Timeout = 25 * time.Second
	client, err := jev.New(ctx, f.config, identity, f.revision, f.source, limits)
	if err != nil {
		return nil, err
	}
	service.client = client
	listener, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		client.Close()
		return nil, err
	}
	nonce := make([]byte, 24)
	if _, err = rand.Read(nonce); err != nil {
		listener.Close()
		client.Close()
		return nil, err
	}
	token := hex.EncodeToString(nonce)
	service.env["CAT_HUNT_PROVIDER"] = "http://" + listener.Addr().String() + "/decide"
	service.env["CAT_HUNT_TOKEN"] = token
	var once sync.Mutex
	used := false
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "POST" || r.URL.Path != "/decide" || r.Header.Get("X-Hunt-Token") != token || r.Host != listener.Addr().String() {
			http.NotFound(w, r)
			return
		}
		once.Lock()
		defer once.Unlock()
		if used {
			http.Error(w, "Decision already consumed", http.StatusConflict)
			return
		}
		used = true
		var input struct {
			Candidates []huntCandidate `json:"candidates"`
			PNG        []byte          `json:"png"`
		}
		decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20))
		decoder.DisallowUnknownFields()
		if decoder.Decode(&input) != nil || decoder.Decode(&struct{}{}) != io.EOF || !validHuntCandidates(input.Candidates) {
			http.Error(w, "Invalid observation", 400)
			return
		}
		binding := jev.Binding{RunID: identity.RunID, JobID: identity.JobID, AttemptID: identity.AttemptID, Target: identity.Target, Revision: f.revision, SourceSHA256: f.source, TestID: "cat-hunt", StepID: "choose-spots", ObservationID: "observed-control-crops"}
		now := time.Now().UTC()
		image, err := jev.PrepareImage(binding, now, input.PNG, jev.Transform{ScaleX: 1, ScaleY: 1}, "synthetic", nil)
		if err != nil || image == nil {
			http.Error(w, "Invalid synthetic image", 400)
			return
		}
		// Inputs are bounded controls. The fixture's hidden answer map is absent.
		result, err := client.Decide(r.Context(), binding, jev.Observation{Binding: binding, ObservedAt: now, Snapshot: "A 3 by 3 contact sheet of magnified observed controls, numbered Spot 1 through Spot 9 in row order. These are crops of one fresh synthetic screenshot. Image coordinates refer to this contact sheet; actions use the separately observed original control geometry. Judge only the central figure in the requested tile.", Image: image}, huntContract(f.scenario, input.Candidates))
		raw, _ := json.MarshalIndent(result.Record, "", "  ")
		if os.WriteFile(filepath.Join(f.dir, "jev-record.json"), raw, 0600) != nil {
			http.Error(w, "Could not save safe record", 500)
			return
		}
		if err != nil {
			http.Error(w, "Jev decision unavailable", http.StatusServiceUnavailable)
			return
		}
		writeDemoJSON(w, http.StatusOK, chooseHunt(result.Answers))
	})
	service.server = &http.Server{Handler: handler, ReadHeaderTimeout: 3 * time.Second, ReadTimeout: 5 * time.Second, WriteTimeout: 30 * time.Second, BaseContext: func(net.Listener) context.Context { return ctx }}
	service.done = make(chan struct{})
	go func() { defer close(service.done); _ = service.server.Serve(listener) }()
	return service, nil
}

type huntService struct {
	env    map[string]string
	client *jev.Client
	server *http.Server
	done   chan struct{}
}

func (s *huntService) Environment() map[string]string { return s.env }
func (s *huntService) Close() []runner.GoalReceipt {
	if s.client != nil {
		s.client.Close()
	}
	if s.server != nil {
		s.server.Close()
		<-s.done
	}
	return nil
}
func validHuntCandidates(cs []huntCandidate) bool {
	if len(cs) != 9 {
		return false
	}
	for id, p := range cs {
		b := p.Box
		if p.ID != id {
			return false
		}
		for _, v := range []float64{b.X, b.Y, b.Width, b.Height} {
			if math.IsNaN(v) || math.IsInf(v, 0) {
				return false
			}
		}
		if b.X < 0 || b.Y < 0 || b.Width <= 0 || b.Height <= 0 || b.X+b.Width > 100 || b.Y+b.Height > 100 {
			return false
		}
	}
	return true
}

func executeHunt(ctx context.Context, config jev.Config, input huntInput, dir string) (runner.RunSummary, error) {
	absolute, err := filepath.Abs(dir)
	if err != nil {
		return runner.RunSummary{}, err
	}
	fixture := catFixture(input.Scenario)
	if input.Broken {
		fixture = bytes.Replace(fixture, []byte(`data-broken="false"`), []byte(`data-broken="true"`), 1)
	}
	hash := sha256.Sum256(fixture)
	spec, err := filepath.Abs("demo/jev/picture-hunt.spec.mjs")
	if err != nil {
		return runner.RunSummary{}, err
	}
	configuration, err := filepath.Abs("demo/jev/playwright.config.cjs")
	if err != nil {
		return runner.RunSummary{}, err
	}
	adapter := playwright.New()
	zero := 0
	plan, err := runner.BuildPlan([]string{spec}, runner.PlanOptions{MaxJobs: 1, MaxParallel: 1, MaxAttempts: 1, Deadline: 75 * time.Second, MaxOutputBytes: 1 << 20, Adapters: []runner.Adapter{adapter}, Config: configuration, Retries: &zero})
	if err != nil {
		return runner.RunSummary{}, err
	}
	raw, _ := json.Marshal(map[string]any{"live": input.Live, "broken": input.Broken, "scenario": input.Scenario, "fixtureSHA256": hex.EncodeToString(hash[:])})
	if err = os.WriteFile(filepath.Join(absolute, "room.html"), fixture, 0600); err != nil {
		return runner.RunSummary{}, err
	}
	if err = os.WriteFile(filepath.Join(absolute, "input.json"), raw, 0600); err != nil {
		return runner.RunSummary{}, err
	}
	revision, err := exec.CommandContext(ctx, "git", "rev-parse", "HEAD").Output()
	if err != nil {
		return runner.RunSummary{}, err
	}
	config.Enabled = input.Live
	return runner.Execute(ctx, plan, runner.ExecuteOptions{Workers: 1, Timeout: 65 * time.Second, RunDeadline: 75 * time.Second, MaxAttempts: 1, MaxOutputBytes: 1 << 20, ReceiptDir: filepath.Join(absolute, "receipts"), Adapters: []runner.Adapter{adapter}, PassEnv: []string{"PLAYWRIGHT_BROWSERS_PATH", "XDG_CACHE_HOME"}, Services: huntServices{config: config, dir: absolute, revision: strings.TrimSpace(string(revision)), source: hex.EncodeToString(hash[:]), scenario: input.Scenario}})
}

func (s *demoServer) runHunt(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", "POST")
		http.Error(w, "Use the demo button", 405)
		return
	}
	if r.Header.Get("Origin") != s.origin || r.Header.Get("X-Demo-Session") != s.session || r.Header.Get("Content-Type") != "application/json" {
		writeDemoJSON(w, 403, map[string]string{"error": "Reopen this local demo."})
		return
	}
	raw, err := io.ReadAll(http.MaxBytesReader(w, r.Body, 512))
	var input huntInput
	var fields map[string]json.RawMessage
	decoder := json.NewDecoder(bytes.NewReader(raw))
	if err != nil || strictjson.Value(decoder) != nil || decoder.Decode(&struct{}{}) != io.EOF || json.Unmarshal(raw, &fields) != nil || len(fields) != 4 || fields["id"] == nil || fields["scenario"] == nil || fields["live"] == nil || fields["broken"] == nil || json.Unmarshal(raw, &input) != nil || !huntID.MatchString(input.ID) || !isCatScenario(input.Scenario) {
		writeDemoJSON(w, 400, map[string]string{"error": "Choose a game level and try again."})
		return
	}
	if input.Live && (!s.config.Enabled || s.config.Images != "synthetic") {
		writeDemoJSON(w, 409, map[string]string{"error": "Connect Jev with --live --images first."})
		return
	}
	if !s.active.TryLock() {
		writeDemoJSON(w, 409, map[string]string{"error": "Another check is running."})
		return
	}
	defer s.active.Unlock()
	s.mu.Lock()
	if s.runs >= maxDemoRuns || (input.Live && s.liveRuns >= maxLiveRuns) {
		s.mu.Unlock()
		writeDemoJSON(w, 429, map[string]string{"error": "Session limit reached. Restart the local demo."})
		return
	}
	s.runs++
	if input.Live {
		s.liveRuns++
	}
	s.mu.Unlock()
	if err = os.MkdirAll(".context", 0700); err != nil {
		writeDemoJSON(w, 500, map[string]string{"error": "Could not prepare the run."})
		return
	}
	dir, err := os.MkdirTemp(".context", "jev-hunt-")
	if err != nil {
		writeDemoJSON(w, 500, map[string]string{"error": "Could not prepare the run."})
		return
	}
	base := "/runs/" + filepath.Base(dir) + "/"
	s.mu.Lock()
	s.hunt = huntState{input.ID, dir, base}
	for i := 0; i < 24; i++ {
		name := fmt.Sprintf("frame-%d.png", i)
		s.artifacts[base+name] = filepath.Join(dir, name)
	}
	s.mu.Unlock()
	ctx, cancel := context.WithTimeout(r.Context(), 80*time.Second)
	defer cancel()
	summary, runErr := executeHunt(ctx, s.config, input, dir)
	// Execute returns the context error alongside its terminal canceled/timed-out
	// receipts. Keep that incomplete result available after the browser disconnects.
	interrupted := errors.Is(runErr, context.Canceled) || errors.Is(runErr, context.DeadlineExceeded)
	summaryPath := filepath.Join(dir, "summary.json")
	raw, marshalErr := json.MarshalIndent(summary, "", "  ")
	if (runErr != nil && !interrupted) || marshalErr != nil || summary.RunID == "" || os.WriteFile(summaryPath, raw, 0600) != nil {
		writeDemoJSON(w, 503, map[string]string{"error": "The 9lives run could not finish. Check Chromium installation and try again."})
		return
	}
	s.mu.Lock()
	s.artifacts[base+"summary.json"] = summaryPath
	s.artifacts[base+"input.json"] = filepath.Join(dir, "input.json")
	s.artifacts[base+"jev-record.json"] = filepath.Join(dir, "jev-record.json")
	s.mu.Unlock()
	writeDemoJSON(w, 200, map[string]any{"outcome": summary.Outcome, "receipt": base + "summary.json", "inputs": base + "input.json", "providerRecord": base + "jev-record.json", "runId": summary.RunID, "receipts": summary.Receipts})
}
func (s *demoServer) huntProgress(w http.ResponseWriter, r *http.Request) {
	if r.Header.Get("X-Demo-Session") != s.session {
		http.NotFound(w, r)
		return
	}
	s.mu.Lock()
	state := s.hunt
	s.mu.Unlock()
	if state.id == "" || state.id != r.URL.Query().Get("id") {
		writeDemoJSON(w, 200, map[string]string{"stage": "starting"})
		return
	}
	file, err := os.Open(filepath.Join(state.dir, "progress.json"))
	if err != nil {
		writeDemoJSON(w, 200, map[string]string{"stage": "starting"})
		return
	}
	defer file.Close()
	var value map[string]any
	if json.NewDecoder(io.LimitReader(file, 16<<10)).Decode(&value) != nil {
		writeDemoJSON(w, 200, map[string]string{"stage": "starting"})
		return
	}
	image, ok := value["image"].(string)
	if !ok || !regexp.MustCompile(`^frame-(?:[0-9]|1[0-9]|2[0-3])\.png$`).MatchString(image) {
		writeDemoJSON(w, 200, map[string]string{"stage": "starting"})
		return
	}
	value["image"] = state.base + image
	writeDemoJSON(w, 200, value)
}
