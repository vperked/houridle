package tests

import (
	"context"
	"encoding/binary"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"houridle/backend/cards"
	"houridle/backend/config"
	"houridle/backend/idler"
	"houridle/backend/notify"
	"houridle/backend/steam"
	"houridle/backend/steamauth"
	"houridle/backend/steamcm"
)

// ---------- status while idling ----------

func TestIdlingStatusIsConfigurable(t *testing.T) {
	for _, tc := range []struct {
		status string
		want   []uint64 // ChangeStatus values sent (none for offline)
	}{
		{"", []uint64{1}}, {"away", []uint64{3}}, {"busy", []uint64{2}}, {"invisible", []uint64{7}}, {"offline", nil},
	} {
		cm := newFakeCM(t, alice, 1)
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		s, err := steamcm.Connect(ctx, steamcm.Config{SteamID: alice, RefreshToken: fakeToken(alice, "client", farFuture), Status: tc.status})
		if err != nil {
			t.Fatal(err)
		}
		s.SetGames([]uint32{730})
		<-cm.gotGames
		var got []uint64
		for _, m := range cm.messages(716) {
			got = append(got, m.Body.Uint(1))
		}
		if !slices.Equal(got, tc.want) {
			t.Errorf("status %q: sent %v, want %v", tc.status, got, tc.want)
		}
		s.Close()
		cancel()
	}
	if _, err := steamcm.ParseStatus("sleepy"); err == nil {
		t.Error("an unknown status should be refused")
	}
	if got, _ := steamcm.ParseStatus("  AWAY "); got != "away" {
		t.Errorf("statuses are case-insensitive, got %q", got)
	}
}

// ---------- alerts ----------

func TestNotifyFormatsPerService(t *testing.T) {
	var mu sync.Mutex
	var bodies []string
	var types []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		mu.Lock()
		bodies = append(bodies, string(b))
		types = append(types, r.Header.Get("Content-Type"))
		mu.Unlock()
	}))
	defer srv.Close()

	text, err := notify.New(srv.URL+"/topic", "alice")
	if err != nil {
		t.Fatal(err)
	}
	if text.Kind() != "text" {
		t.Errorf("kind %q", text.Kind())
	}
	if err := text.Send(context.Background(), "sign-in refused"); err != nil {
		t.Fatal(err)
	}
	if bodies[0] != "houridle · alice: sign-in refused" || !strings.HasPrefix(types[0], "text/plain") {
		t.Errorf("plain webhook got %q (%s)", bodies[0], types[0])
	}

	// Repeats within a few hours are held back.
	text.Alert("same")
	text.Alert("same")
	text.Flush()
	mu.Lock()
	n := len(bodies)
	mu.Unlock()
	if n != 2 {
		t.Errorf("a repeated alert should be sent once, got %d posts", n-1)
	}

	for _, tc := range []struct{ url, kind, field string }{
		{"https://discord.com/api/webhooks/1/abc", "discord", "content"},
		{"https://hooks.slack.com/services/T/B/x", "slack", "text"},
	} {
		n, err := notify.New(tc.url, "")
		if err != nil || n.Kind() != tc.kind {
			t.Errorf("%s: kind %v, err %v", tc.url, n, err)
		}
	}
	for _, bad := range []string{"", "discord.com/api/webhooks/1", "ftp://x", "https://"} {
		if _, err := notify.New(bad, ""); err == nil {
			t.Errorf("%q should be refused", bad)
		}
	}
}

// captureTransport records requests instead of sending them.
type captureTransport struct{ reqs []*http.Request }

func (c *captureTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	c.reqs = append(c.reqs, r)
	return &http.Response{StatusCode: http.StatusNoContent, Body: io.NopCloser(strings.NewReader("")), Request: r}, nil
}

func TestNotifyJSONForDiscordAndSlack(t *testing.T) {
	for _, tc := range []struct{ url, field string }{
		{"https://discord.com/api/webhooks/1/abc", "content"},
		{"https://hooks.slack.com/services/T/B/x", "text"},
	} {
		n, err := notify.New(tc.url, "")
		if err != nil {
			t.Fatal(err)
		}
		capture := &captureTransport{}
		n.Client = &http.Client{Transport: capture}
		if err := n.Send(context.Background(), "refused"); err != nil {
			t.Fatal(err)
		}
		r := capture.reqs[0]
		var body map[string]string
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatalf("%s: body isn't JSON: %v", tc.url, err)
		}
		if body[tc.field] != "houridle · refused" || r.Header.Get("Content-Type") != "application/json" || r.URL.String() != tc.url {
			t.Errorf("%s: sent %v (%s) to %s", tc.url, body, r.Header.Get("Content-Type"), r.URL)
		}
	}
}

