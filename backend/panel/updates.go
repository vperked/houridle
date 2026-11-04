package panel

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"houridle/backend/config"
	"houridle/backend/idler"
	"houridle/backend/steamcm"
	"houridle/backend/steamver"
	"houridle/backend/update"
	"houridle/backend/web"
)

// How often houridle looks for a new release, and for new Steam settings.
// Variables so tests needn't wait.
var (
	UpdateEvery = 6 * time.Hour
	SteamEvery  = 12 * time.Hour
)

const savedSteamFile = "steam-settings.json" // the last steam.json fetched

type updateState struct {
	checked time.Time
	rel     *update.Release // the newest release, if it's newer
	latest  string
	err     string
	busy    bool
}

type steamState struct {
	settings steamver.Settings
	fromRepo bool
	build    string
	checked  time.Time
	err      string
	busy     bool
}

func (c *Controller) updateSource() update.Source {
	cfg, _ := c.config()
	repo, token := update.DefaultRepo, ""
	if cfg.Update != nil {
		if cfg.Update.Repo != "" {
			repo = cfg.Update.Repo
		}
		token = cfg.Update.Token
	}
	return update.Source{Repo: repo, Token: update.TokenFrom(token)}
}

func (c *Controller) autoUpdate() bool {
	cfg, _ := c.config()
	return cfg.Update == nil || !cfg.Update.Manual
}

// CheckUpdate looks for a newer houridle release now.
func (c *Controller) CheckUpdate(ctx context.Context) (web.UpdateInfo, error) {
	rel, err := update.Latest(ctx, c.updateSource())
	c.mu.Lock()
	c.upd.checked = time.Now()
	c.upd.err, c.upd.rel, c.upd.latest = "", nil, ""
	switch {
	case err != nil && !errors.Is(err, update.ErrNoRelease):
		c.upd.err = err.Error()
	case err == nil:
		c.upd.latest = rel.Version
		if update.Newer(rel.Version, c.opts.Version) {
			c.upd.rel = &rel
		}
	}
	c.mu.Unlock()
	if errors.Is(err, update.ErrNoRelease) {
		err = nil
	}
	return c.updateInfo(), err
}

// InstallUpdate installs the newest release and restarts houridle with it
// (idling carries on where it was).
func (c *Controller) InstallUpdate(ctx context.Context) error {
	c.mu.Lock()
	if c.upd.busy {
		c.mu.Unlock()
		return errors.New("already updating")
	}
	c.upd.busy = true
	c.mu.Unlock()
	defer func() {
		c.mu.Lock()
		c.upd.busy = false
		c.mu.Unlock()
	}()
	if _, err := c.CheckUpdate(ctx); err != nil {
		return err
	}
	c.mu.Lock()
	rel := c.upd.rel
	c.mu.Unlock()
	if rel == nil {
		return fmt.Errorf("houridle %s is the newest", c.opts.Version)
	}
	exe := c.opts.ExePath
	if p, err := filepath.EvalSymlinks(exe); err == nil {
		exe = p
	}
	c.buying.Lock() // not in the middle of a purchase
	defer c.buying.Unlock()
	if err := update.Install(ctx, c.updateSource(), *rel, exe); err != nil {
		c.emit(idler.Event{At: time.Now(), Level: idler.EventWarn, Text: "Couldn't update houridle: " + err.Error()})
		return err
	}
	if c.opts.Restart == nil {
		c.emit(idler.Event{At: time.Now(), Level: idler.EventGood, Text: fmt.Sprintf("Installed houridle %s; restart houridle to use it", rel.Version), Alert: true})
		return nil
	}
	c.emit(idler.Event{At: time.Now(), Level: idler.EventGood, Text: fmt.Sprintf("Updated houridle from %s to %s; restarting", c.opts.Version, rel.Version), Alert: true})
	go func() {
		time.Sleep(time.Second) // let the page hear back first
		c.opts.Restart()
	}()
	return nil
}

// SetAutoUpdate turns automatic updates on or off.
func (c *Controller) SetAutoUpdate(on bool) error {
	return c.updateConfig(func(cfg *config.Config) {
		if cfg.Update == nil {
			cfg.Update = &config.UpdateConfig{}
		}
		cfg.Update.Manual = !on
	})
}

