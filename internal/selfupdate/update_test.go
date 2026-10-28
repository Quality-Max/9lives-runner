package selfupdate

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// The candidate is a real executable on every native CI platform. Its version
// endpoint has the public CLI identity shape; no shell script stands in for a
// Windows executable or for the process that validates a downloaded candidate.
func TestMain(m *testing.M) {
	if len(os.Args) == 4 && os.Args[1] == "version" && os.Args[2] == "--format" && os.Args[3] == "json" {
		fmt.Print(`{"version":1,"name":"9l","implementation":"go-runner","cliVersion":"0.5.4"}`)
		os.Exit(0)
	}
	os.Exit(m.Run())
}

type tripper func(*http.Request) (*http.Response, error)

func (f tripper) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func response(r *http.Request, status int, body []byte) *http.Response {
	return &http.Response{StatusCode: status, Body: io.NopCloser(bytes.NewReader(body)), ContentLength: int64(len(body)), Header: http.Header{}, Request: r}
}

func fixtureClient(t *testing.T, archive []byte, latest string, mutateManifest func([]byte) []byte) *Client {
	t.Helper()
	c := New("0.5.3")
	bundle, err := c.bundle()
	if err != nil {
		t.Fatal(err)
	}
	ext := ".tar.gz"
	if runtime.GOOS == "windows" {
		ext = ".zip"
	}
	manifest := checksumFixture(bundle+ext, archive)
	if mutateManifest != nil {
		manifest = mutateManifest(manifest)
	}
	c.http.Transport = tripper(func(r *http.Request) (*http.Response, error) {
		if r.Header.Get("Authorization") != "" {
			t.Error("release request carried credentials")
		}
		switch r.URL.Path {
		case "/Quality-Max/9lives-runner/releases/latest":
			resp := response(r, http.StatusFound, nil)
			resp.Header.Set("Location", repository+"/releases/tag/v"+latest)
			return resp, nil
		case "/Quality-Max/9lives-runner/releases/tag/v" + latest:
			return response(r, http.StatusOK, nil), nil
		case "/Quality-Max/9lives-runner/releases/download/v" + latest + "/SHA256SUMS":
			return response(r, http.StatusOK, manifest), nil
		case "/Quality-Max/9lives-runner/releases/download/v" + latest + "/" + bundle + ext:
			return response(r, http.StatusOK, archive), nil
		}
		t.Errorf("unexpected release request method/path: %s %s", r.Method, r.URL.Path)
		return response(r, http.StatusNotFound, nil), nil
	})
	return c
}

func checksumFixture(selected string, data []byte) []byte {
	var lines []string
	for _, platform := range []string{"darwin", "linux", "windows"} {
		for _, arch := range []string{"amd64", "arm64"} {
			ext := ".tar.gz"
			if platform == "windows" {
				ext = ".zip"
			}
			name := "9l-" + platform + "-" + arch + ext
			digest := sha256.Sum256(nil)
			if name == selected {
				digest = sha256.Sum256(data)
			}
			lines = append(lines, hex.EncodeToString(digest[:])+"  "+name)
		}
	}
	return []byte(strings.Join(lines, "\n") + "\n")
}

type member struct {
	name string
	data []byte
	kind byte
}

func archiveFixture(t *testing.T, platform, architecture string, executable []byte, mutate func([]member) []member) []byte {
	t.Helper()
	bundle := "9l-" + platform + "-" + architecture
	binary := "9l"
	if platform == "windows" {
		binary = "9l.exe"
	}
	members := []member{{bundle + "/", nil, tar.TypeDir}, {bundle + "/" + binary, executable, tar.TypeReg},
		{bundle + "/LICENSE", []byte("license"), tar.TypeReg}, {bundle + "/NOTICE", []byte("notice"), tar.TypeReg},
		{bundle + "/THIRD-PARTY-NOTICES.txt", []byte("dependencies"), tar.TypeReg}}
	if mutate != nil {
		members = mutate(members)
	}
	var out bytes.Buffer
	if platform == "windows" {
		w := zip.NewWriter(&out)
		for _, item := range members {
			h := &zip.FileHeader{Name: item.name, Method: zip.Deflate}
			mode := os.FileMode(0o755)
			if item.kind == tar.TypeDir {
				mode |= os.ModeDir
			}
			if item.kind == tar.TypeSymlink {
				mode |= os.ModeSymlink
			}
			h.SetMode(mode)
			file, err := w.CreateHeader(h)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := file.Write(item.data); err != nil {
				t.Fatal(err)
			}
		}
		if err := w.Close(); err != nil {
			t.Fatal(err)
		}
	} else {
		compressed := gzip.NewWriter(&out)
		w := tar.NewWriter(compressed)
		for _, item := range members {
			h := &tar.Header{Name: item.name, Typeflag: item.kind, Mode: 0o755, Size: int64(len(item.data))}
			if item.kind == tar.TypeSymlink {
				h.Size, h.Linkname = 0, "outside"
			}
			if err := w.WriteHeader(h); err != nil {
				t.Fatal(err)
			}
			if item.kind == tar.TypeReg {
				if _, err := w.Write(item.data); err != nil {
					t.Fatal(err)
				}
			}
		}
		if err := w.Close(); err != nil {
			t.Fatal(err)
		}
		if err := compressed.Close(); err != nil {
			t.Fatal(err)
		}
	}
	return out.Bytes()
}

