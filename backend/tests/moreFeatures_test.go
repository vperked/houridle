package tests

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"houridle/backend/client/steam"
	"houridle/backend/features/freegames"
	"houridle/backend/features/history"
	"houridle/backend/online/steamcm"
	"houridle/backend/run/config"
	"houridle/backend/run/idler"
	"houridle/backend/views/web"
)

// ---------- hours history ----------

func TestHistoryAddsUpAcrossRuns(t *testing.T) {
	path := filepath.Join(t.TempDir(), "history.json")
	day := time.Date(2026, 11, 4, 12, 0, 0, 0, time.Local)

	h, err := history.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	// A run: Record sees running totals and only adds what's new.
	h.Record(day, []history.Sample{{AppID: 252490, Name: "Rust", Idled: 30 * time.Minute}})
	h.Record(day, []history.Sample{{AppID: 252490, Name: "Rust", Idled: time.Hour}, {AppID: 440, Name: "TF2", Idled: time.Hour}})
	if err := h.Save(); err != nil {
		t.Fatal(err)
	}

	// The next day, a new run (a new process starts counting from zero).
	h2, err := history.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	h2.Record(day.AddDate(0, 0, 1), []history.Sample{{AppID: 252490, Name: "Rust", Idled: 2 * time.Hour}})
	s := h2.Summarize(day.AddDate(0, 0, 1), 3)
	if s.Today != 2*time.Hour || s.Week != 4*time.Hour || s.All != 4*time.Hour {
		t.Errorf("today %v, week %v, all %v", s.Today, s.Week, s.All)
	}
	if len(s.Days) != 3 || s.Days[1].Time != 2*time.Hour || s.Days[2].Time != 2*time.Hour || s.Days[0].Time != 0 {
		t.Errorf("days: %+v", s.Days)
	}
	if len(s.Games) != 2 || s.Games[0].Name != "Rust" || s.Games[0].Time != 3*time.Hour || s.Games[1].Time != time.Hour {
		t.Errorf("games: %+v", s.Games)
	}
	if !s.Since.Equal(time.Date(2026, 11, 4, 0, 0, 0, 0, time.Local)) {
		t.Errorf("since %v", s.Since)
	}

	// A month later, those days are out of "7 days" but still in all time.
	later := h2.Summarize(day.AddDate(0, 1, 0), 1)
	if later.Week != 0 || later.All != 4*time.Hour {
		t.Errorf("a month later: week %v, all %v", later.Week, later.All)
	}
}

func TestDamagedHistoryIsSetAside(t *testing.T) {
	path := filepath.Join(t.TempDir(), "history.json")
	os.WriteFile(path, []byte("{not json"), 0o600)
	h, err := history.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	if s := h.Summarize(time.Now(), 1); s.All != 0 {
		t.Error("a damaged file should start fresh")
	}
	if _, err := os.Stat(path + ".bad"); err != nil {
		t.Error("the damaged file should be kept as history.json.bad")
	}
}

// ---------- status page ----------

func newStatusServer(t *testing.T) (*httptest.Server, *web.Server, chan struct{}) {
	t.Helper()
	stopped := make(chan struct{}, 1)
	srv := &web.Server{
		Key:  "secret-key",
		Stop: func() { stopped <- struct{}{} },
		Status: func() web.Status {
			return web.Status{
				Version: "1.6.0", Account: "alice", Started: time.Now(),
				Views:  []idler.View{{Game: &steam.Game{AppID: 252490, Name: `<img src=x onerror=alert(1)>`}, State: idler.StateIdling, Idled: time.Hour, Total: 6 * time.Hour, Target: 10 * time.Hour, Drops: 1}},
				Events: []idler.Event{{At: time.Now(), Level: idler.EventGood, Text: "Signed in"}},
				Stats: &history.Summary{Today: 2 * time.Hour, All: 9 * time.Hour, Since: time.Date(2026, 11, 1, 0, 0, 0, 0, time.Local),
					Days:      []history.DayTotal{{Day: time.Date(2026, 11, 4, 0, 0, 0, 0, time.Local), Time: 2 * time.Hour}},
					Games:     []history.GameTotal{{AppID: 252490, Name: "Rust", Time: 9 * time.Hour}},
					WeekGames: []history.GameTotal{{AppID: 252490, Name: "Rust", Time: 2 * time.Hour}}},
				Run: web.Run{SignedIn: true, Cards: true, Claim: true, Status: "away"},
			}
		},
	}
	ts := httptest.NewServer(srv.Handler())
	t.Cleanup(ts.Close)
	return ts, srv, stopped
}

