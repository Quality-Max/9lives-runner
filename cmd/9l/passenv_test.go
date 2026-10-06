package main

import "testing"

func TestPassEnvAcceptsNamesOnly(t *testing.T) {
	var names envNames
	if err := names.Set("BASE_URL, TEST_USER"); err != nil || len(names) != 2 || names[1] != "TEST_USER" {
		t.Fatalf("names=%v err=%v", names, err)
	}
	for _, invalid := range []string{"TOKEN=secret", "", "A B"} {
		if err := (&envNames{}).Set(invalid); err == nil {
			t.Fatalf("%q must be rejected so values never reach argv", invalid)
		}
	}
}
