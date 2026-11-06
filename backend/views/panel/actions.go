package panel

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"houridle/backend/client/achievements"
	"houridle/backend/client/steam"
	"houridle/backend/features/cards"
	"houridle/backend/features/freegames"
	"houridle/backend/features/market"
	"houridle/backend/features/notify"
	"houridle/backend/online/steamauth"
	"houridle/backend/online/steamcm"
	"houridle/backend/run/config"
	"houridle/backend/run/idler"
	"houridle/backend/views/web"
)

var errNeedsSignIn = errors.New("this needs a saved sign-in: switch to an account, or sign in (Accounts)")

// withConn runs fn on a Steam connection for the current account: the
// idler's own while idling (a second one would knock it off), otherwise a
// short-lived one of its own.
func (c *Controller) withConn(ctx context.Context, fn func(cs *steamcm.Session, steamID uint64) error) error {
	c.mu.Lock()
	src, r := c.src, c.run
	c.mu.Unlock()
	if !src.signedIn() {
		return errNeedsSignIn
	}
	id := src.account.SteamID
	if r != nil {
		if ss, ok := r.sup.(*idler.SessionSupervisor); ok {
			return ss.Use(func(cs *steamcm.Session) error { return fn(cs, id) })
		}
	}
	mgr := &steamauth.Manager{Client: steamauth.NewClient(), Store: src.account.Store}
	sess, err := mgr.Current()
	if err != nil {
		return err
	}
	cs, err := steamcm.Connect(ctx, steamcm.ConfigFor(sess))
	if err != nil {
		return err
	}
	defer cs.Close()
	return fn(cs, id)
}

// withToken is withConn plus a web access token for the account (for
// Steam Community and the store).
func (c *Controller) withToken(ctx context.Context, fn func(cs *steamcm.Session, steamID uint64, token string) error) error {
	return c.withConn(ctx, func(cs *steamcm.Session, id uint64) error {
		token, err := cs.AccessToken(ctx)
		if err != nil {
			return err
		}
		return fn(cs, id, token)
	})
}

// Library is the games to pick from: the account's, or the ones installed
// here for the Steam client. Most played first.
func (c *Controller) Library(ctx context.Context) ([]web.LibraryGame, error) {
	c.mu.Lock()
	src := c.src
	c.mu.Unlock()
	var out []web.LibraryGame
	if src.client {
		for _, g := range c.local {
			out = append(out, web.LibraryGame{AppID: g.AppID, Name: g.Name, Minutes: g.Minutes})
		}
	} else {
		err := c.withConn(ctx, func(cs *steamcm.Session, id uint64) error {
			owned, err := freegames.Library(ctx, cs, id) // includes free games never played
			for _, g := range owned {
				name := g.Name
				if name == "" {
					name = steam.PlaceholderName(g.AppID)
				}
				out = append(out, web.LibraryGame{AppID: g.AppID, Name: name, Minutes: g.Minutes, Shared: g.Shared})
			}
			return err
		})
		if err != nil {
			return nil, err
		}
	}
	c.mu.Lock()
	for _, g := range out {
		c.names[g.AppID] = g.Name
	}
	c.mu.Unlock()
	slices.SortFunc(out, func(a, b web.LibraryGame) int {
		if a.Minutes != b.Minutes {
			return b.Minutes - a.Minutes
		}
		return strings.Compare(strings.ToLower(a.Name), strings.ToLower(b.Name))
	})
	return out, nil
}

// Achievements lists a game's achievements for the account.
func (c *Controller) Achievements(ctx context.Context, appID uint32) ([]web.Achievement, error) {
	c.mu.Lock()
	src := c.src
	c.mu.Unlock()
	if src.client {
		lib, err := c.achievementHelperPaths()
		if err != nil {
			return nil, err
		}
		helper, list, err := achievements.Open(c.opts.ExePath, lib, appID)
		if err != nil {
			return nil, err
		}
		helper.Close()
		out := make([]web.Achievement, len(list))
		for i, a := range list {
			out[i] = web.Achievement{ID: a.ID, Name: a.Name, Description: a.Description, Hidden: a.Hidden, Unlocked: a.Unlocked}
		}
		return out, nil
	}
	var out []web.Achievement
	err := c.withConn(ctx, func(cs *steamcm.Session, _ uint64) error {
		st, err := cs.UserStats(ctx, appID)
		if err != nil {
			return err
		}
		for _, a := range st.Achievements {
			out = append(out, web.Achievement{ID: a.ID, Name: a.Name, Description: a.Description, Hidden: a.Hidden, Unlocked: a.Unlocked, Protected: a.Protected})
		}
		return nil
	})
	if out == nil {
		out = []web.Achievement{}
	}
	return out, err
}

