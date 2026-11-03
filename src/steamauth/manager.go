package steamauth

import (
	"context"
	"errors"
	"fmt"
	"time"
)

// Manager ties the saved session to Steam: it loads it, checks it is still
// good, and keeps the stored token fresh.
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

// rejects are the answers that mean the token itself is no good (as opposed
// to Steam being busy): invalid, denied, revoked or expired.
func rejected(err error) bool {
	var se *Error
	if !errors.As(err, &se) {
		return false
	}
	switch se.EResult {
	case EResultInvalidPassword, 8, EResultAccessDenied, 26, EResultExpired:
		return true
	}
	return false
}

// Refresh asks Steam whether the saved session still works, and keeps the
// stored token up to date if Steam hands out a new one. It returns:
//   - ErrNotLoggedIn / ErrCorrupt / ErrSessionExpired when you need to sign in again
//   - an error wrapping ErrNetwork when Steam couldn't be reached, in which
//     case the saved session is left alone (it may well still be good)
func (m *Manager) Refresh(ctx context.Context) (Session, error) {
	sess, err := m.Current()
	if err != nil {
		return sess, err
	}
	_, renewed, err := m.Client.AccessToken(ctx, sess.RefreshToken, sess.SteamID, true)
	if err != nil {
		if rejected(err) {
			return sess, ErrSessionExpired
		}
		return sess, err
	}
	if renewed == "" || renewed == sess.RefreshToken {
		return sess, nil
	}
	// Steam issued a replacement. Keep it only if it is for the same account.
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
