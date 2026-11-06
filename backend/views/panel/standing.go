package panel

import (
	"context"
	"fmt"
	"strings"
	"time"

	"houridle/backend/features/cards"
	"houridle/backend/features/market"
	"houridle/backend/online/steamauth"
	"houridle/backend/online/steamcm"
	"houridle/backend/run/config"
	"houridle/backend/run/idler"
	"houridle/backend/views/web"
)

// StandingEvery is how often the account's bans and restrictions are
// checked. A variable so tests needn't wait.
var StandingEvery = 12 * time.Hour

type standingState struct {
	val   *web.Standing // the last good reading; nil if none yet
	err   string        // why the last check failed ("" if it didn't)
	tried time.Time     // when it was last checked
	src   string        // the account it's for
	busy  bool
}

// standingRetry is how soon a failed check is tried again (at startup,
// say, before houridle has connected to Steam).
const standingRetry = 2 * time.Minute

// CheckStanding reads the account's bans and restrictions now, and alerts
// if they changed since the last check.
func (c *Controller) CheckStanding(ctx context.Context) (web.Standing, error) {
	c.mu.Lock()
	src := c.src
	c.mu.Unlock()
	out := web.Standing{Checked: time.Now(), DaysSinceBan: -1}
	err := c.withToken(ctx, func(cs *steamcm.Session, id uint64, token string) error {
		s, err := cards.AccountStanding(ctx, id, token)
		if err != nil {
			return err
		}
		out.VACBans, out.GameBans, out.DaysSinceBan, out.TradeBan, out.Limited = s.VACBans, s.GameBans, s.DaysSinceBan, s.TradeBan, s.Limited
		if mc, err := market.NewClient(id, token); err == nil {
			if w, err := mc.Wallet(ctx); err == nil {
				out.Market, out.MarketReason = w.Allowed, w.Reason
			}
		}
		return nil
	})
	c.mu.Lock()
	if c.standing.src != src.id() {
		c.standing = standingState{src: src.id(), busy: c.standing.busy}
	}
	c.standing.tried, c.standing.err = out.Checked, ""
	if err != nil {
		c.standing.err = err.Error()
	} else {
		out.OK = true
		c.standing.val = &out
	}
	c.mu.Unlock()
	if err == nil {
		c.noteStanding(src, out)
	}
	return out, err
}

// noteStanding remembers the account's record and alerts when it's worse
// (or better) than last time.
func (c *Controller) noteStanding(src source, s web.Standing) {
	key := steamauth.FormatSteamID(src.account.SteamID)
	now := config.StandingRecord{VACBans: s.VACBans, GameBans: s.GameBans, TradeBan: s.TradeBan, Limited: s.Limited}
	var before *config.StandingRecord
	c.updateConfig(func(cfg *config.Config) {
		if old, ok := cfg.Standing[key]; ok {
			before = &old
		}
		if cfg.Standing == nil {
			cfg.Standing = map[string]config.StandingRecord{}
		}
		cfg.Standing[key] = now
	})
	who := src.account.Label()
	var worse, better []string
	switch {
	case before == nil:
		if s.VACBans+s.GameBans > 0 || s.TradeBan != "None" {
			c.emit(idler.Event{At: time.Now(), Level: idler.EventWarn, Alert: true, Text: who + "'s Steam record: " + standingText(s)})
		}
		return
	case now.VACBans > before.VACBans:
		worse = append(worse, fmt.Sprintf("a new VAC ban (%d on record)", now.VACBans))
	}
	if before != nil {
		if now.GameBans > before.GameBans {
			worse = append(worse, fmt.Sprintf("a new game ban (%d on record)", now.GameBans))
		}
		if now.TradeBan != before.TradeBan {
			if now.TradeBan == "None" {
				better = append(better, "the trade restriction ("+before.TradeBan+") is lifted")
			} else {
				worse = append(worse, "trading is now restricted ("+now.TradeBan+")")
			}
		}
		if before.Limited && !now.Limited {
			better = append(better, "it's no longer a limited account")
		}
	}
	if len(worse) > 0 {
		c.emit(idler.Event{At: time.Now(), Level: idler.EventBad, Alert: true, Text: fmt.Sprintf("Steam shows %s on %s", strings.Join(worse, " and "), who)})
	}
	if len(better) > 0 {
		c.emit(idler.Event{At: time.Now(), Level: idler.EventGood, Alert: true, Text: fmt.Sprintf("Good news for %s: %s", who, strings.Join(better, ", "))})
	}
}

// standingText sums a record up in words.
func standingText(s web.Standing) string {
	var parts []string
	if s.VACBans > 0 {
		parts = append(parts, fmt.Sprintf("%d VAC ban(s)", s.VACBans))
	}
	if s.GameBans > 0 {
		parts = append(parts, fmt.Sprintf("%d game ban(s)", s.GameBans))
	}
	if s.TradeBan != "" && s.TradeBan != "None" {
		parts = append(parts, "trading "+strings.ToLower(s.TradeBan))
	}
	if len(parts) == 0 {
		return "no bans"
	}
	return strings.Join(parts, ", ")
}

// standingTick checks the account's record when it's due (signed-in
// accounts only).
func (c *Controller) standingTick(ctx context.Context, now time.Time) {
	c.mu.Lock()
	src, st := c.src, c.standing
	wait := StandingEvery
	if st.err != "" {
		wait = standingRetry
	}
	due := src.signedIn() && !st.busy && (st.src != src.id() || st.tried.IsZero() || now.Sub(st.tried) >= wait)
	if due {
		c.standing.busy = true
	}
	c.mu.Unlock()
	if !due {
		return
	}
	go func() {
		defer func() {
			c.mu.Lock()
			c.standing.busy = false
			c.mu.Unlock()
		}()
		c.CheckStanding(ctx)
	}()
}

func (c *Controller) standingInfo() *web.Standing {
	c.mu.Lock()
	defer c.mu.Unlock()
	st := c.standing
	if st.src != c.src.id() || st.tried.IsZero() {
		return nil // not checked yet
	}
	out := web.Standing{Checked: st.tried, DaysSinceBan: -1, TradeBan: "None"}
	if st.val != nil {
		out = *st.val
	}
	out.Error = st.err
	return &out
}