// SetAchievements unlocks and locks achievements; it returns the ones that
// didn't change, with why.
func (c *Controller) SetAchievements(ctx context.Context, appID uint32, unlock, lock []string) (map[string]string, error) {
	failed, err := c.setAchievements(ctx, appID, unlock, lock)
	if err == nil {
		c.noteAchievements(appID, len(unlock)+len(lock)-len(failed), unlockedCount(unlock, failed))
	}
	return failed, err
}

func (c *Controller) setAchievements(ctx context.Context, appID uint32, unlock, lock []string) (map[string]string, error) {
	if len(unlock)+len(lock) == 0 {
		return map[string]string{}, nil
	}
	c.mu.Lock()
	src := c.src
	c.mu.Unlock()
	failed := map[string]string{}
	if src.client {
		lib, err := c.achievementHelperPaths()
		if err != nil {
			return nil, err
		}
		helper, _, err := achievements.Open(c.opts.ExePath, lib, appID)
		if err != nil {
			return nil, err
		}
		defer helper.Close()
		res, err := helper.Apply(unlock, lock)
		for _, f := range res.Failed {
			failed[f.ID] = f.Error
		}
		return failed, err
	}
	err := c.withConn(ctx, func(cs *steamcm.Session, _ uint64) error {
		st, err := cs.UserStats(ctx, appID)
		if err != nil {
			return err
		}
		failed, err = cs.SetAchievements(ctx, st, unlock, lock)
		return err
	})
	return failed, err
}

// MaxBulkGames is how many games one "unlock everything" may cover.
const MaxBulkGames = 100

// SetAllAchievements unlocks (or locks) every achievement it can in each
// game, one game after another, and says what happened in each. Ones only
// the game's own servers can change are left alone. A game that fails
// doesn't stop the rest.
func (c *Controller) SetAllAchievements(ctx context.Context, apps []uint32, unlock bool) ([]web.BulkAchievements, error) {
	if len(apps) == 0 {
		return nil, errors.New("pick at least one game")
	}
	if len(apps) > MaxBulkGames {
		return nil, fmt.Errorf("pick at most %d games at a time", MaxBulkGames)
	}
	out := make([]web.BulkAchievements, 0, len(apps))
	changed, games := 0, 0
	for _, app := range apps {
		if ctx.Err() != nil {
			return out, ctx.Err()
		}
		r := web.BulkAchievements{AppID: app, Name: c.gameName(app)}
		list, err := c.Achievements(ctx, app)
		if err != nil {
			r.Error = err.Error()
			out = append(out, r)
			continue
		}
		var ids []string
		for _, a := range list {
			switch {
			case a.Protected:
				r.GameOnly++
			case a.Unlocked == unlock:
				r.Already++
			default:
				ids = append(ids, a.ID)
			}
		}
		r.Total = len(list)
		if len(ids) > 0 {
			var failed map[string]string
			if unlock {
				failed, err = c.setAchievements(ctx, app, ids, nil)
			} else {
				failed, err = c.setAchievements(ctx, app, nil, ids)
			}
			if err != nil {
				r.Error = err.Error()
			}
			r.Failed = len(failed)
			r.Changed = len(ids) - len(failed)
			if err != nil {
				r.Changed = 0
			}
		}
		if r.Changed > 0 {
			changed += r.Changed
			games++
		}
		out = append(out, r)
	}
	if changed > 0 {
		verb := "Unlocked"
		if !unlock {
			verb = "Locked"
		}
		ev := idler.Event{At: time.Now(), Level: idler.EventInfo, Text: fmt.Sprintf("%s %d achievement(s) in %d game(s) from the page", verb, changed, games)}
		if unlock {
			ev.Kind, ev.N = idler.KindAchievements, changed
		}
		c.emit(ev)
	}
	return out, nil
}

// noteAchievements reports a change; only unlocks count toward the weekly
// summary.
func (c *Controller) noteAchievements(appID uint32, changed, unlocked int) {
	c.emit(idler.Event{At: time.Now(), Level: idler.EventInfo, Text: fmt.Sprintf("Changed %d achievement(s) for %s from the page", changed, c.gameName(appID)), Kind: idler.KindAchievements, N: unlocked})
}

