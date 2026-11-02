package runner

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"
)

// importRepo creates a committed repository at <tmp>/repo holding files, so
// that <tmp> itself is outside the repository.
func importRepo(t *testing.T, files map[string]string) string {
	t.Helper()
	repo := filepath.Join(t.TempDir(), "repo")
	if err := os.MkdirAll(repo, 0o700); err != nil {
		t.Fatal(err)
	}
	provenanceGit(t, repo, "init", "-b", "fixture-branch")
	provenanceGit(t, repo, "-c", "user.name=Fixture", "-c", "user.email=fixture@example.invalid", "-c", "commit.gpgsign=false", "commit", "--allow-empty", "-m", "Fixture")
	for name, contents := range files {
		mustWrite(t, filepath.Join(repo, filepath.FromSlash(name)), contents, 0o600)
	}
	return repo
}

func capture(t *testing.T, spec string) AgentProvenance {
	t.Helper()
	p, err := CaptureAgentProvenance(context.Background(), spec, "fixture-agent")
	if err != nil {
		t.Fatalf("capture: %v", err)
	}
	return p
}

func dependencyPaths(p AgentProvenance) []string {
	var paths []string
	for _, dep := range p.Dependencies {
		paths = append(paths, dep.Kind+":"+dep.Path)
	}
	return paths
}

const checkoutSpec = `#!/usr/bin/env node
import { test, expect } from '@playwright/test';
import { readFileSync } from 'node:fs';
import { checkout } from './helpers/checkout';
import type { Order } from "./helpers/types";
import './setup.js';
// import './ghost-comment';
/* require('./ghost-block') */
const text = "import('./ghost-string')";
const pattern = /['"]import\('.\/ghost-regex'\)/;
const later = () => import('./lazy.js');
const data = require('./data.json');
test('checkout', async () => { expect(await checkout()).toBe(true); });
`

func checkoutFiles() map[string]string {
	return map[string]string{
		"tests/checkout.spec.ts":    checkoutSpec,
		"tests/helpers/checkout.ts": "export { total } from '../../shared';\nexport const checkout = async () => true;\n",
		"tests/helpers/types.ts":    "export type Order = { id: string };\n",
		"tests/setup.ts":            "export * from './helpers/checkout.ts';\n",
		"tests/lazy.ts":             "export const lazy = `${import.meta.url}`;\n",
		"tests/data.json":           "{\"items\": 1}\n",
		"shared/index.mts":          "throw new Error('never executed');\n",
		"shared/index.ts":           "import { other } from '../tests/helpers/checkout';\nexport const total = 1;\n",
		"unrelated.ts":              "export const unrelated = 1;\n",
		"package.json":              "{}\n",
		"package-lock.json":         "{\"lockfileVersion\": 3}\n",
	}
}

