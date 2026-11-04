package steamauth

import (
	"errors"
	"fmt"
	"time"
)

// Manager ties the saved session to Steam: it loads it, checks it offline,
// and keeps the stored token fresh. (Whether Steam still accepts the token
// is only known by signing in, which is what idling and `status` do; Steam's
// web service can't be asked, because it refuses this kind of token.)
type Manager struct {
	Client *Client
	Store  *Store
	Now    func() time.Time // for tests; time.Now when nil
}

// NewManager uses Steam's real Web API and the per-user session file.
func NewManager() (*Manager, error) {
	store, err := DefaultStore()
	if err != nil {
		return nil, err
	}
	return &Manager{Client: NewClient(), Store: store}, nil
}

func (m *Manager) now() time.Time {
	if m.Now != nil {
		return m.Now()
	}
	return time.Now()
}

// Current loads the saved session and checks it without using the network.
func (m *Manager) Current() (Session, error) {
	sess, err := m.Store.Load()
	if err != nil {
		return Session{}, err
	}
	if info, perr := ParseToken(sess.RefreshToken); perr == nil && info.Expired(m.now()) {
		return sess, ErrSessionExpired
	}
	return sess, nil
}

// Adopt saves a replacement refresh token Steam issued for sess, but only
// if it is for the same account and can still sign in. An empty or
// unchanged token is not an error: there is nothing to do.
func (m *Manager) Adopt(sess Session, renewed string) (Session, error) {
	if renewed == "" || renewed == sess.RefreshToken {
		return sess, nil
	}
	info, perr := ParseToken(renewed)
	if perr != nil || info.SteamID != sess.SteamID || !info.CanLogOn() {
		return sess, errors.New("Steam sent a replacement token for a different account; keeping the old one")
	}
	sess.RefreshToken = renewed
	sess.Expires = info.Expires
	sess.SavedAt = m.now()
	if err := m.Store.Save(sess); err != nil {
		return sess, fmt.Errorf("couldn't save the renewed sign-in: %w", err)
	}
	return sess, nil
}

// ErrDifferentAccount is returned by Remember when a different account is
// already saved and the caller didn't agree to replace it.
var ErrDifferentAccount = errors.New("a different Steam account is already signed in")

// Remember saves a finished sign-in. If another account is already saved,
// confirmReplace decides whether to overwrite it (nil means never), so one
// saved session can't silently switch accounts.
func (m *Manager) Remember(res *Result, confirmReplace func(old Session) bool) error {
	if res == nil || !ValidSteamID(res.SteamID) {
		return errors.New("no account to save")
	}
	if old, err := m.Store.Load(); err == nil && old.SteamID != res.SteamID {
		if confirmReplace == nil || !confirmReplace(old) {
			return ErrDifferentAccount
		}
	}
	info, err := ParseToken(res.RefreshToken)
	if err != nil {
		return err
	}
	return m.Store.Save(Session{
		AccountName:  res.AccountName,
		SteamID:      res.SteamID,
		RefreshToken: res.RefreshToken,
		SavedAt:      m.now(),
		Expires:      info.Expires,
	})
}
