// Package prove plans network faults for one Playwright spec and classifies
// whether its assertions fail while each fault is injected. A caught fault
// shows that an assertion failed under the fault; it never shows that the
// assertion checks the intended behavior.
package prove

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"unicode/utf8"

	"github.com/Quality-Max/9lives-runner/internal/strictjson"
)

// Protocol must match proveProtocol in packages/playwright/src/prove.ts.
const Protocol = "9l.prove/1"

const (
	maxFiles      = 512
	maxFileBytes  = 1 << 20
	maxTotalBytes = 8 << 20
	maxRecords    = 2000
	maxPathBytes  = 2048
)

var (
	methodPattern = regexp.MustCompile(`^[A-Z]{1,16}$`)
	faultPattern  = regexp.MustCompile(`^fault-[1-9][0-9]{0,3}$`)
	filePattern   = regexp.MustCompile(`^[a-f0-9]{32}\.ndjson$`)
)

// Target is a request identity: one fault covers every request with the same
// method, origin and path. Query strings and fragments never cross the channel.
type Target struct {
	Method string `json:"method"`
	Origin string `json:"origin"`
	Path   string `json:"path"`
}

func (target Target) Key() string { return target.Method + " " + target.Origin + target.Path }

// Request aggregates what the baseline observed for one Target.
type Request struct {
	Target
	ResourceType string
	Observed     int
	// JSON2xx records a successful JSON response, the precondition for an
	// empty-json fault.
	JSON2xx bool
}

type Observations struct {
	// Attempts counts per-test files whose handshake matched the mode. Zero
	// means the SDK never instrumented a browser context.
	Attempts      int
	Requests      map[string]*Request
	Applied       map[string]int
	NotApplicable map[string]int
	Overflow      bool
}

type record struct {
	Type         string `json:"type"`
	Protocol     string `json:"protocol"`
	Mode         string `json:"mode"`
	Method       string `json:"method"`
	Origin       string `json:"origin"`
	Path         string `json:"path"`
	ResourceType string `json:"resourceType"`
	Status       int    `json:"status"`
	JSON         bool   `json:"json"`
	Fault        string `json:"fault"`
}

var recordFields = map[string][]string{
	"hello":          {"type", "protocol", "mode"},
	"request":        {"type", "method", "origin", "path", "resourceType"},
	"response":       {"type", "method", "origin", "path", "status", "json"},
	"applied":        {"type", "fault"},
	"not-applicable": {"type", "fault"},
	"overflow":       {"type"},
}

// ReadChannel validates every per-test file the SDK wrote into dir. mode is
// "observe" or "fault"; records belonging to the other mode are rejected.
// Errors never echo worker-controlled values.
func ReadChannel(dir, mode string) (Observations, error) {
	observations := Observations{Requests: map[string]*Request{}, Applied: map[string]int{}, NotApplicable: map[string]int{}}
	if mode != "observe" && mode != "fault" {
		return observations, errors.New("unsupported prove mode")
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return observations, errors.New("prove evidence directory unavailable")
	}
	if len(entries) > maxFiles {
		return observations, errors.New("prove evidence file limit exceeded")
	}
	total := 0
	for _, entry := range entries {
		if !entry.Type().IsRegular() || !filePattern.MatchString(entry.Name()) {
			return observations, errors.New("unexpected prove evidence file")
		}
		raw, err := readBounded(filepath.Join(dir, entry.Name()))
		if err != nil {
			return observations, err
		}
		if total += len(raw); total > maxTotalBytes {
			return observations, errors.New("prove evidence size limit exceeded")
		}
		if err := observations.add(raw, mode); err != nil {
			return observations, err
		}
	}
	return observations, nil
}

func readBounded(path string) ([]byte, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, errors.New("prove evidence unavailable")
	}
	defer file.Close()
	raw, err := io.ReadAll(io.LimitReader(file, maxFileBytes+1))
	if err != nil || len(raw) > maxFileBytes {
		return nil, errors.New("prove evidence file exceeds limit")
	}
	return raw, nil
}

