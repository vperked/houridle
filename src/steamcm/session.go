package steamcm

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"houridle/src/steamauth"
)

// Values Steam's own client reports when signing in.
const (
	protocolVersion = 65580
	clientVersion   = 1771
	osWindows10     = 16
	defaultBeat     = 9 * time.Second
	logonTimeout    = 30 * time.Second
)

// Config is who to sign in as.
type Config struct {
	SteamID      uint64
	AccountName  string // optional; sent along with the token like Steam's own client does
	RefreshToken string // a secret; never logged or printed here
	MachineName  string

	// OnPlaying is told when another device starts or stops playing on the
	// account. While blocked, Steam won't count this session's games.
	OnPlaying func(blocked bool, appID uint32)
}

// LogonError is Steam refusing the sign-in.
type LogonError struct {
	EResult  int
	Extended int    // Steam's extra detail code, 0 if none
	Server   string // the server that gave this answer
	Servers  int    // how many servers have refused (they all said no if > 1)
}

func (e *LogonError) Error() string {
	detail := fmt.Sprintf("Steam error %d", e.EResult)
	if e.Extended != 0 {
		detail += fmt.Sprintf(", detail %d", e.Extended)
	}
	if e.Servers > 1 {
		detail += fmt.Sprintf(", refused by %d servers", e.Servers)
	}
	return fmt.Sprintf("%s (%s)", steamauth.EResultText(e.EResult), detail)
}

// AuthFailed reports whether the saved sign-in is no longer good (as opposed
// to Steam being busy or the account being in use).
func (e *LogonError) AuthFailed() bool {
	switch e.EResult {
	case steamauth.EResultInvalidPassword, 8, steamauth.EResultAccessDenied, 26, steamauth.EResultExpired, 63, 65, 85:
		return true
	}
	return false
}

// EResults that are about the server, not the sign-in: another server may
// well take it. (Rate limiting is not one of these: it applies to the
// account or network, and asking more servers only makes it worse.)
const (
	eresultBusy         = 10
	eresultTryAnotherCM = 48
)

func (e *LogonError) tryAnotherServer() bool {
	switch e.EResult {
	case eresultBusy, steamauth.EResultUnavailable, eresultTryAnotherCM:
		return true
	}
	return false
}

// RateLimited reports whether Steam asked houridle to slow down.
func (e *LogonError) RateLimited() bool {
	return e.EResult == steamauth.EResultRateLimited || e.EResult == steamauth.EResultLoginThrottle
}

// LoggedOffError is Steam ending an established session.
type LoggedOffError struct{ EResult int }

func (e *LoggedOffError) Error() string {
	return fmt.Sprintf("Steam signed this session out (error %d)", e.EResult)
}

// Session is a signed-in connection to Steam that can report games as played.
type Session struct {
	c   *conn
	cfg Config

	// sessionID is written once, by logOn, before logonDone closes; every
	// reader runs after that (readLoop waits for logonDone, the heartbeat
	// and callers start after it), so it needs no lock.
	sessionID int32
	logonDone chan struct{} // closed when logOn has finished, either way

	done chan struct{}
	once sync.Once
	err  error // why it ended; set before done closes

	mu      sync.Mutex
	live    bool // signed in: OnPlaying may be called from now on
	games   []uint32
	blocked bool

	jobs    map[uint64]chan Message // requests waiting for a reply, by job ID
	nextJob uint64
}

// maxRefusals is how many different servers must refuse a sign-in before
// houridle believes it. One server saying no can be that server's quirk.
const maxRefusals = 3

// Connect signs in with the refresh token and returns once Steam accepts it.
func Connect(ctx context.Context, cfg Config) (*Session, error) {
	info, err := steamauth.ParseToken(cfg.RefreshToken)
	if err != nil || info.SteamID != cfg.SteamID || !info.CanLogOn() {
		return nil, errors.New("the saved sign-in doesn't match its account")
	}
	servers, err := ListServers(ctx)
	if err != nil {
		return nil, err
	}

	var refused *LogonError // the last "your sign-in is no good" answer
	var busy *LogonError    // the last "not this server" answer
	refusals := 0
	var netErr error
	for i, server := range servers {
		if i == maxServerTries {
			break
		}
		c, err := dial(ctx, ServerURL(server))
		if err != nil {
			if ctx.Err() != nil {
				return nil, ctx.Err()
			}
			netErr = fmt.Errorf("%w: %v", steamauth.ErrNetwork, err)
			continue
		}
		s, err := logOn(ctx, c, cfg)
		if err == nil {
			return s, nil
		}
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		var le *LogonError
		if !errors.As(err, &le) {
			// One server timing out or hanging up is that server's trouble:
			// try the next, as for a server that can't be reached at all.
			if errors.Is(err, steamauth.ErrNetwork) {
				netErr = err
				continue
			}
			return nil, err // e.g. Steam signed in the wrong account
		}
		le.Server = server
		if le.tryAnotherServer() {
			busy = le
			continue
		}
		refusals++
		le.Servers = refusals
		refused = le
		if !le.AuthFailed() || refusals >= maxRefusals {
			return nil, le
		}
	}
	switch {
	case refused != nil:
		return nil, refused
	case busy != nil:
		return nil, busy
	case netErr != nil:
		return nil, netErr
	}
	return nil, fmt.Errorf("%w: no server answered", steamauth.ErrNetwork)
}

