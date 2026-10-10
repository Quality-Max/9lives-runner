package confirm

import (
	"strings"
	"testing"
)

func TestClassifyComparesEachTestAcrossRevisions(t *testing.T) {
	pass := TestFacts{Outcome: "expected", Attempts: 1}
	assertion := TestFacts{Outcome: "unexpected", Attempts: 1, AssertionFailed: true}
	timeout := TestFacts{Outcome: "unexpected", Attempts: 1}
	retried := TestFacts{Outcome: "flaky", Attempts: 2, AssertionFailed: true}
	skipped := TestFacts{Outcome: "skipped", Attempts: 1}
	run := func(tests ...TestFacts) RunFacts {
		facts := RunFacts{Validated: true, Tests: map[string]TestFacts{}}
		for index, test := range tests {
			facts.Tests[string(rune('a'+index))] = test
		}
		return facts
	}
	cases := []struct {
		name           string
		unfixed, fixed RunFacts
		verdict        string
		reason         string
		tests          string
	}{
		{"fails on unfixed, passes on fixed", run(assertion), run(pass), Confirmed, "", "a=assertion-failed/passed:confirmed"},
		{"passes on both", run(pass), run(pass), NotReproduced, "", "a=passed/passed:not-reproduced"},
		{"fails on both", run(assertion), run(assertion), FixIneffective, "", "a=assertion-failed/assertion-failed:fix-ineffective"},
		{"passes on unfixed, fails on fixed", run(pass), run(assertion), Regressed, "", "a=passed/assertion-failed:regressed"},
		{"a passing sanity test does not hide a confirmation", run(assertion, pass), run(pass, pass), Confirmed, "", "a=assertion-failed/passed:confirmed b=passed/passed:not-reproduced"},
		{"a test still failing on fixed outranks a confirmation", run(assertion, assertion), run(pass, assertion), FixIneffective, "", "a=assertion-failed/passed:confirmed b=assertion-failed/assertion-failed:fix-ineffective"},
		{"a regression outranks an ineffective fix", run(assertion, pass), run(assertion, assertion), Regressed, "", "a=assertion-failed/assertion-failed:fix-ineffective b=passed/assertion-failed:regressed"},
		{"a failure without an assertion is not the defect", run(timeout), run(pass), Inconclusive, ReasonFailedWithoutAssertion, "a=failed-without-assertion/passed:inconclusive"},
		{"a retried test hides its first attempt", run(assertion), run(retried), Inconclusive, ReasonRetried, "a=assertion-failed/retried:inconclusive"},
		{"a skipped test checks nothing", run(skipped), run(pass), Inconclusive, ReasonSkipped, "a=skipped/passed:inconclusive"},
		{"an inconclusive test outranks a confirmation", run(assertion, timeout), run(pass, pass), Inconclusive, ReasonFailedWithoutAssertion, "a=assertion-failed/passed:confirmed b=failed-without-assertion/passed:inconclusive"},
		{"tests reported on only one revision", run(assertion), RunFacts{Validated: true, Tests: map[string]TestFacts{"z": pass}}, Inconclusive, ReasonTestsDiffer, "a=assertion-failed/missing:inconclusive z=missing/passed:inconclusive"},
		{"an invalid unfixed run", RunFacts{Tests: map[string]TestFacts{"a": assertion}}, run(pass), Inconclusive, ReasonRunIncomplete, ""},
		{"an invalid fixed run", run(assertion), RunFacts{Tests: map[string]TestFacts{"a": pass}}, Inconclusive, ReasonRunIncomplete, ""},
		{"no tests", run(), run(), Inconclusive, ReasonNoTests, ""},
	}
	for _, test := range cases {
		verdict, reason, tests := Classify(test.unfixed, test.fixed)
		got := []string{}
		for _, result := range tests {
			got = append(got, result.TestID+"="+result.Unfixed+"/"+result.Fixed+":"+result.Result)
		}
		if verdict != test.verdict || reason != test.reason || strings.Join(got, " ") != test.tests {
			t.Errorf("%s: got %s %q %q, want %s %q %q", test.name, verdict, reason, strings.Join(got, " "), test.verdict, test.reason, test.tests)
		}
	}
	if Conclusive(Inconclusive) || !Conclusive(NotReproduced) {
		t.Fatal("only inconclusive is not conclusive")
	}
}
