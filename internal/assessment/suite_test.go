package assessment

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeTree(t *testing.T, files map[string]string) string {
	t.Helper()
	root := t.TempDir()
	for name, content := range files {
		file := filepath.Join(root, name)
		if err := os.MkdirAll(filepath.Dir(file), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(file, []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	return root
}

func TestDiscoverSpecsFromDirectoriesFilesAndPatterns(t *testing.T) {
	root := writeTree(t, map[string]string{
		"tests/auth/login.spec.ts":      "x",
		"tests/cart.test.mjs":           "x",
		"tests/checkout.spec.tsx":       "x",
		"tests/helpers.ts":              "x",
		"tests/node_modules/a.spec.ts":  "x",
		"tests/.cache/b.spec.ts":        "x",
		"tests/fixtures/data.json":      "x",
		"other/explicit-helper.ts":      "x",
		"other/deep/nested/c.spec.js":   "x",
		"other/deep/nested/c.helper.js": "x",
	})
	if err := os.Symlink(filepath.Join(root, "tests/cart.test.mjs"), filepath.Join(root, "tests/link.spec.ts")); err != nil {
		t.Fatal(err)
	}
	t.Chdir(root)
	for _, test := range []struct {
		inputs []string
		want   string
	}{
		// Directories take Playwright-named specs only, skipping dependencies,
		// hidden directories and symbolic links.
		{[]string{"tests"}, "tests/auth/login.spec.ts tests/cart.test.mjs tests/checkout.spec.tsx"},
		// An explicit file is taken as given; repeated inputs are de-duplicated.
		{[]string{"other/explicit-helper.ts", "tests/auth", "tests/auth/login.spec.ts"}, "other/explicit-helper.ts tests/auth/login.spec.ts"},
		// ** matches zero or more directories; a pattern takes any matching file.
		{[]string{"**/*.spec.ts"}, "tests/auth/login.spec.ts"},
		{[]string{"other/**/c.*.js"}, "other/deep/nested/c.helper.js other/deep/nested/c.spec.js"},
		{[]string{"tests/*.spec.tsx"}, "tests/checkout.spec.tsx"},
	} {
		files, err := DiscoverSpecs(test.inputs)
		if err != nil {
			t.Fatalf("%v: %v", test.inputs, err)
		}
		if got := filepath.ToSlash(strings.Join(files, " ")); got != test.want {
			t.Fatalf("%v discovered %q, want %q", test.inputs, got, test.want)
		}
	}
	for _, inputs := range [][]string{{"missing"}, {"other/fixtures"}, {"tests/*.cy.ts"}, {"tests/[.ts"}} {
		if _, err := DiscoverSpecs(inputs); err == nil {
			t.Fatalf("%v accepted", inputs)
		}
	}
}

func TestDiscoverSpecsBoundsTheFileCount(t *testing.T) {
	files := map[string]string{}
	for i := 0; i <= MaxSuiteFiles; i++ {
		files[fmt.Sprintf("tests/d%d/t%d.spec.ts", i%16, i)] = ""
	}
	root := writeTree(t, files)
	if _, err := DiscoverSpecs([]string{filepath.Join(root, "tests")}); err == nil || !strings.Contains(err.Error(), "more than 2048") {
		t.Fatalf("unbounded discovery: %v", err)
	}
}

func TestAssessSuiteRecordsFileFailuresAndSummarizes(t *testing.T) {
	requireNode(t)
	spec := func(body string) []byte {
		return []byte("import {test,expect} from '@playwright/test';\n" + body)
	}
	inputs := []SuiteInput{
		{Path: "a.spec.ts", Source: spec("test.skip(isWeekend(), 'r');\ntest('a', async ({page}) => { await page.waitForTimeout(1); });\ntest('b', () => { expect(1).toBe(1); });\n")},
		{Path: "broken.spec.ts", Source: spec("test('x', () => {\n")},
		{Path: "empty.spec.ts", Source: []byte{}},
		{Path: "unreadable.spec.ts", Error: "source-limit"},
		{Path: "c.spec.ts", Source: spec("test('c', async ({page}) => { await page.waitForTimeout(1); });\n")},
	}
	suite, err := AssessSuite(context.Background(), inputs, []byte(contractJSON), Options{})
	if err != nil {
		t.Fatal(err)
	}
	var outcomes []string
	for _, file := range suite.Files {
		if file.Report != nil {
			outcomes = append(outcomes, file.Path+"=ok")
		} else {
			outcomes = append(outcomes, file.Path+"="+file.Error)
		}
	}
	if strings.Join(outcomes, " ") != "a.spec.ts=ok broken.spec.ts=syntax empty.spec.ts=source-limit unreadable.spec.ts=source-limit c.spec.ts=ok" {
		t.Fatalf("per-file outcomes %v", outcomes)
	}
	s := suite.Summary
	// The shared conditional skip counts once; each wait has no assertion after it.
	if s.Files != 5 || s.Assessed != 2 || s.Failed != 3 || s.Tests != 3 || s.Findings != 5 ||
		s.Rules["fixed-wait"] != 2 || s.Rules["conditional-skip"] != 1 || s.Rules["no-direct-assertion"] != 2 || s.Rules["analysis-limit/unresolved-helper"] != 0 {
		t.Fatalf("summary %+v", s)
	}
	if suite.Version != SuiteVersion || suite.Policy != Policy || suite.Completeness != "partial" || suite.Execution != "not_run" || suite.RequirementsSHA256 != digest([]byte(contractJSON)) {
		t.Fatalf("suite identity %+v", suite)
	}
	if !strings.Contains(strings.Join(suite.Limits, "\n"), "Some files could not be assessed") {
		t.Fatal("failed files not listed as a limit")
	}
}

func TestAssessSuiteStopsAtCancellation(t *testing.T) {
	requireNode(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	suite, err := AssessSuite(ctx, []SuiteInput{{Path: "a.spec.ts", Source: []byte("const a = 1;")}, {Path: "b.spec.ts", Source: []byte("const b = 1;")}}, nil, Options{})
	if err != nil {
		t.Fatal(err)
	}
	for _, file := range suite.Files {
		if file.Report != nil || file.Error != "cancelled" {
			t.Fatalf("cancelled suite produced %+v", file)
		}
	}
	if suite.Summary.Failed != 2 || suite.RequirementsSHA256 != "" {
		t.Fatalf("summary %+v", suite.Summary)
	}
}
