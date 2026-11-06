package tests

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"houridle/backend/features/update"
	"houridle/backend/online/steamcm"
	"houridle/backend/online/steamver"
	"houridle/backend/views/panel"
)

// fakeGitHub serves one release with a build for this computer.
type fakeGitHub struct {
	*httptest.Server
	mu      sync.Mutex
	tag     string
	binary  []byte
	badSum  bool
	tokens  []string // Authorization headers seen
	steam   string   // steam.json in the repository ("" = none)
	private bool     // answer 404 without a token
}

func newFakeGitHub(t *testing.T) *fakeGitHub {
	f := &fakeGitHub{tag: "v99.1.0", binary: []byte("#!/bin/sh\necho new houridle\n")}
	f.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		defer f.mu.Unlock()
		auth := r.Header.Get("Authorization")
		f.tokens = append(f.tokens, auth)
		if f.private && auth == "" {
			http.NotFound(w, r)
			return
		}
		switch r.URL.Path {
		case "/repos/o/houridle/releases/latest":
			fmt.Fprintf(w, `{"tag_name":%q,"body":"Fixes","published_at":"2026-11-01T10:00:00Z","assets":[{"id":1,"name":%q},{"id":2,"name":"SHA256SUMS"},{"id":3,"name":"houridle-other"}]}`,
				f.tag, update.AssetName(runtime.GOOS, runtime.GOARCH))
		case "/repos/o/houridle/releases/assets/1":
			if r.Header.Get("Accept") != "application/octet-stream" {
				w.WriteHeader(http.StatusBadRequest)
				return
			}
			w.Write(f.binary)
		case "/repos/o/houridle/releases/assets/2":
			sum := sha256.Sum256(f.binary)
			if f.badSum {
				sum[0] ^= 0xff
			}
			fmt.Fprintf(w, "%s  %s\n%s  houridle-other\n", hex.EncodeToString(sum[:]), update.AssetName(runtime.GOOS, runtime.GOARCH), strings.Repeat("0", 64))
		case "/repos/o/houridle/contents/steam.json":
			if f.steam == "" {
				http.NotFound(w, r)
				return
			}
			io.WriteString(w, f.steam)
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(f.Close)
	old := update.API
	update.API = f.URL
	t.Cleanup(func() { update.API = old })
	return f
}

func TestVersionsCompare(t *testing.T) {
	for _, c := range []struct {
		a, b  string
		newer bool
	}{{"1.12.0", "1.6.0", true}, {"1.6.0", "1.12.0", false}, {"v2.0.0", "1.99.9", true}, {"1.6.0", "1.6.0-dev", true}, {"1.6.0-dev", "1.6.0", false}, {"1.6.0", "1.6.0", false}} {
		if got := update.Newer(c.a, c.b); got != c.newer {
			t.Errorf("Newer(%s, %s) = %v", c.a, c.b, got)
		}
	}
}

func TestUpdateInstallsAVerifiedBuild(t *testing.T) {
	f := newFakeGitHub(t)
	f.private = true
	src := update.Source{Repo: "o/houridle"}
	if _, err := update.Latest(context.Background(), src); !errors.Is(err, update.ErrNoRelease) {
		t.Errorf("nothing found (no release, or private without a token) is \"no release\": %v", err)
	}
	src.Token = func() string { return "test-token" }
	rel, err := update.Latest(context.Background(), src)
	if err != nil || rel.Version != "99.1.0" {
		t.Fatalf("latest: %+v %v", rel, err)
	}
	exe := filepath.Join(t.TempDir(), "houridle")
	os.WriteFile(exe, []byte("old"), 0o755)

	f.mu.Lock()
	f.badSum = true
	f.mu.Unlock()
	if err := update.Install(context.Background(), src, rel, exe); err == nil || !strings.Contains(err.Error(), "SHA256SUMS") {
		t.Errorf("a download that doesn't match must be refused: %v", err)
	}
	if b, _ := os.ReadFile(exe); string(b) != "old" {
		t.Fatal("a refused update must leave houridle as it was")
	}
	f.mu.Lock()
	f.badSum = false
	f.mu.Unlock()
	if err := update.Install(context.Background(), src, rel, exe); err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(exe); !strings.Contains(string(b), "new houridle") {
		t.Errorf("not replaced: %q", b)
	}
	if st, _ := os.Stat(exe); runtime.GOOS != "windows" && st.Mode().Perm()&0o100 == 0 {
		t.Error("the new build should be runnable")
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, a := range f.tokens[1:] {
		if a != "Bearer test-token" {
			t.Errorf("token sent as %q", a)
		}
	}
}

func TestPanelUpdatesAndRestarts(t *testing.T) {
	t.Setenv("HOURIDLE_GITHUB_TOKEN", "test-token")
	f := newFakeGitHub(t)
	ctl, _, appDir := newPanel(t)
	_ = ctl
	// newPanel's controller has no Restart; make one that has.
	exe := filepath.Join(t.TempDir(), "houridle")
	os.WriteFile(exe, []byte("old"), 0o755)
	var restarted atomic.Bool
	ctl.Close()
	c2, err := panel.New(panel.Options{AppDir: appDir, ExePath: exe, Version: "1.12.0", WebAddr: ":0", Restart: func() { restarted.Store(true) }})
	if err != nil {
		t.Fatal(err)
	}
	defer c2.Close()
	if err := c2.SetAutoUpdate(false); err != nil {
		t.Fatal(err)
	}
	if err := writeRepo(appDir); err != nil {
		t.Fatal(err)
	}
	info, err := c2.CheckUpdate(context.Background())
	if err != nil || !info.Available || info.Latest != "99.1.0" || info.Current != "1.12.0" || info.Auto {
		t.Fatalf("check: %+v %v", info, err)
	}
	if err := c2.InstallUpdate(context.Background()); err != nil {
		t.Fatal(err)
	}
	waitUntil(t, "the restart", restarted.Load)
	if b, _ := os.ReadFile(exe); !strings.Contains(string(b), "new houridle") {
		t.Error("the new build should be in place")
	}
	if !hasEvent(c2, "Updated houridle from 1.12.0 to 99.1.0") {
		t.Error("the update should be in the activity")
	}

	// Nothing newer: nothing to install.
	f.mu.Lock()
	f.tag = "v1.0.0"
	f.mu.Unlock()
	if info, _ := c2.CheckUpdate(context.Background()); info.Available {
		t.Error("an older release isn't an update")
	}
	if err := c2.InstallUpdate(context.Background()); err == nil {
		t.Error("nothing to install should say so")
	}
}

func writeRepo(appDir string) error {
	return os.WriteFile(filepath.Join(appDir, "config.json"), []byte(`{"games":[],"update":{"repo":"o/houridle","manual":true}}`), 0o600)
}

func TestSteamSettingsFollowTheRepo(t *testing.T) {
	t.Setenv("HOURIDLE_GITHUB_TOKEN", "test-token")
	defer steamcm.SetVersions(steamcm.DefaultProtocolVersion, steamcm.DefaultClientVersion)
	f := newFakeGitHub(t)
	manifest := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		io.WriteString(w, "\"win32\"\n{\n\t\"version\"\t\t\"1769731672\"\n}\n")
	}))
	defer manifest.Close()
	oldM := steamver.ManifestURL
	steamver.ManifestURL = manifest.URL
	defer func() { steamver.ManifestURL = oldM }()
	oldE := panel.SteamEvery
	panel.SteamEvery = time.Hour
	defer func() { panel.SteamEvery = oldE }()
	fastTicks(t)

	for _, bad := range []string{`{"protocolVersion":1,"clientPackageVersion":1771}`, `{"protocolVersion":65581,"clientPackageVersion":0}`, `{"protocolVersion":65581,"clientPackageVersion":1,"directoryUrl":"https://evil.example/"}`, `nope`} {
		if _, err := steamver.Parse([]byte(bad)); err == nil {
			t.Errorf("%s should be refused", bad)
		}
	}

	f.mu.Lock()
	f.steam = `{"protocolVersion":65581,"clientPackageVersion":1800,"note":"Steam bumped the protocol"}`
	f.mu.Unlock()
	ctl, _, appDir := newPanel(t)
	writeRepo(appDir)
	ctl.Resume()
	waitUntil(t, "the Steam settings", func() bool { return ctl.Status().Panel.Steam.FromRepo })
	st := ctl.Status().Panel.Steam
	if st.Protocol != 65581 || st.Client != 1800 || st.Build != "1769731672" {
		t.Errorf("steam: %+v", st)
	}
	if p, c := steamcm.Versions(); p != 65581 || c != 1800 {
		t.Errorf("sign-ins should use the new values: %d %d", p, c)
	}
	if _, err := os.Stat(filepath.Join(appDir, "steam-settings.json")); err != nil {
		t.Error("the settings should be kept for the next start")
	}
	if !hasEvent(ctl, "Picked up new Steam settings") {
		t.Error("the change should be in the activity")
	}
}

func TestServerListFallsBackToTheLastGoodOne(t *testing.T) {
	dir := t.TempDir()
	old, oldCache := steamcm.DirectoryURL, steamcm.ServerCache
	defer func() { steamcm.DirectoryURL, steamcm.ServerCache = old, oldCache }()
	steamcm.ServerCache = filepath.Join(dir, "cm-servers.json")
	up := true
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !up {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		io.WriteString(w, `{"response":{"serverlist":[{"endpoint":"a:443","type":"websockets"},{"endpoint":"b:443","type":"websockets"}]}}`)
	}))
	defer srv.Close()
	steamcm.DirectoryURL = srv.URL
	if list, err := steamcm.ListServers(context.Background()); err != nil || len(list) != 2 {
		t.Fatalf("list: %v %v", list, err)
	}
	up = false
	if list, err := steamcm.ListServers(context.Background()); err != nil || len(list) != 2 || list[0] != "a:443" {
		t.Errorf("the last good list should be used: %v %v", list, err)
	}
}
