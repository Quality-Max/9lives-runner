package confirm

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// repository builds a Git repository with two commits: the first ships a
// buggy app, the second fixes it. It returns the root and both commits.
func repository(t *testing.T) (string, string, string) {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git is not installed")
	}
	root := t.TempDir()
	run := func(args ...string) string {
		t.Helper()
		command := exec.Command("git", append([]string{"-C", root, "-c", "user.name=9lives", "-c", "user.email=9lives@example.invalid", "-c", "commit.gpgsign=false"}, args...)...)
		output, err := command.CombinedOutput()
		if err != nil {
			t.Fatalf("git %v: %v %s", args, err, output)
		}
		return strings.TrimSpace(string(output))
	}
	write := func(relative, content string) {
		t.Helper()
		full := filepath.Join(root, filepath.FromSlash(relative))
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	run("init", "--quiet")
	write(".gitignore", "node_modules/\n")
	write("package.json", `{"devDependencies":{"@playwright/test":"1.61.1"}}`)
	write("app/shop.ts", "export const count = (items: unknown[]) => items.length - 1;\n")
	write("tests/existing.spec.ts", "// committed version\n")
	run("add", ".")
	run("commit", "--quiet", "-m", "buggy")
	unfixed := run("rev-parse", "HEAD")
	write("app/shop.ts", "export const count = (items: unknown[]) => items.length;\n")
	run("commit", "--quiet", "-am", "fix")
	fixed := run("rev-parse", "HEAD")
	// Installed dependencies, ignored by Git like a real install.
	write("node_modules/@playwright/test/package.json", `{"name":"@playwright/test"}`)
	resolved, err := filepath.EvalSymlinks(root)
	if err != nil {
		t.Fatal(err)
	}
	return resolved, unfixed, fixed
}

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestOpenFindsTheRepositoryAndRefusesOutsideSpecs(t *testing.T) {
	root, _, _ := repository(t)
	ctx := context.Background()
	spec := filepath.Join(root, "tests", "repro.spec.ts")
	writeFile(t, spec, "// repro\n")
	repo, err := Open(ctx, spec)
	if err != nil || repo.Root != root || repo.SpecPath != "tests/repro.spec.ts" || string(repo.Spec) != "// repro\n" || len(repo.SpecSHA256()) != 64 {
		t.Fatalf("repo=%+v err=%v", repo, err)
	}
	outside := filepath.Join(t.TempDir(), "outside.spec.ts")
	writeFile(t, outside, "")
	for name, path := range map[string]string{"missing": filepath.Join(root, "tests", "missing.spec.ts"), "outside a repository": outside, "a directory": filepath.Join(root, "tests")} {
		var setup SetupError
		if _, err := Open(ctx, path); !errors.As(err, &setup) {
			t.Errorf("%s: err=%v", name, err)
		}
	}
}

func TestResolveRefusesOptionsAndUnknownRevisions(t *testing.T) {
	root, unfixed, fixed := repository(t)
	ctx := context.Background()
	repo := Repository{Root: root, SpecPath: "tests/repro.spec.ts"}
	if commit, err := repo.Resolve(ctx, "HEAD~1"); err != nil || commit != unfixed {
		t.Fatalf("HEAD~1: %s %v", commit, err)
	}
	if commit, err := repo.Head(ctx); err != nil || commit != fixed {
		t.Fatalf("HEAD: %s %v", commit, err)
	}
	for _, ref := range []string{"", "--output=/tmp/x", "-h", "no-such-branch", "HEAD\nHEAD", "HEAD:package.json"} {
		var setup SetupError
		if _, err := repo.Resolve(ctx, ref); !errors.As(err, &setup) {
			t.Errorf("ref %q: err=%v", ref, err)
		}
	}
}

func TestDirtyIgnoresOnlyTheReproductionSpec(t *testing.T) {
	root, _, _ := repository(t)
	ctx := context.Background()
	repo := Repository{Root: root, SpecPath: "tests/repro.spec.ts"}
	writeFile(t, filepath.Join(root, "tests", "repro.spec.ts"), "// repro\n")
	if dirty, err := repo.Dirty(ctx); err != nil || dirty {
		t.Fatalf("an untracked reproduction spec alone: dirty=%v err=%v", dirty, err)
	}
	writeFile(t, filepath.Join(root, "app", "shop.ts"), "// changed\n")
	if dirty, _ := repo.Dirty(ctx); !dirty {
		t.Fatal("a modified tracked file is a change")
	}
	writeFile(t, filepath.Join(root, "app", "shop.ts"), "export const count = (items: unknown[]) => items.length;\n")
	writeFile(t, filepath.Join(root, "app", "new.ts"), "")
	if dirty, _ := repo.Dirty(ctx); !dirty {
		t.Fatal("an untracked file other than the spec is a change")
	}
}

