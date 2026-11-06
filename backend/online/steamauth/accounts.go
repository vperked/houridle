package steamauth

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
)

// Accounts is every Steam account saved on this computer. The default one
// lives in session.dat, where houridle has always kept its sign-in, so a
// single-account setup is unchanged; any others are in accounts/<id>.dat.
// Commands use the default unless told otherwise (-account).
type Accounts struct{ Dir string }

// SavedAccount is one saved account and the file it lives in.
type SavedAccount struct {
	Session
	Store   *Store
	Default bool
	Err     error // set if the file couldn't be read (damaged)
}

// Label is the account name, or the SteamID if the name isn't known.
func (a SavedAccount) Label() string {
	if a.AccountName != "" {
		return a.AccountName
	}
	return FormatSteamID(a.SteamID)
}

// LockKey is the account's idle lock key: "" for the default account (it
// shares the lock with the Steam client), else its SteamID.
func (a SavedAccount) LockKey() string {
	if a.Default {
		return ""
	}
	return FormatSteamID(a.SteamID)
}

// DefaultAccounts is the per-user config folder (see DefaultStore).
func DefaultAccounts() (*Accounts, error) {
	store, err := DefaultStore()
	if err != nil {
		return nil, err
	}
	return &Accounts{Dir: filepath.Dir(store.Path)}, nil
}

func (a *Accounts) defaultStore() *Store { return &Store{Path: filepath.Join(a.Dir, "session.dat")} }

func (a *Accounts) otherDir() string { return filepath.Join(a.Dir, "accounts") }

func (a *Accounts) otherStore(id uint64) *Store {
	return &Store{Path: filepath.Join(a.otherDir(), strconv.FormatUint(id, 10)+".dat")}
}

// List returns every saved account, the default first, then by name.
func (a *Accounts) List() []SavedAccount {
	var out []SavedAccount
	def := a.defaultStore()
	if sess, err := def.Load(); !errors.Is(err, ErrNotLoggedIn) {
		out = append(out, SavedAccount{Session: sess, Store: def, Default: true, Err: err})
	}
	entries, _ := os.ReadDir(a.otherDir())
	var others []SavedAccount
	for _, e := range entries {
		id, err := strconv.ParseUint(strings.TrimSuffix(e.Name(), ".dat"), 10, 64)
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".dat") || err != nil {
			continue
		}
		store := a.otherStore(id)
		sess, err := store.Load()
		if err == nil && sess.SteamID != id {
			err = ErrCorrupt // the file name and the token disagree
		}
		if err != nil {
			sess.SteamID = id
		}
		others = append(others, SavedAccount{Session: sess, Store: store, Err: err})
	}
	slices.SortFunc(others, func(x, y SavedAccount) int {
		return strings.Compare(strings.ToLower(x.Label()), strings.ToLower(y.Label()))
	})
	return append(out, others...)
}

// Multiple reports whether more than one account is saved.
func (a *Accounts) Multiple() bool { return len(a.List()) > 1 }

// Find picks an account by name (any case) or SteamID; "" is the default.
func (a *Accounts) Find(selector string) (SavedAccount, error) {
	all := a.List()
	selector = strings.TrimSpace(selector)
	if selector == "" {
		if len(all) > 0 && all[0].Default {
			return all[0], all[0].Err
		}
		if len(all) == 1 {
			return all[0], all[0].Err
		}
		if len(all) == 0 {
			return SavedAccount{}, ErrNotLoggedIn
		}
		return SavedAccount{}, fmt.Errorf("several accounts are saved and none is the default: pass -account (%s)", a.names(all))
	}
	for _, acc := range all {
		if strings.EqualFold(acc.AccountName, selector) || FormatSteamID(acc.SteamID) == selector {
			return acc, acc.Err
		}
	}
	if len(all) == 0 {
		return SavedAccount{}, ErrNotLoggedIn
	}
	return SavedAccount{}, fmt.Errorf("no saved account %q (saved: %s; add one with houridle login)", selector, a.names(all))
}

func (a *Accounts) names(all []SavedAccount) string {
	var names []string
	for _, acc := range all {
		names = append(names, acc.Label())
	}
	return strings.Join(names, ", ")
}

// StoreFor is where a sign-in for this account belongs: its existing file,
// else session.dat if no account is the default yet, else a new file.
func (a *Accounts) StoreFor(id uint64) *Store {
	for _, acc := range a.List() {
		if acc.SteamID == id {
			return acc.Store
		}
	}
	if _, err := a.defaultStore().Load(); errors.Is(err, ErrNotLoggedIn) {
		return a.defaultStore()
	}
	return a.otherStore(id)
}

// MakeDefault makes the chosen account the default, by swapping its file
// with session.dat.
func (a *Accounts) MakeDefault(selector string) (SavedAccount, error) {
	acc, err := a.Find(selector)
	if err != nil || acc.Default {
		return acc, err
	}
	def := a.defaultStore()
	old, oldErr := def.Load()
	switch {
	case errors.Is(oldErr, ErrNotLoggedIn):
		if err := os.Rename(acc.Store.Path, def.Path); err != nil {
			return acc, err
		}
	case oldErr != nil:
		return acc, errors.New("the current default sign-in is damaged; remove it first with houridle logout")
	default:
		// Three renames, each atomic; a crash between them leaves the old
		// default in a temp file next to session.dat, never lost.
		tmp := def.Path + ".swap"
		if err := os.Rename(def.Path, tmp); err != nil {
			return acc, err
		}
		if err := os.Rename(acc.Store.Path, def.Path); err != nil {
			os.Rename(tmp, def.Path)
			return acc, err
		}
		if err := os.MkdirAll(a.otherDir(), 0o700); err != nil {
			return acc, err
		}
		if err := os.Rename(tmp, a.otherStore(old.SteamID).Path); err != nil {
			return acc, err
		}
	}
	acc.Store, acc.Default = def, true
	return acc, nil
}

// NewManagerFor is NewManager for a chosen account ("" = the default).
func NewManagerFor(selector string) (*Manager, SavedAccount, error) {
	accts, err := DefaultAccounts()
	if err != nil {
		return nil, SavedAccount{}, err
	}
	acc, err := accts.Find(selector)
	if acc.Store == nil {
		return nil, acc, err
	}
	// A damaged file still gets a manager (with the error), so logout can
	// remove it.
	return &Manager{Client: NewClient(), Store: acc.Store}, acc, err
}

// FileName names a per-account file next to houridle: base+ext for the
// default account (and the Steam client, SteamID 0), base-<name>+ext for
// the others, so accounts idling side by side keep their own logs and
// histories.
func (a SavedAccount) FileName(base, ext string) string {
	if a.SteamID == 0 || a.Default {
		return base + ext
	}
	name := unsafeFileName.ReplaceAllString(a.Label(), "_")
	if name == "" {
		name = FormatSteamID(a.SteamID)
	}
	return base + "-" + name + ext
}

var unsafeFileName = regexp.MustCompile(`[^A-Za-z0-9._-]+`)