// Editing an imported helper is a provenance mismatch naming the helper;
// editing an unrelated file is not.
func TestProvenanceImportGraphNamesChangedHelper(t *testing.T) {
	repo := importRepo(t, checkoutFiles())
	spec := filepath.Join(repo, "tests/checkout.spec.ts")
	creation := capture(t, spec)
	want := []string{"lockfile:package-lock.json", "import:shared/index.ts", "import:tests/data.json", "import:tests/helpers/checkout.ts", "import:tests/helpers/types.ts", "import:tests/lazy.ts", "import:tests/setup.ts"}
	if got := dependencyPaths(creation); !reflect.DeepEqual(got, want) || creation.Version != 2 || creation.DependenciesSHA256 != dependenciesDigest(creation.Dependencies) {
		t.Fatalf("dependencies = %v, want %v", got, want)
	}
	raw, _ := json.Marshal(creation)
	if strings.Contains(string(raw), "never executed") || strings.Contains(string(raw), "checkout = async") {
		t.Fatal("snapshot contains source text")
	}

	mustWrite(t, filepath.Join(repo, "unrelated.ts"), "export const unrelated = 2;\n", 0o600)
	mustWrite(t, filepath.Join(repo, "README.md"), "new file\n", 0o600)
	if r := CompareAgentProvenance(creation, capture(t, spec)); r.Status != "matched" || r.Scope != ProvenanceScopeImports || r.Dependencies != "matched" || r.DependencyChanges != nil {
		t.Fatalf("unrelated edit changed provenance: %+v", r)
	}

	for _, change := range []struct {
		file, contents string
		want           []ProvenanceChange
	}{
		{"tests/helpers/checkout.ts", "export { total } from '../../shared';\nexport const checkout = async () => false;\n",
			[]ProvenanceChange{{"tests/helpers/checkout.ts", DependencyImport, "modified"}}},
		{"shared/index.ts", "import { other } from '../tests/helpers/checkout';\nimport './extra.cjs';\nexport const total = 1;\n",
			[]ProvenanceChange{{"shared/extra.cjs", DependencyImport, "added"}, {"shared/index.ts", DependencyImport, "modified"}}},
		{"tests/data.json", "{\"items\": 2}\n", []ProvenanceChange{{"tests/data.json", DependencyImport, "modified"}}},
		{"package-lock.json", "{\"lockfileVersion\": 4}\n", []ProvenanceChange{{"package-lock.json", DependencyLockfile, "modified"}}},
	} {
		t.Run(change.file, func(t *testing.T) {
			repo := importRepo(t, checkoutFiles())
			spec := filepath.Join(repo, "tests/checkout.spec.ts")
			creation := capture(t, spec)
			mustWrite(t, filepath.Join(repo, "shared/extra.cjs"), "module.exports = 1;\n", 0o600)
			mustWrite(t, filepath.Join(repo, change.file), change.contents, 0o600)
			r := CompareAgentProvenance(creation, capture(t, spec))
			if r.Status != "mismatch" || r.Dependencies != "mismatch" || r.Source != "matched" || !reflect.DeepEqual(r.DependencyChanges, change.want) {
				t.Fatalf("got %+v changes %+v", r, r.DependencyChanges)
			}
		})
	}

	// Removing an import from the spec changes the spec and drops the helper.
	t.Run("removed", func(t *testing.T) {
		repo := importRepo(t, checkoutFiles())
		spec := filepath.Join(repo, "tests/checkout.spec.ts")
		creation := capture(t, spec)
		mustWrite(t, spec, strings.Replace(checkoutSpec, "const data = require('./data.json');\n", "", 1), 0o600)
		r := CompareAgentProvenance(creation, capture(t, spec))
		if r.Status != "mismatch" || r.Source != "mismatch" || !reflect.DeepEqual(r.DependencyChanges, []ProvenanceChange{{"tests/data.json", DependencyImport, "removed"}}) {
			t.Fatalf("got %+v", r)
		}
	})
}

