package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/Quality-Max/9lives-runner/internal/runner"
	"github.com/Quality-Max/9lives-runner/internal/strictjson"
)

// Bounds of `9l run --verify`. Every one is fixed: a checks file can ask for
// less, never for more.
const (
	verifyFileVersion   = 1
	verifyMaxFileBytes  = 64 << 10
	verifyMaxURLBytes   = 2048
	verifyMaxPointer    = 512
	verifyMaxBodyBytes  = 1 << 20
	verifyMaxWithinMS   = 30_000
	verifyMaxHeaderSize = 8 << 10
)

// Variables so tests can shorten them; nothing else assigns them.
var (
	verifyProbeTimeout = 5 * time.Second
	verifyTotalTimeout = 60 * time.Second
	verifyPollInterval = 250 * time.Millisecond
)

// verifyFile is the checks file: a closed, versioned document reviewed and
// kept outside the spec it checks.
type verifyFile struct {
	Version int           `json:"version"`
	Checks  []verifyCheck `json:"checks"`
}

type verifyCheck struct {
	ID           string          `json:"id"`
	OutcomeID    string          `json:"outcomeId"`
	Method       string          `json:"method"`
	URL          string          `json:"url"`
	ExpectStatus int             `json:"expectStatus"`
	JSONPointer  *string         `json:"jsonPointer"`
	Equals       json.RawMessage `json:"equals"`
	WithinMS     int             `json:"withinMs"`
	Header       *verifyHeader   `json:"header"`

	// Resolved while loading; none of it is ever persisted or printed.
	target      *url.URL
	expected    any
	tokens      []string
	headerValue string
}

// verifyHeader is one request header whose value is the content of a named
// environment variable, read by the CLI process only.
type verifyHeader struct {
	Name string `json:"name"`
	Env  string `json:"env"`
}

var (
	verifyHeaderName = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9-]{0,63}$`)
	verifyEnvName    = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]{0,127}$`)
	// Headers that change how the request is framed or routed rather than
	// what it carries.
	verifyReservedHeaders = []string{"host", "content-length", "transfer-encoding", "connection", "upgrade", "te", "trailer", "proxy-authorization", "proxy-connection"}
)

// verifier implements runner.PostconditionVerifier for one checks file.
type verifier struct {
	path   string
	digest string
	checks []verifyCheck
	client *http.Client
}

// readVerifyFile returns at most the size limit of a regular file.
func readVerifyFile(path string) ([]byte, error) {
	info, err := os.Stat(path)
	if err != nil || !info.Mode().IsRegular() {
		return nil, errors.New("the checks file is not a readable regular file")
	}
	file, err := os.Open(path)
	if err != nil {
		return nil, errors.New("the checks file is not a readable regular file")
	}
	defer file.Close()
	raw, err := io.ReadAll(io.LimitReader(file, verifyMaxFileBytes+1))
	if err != nil {
		return nil, errors.New("the checks file is not a readable regular file")
	}
	if len(raw) > verifyMaxFileBytes {
		return nil, fmt.Errorf("the checks file exceeds %d bytes", verifyMaxFileBytes)
	}
	return raw, nil
}

// loadVerifier reads and validates a checks file and records its digest. Its
// errors are fixed phrases with at most a check's position or a variable's
// name: a rejected file is never echoed, since a URL may carry a credential.
// passEnv are the variables the run forwards to test processes; a header's
// variable may not be one of them.
func loadVerifier(path string, passEnv []string) (*verifier, error) {
	raw, err := readVerifyFile(path)
	if err != nil {
		return nil, err
	}
	checks, err := parseVerifyFile(raw)
	if err != nil {
		return nil, err
	}
	for index := range checks {
		header := checks[index].Header
		if header == nil {
			continue
		}
		// The value authenticates the engine's own probe. It must not be
		// something the worker also receives.
		if slices.ContainsFunc(passEnv, func(name string) bool { return strings.EqualFold(name, header.Env) }) || runner.InheritedByWorkers(header.Env) {
			return nil, fmt.Errorf("check %d: header variable %s is also given to test processes; use one only the engine reads", index+1, header.Env)
		}
		value, ok := os.LookupEnv(header.Env)
		if !ok || value == "" || len(value) > verifyMaxHeaderSize || strings.ContainsAny(value, "\r\n\x00") {
			return nil, fmt.Errorf("check %d: header variable %s is unset, empty, over %d bytes or not one line", index+1, header.Env, verifyMaxHeaderSize)
		}
		checks[index].headerValue = value
	}
	sum := sha256.Sum256(raw)
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.DisableKeepAlives = true
	return &verifier{path: path, digest: hex.EncodeToString(sum[:]), checks: checks, client: &http.Client{
		Transport: transport,
		// As for --wait-for: a redirect is the application's answer. It is
		// compared, never followed to another service.
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}}, nil
}

