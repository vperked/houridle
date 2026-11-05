// Package update keeps houridle up to date from its GitHub releases: it
// finds the newest release, downloads the build for this computer, checks
// it against the release's SHA256SUMS, and swaps it in for the running
// program. It also fetches single files from the repository (the Steam
// settings in steam.json).
//
// Requests can carry a GitHub token, for a private fork or to raise
// GitHub's limit on requests (a secret: it's only ever sent to GitHub's API,
// and never logged). houridle's own repository is public and needs none.
package update

import (
	"bufio"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"
)

// API is GitHub's API. A variable so tests can use a local fake.
var API = "https://api.github.com"

// DefaultRepo is where houridle's releases are published.
const DefaultRepo = "vperked/houridle"

const (
	apiTimeout      = 30 * time.Second
	downloadTimeout = 10 * time.Minute
	maxBinary       = 200 << 20
)

// Source is a GitHub repository and how to sign in to it.
type Source struct {
	Repo  string        // "owner/name"
	Token func() string // a GitHub token, or "" for a public repository
}

// Release is a published version of houridle.
type Release struct {
	Version   string // "1.12.0"
	Tag       string // "v1.12.0"
	Notes     string
	Published time.Time
	asset     asset // the build for this computer
	sums      asset // SHA256SUMS
}

type asset struct {
	ID   int64
	Name string
}

// AssetName is the release file built for goos/goarch.
func AssetName(goos, goarch string) string {
	if goos == "windows" {
		return "houridle.exe"
	}
	return "houridle-" + goos + "-" + goarch
}

// ErrNoRelease means the repository has no release yet.
var ErrNoRelease = errors.New("no houridle release has been published yet")

// Latest is the newest release, with this computer's build in it.
func Latest(ctx context.Context, src Source) (Release, error) {
	var res struct {
		Tag       string    `json:"tag_name"`
		Body      string    `json:"body"`
		Draft     bool      `json:"draft"`
		Published time.Time `json:"published_at"`
		Assets    []struct {
			ID   int64  `json:"id"`
			Name string `json:"name"`
		} `json:"assets"`
	}
	resp, err := src.get(ctx, fmt.Sprintf("%s/repos/%s/releases/latest", API, src.Repo), "application/vnd.github+json", apiTimeout)
	if err != nil {
		return Release{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusNotFound {
		return Release{}, ErrNoRelease // (or a private repository without a token)
	}
	if err := httpError(resp); err != nil {
		return Release{}, err
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 4<<20)).Decode(&res); err != nil {
		return Release{}, errors.New("couldn't read GitHub's answer")
	}
	rel := Release{Tag: res.Tag, Version: strings.TrimPrefix(res.Tag, "v"), Notes: res.Body, Published: res.Published}
	want := AssetName(runtime.GOOS, runtime.GOARCH)
	for _, a := range res.Assets {
		switch a.Name {
		case want:
			rel.asset = asset{a.ID, a.Name}
		case "SHA256SUMS":
			rel.sums = asset{a.ID, a.Name}
		}
	}
	if rel.asset.ID == 0 {
		return rel, fmt.Errorf("release %s has no build for this computer (%s)", rel.Tag, want)
	}
	if rel.sums.ID == 0 {
		return rel, fmt.Errorf("release %s has no SHA256SUMS to check the download against", rel.Tag)
	}
	return rel, nil
}

// Newer reports whether version a is newer than b ("1.12.0" > "1.6.0"). A
// pre-release ("1.6.0-dev") is older than the same version without one.
func Newer(a, b string) bool {
	pa, sa := split(a)
	pb, sb := split(b)
	for i := range 3 {
		if pa[i] != pb[i] {
			return pa[i] > pb[i]
		}
	}
	return sa == "" && sb != ""
}

func split(v string) ([3]int, string) {
	v = strings.TrimPrefix(strings.TrimSpace(v), "v")
	suffix := ""
	if i := strings.IndexAny(v, "-+"); i >= 0 {
		v, suffix = v[:i], v[i:]
	}
	var out [3]int
	for i, part := range strings.SplitN(v, ".", 3) {
		out[i], _ = strconv.Atoi(part)
	}
	return out, suffix
}

// Install downloads the release's build, checks it against SHA256SUMS, and
// puts it in place of the program at exe. The running program carries on
// until it's restarted.
func Install(ctx context.Context, src Source, rel Release, exe string) error {
	sums, err := src.download(ctx, rel.sums, 1<<20)
	if err != nil {
		return fmt.Errorf("couldn't download SHA256SUMS: %w", err)
	}
	want := ""
	sc := bufio.NewScanner(strings.NewReader(string(sums)))
	for sc.Scan() {
		f := strings.Fields(sc.Text())
		if len(f) == 2 && strings.TrimPrefix(f[1], "*") == rel.asset.Name {
			want = strings.ToLower(f[0])
		}
	}
	if len(want) != 64 {
		return fmt.Errorf("SHA256SUMS doesn't list %s", rel.asset.Name)
	}

	dir := filepath.Dir(exe)
	tmp, err := os.CreateTemp(dir, ".houridle-update-*")
	if err != nil {
		return fmt.Errorf("can't write next to houridle (%s): %w", dir, err)
	}
	defer os.Remove(tmp.Name()) // gone after the rename
	resp, err := src.get(ctx, fmt.Sprintf("%s/repos/%s/releases/assets/%d", API, src.Repo, rel.asset.ID), "application/octet-stream", downloadTimeout)
	if err != nil {
		tmp.Close()
		return err
	}
	defer resp.Body.Close()
	if err := httpError(resp); err != nil {
		tmp.Close()
		return err
	}
	h := sha256.New()
	n, err := io.Copy(io.MultiWriter(tmp, h), io.LimitReader(resp.Body, maxBinary+1))
	if cerr := tmp.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		return fmt.Errorf("couldn't download the update: %w", err)
	}
	if n > maxBinary {
		return errors.New("the download is far too big; not installing it")
	}
	if got := hex.EncodeToString(h.Sum(nil)); got != want {
		return errors.New("the download doesn't match SHA256SUMS; not installing it")
	}
	if err := os.Chmod(tmp.Name(), 0o755); err != nil {
		return err
	}
	if runtime.GOOS == "windows" {
		// A running .exe can't be replaced, but it can be renamed.
		old := exe + ".old"
		os.Remove(old)
		if err := os.Rename(exe, old); err != nil {
			return err
		}
		if err := os.Rename(tmp.Name(), exe); err != nil {
			os.Rename(old, exe)
			return err
		}
		return nil
	}
	return os.Rename(tmp.Name(), exe)
}

