package tests

import (
	"context"
	"testing"
)

func TestAccountStandingAndItsAlerts(t *testing.T) {
	ctl, _, _ := newPanel(t)
	f := newFakeMarket(t)
	ctx := context.Background()

	s, err := ctl.CheckStanding(ctx)
	if err != nil || s.VACBans != 0 || s.GameBans != 0 || s.TradeBan != "None" || s.Limited || s.DaysSinceBan != -1 || !s.Market {
		t.Fatalf("a clean account: %+v %v", s, err)
	}
	if p := ctl.Status().Panel; p.Standing == nil || p.Standing.TradeBan != "None" {
		t.Errorf("the page should get the reading: %+v", p.Standing)
	}
	if hasEvent(ctl, "Steam shows") {
		t.Error("a clean first reading isn't news")
	}

	// A ban and a trade restriction appear: that's an alert.
	f.mu.Lock()
	f.vacBans, f.gameBans, f.tradeBan, f.limited = 2, 1, "Probation", true
	f.blocked = "your account is restricted from trading"
	f.mu.Unlock()
	s, err = ctl.CheckStanding(ctx)
	if err != nil || s.VACBans != 2 || s.GameBans != 1 || s.DaysSinceBan != 12 || s.TradeBan != "Probation" || !s.Limited || s.Market || s.MarketReason == "" {
		t.Fatalf("a banned account: %+v %v", s, err)
	}
	if !hasEvent(ctl, "Steam shows a new VAC ban (2 on record) and a new game ban (1 on record) and trading is now restricted (Probation)") {
		t.Errorf("the change should be alerted: %+v", ctl.Status().Events)
	}

	// The trade restriction lifts.
	f.mu.Lock()
	f.tradeBan = "None"
	f.mu.Unlock()
	if _, err := ctl.CheckStanding(ctx); err != nil {
		t.Fatal(err)
	}
	if !hasEvent(ctl, "the trade restriction (Probation) is lifted") {
		t.Error("good news should be alerted too")
	}
}