// ---------- card drops ----------

const badgePage = `<html><script>g_steamID = "76561197960266729";</script>
<div class="badge_row is_link"> <a class="badge_row_overlay" href="https://steamcommunity.com/id/x/gamecards/252490/"></a>
 <span class="progress_info_bold">1 card drop remaining</span>
 <div class="badge_title"> Rust &nbsp;<span class="badge_view_details">View details</span> </div></div>
<div class="badge_row is_link"> <a class="badge_row_overlay" href="https://steamcommunity.com/id/x/gamecards/440/"></a>
 <span class="progress_info_bold">3 card drops remaining</span>
 <div class="badge_title"> Team Fortress &amp; Friends &nbsp;<span>View details</span> </div></div>
<div class="badge_row is_link"> <a class="badge_row_overlay" href="https://steamcommunity.com/id/x/gamecards/570/"></a>
 <span class="progress_info_bold">No card drops remaining</span>
 <div class="badge_title"> Dota 2 &nbsp;</div></div>
<div class="badge_row is_link"> <a class="badge_row_overlay" href="https://steamcommunity.com/id/x/badges/2"></a>
 <span class="progress_info_bold"> 5 of 28 tasks completed </span>
 <div class="badge_title"> Pillar of Community &nbsp;</div></div>
PAGES</html>`

func TestCardDropsFromBadgePages(t *testing.T) {
	drops := cards.Parse(badgePage)
	want := []cards.Drop{{AppID: 252490, Name: "Rust", Remaining: 1}, {AppID: 440, Name: "Team Fortress & Friends", Remaining: 3}}
	if !slices.Equal(drops, want) {
		t.Fatalf("parsed %+v", drops)
	}

	var cookies []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		cookies = append(cookies, r.Header.Get("Cookie"))
		page := strings.Replace(badgePage, "PAGES", `<a class="pagelink" href="?p=2">2</a>`, 1)
		if r.URL.Query().Get("p") == "2" {
			page = `<script>g_steamID = "76561197960266729";</script>
<div class="badge_row is_link"><a href="/gamecards/730/"></a><span class="progress_info_bold">5 card drops remaining</span><div class="badge_title"> CS &nbsp;</div></div>`
		}
		io.WriteString(w, page)
	}))
	defer srv.Close()
	old := cards.BaseURL
	cards.BaseURL = srv.URL
	defer func() { cards.BaseURL = old }()

	all, err := cards.Remaining(context.Background(), alice, "tok")
	if err != nil {
		t.Fatal(err)
	}
	if len(all) != 3 || all[0].AppID != 730 || all[0].Remaining != 5 {
		t.Errorf("both pages, most drops first: %+v", all)
	}
	if len(cookies) != 2 || !strings.Contains(cookies[0], "steamLoginSecure=76561197960266729%7C%7Ctok") {
		t.Errorf("sign-in cookie: %q", cookies)
	}

	// A page for nobody (the token was refused) must not read as "no drops".
	srv.Config.Handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		io.WriteString(w, `<script>g_steamID = false;</script>`)
	})
	if _, err := cards.Remaining(context.Background(), alice, "tok"); !errors.Is(err, cards.ErrNotSignedIn) {
		t.Errorf("signed-out page: %v", err)
	}
}

// ---------- signed-in idling: targets, turns, cards ----------

func startSupervisor(t *testing.T, games []*steam.Game, opts idler.SessionOptions) (*idler.SessionSupervisor, *fakeCM, chan idler.Event, context.Context) {
	t.Helper()
	cm := newFakeCM(t, alice, 1)
	mgr := managerWith(t, nil, aliceSession())
	events := make(chan idler.Event, 64)
	sup := idler.NewSessionWith(games, mgr, opts, func(e idler.Event) {
		select {
		case events <- e:
		default:
		}
	})
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(func() {
		cancel()
		sup.Wait()
	})
	return sup, cm, events, ctx
}

