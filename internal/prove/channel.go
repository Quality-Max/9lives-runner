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
	// A capability record lists at most this many fault kind names.
	maxCapabilities = 32
)

var (
	methodPattern = regexp.MustCompile(`^[A-Z]{1,16}$`)
	faultPattern  = regexp.MustCompile(`^fault-[1-9][0-9]{0,3}$`)
	filePattern   = regexp.MustCompile(`^[a-f0-9]{32}\.ndjson$`)
	testPattern   = regexp.MustCompile(`^[a-f0-9]{64}$`)
	kindPattern   = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,31}$`)
)

// Reasons an empty-json or malformed-json fault was not applicable to a request.
const (
	ReasonNotJSON     = "not-json"
	ReasonUnreachable = "unreachable"
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
	// JSON2xx records a successful JSON response, the precondition for the
	// empty-json and malformed-json faults.
	JSON2xx bool
}

// Attempt is what one test attempt's file recorded about the run's fault.
type Attempt struct {
	Applied int
	// NotApplicable counts, per reason, requests the fault matched but could
	// not change.
	NotApplicable map[string]int
}

func (attempt Attempt) notApplicable() int {
	total := 0
	for _, count := range attempt.NotApplicable {
		total += count
	}
	return total
}

type Observations struct {
	// Attempts counts per-test files whose handshake matched the mode. Zero
	// means the SDK never instrumented a browser context.
	Attempts int
	Requests map[string]*Request
	// Tests holds each instrumented test's fault facts by its hashed engine
	// test ID, merged over its attempts.
	Tests    map[string]*Attempt
	Overflow bool
	// FaultKinds holds the kinds every instrumented attempt can apply. An
	// SDK that predates capability records applies DefaultKinds only.
	FaultKinds map[string]bool
}

// Applied sums the fault's applications over every test.
func (observations Observations) Applied() int {
	total := 0
	for _, attempt := range observations.Tests {
		total += attempt.Applied
	}
	return total
}

type record struct {
	Type         string   `json:"type"`
	Protocol     string   `json:"protocol"`
	Mode         string   `json:"mode"`
	Method       string   `json:"method"`
	Origin       string   `json:"origin"`
	Path         string   `json:"path"`
	ResourceType string   `json:"resourceType"`
	Status       int      `json:"status"`
	JSON         bool     `json:"json"`
	Fault        string   `json:"fault"`
	Reason       string   `json:"reason"`
	TestID       string   `json:"testId"`
	Retry        int      `json:"retry"`
	Faults       []string `json:"faults"`
}

var recordFields = map[string][]string{
	"hello":          {"type", "protocol", "mode", "testId", "retry"},
	"capabilities":   {"type", "faults"},
	"request":        {"type", "method", "origin", "path", "resourceType"},
	"response":       {"type", "method", "origin", "path", "status", "json"},
	"applied":        {"type", "fault"},
	"not-applicable": {"type", "fault", "reason"},
	"overflow":       {"type"},
}

// ReadChannel validates every per-test file the SDK wrote into dir. mode is
// "observe" or "fault"; records belonging to the other mode are rejected, and
// in fault mode only records for the named fault are accepted. Errors never
// echo worker-controlled values.
func ReadChannel(dir, mode, fault string) (Observations, error) {
	observations := Observations{Requests: map[string]*Request{}, Tests: map[string]*Attempt{}}
	if mode != "observe" && mode != "fault" || (mode == "fault") != faultPattern.MatchString(fault) {
		return observations, errors.New("unsupported prove mode")
	}
	seen := map[string]bool{}
	var kinds map[string]bool
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
		supported, err := observations.add(raw, mode, fault, seen)
		if err != nil {
			return observations, err
		}
		// Workers can load different SDK builds in principle; plan only what
		// every one of them can apply.
		if kinds == nil {
			kinds = supported
		}
		for kind := range kinds {
			if !supported[kind] {
				delete(kinds, kind)
			}
		}
	}
	if kinds == nil {
		kinds = defaultKinds()
	}
	observations.FaultKinds = kinds
	return observations, nil
}

func defaultKinds() map[string]bool {
	kinds := map[string]bool{}
	for _, kind := range DefaultKinds {
		kinds[kind] = true
	}
	return kinds
}

// capabilities validates a capability record. Names this engine does not know
// are ignored, so a newer SDK can advertise kinds an older engine never plans.
func capabilities(names []string) (map[string]bool, error) {
	if len(names) == 0 || len(names) > maxCapabilities {
		return nil, errors.New("invalid prove capabilities")
	}
	kinds, known := defaultKinds(), map[string]bool{}
	for _, kind := range Kinds {
		known[kind] = true
	}
	listed := map[string]bool{}
	for _, name := range names {
		if !kindPattern.MatchString(name) || listed[name] {
			return nil, errors.New("invalid prove capabilities")
		}
		listed[name] = true
		if known[name] {
			kinds[name] = true
		}
	}
	return kinds, nil
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

// add validates one test attempt's file and returns the fault kinds it can
// apply.
func (observations *Observations) add(raw []byte, mode, fault string, seen map[string]bool) (map[string]bool, error) {
	if len(raw) == 0 || !bytes.HasSuffix(raw, []byte("\n")) || !utf8.Valid(raw) {
		return nil, errors.New("missing or unfinished prove evidence")
	}
	lines := bytes.Split(raw[:len(raw)-1], []byte("\n"))
	if len(lines) > maxRecords {
		return nil, errors.New("prove record limit exceeded")
	}
	supported := defaultKinds()
	// Each file is one test attempt's context, where a response always
	// follows its request; pair them here, before attempts are merged.
	requested := map[string]bool{}
	var test *Attempt
	for index, line := range lines {
		entry, err := decodeRecord(line)
		if err != nil {
			return nil, err
		}
		if index == 0 {
			if entry.Type != "hello" || entry.Protocol != Protocol || entry.Mode != mode || !testPattern.MatchString(entry.TestID) || entry.Retry < 0 || entry.Retry > 1024 {
				return nil, errors.New("missing or mismatched prove handshake")
			}
			// One file per test attempt: a second file for the same attempt
			// would let a worker vouch for itself twice.
			key := fmt.Sprintf("%s:%d", entry.TestID, entry.Retry)
			if seen[key] {
				return nil, errors.New("duplicate prove attempt")
			}
			seen[key] = true
			observations.Attempts++
			if test = observations.Tests[entry.TestID]; test == nil {
				test = &Attempt{NotApplicable: map[string]int{}}
				observations.Tests[entry.TestID] = test
			}
			continue
		}
		switch entry.Type {
		case "request", "response":
			if mode != "observe" {
				return nil, errors.New("observation recorded during a fault run")
			}
			target := Target{entry.Method, entry.Origin, entry.Path}
			if !validTarget(target) {
				return nil, errors.New("invalid request target")
			}
			request := observations.Requests[target.Key()]
			if request == nil {
				request = &Request{Target: target}
				observations.Requests[target.Key()] = request
			}
			if entry.Type == "request" {
				if entry.ResourceType != "fetch" && entry.ResourceType != "xhr" {
					return nil, errors.New("unsupported resource type")
				}
				request.ResourceType = entry.ResourceType
				request.Observed++
				requested[target.Key()] = true
			} else {
				if !requested[target.Key()] {
					return nil, errors.New("response without a recorded request")
				}
				if entry.Status < 100 || entry.Status > 599 {
					return nil, errors.New("invalid response status")
				}
				request.JSON2xx = request.JSON2xx || (entry.JSON && entry.Status >= 200 && entry.Status < 300)
			}
		case "applied", "not-applicable":
			if mode != "fault" || entry.Fault != fault {
				return nil, errors.New("invalid fault record")
			}
			if entry.Type == "applied" {
				test.Applied++
			} else {
				if entry.Reason != ReasonNotJSON && entry.Reason != ReasonUnreachable {
					return nil, errors.New("invalid fault record")
				}
				test.NotApplicable[entry.Reason]++
			}
		case "capabilities":
			// Sent once, right after the handshake, and only while observing:
			// the baseline decides which kinds are planned.
			if mode != "observe" || index != 1 {
				return nil, errors.New("unexpected prove capabilities")
			}
			if supported, err = capabilities(entry.Faults); err != nil {
				return nil, err
			}
		case "overflow":
			if index != len(lines)-1 {
				return nil, errors.New("record after overflow")
			}
			observations.Overflow = true
		default:
			return nil, errors.New("unexpected prove record")
		}
	}
	return supported, nil
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

// decodeRecord accepts exactly one closed-schema object per line, with no
// surrounding whitespace (a CRLF line or trailing padding is malformed).
func decodeRecord(line []byte) (record, error) {
	var entry record
	if len(line) == 0 || line[0] != '{' || line[len(line)-1] != '}' || strictjson.Value(json.NewDecoder(bytes.NewReader(line))) != nil {
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