// A helper edited before or during execution is refused or invalidated, and
// an import graph that can no longer be captured fails closed.
func TestAgentProvenanceHelperDriftFailsExecution(t *testing.T) {
	for _, change := range []string{"before", "during", "unresolvable"} {
		t.Run(change, func(t *testing.T) {
			repo := importRepo(t, map[string]string{"test.spec.ts": "import { helper } from './helper';\n", "helper.ts": "export const helper = 1;\n"})
			spec := filepath.Join(repo, "test.spec.ts")
			creation := capture(t, spec)
			edit := func() {
				contents := "export const helper = 2;\n"
				if change == "unresolvable" {
					contents = "export const helper = await import(process.env.HELPER);\n"
				}
				mustWrite(t, filepath.Join(repo, "helper.ts"), contents, 0o600)
			}
			e := &provenanceExecutor{}
			if change == "during" {
				e.mutate = edit
			} else {
				edit()
			}
			job := fakeJob("job-001", "success", 0)
			job.Spec = spec
			summary, err := Execute(context.Background(), Plan{Version: 1, RunID: "run-provenance", Jobs: []Job{job}}, ExecuteOptions{AgentProvenance: &creation, Executor: e, Adapters: []Adapter{fakeAdapter{}}, MaxAttempts: 3, ReceiptDir: t.TempDir()})
			if err != nil || summary.Complete || len(summary.Receipts) != 1 {
				t.Fatalf("helper drift completed: %v", err)
			}
			r := summary.Receipts[0]
			if r.Status != StatusError || r.Validated || r.AgentProvenance.Status == "matched" || e.calls != map[string]int{"before": 0, "during": 1, "unresolvable": 0}[change] {
				t.Fatalf("helper drift passed or retried: %+v calls=%d", r, e.calls)
			}
			switch change {
			case "before", "during":
				if r.AgentProvenance.Dependencies != "mismatch" || !reflect.DeepEqual(r.AgentProvenance.DependencyChanges, []ProvenanceChange{{"helper.ts", DependencyImport, "modified"}}) {
					t.Fatalf("changed helper not named: %+v", r.AgentProvenance)
				}
			case "unresolvable":
				if r.AgentProvenance.Status != "unknown" || !strings.Contains(r.Error, "non-literal specifier") || !strings.Contains(r.Error, "helper.ts") {
					t.Fatalf("unresolvable graph not unknown: %+v %q", r.AgentProvenance, r.Error)
				}
			}
		})
	}
}

// Imports that leave the repository, cannot be resolved or exceed the
// bounds fail capture, so provenance can only be unknown.
func TestProvenanceImportGraphFailsClosed(t *testing.T) {
	big := strings.Repeat("a", provenanceSourceLimit+1)
	almost := "// " + strings.Repeat("a", 1_000_000) + "\n"
	cases := map[string]struct {
		files  map[string]string
		setup  func(t *testing.T, repo string)
		reason string
	}{
		"outside": {files: map[string]string{"t.spec.ts": "import '../outside';\n"}, setup: func(t *testing.T, repo string) {
			mustWrite(t, filepath.Join(filepath.Dir(repo), "outside.ts"), "export {};\n", 0o600)
		}, reason: "outside the repository"},
		"symlink outside": {files: map[string]string{"t.spec.ts": "import './link';\n"}, setup: func(t *testing.T, repo string) {
			outside := filepath.Join(filepath.Dir(repo), "target.ts")
			mustWrite(t, outside, "export {};\n", 0o600)
			if err := os.Symlink(outside, filepath.Join(repo, "link.ts")); err != nil {
				t.Skip("symlinks unavailable")
			}
		}, reason: "outside the repository"},
		"lockfile symlink outside": {files: map[string]string{"t.spec.ts": "export {};\n"}, setup: func(t *testing.T, repo string) {
			outside := filepath.Join(filepath.Dir(repo), "yarn.lock")
			mustWrite(t, outside, "# lock\n", 0o600)
			if err := os.Symlink(outside, filepath.Join(repo, "yarn.lock")); err != nil {
				t.Skip("symlinks unavailable")
			}
		}, reason: "lockfile yarn.lock resolves outside the repository"},
		"missing":                 {files: map[string]string{"t.spec.ts": "import { gone } from './gone';\n"}, reason: `import "./gone" in t.spec.ts:1 cannot be resolved`},
		"missing transitive":      {files: map[string]string{"t.spec.ts": "import './a';\n", "a.ts": "\nexport * from './b';\n"}, reason: `import "./b" in a.ts:2 cannot be resolved`},
		"directory without index": {files: map[string]string{"t.spec.ts": "import './dir';\n", "dir/readme.txt": "x"}, reason: "cannot be resolved"},
		"dynamic variable":        {files: map[string]string{"t.spec.ts": "const m = await import(name);\n"}, reason: "non-literal specifier at line 1 in t.spec.ts"},
		"dynamic concat":          {files: map[string]string{"t.spec.ts": "\nconst m = require('./a' + suffix);\n", "a.ts": ""}, reason: "non-literal specifier at line 2"},
		"dynamic template":        {files: map[string]string{"t.spec.ts": "const m = await import(`./${name}`);\n"}, reason: "non-literal specifier"},
		"escaped specifier":       {files: map[string]string{"t.spec.ts": "import './\\u0061';\n", "a.ts": ""}, reason: "escape sequences"},
		"file url":                {files: map[string]string{"t.spec.ts": "import 'file:///etc/hosts';\n"}, reason: "file URL import"},
		"large helper":            {files: map[string]string{"t.spec.ts": "import './big.json';\n", "big.json": big}, reason: "big.json exceeds 1 MiB"},
		"graph bytes": {files: map[string]string{}, setup: func(t *testing.T, repo string) {
			var spec strings.Builder
			for index := range 9 {
				fmt.Fprintf(&spec, "import './m%d';\n", index)
				mustWrite(t, filepath.Join(repo, fmt.Sprintf("m%d.ts", index)), almost, 0o600)
			}
			mustWrite(t, filepath.Join(repo, "t.spec.ts"), spec.String(), 0o600)
		}, reason: "exceed 8 MiB together"},
		"module count": {files: map[string]string{"t.spec.ts": "import './m0';\n"}, setup: func(t *testing.T, repo string) {
			for index := range provenanceMaxModules + 1 {
				mustWrite(t, filepath.Join(repo, fmt.Sprintf("m%d.ts", index)), fmt.Sprintf("import './m%d';\n", index+1), 0o600)
			}
			mustWrite(t, filepath.Join(repo, fmt.Sprintf("m%d.ts", provenanceMaxModules+1)), "", 0o600)
		}, reason: fmt.Sprintf("more than %d local modules", provenanceMaxModules)},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			repo := importRepo(t, c.files)
			if c.setup != nil {
				c.setup(t, repo)
			}
			_, err := CaptureAgentProvenance(context.Background(), filepath.Join(repo, "t.spec.ts"), "fixture-agent")
			var graph ImportGraphError
			if !errors.As(err, &graph) || !strings.Contains(err.Error(), c.reason) {
				t.Fatalf("err = %v, want import graph error containing %q", err, c.reason)
			}
		})
	}
}