func TestStatusPageNeedsItsKey(t *testing.T) {
	ts, _, stopped := newStatusServer(t)
	noRedirect := &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}

	for _, path := range []string{"/", "/api/status", "/?key=wrong"} {
		resp, err := http.Get(ts.URL + path)
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		if resp.StatusCode != http.StatusUnauthorized {
			t.Errorf("%s without the key: HTTP %d", path, resp.StatusCode)
		}
	}
	resp, _ := http.Post(ts.URL+"/api/stop", "", nil)
	resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("stop without the key: HTTP %d", resp.StatusCode)
	}

	// The link sets a cookie and redirects to a clean address.
	resp, err := noRedirect.Get(ts.URL + "/?key=secret-key")
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	var cookie *http.Cookie
	for _, c := range resp.Cookies() {
		cookie = c
	}
	if resp.StatusCode != http.StatusSeeOther || resp.Header.Get("Location") != "/" || cookie == nil || !cookie.HttpOnly || cookie.SameSite != http.SameSiteStrictMode {
		t.Fatalf("key link: HTTP %d to %q, cookie %+v", resp.StatusCode, resp.Header.Get("Location"), cookie)
	}

	get := func(path string) *http.Response {
		req, _ := http.NewRequest(http.MethodGet, ts.URL+path, nil)
		req.AddCookie(cookie)
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		return resp
	}
	page := get("/")
	body, _ := io.ReadAll(page.Body)
	page.Body.Close()
	if page.StatusCode != 200 || !strings.Contains(string(body), "houridle") || page.Header.Get("X-Frame-Options") != "DENY" {
		t.Errorf("page: HTTP %d, framing %q", page.StatusCode, page.Header.Get("X-Frame-Options"))
	}
	// The React page runs only its own files: no inline code is allowed.
	if csp := page.Header.Get("Content-Security-Policy"); strings.Contains(csp, "unsafe-inline") || !strings.Contains(csp, "script-src 'self'") || !strings.Contains(csp, "img-src 'self' data: https://*.steamstatic.com") {
		t.Errorf("CSP: %q", csp)
	}
	// Its script and stylesheet load without the key (they hold no data).
	assets := regexp.MustCompile(`(?:src|href)="(/assets/[^"]+)"`).FindAllStringSubmatch(string(body), -1)
	if len(assets) < 2 {
		t.Fatalf("the page should load its built script and stylesheet: %q", body)
	}
	for _, a := range assets {
		resp, err := http.Get(ts.URL + a[1])
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		if resp.StatusCode != 200 || !strings.Contains(resp.Header.Get("Cache-Control"), "immutable") {
			t.Errorf("%s: HTTP %d, cache %q", a[1], resp.StatusCode, resp.Header.Get("Cache-Control"))
		}
	}

	st := get("/api/status")
	var data struct {
		Account string `json:"account"`
		Games   []struct {
			Name                 string
			State                string
			Idled, Total, Target int64
			Drops                int
		} `json:"games"`
		Stats struct {
			Today, All int64
			Since      string
			Days       []struct {
				Day  string
				Time int64
			}
			Games []struct {
				AppID     uint32 `json:"appId"`
				Week, All int64
			}
		} `json:"stats"`
		Run struct {
			SignedIn, Cards, Claim bool
			Status                 string
		} `json:"run"`
	}
	json.NewDecoder(st.Body).Decode(&data)
	st.Body.Close()
	g := data.Games[0]
	if data.Account != "alice" || g.State != "idling" || g.Idled != 3600 || g.Total != 6*3600 || g.Target != 10*3600 || g.Drops != 1 || data.Stats.Today != 7200 {
		t.Errorf("status: %+v", data)
	}
	if st := data.Stats; st.Since != "2026-11-01" || len(st.Days) != 1 || st.Days[0].Day != "2026-11-04" || st.Days[0].Time != 7200 || len(st.Games) != 1 || st.Games[0].Week != 7200 || st.Games[0].All != 9*3600 {
		t.Errorf("history for the History tab: %+v", st)
	}
	if r := data.Run; !r.SignedIn || !r.Cards || !r.Claim || r.Status != "away" {
		t.Errorf("run settings: %+v", r)
	}

	// Stop needs the cookie and the page's own header (another site can't send it).
	stop := func(header bool) int {
		req, _ := http.NewRequest(http.MethodPost, ts.URL+"/api/stop", nil)
		req.AddCookie(cookie)
		if header {
			req.Header.Set("X-Houridle", "stop")
		}
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		return resp.StatusCode
	}
	if code := stop(false); code != http.StatusForbidden {
		t.Errorf("stop without the header: HTTP %d", code)
	}
	if code := stop(true); code != http.StatusAccepted {
		t.Errorf("stop: HTTP %d", code)
	}
	select {
	case <-stopped:
	case <-time.After(5 * time.Second):
		t.Fatal("Stop wasn't called")
	}
}

