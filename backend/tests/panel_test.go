package tests

import (
	"bytes"
	"context"
	"encoding/json"
	"houridle/backend/features/update"
	"houridle/backend/online/steamver"
	"io"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"

	"houridle/backend/features/freegames"
	"houridle/backend/online/steamauth"
	"houridle/backend/run/config"
	"houridle/backend/run/handover"
	"houridle/backend/views/panel"
	"houridle/backend/views/web"
)

// newPanel starts a controller for alice (signed in) against a fake Steam,
// with its own config folder and app folder.
func newPanel(t *testing.T) (*panel.Controller, *fakeCM, string) {
	t.Helper()
	isolateUserDirsFor(t)
	t.Setenv("XDG_RUNTIME_DIR", t.TempDir()) // its own idle lock
	accts, err := steamauth.DefaultAccounts()
	if err != nil {
		t.Fatal(err)
	}
	if err := accts.StoreFor(alice).Save(aliceSession()); err != nil {
		t.Fatal(err)
	}
	cm := newFakeCM(t, alice, 1)
	cm.owned = []fakeGame{{ID: 730, Name: "CS", Minutes: 600}, {ID: 440, Name: "TF2", Minutes: 60}}
	// The store answers nothing, so the library is Steam's owned-games list.
	noStore := httptest.NewServer(http.NotFoundHandler())
	t.Cleanup(noStore.Close)
	oldStore := freegames.StoreURL
	freegames.StoreURL = noStore.URL
	t.Cleanup(func() { freegames.StoreURL = oldStore })
	// Background checks (updates, Steam settings) never leave this machine
	// or use a real GitHub sign-in.
	t.Setenv("HOURIDLE_GITHUB_TOKEN", "test-token")
	if update.API == "https://api.github.com" {
		update.API = noStore.URL
		t.Cleanup(func() { update.API = "https://api.github.com" })
	}
	if strings.HasPrefix(steamver.ManifestURL, "https://") {
		oldManifest := steamver.ManifestURL
		steamver.ManifestURL = noStore.URL
		t.Cleanup(func() { steamver.ManifestURL = oldManifest })
	}
	appDir := t.TempDir()
	ctl, err := panel.New(panel.Options{AppDir: appDir, ExePath: "houridle", Version: "test", WebAddr: ":0"})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		ctl.Close()
		handover.LockSuffix = ""
	})
	return ctl, cm, appDir
}

