// Package repro writes a minimal Playwright reproduction for a scan finding,
// URL or goal from deterministic templates. Generation makes no network
// request and runs nothing; the caller decides whether to run the result.
package repro

import (
	"bytes"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

// DefaultDir is where a reproduction is written when no path is given.
const DefaultDir = ".9lives/repros"

// Options describe one reproduction.
type Options struct {
	URL     string
	Goal    string
	Finding map[string]any
	Name    string
	// Output is relative to Root; empty means a new file under DefaultDir.
	Output    string
	Overwrite bool
	// SDK imports test and expect from @9l/playwright, as confirm_finding requires.
	SDK bool
}

// Result names the written file, relative to Root with forward slashes.
type Result struct {
	FilePath    string   `json:"filePath"`
	Code        string   `json:"code"`
	Assumptions []string `json:"assumptions"`
}

// Error is a refused request; its message is safe to return to the caller.
type Error struct{ Message string }

func (e Error) Error() string { return e.Message }

var specName = regexp.MustCompile(`\.(spec|test)\.(ts|js|mjs|cjs|mts|cts)$`)

// Generate writes the reproduction below root. The path must stay inside
// root after symbolic links, name a spec or test file, and replace an
// existing file only with Overwrite.
func Generate(root string, options Options) (Result, error) {
	parsed, err := url.Parse(options.URL)
	if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Host == "" {
		return Result{}, Error{"url must be an absolute http or https URL"}
	}
	if parsed.User != nil {
		return Result{}, Error{"URLs containing credentials are not permitted"}
	}
	title := sanitizeName(firstNonEmpty(options.Name, options.Goal, findingString(options.Finding, "message"), "9lives repro"))
	code := build(options.URL, title, options.Goal, options.Finding, options.SDK)
	output := options.Output
	if output == "" {
		output = DefaultDir + "/repro-" + time.Now().UTC().Format("20060102-150405") + "-" + randomSuffix() + ".spec.ts"
	}
	written, err := write(root, output, code, options.Overwrite)
	if err != nil {
		return Result{}, err
	}
	assumptions := []string{
		"Generated locally from deterministic templates; nothing was run.",
		"Review selectors and assertions before committing; use inspect_page for a stronger role or test-id locator.",
	}
	if strings.HasPrefix(written, DefaultDir+"/") {
		assumptions = append(assumptions, "Written below "+DefaultDir+"; move it inside the Playwright testDir, or pass output_path there, before run_test.")
	}
	if !options.SDK {
		assumptions = append(assumptions, "Imports @playwright/test; generate with sdk:true for a reproduction confirm_finding can run.")
	}
	return Result{FilePath: written, Code: code, Assumptions: assumptions}, nil
}

func write(root, requested, content string, overwrite bool) (string, error) {
	if filepath.IsAbs(requested) || filepath.VolumeName(requested) != "" {
		return "", Error{"output_path must be relative to the project root"}
	}
	if !specName.MatchString(requested) {
		return "", Error{"output_path must name a .spec or .test file, such as tests/repro.spec.ts"}
	}
	root, err := filepath.EvalSymlinks(root)
	if err != nil {
		return "", Error{"project root cannot be resolved"}
	}
	candidate := filepath.Join(root, filepath.FromSlash(requested))
	if !within(root, candidate) {
		return "", Error{"output_path escapes the project root"}
	}
	if err := os.MkdirAll(filepath.Dir(candidate), 0o755); err != nil {
		return "", Error{"output directory cannot be created"}
	}
	// Create the file through its resolved parent, so a symbolic link in the
	// path cannot redirect the write after the check.
	parent, err := filepath.EvalSymlinks(filepath.Dir(candidate))
	if err != nil || !within(root, filepath.Join(parent, "x")) {
		return "", Error{"output_path resolves outside the project root"}
	}
	target := filepath.Join(parent, filepath.Base(candidate))
	info, err := os.Lstat(target)
	switch {
	case err == nil && info.Mode()&fs.ModeSymlink != 0:
		return "", Error{"output_path must not be a symbolic link"}
	case err == nil && !info.Mode().IsRegular():
		return "", Error{"output_path must name a regular file"}
	case err == nil && !overwrite:
		return "", Error{"refusing to overwrite an existing file; set overwrite to true after reviewing it"}
	case err != nil && !errors.Is(err, fs.ErrNotExist):
		return "", Error{"output_path cannot be inspected"}
	}
	if err == nil {
		if err := replace(target, content); err != nil {
			return "", Error{"reproduction could not be written"}
		}
	} else {
		file, err := os.OpenFile(target, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
		if err != nil {
			return "", Error{"reproduction could not be written"}
		}
		_, writeErr := file.WriteString(content)
		if closeErr := file.Close(); writeErr != nil || closeErr != nil {
			os.Remove(target)
			return "", Error{"reproduction could not be written"}
		}
	}
	relative, err := filepath.Rel(root, target)
	if err != nil {
		return "", Error{"reproduction path cannot be expressed relative to the project root"}
	}
	return filepath.ToSlash(relative), nil
}

func replace(target, content string) error {
	temporary, err := os.CreateTemp(filepath.Dir(target), "."+filepath.Base(target)+".*.tmp")
	if err != nil {
		return err
	}
	_, writeErr := temporary.WriteString(content)
	closeErr := temporary.Close()
	if writeErr == nil && closeErr == nil {
		if err := os.Rename(temporary.Name(), target); err == nil {
			return nil
		}
	}
	os.Remove(temporary.Name())
	return errors.New("replace failed")
}

func within(root, candidate string) bool {
	relative, err := filepath.Rel(root, candidate)
	return err == nil && relative != "." && relative != ".." && !strings.HasPrefix(relative, ".."+string(filepath.Separator)) && !filepath.IsAbs(relative)
}

func build(target, title, goal string, finding map[string]any, sdk bool) string {
	module := "@playwright/test"
	if sdk {
		module = "@9l/playwright"
	}
	header := "import { test, expect } from '" + module + "';\n\n"
	category, message := findingString(finding, "category"), findingString(finding, "message")
	switch category {
	case "console", "network":
		return header + fmt.Sprintf(`test(%s, async ({ page }) => {
  const errors: string[] = [];
  page.on('console', (msg) => {
    if (msg.type() === 'error') errors.push(msg.text());
  });
  page.on('pageerror', (err) => errors.push(err.message));

  await page.goto(%s, { waitUntil: 'domcontentloaded' });
  await page.waitForLoadState('networkidle').catch(() => {});

  expect(errors, %s).toEqual([]);
});
`, quote(title), quote(target), quote(firstNonEmpty(message, "No console/page errors expected")))
	case "links":
		return header + fmt.Sprintf(`test(%s, async ({ page, request }) => {
  await page.goto(%s, { waitUntil: 'domcontentloaded' });
  const response = await request.get(%s);
  expect(response.status()).toBeLessThan(400);
});
`, quote(title), quote(target), quote(firstNonEmpty(findingString(finding, "url"), target)))
	case "accessibility":
		return header + fmt.Sprintf(`test(%s, async ({ page }) => {
  await page.goto(%s, { waitUntil: 'domcontentloaded' });
  const target = page.locator(%s).first();
  await expect(target).toBeVisible();
  // Finding: %s
});
`, quote(title), quote(target), quote(firstNonEmpty(findingString(finding, "selector"), "body")), comment(message))
	}
	return header + fmt.Sprintf(`test(%s, async ({ page }) => {
  await page.goto(%s, { waitUntil: 'domcontentloaded' });
  await expect(page).toHaveTitle(/.+/);
  // Goal: %s
});
`, quote(title), quote(target), comment(firstNonEmpty(goal, message, "Add assertions for the target workflow.")))
}

// quote returns a JavaScript string literal; JSON string syntax is a subset.
// encoding/json always escapes U+2028 and U+2029.
func quote(value string) string {
	var buffer bytes.Buffer
	encoder := json.NewEncoder(&buffer)
	encoder.SetEscapeHTML(false)
	_ = encoder.Encode(value)
	return strings.TrimSuffix(buffer.String(), "\n")
}

func comment(value string) string {
	value = strings.NewReplacer("*/", "* /", "\r", " ", "\n", " ", " ", " ", " ", " ").Replace(value)
	return truncate(value, 300)
}

func sanitizeName(value string) string {
	value = truncate(strings.Join(strings.Fields(value), " "), 120)
	if value == "" {
		return "9lives repro"
	}
	return value
}

func truncate(value string, limit int) string {
	runes := []rune(value)
	if len(runes) > limit {
		return string(runes[:limit])
	}
	return value
}

func findingString(finding map[string]any, key string) string {
	value, _ := finding[key].(string)
	return value
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}

func randomSuffix() string {
	var bytes [4]byte
	_, _ = rand.Read(bytes[:])
	return hex.EncodeToString(bytes[:])
}