func TestStableLatestAndSemanticVersionComparison(t *testing.T) {
	for _, tc := range []struct {
		current, latest  string
		available, valid bool
	}{
		{"0.5.3", "0.5.4", true, true}, {"0.9.9", "0.10.0", true, true},
		{"0.5.3", "0.5.3", false, true}, {"1.0.0", "0.5.4", false, true},
		{"dev", "0.5.4", false, false}, {"0.5.3", "01.5.4", false, false},
		{"0.5.3", "0.5.4-beta", false, false}, {"0.5.3", "999999999999999999999999.0.0", false, false},
	} {
		available, err := newer(tc.current, tc.latest)
		if (err == nil) != tc.valid || available != tc.available {
			t.Errorf("%s -> %s: available=%v err=%v", tc.current, tc.latest, available, err)
		}
	}
	c := fixtureClient(t, nil, "0.5.4", nil)
	release, err := c.Check(context.Background())
	if err != nil || !release.Available || release.Latest != "0.5.4" {
		t.Fatalf("release=%+v err=%v", release, err)
	}
}

func TestLatestRejectsForeignAndUnstableRedirects(t *testing.T) {
	for _, destination := range []string{
		"http://github.com/Quality-Max/9lives-runner/releases/tag/v0.5.4",
		"https://example.invalid/Quality-Max/9lives-runner/releases/tag/v0.5.4",
		"https://github.com/foreign/repo/releases/tag/v0.5.4",
		repository + "/releases/tag/v0.5.4-beta", repository + "/releases/tag/v0.5.4?secret=fixture-canary",
	} {
		c := New("0.5.3")
		c.http.Transport = tripper(func(r *http.Request) (*http.Response, error) {
			if r.URL.Path == "/Quality-Max/9lives-runner/releases/latest" {
				resp := response(r, 302, nil)
				resp.Header.Set("Location", destination)
				return resp, nil
			}
			return response(r, 200, nil), nil
		})
		if _, err := c.Check(context.Background()); err == nil || strings.Contains(err.Error(), "fixture-canary") {
			t.Fatalf("foreign/unstable release accepted or error leaked: %v", err)
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	c := New("0.5.3")
	if _, err := c.Check(ctx); err == nil {
		t.Fatal("canceled check succeeded")
	}
}

func TestChecksumManifestMustBeCompleteAndExact(t *testing.T) {
	name, payload := "9l-linux-amd64.tar.gz", []byte("archive")
	valid := checksumFixture(name, payload)
	if err := verifyChecksum(valid, name, payload); err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSpace(string(valid)), "\n")
	for _, invalid := range [][]byte{
		valid[:len(valid)/2], append(append([]byte{}, valid...), []byte(lines[0]+"\n")...),
		[]byte(strings.ReplaceAll(string(valid), "\n", "\r\n")), []byte(strings.ToUpper(string(valid))),
		[]byte(strings.Replace(string(valid), "9l-linux-amd64.tar.gz", "../9l-linux-amd64.tar.gz", 1)),
		[]byte(strings.Replace(string(valid), lines[1], lines[0], 1)),
	} {
		if err := verifyChecksum(invalid, name, payload); err == nil {
			t.Fatal("malformed manifest accepted")
		}
	}
	if err := verifyChecksum(valid, name, []byte("changed")); err == nil {
		t.Fatal("checksum mismatch accepted")
	}
}

func TestArchivesRejectUnsafeMissingDuplicateAndTruncatedMembers(t *testing.T) {
	for _, platform := range []string{"linux", "windows"} {
		binary := "9l"
		if platform == "windows" {
			binary = "9l.exe"
		}
		for _, mutation := range []struct {
			name   string
			change func([]member) []member
		}{
			{"missing", func(m []member) []member { return m[:4] }},
			{"traversal", func(m []member) []member { m[1].name = "../outside"; return m }},
			{"symlink", func(m []member) []member { m[1].kind = tar.TypeSymlink; m[1].data = nil; return m }},
			{"duplicate", func(m []member) []member { m[2] = m[1]; return m }},
			{"empty", func(m []member) []member { m[1].data = nil; return m }},
		} {
			t.Run(platform+"/"+mutation.name, func(t *testing.T) {
				data := archiveFixture(t, platform, "amd64", []byte("binary"), mutation.change)
				if err := extractArchive(data, t.TempDir(), "9l-"+platform+"-amd64", binary); err == nil {
					t.Fatal("unsafe archive accepted")
				}
			})
		}
		valid := archiveFixture(t, platform, "amd64", []byte("binary"), nil)
		if err := extractArchive(valid, t.TempDir(), "9l-"+platform+"-amd64", binary); err != nil {
			t.Fatal(err)
		}
		if err := extractArchive(valid[:len(valid)-8], t.TempDir(), "9l-"+platform+"-amd64", binary); err == nil {
			t.Fatal("truncated archive accepted")
		}
	}
}

func TestInstallValidatesCandidateAndPreservesOldOnFailure(t *testing.T) {
	self, _ := os.Executable()
	candidate, err := os.ReadFile(self)
	if err != nil {
		t.Fatal(err)
	}
	archive := archiveFixture(t, runtime.GOOS, runtime.GOARCH, candidate, nil)
	// A separate native executable provides the old identity. This also lets
	// the test simulate a different updater replacing the on-disk runner while
	// the original process still has its old version in memory.
	oldDirectory := t.TempDir()
	oldSource := filepath.Join(oldDirectory, "old.go")
	oldBinary := filepath.Join(oldDirectory, "old.exe")
	if err := os.WriteFile(oldSource, []byte("package main\nimport \"fmt\"\nfunc main(){fmt.Print(`{\"version\":1,\"name\":\"9l\",\"implementation\":\"go-runner\",\"cliVersion\":\"0.5.3\"}`)}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := exec.Command("go", "build", "-o", oldBinary, oldSource).Run(); err != nil {
		t.Fatalf("build old native identity fixture: %v", err)
	}
	oldExecutable, err := os.ReadFile(oldBinary)
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name, latest      string
		badChecksum, lock bool
	}{
		{"success", "0.5.4", false, false}, {"checksum", "0.5.4", true, false},
		{"identity", "0.5.5", false, false}, {"downgrade", "0.5.2", false, false}, {"locked", "0.5.4", false, true},
		{"already-replaced", "0.5.4", false, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			target := filepath.Join(dir, filepath.Base(self))
			old := oldExecutable
			if tc.name == "already-replaced" {
				old = candidate
			}
			if err := os.WriteFile(target, old, 0o755); err != nil {
				t.Fatal(err)
			}
			if tc.lock {
				if err := os.WriteFile(target+".update-lock", nil, 0o600); err != nil {
					t.Fatal(err)
				}
			}
			var mutate func([]byte) []byte
			if tc.badChecksum {
				mutate = func(b []byte) []byte {
					return bytes.Replace(b, []byte("  9l-"+runtime.GOOS), []byte("  foreign-"+runtime.GOOS), 1)
				}
			}
			c := fixtureClient(t, archive, tc.latest, mutate)
			result, err := c.Install(context.Background(), Release{Latest: tc.latest}, target)
			if tc.name == "success" {
				if err != nil {
					t.Fatal(err)
				}
				if result.Version != "0.5.4" {
					t.Fatal("wrong installed version")
				}
				for _, legal := range []string{"LICENSE", "NOTICE", "THIRD-PARTY-NOTICES.txt"} {
					if info, err := os.Stat(filepath.Join(result.NoticesDirectory, legal)); err != nil || info.Size() == 0 {
						t.Fatalf("legal file missing: %s", legal)
					}
				}
				got, err := exec.Command(target, "version", "--format", "json").Output()
				if err != nil || !bytes.Contains(got, []byte(`"cliVersion":"0.5.4"`)) {
					t.Fatalf("installed candidate did not execute: %v", err)
				}
			} else {
				if err == nil {
					t.Fatal("invalid update succeeded")
				}
				got, _ := os.ReadFile(target)
				if !bytes.Equal(got, old) {
					t.Fatal("failed update changed the previous executable")
				}
				entries, _ := os.ReadDir(dir)
				for _, entry := range entries {
					if strings.HasPrefix(entry.Name(), ".9l-release-") {
						t.Fatal("failed update left staging files")
					}
				}
			}
		})
	}
}

