package runner

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"unicode/utf8"
)

// Bounds for the local import graph of a provenance snapshot. Exceeding any
// of them fails capture, so provenance stays unknown instead of matching on a
// partial graph.
const (
	provenanceSourceLimit   = 1 << 20  // one spec or local module
	provenanceMaxModules    = 256      // local modules, excluding the spec
	provenanceGraphBytes    = 8 << 20  // all local modules together
	provenanceLockfileLimit = 64 << 20 // one lockfile, hashed as a stream
	provenancePathLimit     = 512      // one recorded repo-relative path
)

// Dependency kinds recorded in a version 2 snapshot.
const (
	DependencyImport   = "import"
	DependencyLockfile = "lockfile"
)

// provenanceLockfiles are digested from the nearest directory, between the
// spec and the repository root, that contains any of them. Installed
// packages are left to the lockfile and never read.
var provenanceLockfiles = []string{"package-lock.json", "npm-shrinkwrap.json", "pnpm-lock.yaml", "yarn.lock", "bun.lock", "bun.lockb"}

// provenanceParsed lists the extensions whose imports are followed. Other
// local files (JSON, text) are digested but not parsed.
var provenanceParsed = []string{".ts", ".tsx", ".mts", ".cts", ".js", ".jsx", ".mjs", ".cjs"}

// ImportGraphError says why the spec's local import graph could not be
// captured. Its message names repo-relative paths and bounded specifiers only.
type ImportGraphError struct{ reason string }

func (e ImportGraphError) Error() string { return "local import graph: " + e.reason }

func graphError(format string, args ...any) error {
	return ImportGraphError{fmt.Sprintf(format, args...)}
}

// quoteSpecifier bounds source text before it is placed in a message.
func quoteSpecifier(specifier string) string {
	if len(specifier) > 120 {
		specifier = strings.ToValidUTF8(specifier[:120], "") + "…"
	}
	return strconv.Quote(specifier)
}

type importRef struct {
	specifier string
	line      int
	escaped   bool
}

type graphModule struct{ abs, rel string }

type importGraph struct {
	ctx     context.Context
	root    string
	seen    map[string]bool // repo-relative real paths already queued
	queue   []graphModule
	deps    []ProvenanceDependency
	bytes   int64
	modules int
}

// captureDependencies digests the spec's transitive local imports and the
// nearest lockfile. Source is scanned lexically and never executed.
//
// Modules are identified by their real repo-relative path, not by file
// identity: relative imports resolve from a module's directory, so a hard
// link (or a second spelling on a case-insensitive file system) is followed
// and recorded separately. That can only add entries, never drop one.
func captureDependencies(ctx context.Context, root, specAbs, specRel string, specData []byte) ([]ProvenanceDependency, error) {
	g := &importGraph{ctx: ctx, root: root, seen: map[string]bool{specRel: true}}
	if err := g.follow(graphModule{specAbs, specRel}, specData); err != nil {
		return nil, err
	}
	for len(g.queue) > 0 {
		if ctx.Err() != nil {
			return nil, errors.New("provenance capture canceled")
		}
		module := g.queue[0]
		g.queue = g.queue[1:]
		data, err := g.read(module)
		if err != nil {
			return nil, err
		}
		g.deps = append(g.deps, ProvenanceDependency{Path: module.rel, Kind: DependencyImport, SHA256: provenanceHash(data)})
		if slices.Contains(provenanceParsed, strings.ToLower(filepath.Ext(module.abs))) {
			if err := g.follow(module, data); err != nil {
				return nil, err
			}
		}
	}
	if err := g.lockfiles(filepath.Dir(specAbs)); err != nil {
		return nil, err
	}
	slices.SortFunc(g.deps, func(a, b ProvenanceDependency) int { return strings.Compare(a.Path, b.Path) })
	return g.deps, nil
}