func (observations *Observations) add(raw []byte, mode string) error {
	if len(raw) == 0 || !bytes.HasSuffix(raw, []byte("\n")) || !utf8.Valid(raw) {
		return errors.New("missing or unfinished prove evidence")
	}
	lines := bytes.Split(raw[:len(raw)-1], []byte("\n"))
	if len(lines) > maxRecords {
		return errors.New("prove record limit exceeded")
	}
	// Each file is one test attempt's context, where a response always
	// follows its request; pair them here, before attempts are merged.
	requested := map[string]bool{}
	for index, line := range lines {
		entry, err := decodeRecord(line)
		if err != nil {
			return err
		}
		if index == 0 {
			if entry.Type != "hello" || entry.Protocol != Protocol || entry.Mode != mode {
				return errors.New("missing or mismatched prove handshake")
			}
			observations.Attempts++
			continue
		}
		switch entry.Type {
		case "request", "response":
			if mode != "observe" {
				return errors.New("observation recorded during a fault run")
			}
			target := Target{entry.Method, entry.Origin, entry.Path}
			if !validTarget(target) {
				return errors.New("invalid request target")
			}
			request := observations.Requests[target.Key()]
			if request == nil {
				request = &Request{Target: target}
				observations.Requests[target.Key()] = request
			}
			if entry.Type == "request" {
				if entry.ResourceType != "fetch" && entry.ResourceType != "xhr" {
					return errors.New("unsupported resource type")
				}
				request.ResourceType = entry.ResourceType
				request.Observed++
				requested[target.Key()] = true
			} else {
				if !requested[target.Key()] {
					return errors.New("response without a recorded request")
				}
				if entry.Status < 100 || entry.Status > 599 {
					return errors.New("invalid response status")
				}
				request.JSON2xx = request.JSON2xx || (entry.JSON && entry.Status >= 200 && entry.Status < 300)
			}
		case "applied", "not-applicable":
			if mode != "fault" || !faultPattern.MatchString(entry.Fault) {
				return errors.New("invalid fault record")
			}
			if entry.Type == "applied" {
				observations.Applied[entry.Fault]++
			} else {
				observations.NotApplicable[entry.Fault]++
			}
		case "overflow":
			if index != len(lines)-1 {
				return errors.New("record after overflow")
			}
			observations.Overflow = true
		default:
			return errors.New("unexpected prove record")
		}
	}
	return nil
}

func validTarget(target Target) bool {
	if !methodPattern.MatchString(target.Method) || len(target.Path) > maxPathBytes || !strings.HasPrefix(target.Path, "/") {
		return false
	}
	for _, char := range target.Path {
		if char < 0x20 || char == 0x7f || char == '?' || char == '#' {
			return false
		}
	}
	parsed, err := url.Parse(target.Origin)
	return err == nil && (parsed.Scheme == "http" || parsed.Scheme == "https") && parsed.Host != "" &&
		parsed.User == nil && parsed.Scheme+"://"+parsed.Host == target.Origin
}

// decodeRecord accepts exactly one closed-schema object per line.
func decodeRecord(line []byte) (record, error) {
	var entry record
	if len(line) == 0 || strictjson.Value(json.NewDecoder(bytes.NewReader(line))) != nil {
		return entry, errors.New("malformed prove record")
	}
	var members map[string]json.RawMessage
	if json.Unmarshal(line, &members) != nil || json.Unmarshal(line, &entry) != nil {
		return entry, errors.New("malformed prove record")
	}
	fields, ok := recordFields[entry.Type]
	if !ok || len(members) != len(fields) {
		return entry, fmt.Errorf("unexpected prove record fields")
	}
	for _, field := range fields {
		if members[field] == nil {
			return entry, fmt.Errorf("missing prove record field")
		}
	}
	return entry, nil
}