// updateTick looks for a new release now and then, and installs it when
// updates are automatic.
func (c *Controller) updateTick(ctx context.Context, now time.Time) {
	c.mu.Lock()
	due := !c.upd.busy && now.Sub(c.upd.checked) >= UpdateEvery
	if due {
		c.upd.checked = now // no second check while this one runs
	}
	c.mu.Unlock()
	if !due {
		return
	}
	go func() {
		info, err := c.CheckUpdate(ctx)
		if err != nil || !info.Available {
			return
		}
		if c.autoUpdate() {
			c.InstallUpdate(ctx)
			return
		}
		c.emit(idler.Event{At: time.Now(), Level: idler.EventInfo, Text: fmt.Sprintf("houridle %s is out (this is %s): update from This page", info.Latest, info.Current), Alert: true})
	}()
}

func (c *Controller) updateInfo() web.UpdateInfo {
	c.mu.Lock()
	u := c.upd
	c.mu.Unlock()
	out := web.UpdateInfo{Current: c.opts.Version, Latest: u.latest, Checked: u.checked, Error: u.err, Installing: u.busy, Auto: c.autoUpdate()}
	if u.rel != nil {
		out.Available, out.Published, out.Notes = true, u.rel.Published, u.rel.Notes
	}
	return out
}

// ---------- keeping up with Steam ----------

// loadSteamSettings applies the steam.json fetched last time, and keeps
// Steam's server list next to houridle (see steamcm.ServerCache).
func (c *Controller) loadSteamSettings() {
	steamcm.ServerCache = filepath.Join(c.opts.AppDir, "cm-servers.json")
	s, ok := steamver.LoadSaved(filepath.Join(c.opts.AppDir, savedSteamFile))
	c.mu.Lock()
	defer c.mu.Unlock()
	if ok {
		c.steam.settings, c.steam.fromRepo = s, true
	} else {
		c.steam.settings = steamver.Defaults()
	}
}

// steamTick fetches steam.json from houridle's repository and the Steam
// client version now and then; new sign-in values apply from the next
// sign-in on.
func (c *Controller) steamTick(ctx context.Context, now time.Time) {
	c.mu.Lock()
	due := !c.steam.busy && now.Sub(c.steam.checked) >= SteamEvery
	if due {
		c.steam.busy, c.steam.checked = true, now
	}
	c.mu.Unlock()
	if !due {
		return
	}
	go func() {
		defer func() {
			c.mu.Lock()
			c.steam.busy = false
			c.mu.Unlock()
		}()
		c.refreshSteam(ctx)
	}()
}

func (c *Controller) refreshSteam(ctx context.Context) {
	build, buildErr := steamver.ClientBuild(ctx)
	data, err := update.File(ctx, c.updateSource(), steamver.FileName)
	var s steamver.Settings
	if err == nil {
		s, err = steamver.Parse(data)
	}
	c.mu.Lock()
	old, had := c.steam.settings, c.steam.fromRepo
	if buildErr == nil {
		c.steam.build = build
	}
	c.steam.err = ""
	if err != nil {
		c.steam.err = err.Error()
	}
	if err == nil {
		c.steam.settings, c.steam.fromRepo = s, true
	}
	c.mu.Unlock()
	if err != nil {
		return
	}
	steamver.Apply(s)
	os.WriteFile(filepath.Join(c.opts.AppDir, savedSteamFile), data, 0o600)
	if !had || old.ProtocolVersion != s.ProtocolVersion || old.ClientVersion != s.ClientVersion || old.DirectoryURL != s.DirectoryURL {
		if had || s.ProtocolVersion != steamcm.DefaultProtocolVersion || s.ClientVersion != steamcm.DefaultClientVersion {
			c.emit(idler.Event{At: time.Now(), Level: idler.EventInfo, Text: fmt.Sprintf("Picked up new Steam settings (protocol %d, client %d)", s.ProtocolVersion, s.ClientVersion)})
		}
	}
}

func (c *Controller) steamInfo() web.SteamInfo {
	c.mu.Lock()
	s := c.steam
	c.mu.Unlock()
	protocol, client := steamcm.Versions()
	return web.SteamInfo{Protocol: protocol, Client: client, FromRepo: s.fromRepo, Build: s.build, Checked: s.checked, Error: s.err}
}
