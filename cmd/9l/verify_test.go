package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Quality-Max/9lives-runner/internal/runner"
)

// Canaries that must never reach a receipt, a report or the CLI's output.
const (
	verifyBodyCanary     = "BODY-CANARY-7f3a"
	verifyExpectedCanary = "EXPECTED-CANARY-91bd"
	verifyQueryCanary    = "QUERY-CANARY-c2e0"
	verifyHeaderCanary   = "HEADER-CANARY-5d18"
	verifyHeaderEnv      = "NINELIVES_TEST_VERIFY_KEY"
)

func writeChecks(t *testing.T, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "checks.json")
	writeTestFile(t, path, content)
	return path
}

// orderService is the application under test: one stored order, a private
// endpoint behind a key, a redirect and a body larger than the engine reads.
func orderService(t *testing.T) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("/orders/42", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprintf(w, `{"status":"paid","total":12.50,"count":2,"items":[{"sku":"A1"},{"sku":"B~/2"}],"a/b":{"c~d":true},"note":%q}`, verifyBodyCanary)
	})
	mux.HandleFunc("/private", func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-Api-Key") != verifyHeaderCanary {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		fmt.Fprint(w, `{"ok":true}`)
	})
	mux.HandleFunc("/moved", func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "/orders/42", http.StatusFound)
	})
	mux.HandleFunc("/text", func(w http.ResponseWriter, r *http.Request) { fmt.Fprint(w, "<html>"+verifyBodyCanary) })
	mux.HandleFunc("/huge", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{"status":"paid","pad":"`+strings.Repeat("x", verifyMaxBodyBytes)+`"}`)
	})
	server := httptest.NewServer(mux)
	t.Cleanup(server.Close)
	return server
}

func verifyOne(t *testing.T, check string) runner.Verification {
	t.Helper()
	loaded, err := loadVerifier(writeChecks(t, `{"version":1,"checks":[`+check+`]}`), nil)
	if err != nil {
		t.Fatalf("checks rejected: %v", err)
	}
	records := loaded.Verify(context.Background())
	if len(records) != 1 || records[0].Validate() != nil || records[0].ID != "c" {
		t.Fatalf("records=%+v", records)
	}
	return records[0]
}

func shortenVerifyTimes(t *testing.T, probe, total, poll time.Duration) {
	t.Helper()
	oldProbe, oldTotal, oldPoll := verifyProbeTimeout, verifyTotalTimeout, verifyPollInterval
	verifyProbeTimeout, verifyTotalTimeout, verifyPollInterval = probe, total, poll
	t.Cleanup(func() { verifyProbeTimeout, verifyTotalTimeout, verifyPollInterval = oldProbe, oldTotal, oldPoll })
}

func TestVerifyComparesTheApplicationsOwnAnswer(t *testing.T) {
	server := orderService(t)
	for _, test := range []struct {
		name, check string
		result      runner.VerificationResult
		reason      string
		status      int
	}{
		{"status only", `{"id":"c","method":"GET","url":"%s/orders/42","expectStatus":200}`, runner.VerificationVerified, "", 200},
		{"head", `{"id":"c","method":"HEAD","url":"%s/orders/42","expectStatus":200}`, runner.VerificationVerified, "", 200},
		{"string value", `{"id":"c","method":"GET","url":"%s/orders/42","expectStatus":200,"jsonPointer":"/status","equals":"paid"}`, runner.VerificationVerified, "", 200},
		{"number by value", `{"id":"c","method":"GET","url":"%s/orders/42","expectStatus":200,"jsonPointer":"/total","equals":12.5}`, runner.VerificationVerified, "", 200},
		{"array item", `{"id":"c","method":"GET","url":"%s/orders/42","expectStatus":200,"jsonPointer":"/items/1","equals":{"sku":"B~/2"}}`, runner.VerificationVerified, "", 200},
		{"escaped pointer", `{"id":"c","method":"GET","url":"%s/orders/42","expectStatus":200,"jsonPointer":"/a~1b/c~0d","equals":true}`, runner.VerificationVerified, "", 200},
		{"expected absence", `{"id":"c","method":"GET","url":"%s/orders/43","expectStatus":404}`, runner.VerificationVerified, "", 404},

		{"wrong status", `{"id":"c","method":"GET","url":"%s/orders/43","expectStatus":200}`, runner.VerificationContradicted, runner.VerificationReasonStatus, 404},
		{"wrong value", `{"id":"c","method":"GET","url":"%s/orders/42","expectStatus":200,"jsonPointer":"/status","equals":"refunded"}`, runner.VerificationContradicted, runner.VerificationReasonJSONValue, 200},
		{"wrong type", `{"id":"c","method":"GET","url":"%s/orders/42","expectStatus":200,"jsonPointer":"/count","equals":"2"}`, runner.VerificationContradicted, runner.VerificationReasonJSONValue, 200},
		{"integer is exact", `{"id":"c","method":"GET","url":"%s/orders/42","expectStatus":200,"jsonPointer":"/count","equals":3}`, runner.VerificationContradicted, runner.VerificationReasonJSONValue, 200},
		{"extra member", `{"id":"c","method":"GET","url":"%s/orders/42","expectStatus":200,"jsonPointer":"/items/0","equals":{"sku":"A1","qty":1}}`, runner.VerificationContradicted, runner.VerificationReasonJSONValue, 200},
		{"missing pointer", `{"id":"c","method":"GET","url":"%s/orders/42","expectStatus":200,"jsonPointer":"/refund/status","equals":"paid"}`, runner.VerificationContradicted, runner.VerificationReasonJSONPointer, 200},
		{"index past the end", `{"id":"c","method":"GET","url":"%s/orders/42","expectStatus":200,"jsonPointer":"/items/2","equals":"x"}`, runner.VerificationContradicted, runner.VerificationReasonJSONPointer, 200},
		{"not json", `{"id":"c","method":"GET","url":"%s/text","expectStatus":200,"jsonPointer":"/status","equals":"paid"}`, runner.VerificationContradicted, runner.VerificationReasonNotJSON, 200},
		// The redirect is the answer; the order it points to is never fetched.
		{"redirect not followed", `{"id":"c","method":"GET","url":"%s/moved","expectStatus":200,"jsonPointer":"/status","equals":"paid"}`, runner.VerificationContradicted, runner.VerificationReasonStatus, 302},
		{"redirect expected", `{"id":"c","method":"GET","url":"%s/moved","expectStatus":302}`, runner.VerificationVerified, "", 302},

		{"oversized body", `{"id":"c","method":"GET","url":"%s/huge","expectStatus":200,"jsonPointer":"/status","equals":"paid"}`, runner.VerificationUnreachable, runner.VerificationReasonBodyLimit, 200},
	} {
		t.Run(test.name, func(t *testing.T) {
			record := verifyOne(t, fmt.Sprintf(test.check, server.URL))
			if record.Result != test.result || record.Reason != test.reason || record.HTTPStatus != test.status || record.ObservedBy != "engine" {
				t.Fatalf("record=%+v", record)
			}
		})
	}
}

func TestVerifyReportsAnApplicationItCannotReachAsUnreachable(t *testing.T) {
	record := verifyOne(t, fmt.Sprintf(`{"id":"c","method":"GET","url":"http://%s/orders/42","expectStatus":200}`, closedPort(t)))
	if record.Result != runner.VerificationUnreachable || record.HTTPStatus != 0 || (record.Reason != runner.VerificationReasonConnection && record.Reason != runner.VerificationReasonTimeout) {
		t.Fatalf("refused connection: %+v", record)
	}

	release := make(chan struct{})
	slow := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-release:
		case <-r.Context().Done():
		}
	}))
	t.Cleanup(func() { close(release); slow.Close() })
	shortenVerifyTimes(t, 150*time.Millisecond, 400*time.Millisecond, 20*time.Millisecond)
	started := time.Now()
	record = verifyOne(t, fmt.Sprintf(`{"id":"c","method":"GET","url":"%s","expectStatus":200}`, slow.URL))
	if record.Result != runner.VerificationUnreachable || record.Reason != runner.VerificationReasonTimeout || record.HTTPStatus != 0 || time.Since(started) > 5*time.Second {
		t.Fatalf("timeout: %+v after %s", record, time.Since(started))
	}

	// Every check shares one time budget; what it leaves unprobed did not run.
	check := `{"id":"%s","method":"GET","url":"` + slow.URL + `","expectStatus":200}`
	loaded, err := loadVerifier(writeChecks(t, `{"version":1,"checks":[`+fmt.Sprintf(check, "a")+","+fmt.Sprintf(check, "b")+","+fmt.Sprintf(check, "c")+","+fmt.Sprintf(check, "d")+`]}`), nil)
	if err != nil {
		t.Fatal(err)
	}
	started = time.Now()
	records := loaded.Verify(context.Background())
	if len(records) != 4 || records[0].Result != runner.VerificationUnreachable || records[3].Result != runner.VerificationNotRun || records[3].Reason != runner.VerificationReasonBudget || time.Since(started) > 5*time.Second {
		t.Fatalf("budget: %+v after %s", records, time.Since(started))
	}
	// A run that was canceled probes nothing.
	canceled, cancel := context.WithCancel(context.Background())
	cancel()
	for _, record := range loaded.Verify(canceled) {
		if record.Result != runner.VerificationNotRun || record.Reason != runner.VerificationReasonInterrupted {
			t.Fatalf("canceled: %+v", record)
		}
	}
}

func TestVerifyPollsOnlyWithinTheDeclaredWindow(t *testing.T) {
	shortenVerifyTimes(t, 2*time.Second, 20*time.Second, 20*time.Millisecond)
	var calls atomic.Int32
	readyAfter := int32(4)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if calls.Add(1) < atomic.LoadInt32(&readyAfter) {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		fmt.Fprint(w, `{"status":"paid"}`)
	}))
	t.Cleanup(server.Close)
	check := `{"id":"c","method":"GET","url":"` + server.URL + `","expectStatus":200,"jsonPointer":"/status","equals":"paid"%s}`

	// Without withinMs there is exactly one probe, and it is the verdict.
	if record := verifyOne(t, fmt.Sprintf(check, "")); record.Result != runner.VerificationContradicted || record.HTTPStatus != 404 || calls.Load() != 1 {
		t.Fatalf("single probe: %+v after %d call(s)", record, calls.Load())
	}
	calls.Store(0)
	if record := verifyOne(t, fmt.Sprintf(check, `,"withinMs":10000`)); record.Result != runner.VerificationVerified || record.HTTPStatus != 200 || calls.Load() != 4 {
		t.Fatalf("late success: %+v after %d call(s)", record, calls.Load())
	}
	calls.Store(0)
	atomic.StoreInt32(&readyAfter, 1<<30)
	started := time.Now()
	record := verifyOne(t, fmt.Sprintf(check, `,"withinMs":300`))
	if record.Result != runner.VerificationContradicted || record.Reason != runner.VerificationReasonStatus || record.HTTPStatus != 404 || calls.Load() < 2 || time.Since(started) < 200*time.Millisecond || time.Since(started) > 10*time.Second {
		t.Fatalf("never holds: %+v after %d call(s) in %s", record, calls.Load(), time.Since(started))
	}
}

func TestVerifyHeaderValueComesFromAnEngineOnlyVariable(t *testing.T) {
	server := orderService(t)
	check := `{"version":1,"checks":[{"id":"c","method":"GET","url":"` + server.URL + `/private","expectStatus":200,"jsonPointer":"/ok","equals":true,"header":{"name":"X-Api-Key","env":"` + verifyHeaderEnv + `"}}]}`
	path := writeChecks(t, check)
	if _, err := loadVerifier(path, nil); err == nil || strings.Contains(err.Error(), server.URL) {
		t.Fatalf("an unset header variable must be refused without echoing the file: %v", err)
	}
	t.Setenv(verifyHeaderEnv, verifyHeaderCanary)
	loaded, err := loadVerifier(path, nil)
	if err != nil {
		t.Fatal(err)
	}
	if records := loaded.Verify(context.Background()); records[0].Result != runner.VerificationVerified {
		t.Fatalf("authenticated check: %+v", records)
	}
	// A variable the worker also receives is not the engine's alone.
	for _, passed := range [][]string{{verifyHeaderEnv}, {"OTHER", strings.ToLower(verifyHeaderEnv)}} {
		if _, err := loadVerifier(path, passed); err == nil || strings.Contains(err.Error(), verifyHeaderCanary) {
			t.Fatalf("--pass-env %v: err=%v", passed, err)
		}
	}
	t.Setenv("HOME", verifyHeaderCanary)
	if _, err := loadVerifier(writeChecks(t, strings.ReplaceAll(check, verifyHeaderEnv, "HOME")), nil); err == nil {
		t.Fatal("a variable every worker inherits was accepted as a header source")
	}
}

func TestMalformedChecksFilesAreRejected(t *testing.T) {
	valid := `{"id":"c","method":"GET","url":"http://127.0.0.1:1/x","expectStatus":200}`
	if _, err := parseVerifyFile([]byte(`{"version":1,"checks":[` + valid + `]}`)); err != nil {
		t.Fatal(err)
	}
	many := strings.TrimSuffix(strings.Repeat(`{"id":"c%d","method":"GET","url":"http://127.0.0.1:1/x","expectStatus":200},`, 17), ",")
	ids := make([]any, 17)
	for index := range ids {
		ids[index] = index
	}
	for name, raw := range map[string]string{
		"not json":            `checks`,
		"array":               `[` + valid + `]`,
		"no version":          `{"checks":[` + valid + `]}`,
		"other version":       `{"version":2,"checks":[` + valid + `]}`,
		"unknown top member":  `{"version":1,"checks":[` + valid + `],"notes":"x"}`,
		"duplicate member":    `{"version":1,"version":1,"checks":[` + valid + `]}`,
		"trailing document":   `{"version":1,"checks":[` + valid + `]} {}`,
		"no checks":           `{"version":1,"checks":[]}`,
		"too many checks":     `{"version":1,"checks":[` + fmt.Sprintf(many, ids...) + `]}`,
		"duplicate id":        `{"version":1,"checks":[` + valid + `,` + valid + `]}`,
		"unknown check field": `{"version":1,"checks":[{"id":"c","method":"GET","url":"http://127.0.0.1:1/x","expectStatus":200,"body":"x"}]}`,
		"bad id":              `{"version":1,"checks":[{"id":"has space","method":"GET","url":"http://127.0.0.1:1/x","expectStatus":200}]}`,
		"bad outcome id":      `{"version":1,"checks":[{"id":"c","outcomeId":"a/b","method":"GET","url":"http://127.0.0.1:1/x","expectStatus":200}]}`,
		"post":                `{"version":1,"checks":[{"id":"c","method":"POST","url":"http://127.0.0.1:1/x","expectStatus":200}]}`,
		"lowercase method":    `{"version":1,"checks":[{"id":"c","method":"get","url":"http://127.0.0.1:1/x","expectStatus":200}]}`,
		"file url":            `{"version":1,"checks":[{"id":"c","method":"GET","url":"file:///etc/hosts","expectStatus":200}]}`,
		"url with user info":  `{"version":1,"checks":[{"id":"c","method":"GET","url":"http://user:` + verifyQueryCanary + `@127.0.0.1:1/x","expectStatus":200}]}`,
		"url with fragment":   `{"version":1,"checks":[{"id":"c","method":"GET","url":"http://127.0.0.1:1/x#f","expectStatus":200}]}`,
		"url with bad port":   `{"version":1,"checks":[{"id":"c","method":"GET","url":"http://127.0.0.1:99999/x","expectStatus":200}]}`,
		"url without host":    `{"version":1,"checks":[{"id":"c","method":"GET","url":"http:///x","expectStatus":200}]}`,
		"no status":           `{"version":1,"checks":[{"id":"c","method":"GET","url":"http://127.0.0.1:1/x"}]}`,
		"status as string":    `{"version":1,"checks":[{"id":"c","method":"GET","url":"http://127.0.0.1:1/x","expectStatus":"200"}]}`,
		"pointer alone":       `{"version":1,"checks":[{"id":"c","method":"GET","url":"http://127.0.0.1:1/x","expectStatus":200,"jsonPointer":"/a"}]}`,
		"equals alone":        `{"version":1,"checks":[{"id":"c","method":"GET","url":"http://127.0.0.1:1/x","expectStatus":200,"equals":1}]}`,
		"equals null":         `{"version":1,"checks":[{"id":"c","method":"GET","url":"http://127.0.0.1:1/x","expectStatus":200,"jsonPointer":"/a","equals":null}]}`,
		"pointer on head":     `{"version":1,"checks":[{"id":"c","method":"HEAD","url":"http://127.0.0.1:1/x","expectStatus":200,"jsonPointer":"/a","equals":1}]}`,
		"relative pointer":    `{"version":1,"checks":[{"id":"c","method":"GET","url":"http://127.0.0.1:1/x","expectStatus":200,"jsonPointer":"a","equals":1}]}`,
		"bad pointer escape":  `{"version":1,"checks":[{"id":"c","method":"GET","url":"http://127.0.0.1:1/x","expectStatus":200,"jsonPointer":"/a~2","equals":1}]}`,
		"negative window":     `{"version":1,"checks":[{"id":"c","method":"GET","url":"http://127.0.0.1:1/x","expectStatus":200,"withinMs":-1}]}`,
		"window too long":     `{"version":1,"checks":[{"id":"c","method":"GET","url":"http://127.0.0.1:1/x","expectStatus":200,"withinMs":30001}]}`,
		"literal header":      `{"version":1,"checks":[{"id":"c","method":"GET","url":"http://127.0.0.1:1/x","expectStatus":200,"header":{"name":"Authorization","value":"Bearer x"}}]}`,
		"reserved header":     `{"version":1,"checks":[{"id":"c","method":"GET","url":"http://127.0.0.1:1/x","expectStatus":200,"header":{"name":"Host","env":"X"}}]}`,
		"header env as value": `{"version":1,"checks":[{"id":"c","method":"GET","url":"http://127.0.0.1:1/x","expectStatus":200,"header":{"name":"X-Key","env":"Bearer abc"}}]}`,
	} {
		_, err := parseVerifyFile([]byte(raw))
		if err == nil {
			t.Errorf("%s: accepted", name)
		} else if strings.Contains(err.Error(), verifyQueryCanary) || strings.Contains(err.Error(), "127.0.0.1") {
			t.Errorf("%s: the error echoes the file: %v", name, err)
		}
	}
}

// verifiedProject is a stub project whose one test passes, and a checks file
// against the order service. `during` runs inside the test process.
func verifiedProject(t *testing.T, checks, during string) (spec, checksPath, receipts string) {
	t.Helper()
	spec = stubProject(t, junitPassingReport, 0, during)
	return spec, writeChecks(t, checks), t.TempDir()
}

func runVerified(t *testing.T, args ...string) (int, runner.RunSummary, string) {
	t.Helper()
	var stdout, stderr bytes.Buffer
	code := run(append([]string{"run"}, append(args, "--format", "json", "--timeout", "30s")...), &stdout, &stderr)
	var result runner.RunSummary
	if stdout.Len() > 0 {
		if err := json.Unmarshal(stdout.Bytes(), &result); err != nil {
			t.Fatalf("result does not parse: %v\n%s", err, stdout.String())
		}
	}
	return code, result, stdout.String() + stderr.String()
}

func TestRunVerifyDecidesAGreenTestFromOutsideTheWorker(t *testing.T) {
	server := orderService(t)
	t.Setenv(verifyHeaderEnv, verifyHeaderCanary)
	refused := "http://" + closedPort(t)
	shortenVerifyTimes(t, 3*time.Second, 20*time.Second, 20*time.Millisecond)
	paid := `{"id":"order-paid","outcomeId":"order-stored","method":"GET","url":"` + server.URL + `/orders/42?token=` + verifyQueryCanary + `","expectStatus":200,"jsonPointer":"/status","equals":"paid"}`
	private := `{"id":"private-ok","method":"GET","url":"` + server.URL + `/private","expectStatus":200,"header":{"name":"X-Api-Key","env":"` + verifyHeaderEnv + `"}}`
	wrong := `{"id":"order-note","method":"GET","url":"` + server.URL + `/orders/42","expectStatus":200,"jsonPointer":"/note","equals":"` + verifyExpectedCanary + `"}`
	down := `{"id":"ledger-up","method":"GET","url":"` + refused + `/ledger","expectStatus":200}`
	for _, test := range []struct {
		name, checks string
		code         int
		outcome      runner.RunOutcome
		status       runner.ReceiptStatus
		stop         string
		verified     int
		junit        junitTotals
		text         []string
	}{
		{"verified", paid + "," + private, exitPassed, runner.OutcomePassed, runner.StatusPassed, "", 2, junitTotals{Tests: 4, Skipped: 1},
			[]string{"PASSED", "✓ verify order-paid: verified, GET answered HTTP 200, outcome order-stored", "✓ verify private-ok: verified"}},
		{"contradicted", paid + "," + wrong, exitFailed, runner.OutcomeFailed, runner.StatusFailed, runner.StopVerificationContradicted, 1, junitTotals{Tests: 4, Skipped: 1, Failures: 1},
			[]string{"FAILED", "✗ verify order-note: contradicted (json_value), GET answered HTTP 200", "independent verification contradicted the result: order-note", "FAILED: every planned job ran and an independent check contradicted the result"}},
		{"unreachable", paid + "," + down, exitIncomplete, runner.OutcomeIncomplete, runner.StatusError, runner.StopVerificationIncomplete, 1, junitTotals{Tests: 3, Errors: 2},
			[]string{"ERROR", "? verify ledger-up: unreachable (", "independent verification could not be completed: ledger-up", "INCOMPLETE"}},
	} {
		t.Run(test.name, func(t *testing.T) {
			spec, checks, receipts := verifiedProject(t, `{"version":1,"checks":[`+test.checks+`]}`, "")
			report := filepath.Join(t.TempDir(), "junit.xml")
			code, result, output := runVerified(t, spec, "--verify", checks, "--receipt-dir", receipts, "--junit", report)
			if code != test.code || result.Outcome != test.outcome || len(result.Receipts) != 1 {
				t.Fatalf("exit %d outcome %s: %s", code, result.Outcome, output)
			}
			receipt := result.Receipts[0]
			if receipt.Status != test.status || !receipt.Validated || receipt.VerifiedAssertions != test.verified || receipt.AssertionCoverage != "unknown" || len(receipt.VerificationSHA256) != 64 || !receipt.ValidVerifications() || len(receipt.Verifications) != 2 {
				t.Fatalf("receipt=%+v", receipt)
			}
			if (receipt.FailureSummary == nil) != (test.stop == "") || (receipt.FailureSummary != nil && receipt.FailureSummary.Stop != test.stop) {
				t.Fatalf("failure summary %+v, want %q", receipt.FailureSummary, test.stop)
			}
			totals, xml := readJUnit(t, report)
			if totals != test.junit {
				t.Fatalf("JUnit totals %+v, want %+v:\n%s", totals, test.junit, xml)
			}
			// `9l result` names each check, in text and in a rebuilt report.
			var text, stderr bytes.Buffer
			again := filepath.Join(t.TempDir(), "again.xml")
			if code := run([]string{"result", result.RunID, "--receipt-dir", receipts, "--format", "text", "--junit", again}, &text, &stderr); code != 0 {
				t.Fatalf("result exit %d: %s", code, stderr.String())
			}
			for _, want := range test.text {
				if !strings.Contains(text.String(), want) {
					t.Fatalf("text output lacks %q:\n%s", want, text.String())
				}
			}
			if _, rebuilt := readJUnit(t, again); rebuilt != xml {
				t.Fatalf("result --junit differs from run --junit")
			}
			// Nothing the engine compared or sent is kept or printed.
			leaks := []string{verifyBodyCanary, verifyExpectedCanary, verifyQueryCanary, verifyHeaderCanary, verifyHeaderEnv, strings.TrimPrefix(server.URL, "http://"), strings.TrimPrefix(refused, "http://"), "/orders/42", "X-Api-Key"}
			sources := map[string]string{"output": output, "result text": text.String(), "junit": xml}
			_ = filepath.WalkDir(receipts, func(path string, entry os.DirEntry, err error) error {
				if err == nil && entry.Type().IsRegular() {
					raw, _ := os.ReadFile(path)
					sources[path] = string(raw)
				}
				return nil
			})
			if len(sources) < 6 {
				t.Fatalf("receipts were not persisted: %d source(s)", len(sources))
			}
			for name, content := range sources {
				for _, leak := range leaks {
					if strings.Contains(content, leak) {
						t.Fatalf("%s holds %q", name, leak)
					}
				}
			}
		})
	}
}

func TestRunVerifyPassesNothingToTheTestProcess(t *testing.T) {
	server := orderService(t)
	t.Setenv(verifyHeaderEnv, verifyHeaderCanary)
	marker := filepath.Join(t.TempDir(), "leaked")
	started := filepath.Join(t.TempDir(), "started")
	checksDir := t.TempDir()
	checks := filepath.Join(checksDir, "postconditions.json")
	writeTestFile(t, checks, `{"version":1,"checks":[{"id":"private-ok","method":"GET","url":"`+server.URL+`/private","expectStatus":200,"header":{"name":"X-Api-Key","env":"`+verifyHeaderEnv+`"}}]}`)
	// The test process looks for every trace of the checks in its own
	// environment and arguments; it records only whether it found one.
	during := "touch '" + started + "'\n"
	for _, needle := range []string{verifyHeaderCanary, verifyHeaderEnv, "postconditions.json", checksDir, strings.TrimPrefix(server.URL, "http://"), "private-ok"} {
		during += "if env | grep -q -i -F '" + needle + "' || echo \"$*\" | grep -q -i -F '" + needle + "'; then echo '" + needle + "' >> '" + marker + "'; fi\n"
	}
	spec := stubProject(t, junitPassingReport, 0, during)
	code, result, output := runVerified(t, spec, "--verify", checks, "--receipt-dir", t.TempDir())
	if code != exitPassed || result.Receipts[0].VerifiedAssertions != 1 {
		t.Fatalf("exit %d: %s", code, output)
	}
	if _, err := os.Stat(started); err != nil {
		t.Fatal("the test process did not run its probe script")
	}
	if found, err := os.ReadFile(marker); err == nil {
		t.Fatalf("the test process could see: %s", found)
	}
	// Control: the same script does find a value the run forwards.
	t.Setenv("NINELIVES_TEST_VERIFY_CONTROL", strings.TrimPrefix(server.URL, "http://"))
	if code, _, output := runVerified(t, spec, "--verify", checks, "--receipt-dir", t.TempDir(), "--pass-env", "NINELIVES_TEST_VERIFY_CONTROL"); code != exitPassed {
		t.Fatalf("control run exit %d: %s", code, output)
	}
	if _, err := os.Stat(marker); err != nil {
		t.Fatal("the probe script cannot detect a forwarded value, so its silence proves nothing")
	}
}

func TestRunVerifyRefusesAChecksFileChangedDuringTheRun(t *testing.T) {
	server := orderService(t)
	var probes atomic.Int32
	counted := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { probes.Add(1) }))
	t.Cleanup(counted.Close)
	_ = server
	checks := writeChecks(t, `{"version":1,"checks":[{"id":"up","method":"GET","url":"`+counted.URL+`/a","expectStatus":200}]}`)
	// The test process weakens the check while it runs.
	spec := stubProject(t, junitPassingReport, 0, `printf '{"version":1,"checks":[{"id":"up","method":"GET","url":"`+counted.URL+`/b","expectStatus":200}]}' > '`+checks+`'`)
	code, result, output := runVerified(t, spec, "--verify", checks, "--receipt-dir", t.TempDir())
	if code != exitIncomplete || result.Outcome != runner.OutcomeIncomplete || probes.Load() != 0 {
		t.Fatalf("exit %d outcome %s probes %d: %s", code, result.Outcome, probes.Load(), output)
	}
	receipt := result.Receipts[0]
	if receipt.Status != runner.StatusError || receipt.FailureSummary.Stop != runner.StopVerificationChanged || receipt.VerifiedAssertions != 0 ||
		receipt.Verifications[0].Result != runner.VerificationNotRun || receipt.Verifications[0].Reason != runner.VerificationReasonFileChanged {
		t.Fatalf("receipt=%+v", receipt)
	}
}

func TestRunVerifyLeavesAFailingTestFailed(t *testing.T) {
	server := orderService(t)
	spec := stubProject(t, junitFailingReport, 1, "")
	checks := writeChecks(t, `{"version":1,"checks":[{"id":"order-paid","method":"GET","url":"`+server.URL+`/orders/42","expectStatus":200}]}`)
	code, result, output := runVerified(t, spec, "--verify", checks, "--receipt-dir", t.TempDir())
	receipt := result.Receipts[0]
	if code != exitFailed || receipt.Status != runner.StatusFailed || receipt.FailureSummary.Stop != runner.StopTestFailed || receipt.Verifications[0].Result != runner.VerificationVerified {
		t.Fatalf("exit %d receipt=%+v: %s", code, receipt, output)
	}
}

func TestBadVerifyInputIsAUsageErrorBeforeAnythingRuns(t *testing.T) {
	started := filepath.Join(t.TempDir(), "started")
	spec := stubProject(t, junitPassingReport, 0, "touch '"+started+"'")
	second := filepath.Join(filepath.Dir(spec), "b.spec.ts")
	writeTestFile(t, second, "// stub\n")
	valid := `{"version":1,"checks":[{"id":"c","method":"GET","url":"http://127.0.0.1:1/x?token=` + verifyQueryCanary + `","expectStatus":200}]}`
	for name, args := range map[string][]string{
		"malformed":       {spec, "--verify", writeChecks(t, `{"version":1,"checks":[{"id":"c","method":"DELETE","url":"http://127.0.0.1:1/x?token=`+verifyQueryCanary+`","expectStatus":200}]}`)},
		"not json":        {spec, "--verify", writeChecks(t, verifyQueryCanary)},
		"oversized":       {spec, "--verify", writeChecks(t, `{"version":1,"checks":[],"pad":"`+strings.Repeat("x", verifyMaxFileBytes)+`"}`)},
		"missing file":    {spec, "--verify", filepath.Join(t.TempDir(), "absent.json")},
		"directory":       {spec, "--verify", t.TempDir()},
		"empty path":      {spec, "--verify", ""},
		"two specs":       {spec, second, "--verify", writeChecks(t, valid)},
		"dry run too":     {spec, second, "--verify", writeChecks(t, valid), "--dry-run"},
		"with a skip":     {spec, filepath.Join(filepath.Dir(spec), "notes.txt"), "--verify", writeChecks(t, valid)},
		"malformed, plan": {spec, "--verify", writeChecks(t, `{"version":3}`), "--dry-run"},
	} {
		receipts := filepath.Join(t.TempDir(), "receipts")
		var stdout, stderr bytes.Buffer
		code := run(append([]string{"run"}, append(args, "--receipt-dir", receipts)...), &stdout, &stderr)
		if code != exitUsage || stdout.Len() != 0 || !strings.Contains(stderr.String(), "--verify") || strings.Contains(stderr.String(), verifyQueryCanary) {
			t.Errorf("%s: exit %d stdout=%q stderr=%q", name, code, stdout.String(), stderr.String())
		}
		if _, err := os.Stat(receipts); !os.IsNotExist(err) {
			t.Errorf("%s: a refused run created storage", name)
		}
	}
	if _, err := os.Stat(started); err == nil {
		t.Fatal("a refused run started a test process")
	}
}

func TestPrintResultWithholdsMalformedVerificationRecords(t *testing.T) {
	var out bytes.Buffer
	printResult(&out, runner.RunSummary{Outcome: runner.OutcomeIncomplete, Receipts: []runner.Receipt{{Status: runner.StatusPassed, Spec: "a.spec.ts",
		Verifications: []runner.Verification{{ID: "http://host/?token=" + verifyQueryCanary, Method: "GET", Result: "verified", HTTPStatus: 200, ObservedBy: "engine"}}}}})
	if strings.Contains(out.String(), verifyQueryCanary) || !strings.Contains(out.String(), "malformed") {
		t.Fatalf("text summary:\n%s", out.String())
	}
}
