package main

import (
	"bytes"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"strings"
	"testing"

	"github.com/Quality-Max/9lives-runner/internal/jev"
)

func TestJevDoctorPreflightIsLocalAndSanitized(t *testing.T) {
	var generated [16]byte
	if _, err := rand.Read(generated[:]); err != nil {
		t.Fatal("fixture credential generation failed")
	}
	credential := hex.EncodeToString(generated[:])
	for _, tc := range []struct {
		name, key, status string
		args              []string
		code              int
		present           bool
	}{
		{"disabled", credential, "disabled", []string{"--jev=false"}, 0, false},
		{"missing", "", "missing_credential", []string{"--jev"}, 2, false},
		{"configured", credential, "configured", []string{"--jev", "--jev-model", "jev-1.14.0", "--jev-images", "masked"}, 0, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv(jev.CredentialName, tc.key)
			t.Setenv("PATH", t.TempDir()) // no Node or external helper may be required
			var out, stderr bytes.Buffer
			args := append([]string{"doctor", "--format", "json"}, tc.args...)
			code := run(args, &out, &stderr)
			var report jev.Preflight
			if code != tc.code || json.Unmarshal(out.Bytes(), &report) != nil || report.Status != tc.status || report.CredentialPresent != tc.present || report.Connectivity != "not_checked" || stderr.Len() != 0 {
				t.Fatal("incorrect local preflight report")
			}
			if strings.Contains(out.String(), credential) || strings.Contains(stderr.String(), credential) {
				t.Fatal("preflight exposed credential")
			}
		})
	}
}

func TestJevDoctorRejectsUnsafeOptions(t *testing.T) {
	for _, args := range [][]string{
		{"--jev-model", "jev-1.14.0"},
		{"--jev", "--sdk"},
		{"--jev", "--jev-images", "all"},
		{"--jev", "--jev-model", "https://private.example/"},
	} {
		var out, stderr bytes.Buffer
		if run(append([]string{"doctor"}, args...), &out, &stderr) != 2 || out.Len() != 0 || strings.Contains(stderr.String(), "private.example") {
			t.Fatal("invalid option accepted or echoed")
		}
	}
}
