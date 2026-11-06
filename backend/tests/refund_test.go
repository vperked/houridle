package tests

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"houridle/backend/client/steam"
	"houridle/backend/features/refund"
	"houridle/backend/online/steamcm"
	"houridle/backend/run/config"
	"houridle/backend/run/idler"
	"houridle/backend/views/web"
)

func TestOnlyRecentSteamPurchasesAreRefundable(t *testing.T) {
	now := time.Date(2026, 11, 6, 12, 0, 0, 0, time.UTC)
	me := uint32(alice & 0xffffffff)
	days := func(n int) time.Time { return now.Add(-time.Duration(n) * 24 * time.Hour) }
	licenses := []steamcm.License{
		{PackageID: 1, Created: days(3), PaymentMethod: 2},                 // bought by card 3 days ago
		{PackageID: 2, Created: days(13), PaymentMethod: 128, OwnerID: me}, // Steam wallet, 13 days ago
		{PackageID: 3, Created: days(15), PaymentMethod: 2},                // too long ago
		{PackageID: 4, Created: days(1), PaymentMethod: 1024},              // a free game
		{PackageID: 5, Created: days(1), PaymentMethod: 1},                 // a key bought elsewhere
		{PackageID: 6, Created: days(1), PaymentMethod: 1025},              // family sharing
		{PackageID: 7, Created: days(1), PaymentMethod: 2, OwnerID: me + 1},
		{PackageID: 8, Created: days(1), PaymentMethod: 2, Flags: 16}, // cancelled
		{PackageID: 9, PaymentMethod: 2},                              // no date
		{PackageID: 0, Created: days(1), PaymentMethod: 2},
	}
	var got []uint32
	for _, p := range refund.Purchases(licenses, me, now) {
		got = append(got, p.PackageID)
	}
	if !slices.Equal(got, []uint32{1, 2}) {
		t.Errorf("refundable packages: %v", got)
	}
}

func TestRefundHoldWording(t *testing.T) {
	now := time.Now()
	h := refund.Hold{Until: now.Add(9*24*time.Hour + time.Hour), Played: 72 * time.Minute}
	if !h.Refundable(now) || h.Note(now) != "refundable for 9 days, 1h12m played" {
		t.Errorf("note: %q", h.Note(now))
	}
	if (refund.Hold{Until: now.Add(time.Hour), Played: -1}).Note(now) != "refundable for under 2 hours" {
		t.Error("an unknown playtime isn't shown")
	}
	if (refund.Hold{Until: now.Add(time.Hour), Played: 2 * time.Hour}).Refundable(now) {
		t.Error("2 hours played can't be refunded")
	}
	if (refund.Hold{Until: now.Add(-time.Minute)}).Refundable(now) {
		t.Error("a closed window can't be refunded")
	}
	for d, want := range map[time.Duration]string{30 * time.Hour: "1 day", 5 * time.Hour: "5 hours", 50 * time.Hour: "2 days"} {
		if got := refund.Span(d); got != want {
			t.Errorf("Span(%v) = %q, want %q", d, got, want)
		}
	}
}

// fakeStorePackages answers the store's packagedetails with apps for each
// package, and counts the requests.
func fakeStorePackages(t *testing.T, apps map[string]string) *atomic.Int32 {
	t.Helper()
	var hits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/packagedetails" {
			http.NotFound(w, r)
			return
		}
		hits.Add(1)
		id := r.URL.Query().Get("packageids")
		body, ok := apps[id]
		if !ok {
			io.WriteString(w, `{"`+id+`":{"success":false}}`)
			return
		}
		io.WriteString(w, `{"`+id+`":{"success":true,"data":{"name":"x","apps":[`+body+`]}}}`)
	}))
	t.Cleanup(srv.Close)
	old := refund.StoreURL
	refund.StoreURL = srv.URL
	t.Cleanup(func() { refund.StoreURL = old })
	return &hits
}

func TestPackageAppsComeFromTheStoreOnce(t *testing.T) {
	hits := fakeStorePackages(t, map[string]string{"500": `{"id":440,"name":"TF2"},{"id":441,"name":"Soundtrack"}`})
	apps := refund.NewApps()
	for range 2 {
		got, err := apps.Of(context.Background(), 500)
		if err != nil || !slices.Equal(got, []uint32{440, 441}) {
			t.Fatalf("apps: %v %v", got, err)
		}
	}
	if hits.Load() != 1 {
		t.Errorf("asked the store %d times", hits.Load())
	}
	if got, err := apps.Of(context.Background(), 999); err != nil || len(got) != 0 {
		t.Errorf("a package the store doesn't know: %v %v", got, err)
	}
}

