package tests

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"houridle/backend/features/cards"
	"houridle/backend/features/inventory"
	"houridle/backend/online/steamauth"
	"houridle/backend/run/config"
	"houridle/backend/views/panel"
	"houridle/backend/views/web"
)

func waitUntil(t *testing.T, what string, ok func() bool) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for !ok() {
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %s", what)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

func hasEvent(ctl *panel.Controller, text string) bool {
	for _, e := range ctl.Status().Events {
		if strings.Contains(e.Text, text) {
			return true
		}
	}
	return false
}

func fastTicks(t *testing.T) {
	old := panel.TickEvery
	panel.TickEvery = 30 * time.Millisecond
	t.Cleanup(func() { panel.TickEvery = old })
}

func TestSteamLevel(t *testing.T) {
	for level, xp := range map[int]int{0: 0, 1: 100, 2: 200, 10: 1000, 11: 1200, 20: 3000, 21: 3300} {
		if got := cards.XPForLevel(level); got != xp {
			t.Errorf("level %d starts at %d XP, not %d", level, xp, got)
		}
	}
	page := `<div class="profile_xp_block_level">Level <span class="friendPlayerLevel lvl_10"><span class="friendPlayerLevelNum">12</span></span></div><div class="profile_xp_block_xp">XP 1,640</div>`
	if l, ok := cards.ParseLevel(page); !ok || l.Level != 12 || l.XP != 1640 || l.LevelXP != 1400 || l.NextXP != 1600 {
		t.Errorf("level: %+v %v", l, ok)
	}

	ctl, _, _ := newPanel(t)
	newFakeMarket(t)
	lvl, err := ctl.Level(context.Background())
	if err != nil || lvl.Partial != 1 || lvl.Missing != 2 || lvl.Ready != 0 {
		t.Fatalf("level: %+v %v", lvl, err)
	}
}

func TestScheduleWindows(t *testing.T) {
	monday := &config.Schedule{On: true, Start: "22:00", End: "08:00", Days: [7]bool{true}}
	at := func(day, hour int) time.Time { return time.Date(2026, 11, 2+day, hour, 0, 0, 0, time.Local) } // Nov 2 2026 is a Monday
	for _, c := range []struct {
		t    time.Time
		want bool
	}{{at(0, 23), true}, {at(1, 7), true}, {at(0, 21), false}, {at(1, 9), false}, {at(1, 23), false}} {
		if got := panel.InSchedule(monday, c.t); got != c.want {
			t.Errorf("%s: %v, want %v", c.t.Format("Mon 15:04"), got, c.want)
		}
	}
	if !panel.InSchedule(nil, time.Now()) || !panel.InSchedule(&config.Schedule{On: false}, time.Now()) {
		t.Error("no schedule means any time")
	}
}

func TestSchedulePausesAndResumes(t *testing.T) {
	fastTicks(t)
	ctl, cm, appDir := newPanel(t)
	ctl.Resume()
	if err := ctl.Start(web.Settings{Mode: "games", Games: []uint32{440}}); err != nil {
		t.Fatal(err)
	}
	<-cm.gotGames
	every := [7]bool{true, true, true, true, true, true, true}
	now := time.Now()
	hhmm := func(d time.Duration) string { return now.Add(d).Format("15:04") }

	if err := ctl.SetSchedule(web.ScheduleInfo{On: true, Start: hhmm(2 * time.Hour), End: hhmm(3 * time.Hour), Days: every}); err != nil {
		t.Fatal(err)
	}
	waitUntil(t, "a pause", func() bool { return !ctl.Status().Panel.Idling })
	p := ctl.Status().Panel
	if !p.Schedule.Paused || p.Schedule.Active || p.Schedule.Next.IsZero() || !hasEvent(ctl, "Paused by your schedule") {
		t.Errorf("paused: %+v", p.Schedule)
	}
	if cfg, _ := config.Load(appDir); !cfg.Panel.Idling {
		t.Error("a pause must still resume later")
	}

	if err := ctl.SetSchedule(web.ScheduleInfo{On: true, Start: hhmm(-time.Hour), End: hhmm(time.Hour), Days: every}); err != nil {
		t.Fatal(err)
	}
	waitUntil(t, "a resume", func() bool { return ctl.Status().Panel.Idling && hasEvent(ctl, "Started by your schedule") })
	if err := ctl.SetSchedule(web.ScheduleInfo{On: true, Start: "25:00", End: "08:00", Days: every}); err == nil {
		t.Error("a bad time should be refused")
	}
	if err := ctl.SetSchedule(web.ScheduleInfo{On: true, Start: "22:00", End: "08:00"}); err == nil {
		t.Error("no days should be refused")
	}
}

func TestGradualAchievements(t *testing.T) {
	fastTicks(t)
	ctl, cm, appDir := newPanel(t)
	cm.schema = achievementSchema()
	cm.statValues = map[uint32]uint32{1: 0} // both normal achievements locked
	global := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		io.WriteString(w, `{"achievementpercentages":{"achievements":[{"name":"ACH_FIRST","percent":"10.5"},{"name":"ACH_SECRET","percent":80.1},{"name":"ACH_SERVER","percent":"50"}]}}`)
	}))
	defer global.Close()
	old := panel.GlobalAchievementsURL
	panel.GlobalAchievementsURL = global.URL
	defer func() { panel.GlobalAchievementsURL = old }()

	if err := ctl.StartDrip(context.Background(), []uint32{252490}, 30); err != nil {
		t.Fatal(err)
	}
	p := ctl.Status().Panel
	if len(p.Drip) != 1 || p.Drip[0].Left != 2 || p.Drip[0].Every != 30 || time.Until(p.Drip[0].Next) < 4*time.Minute {
		t.Fatalf("drip: %+v", p.Drip)
	}
	ctl.Resume()
	due := func() {
		cfg, _ := config.Load(appDir)
		for i := range cfg.Drip {
			cfg.Drip[i].Next = time.Now().Add(-time.Second)
		}
		config.Save(appDir, cfg)
	}
	due()
	// The most common achievement goes first.
	waitUntil(t, "the first unlock", func() bool { return hasEvent(ctl, "Unlocked “Secret”") })
	if p := ctl.Status().Panel; len(p.Drip) != 1 || p.Drip[0].Left != 1 || time.Until(p.Drip[0].Next) < 10*time.Minute {
		t.Errorf("after one: %+v", p.Drip)
	}
	due()
	waitUntil(t, "the last unlock", func() bool { return hasEvent(ctl, "Every achievement in") })
	if p := ctl.Status().Panel; len(p.Drip) != 0 {
		t.Errorf("done games leave the list: %+v", p.Drip)
	}
	list, _ := ctl.Achievements(context.Background(), 252490)
	for _, a := range list {
		if a.Protected == a.Unlocked {
			t.Errorf("%s: unlocked %v", a.ID, a.Unlocked)
		}
	}
	if err := ctl.StartDrip(context.Background(), []uint32{252490}, 30); err == nil {
		t.Error("nothing left to unlock should be refused")
	}
}

