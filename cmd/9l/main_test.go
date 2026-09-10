package main

import (
	"bytes"
	"reflect"
	"testing"
)

func TestFlagsFirstAllowsFlagsAfterSpecs(t *testing.T) {
	got := flagsFirst([]string{"a.spec.ts", "--workers", "3", "b.spec.ts", "--format=json"}, map[string]bool{"--workers": true, "--format": true})
	want := []string{"--workers", "3", "--format=json", "a.spec.ts", "b.spec.ts"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %#v want %#v", got, want)
	}
}

func TestRunRejectsMissingSpecs(t *testing.T) {
	var stdout, stderr bytes.Buffer
	if code := run([]string{"plan"}, &stdout, &stderr); code != 2 {
		t.Fatalf("code=%d stderr=%s", code, stderr.String())
	}
}
