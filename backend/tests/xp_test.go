package tests

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"sync"
	"testing"

	"houridle/backend/cards"
	"houridle/backend/idler"
	"houridle/backend/steam"
	"houridle/backend/steamcm"
)

const craftPage = `<script>g_steamID = "76561197960266729";</script>
<div class="badge_row is_link"><a class="badge_row_overlay" href="https://steamcommunity.com/id/x/gamecards/440/"></a>
 <a class="badge_craft_button" href="https://steamcommunity.com/id/x/gamecards/440/">Ready</a>
 <div class="badge_title"> Team Fortress 2 &nbsp;</div><div class="badge_progress_info"> 6 of 6 cards collected </div></div>
<div class="badge_row is_link"><a class="badge_row_overlay" href="https://steamcommunity.com/id/x/gamecards/440/?border=1"></a>
 <a class="badge_craft_button" href="https://steamcommunity.com/id/x/gamecards/440/?border=1">Ready</a>
 <div class="badge_title"> Team Fortress 2 &nbsp;</div></div>
<div class="badge_row is_link"><a class="badge_row_overlay" href="https://steamcommunity.com/id/x/gamecards/252490/"></a>
 <div class="badge_title"> Rust &nbsp;</div><div class="badge_progress_info"> 4 of 5 cards collected </div></div>
<div class="badge_row is_link"><a class="badge_row_overlay" href="https://steamcommunity.com/id/x/gamecards/730/"></a>
 <div class="badge_title"> CS &nbsp;</div><div class="badge_progress_info"> 1 of 8 cards collected </div></div>
<div class="badge_row is_link"><a class="badge_row_overlay" href="https://steamcommunity.com/id/x/badges/2"></a>
 <div class="badge_title"> Pillar of Community &nbsp;</div></div>`

func TestBadgesFromTheBadgePage(t *testing.T) {
	badges := cards.ParseBadges(craftPage)
	if len(badges) != 3 {
		t.Fatalf("game badges only, the foil row merged: %+v", badges)
	}
	tf2 := badges[0]
	if tf2.AppID != 440 || !tf2.Ready || !tf2.Foil || tf2.Collected != 6 || tf2.Total != 6 {
		t.Errorf("TF2: %+v", tf2)
	}
	closest := cards.Closest(badges)
	if len(closest) != 2 || closest[0].AppID != 252490 || closest[0].Missing() != 1 || closest[1].Missing() != 7 {
		t.Errorf("closest, fewest missing first: %+v", closest)
	}

	setPage := `<div class="badge_card_set_card owned"> <div class="game_card_ctn"></div> <div class="badge_card_set_text badge_card_set_title ellipsis"> <div class="badge_card_set_text_qty">(2)</div> Killer wood pile <div style="clear: right"></div> </div> <div class="badge_card_set_text ellipsis"> 4 of 5, Series 1 </div> </div>
<div class="badge_card_set_card unowned"> <div class="game_card_ctn"></div> <div class="badge_card_set_text badge_card_set_title ellipsis"> Tastes like chicken <div style="clear: right"></div> </div> <div class="badge_card_set_text ellipsis"> 1 of 5, Series 1 </div> </div>`
	set := cards.ParseSetCards(setPage, 252490)
	if len(set) != 2 || set[0].Name != "Killer wood pile" || set[0].Owned != 2 || set[1].Owned != 0 ||
		set[1].Market != "https://steamcommunity.com/market/listings/753/252490-Tastes%20like%20chicken" {
		t.Errorf("set: %+v", set)
	}
}