// File is one file from the repository's default branch.
func File(ctx context.Context, src Source, path string) ([]byte, error) {
	resp, err := src.get(ctx, fmt.Sprintf("%s/repos/%s/contents/%s", API, src.Repo, path), "application/vnd.github.raw", apiTimeout)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusNotFound {
		return nil, fmt.Errorf("%s isn't in %s yet", path, src.Repo)
	}
	if err := httpError(resp); err != nil {
		return nil, err
	}
	return io.ReadAll(io.LimitReader(resp.Body, 1<<20))
}

func (src Source) token() string {
	if src.Token == nil {
		return ""
	}
	return src.Token()
}

func (src Source) get(ctx context.Context, u, accept string, timeout time.Duration) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", accept)
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")
	if t := src.token(); t != "" {
		req.Header.Set("Authorization", "Bearer "+t) // Go drops it on the redirect to the download host
	}
	resp, err := (&http.Client{Timeout: timeout}).Do(req)
	if err != nil {
		return nil, fmt.Errorf("couldn't reach GitHub: %w", unwrap(err))
	}
	return resp, nil
}

func (src Source) download(ctx context.Context, a asset, limit int64) ([]byte, error) {
	resp, err := src.get(ctx, fmt.Sprintf("%s/repos/%s/releases/assets/%d", API, src.Repo, a.ID), "application/octet-stream", apiTimeout)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if err := httpError(resp); err != nil {
		return nil, err
	}
	return io.ReadAll(io.LimitReader(resp.Body, limit))
}

func httpError(resp *http.Response) error {
	switch {
	case resp.StatusCode == http.StatusUnauthorized:
		return errors.New("GitHub didn't accept the token")
	case resp.StatusCode == http.StatusForbidden || resp.StatusCode == http.StatusTooManyRequests:
		return errors.New("GitHub is limiting requests; trying again later")
	case resp.StatusCode != http.StatusOK:
		return fmt.Errorf("GitHub answered HTTP %d", resp.StatusCode)
	}
	return nil
}

func unwrap(err error) error {
	for {
		u := errors.Unwrap(err)
		if u == nil {
			return err
		}
		err = u
	}
}

// TokenFrom finds a GitHub token: the one given (from config.json), else
// the GITHUB_TOKEN or GH_TOKEN environment variables, else the GitHub CLI's
// sign-in (gh auth token) if it's installed.
func TokenFrom(configured string) func() string {
	return func() string {
		if configured != "" {
			return configured
		}
		for _, v := range []string{"HOURIDLE_GITHUB_TOKEN", "GITHUB_TOKEN", "GH_TOKEN"} {
			if t := strings.TrimSpace(os.Getenv(v)); t != "" {
				return t
			}
		}
		if path, err := exec.LookPath("gh"); err == nil {
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			if out, err := exec.CommandContext(ctx, path, "auth", "token").Output(); err == nil {
				return strings.TrimSpace(string(out))
			}
		}
		return ""
	}
}
