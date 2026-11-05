package idler

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"sync"
	"time"

	"houridle/backend/cards"
	"houridle/backend/freegames"
	"houridle/backend/steam"
	"houridle/backend/steamauth"
	"houridle/backend/steamcm"
)

// Runner is what the console, the tray and the log need from an idling
// session: start it, wait for it to wind down, and read each game's status.
// Supervisor idles through the Steam client; SessionSupervisor through a
// signed-in houridle session.
type Runner interface {
	Start(ctx context.Context)
	Wait()
	Views() []View
	// Finished is closed when there is nothing left to idle (every game
	// reached its target, or every card dropped). nil if that never happens.
	Finished() <-chan struct{}
}

const (
	reconnectMin    = 5 * time.Second
	reconnectMax    = 5 * time.Minute
	retryAfterLogin = time.Minute // sign-in refused; maybe you ran houridle login since

	eresultLogonSessionReplaced = 34                  // a newer connection with the same sign-in took over
	retryAfterRefused           = 5 * time.Minute     // Steam refused the sign-in; each try is several attempts
	retryAfterLimit             = 10 * time.Minute    // Steam asked us to slow down
	renewEvery                  = 24 * time.Hour      // check the saved token on long runs
	renewWithin                 = 90 * 24 * time.Hour // renew when it has less than this left
	sessionNoteLogin            = "sign in needed"
	sessionNoteOffline          = "offline"
	sessionNoteInUse            = "in use elsewhere"

	// DefaultRotate is how often games swap when there are more than
	// MaxGames to idle.
	DefaultRotate = time.Hour
)

// Refresh intervals. Variables so tests can run them quickly.
var (
	PlaytimeEvery   = 10 * time.Minute // re-read Steam's recorded playtime
	CardsEvery      = 20 * time.Minute // re-read the badge pages (cards mode)
	CardsRetryEvery = 5 * time.Minute  // after the badge pages couldn't be read
)

// CardDrops reads which games have card drops left. A variable so tests
// can stand in for Steam Community.
var CardDrops = cards.Remaining

// OwnedLibrary is every game the account owns, for -all. A variable so
// tests can stand in for the Steam store.
var OwnedLibrary = freegames.Library

// ClaimFree adds Steam's free giveaways to the account (-claim). A variable
// so tests can stand in for the Steam store.
var ClaimFree = freegames.ClaimAll

// ClaimEvery is how often -claim looks for giveaways (they last days).
var ClaimEvery = 12 * time.Hour

// Badge crafting (-craft) and sale events (-events). Variables so tests can
// stand in for Steam and run them quickly.
var (
	CraftEvery  = 6 * time.Hour
	EventsEvery = 12 * time.Hour
	ReadBadges  = cards.Badges
	CraftBadge  = cards.Craft
)

// SessionOptions changes how a SessionSupervisor idles.
type SessionOptions struct {
	Status string        // friends-list status while idling ("" = online)
	Target time.Duration // a game is done once Steam has this much playtime on it (0 = never)
	Rotate time.Duration // with more than MaxGames games, swap this often (0 = DefaultRotate)
	Cards  bool          // idle the games with card drops left, from the badge pages
	All    bool          // idle every game the account owns (under Target, if set); new ones join
	Claim  bool          // add Steam's free giveaways to the account now and then
	Craft  bool          // craft game badges (Steam XP) whenever a full card set is there
	Events bool          // sale events: explore the discovery queue and claim the daily sticker
}

// sessionGame is one game's part of the session.
type sessionGame struct {
	game      *steam.Game
	active    bool          // reported to Steam as being played
	done      string        // why it's finished ("" while not)
	idled     time.Duration // this run, banked up to the supervisor's mark
	steamMin  int           // Steam's recorded playtime in minutes; -1 unknown
	baseMin   int           // steamMin when first read...
	baseIdled time.Duration // ...and idled at that moment, to estimate between reads
	drops     int           // card drops left (cards mode); -1 unknown
}

// total estimates Steam's playtime now: its last reading, plus what was
// idled since (Steam updates its number in chunks).
func (g *sessionGame) total(idled time.Duration) time.Duration {
	if g.steamMin < 0 {
		return 0
	}
	est := time.Duration(g.baseMin)*time.Minute + idled - g.baseIdled
	return max(time.Duration(g.steamMin)*time.Minute, est)
}

