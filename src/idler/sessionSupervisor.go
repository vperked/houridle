package idler

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"houridle/src/steam"
	"houridle/src/steamauth"
	"houridle/src/steamcm"
)

// Runner is what the console, the tray and the log need from an idling
// session: start it, wait for it to wind down, and read each game's status.
// Supervisor idles through the Steam client; SessionSupervisor through a
// signed-in houridle session.
type Runner interface {
	Start(ctx context.Context)
	Wait()
	Views() []View
}

const (
	reconnectMin      = 5 * time.Second
	reconnectMax      = 5 * time.Minute
	retryAfterLogin   = time.Minute         // sign-in refused; maybe you ran houridle login since
	retryAfterRefused = 5 * time.Minute     // Steam refused the sign-in; each try is several attempts
	retryAfterLimit   = 10 * time.Minute    // Steam asked us to slow down
	renewEvery        = 24 * time.Hour      // check the saved token on long runs
	renewWithin       = 90 * 24 * time.Hour // renew when it has less than this left
	sessionNoteLogin  = "sign in needed"
	sessionNoteOnline = "offline"
	sessionNoteInUse  = "in use elsewhere"
)

// SessionSupervisor idles every game over one signed-in Steam connection,
// using the session saved by `houridle login`. It reconnects after drops.
type SessionSupervisor struct {
	games   []*steam.Game
	mgr     *steamauth.Manager
	onEvent func(Event)
	wg      sync.WaitGroup

	// playMu orders the state set right after signing in with the state
	// onPlaying sets, so a change reported in between can't be overwritten
	// by the older reading.
	playMu sync.Mutex

	mu      sync.Mutex // guards the fields below
	state   State
	since   time.Time
	banked  time.Duration
	note    string
	lastMsg string // last warning/error shown, so retries don't repeat it

	// unsaved is a renewed sign-in Steam issued that couldn't be written to
	// disk. It is used for the next connections (and saving retried), so a
	// failed save never sends houridle back to the token it replaced.
	unsaved *steamauth.Session
}

// NewSession prepares a supervisor for games using the saved session in mgr.
func NewSession(games []*steam.Game, mgr *steamauth.Manager, onEvent func(Event)) *SessionSupervisor {
	return &SessionSupervisor{games: games, mgr: mgr, onEvent: onEvent, state: StateStarting}
}

// Start connects in the background and returns immediately.
func (s *SessionSupervisor) Start(ctx context.Context) {
	s.wg.Add(1)
	go func() {
		defer s.wg.Done()
		s.run(ctx)
	}()
}

// Wait blocks until the connection has closed (after ctx is cancelled).
func (s *SessionSupervisor) Wait() { s.wg.Wait() }

// Views returns every game's status; they all share the connection's.
func (s *SessionSupervisor) Views() []View {
	s.mu.Lock()
	defer s.mu.Unlock()
	idled := s.banked
	if s.state == StateIdling {
		idled += time.Since(s.since)
	}
	out := make([]View, len(s.games))
	for i, g := range s.games {
		out[i] = View{Game: g, State: s.state, Idled: idled, Note: s.note}
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

func (s *SessionSupervisor) setState(state State, note string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.state == StateIdling && state != StateIdling {
		s.banked += time.Since(s.since)
	}
	if state == StateIdling && s.state != StateIdling {
		s.since = time.Now()
	}
	s.state = state
	s.note = note
}

func (s *SessionSupervisor) appIDs() []uint32 {
	ids := make([]uint32, len(s.games))
	for i, g := range s.games {
		ids[i] = g.AppID
	}
	return ids
}

// run is the connect / idle / reconnect loop. It returns when ctx ends.
func (s *SessionSupervisor) run(ctx context.Context) {
	defer func() { s.setState(StateStopped, s.currentNote()) }()
	backoff := reconnectMin
	for ctx.Err() == nil {
		s.setState(StateStarting, "")
		wait, connected := s.once(ctx)
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
// connection ends. wait is how long to pause before the next attempt (0 for
// the default backoff); connected says whether we got signed in.
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
	})
	if err != nil {
		return s.connectFailed(ctx, err), false
	}
	defer cs.Close()

	if err := cs.SetGames(s.appIDs()); err != nil {
		s.warn(EventWarn, "Couldn't tell Steam which games to idle: "+err.Error())
		return 0, true
	}
	s.mu.Lock()
	s.lastMsg = ""
	s.mu.Unlock()
	s.playMu.Lock()
	if cs.Blocked() {
		s.setState(StateWaiting, sessionNoteInUse)
	} else {
		s.setState(StateIdling, "")
		s.emit(EventGood, fmt.Sprintf("Signed in as %s, idling %d game(s)", accountLabel(sess), len(s.games)))
	}
	s.playMu.Unlock()

	s.renewSignIn(ctx, cs, sess)
	renew := time.NewTicker(renewEvery)
	defer renew.Stop()
	for {
		select {
		case <-ctx.Done():
			return 0, true
		case <-renew.C:
			if cur, err := s.current(); err == nil {
				s.renewSignIn(ctx, cs, cur)
			}
		case <-cs.Done():
			return s.connectionLost(cs.Err()), true
		}
	}
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
		s.setState(StateWaiting, sessionNoteOnline)
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
		s.emit(EventWarn, "Steam signed this session out (another sign-in on this account?). Reconnecting in a minute.")
		return retryAfterLogin
	}
	s.setState(StateWaiting, sessionNoteOnline)
	s.emit(EventWarn, "Connection to Steam lost, reconnecting")
	return 0
}