func TestStatusPageAddresses(t *testing.T) {
	for in, want := range map[string]string{"8080": ":8080", ":9000": ":9000", "127.0.0.1:8080": "127.0.0.1:8080"} {
		if got, err := web.ParseAddr(in); err != nil || got != want {
			t.Errorf("ParseAddr(%q) = %q, %v", in, got, err)
		}
	}
	for _, bad := range []string{"", "abc", ":0", ":70000", "host"} {
		if _, err := web.ParseAddr(bad); err == nil {
			t.Errorf("ParseAddr(%q) should fail", bad)
		}
	}
	if links := web.Links("127.0.0.1:8080", "k"); len(links) != 1 || links[0] != "http://127.0.0.1:8080/?key=k" {
		t.Errorf("links: %v", links)
	}
	if k1, k2 := web.NewKey(), web.NewKey(); len(k1) < 32 || k1 == k2 {
		t.Errorf("keys should be long and random: %q %q", k1, k2)
	}
}

// ---------- idle everything you own ----------

func TestAllIdlesOwnedGamesUnderTheTarget(t *testing.T) {
	oldEvery := idler.PlaytimeEvery
	idler.PlaytimeEvery = 50 * time.Millisecond
	defer func() { idler.PlaytimeEvery = oldEvery }()

	oldLib := idler.OwnedLibrary
	idler.OwnedLibrary = func(ctx context.Context, cs *steamcm.Session, _ uint64) ([]steamcm.OwnedGame, error) {
		return cs.OwnedGames(ctx)
	}
	t.Cleanup(func() { idler.OwnedLibrary = oldLib }) // after the supervisor stops
	sup, cm, _, ctx := startSupervisor(t, nil, idler.SessionOptions{All: true, Target: 10 * time.Hour})
	cm.mu.Lock()
	cm.owned = []fakeGame{{ID: 730, Name: "CS", Minutes: 20 * 60}, {ID: 440, Name: "TF2", Minutes: 60}}
	cm.mu.Unlock()
	sup.Start(ctx)
	if got := <-cm.gotGames; !slices.Equal(got, []uint64{440}) {
		t.Fatalf("only the game under 10 hours: %v", got)
	}

	// A new purchase joins at the next refresh.
	cm.mu.Lock()
	cm.owned = append(cm.owned, fakeGame{ID: 570, Name: "Dota", Minutes: 0})
	cm.mu.Unlock()
	waitFor(t, "the new game", func() bool {
		select {
		case got := <-cm.gotGames:
			slices.Sort(got)
			return slices.Equal(got, []uint64{440, 570})
		default:
			return false
		}
	})
}

func TestAllDoesNotFinishBeforeReadingTheLibrary(t *testing.T) {
	oldLib := idler.OwnedLibrary
	idler.OwnedLibrary = func(ctx context.Context, cs *steamcm.Session, _ uint64) ([]steamcm.OwnedGame, error) {
		return cs.OwnedGames(ctx)
	}
	t.Cleanup(func() { idler.OwnedLibrary = oldLib }) // after the supervisor stops
	sup, cm, _, ctx := startSupervisor(t, nil, idler.SessionOptions{All: true})
	cm.mu.Lock()
	cm.callErr = 20 // Steam unavailable: the owned-games call fails
	cm.mu.Unlock()
	sup.Start(ctx)
	select {
	case <-sup.Finished():
		t.Fatal("a failed library read isn't \"no games\"")
	case <-time.After(300 * time.Millisecond):
	}
}

// ---------- free games ----------