// SessionSupervisor idles games over one signed-in Steam connection, using
// the session saved by `houridle login`. It reconnects after drops, reads
// real playtime from Steam, retires games that reach their target, rotates
// through more than MaxGames games, and (cards mode) follows card drops.
type SessionSupervisor struct {
	mgr      *steamauth.Manager
	opts     SessionOptions
	onEvent  func(Event)
	wg       sync.WaitGroup
	finished chan struct{}
	finish   sync.Once

	// playMu orders the state set right after signing in with the state
	// onPlaying sets, so a change reported in between can't be overwritten
	// by the older reading.
	playMu sync.Mutex

	// unsaved is a renewed sign-in Steam issued that couldn't be written to
	// disk (guarded by mu). It is used for the next connections (and saving
	// retried), so a failed save never sends houridle back to the token it
	// replaced.
	unsaved *steamauth.Session

	mu        sync.Mutex // guards the fields below
	games     []*sessionGame
	state     State
	note      string
	lastMsg   string           // last warning/error shown, so retries don't repeat it
	mark      time.Time        // idle time is banked up to here
	conn      *steamcm.Session // the live connection while signed in; nil otherwise
	cardsRead bool             // the badge pages have been read at least once
	ownedRead bool             // Steam's list of owned games has been read at least once
}

// NewSession prepares a supervisor for games using the saved session in mgr.
func NewSession(games []*steam.Game, mgr *steamauth.Manager, onEvent func(Event)) *SessionSupervisor {
	return NewSessionWith(games, mgr, SessionOptions{}, onEvent)
}

// NewSessionWith is NewSession with options. In cards mode games may be
// empty: the badge pages decide.
func NewSessionWith(games []*steam.Game, mgr *steamauth.Manager, opts SessionOptions, onEvent func(Event)) *SessionSupervisor {
	if opts.Rotate <= 0 {
		opts.Rotate = DefaultRotate
	}
	s := &SessionSupervisor{mgr: mgr, opts: opts, onEvent: onEvent, state: StateStarting, finished: make(chan struct{})}
	drops := 0
	if opts.Cards {
		drops = -1 // unknown until the badge pages are read
	}
	for _, g := range games {
		s.games = append(s.games, &sessionGame{game: g, steamMin: -1, drops: drops})
	}
	return s
}

// Start connects in the background and returns immediately.
func (s *SessionSupervisor) Start(ctx context.Context) {
	s.wg.Add(1)
	go func() {
		defer s.wg.Done()
		s.run(ctx)
	}()
}

// Wait blocks until the connection has closed (after ctx is cancelled, or
// once Finished).
func (s *SessionSupervisor) Wait() { s.wg.Wait() }

// ErrNotConnected means the supervisor isn't signed in to Steam right now
// (it's connecting or reconnecting).
var ErrNotConnected = errors.New("houridle isn't connected to Steam right now (it's reconnecting); try again in a minute")

// Use runs fn on the supervisor's live Steam connection: Steam keeps one
// connection per sign-in, so a second one (to list games, change
// achievements...) would knock idling off.
func (s *SessionSupervisor) Use(fn func(*steamcm.Session) error) error {
	s.mu.Lock()
	cs := s.conn
	s.mu.Unlock()
	if cs == nil || cs.Err() != nil {
		return ErrNotConnected
	}
	return fn(cs)
}

// Finished is closed when nothing is left to idle.
func (s *SessionSupervisor) Finished() <-chan struct{} { return s.finished }

// Views returns every game's status: done, queued (waiting for a turn), or
// sharing the connection's state.
func (s *SessionSupervisor) Views() []View {
	s.mu.Lock()
	defer s.mu.Unlock()
	now := time.Now()
	waiting := s.openCountLocked() > MaxGames // only then do games queue
	out := make([]View, len(s.games))
	for i, g := range s.games {
		idled := g.idled
		if s.state == StateIdling && g.active {
			idled += now.Sub(s.mark)
		}
		v := View{Game: g.game, Idled: idled, Total: g.total(idled), Target: s.opts.Target, Drops: g.drops}
		switch {
		case g.done != "":
			v.State, v.Note = StateDone, g.done
		case waiting && !g.active:
			v.State, v.Note = StateQueued, "queued"
		default:
			v.State, v.Note = s.state, s.note
		}
		out[i] = v
	}
	return out
}