func TestReplaceRunningExecutableOnNativePlatform(t *testing.T) {
	if os.Getenv("NINELIVES_UPDATE_HELPER") == "1" {
		self, _ := os.Executable()
		if _, err := replaceExecutable(os.Getenv("NINELIVES_UPDATE_CANDIDATE"), self, filepath.Join(filepath.Dir(self), "previous.exe")); err != nil {
			t.Fatal(err)
		}
		return
	}
	self, _ := os.Executable()
	data, err := os.ReadFile(self)
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	target, candidate := filepath.Join(dir, "running"+filepath.Ext(self)), filepath.Join(dir, "candidate"+filepath.Ext(self))
	if err := os.WriteFile(target, data, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(candidate, data, 0o755); err != nil {
		t.Fatal(err)
	}
	command := exec.Command(target, "-test.run=^TestReplaceRunningExecutableOnNativePlatform$")
	command.Env = append(os.Environ(), "NINELIVES_UPDATE_HELPER=1", "NINELIVES_UPDATE_CANDIDATE="+candidate)
	if err := command.Run(); err != nil {
		t.Fatalf("running executable replacement failed on %s/%s: %v", runtime.GOOS, runtime.GOARCH, err)
	}
	if _, err := os.Stat(candidate); !os.IsNotExist(err) {
		t.Fatal("replacement did not consume candidate")
	}
	if _, err := exec.Command(target, "version", "--format", "json").Output(); err != nil {
		t.Fatal("replacement executable cannot run")
	}
}

func TestFailedReplacementRestoresPreviousExecutable(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "9l.exe")
	if err := os.WriteFile(target, []byte("previous"), 0o755); err != nil {
		t.Fatal(err)
	}
	if _, err := replaceExecutable(filepath.Join(dir, "missing"), target, filepath.Join(dir, "backup")); err == nil {
		t.Fatal("missing candidate installed")
	}
	data, _ := os.ReadFile(target)
	if string(data) != "previous" {
		t.Fatal("replacement failure lost previous executable")
	}
}

