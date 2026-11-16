package playwright

import (
	"os"
	"path/filepath"
	"testing"
)

// Playwright reports resolved attachment paths. A project opened through a
// symlinked directory must keep its own attachments and still refuse others.
func TestAttachmentInProjectThroughSymlinkedDirectory(t *testing.T) {
	base, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	real := filepath.Join(base, "real-project")
	outside := filepath.Join(base, "outside")
	for _, dir := range []string{filepath.Join(real, "test-results"), outside} {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	alias := filepath.Join(base, "alias")
	if err := os.Symlink(real, alias); err != nil {
		t.Skip("symlinks unavailable")
	}
	inside := filepath.Join(real, "test-results", "error-context.md")
	secret := filepath.Join(outside, "secret.txt")
	escape := filepath.Join(real, "test-results", "escape.md")
	for _, file := range []string{inside, secret} {
		if err := os.WriteFile(file, []byte("x"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.Symlink(secret, escape); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name, workDir, path string
		want                bool
	}{
		{"resolved path under an aliased project", alias, inside, true},
		{"aliased path under an aliased project", alias, filepath.Join(alias, "test-results", "error-context.md"), true},
		{"plain project", real, inside, true},
		{"missing file is only a reference", alias, filepath.Join(real, "test-results", "gone.png"), true},
		{"file outside the project", alias, secret, false},
		{"symlink escaping the project", alias, escape, false},
		{"parent traversal", alias, filepath.Join(alias, "..", "outside", "secret.txt"), false},
	} {
		if got := attachmentInProject(tc.workDir, tc.path); got != tc.want {
			t.Errorf("%s: got %v", tc.name, got)
		}
	}
}