func (s *SessionSupervisor) emit(level EventLevel, text string) {
	s.onEvent(Event{At: time.Now(), Level: level, Text: text})
}

// warn shows a message once per distinct text, until something succeeds.
func (s *SessionSupervisor) warn(level EventLevel, text string) {
	s.mu.Lock()
	repeat := s.lastMsg == text
	s.lastMsg = text
	s.mu.Unlock()
	if !repeat {
		s.emit(level, text)
	}
}

// bankLocked adds the idle time since the mark to every active game. Call
// it (holding mu) before changing the state or which games are active.
func (s *SessionSupervisor) bankLocked(now time.Time) {
	if s.state == StateIdling {
		for _, g := range s.games {
			if g.active {
				g.idled += now.Sub(s.mark)
			}
		}
	}
	s.mark = now
}

func (s *SessionSupervisor) setState(state State, note string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.bankLocked(time.Now())
	s.state = state
	s.note = note
}

// pickLocked chooses which games Steam is told about, at most MaxGames.
// Finished games drop out and free slots go to the games that have idled
// least this run; with rotate, every slot is re-chosen that way, so with
// more than MaxGames games each gets a fair share. It reports whether the
// set changed.
func (s *SessionSupervisor) pickLocked(rotate bool) bool {
	s.bankLocked(time.Now())
	want := make(map[*sessionGame]bool, MaxGames)
	var open []*sessionGame
	for _, g := range s.games {
		switch {
		case g.done != "":
		case g.active && !rotate:
			want[g] = true // keeps its slot until the next rotation
		default:
			open = append(open, g)
		}
	}
	slices.SortStableFunc(open, func(a, b *sessionGame) int {
		switch {
		case a.idled < b.idled:
			return -1
		case a.idled > b.idled:
			return 1
		}
		return 0
	})
	for _, g := range open {
		if len(want) == MaxGames {
			break
		}
		want[g] = true
	}
	changed := false
	for _, g := range s.games {
		if g.active != want[g] {
			g.active = want[g]
			changed = true
		}
	}
	return changed
}

func (s *SessionSupervisor) activeIDsLocked() []uint32 {
	var ids []uint32
	for _, g := range s.games {
		if g.active {
			ids = append(ids, g.game.AppID)
		}
	}
	return ids
}

// openCountLocked is how many games aren't done.
func (s *SessionSupervisor) openCountLocked() int {
	n := 0
	for _, g := range s.games {
		if g.done == "" {
			n++
		}
	}
	return n
}

// allDone reports whether there's nothing left to idle. Only targets and
// cards finish games, so without them this is never true. In cards mode an
// empty list only counts once the badge pages have actually been read: a
// failed first read means "don't know yet", not "no drops left".
func (s *SessionSupervisor) allDone() bool {
	if s.opts.Target <= 0 && !s.opts.Cards {
		return false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.opts.Cards && !s.cardsRead || s.opts.All && !s.ownedRead {
		return false
	}
	return s.openCountLocked() == 0
}

// run is the connect / idle / reconnect loop. It returns when ctx ends or
// every game is done.
func (s *SessionSupervisor) run(ctx context.Context) {
	defer func() { s.setState(StateStopped, s.currentNote()) }()
	backoff := reconnectMin
	for ctx.Err() == nil {
		s.setState(StateStarting, "")
		wait, connected := s.once(ctx)
		if s.allDone() {
			s.finish.Do(func() { close(s.finished) })
			return
		}
		if connected {
			backoff = reconnectMin
		}
		if wait == 0 {
			wait = backoff
			backoff = min(backoff*2, reconnectMax)
		}
		if !sleepCtx(ctx, wait) {
			return
		}
	}
}

func (s *SessionSupervisor) currentNote() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.note
}