func TestPanelIdlesAndSharesItsConnection(t *testing.T) {
	ctl, cm, appDir := newPanel(t)
	if err := ctl.Start(web.Settings{Mode: "games"}); err == nil {
		t.Error("starting with no games should be refused")
	}
	if err := ctl.Start(web.Settings{Mode: "games", Games: []uint32{730}, Status: "away"}); err != nil {
		t.Fatal(err)
	}
	if got := <-cm.gotGames; !slices.Equal(got, []uint64{730}) {
		t.Fatalf("Steam was told %v", got)
	}
	waitFor(t, "idling", func() bool {
		st := ctl.Status()
		return st.Panel.Idling && len(st.Views) == 1 && st.Views[0].Game.AppID == 730 && st.Run.Status == "away"
	})

	// While idling, the library comes through the idler's own connection:
	// a second sign-in would knock idling off.
	lib, err := ctl.Library(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(lib) != 2 || lib[0].AppID != 730 {
		t.Errorf("library, most played first: %+v", lib)
	}
	if n := len(cm.messages(5514)); n != 1 {
		t.Errorf("the library must use the idler's connection, but houridle signed in %d times", n)
	}

	// What it's doing is remembered, so a restart carries on.
	cfg, _ := config.Load(appDir)
	if cfg.Panel == nil || !cfg.Panel.Idling || cfg.Panel.Status != "away" || !slices.Equal(cfg.GamesFor(steamauth.FormatSteamID(alice), true), []uint32{730}) {
		t.Errorf("remembered: %+v %v", cfg.Panel, cfg.Games)
	}

	ctl.StopIdling()
	waitFor(t, "the games to be cleared", func() bool {
		select {
		case g := <-cm.gotGames:
			return len(g) == 0
		default:
			return false
		}
	})
	if ctl.Status().Panel.Idling {
		t.Error("should have stopped")
	}
	cfg, _ = config.Load(appDir)
	if cfg.Panel.Idling {
		t.Error("a stop from the page shouldn't resume after a restart")
	}

	// Not idling: it opens a connection of its own for the library.
	if _, err := ctl.Library(context.Background()); err != nil {
		t.Fatal(err)
	}
	if n := len(cm.messages(5514)); n != 2 {
		t.Errorf("expected a second sign-in for the library once idle, got %d", n)
	}
}

func TestPanelChecksSettings(t *testing.T) {
	ctl, _, _ := newPanel(t)
	for _, st := range []web.Settings{
		{Mode: "games", Games: []uint32{1}, Until: "soon"},
		{Mode: "games", Games: []uint32{1}, Rotate: "1m"},
		{Mode: "games", Games: []uint32{1}, Status: "sleepy"},
		{Mode: "cards", Until: "10"},
		{Mode: "dance"},
		{Mode: "games", Games: []uint32{1}, Stop: "whenever"},
	} {
		if err := ctl.Start(st); err == nil {
			t.Errorf("%+v should be refused", st)
		}
	}
	if ctl.Status().Panel.Idling {
		t.Error("nothing should be idling")
	}
}

func TestPanelPageControls(t *testing.T) {
	ctl, cm, _ := newPanel(t)
	cm.schema = achievementSchema()
	cm.statValues = map[uint32]uint32{1: 0}
	srv := &web.Server{Key: "k", Status: ctl.Status, Stop: ctl.StopIdling, Control: ctl}
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	call := func(method, path string, body any, header bool) (int, map[string]any, []byte) {
		t.Helper()
		var rd io.Reader
		if body != nil {
			b, _ := json.Marshal(body)
			rd = bytes.NewReader(b)
		}
		req, _ := http.NewRequest(method, ts.URL+path, rd)
		req.AddCookie(&http.Cookie{Name: "houridle_key", Value: "k"})
		if header {
			req.Header.Set("X-Houridle", "1")
		}
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		raw, _ := io.ReadAll(resp.Body)
		var out map[string]any
		json.Unmarshal(raw, &out)
		return resp.StatusCode, out, raw
	}

	// Changes need the page's own header, as Stop does.
	if code, _, _ := call("POST", "/api/idle/start", web.Settings{Mode: "games", Games: []uint32{440}}, false); code != http.StatusForbidden {
		t.Errorf("start without the header: HTTP %d", code)
	}
	if code, out, _ := call("POST", "/api/idle/start", web.Settings{Mode: "games"}, true); code != http.StatusConflict || !strings.Contains(out["error"].(string), "pick at least one game") {
		t.Errorf("a refused start: HTTP %d %v", code, out)
	}
	if code, _, _ := call("POST", "/api/idle/start", web.Settings{Mode: "games", Games: []uint32{440}}, true); code != 200 {
		t.Fatalf("start: HTTP %d", code)
	}
	<-cm.gotGames

	code, _, raw := call("GET", "/api/status", nil, false)
	var st struct {
		Panel struct {
			Idling   bool
			Source   string
			Accounts []struct{ Name string }
		} `json:"panel"`
	}
	json.Unmarshal(raw, &st)
	if code != 200 || !st.Panel.Idling || st.Panel.Source != steamauth.FormatSteamID(alice) || len(st.Panel.Accounts) != 1 || st.Panel.Accounts[0].Name != "alice" {
		t.Errorf("status: HTTP %d %s", code, raw)
	}

	code, _, raw = call("GET", "/api/library", nil, false)
	if code != 200 || !strings.Contains(string(raw), `"name":"TF2"`) {
		t.Errorf("library: HTTP %d %s", code, raw)
	}
	code, _, raw = call("GET", "/api/achievements?app=252490", nil, false)
	if code != 200 || !strings.Contains(string(raw), "ACH_FIRST") {
		t.Errorf("achievements: HTTP %d %s", code, raw)
	}
	code, out, _ := call("POST", "/api/achievements", map[string]any{"app": 252490, "unlock": []string{"ACH_FIRST"}}, true)
	if code != 200 || len(cm.messages(5466)) != 1 {
		t.Errorf("unlock: HTTP %d %v, stores %d", code, out, len(cm.messages(5466)))
	}
	if n := len(cm.messages(5514)); n != 1 {
		t.Errorf("everything should share the idler's connection, but houridle signed in %d times", n)
	}

	// A new key keeps this browser signed in and shuts old links out.
	req, _ := http.NewRequest("POST", ts.URL+"/api/webkey", nil)
	req.AddCookie(&http.Cookie{Name: "houridle_key", Value: "k"})
	req.Header.Set("X-Houridle", "1")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	var fresh *http.Cookie
	for _, c := range resp.Cookies() {
		fresh = c
	}
	if resp.StatusCode != 200 || fresh == nil || fresh.Value == "k" {
		t.Fatalf("new key: HTTP %d, cookie %+v", resp.StatusCode, fresh)
	}
	if code, _, _ := call("GET", "/api/status", nil, false); code != http.StatusUnauthorized {
		t.Errorf("the old key should stop working, got HTTP %d", code)
	}

	// A status page without a controller has no control endpoints.
	plain := httptest.NewServer((&web.Server{Key: "k", Status: ctl.Status, Stop: func() {}}).Handler())
	defer plain.Close()
	req, _ = http.NewRequest("POST", plain.URL+"/api/idle/start", strings.NewReader("{}"))
	req.AddCookie(&http.Cookie{Name: "houridle_key", Value: "k"})
	req.Header.Set("X-Houridle", "1")
	if resp, err := http.DefaultClient.Do(req); err == nil {
		resp.Body.Close()
		if resp.StatusCode != http.StatusNotFound && resp.StatusCode != http.StatusMethodNotAllowed {
			t.Errorf("start on a status-only page: HTTP %d", resp.StatusCode)
		}
	}
}

func TestQRCodeIsSVG(t *testing.T) {
	svg, err := web.QRSVG("https://s.team/q/1/123")
	if err != nil || !strings.HasPrefix(svg, "<svg") || !strings.Contains(svg, "<path") {
		t.Errorf("QR: %v %.60s", err, svg)
	}
}