// unlockedCount is how many of unlock went through (weren't in failed).
func unlockedCount(unlock []string, failed map[string]string) int {
	n := 0
	for _, id := range unlock {
		if _, bad := failed[id]; !bad {
			n++
		}
	}
	return n
}

func (c *Controller) achievementHelperPaths() (string, error) {
	if !steam.Running() {
		return "", errors.New("start Steam and sign in first")
	}
	return steam.FindAPILibrary(c.opts.AppDir, c.steamDir)
}

// FreeGames shows Steam's current giveaways, or (claim) adds them.
func (c *Controller) FreeGames(ctx context.Context, claim bool) ([]web.FreeGame, error) {
	var results []freegames.Result
	err := c.withConn(ctx, func(cs *steamcm.Session, id uint64) error {
		var err error
		results, err = freegames.ClaimAll(ctx, cs, id, !claim)
		return err
	})
	if err != nil {
		return nil, err
	}
	out := []web.FreeGame{}
	for _, r := range results {
		g := web.FreeGame{Name: r.Offer.String(), AppID: r.Offer.AppID, Outcome: outcomeName(r.Outcome), Review: webReview(r.Offer.Review)}
		if r.Err != nil {
			g.Error = r.Err.Error()
		}
		if r.Outcome == freegames.Added || r.Outcome == freegames.Sent {
			c.emit(idler.Event{At: time.Now(), Level: idler.EventGood, Text: "Added a free game to your account: " + g.Name, Alert: true})
		}
		out = append(out, g)
	}
	return out, nil
}

func webReview(r freegames.Review) web.Review {
	return web.Review{Summary: r.Summary, Positive: r.Positive, Count: r.Count}
}

const (
	defaultFreeToPlay = 50
	maxFreeToPlay     = 200
)

// FreeToPlay lists the best-reviewed free-to-play games, marking the ones
// the account has played (Steam lists free games once they've been played).
func (c *Controller) FreeToPlay(ctx context.Context, q web.FreeToPlayQuery) ([]web.FreeToPlay, error) {
	n := q.Count
	if n <= 0 {
		n = defaultFreeToPlay
	}
	n = min(max(n, 10), maxFreeToPlay)
	top, err := freegames.TopFreeToPlay(ctx, n, freegames.Features{Achievements: q.Achievements, Cards: q.Cards, Sort: q.Sort})
	if err != nil {
		return nil, err
	}
	owned := map[uint32]bool{}
	c.withConn(ctx, func(cs *steamcm.Session, id uint64) error { // best effort
		games, err := freegames.Library(ctx, cs, id) // free games never played count too
		for _, g := range games {
			owned[g.AppID] = true
		}
		return err
	})
	out := make([]web.FreeToPlay, 0, len(top))
	for _, g := range top {
		out = append(out, web.FreeToPlay{AppID: g.AppID, Name: g.Name, Review: webReview(g.Review), Owned: owned[g.AppID]})
		c.mu.Lock()
		c.names[g.AppID] = g.Name
		c.mu.Unlock()
	}
	return out, nil
}

// Drops is how many card drops each game has left (games with none aren't
// listed), from the badge pages.
func (c *Controller) Drops(ctx context.Context) (map[uint32]int, error) {
	out := map[uint32]int{}
	err := c.withToken(ctx, func(cs *steamcm.Session, id uint64, token string) error {
		drops, err := cards.Remaining(ctx, id, token)
		for _, d := range drops {
			out[d.AppID] = d.Remaining
		}
		return err
	})
	return out, err
}

// AddFreeGames adds free-to-play games to the account; it returns the ones
// Steam added.
func (c *Controller) AddFreeGames(ctx context.Context, appIDs []uint32) ([]uint32, error) {
	var granted []uint32
	err := c.withConn(ctx, func(cs *steamcm.Session, _ uint64) error {
		var err error
		granted, err = cs.AddFreeGames(ctx, appIDs)
		return err
	})
	if len(granted) > 0 {
		names := make([]string, 0, len(granted))
		for _, id := range granted {
			names = append(names, c.gameName(id))
		}
		c.emit(idler.Event{At: time.Now(), Level: idler.EventGood, Text: fmt.Sprintf("Added %d free-to-play game(s) to your account: %s", len(granted), strings.Join(names, ", "))})
	}
	return granted, err
}

// ---------- badges and sale events ----------

const closestBadges = 15