// once makes one connection attempt and, if it works, idles until the
// connection ends or nothing is left. wait is how long to pause before the
// next attempt (0 for the default backoff); connected says whether we got
// signed in.
func (s *SessionSupervisor) once(ctx context.Context) (wait time.Duration, connected bool) {
	sess, err := s.current()
	switch {
	case ctx.Err() != nil:
		return 0, false
	case errors.Is(err, steamauth.ErrNotLoggedIn), errors.Is(err, steamauth.ErrCorrupt), errors.Is(err, steamauth.ErrSessionExpired):
		s.setState(StateFailing, sessionNoteLogin)
		s.warn(EventBad, err.Error()+". Checking again every minute.")
		return retryAfterLogin, false
	case err != nil:
		s.warn(EventWarn, err.Error())
		return 0, false
	}

	cs, err := steamcm.Connect(ctx, steamcm.Config{
		SteamID:      sess.SteamID,
		AccountName:  sess.AccountName,
		RefreshToken: sess.RefreshToken,
		OnPlaying:    s.onPlaying,
		Status:       s.opts.Status,
	})
	if err != nil {
		return s.connectFailed(ctx, err), false
	}
	defer cs.Close()
	s.mu.Lock()
	s.conn = cs
	s.mu.Unlock()
	defer func() {
		s.mu.Lock()
		s.conn = nil
		s.mu.Unlock()
	}()

	cardsWait := CardsEvery
	if s.opts.Cards && !s.updateCards(ctx, cs, sess.SteamID) {
		cardsWait = CardsRetryEvery
	}
	s.updatePlaytime(ctx, cs, sess.SteamID)
	if s.allDone() {
		return 0, true
	}
	if err := s.report(cs, true, false); err != nil {
		s.warn(EventWarn, "Couldn't tell Steam which games to idle: "+err.Error())
		return 0, true
	}
	s.mu.Lock()
	s.lastMsg = ""
	active, open := len(s.activeIDsLocked()), s.openCountLocked()
	s.mu.Unlock()
	s.playMu.Lock()
	if cs.Blocked() {
		s.setState(StateWaiting, sessionNoteInUse)
	} else {
		s.setState(StateIdling, "")
		msg := fmt.Sprintf("Signed in as %s, idling %d game(s)", accountLabel(sess), active)
		if open > active {
			msg += fmt.Sprintf(" (%d more take turns every %s)", open-active, shortDuration(s.opts.Rotate))
		}
		s.emit(EventGood, msg)
	}
	s.playMu.Unlock()

	s.renewSignIn(ctx, cs, sess)
	renew := time.NewTicker(renewEvery)
	defer renew.Stop()
	playtime := time.NewTicker(PlaytimeEvery)
	defer playtime.Stop()
	rotate := time.NewTicker(s.opts.Rotate)
	defer rotate.Stop()
	cardsTimer := time.NewTimer(cardsWait)
	defer cardsTimer.Stop()
	if !s.opts.Cards {
		cardsTimer.Stop()
	}
	claim := time.NewTicker(ClaimEvery)
	defer claim.Stop()
	if s.opts.Claim {
		s.claimFree(ctx, cs, sess.SteamID)
	} else {
		claim.Stop()
	}
	craft := time.NewTicker(CraftEvery)
	defer craft.Stop()
	if s.opts.Craft {
		s.craftBadges(ctx, cs, sess.SteamID)
	} else {
		craft.Stop()
	}
	events := time.NewTicker(EventsEvery)
	defer events.Stop()
	if s.opts.Events {
		s.saleEvents(ctx, cs)
	} else {
		events.Stop()
	}
	seenItems, itemsChanged := cs.NewItems()
	for {
		select {
		case <-ctx.Done():
			return 0, true
		case <-itemsChanged:
			var n int
			n, itemsChanged = cs.NewItems()
			if seenItems >= 0 && n > seenItems {
				if s.opts.Cards {
					// Probably a card drop: read the badge pages now to say which.
					if s.updateCards(ctx, cs, sess.SteamID) {
						cardsTimer.Reset(CardsEvery)
						if s.opts.Craft {
							s.craftBadges(ctx, cs, sess.SteamID)
						}
					}
				} else {
					s.onEvent(Event{At: time.Now(), Level: EventGood, Text: fmt.Sprintf("%d new item(s) in your inventory, likely a card drop", n-seenItems), Alert: true, Kind: KindItem, N: n - seenItems})
				}
			}
			seenItems = n
		case <-renew.C:
			if cur, err := s.current(); err == nil {
				s.renewSignIn(ctx, cs, cur)
			}
		case <-playtime.C:
			s.updatePlaytime(ctx, cs, sess.SteamID)
		case <-rotate.C:
			if err := s.report(cs, false, true); err != nil {
				s.warn(EventWarn, "Couldn't tell Steam which games to idle: "+err.Error())
			}
			continue
		case <-claim.C:
			s.claimFree(ctx, cs, sess.SteamID)
			continue
		case <-craft.C:
			s.craftBadges(ctx, cs, sess.SteamID)
			continue
		case <-events.C:
			s.saleEvents(ctx, cs)
			continue
		case <-cardsTimer.C:
			next := CardsEvery
			if !s.updateCards(ctx, cs, sess.SteamID) {
				next = CardsRetryEvery
			} else if s.opts.Craft {
				s.craftBadges(ctx, cs, sess.SteamID) // a drop may have completed a set
			}
			cardsTimer.Reset(next)
		case <-cs.Done():
			return s.connectionLost(cs.Err()), true
		}
		if s.allDone() {
			s.emit(EventGood, s.finishedText())
			return 0, true
		}
		if err := s.report(cs, false, false); err != nil {
			s.warn(EventWarn, "Couldn't tell Steam which games to idle: "+err.Error())
		}
	}
}