// parseVerifyFile decodes a checks file strictly: one JSON object, no
// duplicate or unknown members, no null, bounded nesting, and every check
// within its closed form.
func parseVerifyFile(raw []byte) ([]verifyCheck, error) {
	malformed := errors.New("the checks file is not a version 1 checks document: one JSON object with version and checks, without unknown, duplicate or null members")
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	if strictjson.Value(decoder) != nil {
		return nil, malformed
	}
	if _, err := decoder.Token(); err != io.EOF {
		return nil, malformed
	}
	var file verifyFile
	decoder = json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&file); err != nil || file.Version != verifyFileVersion {
		return nil, malformed
	}
	if len(file.Checks) == 0 || len(file.Checks) > runner.MaxVerifications {
		return nil, fmt.Errorf("the checks file needs 1 to %d checks", runner.MaxVerifications)
	}
	seen := map[string]bool{}
	for index := range file.Checks {
		check := &file.Checks[index]
		fail := func(reason string) ([]verifyCheck, error) {
			return nil, fmt.Errorf("check %d: %s", index+1, reason)
		}
		if !runner.ValidVerificationID(check.ID) || seen[check.ID] {
			return fail("id must be distinct, 1 to 80 letters, digits, '_', '.' or '-', starting with a letter or digit")
		}
		seen[check.ID] = true
		if check.OutcomeID != "" && !runner.ValidVerificationID(check.OutcomeID) {
			return fail("outcomeId must be 1 to 80 letters, digits, '_', '.' or '-', starting with a letter or digit")
		}
		if check.Method != http.MethodGet && check.Method != http.MethodHead {
			return fail("method must be GET or HEAD")
		}
		target, err := verifyTarget(check.URL)
		if err != nil {
			return fail(err.Error())
		}
		check.target = target
		if check.ExpectStatus < 100 || check.ExpectStatus > 599 {
			return fail("expectStatus must be an HTTP status from 100 to 599")
		}
		if (check.JSONPointer == nil) != (check.Equals == nil) {
			return fail("jsonPointer and equals are given together or not at all")
		}
		if check.JSONPointer != nil {
			if check.Method != http.MethodGet {
				return fail("jsonPointer needs method GET; a HEAD answer has no body")
			}
			tokens, err := pointerTokens(*check.JSONPointer)
			if err != nil {
				return fail("jsonPointer must be an RFC 6901 pointer of at most 512 bytes")
			}
			check.tokens = tokens
			if check.expected, err = decodeJSONValue(check.Equals); err != nil {
				return fail("equals must be one JSON value")
			}
		}
		if check.WithinMS < 0 || check.WithinMS > verifyMaxWithinMS {
			return fail(fmt.Sprintf("withinMs must be from 0 to %d", verifyMaxWithinMS))
		}
		if header := check.Header; header != nil {
			if !verifyHeaderName.MatchString(header.Name) || slices.Contains(verifyReservedHeaders, strings.ToLower(header.Name)) || !verifyEnvName.MatchString(header.Env) {
				return fail("header needs a plain header name and the name of an environment variable")
			}
		}
	}
	return file.Checks, nil
}

