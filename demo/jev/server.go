package main

import (
	"bytes"
	"context"
	"crypto/rand"
	_ "embed"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/Quality-Max/9lives-runner/internal/jev"
	"github.com/Quality-Max/9lives-runner/internal/strictjson"
)

//go:embed interactive.html
var interactiveHTML string

const maxDemoRuns = 24
const maxLiveRuns = 6

type demoExecutor func(context.Context, jev.Config, string) (string, report, error)
type demoServer struct {
	config          jev.Config
	origin, session string
	execute         demoExecutor
	active          sync.Mutex
	mu              sync.Mutex
	runs, liveRuns  int
	artifacts       map[string]string
}

type browserResult struct {
	Scenario   string     `json:"scenario"`
	Passed     bool       `json:"passed"`
	OrderCount *int       `json:"orderCount"`
	Declined   *float64   `json:"declined"`
	Confirmed  *float64   `json:"confirmed"`
	Observed   *rectangle `json:"observed"`
	Predicted  *rectangle `json:"predicted"`
	Image      string     `json:"image"`
	Report     string     `json:"report"`
	Record     jev.Record `json:"record"`
	Cats       *catResult `json:"cats,omitempty"`
	Error      string     `json:"error,omitempty"`
}

func serveDemo(ctx context.Context, config jev.Config, port int, open bool, out, errOut io.Writer) int {
	listener, err := net.Listen("tcp4", net.JoinHostPort("127.0.0.1", strconv.Itoa(port)))
	if err != nil {
		fmt.Fprintln(errOut, "Could not open the local demo. Try --port 0 for a free port.")
		return 2
	}
	origin := "http://" + listener.Addr().String()
	handler, err := newDemoServer(config, origin, executeDemo)
	if err != nil {
		listener.Close()
		fmt.Fprintln(errOut, "Could not start the local demo.")
		return 2
	}
	server := &http.Server{Handler: handler, ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 10 * time.Second, WriteTimeout: 95 * time.Second, IdleTimeout: 30 * time.Second, MaxHeaderBytes: 8192, BaseContext: func(net.Listener) context.Context { return ctx }}
	done := make(chan struct{})
	stopped := make(chan struct{})
	defer func() { close(done); <-stopped }()
	go func() {
		defer close(stopped)
		select {
		case <-ctx.Done():
			// The base context already cancelled browser/provider work. Wait for
			// owned process cleanup before main exits, including on Ctrl+C.
			shutdown, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			if server.Shutdown(shutdown) != nil {
				server.Close()
			}
		case <-done:
		}
	}()
	fmt.Fprintln(out, "Your interactive Jev demo is ready:", origin)
	fmt.Fprintln(out, "Try a payment, run the browser check, then ask Jev to look. Press Ctrl+C to stop.")
	if open {
		if err := openDemo(ctx, origin); err != nil {
			fmt.Fprintln(out, "Open the address above in your browser.")
		}
	}
	err = server.Serve(listener)
	if err != nil && !errors.Is(err, http.ErrServerClosed) {
		fmt.Fprintln(errOut, "The local demo stopped.")
		return 2
	}
	return 0
}

func openDemo(ctx context.Context, url string) error {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	var command string
	var args []string
	switch runtime.GOOS {
	case "darwin":
		command = "open"
		args = []string{url}
	case "windows":
		command = "rundll32"
		args = []string{"url.dll,FileProtocolHandler", url}
	default:
		command = "xdg-open"
		args = []string{url}
	}
	cmd := exec.CommandContext(ctx, command, args...)
	cmd.Env = demoEnvironment()
	cmd.Stdout, cmd.Stderr = io.Discard, io.Discard
	return cmd.Run()
}

func newDemoServer(config jev.Config, origin string, execute demoExecutor) (*demoServer, error) {
	var nonce [24]byte
	if _, err := rand.Read(nonce[:]); err != nil {
		return nil, err
	}
	return &demoServer{config: config, origin: origin, session: hex.EncodeToString(nonce[:]), execute: execute, artifacts: map[string]string{}}, nil
}