func TestCraftingABadge(t *testing.T) {
	var mu sync.Mutex
	var got []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		r.ParseForm()
		sid, _ := r.Cookie("sessionid")
		login, _ := r.Cookie("steamLoginSecure")
		if !strings.HasSuffix(r.URL.Path, "/ajaxcraftbadge/") || sid == nil || login == nil || r.PostForm.Get("sessionid") != sid.Value {
			w.WriteHeader(http.StatusForbidden)
			return
		}
		mu.Lock()
		got = append(got, r.PostForm.Get("appid")+"/"+r.PostForm.Get("border_color"))
		n := len(got)
		mu.Unlock()
		if r.PostForm.Get("appid") == "730" {
			io.WriteString(w, `{"success":2}`)
			return
		}
		fmt.Fprintf(w, `{"success":1,"Badge":{"level":%d,"xp":%d}}`, n, n*100)
	}))
	defer srv.Close()
	old := cards.BaseURL
	cards.BaseURL = srv.URL
	defer func() { cards.BaseURL = old }()

	c, err := cards.Craft(context.Background(), alice, "tok", 440, true)
	if err != nil || c.Level != 1 || c.XP != 100 {
		t.Fatalf("craft: %+v %v", c, err)
	}
	if _, err := cards.Craft(context.Background(), alice, "tok", 730, false); err == nil {
		t.Error("an incomplete set shouldn't count as crafted")
	}
	if !slices.Equal(got, []string{"440/1", "730/0"}) {
		t.Errorf("requests: %v", got)
	}
}

func TestIdlingCraftsBadgesAndDoesSaleEvents(t *testing.T) {
	oldRead, oldCraft := idler.ReadBadges, idler.CraftBadge
	defer func() { idler.ReadBadges, idler.CraftBadge = oldRead, oldCraft }()
	idler.ReadBadges = func(context.Context, uint64, string) ([]cards.Badge, error) {
		return []cards.Badge{{AppID: 440, Name: "TF2", Ready: true, Total: 6, Collected: 6}, {AppID: 730, Name: "CS", Total: 8, Collected: 1}}, nil
	}
	var mu sync.Mutex
	sets := 2 // two full TF2 sets: two levels, then out of sets
	idler.CraftBadge = func(_ context.Context, _ uint64, token string, app uint32, foil bool) (cards.Crafted, error) {
		mu.Lock()
		defer mu.Unlock()
		if app != 440 || foil || token != "web-access-token" || sets == 0 {
			return cards.Crafted{}, fmt.Errorf("no full set")
		}
		sets--
		return cards.Crafted{Level: 2 - sets}, nil
	}

	sup, cm, events, ctx := startSupervisor(t, []*steam.Game{{AppID: 440, Name: "TF2"}}, idler.SessionOptions{Craft: true, Events: true})
	cm.mu.Lock()
	cm.stickerReady = true
	cm.mu.Unlock()
	sup.Start(ctx)
	var crafted, sticker, explored int
	waitFor(t, "crafting and sale events", func() bool {
		for {
			select {
			case e := <-events:
				switch {
				case strings.HasPrefix(e.Text, "Crafted the TF2 badge") && e.Alert:
					crafted++
				case strings.Contains(e.Text, "sticker") && e.Alert:
					sticker++
				case strings.Contains(e.Text, "discovery queue (3 games)"):
					explored++
				}
			default:
				return crafted == 2 && sticker == 1 && explored == 1
			}
		}
	})
	cm.mu.Lock()
	skips := slices.Clone(cm.calls)
	cm.mu.Unlock()
	if !slices.Contains(skips, "skip 10") || !slices.Contains(skips, "skip 30") {
		t.Errorf("every queue item should be stepped through: %v", skips)
	}
}

func TestDiscoveryQueueNeedsACountry(t *testing.T) {
	cm := newFakeCM(t, alice, 1)
	s, err := connectAlice(t, cm)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	// No account info arrived (the fake doesn't send one): houridle falls
	// back to a country, as Steam refuses a queue without one.
	if n, err := s.ExploreQueue(context.Background()); err != nil || n != 3 {
		t.Errorf("explored %d, %v", n, err)
	}
	if err := s.ClaimSaleItem(context.Background()); err != steamcm.ErrNoSticker {
		t.Errorf("no event: %v", err)
	}
}