// verifyTarget accepts what --wait-for accepts of an http(s) URL, and
// additionally refuses user info and fragments: credentials belong in the
// header variable, never in a file.
func verifyTarget(raw string) (*url.URL, error) {
	invalid := errors.New("url must be an http(s) URL of at most 2048 bytes with a host name or IP address, without user info or fragment")
	lower := strings.ToLower(raw)
	if len(raw) > verifyMaxURLBytes || strings.ContainsAny(raw, " \r\n\t\x00") || !(strings.HasPrefix(lower, "http://") || strings.HasPrefix(lower, "https://")) {
		return nil, invalid
	}
	parsed, err := url.Parse(raw)
	if err != nil || parsed.User != nil || parsed.Fragment != "" || strings.Contains(raw, "#") || parsed.Hostname() == "" || !validWaitHost(parsed.Hostname()) ||
		strings.Contains(parsed.Hostname(), ":") != strings.HasPrefix(parsed.Host, "[") {
		return nil, invalid
	}
	if port := parsed.Port(); port != "" || strings.HasSuffix(parsed.Host, ":") {
		if !validPort(port) {
			return nil, invalid
		}
	}
	return parsed, nil
}

// pointerTokens splits an RFC 6901 JSON Pointer into its reference tokens.
func pointerTokens(pointer string) ([]string, error) {
	if len(pointer) > verifyMaxPointer || (pointer != "" && !strings.HasPrefix(pointer, "/")) {
		return nil, errors.New("invalid JSON pointer")
	}
	if pointer == "" {
		return []string{}, nil
	}
	tokens := strings.Split(pointer[1:], "/")
	for index, token := range tokens {
		for at := 0; at < len(token); at++ {
			if token[at] == '~' && (at+1 == len(token) || (token[at+1] != '0' && token[at+1] != '1')) {
				return nil, errors.New("invalid JSON pointer")
			}
		}
		tokens[index] = strings.ReplaceAll(strings.ReplaceAll(token, "~1", "/"), "~0", "~")
	}
	return tokens, nil
}

// decodeJSONValue decodes exactly one JSON value, keeping numbers as written.
func decodeJSONValue(raw []byte) (any, error) {
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	var value any
	if err := decoder.Decode(&value); err != nil {
		return nil, err
	}
	if _, err := decoder.Token(); err != io.EOF {
		return nil, errors.New("trailing data")
	}
	return value, nil
}

// resolvePointer returns the value the tokens address in a decoded document.
func resolvePointer(document any, tokens []string) (any, bool) {
	current := document
	for _, token := range tokens {
		switch node := current.(type) {
		case map[string]any:
			next, ok := node[token]
			if !ok {
				return nil, false
			}
			current = next
		case []any:
			if token == "" || len(token) > 9 || (len(token) > 1 && token[0] == '0') || strings.Trim(token, "0123456789") != "" {
				return nil, false
			}
			index, err := strconv.Atoi(token)
			if err != nil || index >= len(node) {
				return nil, false
			}
			current = node[index]
		default:
			return nil, false
		}
	}
	return current, true
}

var jsonInteger = regexp.MustCompile(`^-?(0|[1-9][0-9]*)$`)

// jsonEqual compares two decoded JSON values exactly: same type, same
// members, same order of array items. Numbers compare by value, integers
// digit for digit and anything with a fraction or exponent as a double.
func jsonEqual(left, right any) bool {
	switch a := left.(type) {
	case nil:
		return right == nil
	case bool:
		b, ok := right.(bool)
		return ok && a == b
	case string:
		b, ok := right.(string)
		return ok && a == b
	case json.Number:
		b, ok := right.(json.Number)
		if !ok {
			return false
		}
		if jsonInteger.MatchString(a.String()) && jsonInteger.MatchString(b.String()) {
			return a.String() == b.String() || (isZero(a) && isZero(b))
		}
		x, errA := a.Float64()
		y, errB := b.Float64()
		return errA == nil && errB == nil && x == y
	case []any:
		b, ok := right.([]any)
		if !ok || len(a) != len(b) {
			return false
		}
		for index := range a {
			if !jsonEqual(a[index], b[index]) {
				return false
			}
		}
		return true
	case map[string]any:
		b, ok := right.(map[string]any)
		if !ok || len(a) != len(b) {
			return false
		}
		for key, value := range a {
			other, present := b[key]
			if !present || !jsonEqual(value, other) {
				return false
			}
		}
		return true
	}
	return false
}

func isZero(number json.Number) bool { return number.String() == "0" || number.String() == "-0" }

func (v *verifier) SourceSHA256() string { return v.digest }

func (v *verifier) Planned() []runner.Verification {
	planned := make([]runner.Verification, 0, len(v.checks))
	for _, check := range v.checks {
		planned = append(planned, v.record(check, runner.VerificationNotRun, runner.VerificationReasonNotValidated, 0))
	}
	return planned
}