// report re-picks the active games (see pickLocked) and tells Steam if
// they changed, or always (right after connecting).
func (s *SessionSupervisor) report(cs *steamcm.Session, always, rotate bool) error {
	s.mu.Lock()
	changed := s.pickLocked(rotate)
	ids := s.activeIDsLocked()
	s.mu.Unlock()
	if !changed && !always {
		return nil
	}
	return cs.SetGames(ids)
}

func (s *SessionSupervisor) finishedText() string {
	if s.opts.All && s.opts.Target <= 0 {
		return "You own no games to idle"
	}
	if s.opts.Cards {
		return "Every card drop has been received; nothing left to idle"
	}
	return fmt.Sprintf("Every game reached %s; nothing left to idle", shortDuration(s.opts.Target))
}

// craftBadges crafts every badge whose card set is complete (-craft). Each
// craft gives Steam XP and uses up a set of cards.
func (s *SessionSupervisor) craftBadges(ctx context.Context, cs *steamcm.Session, steamID uint64) {
	token, err := cs.AccessToken(ctx)
	var badges []cards.Badge
	if err == nil {
		badges, err = ReadBadges(ctx, steamID, token)
	}
	if err != nil {
		if ctx.Err() == nil {
			s.warn(EventWarn, "Couldn't read your badges to craft: "+err.Error())
		}
		return
	}
	cards.CraftReady(ctx, cards.ReadyToCraft(badges),
		func(ctx context.Context, appID uint32, foil bool) (cards.Crafted, error) {
			return CraftBadge(ctx, steamID, token, appID, foil)
		},
		func(c cards.Craftable, got cards.Crafted) {
			s.onEvent(Event{At: time.Now(), Level: EventGood, Text: cards.CraftedText(c, got), Alert: true, Kind: KindCraft, N: 1})
		},
		func(c cards.Craftable, err error) {
			s.warn(EventWarn, fmt.Sprintf("Couldn't craft the %s badge: %v", c.Label(), err))
		})
}

// saleEvents explores the discovery queue and claims the daily sale-event
// sticker (-events). Outside sales the queue is still explored (harmless)
// and there's no sticker.
func (s *SessionSupervisor) saleEvents(ctx context.Context, cs *steamcm.Session) {
	n, err := cs.ExploreQueue(ctx)
	if err != nil {
		if ctx.Err() == nil {
			s.warn(EventWarn, "Couldn't explore the discovery queue: "+err.Error())
		}
		return
	}
	s.emit(EventInfo, fmt.Sprintf("Explored the discovery queue (%d games)", n))
	switch err := cs.ClaimSaleItem(ctx); {
	case err == nil:
		s.onEvent(Event{At: time.Now(), Level: EventGood, Text: "Claimed today's sale-event sticker", Alert: true})
	case !errors.Is(err, steamcm.ErrNoSticker) && ctx.Err() == nil:
		s.warn(EventWarn, "Couldn't claim the sale-event sticker: "+err.Error())
	}
}