// Badges is the badges ready to craft and the ones closest to a full set.
func (c *Controller) Badges(ctx context.Context) (web.BadgesInfo, error) {
	out := web.BadgesInfo{Ready: []web.BadgeInfo{}, Closest: []web.BadgeInfo{}}
	err := c.withToken(ctx, func(cs *steamcm.Session, id uint64, token string) error {
		badges, err := cards.Badges(ctx, id, token)
		if err != nil {
			return err
		}
		info := func(b cards.Badge) web.BadgeInfo {
			return web.BadgeInfo{AppID: b.AppID, Name: b.Name, Collected: b.Collected, Total: b.Total, Foil: b.Foil, Market: cards.MarketSearch(b.AppID)}
		}
		for _, b := range badges {
			if b.Ready || b.Foil {
				out.Ready = append(out.Ready, info(b))
			}
		}
		for i, b := range cards.Closest(badges) {
			if i == closestBadges {
				break
			}
			out.Closest = append(out.Closest, info(b))
		}
		return nil
	})
	return out, err
}

// SetCards lists a game's cards: which you have, and Market links for all.
func (c *Controller) SetCards(ctx context.Context, appID uint32) ([]web.CardInfo, error) {
	out := []web.CardInfo{}
	err := c.withToken(ctx, func(cs *steamcm.Session, id uint64, token string) error {
		set, err := cards.SetCards(ctx, id, token, appID)
		if err != nil {
			return err
		}
		// The Market's name for a card can differ ("... (Trading Card)"):
		// link to its listing by the Market's own name when it's known.
		var known map[string]market.Card
		if onMarket, err := c.gameCards(ctx, appID); err == nil {
			known = byName(onMarket)
		}
		for _, card := range set {
			link := card.Market
			if m, ok := known[strings.ToLower(card.Name)]; ok {
				link = market.ListingURL(m.HashName)
			}
			out = append(out, web.CardInfo{Name: card.Name, Owned: card.Owned, Market: link})
		}
		return nil
	})
	return out, err
}

// CraftNow crafts every badge whose set is complete.
func (c *Controller) CraftNow(ctx context.Context) ([]string, error) {
	var crafted []string
	err := c.withToken(ctx, func(cs *steamcm.Session, id uint64, token string) error {
		badges, err := cards.Badges(ctx, id, token)
		if err != nil {
			return err
		}
		var firstErr error
		cards.CraftReady(ctx, cards.ReadyToCraft(badges),
			func(ctx context.Context, appID uint32, foil bool) (cards.Crafted, error) {
				return cards.Craft(ctx, id, token, appID, foil)
			},
			func(cr cards.Craftable, got cards.Crafted) {
				crafted = append(crafted, cr.Label())
				c.emit(idler.Event{At: time.Now(), Level: idler.EventGood, Text: cards.CraftedText(cr, got), Alert: true, Kind: idler.KindCraft, N: 1})
			},
			func(_ cards.Craftable, err error) {
				if firstErr == nil {
					firstErr = err
				}
			})
		if len(crafted) == 0 {
			return firstErr // nothing crafted: say why (nil if nothing was ready)
		}
		return nil
	})
	return crafted, err
}

// ExploreNow explores the discovery queue and claims the sale-event
// sticker if there is one.
func (c *Controller) ExploreNow(ctx context.Context) (web.Explored, error) {
	var out web.Explored
	err := c.withConn(ctx, func(cs *steamcm.Session, _ uint64) error {
		n, err := cs.ExploreQueue(ctx)
		if err != nil {
			return err
		}
		out.Games = n
		switch err := cs.ClaimSaleItem(ctx); {
		case err == nil:
			out.Sticker = true
			c.emit(idler.Event{At: time.Now(), Level: idler.EventGood, Text: "Claimed today's sale-event sticker", Alert: true})
		case !errors.Is(err, steamcm.ErrNoSticker):
			return err
		}
		return nil
	})
	if err == nil {
		c.emit(idler.Event{At: time.Now(), Level: idler.EventInfo, Text: fmt.Sprintf("Explored the discovery queue from the page (%d games)", out.Games)})
	}
	return out, err
}

func outcomeName(o freegames.Outcome) string {
	switch o {
	case freegames.Added:
		return "added"
	case freegames.Sent:
		return "sent"
	case freegames.WouldAdd:
		return "would-add"
	case freegames.Owned:
		return "owned"
	case freegames.NeedsBase:
		return "needs-base"
	}
	return "failed"
}

// ---------- accounts ----------

