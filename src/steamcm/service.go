package steamcm

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"houridle/src/steamauth"
)

const callTimeout = 30 * time.Second

// deliver hands a service reply to whoever is waiting for it.
func (s *Session) deliver(m Message) {
	id := m.Header.Uint(11) // jobid_target
	s.mu.Lock()
	ch := s.jobs[id]
	delete(s.jobs, id)
	s.mu.Unlock()
	if ch != nil {
		ch <- m // buffered: never blocks the reader
	}
}

// Call runs one of Steam's service methods (such as "Player.GetOwnedGames#1")
// on the signed-in session and returns the reply body.
func (s *Session) Call(ctx context.Context, method string, body []byte) (*steamauth.Decoded, error) {
	ch := make(chan Message, 1)
	s.mu.Lock()
	s.nextJob++
	id := s.nextJob
	s.jobs[id] = ch
	s.mu.Unlock()
	forget := func() {
		s.mu.Lock()
		delete(s.jobs, id)
		s.mu.Unlock()
	}

	sendCtx, cancel := context.WithTimeout(ctx, callTimeout)
	defer cancel()
	err := s.c.send(sendCtx, emsgServiceMethodCall, header{
		steamID: s.cfg.SteamID, sessionID: s.sessionID, jobSource: id, jobName: method,
	}, body)
	if err != nil {
		forget()
		s.finish(err)
		return nil, fmt.Errorf("%w: %v", steamauth.ErrNetwork, err)
	}

	timer := time.NewTimer(callTimeout)
	defer timer.Stop()
	select {
	case m := <-ch:
		return reply(m)
	case <-s.done:
		forget()
		select { // the answer may have come just before the connection ended
		case m := <-ch:
			return reply(m)
		default:
		}
		return nil, fmt.Errorf("%w: connection closed", steamauth.ErrNetwork)
	case <-timer.C:
		forget()
		return nil, fmt.Errorf("%w: Steam didn't answer", steamauth.ErrNetwork)
	case <-ctx.Done():
		forget()
		return nil, ctx.Err()
	}
}

// reply turns a service method's answer into its body, or Steam's error.
func reply(m Message) (*steamauth.Decoded, error) {
	if code := int32(m.Header.Int(13)); m.Header.Has(13) && code != steamauth.EResultOK {
		return nil, &steamauth.Error{EResult: int(code), Message: m.Header.Str(14)}
	}
	return steamauth.Decode(m.Body)
}

// OwnedGame is a game or app the account owns.
type OwnedGame struct {
	AppID   uint32
	Name    string
	Minutes int // total playtime
}

// OwnedGames asks Steam which games the signed-in account owns, most played
// first. This works however the profile's privacy is set.
func (s *Session) OwnedGames(ctx context.Context) ([]OwnedGame, error) {
	req := new(steamauth.Encoder).
		Varint(1, s.cfg.SteamID).
		Bool(2, true).  // include names and playtime
		Bool(3, true).  // include free-to-play games you've played
		Bool(6, false). // include apps Steam hasn't vetted
		String(7, "english")
	resp, err := s.Call(ctx, "Player.GetOwnedGames#1", req.Out())
	if err != nil {
		return nil, err
	}
	var out []OwnedGame
	for _, g := range resp.Messages(2) {
		id := g.Int(1)
		if id <= 0 {
			continue
		}
		out = append(out, OwnedGame{
			AppID:   uint32(id),
			Name:    strings.TrimSpace(g.Str(2)),
			Minutes: int(int32(g.Int(4))),
		})
	}
	if len(out) == 0 && resp.Uint(1) > 0 {
		return nil, errors.New("Steam listed games but houridle couldn't read them")
	}
	slices.SortFunc(out, func(a, b OwnedGame) int { return b.Minutes - a.Minutes })
	return out, nil
}

// RenewRefreshToken asks Steam, over this signed-in connection, for a
// replacement for the refresh token. Steam's web service doesn't accept
// this kind of token, but its servers do. The result is empty when Steam
// decides the token doesn't need replacing yet.
func (s *Session) RenewRefreshToken(ctx context.Context) (string, error) {
	req := new(steamauth.Encoder).
		String(1, s.cfg.RefreshToken).
		Fixed64(2, s.cfg.SteamID).
		Varint(3, 1) // allow renewal
	resp, err := s.Call(ctx, "Authentication.GenerateAccessTokenForApp#1", req.Out())
	if err != nil {
		return "", err
	}
	return resp.Str(2), nil
}