func TestGamesAreDoneAtTheirTarget(t *testing.T) {
	oldEvery := idler.PlaytimeEvery
	idler.PlaytimeEvery = 50 * time.Millisecond
	defer func() { idler.PlaytimeEvery = oldEvery }()

	games := []*steam.Game{{AppID: 730, Name: "CS"}, {AppID: 440, Name: "App 440"}}
	sup, cm, _, ctx := startSupervisor(t, games, idler.SessionOptions{Target: 100 * time.Hour})
	cm.mu.Lock()
	cm.owned = []fakeGame{{ID: 730, Name: "CS", Minutes: 100 * 60}, {ID: 440, Name: "TF2", Minutes: 60}}
	cm.mu.Unlock()
	sup.Start(ctx)

	// CS already has 100 hours: only TF2 is idled.
	if got := <-cm.gotGames; !slices.Equal(got, []uint64{440}) {
		t.Fatalf("Steam was told %v", got)
	}
	waitFor(t, "views", func() bool { return sup.Views()[1].State == idler.StateIdling })
	v := sup.Views()
	if v[0].State != idler.StateDone || v[1].Total < time.Hour || v[1].Total > time.Hour+time.Second || v[1].Target != 100*time.Hour || v[1].Game.Name != "TF2" {
		t.Errorf("views: %+v %+v", v[0], v[1])
	}

	// TF2 reaches it too: nothing left, the supervisor finishes by itself.
	cm.mu.Lock()
	cm.owned[1].Minutes = 100 * 60
	cm.mu.Unlock()
	select {
	case <-sup.Finished():
	case <-time.After(5 * time.Second):
		t.Fatal("should finish once every game is done")
	}
}

func TestMoreThan32GamesTakeTurns(t *testing.T) {
	var games []*steam.Game
	for i := range 40 {
		games = append(games, &steam.Game{AppID: uint32(1000 + i), Name: "G"})
	}
	sup, cm, _, ctx := startSupervisor(t, games, idler.SessionOptions{Rotate: 200 * time.Millisecond})
	sup.Start(ctx)

	first := <-cm.gotGames
	if len(first) != idler.MaxGames || first[0] != 1000 {
		t.Fatalf("first turn: %d games starting at %v", len(first), first[0])
	}
	waitFor(t, "queued games", func() bool {
		queued := 0
		for _, v := range sup.Views() {
			if v.State == idler.StateQueued {
				queued++
			}
		}
		return queued == 8
	})
	// The next turn goes to the 8 that waited, plus 24 of the others.
	var second []uint64
	waitFor(t, "a rotation", func() bool {
		select {
		case second = <-cm.gotGames:
			return true
		default:
			return false
		}
	})
	for id := uint64(1032); id < 1040; id++ {
		if !slices.Contains(second, id) {
			t.Errorf("game %d waited and should play next turn: %v", id, second)
		}
	}
	if len(second) != idler.MaxGames {
		t.Errorf("each turn is %d games, got %d", idler.MaxGames, len(second))
	}
}

func TestCardsModeFollowsDrops(t *testing.T) {
	oldEvery, oldDrops := idler.CardsEvery, idler.CardDrops
	idler.CardsEvery = 50 * time.Millisecond
	var mu sync.Mutex
	left := []cards.Drop{{AppID: 252490, Name: "Rust", Remaining: 1}, {AppID: 440, Name: "TF2", Remaining: 2}}
	var tokens []string
	idler.CardDrops = func(_ context.Context, id uint64, token string) ([]cards.Drop, error) {
		mu.Lock()
		defer mu.Unlock()
		tokens = append(tokens, token)
		return slices.Clone(left), nil
	}
	defer func() { idler.CardsEvery, idler.CardDrops = oldEvery, oldDrops }()

	sup, cm, _, ctx := startSupervisor(t, nil, idler.SessionOptions{Cards: true})
	sup.Start(ctx)
	got := <-cm.gotGames
	slices.Sort(got)
	if !slices.Equal(got, []uint64{440, 252490}) {
		t.Fatalf("should idle the games with drops: %v", got)
	}
	mu.Lock()
	if tokens[0] != "web-access-token" {
		t.Errorf("badge pages should be read with an access token from the session, got %q", tokens[0])
	}
	left = left[1:] // Rust dropped its last card
	mu.Unlock()
	waitFor(t, "Rust done", func() bool {
		for _, v := range sup.Views() {
			if v.Game.AppID == 252490 {
				return v.State == idler.StateDone
			}
		}
		return false
	})
	mu.Lock()
	left = nil
	mu.Unlock()
	select {
	case <-sup.Finished():
	case <-time.After(5 * time.Second):
		t.Fatal("should finish when no drops are left")
	}
}

// ---------- achievements over the sign-in ----------

// kv builds binary KeyValues for tests.
type kv struct{ b []byte }

func (k *kv) open(key string) *kv { k.b = append(append(append(k.b, 0), key...), 0); return k }
func (k *kv) str(key, v string) *kv {
	k.b = append(append(append(append(append(k.b, 1), key...), 0), v...), 0)
	return k
}
func (k *kv) i32(key string, v int32) *kv {
	k.b = append(append(append(k.b, 2), key...), 0)
	k.b = binary.LittleEndian.AppendUint32(k.b, uint32(v))
	return k
}
func (k *kv) end() *kv { k.b = append(k.b, 8); return k }

