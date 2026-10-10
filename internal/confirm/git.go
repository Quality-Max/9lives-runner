package confirm

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"regexp"
	"strings"
)

// SetupError is a problem the caller can fix by changing the command or the
// repository, such as an unknown ref or a spec outside the repository.
type SetupError struct{ Message string }

func (err SetupError) Error() string { return err.Message }

const maxGitOutput = 16 << 20

var commitPattern = regexp.MustCompile(`^[0-9a-f]{40}([0-9a-f]{24})?$`)

// manifests are compared between each revision and the working tree whose
// node_modules both runs use.
var manifests = []string{"package.json", "package-lock.json", "npm-shrinkwrap.json", "yarn.lock", "pnpm-lock.yaml", "bun.lock", "bun.lockb"}

// git runs one git command in root with bounded output. Hooks never run:
// core.hooksPath points at an empty directory the caller owns, or is unset
// for commands that run none.
func git(ctx context.Context, root string, args ...string) ([]byte, error) {
	command := exec.CommandContext(ctx, "git", append([]string{"-C", root}, args...)...)
	var stdout, stderr limitedBuffer
	stdout.limit, stderr.limit = maxGitOutput, 4096
	command.Stdout, command.Stderr = &stdout, &stderr
	// A checkout must not download Git LFS content.
	command.Env = append(os.Environ(), "GIT_LFS_SKIP_SMUDGE=1", "GIT_TERMINAL_PROMPT=0")
	if err := command.Run(); err != nil {
		return nil, err
	}
	if stdout.overflow {
		return nil, errors.New("git output exceeded its limit")
	}
	return stdout.Bytes(), nil
}

type limitedBuffer struct {
	bytes.Buffer
	limit    int
	overflow bool
}

func (buffer *limitedBuffer) Write(data []byte) (int, error) {
	if room := buffer.limit - buffer.Len(); len(data) > room {
		buffer.overflow = true
		if room > 0 {
			buffer.Buffer.Write(data[:room])
		}
		return len(data), nil
	}
	return buffer.Buffer.Write(data)
}

// Repository is the Git working tree that contains a reproduction spec.
type Repository struct {
	Root string
	// SpecPath is the spec's slash-separated path relative to Root.
	SpecPath string
	Spec     []byte
}

// Open finds the repository that contains spec and reads the spec.
func Open(ctx context.Context, spec string) (Repository, error) {
	absolute, err := filepath.Abs(spec)
	if err == nil {
		absolute, err = filepath.EvalSymlinks(absolute)
	}
	if err != nil {
		return Repository{}, SetupError{"reproduction spec not found: " + spec}
	}
	info, err := os.Stat(absolute)
	if err != nil || !info.Mode().IsRegular() {
		return Repository{}, SetupError{"reproduction spec is not a regular file: " + spec}
	}
	raw, err := git(ctx, filepath.Dir(absolute), "rev-parse", "--show-toplevel")
	if err != nil {
		return Repository{}, SetupError{"reproduction spec is not inside a Git working tree: " + spec}
	}
	root, err := filepath.EvalSymlinks(strings.TrimSpace(string(raw)))
	if err != nil {
		return Repository{}, SetupError{"Git working tree unavailable"}
	}
	relative, err := filepath.Rel(root, absolute)
	if err != nil || relative == "." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) || relative == ".." {
		return Repository{}, SetupError{"reproduction spec must be inside the repository"}
	}
	content, err := os.ReadFile(absolute)
	if err != nil {
		return Repository{}, SetupError{"reproduction spec could not be read"}
	}
	return Repository{Root: root, SpecPath: filepath.ToSlash(relative), Spec: content}, nil
}

// SpecSHA256 identifies the spec bytes both revisions run.
func (repo Repository) SpecSHA256() string {
	sum := sha256.Sum256(repo.Spec)
	return hex.EncodeToString(sum[:])
}

// Resolve returns the commit a ref names. Refs that look like options are
// refused rather than passed to git.
func (repo Repository) Resolve(ctx context.Context, ref string) (string, error) {
	if ref == "" || strings.HasPrefix(ref, "-") || strings.ContainsAny(ref, "\x00\n\r") {
		return "", SetupError{fmt.Sprintf("invalid revision %q", ref)}
	}
	raw, err := git(ctx, repo.Root, "rev-parse", "--verify", "--quiet", "--end-of-options", ref+"^{commit}")
	commit := strings.TrimSpace(string(raw))
	if err != nil || !commitPattern.MatchString(commit) {
		return "", SetupError{fmt.Sprintf("revision %q does not name a commit in this repository", ref)}
	}
	return commit, nil
}

// Head returns the working tree's current commit.
func (repo Repository) Head(ctx context.Context) (string, error) {
	return repo.Resolve(ctx, "HEAD")
}

// Dirty reports whether the working tree has changes other than the
// reproduction spec, including untracked files.
func (repo Repository) Dirty(ctx context.Context) (bool, error) {
	raw, err := git(ctx, repo.Root, "status", "--porcelain=v1", "-z", "--untracked-files=all")
	if err != nil {
		return false, errors.New("git status failed")
	}
	entries := bytes.Split(raw, []byte{0})
	for index := 0; index < len(entries); index++ {
		entry := entries[index]
		if len(entry) < 4 {
			continue
		}
		// A rename or copy carries its source path in the next entry.
		if entry[0] == 'R' || entry[0] == 'C' {
			index++
		}
		if string(entry[3:]) != repo.SpecPath {
			return true, nil
		}
	}
	return false, nil
}