// refundSetup starts a supervisor for CS and TF2 where TF2 was bought two
// days ago and has 30 minutes on record.
func refundSetup(t *testing.T, opts idler.SessionOptions) (*idler.SessionSupervisor, *fakeCM, chan idler.Event, context.Context) {
	t.Helper()
	fakeStorePackages(t, map[string]string{"500": `{"id":440,"name":"TF2"}`})
	games := []*steam.Game{{AppID: 730, Name: "CS"}, {AppID: 440, Name: "TF2"}}
	sup, cm, events, ctx := startSupervisor(t, games, opts)
	cm.mu.Lock()
	cm.licenses = []uint32{100, 500}
	cm.bought = map[uint32]fakePurchase{
		100: {at: time.Now().Add(-400 * 24 * time.Hour), payment: 2},
		500: {at: time.Now().Add(-2 * 24 * time.Hour), payment: 2},
	}
	cm.owned = []fakeGame{{ID: 730, Name: "CS", Minutes: 5000}, {ID: 440, Name: "TF2", Minutes: 30}}
	cm.mu.Unlock()
	return sup, cm, events, ctx
}

func TestRefundableGamesAreHeldBack(t *testing.T) {
	oldEvery := idler.PlaytimeEvery
	idler.PlaytimeEvery = 50 * time.Millisecond
	defer func() { idler.PlaytimeEvery = oldEvery }()

	sup, cm, events, ctx := refundSetup(t, idler.SessionOptions{})
	sup.Start(ctx)

	if got := <-cm.gotGames; !slices.Equal(got, []uint64{730}) {
		t.Fatalf("Steam was told %v; TF2 can still be refunded", got)
	}
	waitFor(t, "TF2 held", func() bool { return sup.Views()[1].State == idler.StateHeld })
	if v := sup.Views()[1]; !strings.HasPrefix(v.Note, "refundable for 11 days, 30m played") {
		t.Errorf("note: %q", v.Note)
	}
	waitFor(t, "an alert", func() bool {
		for {
			select {
			case e := <-events:
				if e.Alert && strings.HasPrefix(e.Text, "Not idling TF2: you can still refund it for 11 days (30m of 2h played)") {
					return true
				}
			default:
				return false
			}
		}
	})

	// Once it has 2 hours on record it can't be refunded: it joins in.
	cm.mu.Lock()
	cm.owned[1].Minutes = 120
	cm.mu.Unlock()
	waitFor(t, "TF2 idled", func() bool {
		select {
		case got := <-cm.gotGames:
			return slices.Equal(got, []uint64{730, 440})
		default:
			return false
		}
	})
	if v := sup.Views()[1]; v.State == idler.StateHeld {
		t.Errorf("TF2 still held: %+v", v)
	}
}

func TestRefundableIdlesThemAnyway(t *testing.T) {
	sup, cm, _, ctx := refundSetup(t, idler.SessionOptions{Refundable: true})
	sup.Start(ctx)
	if got := <-cm.gotGames; !slices.Equal(got, []uint64{730, 440}) {
		t.Fatalf("Steam was told %v", got)
	}
}

func TestAClosingRefundWindowIsAnnounced(t *testing.T) {
	fakeStorePackages(t, map[string]string{"500": `{"id":440,"name":"TF2"}`})
	sup, cm, events, ctx := startSupervisor(t, []*steam.Game{{AppID: 440, Name: "TF2"}}, idler.SessionOptions{})
	cm.mu.Lock()
	cm.licenses = []uint32{500}
	cm.bought = map[uint32]fakePurchase{500: {at: time.Now().Add(-13*24*time.Hour - time.Hour), payment: 2}}
	cm.owned = []fakeGame{{ID: 440, Name: "TF2", Minutes: 0}}
	cm.mu.Unlock()
	sup.Start(ctx)

	if got := <-cm.gotGames; len(got) != 0 {
		t.Fatalf("Steam was told %v", got)
	}
	waitFor(t, "the alert", func() bool {
		select {
		case e := <-events:
			return e.Alert && strings.HasPrefix(e.Text, "Not idling TF2") && strings.Contains(e.Text, " hours (0m of 2h played)")
		default:
			return false
		}
	})
}

func TestPanelSwitchIdlesRefundableGames(t *testing.T) {
	ctl, cm, appDir := newPanel(t)
	fakeStorePackages(t, map[string]string{"500": `{"id":440,"name":"TF2"}`})
	cm.mu.Lock()
	cm.licenses = []uint32{500}
	cm.bought = map[uint32]fakePurchase{500: {at: time.Now().Add(-24 * time.Hour), payment: 2}}
	cm.mu.Unlock()

	sorted := func(g []uint64) []uint64 { slices.Sort(g); return g }
	if err := ctl.Start(web.Settings{Mode: "games", Games: []uint32{730, 440}}); err != nil {
		t.Fatal(err)
	}
	if got := sorted(<-cm.gotGames); !slices.Equal(got, []uint64{730}) {
		t.Fatalf("switch off: Steam was told %v", got)
	}
	ctl.StopIdling()
	waitFor(t, "idling to stop", func() bool { return !ctl.Status().Panel.Idling })

	if err := ctl.Start(web.Settings{Mode: "games", Games: []uint32{730, 440}, Refundable: true}); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "both games idled", func() bool {
		select {
		case g := <-cm.gotGames:
			return slices.Equal(sorted(g), []uint64{440, 730})
		default:
			return false
		}
	})
	if cfg, _ := config.Load(appDir); cfg.Panel == nil || !cfg.Panel.Refundable {
		t.Errorf("the switch should be remembered: %+v", cfg.Panel)
	}
}