// absoluteSpecifier reports a module specifier that names a host path rather
// than a path relative to the importer: a leading slash or backslash
// (including UNC `//server` and `\\server`) or a drive letter (`C:`). It is
// classified the same way on every OS, because a specifier is not a host path.
func absoluteSpecifier(specifier string) bool {
	if strings.HasPrefix(specifier, "/") || strings.HasPrefix(specifier, `\`) {
		return true
	}
	if len(specifier) >= 2 && specifier[1] == ':' {
		c := specifier[0] | 0x20
		return c >= 'a' && c <= 'z'
	}
	return false
}

// relativeSpecifier reports `.`, `..` and paths starting with either and a
// slash or backslash. A backslash form resolves only where it is a separator
// (Windows); elsewhere it cannot be resolved and fails capture.
func relativeSpecifier(specifier string) bool {
	if specifier == "." || specifier == ".." {
		return true
	}
	for _, prefix := range []string{"./", "../", `.\`, `..\`} {
		if strings.HasPrefix(specifier, prefix) {
			return true
		}
	}
	return false
}

// follow queues the local modules that one source file imports.
func (g *importGraph) follow(module graphModule, data []byte) error {
	refs, err := scanImports(data)
	if err != nil {
		return graphError("%s in %s", err, module.rel)
	}
	for _, ref := range refs {
		specifier := ref.specifier
		switch {
		case ref.escaped:
			// The decoded specifier is unknown, so it cannot be classified.
			return graphError("import %s in %s:%d uses escape sequences", quoteSpecifier(specifier), module.rel, ref.line)
		case strings.HasPrefix(specifier, "file:"):
			return graphError("file URL import %s in %s:%d is not supported", quoteSpecifier(specifier), module.rel, ref.line)
		case absoluteSpecifier(specifier):
			return graphError("import %s in %s:%d is an absolute path, which provenance treats as outside the repository", quoteSpecifier(specifier), module.rel, ref.line)
		case !relativeSpecifier(specifier):
			continue // a package, built-in or path alias; packages are left to the lockfile
		}
		base := filepath.Join(filepath.Dir(module.abs), filepath.FromSlash(specifier))
		resolved := resolveLocalModule(filepath.Clean(base), true)
		if resolved == "" {
			return graphError("import %s in %s:%d cannot be resolved", quoteSpecifier(specifier), module.rel, ref.line)
		}
		real, err := filepath.EvalSymlinks(resolved)
		if err != nil {
			return graphError("import %s in %s:%d cannot be resolved", quoteSpecifier(specifier), module.rel, ref.line)
		}
		rel, err := filepath.Rel(g.root, real)
		if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) || filepath.IsAbs(rel) {
			return graphError("import %s in %s:%d resolves outside the repository", quoteSpecifier(specifier), module.rel, ref.line)
		}
		rel = filepath.ToSlash(rel)
		if slices.Contains(strings.Split(rel, "/"), "node_modules") {
			continue // installed package files are left to the lockfile
		}
		info, err := os.Lstat(real)
		if err != nil || !info.Mode().IsRegular() {
			return graphError("import %s in %s:%d is not a regular file", quoteSpecifier(specifier), module.rel, ref.line)
		}
		if g.seen[rel] {
			continue
		}
		if !validDependencyPath(rel) {
			return graphError("import %s in %s:%d has a path that cannot be recorded", quoteSpecifier(specifier), module.rel, ref.line)
		}
		if g.modules++; g.modules > provenanceMaxModules {
			return graphError("more than %d local modules", provenanceMaxModules)
		}
		g.seen[rel] = true
		g.queue = append(g.queue, graphModule{real, rel})
	}
	return nil
}

func (g *importGraph) read(module graphModule) ([]byte, error) {
	info, err := os.Lstat(module.abs)
	if err != nil || !info.Mode().IsRegular() {
		return nil, graphError("%s is unavailable", module.rel)
	}
	if info.Size() > provenanceSourceLimit {
		return nil, graphError("%s exceeds 1 MiB", module.rel)
	}
	file, err := os.Open(module.abs)
	if err != nil {
		return nil, graphError("%s is unavailable", module.rel)
	}
	defer file.Close()
	opened, err := file.Stat()
	if err != nil || !os.SameFile(info, opened) {
		return nil, graphError("%s changed during capture", module.rel)
	}
	data, err := io.ReadAll(io.LimitReader(file, provenanceSourceLimit+1))
	if err != nil {
		return nil, graphError("%s is unavailable", module.rel)
	}
	if len(data) > provenanceSourceLimit {
		return nil, graphError("%s exceeds 1 MiB", module.rel)
	}
	if g.bytes += int64(len(data)); g.bytes > provenanceGraphBytes {
		return nil, graphError("local modules exceed %d MiB together", provenanceGraphBytes>>20)
	}
	return data, nil
}

// lockfiles digests the lockfiles in the nearest directory, from the spec's
// directory up to the repository root, that has any.
func (g *importGraph) lockfiles(dir string) error {
	for {
		found := false
		for _, name := range provenanceLockfiles {
			candidate := filepath.Join(dir, name)
			if _, err := os.Lstat(candidate); err != nil {
				continue
			}
			found = true
			sum, rel, err := g.lockfile(candidate)
			if err != nil {
				return err
			}
			if err := g.addLockfile(rel, sum); err != nil {
				return err
			}
		}
		rel, err := filepath.Rel(g.root, dir)
		if found || err != nil || rel == "." || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
			return nil
		}
		dir = filepath.Dir(dir)
	}
}

// addLockfile records a lockfile once per path. A lockfile the spec also
// imports (for example as JSON), or two lockfile names linked to one file,
// keeps a single entry of kind lockfile; differing digests mean it changed
// during capture.
func (g *importGraph) addLockfile(rel, sum string) error {
	for index, dep := range g.deps {
		if dep.Path == rel {
			if dep.SHA256 != sum {
				return graphError("lockfile %s changed during capture", rel)
			}
			g.deps[index].Kind = DependencyLockfile
			return nil
		}
	}
	g.deps = append(g.deps, ProvenanceDependency{Path: rel, Kind: DependencyLockfile, SHA256: sum})
	return nil
}

func (g *importGraph) lockfile(path string) (string, string, error) {
	shown, _ := filepath.Rel(g.root, path)
	shown = filepath.ToSlash(shown)
	real, err := filepath.EvalSymlinks(path)
	if err != nil {
		return "", "", graphError("lockfile %s is unavailable", shown)
	}
	rel, err := filepath.Rel(g.root, real)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) || filepath.IsAbs(rel) {
		return "", "", graphError("lockfile %s resolves outside the repository", shown)
	}
	rel = filepath.ToSlash(rel)
	info, err := os.Lstat(real)
	if err != nil || !info.Mode().IsRegular() || !validDependencyPath(rel) {
		return "", "", graphError("lockfile %s is not a regular file", shown)
	}
	if info.Size() > provenanceLockfileLimit {
		return "", "", graphError("lockfile %s exceeds %d MiB", shown, provenanceLockfileLimit>>20)
	}
	file, err := os.Open(real)
	if err != nil {
		return "", "", graphError("lockfile %s is unavailable", shown)
	}
	defer file.Close()
	opened, err := file.Stat()
	if err != nil || !os.SameFile(info, opened) {
		return "", "", graphError("lockfile %s changed during capture", shown)
	}
	hash := sha256.New()
	n, err := io.Copy(hash, io.LimitReader(file, provenanceLockfileLimit+1))
	if err != nil || n > provenanceLockfileLimit {
		return "", "", graphError("lockfile %s is unavailable or exceeds %d MiB", shown, provenanceLockfileLimit>>20)
	}
	return hex.EncodeToString(hash.Sum(nil)), rel, nil
}

// resolveLocalModule follows the Playwright loader's resolution of a relative
// specifier: the exact file, then a TypeScript or default extension, then the
// directory's index file.
func resolveLocalModule(base string, directory bool) string {
	if info, err := os.Stat(base); err == nil && info.Mode().IsRegular() {
		return base
	}
	lookups := []struct{ ext, others string }{
		{".js", ".jsx .ts .tsx"},
		{".jsx", ".tsx"},
		{".cjs", ".cts"},
		{".mjs", ".mts"},
		{"", ".js .ts .jsx .tsx .cjs .mjs .cts .mts"},
	}
	for _, lookup := range lookups {
		if !strings.HasSuffix(base, lookup.ext) {
			continue
		}
		stem := strings.TrimSuffix(base, lookup.ext)
		for _, other := range strings.Fields(lookup.others) {
			if info, err := os.Stat(stem + other); err == nil && info.Mode().IsRegular() {
				return stem + other
			}
		}
		break
	}
	if info, err := os.Stat(base); directory && err == nil && info.IsDir() {
		return resolveLocalModule(filepath.Join(base, "index"), false)
	}
	return ""
}

func validDependencyPath(path string) bool {
	if path == "" || len(path) > provenancePathLimit || !utf8.ValidString(path) || strings.HasPrefix(path, "/") || strings.Contains(path, "\\") {
		return false
	}
	for _, r := range path {
		if r < 0x20 || r == 0x7f {
			return false
		}
	}
	for _, segment := range strings.Split(path, "/") {
		if segment == "" || segment == "." || segment == ".." {
			return false
		}
	}
	return true
}

// dependenciesDigest binds the sorted entries into one digest.
func dependenciesDigest(deps []ProvenanceDependency) string {
	hash := sha256.New()
	for _, dep := range deps {
		fmt.Fprintf(hash, "%s\x00%s\x00%s\n", dep.Kind, dep.Path, dep.SHA256)
	}
	return hex.EncodeToString(hash.Sum(nil))
}

// jsToken is one significant token of a lexical scan: 'i' identifier, 's'
// complete string or substitution-free template, 't' template with a
// substitution, 'T' end of such a template, 'p' punctuator, 'n' number or
// unterminated literal, 'r' regular expression.
type jsToken struct {
	kind    byte
	text    string
	escaped bool
	line    int
}

var regexKeywords = []string{"return", "typeof", "instanceof", "in", "of", "new", "delete", "void", "throw", "case", "do", "else", "yield", "await"}

type importScanner struct {
	refs    []importRef
	err     error
	prev    [3]jsToken
	pending *importRef
}

func isDot(t jsToken) bool { return t.kind == 'p' && (t.text == "." || t.text == "?.") }

// emit recognizes `import 'x'`, `from 'x'` (import and export), and
// `import('x')` / `require('x')` with a single literal argument. A call
// whose argument is not a literal cannot be followed and fails the scan.
func (s *importScanner) emit(tok jsToken) {
	if s.err != nil {
		return
	}
	if s.pending != nil {
		if tok.kind == 'p' && (tok.text == ")" || tok.text == ",") {
			s.refs = append(s.refs, *s.pending)
		} else {
			s.err = fmt.Errorf("import or require with a non-literal specifier at line %d", s.pending.line)
		}
		s.pending = nil
	}
	p1, p2, p3 := s.prev[0], s.prev[1], s.prev[2]
	if tok.kind == 's' && p1.kind == 'i' && (p1.text == "from" || p1.text == "import") && !isDot(p2) {
		s.refs = append(s.refs, importRef{tok.text, tok.line, tok.escaped})
	}
	if p1.kind == 'p' && p1.text == "(" && p2.kind == 'i' && (p2.text == "import" || p2.text == "require") && !isDot(p3) && !(p3.kind == 'i' && p3.text == "function") {
		switch {
		case tok.kind == 's':
			s.pending = &importRef{tok.text, tok.line, tok.escaped}
		case tok.kind == 'p' && tok.text == ")":
		default:
			s.err = fmt.Errorf("import or require with a non-literal specifier at line %d", tok.line)
		}
	}
	s.prev = [3]jsToken{tok, p1, p2}
}

func (s *importScanner) regexAllowed() bool {
	p := s.prev[0]
	switch p.kind {
	case 0:
		return true
	case 'i':
		return slices.Contains(regexKeywords, p.text)
	case 'p':
		return p.text != ")" && p.text != "]" && p.text != "}"
	}
	return false
}

func isIdentByte(c byte) bool {
	return c == '_' || c == '$' || c >= 0x80 || c == '\\' || (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9')
}

// scanImports lexically scans JavaScript or TypeScript for module
// specifiers. It skips comments, strings, templates and regular expressions
// so that import-like text inside them is ignored. It is not a parser: a
// regular expression directly after `)` or `}`, or a quote in JSX text, is
// misread as the start of a string, which ends at the line end and can hide
// an import later on that line.
func scanImports(src []byte) ([]importRef, error) {
	s := &importScanner{}
	n, i, line, depth := len(src), 0, 1, 0
	var templates []int // brace depth at each open template substitution
	// A UTF-8 byte order mark is not part of the first token. The caller
	// hashes the original bytes; only the scan skips it.
	src = bytes.TrimPrefix(src, []byte("\xef\xbb\xbf"))
	n = len(src)
	if bytes.HasPrefix(src, []byte("#!")) {
		for i < n && src[i] != '\n' {
			i++
		}
	}
	// template scans a template body from i until its end or a substitution.
	template := func(head bool, start int) {
		var value strings.Builder
		escaped := false
		for i < n {
			c := src[i]
			switch {
			case c == '\\':
				if i+1 < n && strings.IndexByte("\\'\"`", src[i+1]) >= 0 {
					value.WriteByte(src[i+1])
				} else {
					escaped = true
				}
				if i+1 < n && src[i+1] == '\n' {
					line++
				}
				i += 2
				continue
			case c == '\n':
				line++
			case c == '`':
				i++
				if head {
					s.emit(jsToken{'s', value.String(), escaped, start})
				} else {
					s.emit(jsToken{'T', "", false, line})
				}
				return
			case c == '$' && i+1 < n && src[i+1] == '{':
				i += 2
				if head {
					s.emit(jsToken{'t', "", false, start})
				}
				templates = append(templates, depth)
				return
			}
			value.WriteByte(c)
			i++
		}
	}
	for i < n && s.err == nil {
		c := src[i]
		next := byte(0)
		if i+1 < n {
			next = src[i+1]
		}
		switch {
		case c == '\n':
			line++
			i++
		case c == ' ' || c == '\t' || c == '\r' || c == '\f' || c == '\v':
			i++
		case c == '/' && next == '/':
			for i < n && src[i] != '\n' {
				i++
			}
		case c == '/' && next == '*':
			end := bytes.Index(src[i+2:], []byte("*/"))
			if end < 0 {
				end = n - i - 2
			}
			line += bytes.Count(src[i:i+2+end], []byte("\n"))
			i += end + 4
		case c == '\'' || c == '"':
			start, quote := line, c
			var value strings.Builder
			escaped, closed := false, false
			for i++; i < n; i++ {
				ch := src[i]
				if ch == '\\' {
					// Decode the escapes a path can need; anything else
					// leaves the value unknown.
					if i+1 < n && strings.IndexByte("\\'\"`", src[i+1]) >= 0 {
						value.WriteByte(src[i+1])
					} else {
						escaped = true
					}
					if i+1 < n && src[i+1] == '\n' {
						line++
					}
					i++
					continue
				}
				if ch == quote {
					closed = true
					i++
					break
				}
				if ch == '\n' {
					break
				}
				value.WriteByte(ch)
			}
			if closed {
				s.emit(jsToken{'s', value.String(), escaped, start})
			} else {
				s.emit(jsToken{'n', "", false, start})
			}
		case c == '`':
			i++
			template(true, line)
		case c == '}' && len(templates) > 0 && depth == templates[len(templates)-1]:
			templates = templates[:len(templates)-1]
			i++
			template(false, line)
		case isIdentByte(c) && !(c >= '0' && c <= '9'):
			start := i
			for i < n && isIdentByte(src[i]) {
				i++
			}
			s.emit(jsToken{'i', string(src[start:i]), false, line})
		case (c >= '0' && c <= '9') || (c == '.' && next >= '0' && next <= '9'):
			for i < n && (isIdentByte(src[i]) || src[i] == '.') {
				i++
			}
			s.emit(jsToken{'n', "", false, line})
		case c == '/' && s.regexAllowed():
			class := false
			for i++; i < n && src[i] != '\n'; i++ {
				if src[i] == '\\' {
					i++
				} else if src[i] == '[' {
					class = true
				} else if src[i] == ']' {
					class = false
				} else if src[i] == '/' && !class {
					i++
					break
				}
			}
			for i < n && isIdentByte(src[i]) {
				i++
			}
			s.emit(jsToken{'r', "", false, line})
		case c == '?' && next == '.' && !(i+2 < n && src[i+2] >= '0' && src[i+2] <= '9'):
			i += 2
			s.emit(jsToken{'p', "?.", false, line})
		default:
			if c == '{' {
				depth++
			} else if c == '}' {
				depth--
			}
			i++
			s.emit(jsToken{'p', string(c), false, line})
		}
	}
	if s.err == nil && s.pending != nil {
		s.err = fmt.Errorf("import or require with a non-literal specifier at line %d", s.pending.line)
	}
	return s.refs, s.err
}