// UseSource switches to another account (by SteamID) or the Steam client
// ("client"). Idling stops; start it again for the new one.
func (c *Controller) UseSource(id string) error {
	c.op.Lock()
	defer c.op.Unlock()
	src, err := c.findSource(id)
	if err != nil {
		return err
	}
	if src.id() == "" {
		return errors.New("nothing to switch to")
	}
	c.mu.Lock()
	same := c.src.id() == src.id()
	c.mu.Unlock()
	if same {
		return nil
	}
	c.stopRun()
	if src.signedIn() { // it was idling alongside: it's the main account now
		c.stopExtra(src.account.SteamID)
		c.updateConfig(func(cfg *config.Config) {
			cfg.Extras = slices.DeleteFunc(cfg.Extras, func(e config.ExtraAccount) bool { return e.SteamID == src.id() })
		})
	}
	if err := c.takeLock(src); err != nil {
		c.mu.Lock()
		old := c.src
		c.mu.Unlock()
		if old.id() != "" {
			c.takeLock(old) // get our old lock back
		}
		return err
	}
	c.mu.Lock()
	c.src = src
	st := c.settings
	c.mu.Unlock()
	if src.client && (st.Mode == "all" || st.Mode == "cards") {
		st.Mode = "games"
	}
	c.openHistory()
	c.reloadNotifier()
	c.remember(src, st, false)
	c.emit(idler.Event{At: time.Now(), Level: idler.EventInfo, Text: "Switched to " + src.label()})
	return nil
}

// MakeDefault makes a saved account the default. It swaps the accounts'
// files, so it can't happen while idling.
func (c *Controller) MakeDefault(id string) error {
	c.op.Lock()
	defer c.op.Unlock()
	c.mu.Lock()
	idling, cur := c.run != nil, c.src
	c.mu.Unlock()
	if idling {
		return errors.New("stop idling first: changing the default moves the accounts' files")
	}
	accts, err := steamauth.DefaultAccounts()
	if err != nil {
		return err
	}
	acc, err := accts.MakeDefault(id)
	if err != nil {
		return err
	}
	// The files moved: find the current account again, under its new lock.
	if cur.signedIn() {
		if src, err := c.findSource(steamauth.FormatSteamID(cur.account.SteamID)); err == nil {
			if err := c.takeLock(src); err != nil {
				return err
			}
			c.mu.Lock()
			c.src = src
			c.mu.Unlock()
			c.openHistory()
		}
	}
	c.emit(idler.Event{At: time.Now(), Level: idler.EventInfo, Text: acc.Label() + " is now the default account"})
	return nil
}

// Logout forgets a saved account on this computer. If it's the one in use,
// idling stops and houridle switches to the default (or the Steam client).
func (c *Controller) Logout(id string) error {
	c.op.Lock()
	defer c.op.Unlock()
	accts, err := steamauth.DefaultAccounts()
	if err != nil {
		return err
	}
	acc, err := accts.Find(id)
	if err != nil && acc.Store == nil {
		return err
	}
	c.mu.Lock()
	current := c.src.signedIn() && c.src.account.SteamID == acc.SteamID
	c.mu.Unlock()
	if current {
		c.stopRun()
		c.mu.Lock()
		if c.lock != nil {
			c.lock.Release()
			c.lock = nil
		}
		c.src = source{}
		c.mu.Unlock()
	}
	if err := acc.Store.Delete(); err != nil {
		return fmt.Errorf("couldn't remove the saved sign-in: %w", err)
	}
	// Removing the default with others saved would leave none.
	if all := accts.List(); len(all) > 0 && !all[0].Default && all[0].Err == nil {
		accts.MakeDefault(steamauth.FormatSteamID(all[0].SteamID))
	}
	c.emit(idler.Event{At: time.Now(), Level: idler.EventInfo, Text: "Signed out of " + acc.Label() + " on this computer"})
	if current {
		if src, err := c.findSource(""); err == nil && src.id() != "" {
			if err := c.takeLock(src); err == nil {
				c.mu.Lock()
				c.src = src
				c.mu.Unlock()
			}
		}
		c.openHistory()
		c.mu.Lock()
		src, st := c.src, c.settings
		c.mu.Unlock()
		c.remember(src, st, false)
	}
	return nil
}

// ---------- QR sign-in ----------

const loginTimeout = 5 * time.Minute