func fakeStore(t *testing.T, cm *fakeCM) (claimed *[]url.Values) {
	t.Helper()
	var mu sync.Mutex
	var posts []url.Values
	row := func(key string, discount int, price int, title string, review ...string) string {
		rv := ""
		if len(review) > 0 {
			rv = fmt.Sprintf(`<span class="search_review_summary positive" data-tooltip-html="%s">`, review[0])
		}
		return fmt.Sprintf(`<a href="x" data-ds-itemkey="%s" class="search_result_row ds_collapse_flag"><span class="title">%s</span>%s<div class="discount_block" data-price-final="%d" data-discount="%d"></div></a>`, key, title, rv, price, discount)
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/search/results/" && r.URL.Query().Get("category2") == "22,29":
			html := row("App_600", 0, 0, "Cards F2P", "Very Positive&lt;br&gt;90% of the 3,000 user reviews for this game are positive.")
			json.NewEncoder(w).Encode(map[string]any{"success": 1, "results_html": html, "total_count": 1})
		case r.URL.Path == "/search/results/" && r.URL.Query().Get("sort_by") == "Reviews_DESC":
			html := row("App_700", 0, 0, "Great F2P", "Overwhelmingly Positive&lt;br&gt;97% of the 12,345 user reviews for this game are positive.") +
				row("App_800", 0, 0, "Good F2P", "Very Positive&lt;br&gt;88% of the 999 user reviews for this game are positive.") +
				row("App_900", 0, 0, "Played F2P", "Positive&lt;br&gt;80% of the 50 user reviews for this game are positive.")
			json.NewEncoder(w).Encode(map[string]any{"success": 1, "results_html": html, "total_count": 3})
		case r.URL.Path == "/search/results/":
			html := row("App_100", 100, 0, "Free Game", "Mixed&lt;br&gt;60% of the 200 user reviews for this game are positive.") + row("App_200", 100, 0, "Free DLC") + row("App_300", 0, 0, "Free To Play") + row("App_400", 50, 499, "Half Off") + row("App_500", 100, 0, "Owned Game", "Very Positive&lt;br&gt;90% of the 1,500 user reviews for this game are positive.")
			json.NewEncoder(w).Encode(map[string]any{"success": 1, "results_html": html, "total_count": 5})
		case r.URL.Path == "/api/appdetails":
			id := r.URL.Query().Get("appids")
			details := map[string]any{
				"100": map[string]any{"type": "game", "name": "Free Game", "package_groups": []any{map[string]any{"subs": []any{map[string]any{"packageid": 1001, "is_free_license": true, "price_in_cents_with_discount": 0}}}}},
				"200": map[string]any{"type": "dlc", "name": "Free DLC", "fullgame": map[string]any{"appid": "999", "name": "Base Game"}, "package_groups": []any{map[string]any{"subs": []any{map[string]any{"packageid": 2001, "is_free_license": true, "price_in_cents_with_discount": 0}}}}},
				"500": map[string]any{"type": "game", "name": "Owned Game", "package_groups": []any{map[string]any{"subs": []any{map[string]any{"packageid": 5001, "is_free_license": true, "price_in_cents_with_discount": 0}}}}},
			}
			json.NewEncoder(w).Encode(map[string]any{id: map[string]any{"success": true, "data": details[id]}})
		case r.URL.Path == "/dynamicstore/userdata/":
			if c, _ := r.Cookie("steamLoginSecure"); c == nil {
				w.WriteHeader(http.StatusUnauthorized)
				return
			}
			json.NewEncoder(w).Encode(map[string]any{"rgOwnedApps": []int{730, 4242, 4343}})
		case strings.HasPrefix(r.URL.Path, "/checkout/addfreelicense/"):
			r.ParseForm()
			sid, _ := r.Cookie("sessionid")
			login, _ := r.Cookie("steamLoginSecure")
			if sid == nil || login == nil || r.PostForm.Get("sessionid") != sid.Value {
				w.WriteHeader(http.StatusForbidden)
				return
			}
			mu.Lock()
			posts = append(posts, r.PostForm)
			mu.Unlock()
			var sub uint32
			fmt.Sscan(strings.TrimPrefix(r.URL.Path, "/checkout/addfreelicense/"), &sub)
			go cm.grant(sub)
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)
	old := freegames.StoreURL
	freegames.StoreURL = srv.URL
	t.Cleanup(func() { freegames.StoreURL = old })
	return &posts
}

