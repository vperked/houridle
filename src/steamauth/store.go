package steamauth

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strconv"
	"time"
)

// Errors the commands turn into advice.
var (
	ErrNotLoggedIn    = errors.New("not signed in: run houridle login")
	ErrSessionExpired = errors.New("Steam no longer accepts the saved sign-in (it expired, was revoked, or Steam wants it verified again): run houridle login")
	ErrCorrupt        = errors.New("the saved sign-in is damaged or doesn't match its account: run houridle login")
)

const sessionVersion = 1

// Session is a saved sign-in: whose it is and the refresh token that lets
// houridle sign in as them without a password.
//
// The refresh token is a secret. Session prints without it, so it can't end
// up in a log by accident.
type Session struct {
	AccountName  string
	SteamID      uint64
	RefreshToken string
	SavedAt      time.Time
	Expires      time.Time // when the refresh token stops working (from the token)
}

// String shows who the session is for, never the token.
func (s Session) String() string {
	return fmt.Sprintf("session{%s %d}", s.AccountName, s.SteamID)
}

// GoString keeps %#v from printing the token too.
func (s Session) GoString() string { return s.String() }

// file is the stored form. The SteamID is a string so no JSON reader can
// round it as a float.
type file struct {
	Version      int       `json:"version"`
	AccountName  string    `json:"accountName"`
	SteamID      string    `json:"steamId"`
	RefreshToken string    `json:"refreshToken"`
	SavedAt      time.Time `json:"savedAt"`
}

// Store keeps the session in one file that only the current user can read.
// On Windows the contents are also encrypted for the user with DPAPI.
type Store struct{ Path string }

// DefaultStore is the per-user file in the OS config folder (%AppData% on
// Windows, ~/.config on Linux).
func DefaultStore() (*Store, error) {
	dir, err := os.UserConfigDir()
	if err != nil {
		return nil, fmt.Errorf("can't find a place to keep your sign-in: %w", err)
	}
	return &Store{Path: filepath.Join(dir, "houridle", "session.dat")}, nil
}

// Load returns the saved session. It fails with ErrNotLoggedIn if there is
// none, and ErrCorrupt if the file can't be read, is for another version, or
// its token belongs to a different account than the file claims.
func (s *Store) Load() (Session, error) {
	raw, err := os.ReadFile(s.Path)
	if errors.Is(err, fs.ErrNotExist) {
		return Session{}, ErrNotLoggedIn
	}
	if err != nil {
		return Session{}, fmt.Errorf("couldn't read the saved sign-in: %w", err)
	}
	plain, err := unprotect(raw)
	if err != nil {
		return Session{}, ErrCorrupt
	}
	var f file
	if json.Unmarshal(plain, &f) != nil || f.Version != sessionVersion {
		return Session{}, ErrCorrupt
	}
	id, err := strconv.ParseUint(f.SteamID, 10, 64)
	if err != nil || !ValidSteamID(id) {
		return Session{}, ErrCorrupt
	}
	info, err := ParseToken(f.RefreshToken)
	if err != nil || info.SteamID != id || !info.CanLogOn() {
		return Session{}, ErrCorrupt
	}
	return Session{
		AccountName:  f.AccountName,
		SteamID:      id,
		RefreshToken: f.RefreshToken,
		SavedAt:      f.SavedAt,
		Expires:      info.Expires,
	}, nil
}

// Save writes the session, replacing any earlier one. It refuses a token
// that isn't for the account named in the session.
func (s *Store) Save(sess Session) error {
	info, err := ParseToken(sess.RefreshToken)
	if err != nil || info.SteamID != sess.SteamID || !ValidSteamID(sess.SteamID) {
		return errors.New("refusing to save a sign-in whose token doesn't match its account")
	}
	if sess.SavedAt.IsZero() {
		sess.SavedAt = time.Now()
	}
	data, err := json.Marshal(file{
		Version:      sessionVersion,
		AccountName:  sess.AccountName,
		SteamID:      strconv.FormatUint(sess.SteamID, 10),
		RefreshToken: sess.RefreshToken,
		SavedAt:      sess.SavedAt.UTC(),
	})
	if err != nil {
		return err
	}
	sealed, err := protect(data)
	if err != nil {
		return fmt.Errorf("couldn't protect the saved sign-in: %w", err)
	}

	dir := filepath.Dir(s.Path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	// Write to a private temp file, then rename over the real one, so a crash
	// never leaves a half-written (or world-readable) session.
	tmp, err := os.CreateTemp(dir, "session-*.tmp")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	if err := tmp.Chmod(0o600); err != nil && !permsUnsupported {
		tmp.Close()
		return err
	}
	if _, err := tmp.Write(sealed); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), s.Path)
}

// Delete removes the saved session. Not having one is not an error.
func (s *Store) Delete() error {
	err := os.Remove(s.Path)
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	return nil
}