// logOn signs in over an open connection.
func logOn(ctx context.Context, c *conn, cfg Config) (*Session, error) {
	s := &Session{c: c, cfg: cfg, done: make(chan struct{}), logonDone: make(chan struct{}), jobs: map[uint64]chan Message{}}
	defer close(s.logonDone)

	logon := make(chan Message, 1)
	go s.readLoop(logon)

	hello := new(steamauth.Encoder).Varint(1, protocolVersion).Out()
	if err := c.send(ctx, emsgClientHello, header{}, hello); err != nil {
		s.finish(err)
		return nil, fmt.Errorf("%w: %v", steamauth.ErrNetwork, err)
	}
	machine := cfg.MachineName
	if machine == "" {
		machine = "houridle"
	}
	req := new(steamauth.Encoder).
		Varint(1, protocolVersion).
		Varint(5, clientVersion).
		String(6, "english").
		Varint(7, osWindows10).
		Bool(8, false).
		Varint(21, 2).
		Varint(33, 2).
		String(96, machine).
		Bool(102, true).
		String(108, cfg.RefreshToken)
	if cfg.AccountName != "" {
		req.String(50, cfg.AccountName)
	}
	if err := c.send(ctx, emsgClientLogon, header{steamID: cfg.SteamID}, req.Out()); err != nil {
		s.finish(err)
		return nil, fmt.Errorf("%w: %v", steamauth.ErrNetwork, err)
	}

	timer := time.NewTimer(logonTimeout)
	defer timer.Stop()
	select {
	case m := <-logon:
		return s.finishLogon(m)
	case <-s.done:
		// Steam often answers a refused sign-in and hangs up straight away;
		// if both have happened, the answer is what matters.
		select {
		case m := <-logon:
			if _, err := s.finishLogon(m); err != nil {
				return nil, err
			}
		default:
		}
		return nil, fmt.Errorf("%w: connection closed while signing in", steamauth.ErrNetwork)
	case <-timer.C:
		s.finish(errors.New("sign-in timed out"))
		return nil, fmt.Errorf("%w: Steam didn't answer the sign-in", steamauth.ErrNetwork)
	case <-ctx.Done():
		s.finish(ctx.Err())
		return nil, ctx.Err()
	}
}

func (s *Session) finishLogon(m Message) (*Session, error) {
	resp, err := steamauth.Decode(m.Body)
	if err != nil {
		s.finish(err)
		return nil, err
	}
	eresult := 2
	if resp.Has(1) {
		eresult = int(int32(resp.Int(1)))
	}
	if eresult != steamauth.EResultOK {
		le := &LogonError{EResult: eresult, Extended: int(int32(resp.Int(10)))}
		s.finish(le)
		return nil, le
	}
	// The session must be for the account the token belongs to.
	if got := m.Header.Uint(1); got != s.cfg.SteamID {
		err := errors.New("Steam signed in a different account than the saved one; disconnected")
		s.finish(err)
		return nil, err
	}
	s.sessionID = int32(m.Header.Int(2))
	s.mu.Lock()
	s.live = true
	s.mu.Unlock()

	beat := time.Duration(resp.Int(3)) * time.Second
	if beat <= 0 {
		beat = defaultBeat
	}
	go s.heartbeat(beat)
	go s.keepAlive(PingEvery, PingTimeout)
	return s, nil
}

// readLoop handles everything Steam sends until the connection ends.
func (s *Session) readLoop(logon chan<- Message) {
	for {
		msgs, err := s.c.read(context.Background())
		if err != nil {
			s.finish(err)
			return
		}
		for _, m := range msgs {
			switch m.EMsg {
			case emsgClientLogOnResponse:
				select {
				case logon <- m:
				default:
				}
				// Let logOn finish with the answer before handling anything
				// sent after it, so those messages see the signed-in session.
				select {
				case <-s.logonDone:
				case <-s.done:
					return
				}
			case emsgClientLoggedOff:
				d, _ := steamauth.Decode(m.Body)
				code := steamauth.EResultFail
				if d != nil && d.Has(1) {
					code = int(int32(d.Int(1)))
				}
				s.finish(&LoggedOffError{EResult: code})
				return
			case emsgServiceMethodResp:
				s.deliver(m)
			case emsgPlayingSessionState:
				if d, err := steamauth.Decode(m.Body); err == nil {
					s.setBlocked(d.Bool(2), uint32(d.Uint(3)))
				}
			case emsgKickPlayingSession:
				s.setBlocked(true, 0)
			}
		}
	}
}