func (s *demoServer) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Referrer-Policy", "no-referrer")
	w.Header().Set("Content-Security-Policy", "default-src 'self'; script-src 'self' 'unsafe-inline'; style-src 'self' 'unsafe-inline'; img-src 'self' data:; font-src 'self'; connect-src 'self'; frame-src 'self'; frame-ancestors 'self'; object-src 'none'; base-uri 'none'; form-action 'none'")
	if r.Host != strings.TrimPrefix(s.origin, "http://") {
		http.Error(w, "Open the demo using its printed local address.", http.StatusForbidden)
		return
	}
	if r.URL.Path == "/api/run" {
		s.run(w, r)
		return
	}
	if r.Method != http.MethodGet {
		w.Header().Set("Allow", "GET")
		http.Error(w, "Use the demo buttons.", http.StatusMethodNotAllowed)
		return
	}
	switch r.URL.Path {
	case "/":
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		io.WriteString(w, strings.Replace(interactiveHTML, "__DEMO_SESSION__", s.session, 1))
	case "/cats":
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		io.WriteString(w, strings.Replace(catsHTML, "__DEMO_SESSION__", s.session, 1))
	case "/api/config":
		s.mu.Lock()
		remaining := maxLiveRuns - s.liveRuns
		runsRemaining := maxDemoRuns - s.runs
		s.mu.Unlock()
		writeDemoJSON(w, http.StatusOK, map[string]any{"live": s.config.Enabled, "images": s.config.Images == "synthetic", "remaining": remaining, "runsRemaining": runsRemaining})
	case "/shop":
		scenario := r.URL.Query().Get("scenario")
		if !validScenario(scenario) {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Write(fixtureHTML(scenario))
	default:
		assets := map[string]string{"/assets/heading.woff2": "website/public/brand/space-grotesk-latin.woff2", "/assets/body.woff2": "website/public/brand/dm-sans-latin.woff2", "/assets/logo.svg": "website/public/brand/logo-wordmark-color.svg", "/assets/logo-dark.svg": "website/public/brand/logo-wordmark-white.svg"}
		file := assets[r.URL.Path]
		if file == "" {
			s.mu.Lock()
			file = s.artifacts[r.URL.Path]
			s.mu.Unlock()
		}
		if file == "" {
			http.NotFound(w, r)
			return
		}
		f, err := os.Open(file)
		if err != nil {
			http.NotFound(w, r)
			return
		}
		defer f.Close()
		info, err := f.Stat()
		if err != nil {
			http.NotFound(w, r)
			return
		}
		http.ServeContent(w, r, info.Name(), info.ModTime(), f)
	}
}

func (s *demoServer) run(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", "POST")
		http.Error(w, "Use the demo buttons.", http.StatusMethodNotAllowed)
		return
	}
	if r.Header.Get("Origin") != s.origin || r.Header.Get("X-Demo-Session") != s.session || r.Header.Get("Content-Type") != "application/json" {
		writeDemoJSON(w, http.StatusForbidden, map[string]string{"error": "Please reopen the local demo and try again."})
		return
	}
	var input struct {
		Scenario string `json:"scenario"`
		Live     bool   `json:"live"`
	}
	raw, err := io.ReadAll(http.MaxBytesReader(w, r.Body, 256))
	decoder := json.NewDecoder(bytes.NewReader(raw))
	var fields map[string]json.RawMessage
	if err != nil || strictjson.Value(decoder) != nil || decoder.Decode(&struct{}{}) != io.EOF || json.Unmarshal(raw, &fields) != nil || len(fields) != 2 || fields["scenario"] == nil || fields["live"] == nil || json.Unmarshal(raw, &input) != nil || !validScenario(input.Scenario) {
		writeDemoJSON(w, http.StatusBadRequest, map[string]string{"error": "Choose one of the demo scenes."})
		return
	}
	if input.Live && !s.config.Enabled {
		writeDemoJSON(w, http.StatusConflict, map[string]string{"error": "Jev is not connected. You can still try the shop and browser check."})
		return
	}
	if input.Live && isCatScenario(input.Scenario) && s.config.Images != "synthetic" {
		writeDemoJSON(w, http.StatusConflict, map[string]string{"error": "Restart with --live --images to let Jev see the cat room."})
		return
	}
	if !s.active.TryLock() {
		writeDemoJSON(w, http.StatusConflict, map[string]string{"error": "A check is already running. Give it a moment."})
		return
	}
	defer s.active.Unlock()
	s.mu.Lock()
	if s.runs >= maxDemoRuns || (input.Live && s.liveRuns >= maxLiveRuns) {
		s.mu.Unlock()
		writeDemoJSON(w, http.StatusTooManyRequests, map[string]string{"error": "This demo session has reached its limit. Restart the local demo for a new session."})
		return
	}
	s.runs++
	if input.Live {
		s.liveRuns++
	}
	s.mu.Unlock()
	config := s.config
	config.Enabled = input.Live
	ctx, cancel := context.WithTimeout(r.Context(), 90*time.Second)
	defer cancel()
	dir, result, err := s.execute(ctx, config, input.Scenario)
	if dir == "" {
		writeDemoJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "The browser check could not start. Check that Chromium is installed, then try again."})
		return
	}
	base := "/runs/" + filepath.Base(dir) + "/"
	s.mu.Lock()
	for _, name := range []string{"checkout.png", "index.html", "record.json"} {
		s.artifacts[base+name] = filepath.Join(dir, name)
	}
	s.mu.Unlock()
	body := browserResult{Cats: result.Cats, Scenario: input.Scenario, Passed: result.Capture.AssertionPassed, OrderCount: result.Capture.OrderCount, Declined: result.Declined, Confirmed: result.Confirmed, Observed: result.Observed, Predicted: result.Predicted, Image: base + "checkout.png", Report: base + "index.html", Record: result.Record}
	if err != nil {
		body.Error = "Jev could not answer this time. The browser check below still shows what happened. You can try again."
	}
	writeDemoJSON(w, http.StatusOK, body)
}

func writeDemoJSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(body)
}