func TestCandidateIdentityRejectsAmbiguousAndForeignEvidence(t *testing.T) {
	valid := `{"version":1,"name":"9l","implementation":"go-runner","cliVersion":"0.5.4"}`
	if err := validateIdentity([]byte(valid), "0.5.4"); err != nil {
		t.Fatal(err)
	}
	for _, data := range []string{
		strings.Replace(valid, `"version":1`, `"version":0,"version":1`, 1),
		strings.Replace(valid, `"name":"9l"`, `"name":null`, 1),
		strings.Replace(valid, `"implementation":"go-runner"`, `"implementation":"python"`, 1),
		strings.Replace(valid, `"cliVersion":"0.5.4"`, `"cliVersion":"0.5.3"`, 1),
		valid + valid, valid[:len(valid)-1],
	} {
		if err := validateIdentity([]byte(data), "0.5.4"); err == nil {
			t.Fatal("malformed/foreign candidate identity accepted")
		}
	}
}

func TestDownloadLimitsAndFailureDoNotReturnPartialData(t *testing.T) {
	for _, tc := range []struct {
		status int
		length int64
		data   string
	}{
		{http.StatusNotFound, 0, ""}, {http.StatusOK, 9, "payload"}, {http.StatusOK, -1, "payload"},
	} {
		c := New("0.5.3")
		c.http.Transport = tripper(func(r *http.Request) (*http.Response, error) {
			result := response(r, tc.status, []byte(tc.data))
			result.ContentLength = tc.length
			return result, nil
		})
		data, err := c.download(context.Background(), repository+"/releases/download/v0.5.4/SHA256SUMS", 3)
		if err == nil || data != nil {
			t.Fatal("failed or oversized download returned admissible data")
		}
	}
}