// BeginLogin starts a QR sign-in: the page shows the code, the Steam
// mobile app scans it. A sign-in already waiting is reused.
func (c *Controller) BeginLogin() (web.Login, error) {
	c.mu.Lock()
	if c.login.State == "waiting" {
		l := c.login
		c.mu.Unlock()
		return l, nil
	}
	c.mu.Unlock()

	client := steamauth.NewClient()
	ctx, cancel := context.WithTimeout(context.Background(), loginTimeout)
	p, err := client.BeginQR(ctx, "houridle")
	if err != nil {
		cancel()
		return web.Login{}, err
	}
	c.setLogin(web.Login{State: "waiting", URL: p.ChallengeURL})
	go func() {
		defer cancel()
		for {
			res, err := client.Poll(ctx, p)
			if err != nil {
				c.setLogin(web.Login{State: "failed", Error: loginError(err)})
				return
			}
			if res != nil {
				c.finishLogin(res)
				return
			}
			c.mu.Lock()
			changed := c.login.URL != p.ChallengeURL
			c.mu.Unlock()
			if changed { // Steam rotated the code
				c.setLogin(web.Login{State: "waiting", URL: p.ChallengeURL})
			}
			select {
			case <-ctx.Done():
				c.setLogin(web.Login{State: "failed", Error: "timed out waiting for the Steam app; start again"})
				return
			case <-time.After(p.Interval):
			}
		}
	}()
	return c.LoginState(), nil
}

func (c *Controller) setLogin(l web.Login) {
	if l.URL != "" {
		l.QR, _ = web.QRSVG(l.URL)
	}
	c.mu.Lock()
	c.login = l
	c.mu.Unlock()
}

// LoginState is the QR sign-in's progress.
func (c *Controller) LoginState() web.Login {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.login
}

func (c *Controller) finishLogin(res *steamauth.Result) {
	accts, err := steamauth.DefaultAccounts()
	if err == nil {
		mgr := &steamauth.Manager{Client: steamauth.NewClient(), Store: accts.StoreFor(res.SteamID)}
		err = mgr.Remember(res, nil)
	}
	if err != nil {
		c.setLogin(web.Login{State: "failed", Error: "signed in, but couldn't save it: " + err.Error()})
		return
	}
	label := res.AccountName
	if label == "" {
		label = steamauth.FormatSteamID(res.SteamID)
	}
	c.setLogin(web.Login{State: "done", Account: label})
	c.emit(idler.Event{At: time.Now(), Level: idler.EventGood, Text: "Signed in as " + label + " and saved it"})

	// With nothing to idle with yet, use the new account right away.
	c.mu.Lock()
	none := c.src.id() == ""
	c.mu.Unlock()
	if none {
		c.UseSource(steamauth.FormatSteamID(res.SteamID))
	}
}

func loginError(err error) string {
	if errors.Is(err, context.DeadlineExceeded) {
		return "timed out waiting for the Steam app; start again"
	}
	return "sign-in failed: " + err.Error()
}

// ---------- alerts and the page key ----------

// SetNotify saves the webhook for alerts ("" turns them off) and sends a
// test to a new one first, so a wrong URL isn't saved.
func (c *Controller) SetNotify(url string) error {
	url = strings.TrimSpace(url)
	if url != "" {
		n, err := notify.New(url, "")
		if err != nil {
			return err
		}
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		if err := n.Send(ctx, "test message: alerts work. You'll hear from houridle if signing in fails, idling stops working, or every game is done."); err != nil {
			return fmt.Errorf("%w; nothing was saved", err)
		}
	}
	if err := c.updateConfig(func(cfg *config.Config) { cfg.Notify = url }); err != nil {
		return err
	}
	c.setNotifier(url)
	return nil
}

// TestNotify sends a test alert to the saved webhook.
func (c *Controller) TestNotify(ctx context.Context) error {
	c.mu.Lock()
	n := c.notifier
	c.mu.Unlock()
	if n == nil {
		return errors.New("no webhook saved yet")
	}
	return n.Send(ctx, "test message from the status page")
}

func (c *Controller) reloadNotifier() {
	cfg, _ := c.config()
	c.setNotifier(cfg.Notify)
}

// NewKey makes and saves a new key for the page; old links stop working.
func (c *Controller) NewKey() (string, error) {
	key := web.NewKey()
	if err := c.updateConfig(func(cfg *config.Config) { cfg.WebKey = key }); err != nil {
		return "", err
	}
	c.emit(idler.Event{At: time.Now(), Level: idler.EventInfo, Text: "Made a new key for the status page; old links stopped working"})
	return key, nil
}