// fileAt returns a file's content at a commit, or false when it is absent.
func (repo Repository) fileAt(ctx context.Context, commit, relative string) ([]byte, bool) {
	raw, err := git(ctx, repo.Root, "cat-file", "blob", commit+":"+relative)
	if err != nil {
		return nil, false
	}
	return raw, true
}

// SpecAt says how the reproduction spec relates to a commit's file at the
// same path.
func (repo Repository) SpecAt(ctx context.Context, commit string) string {
	content, ok := repo.fileAt(ctx, commit, repo.SpecPath)
	switch {
	case !ok:
		return SpecAbsent
	case bytes.Equal(content, repo.Spec):
		return SpecSame
	default:
		return SpecReplaced
	}
}

// ancestors lists the spec's directories from the repository root down,
// slash-separated, with "" for the root.
func (repo Repository) ancestors() []string {
	directories := []string{""}
	current := ""
	for _, part := range strings.Split(path.Dir(repo.SpecPath), "/") {
		if part == "." || part == "" {
			continue
		}
		current = path.Join(current, part)
		directories = append(directories, current)
	}
	return directories
}

// DependenciesDiffer reports whether a commit's package manifests or
// lockfiles, in any directory from the root down to the spec, differ from
// the working tree's, whose node_modules every run uses.
func (repo Repository) DependenciesDiffer(ctx context.Context, commit string) bool {
	for _, directory := range repo.ancestors() {
		for _, name := range manifests {
			relative := path.Join(directory, name)
			installed, installedErr := os.ReadFile(filepath.Join(repo.Root, filepath.FromSlash(relative)))
			committed, ok := repo.fileAt(ctx, commit, relative)
			if (installedErr == nil) != ok || (ok && !bytes.Equal(installed, committed)) {
				return true
			}
		}
	}
	return false
}

// Checkout is a detached worktree of one commit holding the working tree's
// reproduction spec.
type Checkout struct {
	// Spec is the absolute path of the reproduction spec in the checkout.
	Spec  string
	repo  Repository
	dir   string
	tree  string
	links []string
}

// Checkout creates a detached worktree of commit in a private temporary
// directory, writes the reproduction spec at its path and links each
// node_modules directory between the root and the spec from the working
// tree. Close removes it.
func (repo Repository) Checkout(ctx context.Context, commit string) (*Checkout, error) {
	dir, err := os.MkdirTemp("", "9lives-confirm-")
	if err != nil {
		return nil, errors.New("checkout directory could not be created")
	}
	checkout := &Checkout{repo: repo, dir: dir, tree: filepath.Join(dir, "tree")}
	hooks := filepath.Join(dir, "hooks")
	if err := os.Mkdir(hooks, 0o700); err != nil {
		checkout.Close()
		return nil, errors.New("checkout directory could not be created")
	}
	if _, err := git(ctx, repo.Root, "-c", "core.hooksPath="+hooks, "worktree", "add", "--detach", "--quiet", checkout.tree, commit); err != nil {
		checkout.Close()
		return nil, errors.New("git worktree add failed")
	}
	for _, directory := range repo.ancestors() {
		source := filepath.Join(repo.Root, filepath.FromSlash(directory), "node_modules")
		target := filepath.Join(checkout.tree, filepath.FromSlash(directory), "node_modules")
		if info, err := os.Stat(source); err != nil || !info.IsDir() {
			continue
		}
		if _, err := os.Lstat(target); err == nil {
			continue
		}
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			checkout.Close()
			return nil, errors.New("checkout directory could not be prepared")
		}
		if err := os.Symlink(source, target); err != nil {
			checkout.Close()
			return nil, SetupError{"node_modules could not be linked into the revision checkout; on Windows, enable Developer Mode or run with permission to create symbolic links"}
		}
		checkout.links = append(checkout.links, target)
	}
	checkout.Spec = filepath.Join(checkout.tree, filepath.FromSlash(repo.SpecPath))
	if err := os.MkdirAll(filepath.Dir(checkout.Spec), 0o755); err != nil {
		checkout.Close()
		return nil, errors.New("checkout directory could not be prepared")
	}
	// Replace, never follow, whatever the revision has at the spec's path.
	_ = os.Remove(checkout.Spec)
	if err := os.WriteFile(checkout.Spec, repo.Spec, 0o644); err != nil {
		checkout.Close()
		return nil, errors.New("reproduction spec could not be written into the checkout")
	}
	return checkout, nil
}

// Close removes the worktree and its registration. It is safe to call more
// than once.
func (checkout *Checkout) Close() {
	if checkout.dir == "" {
		return
	}
	// Unlink the shared node_modules first, so no removal below can reach
	// the working tree's installed dependencies through them. Every recorded
	// path is a link this checkout created; os.Remove deletes the link, never
	// its target, whatever kind of link the platform reports.
	for _, link := range checkout.links {
		_ = os.Remove(link)
		if _, err := os.Lstat(link); err == nil {
			// Leave the checkout in place rather than delete through a link.
			return
		}
	}
	checkout.links = nil
	// Removal must not depend on the run's context, which may be canceled.
	ctx := context.Background()
	if _, err := os.Stat(checkout.tree); err == nil {
		if _, err := git(ctx, checkout.repo.Root, "worktree", "remove", "--force", checkout.tree); err != nil {
			_ = os.RemoveAll(checkout.tree)
		}
	}
	_, _ = git(ctx, checkout.repo.Root, "worktree", "prune")
	_ = os.RemoveAll(checkout.dir)
	checkout.dir = ""
}