// jsString writes s as a single-quoted JavaScript string literal.
func jsString(s string) string {
	return "'" + strings.NewReplacer(`\`, `\\`, `'`, `\'`).Replace(s) + "'"
}

// An absolute specifier names a host path, not a module relative to the
// importer, so every form fails capture on every OS, even one that names a
// file inside the repository: POSIX, rooted backslash, drive-letter and UNC.
func TestProvenanceAbsoluteSpecifiersAreOutsideOnEveryOS(t *testing.T) {
	repo := importRepo(t, map[string]string{"inside.ts": "export {};\n"})
	outside := filepath.Join(filepath.Dir(repo), "abs.ts")
	mustWrite(t, outside, "export {};\n", 0o600)
	inside := filepath.Join(repo, "inside.ts")
	for _, specifier := range []string{
		outside, filepath.ToSlash(outside), inside, filepath.ToSlash(inside),
		"/etc/hosts", `\etc\hosts`, "C:/Windows/win.ini", `c:\Windows\win.ini`, "C:relative.ts",
		`\\server\share\x.ts`, "//server/share/x.ts", `\\?\C:\x.ts`,
	} {
		for _, form := range []string{"import %s;\n", "export * from %s;\n", "await import(%s);\n", "require(%s);\n"} {
			source := fmt.Sprintf(form, jsString(specifier))
			mustWrite(t, filepath.Join(repo, "t.spec.ts"), source, 0o600)
			_, err := CaptureAgentProvenance(context.Background(), filepath.Join(repo, "t.spec.ts"), "fixture-agent")
			var graph ImportGraphError
			if !errors.As(err, &graph) || !strings.Contains(err.Error(), "is an absolute path, which provenance treats as outside the repository") {
				t.Errorf("%q: err = %v", source, err)
			}
		}
	}
	for specifier, want := range map[string]bool{
		"/x": true, `\x`: true, "C:/x": true, "z:x": true, `\\s\x`: true, "//s/x": true,
		"./x": false, ".": false, "node:fs": false, "@scope/x": false, "1:x": false, "x": false,
	} {
		if absoluteSpecifier(specifier) != want {
			t.Errorf("absoluteSpecifier(%q) != %v", specifier, want)
		}
	}
}

// A backslash relative specifier resolves where backslash is a separator and
// otherwise fails capture; it is never skipped as a package.
func TestProvenanceBackslashRelativeSpecifier(t *testing.T) {
	repo := importRepo(t, map[string]string{"t.spec.ts": `import '.\\helpers\\a';` + "\n", "helpers/a.ts": "export {};\n"})
	p, err := CaptureAgentProvenance(context.Background(), filepath.Join(repo, "t.spec.ts"), "fixture-agent")
	if filepath.Separator == '\\' {
		if err != nil || !reflect.DeepEqual(dependencyPaths(p), []string{"import:helpers/a.ts"}) {
			t.Fatalf("windows: %v %v", dependencyPaths(p), err)
		}
	} else if !strings.Contains(fmt.Sprint(err), `import ".\\helpers\\a" in t.spec.ts:1 cannot be resolved`) {
		t.Fatalf("posix: %v", err)
	}
}

// Cycles terminate, every module is recorded once, and the spec is not
// recorded as its own dependency.
func TestProvenanceImportGraphCycles(t *testing.T) {
	repo := importRepo(t, map[string]string{
		"t.spec.ts": "import './a';\nimport './a.ts';\nimport './b.js';\n",
		"a.ts":      "import './b';\nimport './t.spec';\n",
		"b.ts":      "import './a';\nimport './b';\n",
	})
	p := capture(t, filepath.Join(repo, "t.spec.ts"))
	if got := dependencyPaths(p); !reflect.DeepEqual(got, []string{"import:a.ts", "import:b.ts"}) {
		t.Fatalf("dependencies = %v", got)
	}
}

// Packages are not followed; relative imports into installed packages are
// left to the lockfile.
func TestProvenanceImportGraphSkipsPackages(t *testing.T) {
	repo := importRepo(t, map[string]string{
		"t.spec.ts":                       "import { test } from '@playwright/test';\nimport x from '@9l/playwright';\nimport 'node:fs';\nimport y from 'lodash';\nimport z from '@/helpers';\nimport '#internal';\nimport './node_modules/pkg/index.js';\n",
		"node_modules/pkg/index.js":       "import './missing-is-fine';\n",
		"nested/deeper/yarn.lock":         "# not on the spec's path\n",
		"pnpm-lock.yaml":                  "lockfileVersion: 9\n",
		"node_modules/.package-lock.json": "{}\n",
	})
	p := capture(t, filepath.Join(repo, "t.spec.ts"))
	if got := dependencyPaths(p); !reflect.DeepEqual(got, []string{"lockfile:pnpm-lock.yaml"}) {
		t.Fatalf("dependencies = %v", got)
	}
}

// The nearest directory with a lockfile wins over the repository root.
func TestProvenanceNearestLockfile(t *testing.T) {
	repo := importRepo(t, map[string]string{"package-lock.json": "{}", "app/yarn.lock": "#", "app/e2e/t.spec.ts": ""})
	p := capture(t, filepath.Join(repo, "app/e2e/t.spec.ts"))
	if got := dependencyPaths(p); !reflect.DeepEqual(got, []string{"lockfile:app/yarn.lock"}) {
		t.Fatalf("dependencies = %v", got)
	}
}

func TestScanImports(t *testing.T) {
	for source, want := range map[string][]string{
		"import a from './a'; export * from \"./b\"; export { c } from './c';": {"./a", "./b", "./c"},
		"import x = require('./x');":                                               {"./x"},
		"const m = await import('./m', { with: { type: 'json' } });":               {"./m"},
		"const t = `${await import('./inner')}`;":                                  {"./inner"},
		"const t = `a ${`b ${import('./deep')}`} c`; import './after';":            {"./deep", "./after"},
		"const q = x / 2; const r = /import('.\\/no')/g; import './yes';":          {"./yes"},
		"if (a) { } /'/.test(s);\nimport './next-line';":                           {"./next-line"},
		"obj.require(x); obj?.import(y); import.meta.url; function require(id) {}": nil,
		"const from = 'not-an-import'; ({ import: 'nor-this' });":                  nil,
		"import type { T } from './types';":                                        {"./types"},
		`import 'c:\\x\\y'; import "it\"s"; import 'a\'b';`:                        {`c:\x\y`, `it"s`, `a'b`},
		"const s = 'unterminated\nimport './ok';":                                  {"./ok"},
		"require();": nil,
	} {
		refs, err := scanImports([]byte(source))
		var got []string
		for _, ref := range refs {
			got = append(got, ref.specifier)
		}
		if err != nil || !slices.Equal(got, want) {
			t.Errorf("%q: got %v, %v; want %v", source, got, err, want)
		}
	}
}

// Version 2 records must carry a sorted, bounded list whose digest matches;
// version 1 records stay valid and are compared with scope "spec".
func TestAgentProvenanceDependencyRecordValidation(t *testing.T) {
	repo := importRepo(t, map[string]string{"t.spec.ts": "import './a';\nimport './b';\n", "a.ts": "", "b.ts": ""})
	p := capture(t, filepath.Join(repo, "t.spec.ts"))
	tamper := func(change func(*AgentProvenance)) []byte {
		copied := p
		copied.Dependencies = slices.Clone(p.Dependencies)
		change(&copied)
		raw, _ := json.Marshal(copied)
		return raw
	}
	for name, raw := range map[string][]byte{
		"digest": tamper(func(c *AgentProvenance) { c.Dependencies[0].SHA256 = strings.Repeat("0", 64) }),
		"order":  tamper(func(c *AgentProvenance) { c.Dependencies[0], c.Dependencies[1] = c.Dependencies[1], c.Dependencies[0] }),
		"escape": tamper(func(c *AgentProvenance) {
			c.Dependencies[0].Path = "../a.ts"
			c.DependenciesSHA256 = dependenciesDigest(c.Dependencies)
		}),
		"kind": tamper(func(c *AgentProvenance) {
			c.Dependencies[0].Kind = "package"
			c.DependenciesSHA256 = dependenciesDigest(c.Dependencies)
		}),
		"missing":  tamper(func(c *AgentProvenance) { c.DependenciesSHA256 = "" }),
		"v1 extra": tamper(func(c *AgentProvenance) { c.Version = 1 }),
		"unknown":  []byte(strings.Replace(string(tamper(func(*AgentProvenance) {})), `"version":2`, `"version":2,"contents":"x"`, 1)),
	} {
		if _, err := ParseAgentProvenance(raw); err == nil {
			t.Errorf("%s: invalid record accepted", name)
		}
	}
	if _, err := ParseAgentProvenance(tamper(func(*AgentProvenance) {})); err != nil {
		t.Fatal(err)
	}
	v1 := p
	v1.Version, v1.Dependencies, v1.DependenciesSHA256 = 1, nil, ""
	raw, _ := json.Marshal(v1)
	if _, err := ParseAgentProvenance(raw); err != nil {
		t.Fatal(err)
	}
	mustWrite(t, filepath.Join(repo, "a.ts"), "changed", 0o600)
	r := CompareAgentProvenance(v1, capture(t, filepath.Join(repo, "t.spec.ts")))
	if r.Status != "matched" || r.Scope != ProvenanceScopeSpec || r.Dependencies != "" {
		t.Fatalf("version 1 record not compared as spec scope: %+v", r)
	}
}
