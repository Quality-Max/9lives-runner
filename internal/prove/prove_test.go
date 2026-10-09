package prove

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const origin = "http://127.0.0.1:4100"

func writeChannel(t *testing.T, files ...string) string {
	t.Helper()
	dir := t.TempDir()
	for index, content := range files {
		name := strings.Repeat("a", 31) + string(rune('0'+index)) + ".ndjson"
		if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

const testA, testB = "a000000000000000000000000000000000000000000000000000000000000000", "b000000000000000000000000000000000000000000000000000000000000000"

func hello(mode string) string { return helloFor(mode, testA, 0) }

func helloFor(mode, test string, retry int) string {
	return `{"type":"hello","protocol":"9l.prove/1","mode":"` + mode + `","testId":"` + test + `","retry":` + itoa(retry) + `}` + "\n"
}

func observed(method, path string, status int, json bool) string {
	flag := "false"
	if json {
		flag = "true"
	}
	return `{"type":"request","method":"` + method + `","origin":"` + origin + `","path":"` + path + `","resourceType":"fetch"}` + "\n" +
		`{"type":"response","method":"` + method + `","origin":"` + origin + `","path":"` + path + `","status":` + itoa(status) + `,"json":` + flag + "}\n"
}

func itoa(value int) string {
	if value == 0 {
		return "0"
	}
	digits := ""
	for ; value > 0; value /= 10 {
		digits = string(rune('0'+value%10)) + digits
	}
	return digits
}

func TestReadChannelAggregatesRequestsAcrossAttempts(t *testing.T) {
	dir := writeChannel(t,
		hello("observe")+observed("GET", "/api/cart", 200, true)+observed("POST", "/api/orders", 201, true),
		helloFor("observe", testB, 0)+observed("GET", "/api/cart", 200, true)+observed("GET", "/api/text", 200, false),
	)
	observations, err := ReadChannel(dir, "observe", "")
	if err != nil {
		t.Fatal(err)
	}
	if observations.Attempts != 2 || len(observations.Requests) != 3 {
		t.Fatalf("unexpected observations: %#v", observations)
	}
	cart := observations.Requests["GET "+origin+"/api/cart"]
	if cart.Observed != 2 || !cart.JSON2xx || cart.ResourceType != "fetch" {
		t.Fatalf("unexpected cart aggregate: %#v", cart)
	}
	if observations.Requests["GET "+origin+"/api/text"].JSON2xx {
		t.Fatal("a non-JSON response must not enable empty-json")
	}
}

func TestReadChannelRejectsUntrustedRecords(t *testing.T) {
	valid := hello("observe")
	cases := map[string]string{
		"missing handshake":     observed("GET", "/a", 200, true),
		"wrong mode":            hello("fault") + observed("GET", "/a", 200, true),
		"wrong protocol":        `{"type":"hello","protocol":"9l.prove/2","mode":"observe","testId":"` + testA + `","retry":0}` + "\n",
		"short test id":         `{"type":"hello","protocol":"9l.prove/1","mode":"observe","testId":"abc","retry":0}` + "\n",
		"negative retry":        helloFor("observe", testA, 0)[:len(helloFor("observe", testA, 0))-3] + "-1}\n",
		"unknown field":         valid + `{"type":"request","method":"GET","origin":"` + origin + `","path":"/a","resourceType":"fetch","url":"x"}` + "\n",
		"missing field":         valid + `{"type":"request","method":"GET","origin":"` + origin + `","path":"/a"}` + "\n",
		"duplicate member":      valid + `{"type":"overflow","type":"overflow"}` + "\n",
		"null value":            valid + `{"type":"request","method":"GET","origin":"` + origin + `","path":null,"resourceType":"fetch"}` + "\n",
		"query in path":         valid + `{"type":"request","method":"GET","origin":"` + origin + `","path":"/a?token=1","resourceType":"fetch"}` + "\n",
		"credentials in origin": valid + `{"type":"request","method":"GET","origin":"http://user:pw@host","path":"/a","resourceType":"fetch"}` + "\n",
		"origin with path":      valid + `{"type":"request","method":"GET","origin":"http://host/x","path":"/a","resourceType":"fetch"}` + "\n",
		"non-http origin":       valid + `{"type":"request","method":"GET","origin":"file://host","path":"/a","resourceType":"fetch"}` + "\n",
		"lowercase method":      valid + `{"type":"request","method":"get","origin":"` + origin + `","path":"/a","resourceType":"fetch"}` + "\n",
		"document resource":     valid + `{"type":"request","method":"GET","origin":"` + origin + `","path":"/a","resourceType":"document"}` + "\n",
		"response only":         valid + `{"type":"response","method":"GET","origin":"` + origin + `","path":"/a","status":200,"json":true}` + "\n",
		"bad status":            valid + `{"type":"response","method":"GET","origin":"` + origin + `","path":"/a","status":42,"json":true}` + "\n",
		"fault record observed": valid + `{"type":"applied","fault":"fault-1"}` + "\n",
		"record after overflow": valid + `{"type":"overflow"}` + "\n" + observed("GET", "/a", 200, true),
		"unfinished line":       strings.TrimSuffix(valid, "\n"),
		"trailing data":         valid + `{"type":"overflow"} {}` + "\n",
		"string status":         valid + `{"type":"response","method":"GET","origin":"` + origin + `","path":"/a","status":"200","json":true}` + "\n",
	}
	for name, content := range cases {
		t.Run(name, func(t *testing.T) {
			if _, err := ReadChannel(writeChannel(t, content), "observe", ""); err == nil {
				t.Fatal("accepted untrusted prove evidence")
			}
		})
	}
}

func TestReadChannelRejectsForeignFiles(t *testing.T) {
	dir := writeChannel(t, hello("observe"))
	if err := os.WriteFile(filepath.Join(dir, "notes.txt"), []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := ReadChannel(dir, "observe", ""); err == nil {
		t.Fatal("accepted an unexpected file in the evidence directory")
	}
	// Two files claiming the same test attempt would let a worker vouch twice.
	if _, err := ReadChannel(writeChannel(t, hello("observe"), hello("observe")), "observe", ""); err == nil {
		t.Fatal("accepted a duplicate test attempt")
	}
}

func TestReadChannelCountsFaultApplicationsPerTest(t *testing.T) {
	dir := writeChannel(t,
		hello("fault")+`{"type":"applied","fault":"fault-3"}`+"\n"+`{"type":"applied","fault":"fault-3"}`+"\n",
		helloFor("fault", testA, 1)+`{"type":"not-applicable","fault":"fault-3","reason":"unreachable"}`+"\n",
		helloFor("fault", testB, 0)+`{"type":"not-applicable","fault":"fault-3","reason":"not-json"}`+"\n",
	)
	observations, err := ReadChannel(dir, "fault", "fault-3")
	if err != nil {
		t.Fatal(err)
	}
	// Attempts of one test merge; tests stay apart.
	if observations.Attempts != 3 || observations.Applied() != 2 || observations.Tests[testA].Applied != 2 || observations.Tests[testA].NotApplicable[ReasonUnreachable] != 1 || observations.Tests[testB].NotApplicable[ReasonNotJSON] != 1 {
		t.Fatalf("unexpected fault counts: %#v", observations.Tests)
	}
	for name, content := range map[string]string{
		"observation during a fault run": hello("fault") + observed("GET", "/a", 200, true),
		"another fault's record":         hello("fault") + `{"type":"applied","fault":"fault-4"}` + "\n",
		"unknown reason":                 hello("fault") + `{"type":"not-applicable","fault":"fault-3","reason":"other"}` + "\n",
		"missing reason":                 hello("fault") + `{"type":"not-applicable","fault":"fault-3"}` + "\n",
	} {
		if _, err := ReadChannel(writeChannel(t, content), "fault", "fault-3"); err == nil {
			t.Fatalf("%s: accepted", name)
		}
	}
	if _, err := ReadChannel(writeChannel(t, hello("fault")), "fault", ""); err == nil {
		t.Fatal("fault mode without a fault id")
	}
}

func TestPlanOrdersRequestsAndKeepsFaultsBeyondTheBudget(t *testing.T) {
	observations, err := ReadChannel(writeChannel(t, hello("observe")+observed("POST", "/api/orders", 200, true)+observed("GET", "/api/text", 200, false)+observed("GET", "/api/cart", 200, true)), "observe", "")
	if err != nil {
		t.Fatal(err)
	}
	requests, faults, notRun := Plan(observations, 5)
	if len(requests) != 3 || requests[0].ID != "req-1" || requests[0].Method != "GET" || requests[2].Method != "POST" {
		t.Fatalf("requests not ordered by method, origin and path: %#v", requests)
	}
	for _, request := range requests {
		if request.URL != "" || len(request.Digest) != 64 {
			t.Fatalf("plan must not expose URLs by default: %#v", request)
		}
	}
	// cart: 3 kinds, text: abort and http-500 only, orders: 3 kinds.
	if len(faults) != 5 || len(notRun) != 3 {
		t.Fatalf("budget split %d/%d", len(faults), len(notRun))
	}
	kinds := []string{}
	for _, fault := range append(faults, notRun...) {
		kinds = append(kinds, fault.Request+":"+fault.Kind)
	}
	want := "req-1:abort req-1:http-500 req-1:empty-json req-2:abort req-2:http-500 req-3:abort req-3:http-500 req-3:empty-json"
	if strings.Join(kinds, " ") != want {
		t.Fatalf("faults %q, want %q", strings.Join(kinds, " "), want)
	}
	if faults[0].ID != "fault-1" || notRun[2].ID != "fault-8" {
		t.Fatal("fault IDs must be sequential across planned and not-run faults")
	}
	if got := WithURLs(requests)[0].URL; got != origin+"/api/cart" {
		t.Fatalf("local URL %q", got)
	}
	if env := faults[2].Env(); env != `{"id":"fault-3","kind":"empty-json","method":"GET","origin":"`+origin+`","path":"/api/cart"}` {
		t.Fatalf("fault env %s", env)
	}
}

func TestClassifyIsPerTestAndAFaultSurvivesWhenAnyExercisedTestPasses(t *testing.T) {
	passed := TestFacts{Outcome: "expected", Attempts: 1, Applied: 2}
	caught := TestFacts{Outcome: "unexpected", Attempts: 1, AssertionFailed: true, Applied: 1}
	action := TestFacts{Outcome: "unexpected", Attempts: 1, Applied: 1}
	untouched := TestFacts{Outcome: "unexpected", Attempts: 1, AssertionFailed: true}
	retried := TestFacts{Outcome: "flaky", Attempts: 2, AssertionFailed: true, Applied: 2}
	notJSON := TestFacts{Outcome: "expected", Attempts: 1, NotApplicable: map[string]int{ReasonNotJSON: 1}}
	down := TestFacts{Outcome: "expected", Attempts: 1, NotApplicable: map[string]int{ReasonUnreachable: 1}}
	valid := func(tests ...TestFacts) RunFacts {
		facts := RunFacts{Validated: true, Tests: map[string]TestFacts{}}
		for index, test := range tests {
			facts.Tests[string(rune('a'+index))] = test
		}
		return facts
	}
	cases := []struct {
		name  string
		facts RunFacts
		want  string
		tests []string
	}{
		{"assertion failed under the fault", valid(caught), Caught, []string{Caught}},
		{"test passed under the fault", valid(passed), Survived, []string{Survived}},
		{"failed only on an action", valid(action), FailedWithoutAssertion, []string{FailedWithoutAssertion}},
		{"fault never matched", valid(TestFacts{Outcome: "expected", Attempts: 1}), NotExercised, []string{NotExercised}},
		{"failure without the fault is not credited", valid(untouched), NotExercised, []string{NotExercised}},
		{"response was not JSON", valid(notJSON), NotApplicable, []string{NotApplicable}},
		{"upstream unreachable", valid(down), NotApplicable, []string{NotApplicable}},
		{"a retried test hides what its first attempt saw", valid(retried), Retried, []string{Retried}},
		// Multi-test specs: the unrelated failure in b does not hide that a survived.
		{"one test survives while an unexercised one fails", valid(passed, untouched), Survived, []string{Survived, NotExercised}},
		{"one test survives while another catches", valid(caught, passed), Survived, []string{Caught, Survived}},
		{"every exercised test catches", valid(caught, action, untouched), Caught, []string{Caught, FailedWithoutAssertion, NotExercised}},
		{"a retried test outranks a caught one", valid(caught, retried), Retried, []string{Caught, Retried}},
		{"invalid evidence", RunFacts{Tests: map[string]TestFacts{"a": caught}}, Incomplete, nil},
		{"overflowed channel", RunFacts{Validated: true, Overflow: true, Tests: map[string]TestFacts{"a": passed}}, Incomplete, nil},
	}
	for _, test := range cases {
		got, tests := Classify(test.facts)
		results := make([]string, len(tests))
		for index, result := range tests {
			results[index] = result.Result
		}
		if got != test.want || strings.Join(results, ",") != strings.Join(test.tests, ",") {
			t.Errorf("%s: got %s %v, want %s %v", test.name, got, results, test.want, test.tests)
		}
	}
	if _, tests := Classify(valid(down)); tests[0].Reason != ReasonUnreachable {
		t.Fatalf("reason %q", tests[0].Reason)
	}
	summary := Summarize([]FaultReport{{Result: Caught}, {Result: Survived}, {Result: NotRun}, {Result: NotExercised}, {Result: Incomplete}, {Result: Retried}})
	if summary != (Summary{Faults: 6, Caught: 1, Survived: 1, Inconclusive: 3, NotRun: 1, Exercised: 3}) {
		t.Fatalf("summary %#v", summary)
	}
}

func TestCompletenessNeedsEveryFaultRunAndOneExercised(t *testing.T) {
	cases := []struct {
		name   string
		report Report
		want   string
	}{
		{"complete", Report{Summary: Summary{Faults: 2, Exercised: 2}}, ""},
		{"interrupted", Report{Interrupted: true, Summary: Summary{Faults: 2, Exercised: 1}}, IncompleteInterrupted},
		{"invalid evidence", Report{InvalidEvidence: true, Summary: Summary{Faults: 2, Exercised: 2}}, IncompleteInvalidEvidence},
		{"beyond the budget", Report{Summary: Summary{Faults: 3, Exercised: 2, NotRun: 1}}, IncompleteFaultsNotRun},
		{"nothing exercised", Report{Summary: Summary{Faults: 3}}, IncompleteNothingExercised},
	}
	for _, test := range cases {
		complete, reason := Completeness(test.report)
		if complete != (test.want == "") || reason != test.want {
			t.Errorf("%s: complete=%v reason=%q", test.name, complete, reason)
		}
	}
}

func TestReadChannelPairsResponsesWithinEachAttempt(t *testing.T) {
	response := `{"type":"response","method":"GET","origin":"` + origin + `","path":"/api/cart","status":200,"json":true}` + "\n"
	// The second attempt's response has no request of its own; an earlier
	// attempt's request for the same target must not vouch for it.
	dir := writeChannel(t, hello("observe")+observed("GET", "/api/cart", 500, false), helloFor("observe", testA, 1)+response)
	if _, err := ReadChannel(dir, "observe", ""); err == nil {
		t.Fatal("accepted a response-only stream because another attempt recorded the request")
	}
}