// claimFree adds Steam's current giveaways to the account (-claim).
func (s *SessionSupervisor) claimFree(ctx context.Context, cs *steamcm.Session, steamID uint64) {
	results, err := ClaimFree(ctx, cs, steamID, false)
	if err != nil {
		if ctx.Err() == nil {
			s.warn(EventWarn, "Couldn't check for free games: "+err.Error())
		}
		return
	}
	var added, failed []string
	for _, r := range results {
		switch r.Outcome {
		case freegames.Added, freegames.Sent:
			added = append(added, r.Offer.String())
		case freegames.Failed:
			failed = append(failed, r.Offer.String()+" ("+r.Err.Error()+")")
		}
	}
	for _, name := range added {
		s.onEvent(Event{At: time.Now(), Level: EventGood, Text: "Added a free game to your account: " + name, Alert: true})
	}
	if len(failed) > 0 {
		s.warn(EventWarn, fmt.Sprintf("Couldn't add %d free game(s): %s", len(failed), strings.Join(failed, "; ")))
	}
}

// updatePlaytime reads Steam's recorded playtime for every game and retires
// the ones that reached the target. Failing only costs accuracy.
func (s *SessionSupervisor) updatePlaytime(ctx context.Context, cs *steamcm.Session, steamID uint64) {
	var owned []steamcm.OwnedGame
	var err error
	if s.opts.All {
		owned, err = OwnedLibrary(ctx, cs, steamID) // every game, free ones never played included
	} else {
		owned, err = cs.OwnedGames(ctx)
	}
	if err != nil {
		if ctx.Err() == nil {
			s.warn(EventWarn, "Couldn't read your playtime from Steam: "+err.Error())
		}
		return
	}
	byID := make(map[uint32]steamcm.OwnedGame, len(owned))
	for _, g := range owned {
		byID[g.AppID] = g
	}

	var reached []string
	added := 0
	s.mu.Lock()
	s.bankLocked(time.Now())
	s.ownedRead = true
	if s.opts.All {
		added = s.addOwnedLocked(owned)
	}
	for _, g := range s.games {
		o, ok := byID[g.game.AppID]
		if !ok {
			continue // not owned, or Steam left it out (hidden); keep idling
		}
		if g.steamMin < 0 {
			g.baseMin, g.baseIdled = o.Minutes, g.idled
		}
		if o.Minutes > g.steamMin {
			g.steamMin = o.Minutes
			g.baseMin, g.baseIdled = o.Minutes, g.idled // restart the estimate from the fresh number
		}
		if g.game.Name == steam.PlaceholderName(g.game.AppID) && o.Name != "" {
			g.game.Name = o.Name
		}
		if s.opts.Target > 0 && g.done == "" && g.total(g.idled) >= s.opts.Target {
			g.done = "reached " + shortDuration(s.opts.Target)
			reached = append(reached, g.game.Name)
		}
	}
	s.mu.Unlock()
	for _, name := range reached {
		s.emit(EventGood, fmt.Sprintf("%s reached %s on record; done with it", name, shortDuration(s.opts.Target)))
	}
	if added > 0 {
		s.emit(EventInfo, fmt.Sprintf("Added %d of your games to idle", added))
	}
}

// addOwnedLocked adds the owned games houridle isn't idling yet (in -all
// mode), leaving out those already at the target. It returns how many.
func (s *SessionSupervisor) addOwnedLocked(owned []steamcm.OwnedGame) int {
	have := make(map[uint32]bool, len(s.games))
	for _, g := range s.games {
		have[g.game.AppID] = true
	}
	added := 0
	for _, o := range owned {
		if have[o.AppID] || s.opts.Target > 0 && time.Duration(o.Minutes)*time.Minute >= s.opts.Target {
			continue
		}
		name := o.Name
		if name == "" {
			name = steam.PlaceholderName(o.AppID)
		}
		s.games = append(s.games, &sessionGame{game: &steam.Game{AppID: o.AppID, Name: name, Minutes: o.Minutes}, steamMin: -1})
		added++
	}
	return added
}