// setBlocked records that another device is (or has stopped) playing. When
// it stops, the games are reported again; houridle never takes the account
// back from someone who is playing for real.
//
// OnPlaying is only called once the sign-in has succeeded. A change that
// arrives with the sign-in itself may be reported just before Connect
// returns; callers read Blocked after Connect for the state at that point.
func (s *Session) setBlocked(blocked bool, app uint32) {
	s.mu.Lock()
	was := s.blocked
	s.blocked = blocked
	live := s.live
	games := append([]uint32(nil), s.games...)
	s.mu.Unlock()

	if !live {
		return
	}
	if s.cfg.OnPlaying != nil && was != blocked {
		s.cfg.OnPlaying(blocked, app)
	}
	if was && !blocked && len(games) > 0 {
		s.send(emsgClientGamesPlayed, gamesPlayedBody(games))
	}
}

func (s *Session) heartbeat(every time.Duration) {
	t := time.NewTicker(every)
	defer t.Stop()
	for {
		select {
		case <-s.done:
			return
		case <-t.C:
			if err := s.send(emsgClientHeartBeat, nil); err != nil {
				return
			}
		}
	}
}

// PingEvery and PingTimeout set how often houridle checks that Steam is
// still there, and how long it waits for the answer. Steam never answers a
// heartbeat, so without this a connection that silently died (a network
// change, a laptop waking up) would only be noticed when the operating
// system gives up on it, which can take a quarter of an hour. They are
// generous so a merely slow server never causes a reconnect. Variables only
// so tests can shorten them.
var (
	PingEvery   = 2 * time.Minute
	PingTimeout = 30 * time.Second
)

// keepAlive pings Steam (a websocket ping, answered by the server itself)
// and ends the session if no answer comes. readLoop is always reading, which
// is what lets the answer arrive.
func (s *Session) keepAlive(every, timeout time.Duration) {
	t := time.NewTicker(every)
	defer t.Stop()
	for {
		select {
		case <-s.done:
			return
		case <-t.C:
			ctx, cancel := context.WithTimeout(context.Background(), timeout)
			err := s.c.ws.Ping(ctx)
			cancel()
			if err != nil {
				s.finish(fmt.Errorf("Steam stopped answering: %w", err))
				return
			}
		}
	}
}

func (s *Session) send(emsg uint32, body []byte) error {
	return s.sendWithin(10*time.Second, emsg, body)
}

func (s *Session) sendWithin(timeout time.Duration, emsg uint32, body []byte) error {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	err := s.c.send(ctx, emsg, header{steamID: s.cfg.SteamID, sessionID: s.sessionID}, body)
	if err != nil {
		s.finish(err)
	}
	return err
}

func gamesPlayedBody(appIDs []uint32) []byte {
	b := new(steamauth.Encoder)
	for _, id := range appIDs {
		b.Message(1, new(steamauth.Encoder).Fixed64(2, uint64(id)))
	}
	return b.Varint(2, osWindows10).Out()
}

// SetGames reports exactly these apps as being played (none stops them all).
func (s *Session) SetGames(appIDs []uint32) error {
	s.mu.Lock()
	s.games = append([]uint32(nil), appIDs...)
	blocked := s.blocked
	s.mu.Unlock()
	if blocked {
		return nil // sent when the other device stops
	}
	return s.send(emsgClientGamesPlayed, gamesPlayedBody(appIDs))
}

// Blocked reports whether another device is playing on the account.
func (s *Session) Blocked() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.blocked
}

// Done is closed when the connection ends; Err then says why.
func (s *Session) Done() <-chan struct{} { return s.done }

// Err is why the session ended (nil while it is running).
func (s *Session) Err() error {
	select {
	case <-s.done:
		return s.err
	default:
		return nil
	}
}

func (s *Session) finish(err error) {
	s.once.Do(func() {
		s.err = err
		close(s.done)
		s.c.close()
	})
}

// Close stops the games and disconnects.
//
// It deliberately does NOT send Steam a LogOff message. For a sign-in made
// with a saved token, LogOff means "sign out": Steam ends that token for
// good, and the next start is refused with "access denied" until you log in
// again. Just dropping the connection leaves the token alone.
func (s *Session) Close() {
	select {
	case <-s.done:
		return
	default:
	}
	// Briefly: on a stalled connection this must not hold up shutdown.
	s.sendWithin(time.Second, emsgClientGamesPlayed, gamesPlayedBody(nil))
	s.finish(nil)
}