func TestWeeklySummary(t *testing.T) {
	ctl, cm, _ := newPanel(t)
	cm.schema = achievementSchema()
	cm.statValues = map[uint32]uint32{1: 0}
	var mu sync.Mutex
	var got []string
	hook := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		mu.Lock()
		got = append(got, string(b))
		mu.Unlock()
	}))
	defer hook.Close()

	if _, err := ctl.SendWeekly(context.Background()); err == nil {
		t.Error("without alerts there's nowhere to send it")
	}
	if err := ctl.SetNotify(hook.URL); err != nil {
		t.Fatal(err)
	}
	if !ctl.Status().Panel.Weekly {
		t.Error("the weekly summary is on once alerts are")
	}
	if _, err := ctl.SetAllAchievements(context.Background(), []uint32{252490}, true); err != nil {
		t.Fatal(err)
	}
	waitUntil(t, "the count", func() bool {
		return strings.Contains(ctl.WeeklyText(context.Background()), "2 achievement(s) unlocked")
	})
	text, err := ctl.SendWeekly(context.Background())
	if err != nil || !strings.Contains(text, "game-hours") {
		t.Fatalf("summary: %q %v", text, err)
	}
	mu.Lock()
	last := got[len(got)-1]
	mu.Unlock()
	if !strings.Contains(last, "Your week with houridle") {
		t.Errorf("sent: %s", last)
	}
	ctl.SetWeekly(false)
	if ctl.Status().Panel.Weekly {
		t.Error("turned off")
	}
}