// updateCards reads the badge pages and makes the game list the games
// with drops left. It reports whether the pages could be read.
func (s *SessionSupervisor) updateCards(ctx context.Context, cs *steamcm.Session, steamID uint64) bool {
	token, err := cs.AccessToken(ctx)
	var drops []cards.Drop
	if err == nil {
		drops, err = CardDrops(ctx, steamID, token)
	}
	if err != nil {
		if ctx.Err() == nil {
			s.warn(EventWarn, "Couldn't read your card drops: "+err.Error()+". Trying again in a few minutes.")
		}
		return false
	}

	left := make(map[uint32]cards.Drop, len(drops))
	for _, d := range drops {
		left[d.AppID] = d
	}
	type dropped struct {
		name    string
		n, left int
	}
	var finished, added []string
	var got []dropped
	s.mu.Lock()
	s.bankLocked(time.Now())
	s.cardsRead = true
	known := make(map[uint32]bool, len(s.games))
	for _, g := range s.games {
		known[g.game.AppID] = true
		d, ok := left[g.game.AppID]
		switch {
		case ok:
			if g.drops > d.Remaining {
				got = append(got, dropped{g.game.Name, g.drops - d.Remaining, d.Remaining})
			}
			g.drops = d.Remaining
			if g.done != "" {
				g.done = "" // a drop came back (rare: new cards added)
			}
		case g.done == "":
			if g.drops > 0 {
				got = append(got, dropped{g.game.Name, g.drops, 0})
			}
			g.drops = 0
			g.done = "all cards dropped"
			finished = append(finished, g.game.Name)
		}
	}
	for _, d := range drops {
		if !known[d.AppID] {
			s.games = append(s.games, &sessionGame{game: &steam.Game{AppID: d.AppID, Name: d.Name}, steamMin: -1, drops: d.Remaining})
			added = append(added, d.Name)
		}
	}
	total := 0
	for _, d := range drops {
		total += d.Remaining
	}
	s.mu.Unlock()

	for _, d := range got {
		text := fmt.Sprintf("Card drop: %s", d.name)
		if d.n > 1 {
			text = fmt.Sprintf("%d card drops: %s", d.n, d.name)
		}
		if d.left > 0 {
			text += fmt.Sprintf(" (%d left)", d.left)
		}
		s.onEvent(Event{At: time.Now(), Level: EventGood, Text: text, Alert: true, Kind: KindDrop, N: d.n})
	}
	for _, name := range finished {
		s.emit(EventGood, name+": every card drop received")
	}
	if len(added) > 0 {
		s.emit(EventInfo, fmt.Sprintf("%d card drop(s) left across %d game(s)", total, len(drops)))
	}
	return true
}

// renewSignIn swaps in a fresh refresh token when the saved one is within
// renewWithin of running out, so a long-running houridle never has to ask
// for a new login. Failing is not a problem until the token really expires.
func (s *SessionSupervisor) renewSignIn(ctx context.Context, cs *steamcm.Session, sess steamauth.Session) {
	if !sess.Expires.IsZero() && time.Until(sess.Expires) > renewWithin {
		return
	}
	token, err := cs.RenewRefreshToken(ctx)
	if err != nil {
		if ctx.Err() == nil {
			s.warn(EventWarn, "Couldn't renew the saved sign-in yet: "+err.Error())
		}
		return
	}
	updated, err := s.mgr.Adopt(sess, token)
	switch {
	case errors.Is(err, steamauth.ErrRenewedNotSaved):
		s.mu.Lock()
		s.unsaved = &updated
		s.mu.Unlock()
		s.warn(EventWarn, err.Error()+". Using it anyway, and trying to save it again on the next connection.")
	case err != nil:
		s.warn(EventWarn, err.Error())
	case updated.RefreshToken != sess.RefreshToken:
		s.emit(EventGood, "Renewed the saved sign-in, good until "+updated.Expires.Format("2006-01-02"))
	}
}

