package main

import (
	"bytes"
	"strings"
	"testing"
)

func TestTier1JSONCommandIsBoundedAndStrict(t *testing.T) {
	valid := `{"version":1,"framework":"playwright","errorMessage":"waiting for locator('#old')","testCode":"page.locator('#old').click()","pageSnapshot":"<button id='old-new'>x</button>"}`
	var out, errOut bytes.Buffer
	if code := tier1Command([]string{"--format", "json"}, strings.NewReader(valid), &out, &errOut); code != 0 || !strings.Contains(out.String(), `"decision":"propose"`) {
		t.Fatalf("code=%d stdout=%s stderr=%s", code, out.String(), errOut.String())
	}
	for _, input := range []string{valid + valid, `{"version":1,"version":1,"framework":"playwright"}`, `{"version":1,"framework":"playwright","errorMessage":null}`, `{"version":1,"framework":"playwright","failureType":"banana"}`, `{"version":1,"framework":"playwright","unknown":true}`, `{"version":1,"framework":"bogus"}`, `{"version":2,"framework":"playwright"}`, `{"version":1,"framework":"playwright","errorMessage":"\ud800"}`} {
		out.Reset()
		errOut.Reset()
		if code := tier1Command([]string{"--format", "json"}, strings.NewReader(input), &out, &errOut); code == 0 || out.Len() != 0 || strings.Contains(errOut.String(), "old") {
			t.Fatalf("bad input leaked or succeeded: code=%d out=%q err=%q", code, out.String(), errOut.String())
		}
	}
	out.Reset()
	errOut.Reset()
	if code := tier1Command([]string{"--format", "json"}, strings.NewReader(strings.Repeat("x", (4<<20)+1)), &out, &errOut); code == 0 || out.Len() != 0 || strings.Contains(errOut.String(), "xxxx") {
		t.Fatalf("oversized input leaked or succeeded: code=%d out=%q err=%q", code, out.String(), errOut.String())
	}
	malformed := append([]byte(`{"version":1,"framework":"playwright","errorMessage":"`), 0xff, '"', '}')
	out.Reset()
	errOut.Reset()
	if code := tier1Command([]string{"--format", "json"}, bytes.NewReader(malformed), &out, &errOut); code == 0 || out.Len() != 0 {
		t.Fatalf("malformed UTF-8 succeeded: code=%d out=%q err=%q", code, out.String(), errOut.String())
	}
}

func TestTier1CLIAssertionEvidenceWinsCallerClassification(t *testing.T) {
	input := `{"version":1,"framework":"playwright","failureType":"locator_not_found","failedSelector":"#old","errorMessage":"AssertionError: waiting for locator('#old')","testCode":"await page.locator('#old').click();\nawait expect(page.locator('#other')).toBeVisible();","pageSnapshot":"<button id='old-new'>x</button>"}`
	var out, stderr bytes.Buffer
	if code := tier1Command([]string{"--format", "json"}, strings.NewReader(input), &out, &stderr); code != 0 || !strings.Contains(out.String(), `"decision":"refuse"`) || !strings.Contains(out.String(), `"failureType":"assertion_failed"`) || strings.Contains(out.String(), "proposedCode") {
		t.Fatalf("assertion evidence overridden: code=%d out=%s err=%s", code, out.String(), stderr.String())
	}
}