func TestClaimFreeGames(t *testing.T) {
	cm := newFakeCM(t, alice, 1)
	cm.licenses = []uint32{5001} // already owns "Owned Game"
	posts := fakeStore(t, cm)

	offers, err := freegames.Find(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	// Only 100%-off giveaways count, best reviewed first.
	if len(offers) != 3 || offers[0].Name != "Owned Game" || offers[1].Name != "Free Game" || !offers[2].DLC || offers[2].BaseAppID != 999 {
		t.Fatalf("offers: %+v", offers)
	}
	if r := offers[0].Review; r.Summary != "Very Positive" || r.Positive != 90 || r.Count != 1500 {
		t.Errorf("review: %+v", r)
	}

	s, err := connectAlice(t, cm)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	dry, err := freegames.ClaimAll(context.Background(), s, alice, true)
	if err != nil {
		t.Fatal(err)
	}
	outcomes := map[string]freegames.Outcome{}
	for _, r := range dry {
		outcomes[r.Offer.Name] = r.Outcome
	}
	if outcomes["Free Game"] != freegames.WouldAdd || outcomes["Free DLC"] != freegames.NeedsBase || outcomes["Owned Game"] != freegames.Owned || len(*posts) != 0 {
		t.Fatalf("dry run: %v, posts %d", outcomes, len(*posts))
	}

	results, err := freegames.ClaimAll(context.Background(), s, alice, false)
	if err != nil {
		t.Fatal(err)
	}
	for _, r := range results {
		if r.Offer.Name == "Free Game" && r.Outcome != freegames.Added {
			t.Errorf("Free Game: outcome %v, err %v (Steam confirmed the license)", r.Outcome, r.Err)
		}
	}
	if len(*posts) != 1 {
		t.Errorf("exactly one claim should be sent, got %d", len(*posts))
	}
}

func TestTopFreeToPlay(t *testing.T) {
	cm := newFakeCM(t, alice, 1)
	fakeStore(t, cm)
	top, err := freegames.TopFreeToPlay(context.Background(), 2, freegames.Features{})
	if err != nil {
		t.Fatal(err)
	}
	if len(top) != 2 || top[0].Name != "Great F2P" || top[0].Review.Positive != 97 || top[0].Review.Count != 12345 || top[1].AppID != 800 {
		t.Fatalf("top: %+v", top)
	}
	// With achievements and trading cards asked for, the store filters for both.
	both, err := freegames.TopFreeToPlay(context.Background(), 5, freegames.Features{Achievements: true, Cards: true})
	if err != nil || len(both) != 1 || both[0].AppID != 600 {
		t.Fatalf("filtered: %+v %v", both, err)
	}
	s, err := connectAlice(t, cm)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	granted, err := s.AddFreeGames(context.Background(), []uint32{700, 800})
	if err != nil || !slices.Equal(granted, []uint32{700, 800}) {
		t.Fatalf("granted %v, %v", granted, err)
	}
	// Adding them again grants nothing: they're already on the account.
	if again, err := s.AddFreeGames(context.Background(), []uint32{700}); err != nil || len(again) != 0 {
		t.Errorf("again: %v, %v", again, err)
	}
}

func TestLibraryIncludesFreeGamesNeverPlayed(t *testing.T) {
	cm := newFakeCM(t, alice, 1)
	cm.owned = []fakeGame{{ID: 730, Name: "CS", Minutes: 600}}
	fakeStore(t, cm)
	s, err := connectAlice(t, cm)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	lib, err := freegames.Library(context.Background(), s, alice)
	if err != nil {
		t.Fatal(err)
	}
	// 4242 is a free game added but never played (Steam's owned-games list
	// leaves it out); 4343 is DLC, which isn't a game to idle.
	if len(lib) != 2 || lib[0].AppID != 730 || lib[0].Minutes != 600 || lib[1].AppID != 4242 || lib[1].Name != "Free Game Never Played" {
		t.Errorf("library: %+v", lib)
	}
}

// ---------- config holds secrets ----------

func TestConfigIsPrivate(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")
	os.WriteFile(path, []byte(`{"games":[1]}`), 0o644) // an older, readable file
	if err := config.Save(dir, config.Config{Games: []uint32{1}, WebKey: "secret"}); err != nil {
		t.Fatal(err)
	}
	info, _ := os.Stat(path)
	if mode := info.Mode().Perm(); mode != 0o600 && os.PathSeparator == '/' {
		t.Errorf("config.json holds the page key; mode %o, want 600", mode)
	}
}