// current is the saved sign-in, or the renewed one that couldn't be saved
// if that is newer and for the same account. Saving it is retried here.
func (s *SessionSupervisor) current() (steamauth.Session, error) {
	sess, err := s.mgr.Current()
	if err != nil && !errors.Is(err, steamauth.ErrSessionExpired) {
		return sess, err
	}
	s.mu.Lock()
	pending := s.unsaved
	s.mu.Unlock()
	if pending == nil {
		return sess, err
	}
	if pending.SteamID != sess.SteamID || !pending.Expires.After(sess.Expires) {
		// The saved one changed since (houridle login): it wins.
		s.forgetUnsaved(pending)
		return sess, err
	}
	updated, aerr := s.mgr.Adopt(sess, pending.RefreshToken)
	switch {
	case aerr == nil:
		s.forgetUnsaved(pending)
		s.emit(EventGood, "Saved the renewed sign-in, good until "+updated.Expires.Format("2006-01-02"))
	case !errors.Is(aerr, steamauth.ErrRenewedNotSaved):
		s.forgetUnsaved(pending)
		return sess, err
	}
	return updated, nil
}

func (s *SessionSupervisor) forgetUnsaved(which *steamauth.Session) {
	s.mu.Lock()
	if s.unsaved == which {
		s.unsaved = nil
	}
	s.mu.Unlock()
}

func accountLabel(sess steamauth.Session) string {
	if sess.AccountName != "" {
		return sess.AccountName
	}
	return steamauth.FormatSteamID(sess.SteamID)
}

// onPlaying runs when another device starts or stops playing on the account.
func (s *SessionSupervisor) onPlaying(blocked bool, _ uint32) {
	s.playMu.Lock()
	defer s.playMu.Unlock()
	if blocked {
		s.setState(StateWaiting, sessionNoteInUse)
		s.emit(EventWarn, "You're playing on another device; idling pauses until you stop")
		return
	}
	s.setState(StateIdling, "")
	s.emit(EventGood, "The other device stopped playing; idling again")
}

// connectFailed explains a failed attempt and says how long to wait.
func (s *SessionSupervisor) connectFailed(ctx context.Context, err error) time.Duration {
	if ctx.Err() != nil {
		return 0
	}
	var le *steamcm.LogonError
	switch {
	case errors.As(err, &le) && le.AuthFailed():
		s.setState(StateFailing, sessionNoteLogin)
		s.warn(EventBad, "Steam refused the saved sign-in: "+le.Error()+". Run houridle login again. Trying again every 5 minutes.")
		return retryAfterRefused
	case errors.As(err, &le) && le.RateLimited():
		s.setState(StateWaiting, "rate limited")
		s.warn(EventWarn, "Steam is limiting sign-ins; waiting 10 minutes")
		return retryAfterLimit
	case errors.As(err, &le):
		s.setState(StateWaiting, "refused")
		s.warn(EventWarn, "Steam refused the sign-in: "+le.Error())
		return retryAfterRefused
	case errors.Is(err, steamauth.ErrNetwork):
		s.setState(StateWaiting, sessionNoteOffline)
		s.warn(EventWarn, "Can't reach Steam; retrying")
		return 0
	default:
		s.setState(StateFailing, "failed")
		s.warn(EventBad, "Couldn't sign in: "+err.Error())
		return 0
	}
}

// connectionLost handles an established connection ending.
func (s *SessionSupervisor) connectionLost(err error) time.Duration {
	var off *steamcm.LoggedOffError
	if errors.As(err, &off) {
		s.setState(StateWaiting, "signed out")
		if off.EResult == eresultLogonSessionReplaced {
			// Steam keeps one connection per saved sign-in; a newer one using
			// the same sign-in (another houridle start, status or -list) wins.
			s.emit(EventWarn, "Another houridle signed in with this sign-in and took over the connection. Reconnecting in a minute.")
		} else {
			s.emit(EventWarn, "Steam signed this session out (another sign-in on this account?). Reconnecting in a minute.")
		}
		return retryAfterLogin
	}
	s.setState(StateWaiting, sessionNoteOffline)
	s.emit(EventWarn, "Connection to Steam lost, reconnecting")
	return 0
}

// shortDuration formats whole hours as "100h" and the rest like "90m".
func shortDuration(d time.Duration) string {
	if d >= time.Hour && d%time.Hour == 0 {
		return fmt.Sprintf("%dh", int(d/time.Hour))
	}
	if d%time.Minute == 0 {
		return fmt.Sprintf("%dm", int(d/time.Minute))
	}
	return d.String()
}
