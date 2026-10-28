// Package selfupdate installs the runner from the same stable GitHub release
// and archive contract as install/9l.run.sh. It never installs project SDKs.
package selfupdate

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"time"

	"github.com/Quality-Max/9lives-runner/internal/runner"
	"github.com/Quality-Max/9lives-runner/internal/strictjson"
)

const repository = "https://github.com/Quality-Max/9lives-runner"
const maxArchive = 128 << 20
const maxBinary = 96 << 20
const maxLegal = 1 << 20

var stableVersion = regexp.MustCompile(`^(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)$`)

// Client uses public releases only: no account, token, or project information
// is sent. Its endpoints cannot be overridden by project configuration.
type Client struct {
	current, platform, architecture string
	http                            *http.Client
}

type Release struct {
	Current, Latest string
	Available       bool
}

type Installation struct {
	Version, NoticesDirectory string
}

func New(current string) *Client {
	return &Client{current: current, platform: runtime.GOOS, architecture: runtime.GOARCH,
		http: &http.Client{Timeout: 30 * time.Second, CheckRedirect: releaseRedirect}}
}

func releaseRedirect(req *http.Request, via []*http.Request) error {
	if len(via) >= 5 || req.URL.Scheme != "https" || req.URL.User != nil || req.URL.Port() != "" {
		return errors.New("release redirect refused")
	}
	switch req.URL.Hostname() {
	case "github.com", "release-assets.githubusercontent.com", "objects.githubusercontent.com", "github-releases.githubusercontent.com":
		return nil
	}
	return errors.New("release redirect host refused")
}

func parseVersion(value string) ([3]uint64, error) {
	var result [3]uint64
	if len(value) > 64 || !stableVersion.MatchString(value) {
		return result, errors.New("updates require a stable runner version")
	}
	for i, part := range strings.Split(value, ".") {
		n, err := strconv.ParseUint(part, 10, 64)
		if err != nil {
			return result, errors.New("release version is out of range")
		}
		result[i] = n
	}
	return result, nil
}

func newer(current, latest string) (bool, error) {
	a, err := parseVersion(current)
	if err != nil {
		return false, err
	}
	b, err := parseVersion(latest)
	if err != nil {
		return false, err
	}
	for i := range a {
		if a[i] != b[i] {
			return b[i] > a[i], nil
		}
	}
	return false, nil
}

// Check follows the release marked Latest, which the release workflow marks
// only after CLI verification and SDK publication. The API's highest tag is
// deliberately not used: a partially published release must not be offered.
func (c *Client) Check(ctx context.Context) (Release, error) {
	if _, err := parseVersion(c.current); err != nil {
		return Release{}, err
	}
	ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	req, _ := http.NewRequestWithContext(ctx, http.MethodHead, repository+"/releases/latest", nil)
	response, err := c.http.Do(req)
	if err != nil {
		return Release{}, errors.New("could not check the latest release")
	}
	defer response.Body.Close()
	final := response.Request.URL
	prefix := "/Quality-Max/9lives-runner/releases/tag/v"
	if response.StatusCode != http.StatusOK || final.Scheme != "https" || final.Host != "github.com" ||
		final.User != nil || final.RawQuery != "" || final.Fragment != "" || !strings.HasPrefix(final.Path, prefix) {
		return Release{}, errors.New("latest release did not resolve to a stable tag of this repository")
	}
	latest := strings.TrimPrefix(final.Path, prefix)
	available, err := newer(c.current, latest)
	if err != nil {
		return Release{}, err
	}
	return Release{Current: c.current, Latest: latest, Available: available}, nil
}

func (c *Client) bundle() (string, error) {
	if (c.platform != "darwin" && c.platform != "linux" && c.platform != "windows") ||
		(c.architecture != "amd64" && c.architecture != "arm64") {
		return "", errors.New("self-update supports macOS, Linux and Windows on amd64 and arm64")
	}
	return "9l-" + c.platform + "-" + c.architecture, nil
}

func (c *Client) download(ctx context.Context, address string, limit int64) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, address, nil)
	if err != nil {
		return nil, errors.New("invalid release address")
	}
	response, err := c.http.Do(req)
	if err != nil {
		return nil, errors.New("release download failed")
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK || response.ContentLength > limit {
		return nil, errors.New("release download missing or too large")
	}
	data, err := io.ReadAll(io.LimitReader(response.Body, limit+1))
	if err != nil || int64(len(data)) > limit {
		return nil, errors.New("release download incomplete or too large")
	}
	return data, nil
}

// verifyChecksum requires the exact six-archive checksum contract. Duplicate,
// foreign, missing, malformed, uppercase or CRLF records are not accepted.
func verifyChecksum(manifest []byte, name string, archive []byte) error {
	expected := map[string]bool{}
	for _, platform := range []string{"darwin", "linux", "windows"} {
		for _, arch := range []string{"amd64", "arm64"} {
			ext := ".tar.gz"
			if platform == "windows" {
				ext = ".zip"
			}
			expected["9l-"+platform+"-"+arch+ext] = true
		}
	}
	lines := strings.Split(strings.TrimSuffix(string(manifest), "\n"), "\n")
	if len(lines) != len(expected) {
		return errors.New("release checksum manifest is incomplete")
	}
	var selected string
	for _, line := range lines {
		if len(line) < 67 || line[64:66] != "  " || !expected[line[66:]] {
			return errors.New("release checksum manifest is malformed")
		}
		digest := line[:64]
		if _, err := hex.DecodeString(digest); err != nil || strings.ToLower(digest) != digest {
			return errors.New("release checksum manifest is malformed")
		}
		delete(expected, line[66:])
		if line[66:] == name {
			selected = digest
		}
	}
	actual := sha256.Sum256(archive)
	if selected != hex.EncodeToString(actual[:]) {
		return errors.New("release checksum verification failed")
	}
	return nil
}

