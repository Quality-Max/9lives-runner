// Package browsercheck runs the page scan and inspection checks in a Node
// helper process. The helper owns the browser; this package owns its
// environment, time budget, cancellation and the size of what it reads back.
package browsercheck

import (
	"bytes"
	"context"
	"embed"
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/Quality-Max/9lives-runner/internal/runner"
)

// helper is packages/browser-checks compiled by scripts/bundle-browser-checks.cjs.
//
//go:embed all:helper
var helper embed.FS

// ProtocolVersion is the version of the helper's stdin request and stdout response.
const ProtocolVersion = 1

const (
	maxOutput = 16 << 20
	maxStderr = 4 << 10
)

// Checks are the scan checks the helper runs; it rejects any other name.
var Checks = []string{"console", "links", "accessibility", "performance", "seo", "security_headers", "cookies", "mixed_content", "weight"}

// Request is one scan or inspection.
type Request struct {
	Version     int    `json:"version"`
	Tool        string `json:"tool"`
	Args        any    `json:"args"`
	Format      string `json:"format,omitempty"`
	MinSeverity string `json:"minSeverity,omitempty"`
}

// Response is the helper's result: the structured result and, for a
// Markdown or issue format, its rendering.
type Response struct {
	Version int             `json:"version"`
	Result  json.RawMessage `json:"result,omitempty"`
	Text    string          `json:"text,omitempty"`
	Error   string          `json:"error,omitempty"`
}

// CheckError is a check the helper refused or could not complete, such as
// a blocked destination, an unreachable page or a missing browser.
type CheckError struct{ Message string }

func (e CheckError) Error() string { return e.Message }

// Errors for a helper that did not produce a result.
var (
	ErrNodeUnavailable = errors.New("node is required for browser checks and was not found on PATH")
	ErrTimeout         = errors.New("browser check exceeded its time limit")
	ErrOutputLimit     = errors.New("browser check output exceeded its size limit")
)

// Options bound one helper run.
type Options struct {
	// Root is the helper's working directory: Playwright is resolved from it
	// and workspace-relative paths stay inside it.
	Root    string
	Timeout time.Duration
}

// helperEnv names the only variables the helper inherits: enough to find
// node and the installed browsers, and nothing a test or provider uses.
var helperEnv = []string{"PATH", "HOME", "USERPROFILE", "LOCALAPPDATA", "APPDATA", "SystemRoot", "TEMP", "TMP", "TMPDIR", "XDG_CACHE_HOME", "PLAYWRIGHT_BROWSERS_PATH"}

// Run starts the helper, sends request and returns its response. Cancelling
// ctx stops the helper and its browser.
func Run(parent context.Context, request Request, options Options) (Response, error) {
	request.Version = ProtocolVersion
	input, err := json.Marshal(request)
	if err != nil {
		return Response{}, err
	}
	dir, err := os.MkdirTemp("", "9lives-browser-checks-")
	if err != nil {
		return Response{}, err
	}
	defer os.RemoveAll(dir)
	if err := materialize(dir); err != nil {
		return Response{}, err
	}
	ctx, cancel := context.WithTimeout(parent, options.Timeout)
	defer cancel()
	command := exec.Command("node", filepath.Join(dir, "helper.js"))
	command.Dir = options.Root
	command.Env = []string{}
	for _, name := range helperEnv {
		if value, ok := os.LookupEnv(name); ok {
			command.Env = append(command.Env, name+"="+value)
		}
	}
	command.Stdin = bytes.NewReader(input)
	output := &boundedWriter{limit: maxOutput, cancel: cancel}
	stderr := &tailWriter{limit: maxStderr}
	command.Stdout, command.Stderr = output, stderr
	err = runner.RunOwnedCommand(ctx, command)
	switch {
	case parent.Err() != nil:
		return Response{}, parent.Err()
	case output.overflow:
		return Response{}, ErrOutputLimit
	case ctx.Err() != nil:
		return Response{}, ErrTimeout
	case errors.Is(err, exec.ErrNotFound):
		return Response{}, ErrNodeUnavailable
	}
	var response Response
	decoder := json.NewDecoder(bytes.NewReader(output.buffer.Bytes()))
	if decoder.Decode(&response) != nil || response.Version != ProtocolVersion {
		detail := strings.TrimSpace(stderr.String())
		if detail == "" {
			detail = "no result"
		}
		return Response{}, CheckError{"browser-check helper failed: " + detail}
	}
	if response.Error != "" {
		return Response{}, CheckError{response.Error}
	}
	if err != nil || len(response.Result) == 0 {
		return Response{}, CheckError{"browser-check helper returned no result"}
	}
	return response, nil
}

func materialize(dir string) error {
	return fs.WalkDir(helper, "helper", func(name string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		target := filepath.Join(dir, filepath.FromSlash(strings.TrimPrefix(name, "helper")))
		if entry.IsDir() {
			return os.MkdirAll(target, 0o700)
		}
		data, err := helper.ReadFile(name)
		if err != nil {
			return err
		}
		return os.WriteFile(target, data, 0o600)
	})
}

type boundedWriter struct {
	buffer   bytes.Buffer
	limit    int
	cancel   context.CancelFunc
	overflow bool
}

func (w *boundedWriter) Write(p []byte) (int, error) {
	if w.buffer.Len()+len(p) > w.limit {
		w.overflow = true
		w.cancel()
		return 0, ErrOutputLimit
	}
	return w.buffer.Write(p)
}

// tailWriter keeps the last bytes written: a crashing helper's reason is
// at the end of its stderr.
type tailWriter struct {
	data  []byte
	limit int
}

func (w *tailWriter) Write(p []byte) (int, error) {
	w.data = append(w.data, p...)
	if len(w.data) > w.limit {
		w.data = w.data[len(w.data)-w.limit:]
	}
	return len(p), nil
}

func (w *tailWriter) String() string { return string(w.data) }
