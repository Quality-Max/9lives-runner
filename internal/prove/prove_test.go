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

func hello(mode string) string {
	return `{"type":"hello","protocol":"9l.prove/1","mode":"` + mode + `"}` + "\n"
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
	digits := ""
	for ; value > 0; value /= 10 {
		digits = string(rune('0'+value%10)) + digits
	}
	return digits
}

func TestReadChannelAggregatesRequestsAcrossAttempts(t *testing.T) {
	dir := writeChannel(t,
		hello("observe")+observed("GET", "/api/cart", 200, true)+observed("POST", "/api/orders", 201, true),
		hello("observe")+observed("GET", "/api/cart", 200, true)+observed("GET", "/api/text", 200, false),
	)
	observations, err := ReadChannel(dir, "observe")
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
		"wrong protocol":        `{"type":"hello","protocol":"9l.prove/2","mode":"observe"}` + "\n",
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
			if _, err := ReadChannel(writeChannel(t, content), "observe"); err == nil {
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
	if _, err := ReadChannel(dir, "observe"); err == nil {
		t.Fatal("accepted an unexpected file in the evidence directory")
	}
}

func TestReadChannelCountsFaultApplications(t *testing.T) {
	dir := writeChannel(t, hello("fault")+`{"type":"applied","fault":"fault-3"}`+"\n"+`{"type":"applied","fault":"fault-3"}`+"\n"+`{"type":"not-applicable","fault":"fault-3"}`+"\n")
	observations, err := ReadChannel(dir, "fault")
	if err != nil {
		t.Fatal(err)
	}
	if observations.Applied["fault-3"] != 2 || observations.NotApplicable["fault-3"] != 1 {
		t.Fatalf("unexpected fault counts: %#v", observations)
	}
	if _, err := ReadChannel(writeChannel(t, hello("fault")+observed("GET", "/a", 200, true)), "fault"); err == nil {
		t.Fatal("accepted an observation during a fault run")
	}
}

func TestPlanOrdersRequestsAndKeepsFaultsBeyondTheBudget(t *testing.T) {
	observations, err := ReadChannel(writeChannel(t, hello("observe")+observed("POST", "/api/orders", 200, true)+observed("GET", "/api/text", 200, false)+observed("GET", "/api/cart", 200, true)), "observe")
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

func TestClassify(t *testing.T) {
	cases := []struct {
		name  string
		facts RunFacts
		want  string
	}{
		{"assertion failed under the fault", RunFacts{Validated: true, Applied: 1, Unexpected: 1, UnexpectedWithAssertion: 1}, Caught},
		{"tests passed under the fault", RunFacts{Validated: true, Passed: true, Applied: 2}, Survived},
		{"failed only on an action", RunFacts{Validated: true, Applied: 1, Unexpected: 1}, FailedWithoutAssertion},
		{"fault never matched", RunFacts{Validated: true, Passed: true}, NotExercised},
		{"failure without the fault is not credited", RunFacts{Validated: true, Unexpected: 1, UnexpectedWithAssertion: 1}, NotExercised},
		{"response was not JSON", RunFacts{Validated: true, Passed: true, NotApplicable: 1}, NotApplicable},
		{"invalid evidence", RunFacts{Applied: 1, Unexpected: 1, UnexpectedWithAssertion: 1}, Incomplete},
		{"overflowed channel", RunFacts{Validated: true, Overflow: true, Applied: 1, Passed: true}, Incomplete},
	}
	for _, test := range cases {
		if got := Classify(test.facts); got != test.want {
			t.Errorf("%s: got %s, want %s", test.name, got, test.want)
		}
	}
	summary := Summarize([]FaultReport{{Result: Caught}, {Result: Survived}, {Result: NotRun}, {Result: NotExercised}, {Result: Incomplete}})
	if summary != (Summary{Faults: 5, Caught: 1, Survived: 1, Inconclusive: 2, NotRun: 1}) {
		t.Fatalf("summary %#v", summary)
	}
}

func TestReadChannelPairsResponsesWithinEachAttempt(t *testing.T) {
	response := `{"type":"response","method":"GET","origin":"` + origin + `","path":"/api/cart","status":200,"json":true}` + "\n"
	// The second attempt's response has no request of its own; an earlier
	// attempt's request for the same target must not vouch for it.
	dir := writeChannel(t, hello("observe")+observed("GET", "/api/cart", 500, false), hello("observe")+response)
	if _, err := ReadChannel(dir, "observe"); err == nil {
		t.Fatal("accepted a response-only stream because another attempt recorded the request")
	}
}