// Install is called only after approval. No shell, project dependency, or
// privileged helper is used. Failure before replacement leaves the executable
// unchanged; platform replacement preserves a recoverable copy on rollback
// failure. Legal files remain in the private release directory beside it.
func (c *Client) Install(ctx context.Context, release Release, executable string) (Installation, error) {
	if ctx.Err() != nil {
		return Installation{}, errors.New("update canceled before installation")
	}
	available, err := newer(c.current, release.Latest)
	if err != nil || !available {
		return Installation{}, errors.New("refusing a downgrade or invalid release")
	}
	bundle, err := c.bundle()
	if err != nil {
		return Installation{}, err
	}
	target, err := filepath.EvalSymlinks(executable)
	if err != nil {
		return Installation{}, errors.New("could not locate the installed runner")
	}
	previous, err := os.Lstat(target)
	if err != nil || !previous.Mode().IsRegular() || previous.Mode()&(os.ModeSetuid|os.ModeSetgid) != 0 {
		return Installation{}, errors.New("installed runner must be an ordinary executable file")
	}
	lock, err := os.OpenFile(target+".update-lock", os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		return Installation{}, errors.New("cannot lock the runner for update; check installation permissions and existing update locks")
	}
	defer func() { _ = lock.Close(); _ = os.Remove(target + ".update-lock") }()
	ctx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()
	// A long-running process may still carry its original version after another
	// updater replaced its executable. Recheck the on-disk identity under the
	// lock; otherwise that stale process could overwrite a newer installation.
	if err := verifyIdentity(ctx, target, c.current); err != nil {
		return Installation{}, errors.New("installed runner no longer matches this process; restart 9l update")
	}
	ext, binary := ".tar.gz", "9l"
	if c.platform == "windows" {
		ext, binary = ".zip", "9l.exe"
	}
	name := bundle + ext
	address := repository + "/releases/download/v" + release.Latest + "/"
	manifest, err := c.download(ctx, address+"SHA256SUMS", 16<<10)
	if err != nil {
		return Installation{}, err
	}
	archive, err := c.download(ctx, address+name, maxArchive)
	if err != nil {
		return Installation{}, err
	}
	if err := verifyChecksum(manifest, name, archive); err != nil {
		return Installation{}, err
	}
	stage, err := os.MkdirTemp(filepath.Dir(target), ".9l-release-"+release.Latest+"-")
	if err != nil {
		return Installation{}, errors.New("cannot stage an update beside the runner; check installation permissions")
	}
	keep := false
	defer func() {
		if !keep {
			_ = os.RemoveAll(stage)
		}
	}()
	if err := extractArchive(archive, stage, bundle, binary); err != nil {
		return Installation{}, err
	}
	candidate := filepath.Join(stage, binary)
	if err := verifyIdentity(ctx, candidate, release.Latest); err != nil {
		return Installation{}, err
	}
	now, err := os.Lstat(target)
	if err != nil || !now.Mode().IsRegular() || !os.SameFile(previous, now) {
		return Installation{}, errors.New("installed runner changed during the update")
	}
	if ctx.Err() != nil {
		return Installation{}, errors.New("update canceled before installation")
	}
	preserve, err := replaceExecutable(candidate, target, filepath.Join(stage, "9l.previous.exe"))
	keep = preserve || err == nil
	if err != nil {
		return Installation{}, err
	}
	return Installation{Version: release.Latest, NoticesDirectory: stage}, nil
}

type identityBuffer struct{ data []byte }

func (b *identityBuffer) Write(p []byte) (int, error) {
	if len(b.data)+len(p) > 16<<10 {
		return 0, errors.New("candidate output limit")
	}
	b.data = append(b.data, p...)
	return len(p), nil
}

func verifyIdentity(ctx context.Context, executable, version string) error {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	command := exec.CommandContext(ctx, executable, "version", "--format", "json")
	command.Env = []string{}
	for _, key := range []string{"SystemRoot", "WINDIR"} {
		if value, present := os.LookupEnv(key); present {
			command.Env = append(command.Env, key+"="+value)
		}
	}
	output := &identityBuffer{}
	command.Stdout, command.Stderr = output, io.Discard
	if err := runner.RunOwnedCommand(ctx, command); err != nil {
		return errors.New("release binary could not verify its identity")
	}
	return validateIdentity(output.data, version)
}

func validateIdentity(data []byte, version string) error {
	if err := strictjson.Value(json.NewDecoder(bytes.NewReader(data))); err != nil {
		return errors.New("release binary identity is malformed")
	}
	var identity struct {
		Version        int    `json:"version"`
		Name           string `json:"name"`
		Implementation string `json:"implementation"`
		CLIVersion     string `json:"cliVersion"`
	}
	if err := json.Unmarshal(data, &identity); err != nil || identity.Version != 1 || identity.Name != "9l" ||
		identity.Implementation != "go-runner" || identity.CLIVersion != version {
		return fmt.Errorf("release binary did not identify as 9l %s", version)
	}
	return nil
}

// ReleaseURL is safe to show: it contains only a validated stable version,
// never a response URL, raw error, signed CDN query or private path.
func ReleaseURL(release Release) string {
	if _, err := parseVersion(release.Latest); err != nil {
		return repository + "/releases"
	}
	return repository + "/releases/tag/v" + url.PathEscape(release.Latest)
}