func TestSpecAtAndDependenciesCompareWithTheWorkingTree(t *testing.T) {
	root, unfixed, fixed := repository(t)
	ctx := context.Background()
	repo := Repository{Root: root, SpecPath: "tests/existing.spec.ts", Spec: []byte("// committed version\n")}
	if got := repo.SpecAt(ctx, unfixed); got != SpecSame {
		t.Fatalf("same spec: %s", got)
	}
	repo.Spec = []byte("// edited\n")
	if got := repo.SpecAt(ctx, unfixed); got != SpecReplaced {
		t.Fatalf("edited spec: %s", got)
	}
	repo.SpecPath = "tests/repro.spec.ts"
	if got := repo.SpecAt(ctx, unfixed); got != SpecAbsent {
		t.Fatalf("new spec: %s", got)
	}
	if repo.DependenciesDiffer(ctx, unfixed) || repo.DependenciesDiffer(ctx, fixed) {
		t.Fatal("unchanged manifests reported as different")
	}
	writeFile(t, filepath.Join(root, "package-lock.json"), "{}")
	if !repo.DependenciesDiffer(ctx, fixed) {
		t.Fatal("a lockfile only in the working tree must be reported")
	}
}

func TestCheckoutIsolatesARevisionAndCleansUpWithoutTouchingDependencies(t *testing.T) {
	root, unfixed, _ := repository(t)
	ctx := context.Background()
	// A hook that would run on checkout must not.
	marker := filepath.Join(t.TempDir(), "hook-ran")
	hook := filepath.Join(root, ".git", "hooks", "post-checkout")
	writeFile(t, hook, "#!/bin/sh\ntouch '"+filepath.ToSlash(marker)+"'\n")
	if err := os.Chmod(hook, 0o755); err != nil {
		t.Fatal(err)
	}
	spec := filepath.Join(root, "tests", "existing.spec.ts")
	writeFile(t, spec, "// reproduction from the working tree\n")
	repo, err := Open(ctx, spec)
	if err != nil {
		t.Fatal(err)
	}
	checkout, err := repo.Checkout(ctx, unfixed)
	if err != nil {
		if runtime.GOOS == "windows" && errors.As(err, new(SetupError)) {
			t.Skip("this Windows account cannot create symbolic links")
		}
		t.Fatal(err)
	}
	tree := filepath.Dir(filepath.Dir(checkout.Spec))
	app, _ := os.ReadFile(filepath.Join(tree, "app", "shop.ts"))
	written, _ := os.ReadFile(checkout.Spec)
	if !strings.Contains(string(app), "items.length - 1") || string(written) != "// reproduction from the working tree\n" {
		t.Fatalf("checkout holds app %q and spec %q", app, written)
	}
	if _, err := os.Stat(filepath.Join(tree, "node_modules", "@playwright", "test", "package.json")); err != nil {
		t.Fatalf("node_modules not linked: %v", err)
	}
	if _, err := os.Stat(marker); err == nil {
		t.Fatal("a repository hook ran during checkout")
	}
	// The caller's working tree is untouched.
	if current, _ := os.ReadFile(filepath.Join(root, "app", "shop.ts")); strings.Contains(string(current), "- 1") {
		t.Fatal("checkout changed the working tree")
	}
	checkout.Close()
	checkout.Close()
	if _, err := os.Stat(tree); !os.IsNotExist(err) {
		t.Fatalf("checkout left behind: %v", err)
	}
	if _, err := os.Stat(filepath.Join(root, "node_modules", "@playwright", "test", "package.json")); err != nil {
		t.Fatalf("cleanup removed the installed dependencies: %v", err)
	}
	list, err := exec.Command("git", "-C", root, "worktree", "list", "--porcelain").Output()
	if err != nil || strings.Count(string(list), "worktree ") != 1 {
		t.Fatalf("worktree registration left behind: %s %v", list, err)
	}
}
