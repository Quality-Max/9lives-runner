package repro

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestGenerateWritesTheFindingTemplateBelowTheDefaultDirectory(t *testing.T) {
	root := t.TempDir()
	result, err := Generate(root, Options{URL: "https://example.com/", Finding: map[string]any{"category": "console", "message": "error: </script> \"quoted\"\u2028next"}})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(result.FilePath, DefaultDir+"/repro-") || !strings.HasSuffix(result.FilePath, ".spec.ts") {
		t.Fatalf("path %q", result.FilePath)
	}
	written, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(result.FilePath)))
	if err != nil || string(written) != result.Code {
		t.Fatalf("written file differs from returned code: %v", err)
	}
	for _, want := range []string{
		"import { test, expect } from '@playwright/test';",
		// The title collapses whitespace, U+2028 included; the message keeps it, escaped.
		`test("error: </script> \"quoted\" next", async ({ page }) => {`,
		`expect(errors, "error: </script> \"quoted\"\u2028next").toEqual([]);`,
	} {
		if !strings.Contains(result.Code, want) {
			t.Fatalf("code lacks %q:\n%s", want, result.Code)
		}
	}
}

func TestGenerateTemplates(t *testing.T) {
	root := t.TempDir()
	for _, tc := range []struct {
		name    string
		options Options
		want    []string
	}{
		{"links use the request fixture", Options{Finding: map[string]any{"category": "links", "url": "https://example.com/gone"}},
			[]string{"async ({ page, request }) =>", `await request.get("https://example.com/gone")`}},
		{"accessibility asserts the selector", Options{Finding: map[string]any{"category": "accessibility", "selector": "img.hero", "message": "Missing alt */ text"}},
			[]string{`page.locator("img.hero").first()`, "// Finding: Missing alt * / text"}},
		{"goal falls back to the title check", Options{Goal: "line one\nline two */"},
			[]string{"await expect(page).toHaveTitle(/.+/);", "// Goal: line one line two * /", `test("line one line two */"`}},
		{"sdk imports 9lives", Options{SDK: true}, []string{"import { test, expect } from '@9l/playwright';", `test("9lives repro"`}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			tc.options.URL = "https://example.com/"
			result, err := Generate(root, tc.options)
			if err != nil {
				t.Fatal(err)
			}
			for _, want := range tc.want {
				if !strings.Contains(result.Code, want) {
					t.Fatalf("code lacks %q:\n%s", want, result.Code)
				}
			}
		})
	}
}

func TestGenerateRefusesUnsafeTargets(t *testing.T) {
	root := t.TempDir()
	outside := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "existing.spec.ts"), []byte("keep"), 0o644); err != nil {
		t.Fatal(err)
	}
	symlinks := os.Symlink(outside, filepath.Join(root, "linked")) == nil &&
		os.Symlink(filepath.Join(outside, "target.spec.ts"), filepath.Join(root, "dangling.spec.ts")) == nil
	cases := []struct{ url, output, want string }{
		{"file:///etc/passwd", "", "http or https"},
		{"https://user:pass@example.com/", "", "credentials"},
		{"https://example.com/", "../escape.spec.ts", "escapes the project root"},
		{"https://example.com/", filepath.Join(outside, "abs.spec.ts"), "relative to the project root"},
		{"https://example.com/", "src/app.ts", ".spec or .test file"},
		{"https://example.com/", "existing.spec.ts", "refusing to overwrite"},
	}
	if symlinks {
		cases = append(cases,
			struct{ url, output, want string }{"https://example.com/", "linked/x.spec.ts", "resolves outside the project root"},
			struct{ url, output, want string }{"https://example.com/", "dangling.spec.ts", "symbolic link"})
	}
	for _, tc := range cases {
		_, err := Generate(root, Options{URL: tc.url, Output: tc.output})
		var refused Error
		if !errors.As(err, &refused) || !strings.Contains(refused.Message, tc.want) {
			t.Fatalf("%s %s: %v", tc.url, tc.output, err)
		}
	}
	if entries, _ := os.ReadDir(outside); len(entries) != 0 {
		t.Fatalf("wrote outside the root: %v", entries)
	}
	if data, _ := os.ReadFile(filepath.Join(root, "existing.spec.ts")); string(data) != "keep" {
		t.Fatal("existing file was replaced without overwrite")
	}
	result, err := Generate(root, Options{URL: "https://example.com/", Output: "existing.spec.ts", Overwrite: true})
	if err != nil || result.FilePath != "existing.spec.ts" {
		t.Fatalf("overwrite: %v %v", result, err)
	}
	if data, _ := os.ReadFile(filepath.Join(root, "existing.spec.ts")); string(data) != result.Code {
		t.Fatal("overwrite did not replace the file")
	}
}