// fakeCommunity stands in for the inventory, gems and booster creator.
type fakeCommunity struct {
	mu      sync.Mutex
	ground  []string
	boosted []string
	cards   []string // asset IDs still there
}

func newFakeCommunity(t *testing.T) *fakeCommunity {
	f := &fakeCommunity{cards: []string{"a1", "a2", "a3", "k1"}}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		defer f.mu.Unlock()
		r.ParseForm()
		p := r.URL.Path
		switch {
		case p == fmt.Sprintf("/inventory/%d/753/6", alice):
			var assets []map[string]string
			for _, a := range f.cards {
				class := "bye"
				if a == "k1" {
					class = "kill"
				}
				assets = append(assets, map[string]string{"assetid": a, "classid": class, "instanceid": "0", "amount": "1"})
			}
			assets = append(assets, map[string]string{"assetid": "f1", "classid": "foil", "instanceid": "0", "amount": "1"},
				map[string]string{"assetid": "g1", "classid": "gems", "instanceid": "0", "amount": "500"},
				map[string]string{"assetid": "s1", "classid": "sack", "instanceid": "0", "amount": "1"})
			card := func(class, name, border string) map[string]any {
				return map[string]any{"classid": class, "instanceid": "0", "name": name, "market_hash_name": "252490-" + name, "market_fee_app": 252490,
					"tags": []map[string]string{{"category": "item_class", "internal_name": "item_class_2"}, {"category": "cardborder", "internal_name": border}, {"category": "Game", "localized_tag_name": "Rust"}}}
			}
			json.NewEncoder(w).Encode(map[string]any{"success": 1, "assets": assets, "descriptions": []map[string]any{
				card("bye", "Bye Zombies", "cardborder_0"), card("kill", "Killer wood pile", "cardborder_0"), card("foil", "Bye Zombies (Foil)", "cardborder_1"),
				{"classid": "gems", "instanceid": "0", "name": "Gems", "market_hash_name": "753-Gems", "tags": []map[string]string{{"category": "item_class", "internal_name": "item_class_7"}}},
				{"classid": "sack", "instanceid": "0", "name": "Sack of Gems", "market_hash_name": "753-Sack of Gems", "tags": []map[string]string{{"category": "item_class", "internal_name": "item_class_7"}}},
			}})
		case p == fmt.Sprintf("/profiles/%d/ajaxgetgoovalue/", alice):
			io.WriteString(w, `{"strTitle":"Turn it into Gems?","goo_value":"20","item_type":24}`) // like Steam: no "success"
		case p == fmt.Sprintf("/profiles/%d/ajaxgrindintogoo/", alice):
			sid, _ := r.Cookie("sessionid")
			if sid == nil || r.PostForm.Get("sessionid") != sid.Value || r.PostForm.Get("goo_value_expected") != "20" {
				w.WriteHeader(http.StatusForbidden)
				return
			}
			a := r.PostForm.Get("assetid")
			f.ground = append(f.ground, a)
			for i, c := range f.cards {
				if c == a {
					f.cards = append(f.cards[:i], f.cards[i+1:]...)
					break
				}
			}
			io.WriteString(w, `{"success":1,"goo_value_received ":20}`)
		case p == "/tradingcards/boostercreator/":
			io.WriteString(w, `<script>CBoosterCreatorPage.Init(
				[{"appid":252490,"name":"Rust","series":1,"price":"400","unavailable":false},{"appid":440,"name":"TF2","series":1,"price":"300","unavailable":true,"available_at_time":"Nov 5 @ 3:00pm"}],
				parseFloat( "1500" ), parseFloat( "1000" ), parseFloat( "500" ), "<div>" );</script>`)
		case p == "/tradingcards/ajaxcreatebooster/":
			f.boosted = append(f.boosted, r.PostForm.Get("appid"))
			io.WriteString(w, `{"purchase_result":{"communityitemid":"9","success":1},"goo_amount":"1100"}`)
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)
	old := inventory.BaseURL
	inventory.BaseURL = srv.URL
	t.Cleanup(func() { inventory.BaseURL = old })
	return f
}