func achievementSchema() []byte {
	s := new(kv).open("252490").str("gamename", "Rust").open("stats").
		open("1").str("type", "ACHIEVEMENTS").open("bits"). // Steam's current format
		open("0").str("name", "ACH_FIRST").i32("bit", 0).open("display").open("name").str("english", "First Steps").str("token", "#x").end().str("desc", "Do a thing").str("hidden", "0").end().end().
		open("1").str("name", "ACH_SECRET").open("display").str("name", "Secret").str("hidden", "1").end().end().          // bit from the key
		open("2").str("name", "ACH_SERVER").str("permission", "1").open("display").str("name", "Server Only").end().end(). // set by game servers only
		end().end().
		open("2").i32("type", 1).str("name", "kills").end().
		end().end().end()
	return s.b
}

func TestAchievementsOverTheSignIn(t *testing.T) {
	cm := newFakeCM(t, alice, 1)
	cm.schema = achievementSchema()
	cm.statValues = map[uint32]uint32{1: 0b10, 2: 57}
	s, err := connectAlice(t, cm)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	ctx := context.Background()
	st, err := s.UserStats(ctx, 252490)
	if err != nil {
		t.Fatal(err)
	}
	want := []steamcm.Achievement{
		{ID: "ACH_FIRST", Name: "First Steps", Description: "Do a thing"},
		{ID: "ACH_SECRET", Name: "Secret", Hidden: true, Unlocked: true},
	}
	if len(st.Achievements) != 3 || !st.Achievements[2].Protected || st.Achievements[0].Protected {
		t.Fatalf("got %+v", st.Achievements)
	}
	for i, a := range st.Achievements[:2] {
		w := want[i]
		if a.ID != w.ID || a.Name != w.Name || a.Description != w.Description || a.Hidden != w.Hidden || a.Unlocked != w.Unlocked {
			t.Errorf("achievement %d: %+v, want %+v", i, a, w)
		}
	}

	// A protected one (set by the game's servers) is left out, so Steam
	// doesn't refuse the whole save; the others still go through.
	failed, err := s.SetAchievements(ctx, st, []string{"ACH_FIRST", "ACH_SERVER"}, []string{"ACH_SECRET", "NOPE"})
	if err != nil {
		t.Fatal(err)
	}
	if len(failed) != 2 || failed["NOPE"] == "" || !strings.Contains(failed["ACH_SERVER"], "game's own servers") {
		t.Errorf("failed: %v", failed)
	}
	store := cm.messages(5466)
	if len(store) != 1 {
		t.Fatalf("expected one store, got %d", len(store))
	}
	b := store[0].Body
	stats := b.Messages(6)
	if b.Uint(1) != 252490 || b.Uint(2) != alice || b.Uint(3) != alice || b.Uint(4) != 1234 || len(stats) != 1 || stats[0].Uint(1) != 1 || stats[0].Uint(2) != 0b01 {
		t.Errorf("store message: game %d crc %d stats %d", b.Uint(1), b.Uint(4), len(stats))
	}
	if !st.Achievements[0].Unlocked || st.Achievements[1].Unlocked {
		t.Error("the in-memory list should show the change")
	}

	// A stat the game guards on its servers comes back reverted.
	cm.revert = []uint32{1}
	failed, _ = s.SetAchievements(ctx, st, nil, []string{"ACH_FIRST"})
	if !strings.Contains(failed["ACH_FIRST"], "reverted") {
		t.Errorf("reverted stat: %v", failed)
	}

	cm.statsResult = 15
	if _, err := s.UserStats(ctx, 1); err == nil || !strings.Contains(err.Error(), "owned") {
		t.Errorf("denied stats: %v", err)
	}
}

// ---------- several accounts ----------