func (v *verifier) record(check verifyCheck, result runner.VerificationResult, reason string, status int) runner.Verification {
	return runner.Verification{ID: check.ID, OutcomeID: check.OutcomeID, Method: check.Method, Result: result, Reason: reason, HTTPStatus: status, ObservedBy: runner.VerificationObserver}
}

// Unchanged re-reads the checks file. The digest only detects a change made
// between the two reads; it does not stop a process from reading the file.
func (v *verifier) Unchanged() bool {
	raw, err := readVerifyFile(v.path)
	if err != nil {
		return false
	}
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:]) == v.digest
}

// Verify probes each check in file order, in this process. A check without
// withinMs is probed once; with it, until it holds or the window closes. All
// checks share one time budget, and a check the budget or the run's own
// cancellation left unprobed is reported not run.
func (v *verifier) Verify(ctx context.Context) []runner.Verification {
	defer v.client.CloseIdleConnections()
	total, cancel := context.WithTimeout(ctx, verifyTotalTimeout)
	defer cancel()
	records := make([]runner.Verification, 0, len(v.checks))
	for _, check := range v.checks {
		if total.Err() != nil {
			reason := runner.VerificationReasonBudget
			if ctx.Err() != nil {
				reason = runner.VerificationReasonInterrupted
			}
			records = append(records, v.record(check, runner.VerificationNotRun, reason, 0))
			continue
		}
		window := time.Now().Add(time.Duration(check.WithinMS) * time.Millisecond)
		for {
			result, reason, status := v.probe(total, check)
			if result == runner.VerificationVerified || !time.Now().Add(verifyPollInterval).Before(window) || total.Err() != nil {
				records = append(records, v.record(check, result, reason, status))
				break
			}
			select {
			case <-total.Done():
			case <-time.After(verifyPollInterval):
			}
		}
	}
	return records
}

// probe makes one request and compares the answer. Nothing of the answer
// but its status leaves this function.
func (v *verifier) probe(ctx context.Context, check verifyCheck) (runner.VerificationResult, string, int) {
	ctx, cancel := context.WithTimeout(ctx, verifyProbeTimeout)
	defer cancel()
	unreachable := func(err error) (runner.VerificationResult, string, int) {
		reason := runner.VerificationReasonConnection
		switch described := waitReason(err); {
		case ctx.Err() != nil || strings.HasSuffix(described, "timed out"):
			reason = runner.VerificationReasonTimeout
		case described == "certificate not trusted" || described == "tls handshake failed":
			reason = runner.VerificationReasonTLS
		}
		return runner.VerificationUnreachable, reason, 0
	}
	request, err := http.NewRequestWithContext(ctx, check.Method, check.target.String(), nil)
	if err != nil {
		return unreachable(err)
	}
	if check.Header != nil {
		request.Header.Set(check.Header.Name, check.headerValue)
	}
	if check.JSONPointer != nil {
		request.Header.Set("Accept", "application/json")
	}
	response, err := v.client.Do(request)
	if err != nil {
		return unreachable(err)
	}
	defer response.Body.Close()
	status := response.StatusCode
	if status != check.ExpectStatus {
		return runner.VerificationContradicted, runner.VerificationReasonStatus, status
	}
	if check.JSONPointer == nil {
		return runner.VerificationVerified, "", status
	}
	body, err := io.ReadAll(io.LimitReader(response.Body, verifyMaxBodyBytes+1))
	if err != nil {
		_, reason, _ := unreachable(err)
		return runner.VerificationUnreachable, reason, status
	}
	if len(body) > verifyMaxBodyBytes {
		return runner.VerificationUnreachable, runner.VerificationReasonBodyLimit, status
	}
	document, err := decodeJSONValue(body)
	if err != nil {
		return runner.VerificationContradicted, runner.VerificationReasonNotJSON, status
	}
	observed, found := resolvePointer(document, check.tokens)
	if !found {
		return runner.VerificationContradicted, runner.VerificationReasonJSONPointer, status
	}
	if !jsonEqual(observed, check.expected) {
		return runner.VerificationContradicted, runner.VerificationReasonJSONValue, status
	}
	return runner.VerificationVerified, "", status
}
