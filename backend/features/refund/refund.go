// Package refund knows Steam's refund window: a game bought in the last 14
// days and played for less than 2 hours can be refunded. Idling counts as
// playing, so idling such a game can quietly use up the right to a refund;
// houridle holds those games back unless told otherwise.
package refund

import (
	"context"
	"fmt"
	"net/http"
	"strconv"
	"sync"
	"time"

	"houridle/backend/online/steamcm"
	"houridle/backend/online/steamweb"
)

// Steam's refund rules.
const (
	Window  = 14 * 24 * time.Hour
	MaxPlay = 2 * time.Hour
)

// StoreURL is the Steam store. A variable so tests can use a local fake.
var StoreURL = "https://store.steampowered.com"

// Payment methods that aren't a purchase through Steam, so there is nothing
// to refund: keys, gifts of free items, promotions, family sharing...
var notBought = map[uint32]bool{
	0:    true, // none
	1:    true, // activation code (a key bought elsewhere)
	8:    true, // guest pass
	16:   true, // hardware promotion
	30:   true, // authorized device
	64:   true, // auto grant
	129:  true, // Valve
	130:  true, // master complimentary
	131:  true, // promotional
	134:  true, // master subscription
	256:  true, // OEM ticket
	1024: true, // complimentary (free games, giveaways)
	1025: true, // family group
}

// License flags that mean the license no longer counts.
const deadFlags = 8 | 16 | 32 | 1024 // expired, cancelled by user, by admin, friendly-fraud lock

// Purchase is a package bought recently enough to be refundable.
type Purchase struct {
	PackageID uint32
	Bought    time.Time
}

// Purchases picks the licenses that are Steam purchases by this account
// (accountID is the low 32 bits of its SteamID) still inside the window.
func Purchases(licenses []steamcm.License, accountID uint32, now time.Time) []Purchase {
	var out []Purchase
	for _, l := range licenses {
		switch {
		case l.PackageID == 0, notBought[l.PaymentMethod], l.Flags&deadFlags != 0:
		case l.OwnerID != 0 && l.OwnerID != accountID:
		case l.Created.Unix() <= 0, l.Created.After(now), now.Sub(l.Created) >= Window:
		default:
			out = append(out, Purchase{PackageID: l.PackageID, Bought: l.Created})
		}
	}
	return out
}

// Hold is a game that could still be refunded.
type Hold struct {
	Until  time.Time     // when the refund window closes
	Played time.Duration // playtime on record; -1 if not known yet
}

// Refundable reports whether the game can still be refunded at now.
func (h Hold) Refundable(now time.Time) bool {
	return now.Before(h.Until) && h.Played < MaxPlay
}

// Note says how much of the window is left, for the game's status line.
func (h Hold) Note(now time.Time) string {
	note := "refundable for " + Span(h.Until.Sub(now))
	if h.Played > 0 {
		note += ", " + Short(h.Played) + " played"
	}
	return note
}

// Span reads a time left as "9 days", "1 day", "5 hours" or "under 2 hours".
func Span(d time.Duration) string {
	switch {
	case d >= 48*time.Hour:
		return strconv.Itoa(int(d/(24*time.Hour))) + " days"
	case d >= 24*time.Hour:
		return "1 day"
	case d >= 2*time.Hour:
		return strconv.Itoa(int(d/time.Hour)) + " hours"
	default:
		return "under 2 hours"
	}
}

// Short reads a playtime as "1h12m" or "45m".
func Short(d time.Duration) string {
	d = d.Round(time.Minute)
	if d < time.Hour {
		return fmt.Sprintf("%dm", int(d/time.Minute))
	}
	return fmt.Sprintf("%dh%02dm", int(d/time.Hour), int(d%time.Hour/time.Minute))
}

// Apps finds which games a package contains, asking the Steam store and
// remembering the answers.
type Apps struct {
	mu     sync.Mutex
	known  map[uint32][]uint32
	client *http.Client
}

// NewApps returns an empty package lookup.
func NewApps() *Apps {
	return &Apps{known: map[uint32][]uint32{}, client: &http.Client{Timeout: 15 * time.Second}}
}

// Of returns the app IDs in package pkg.
func (a *Apps) Of(ctx context.Context, pkg uint32) ([]uint32, error) {
	a.mu.Lock()
	apps, ok := a.known[pkg]
	a.mu.Unlock()
	if ok {
		return apps, nil
	}
	id := strconv.FormatUint(uint64(pkg), 10)
	var res map[string]struct {
		Success bool `json:"success"`
		Data    struct {
			Apps []struct {
				ID uint32 `json:"id"`
			} `json:"apps"`
		} `json:"data"`
	}
	if err := steamweb.GetJSON(ctx, a.client, StoreURL+"/api/packagedetails?packageids="+id, &res, 1<<20); err != nil {
		return nil, err
	}
	entry, ok := res[id]
	if !ok {
		return nil, fmt.Errorf("the store said nothing about package %d", pkg)
	}
	apps = []uint32{}
	if entry.Success {
		for _, app := range entry.Data.Apps {
			apps = append(apps, app.ID)
		}
	}
	a.mu.Lock()
	a.known[pkg] = apps
	a.mu.Unlock()
	return apps, nil
}