func TestSeveralAccounts(t *testing.T) {
	dir := t.TempDir()
	accts := &steamauth.Accounts{Dir: dir}
	if _, err := accts.Find(""); !errors.Is(err, steamauth.ErrNotLoggedIn) {
		t.Fatalf("nothing saved: %v", err)
	}
	save := func(name string, id uint64) {
		t.Helper()
		store := accts.StoreFor(id)
		if err := store.Save(steamauth.Session{AccountName: name, SteamID: id, RefreshToken: fakeToken(id, "client", farFuture)}); err != nil {
			t.Fatal(err)
		}
	}
	save("alice", alice)
	if p := accts.StoreFor(alice).Path; filepath.Base(p) != "session.dat" {
		t.Errorf("the first account is the default, in session.dat: %s", p)
	}
	save("bob", bob)
	if p := accts.StoreFor(bob).Path; !strings.Contains(p, filepath.Join("accounts", "")) {
		t.Errorf("another account gets its own file: %s", p)
	}
	all := accts.List()
	if len(all) != 2 || !all[0].Default || all[0].AccountName != "alice" || all[1].AccountName != "bob" {
		t.Fatalf("list: %+v", all)
	}
	if acc, err := accts.Find("BOB"); err != nil || acc.SteamID != bob {
		t.Errorf("find by name: %v %v", acc.SteamID, err)
	}
	if acc, err := accts.Find(steamauth.FormatSteamID(bob)); err != nil || acc.AccountName != "bob" {
		t.Errorf("find by SteamID: %v", err)
	}
	if _, err := accts.Find("carol"); err == nil || !strings.Contains(err.Error(), "alice, bob") {
		t.Errorf("unknown account should list the saved ones: %v", err)
	}

	if _, err := accts.MakeDefault("bob"); err != nil {
		t.Fatal(err)
	}
	all = accts.List()
	if len(all) != 2 || all[0].AccountName != "bob" || !all[0].Default || all[1].AccountName != "alice" {
		t.Errorf("after switching the default: %+v", all)
	}
	if _, err := os.Stat(filepath.Join(dir, "session.dat.swap")); err == nil {
		t.Error("no temp file should be left behind")
	}
}

func TestGameListsPerAccount(t *testing.T) {
	var cfg config.Config
	cfg.SetGamesFor("", true, []uint32{1})
	cfg.SetGamesFor("111", true, []uint32{2}) // the default account shares the main list
	cfg.SetGamesFor("222", false, []uint32{3})
	if !slices.Equal(cfg.Games, []uint32{2}) || !slices.Equal(cfg.GamesFor("222", false), []uint32{3}) {
		t.Errorf("config: %+v", cfg)
	}
	if got := cfg.GamesFor("333", false); got != nil {
		t.Errorf("an account without a list must not borrow another's: %v", got)
	}
	if got := cfg.GamesFor("111", true); !slices.Equal(got, []uint32{2}) {
		t.Errorf("default account: %v", got)
	}
}

func TestCardsModeKeepsTryingAfterAFailedFirstRead(t *testing.T) {
	oldRetry, oldDrops := idler.CardsRetryEvery, idler.CardDrops
	idler.CardsRetryEvery = 50 * time.Millisecond
	var mu sync.Mutex
	calls := 0
	idler.CardDrops = func(context.Context, uint64, string) ([]cards.Drop, error) {
		mu.Lock()
		defer mu.Unlock()
		calls++
		if calls == 1 {
			return nil, errors.New("Steam Community answered HTTP 502")
		}
		return []cards.Drop{{AppID: 440, Name: "TF2", Remaining: 1}}, nil
	}
	defer func() { idler.CardsRetryEvery, idler.CardDrops = oldRetry, oldDrops }()

	sup, cm, _, ctx := startSupervisor(t, nil, idler.SessionOptions{Cards: true})
	sup.Start(ctx)
	// Not knowing the drops yet is not "no drops left": it must not finish,
	// and once the pages load it idles the game.
	waitFor(t, "TF2 to be idled", func() bool {
		select {
		case g := <-cm.gotGames:
			return slices.Equal(g, []uint64{440})
		case <-sup.Finished():
			t.Fatal("finished after a failed first read, as if every card had dropped")
		default:
		}
		return false
	})
}

func TestRevertedStatKeepsSteamsValue(t *testing.T) {
	cm := newFakeCM(t, alice, 1)
	cm.schema = achievementSchema()
	cm.statValues = map[uint32]uint32{1: 0}
	cm.revert = []uint32{1}
	s, err := connectAlice(t, cm)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	st, err := s.UserStats(context.Background(), 252490)
	if err != nil {
		t.Fatal(err)
	}
	failed, err := s.SetAchievements(context.Background(), st, []string{"ACH_FIRST", "ACH_SECRET"}, nil)
	if err != nil || len(failed) != 2 {
		t.Fatalf("failed %v, err %v", failed, err)
	}
	// Steam kept stat 1 at 0, so a later change in the same stat must not
	// re-send the refused bits.
	cm.revert = nil
	if _, err := s.SetAchievements(context.Background(), st, []string{"ACH_FIRST"}, nil); err != nil {
		t.Fatal(err)
	}
	last := cm.messages(5466)
	if got := last[len(last)-1].Body.Messages(6)[0].Uint(2); got != 0b01 {
		t.Errorf("sent stat value %b, want only ACH_FIRST's bit", got)
	}
}