func TestInventoryGemsAndBoosters(t *testing.T) {
	ctl, _, _ := newPanel(t)
	newFakeMarket(t)
	f := newFakeCommunity(t)
	ctx := context.Background()

	inv, err := ctl.Inventory(ctx)
	if err != nil {
		t.Fatal(err)
	}
	// 3 Bye Zombies at 13, 1 Killer wood pile at 2; the foil isn't priced.
	if inv.Gems != 1500 || inv.Cards != 5 || inv.Spare != 2 || inv.Value != 41 || len(inv.Games) != 1 {
		t.Fatalf("inventory: %+v", inv)
	}
	if g := inv.Games[0]; g.Name != "Rust" || g.Kinds != 3 || g.Foils != 1 || g.Gems != 20 || g.Spare != 2 {
		t.Errorf("game: %+v", g)
	}

	if _, err := ctl.GrindDuplicates(ctx, []uint32{252490}, 60); err == nil || !strings.Contains(err.Error(), "changed") {
		t.Errorf("a different total must be refused: %v", err)
	}
	res, err := ctl.GrindDuplicates(ctx, []uint32{252490}, 40)
	if err != nil || res.Cards != 2 || res.Gems != 40 {
		t.Fatalf("grind: %+v %v", res, err)
	}
	f.mu.Lock()
	kept := strings.Join(f.cards, ",")
	f.mu.Unlock()
	if kept != "a1,k1" {
		t.Errorf("one of each card must be kept: %s", kept)
	}
	if _, err := ctl.GrindDuplicates(ctx, []uint32{252490}, 40); err == nil {
		t.Error("nothing spare is left")
	}

	b, err := ctl.Boosters(ctx)
	if err != nil || b.Gems != 1500 || len(b.Boosters) != 2 || !b.Boosters[0].Available || b.Boosters[0].AppID != 252490 || b.Boosters[1].AvailableAt == "" {
		t.Fatalf("boosters: %+v %v", b, err)
	}
	if err := ctl.MakeBooster(ctx, 252490, 300); err == nil {
		t.Error("a higher price must be refused")
	}
	if err := ctl.MakeBooster(ctx, 440, 300); err == nil {
		t.Error("an unavailable booster must be refused")
	}
	if err := ctl.MakeBooster(ctx, 252490, 400); err != nil {
		t.Fatal(err)
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.boosted) != 1 || f.boosted[0] != "252490" {
		t.Errorf("made: %v", f.boosted)
	}
}

func TestExtraAccounts(t *testing.T) {
	ctl, _, _ := newPanel(t)
	accts, _ := steamauth.DefaultAccounts()
	if err := accts.StoreFor(bob).Save(steamauth.Session{AccountName: "bob", SteamID: bob, RefreshToken: fakeToken(bob, "client", farFuture)}); err != nil {
		t.Fatal(err)
	}
	bobID := steamauth.FormatSteamID(bob)
	if err := ctl.SetExtra(steamauth.FormatSteamID(alice), "cards", true); err == nil {
		t.Error("the main account can't idle twice")
	}
	if err := ctl.SetExtra(bobID, "everything", true); err == nil {
		t.Error("an unknown mode should be refused")
	}
	if err := ctl.SetExtra(bobID, "cards", true); err != nil {
		t.Fatal(err)
	}
	ex := ctl.Status().Panel.Extras
	if len(ex) != 1 || ex[0].SteamID != bobID || !ex[0].On || ex[0].Mode != "cards" {
		t.Fatalf("extras: %+v", ex)
	}
	if err := ctl.SetExtra(bobID, "", false); err != nil {
		t.Fatal(err)
	}
	if ex := ctl.Status().Panel.Extras; len(ex) != 1 || ex[0].On {
		t.Errorf("off: %+v", ex)
	}
}
