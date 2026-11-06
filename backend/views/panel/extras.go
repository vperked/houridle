package panel

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"time"

	"houridle/backend/online/steamauth"
	"houridle/backend/run/config"
	"houridle/backend/run/handover"
	"houridle/backend/run/idler"
	"houridle/backend/views/web"
)

// extraRun is another saved account idling alongside the main one, with
// its own Steam connection and its own idle lock.
type extraRun struct {
	label  string
	mode   string
	sup    *idler.SessionSupervisor
	cancel context.CancelFunc
	lock   *handover.Lock
}

// SetExtra starts or stops idling another saved account alongside the
// main one: its card drops ("cards") or every game it owns ("all").
func (c *Controller) SetExtra(steamID, mode string, on bool) error {
	if on && mode != "cards" && mode != "all" {
		return errors.New("pick card drops or every game")
	}
	accts, err := steamauth.DefaultAccounts()
	if err != nil {
		return err
	}
	acct, err := accts.Find(steamID)
	if err != nil {
		return err
	}
	c.op.Lock()
	defer c.op.Unlock()
	c.stopExtra(acct.SteamID)
	if on {
		c.mu.Lock()
		main := c.src.signedIn() && c.src.account.SteamID == acct.SteamID
		c.mu.Unlock()
		if main {
			return errors.New("that's the account idling already; pick another")
		}
		if err := c.startExtra(acct, mode); err != nil {
			return err
		}
	}
	return c.updateConfig(func(cfg *config.Config) {
		cfg.Extras = slices.DeleteFunc(cfg.Extras, func(e config.ExtraAccount) bool { return e.SteamID == steamID })
		if on {
			cfg.Extras = append(cfg.Extras, config.ExtraAccount{SteamID: steamID, Mode: mode, On: true})
		}
	})
}

// startExtra starts one extra account (call holding op).
func (c *Controller) startExtra(acct steamauth.SavedAccount, mode string) error {
	if acct.Err != nil {
		return fmt.Errorf("%s's saved sign-in is damaged; sign in again", acct.Label())
	}
	lock, err := handover.TakeLockFor(acct.LockKey()) // each account has its own
	if err != nil {
		return fmt.Errorf("another houridle is idling %s already", acct.Label())
	}
	label := acct.Label()
	mgr := &steamauth.Manager{Client: steamauth.NewClient(), Store: acct.Store}
	sup := idler.NewSessionWith(nil, mgr, idler.SessionOptions{Cards: mode == "cards", All: mode == "all"}, func(e idler.Event) {
		e.Text = label + ": " + e.Text
		c.emit(e)
	})
	ctx, cancel := context.WithCancel(context.Background())
	r := &extraRun{label: label, mode: mode, sup: sup, cancel: cancel, lock: lock}
	c.mu.Lock()
	if c.extras == nil {
		c.extras = map[uint64]*extraRun{}
	}
	c.extras[acct.SteamID] = r
	c.mu.Unlock()
	sup.Start(ctx)
	c.logf("start", fmt.Sprintf("also idling %s (%s)", label, map[string]string{"cards": "card drops", "all": "every game"}[mode]))
	return nil
}

// stopExtra stops one extra account, if it's running (call holding op).
func (c *Controller) stopExtra(id uint64) {
	c.mu.Lock()
	r := c.extras[id]
	delete(c.extras, id)
	c.mu.Unlock()
	if r == nil {
		return
	}
	r.cancel()
	r.sup.Wait()
	r.lock.Release()
	c.logf("stop", "stopped idling "+r.label)
}

// stopExtras stops every extra account (houridle is exiting; they resume
// on the next start).
func (c *Controller) stopExtras() {
	c.mu.Lock()
	ids := make([]uint64, 0, len(c.extras))
	for id := range c.extras {
		ids = append(ids, id)
	}
	c.mu.Unlock()
	for _, id := range ids {
		c.stopExtra(id)
	}
}

// resumeExtras starts the extra accounts that were idling last time.
func (c *Controller) resumeExtras() {
	cfg, _ := c.config()
	accts, err := steamauth.DefaultAccounts()
	if err != nil {
		return
	}
	c.op.Lock()
	defer c.op.Unlock()
	for _, e := range cfg.Extras {
		if !e.On {
			continue
		}
		acct, err := accts.Find(e.SteamID)
		if err == nil {
			c.mu.Lock()
			main := c.src.signedIn() && c.src.account.SteamID == acct.SteamID
			c.mu.Unlock()
			if main {
				continue
			}
			err = c.startExtra(acct, e.Mode)
		}
		if err != nil {
			c.emit(idler.Event{At: time.Now(), Level: idler.EventWarn, Text: "Couldn't start idling another account again: " + err.Error()})
		}
	}
}

func (c *Controller) extrasInfo(cfg config.Config, saved []steamauth.SavedAccount) []web.ExtraInfo {
	out := []web.ExtraInfo{}
	c.mu.Lock()
	runs := make(map[uint64]*extraRun, len(c.extras))
	for id, r := range c.extras {
		runs[id] = r
	}
	main := c.src
	c.mu.Unlock()
	modes := map[string]string{}
	for _, e := range cfg.Extras {
		modes[e.SteamID] = e.Mode
	}
	for _, a := range saved {
		if a.Err != nil || main.signedIn() && main.account.SteamID == a.SteamID {
			continue
		}
		id := steamauth.FormatSteamID(a.SteamID)
		info := web.ExtraInfo{SteamID: id, Name: a.Label(), Mode: modes[id]}
		if info.Mode == "" {
			info.Mode = "cards"
		}
		if r := runs[a.SteamID]; r != nil {
			info.On, info.Mode = true, r.mode
			for _, v := range r.sup.Views() {
				info.Games++
				info.Idled += int64(v.Idled / time.Second)
				switch v.State {
				case idler.StateIdling:
					info.Idling++
				case idler.StateFailing, idler.StateWaiting:
					if v.Note != "" {
						info.Note = v.Note
					}
				}
			}
		}
		out = append(out, info)
	}
	return out
}
