// Package panel is `houridle serve`: a houridle that keeps running and is
// driven from its web page. It starts, stops and reconfigures idling,
// lists your games, changes achievements, claims free games, and manages
// accounts and alerts: everything the command line does.
//
// Steam keeps one connection per sign-in, so whatever needs Steam while
// idling goes through the idler's own connection (see withConn).
package panel

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"time"

	"houridle/backend/client/steam"
	"houridle/backend/features/history"
	"houridle/backend/features/notify"
	"houridle/backend/online/steamauth"
	"houridle/backend/online/steamcm"
	"houridle/backend/run/config"
	"houridle/backend/run/eventlog"
	"houridle/backend/run/handover"
	"houridle/backend/run/idler"
	"houridle/backend/run/stoptime"
	"houridle/backend/views/web"
)

// The page drives the controller through web.Controller.
var _ web.Controller = (*Controller)(nil)

const (
	recentEvents = 40
	historyDays  = 84 // 12 weeks: the History calendar
	clientSource = "client"
)

// Options sets up a Controller.
type Options struct {
	AppDir   string
	ExePath  string // houridle itself, re-run as idle helpers (Steam client mode)
	Version  string
	WebAddr  string
	Log      *eventlog.Log      // houridle.log; may be nil
	Print    func(idler.Event)  // also shows events (e.g. on the console); may be nil
	Quit     context.CancelFunc // ends houridle serve
	Restart  func()             // ends houridle serve and starts it again (after an update); may be nil
	Selector string             // the account to start with (-account), "" for the last one used
}

// Controller runs idling for the page. Its methods are safe to call from
// several goroutines; changes are made one at a time.
type Controller struct {
	opts     Options
	steamDir string                 // "" if Steam isn't installed here
	local    map[uint32]*steam.Game // games installed here (Steam client)

	op       sync.Mutex  // one change at a time (start, stop, switch account...)
	buying   sync.Mutex  // one purchase (cards or a game) at a time
	cfgMu    sync.Mutex  // one read-change-save of config.json at a time (see updateConfig)
	cfgCache configCache // config.json as last read (see config)

	mu        sync.Mutex // guards the fields below
	src       source
	lock      *handover.Lock
	run       *run
	settings  web.Settings // the last settings used (or saved)
	events    []idler.Event
	history   *history.History
	notifier  *notify.Notifier
	alerts    notify.Tracker
	names     map[uint32]string             // game names seen in the library, for starting
	cards     map[uint32]cachedCards        // games' Market card prices (see gameCards)
	gems      map[uint32]int                // gems one of a game's cards turns into
	level     levelCache                    // the Steam level, read a while ago
	percents  map[uint32]map[string]float64 // share of players with each achievement
	dripSkip  map[uint32]map[string]bool    // achievements Steam refused to unlock
	dripping  bool                          // a gradual unlock is under way
	extras    map[uint64]*extraRun          // other accounts idling alongside
	schedSeen bool                          // the schedule has been checked once
	schedIn   bool                          // ...and was inside its hours then
	bgCancel  context.CancelFunc            // stops the background work
	upd       updateState                   // the newest release
	steam     steamState                    // what Steam's sign-in expects
	standing  standingState                 // the account's bans and restrictions
	login     web.Login
}

// source is what idling goes through: the Steam client, or a saved account.
type source struct {
	client  bool
	account steamauth.SavedAccount
}

func (s source) id() string {
	switch {
	case s.client:
		return clientSource
	case s.account.SteamID != 0:
		return steamauth.FormatSteamID(s.account.SteamID)
	}
	return ""
}

func (s source) signedIn() bool { return !s.client && s.account.SteamID != 0 }

// run is one stretch of idling.
type run struct {
	sup      idler.Runner
	cancel   context.CancelFunc
	done     chan struct{}
	settings web.Settings
	started  time.Time
	stopAt   time.Time
	target   time.Duration
	rotate   time.Duration
}

// New sets up the controller: it picks the account (or the Steam client) to
// idle with, and takes its idle lock so no other houridle idles it too.
func New(opts Options) (*Controller, error) {
	c := &Controller{opts: opts, local: map[uint32]*steam.Game{}, names: map[uint32]string{}}
	if dir, err := steam.Path(); err == nil {
		c.steamDir = dir
		c.local = steam.LocalGames(dir)
	}
	cfg, err := config.Load(opts.AppDir)
	if err != nil {
		c.logf("warn", err.Error())
	}
	if cfg.Panel != nil {
		c.settings = web.Settings{Mode: cfg.Panel.Mode, Until: cfg.Panel.Until, Rotate: cfg.Panel.Rotate, Status: cfg.Panel.Status, Claim: cfg.Panel.Claim, Craft: cfg.Panel.Craft, Events: cfg.Panel.Events, Refundable: cfg.Panel.Refundable}
	}
	if c.settings.Mode == "" {
		c.settings.Mode = "games"
	}

	want := opts.Selector
	if want == "" && cfg.Panel != nil {
		want = cfg.Panel.Source
	}
	src, err := c.findSource(want)
	if err != nil && opts.Selector != "" {
		return nil, err
	}
	if err != nil { // the remembered one is gone: fall back to the default
		src, _ = c.findSource("")
	}
	if src.id() != "" {
		if err := c.takeLock(src); err != nil {
			return nil, err
		}
	}
	c.src = src
	c.loadSteamSettings()
	c.openHistory()
	c.setNotifier(cfg.Notify)
	return c, nil
}

// Resume starts idling again if houridle serve was idling when it last
// stopped (after a reboot, say).
func (c *Controller) Resume() {
	ctx, cancel := context.WithCancel(context.Background())
	c.mu.Lock()
	c.bgCancel = cancel
	c.mu.Unlock()
	c.resumeExtras()
	cfg, _ := c.config()
	if cfg.Panel == nil || !cfg.Panel.Idling || !InSchedule(cfg.Schedule, time.Now()) {
		go c.background(ctx)
		return
	}
	defer func() { go c.background(ctx) }()
	st := c.currentSettings()
	st.Games = cfg.GamesFor(c.gamesKey(), c.sharedGames())
	if err := c.Start(st); err != nil {
		c.emit(idler.Event{At: time.Now(), Level: idler.EventWarn, Text: "Couldn't start idling again: " + err.Error()})
	}
}

// Close stops idling and releases the lock (houridle serve is exiting).
// The remembered state is left as it is, so the next start resumes.
func (c *Controller) Close() {
	c.mu.Lock()
	if c.bgCancel != nil {
		c.bgCancel()
	}
	c.mu.Unlock()
	c.op.Lock()
	defer c.op.Unlock()
	c.stopRun()
	c.stopExtras()
	c.mu.Lock()
	if c.lock != nil {
		c.lock.Release()
		c.lock = nil
	}
	c.mu.Unlock()
}

// findSource resolves "client", a SteamID or account name, or "" (the
// default account, else the Steam client if it's installed).
func (c *Controller) findSource(id string) (source, error) {
	if id == clientSource {
		if c.steamDir == "" {
			return source{}, errors.New("Steam isn't installed on this computer")
		}
		return source{client: true}, nil
	}
	accts, err := steamauth.DefaultAccounts()
	if err != nil {
		return source{}, err
	}
	acc, err := accts.Find(id)
	if err == nil {
		return source{account: acc}, nil
	}
	if id == "" && c.steamDir != "" {
		return source{client: true}, nil
	}
	if id == "" && errors.Is(err, steamauth.ErrNotLoggedIn) {
		return source{}, nil // nothing to idle with yet: the page offers to sign in
	}
	return source{}, err
}

// takeLock takes the idle lock for src (each account other than the
// default has its own), releasing the one held.
func (c *Controller) takeLock(src source) error {
	c.mu.Lock()
	old := c.lock
	c.lock = nil
	c.mu.Unlock()
	if old != nil {
		old.Release()
	}
	key := ""
	if src.signedIn() {
		key = src.account.LockKey()
	}
	lock, err := handover.TakeLockFor(key)
	if err != nil {
		return fmt.Errorf("another houridle is idling with %s; stop it first", src.label())
	}
	c.mu.Lock()
	c.lock = lock
	c.mu.Unlock()
	return nil
}

func (s source) label() string {
	if s.client {
		return "the Steam client"
	}
	return s.account.Label()
}

// ---------- idling ----------

// Start starts idling with st, replacing any idling already going on.
func (c *Controller) Start(st web.Settings) error {
	c.op.Lock()
	defer c.op.Unlock()

	c.mu.Lock()
	src := c.src
	c.mu.Unlock()
	if src.id() == "" {
		return errors.New("sign in to Steam first (Accounts)")
	}
	plan, err := c.plan(src, st)
	if err != nil {
		return err
	}
	c.stopRun()

	ctx, cancel := context.WithCancel(context.Background())
	if !plan.stopAt.IsZero() {
		ctx, cancel = context.WithDeadline(context.Background(), plan.stopAt)
	}
	var sup idler.Runner
	if src.client {
		sup = idler.New(plan.games, c.opts.ExePath, plan.libPath, c.emit)
	} else {
		mgr := &steamauth.Manager{Client: steamauth.NewClient(), Store: src.account.Store}
		sup = idler.NewSessionWith(plan.games, mgr, plan.opts, c.emit)
	}
	r := &run{sup: sup, cancel: cancel, done: make(chan struct{}), settings: st, started: time.Now(), stopAt: plan.stopAt, target: plan.opts.Target, rotate: plan.opts.Rotate}
	c.mu.Lock()
	c.run = r
	c.settings = st
	h := c.history
	c.mu.Unlock()

	c.remember(src, st, true)
	c.logf("start", c.describe(src, st, plan.games))
	sup.Start(ctx)
	finishHistory := history.Track(ctx, h, func() []history.Sample { return samples(sup.Views()) }, func(err error) {
		c.logf("warn", "couldn't save the hours history: "+err.Error())
	})
	go c.watch(ctx, r, finishHistory)
	return nil
}

// plan is a checked Start request.
type plan struct {
	games   []*steam.Game
	opts    idler.SessionOptions
	stopAt  time.Time
	libPath string
}

func (c *Controller) plan(src source, st web.Settings) (plan, error) {
	var p plan
	switch st.Mode {
	case "games", "":
		if len(st.Games) == 0 {
			return p, errors.New("pick at least one game")
		}
	case "all", "cards":
		if !src.signedIn() {
			return p, errors.New("idling every game or card drops needs a saved sign-in, not the Steam client")
		}
	default:
		return p, fmt.Errorf("unknown mode %q", st.Mode)
	}
	if src.client && (st.Until != "" || st.Rotate != "" || st.Claim || st.Craft || st.Events || (st.Status != "" && st.Status != "online")) {
		return p, errors.New("hour targets, turns, status, free games, badge crafting and sale events need a saved sign-in, not the Steam client")
	}
	var err error
	if p.opts.Status, err = steamcm.ParseStatus(st.Status); err != nil {
		return p, err
	}
	if st.Until != "" {
		if p.opts.Target, err = stoptime.ParseHours(st.Until); err != nil {
			return p, fmt.Errorf("stop each game at: %w", err)
		}
	}
	if st.Rotate != "" {
		if p.opts.Rotate, err = stoptime.ParseHours(st.Rotate); err != nil {
			return p, fmt.Errorf("turns: %w", err)
		}
		if p.opts.Rotate < 10*time.Minute {
			return p, errors.New("turns: no more often than every 10 minutes")
		}
	}
	if st.Mode == "cards" && p.opts.Target > 0 {
		return p, errors.New("card drops decide when a game is done; leave out the hour target")
	}
	p.opts.Cards, p.opts.All, p.opts.Claim = st.Mode == "cards", st.Mode == "all", st.Claim
	p.opts.Craft, p.opts.Events, p.opts.Refundable = st.Craft, st.Events, st.Refundable
	if st.Stop != "" && !strings.EqualFold(st.Stop, "never") {
		if p.stopAt, err = stoptime.Parse(st.Stop, time.Now()); err != nil {
			return p, fmt.Errorf("stop: %w", err)
		}
	}

	if st.Mode == "games" || st.Mode == "" {
		ids := slices.Clone(st.Games)
		slices.Sort(ids)
		ids = slices.Compact(ids)
		if src.client && len(ids) > idler.MaxGames {
			return p, fmt.Errorf("through the Steam client, at most %d games at once", idler.MaxGames)
		}
		known := map[uint32]*steam.Game{}
		for id, g := range c.local {
			known[id] = g
		}
		c.mu.Lock()
		for id, name := range c.names {
			if _, ok := known[id]; !ok {
				known[id] = &steam.Game{AppID: id, Name: name}
			}
		}
		c.mu.Unlock()
		p.games = steam.ResolveGames(ids, known)
	}
	if src.client {
		if !steam.Running() {
			c.emit(idler.Event{At: time.Now(), Level: idler.EventWarn, Text: "Steam isn't running. Start it and sign in; idling begins once it's up."})
		}
		if p.libPath, err = steam.FindAPILibrary(c.opts.AppDir, c.steamDir); err != nil {
			return p, err
		}
	}
	return p, nil
}

// watch waits for a run to end (stopped, stop time, or nothing left to
// idle) and tidies up after it.
func (c *Controller) watch(ctx context.Context, r *run, finishHistory func()) {
	finished := r.sup.Finished()
	isFinished := false
	select {
	case <-ctx.Done():
	case <-finished:
		isFinished = true
	}
	timeUp := errors.Is(ctx.Err(), context.DeadlineExceeded)
	r.cancel()
	r.sup.Wait()
	finishHistory()

	var total time.Duration
	for _, v := range r.sup.Views() {
		total += v.Idled
	}
	c.logf("stop", fmt.Sprintf("after %s, +%.2f game-hours", time.Since(r.started).Round(time.Second), total.Hours()))
	switch {
	case isFinished:
		c.emit(idler.Event{At: time.Now(), Level: idler.EventGood, Text: "Finished: nothing left to idle", Alert: true})
	case timeUp:
		c.emit(idler.Event{At: time.Now(), Level: idler.EventInfo, Text: "Reached the stop time"})
	}

	c.mu.Lock()
	current := c.run == r
	if current {
		c.run = nil
	}
	src, st := c.src, c.settings
	c.mu.Unlock()
	if current && (isFinished || timeUp) {
		c.remember(src, st, false) // it ended by itself: don't resume it after a restart
	}
	close(r.done)
}

// StopIdling stops idling; houridle keeps serving the page.
func (c *Controller) StopIdling() {
	c.op.Lock()
	defer c.op.Unlock()
	c.mu.Lock()
	idling := c.run != nil
	src, st := c.src, c.settings
	c.mu.Unlock()
	if !idling {
		return
	}
	c.stopRun()
	c.remember(src, st, false)
	c.emit(idler.Event{At: time.Now(), Level: idler.EventInfo, Text: "Stopped idling from the page"})
}

// stopRun stops the current run and waits for it (call holding op).
func (c *Controller) stopRun() {
	c.mu.Lock()
	r := c.run
	c.mu.Unlock()
	if r == nil {
		return
	}
	r.cancel()
	<-r.done
}

// Quit exits houridle serve.
func (c *Controller) Quit() {
	c.logf("info", "quit from the page")
	if c.opts.Quit != nil {
		c.opts.Quit()
	}
}

// remember saves what the panel is doing in config.json, so a restart
// carries on: the source, the settings, the game list, and whether idling.
func (c *Controller) remember(src source, st web.Settings, idling bool) {
	err := c.updateConfig(func(cfg *config.Config) {
		cfg.Panel = &config.PanelConfig{Source: src.id(), Idling: idling, Mode: st.Mode, Until: st.Until, Rotate: st.Rotate, Status: st.Status, Claim: st.Claim, Craft: st.Craft, Events: st.Events, Refundable: st.Refundable}
		if (st.Mode == "games" || st.Mode == "") && len(st.Games) > 0 {
			cfg.SetGamesFor(c.gamesKeyFor(src), c.sharedGamesFor(src), st.Games)
		}
	})
	if err != nil {
		c.logf("warn", "couldn't save config.json: "+err.Error())
	}
}

// gamesKey and sharedGames say where the current source's game list lives
// in config.json (see config.GamesFor).
func (c *Controller) gamesKey() string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.gamesKeyFor(c.src)
}

func (c *Controller) sharedGames() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.sharedGamesFor(c.src)
}

func (c *Controller) gamesKeyFor(src source) string {
	if src.signedIn() {
		return steamauth.FormatSteamID(src.account.SteamID)
	}
	return ""
}

func (c *Controller) sharedGamesFor(src source) bool { return !src.signedIn() || src.account.Default }

func (c *Controller) currentSettings() web.Settings {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.settings
}

func (c *Controller) describe(src source, st web.Settings, games []*steam.Game) string {
	what := map[string]string{"all": "every game you own", "cards": "the games with card drops left"}[st.Mode]
	if what == "" {
		names := make([]string, len(games))
		for i, g := range games {
			names[i] = fmt.Sprintf("%s (%d)", g.Name, g.AppID)
		}
		what = fmt.Sprintf("%d game(s): %s", len(games), strings.Join(names, ", "))
	}
	return fmt.Sprintf("idling %s from the page, with %s", what, src.label())
}

func samples(views []idler.View) []history.Sample {
	out := make([]history.Sample, len(views))
	for i, v := range views {
		out[i] = history.Sample{AppID: v.Game.AppID, Name: v.Game.Name, Idled: v.Idled}
	}
	return out
}

// ---------- events, log, alerts ----------

func (c *Controller) emit(e idler.Event) {
	c.logf(levelWord(e.Level), e.Text)
	c.mu.Lock()
	if len(c.events) == recentEvents {
		c.events = append(c.events[:0], c.events[1:]...)
	}
	c.events = append(c.events, e)
	c.mu.Unlock()
	c.alerts.Event(e.Text, e.Level == idler.EventBad, e.Level == idler.EventGood, e.Alert)
	if e.Kind != "" {
		go c.count(e.Kind, e.N)
	}
	if e.Kind == idler.KindCraft {
		c.forgetLevel()
	}
	if c.opts.Print != nil {
		c.opts.Print(e)
	}
}

func (c *Controller) logf(word, text string) {
	c.opts.Log.Printf("%-5s  %s", word, text)
}

func levelWord(l idler.EventLevel) string {
	switch l {
	case idler.EventGood:
		return "ok"
	case idler.EventWarn:
		return "warn"
	case idler.EventBad:
		return "error"
	}
	return "info"
}

// openHistory opens the current source's history file.
func (c *Controller) openHistory() {
	c.mu.Lock()
	defer c.mu.Unlock()
	name := "history.json"
	if c.src.signedIn() {
		name = c.src.account.FileName("history", ".json")
	}
	h, err := history.Open(filepath.Join(c.opts.AppDir, name))
	if err != nil {
		c.opts.Log.Printf("warn   hours won't be added to the history: %v", err)
	}
	c.history = h
}

func (c *Controller) setNotifier(url string) {
	var n *notify.Notifier
	if url != "" && !strings.EqualFold(url, "off") {
		prefix := ""
		if accts, err := steamauth.DefaultAccounts(); err == nil && accts.Multiple() {
			c.mu.Lock()
			if c.src.signedIn() {
				prefix = c.src.account.Label()
			}
			c.mu.Unlock()
		}
		n, _ = notify.New(url, prefix)
	}
	c.mu.Lock()
	c.notifier = n
	c.mu.Unlock()
	c.alerts.Set(n)
}

// ---------- status ----------

// Status is everything the page shows.
func (c *Controller) Status() web.Status {
	c.mu.Lock()
	r, src, st, h, n := c.run, c.src, c.settings, c.history, c.notifier
	events := slices.Clone(c.events)
	login := c.login
	c.mu.Unlock()

	out := web.Status{Version: c.opts.Version, Account: "", Events: events}
	if src.signedIn() {
		out.Account = src.account.Label()
	} else if src.client {
		out.Account = "Steam client"
	}
	if r != nil {
		out.Started, out.StopAt, out.Views = r.started, r.stopAt, r.sup.Views()
		out.Run = web.Run{SignedIn: src.signedIn(), Cards: r.settings.Mode == "cards", All: r.settings.Mode == "all", Claim: r.settings.Claim,
			Target: r.target, Rotate: r.rotate, Status: r.settings.Status, Craft: r.settings.Craft, Events: r.settings.Events}
		if out.Run.Status == "" && src.signedIn() {
			out.Run.Status = "online"
		}
	} else {
		out.Started = time.Now()
		out.Run = web.Run{SignedIn: src.signedIn()}
	}
	if h != nil {
		sum := h.Summarize(time.Now(), historyDays)
		out.Stats = &sum
	}

	cfg, _ := c.config()
	if r == nil {
		st.Games = cfg.GamesFor(c.gamesKeyFor(src), c.sharedGamesFor(src))
	} else {
		st = r.settings
	}
	if st.Games == nil {
		st.Games = []uint32{} // the page reads .length
	}
	p := &web.Panel{Idling: r != nil, Source: src.id(), SignedIn: src.signedIn(), SteamClient: c.steamDir != "", Settings: st, WebAddr: c.opts.WebAddr, Login: login}
	if n != nil {
		p.Notify, p.NotifyKind = true, n.Kind()
	}
	p.Schedule = c.scheduleInfo(cfg, r != nil)
	p.Drip = dripInfo(cfg)
	p.Weekly = n != nil && (cfg.Weekly == nil || !cfg.Weekly.Off)
	p.Update = c.updateInfo()
	p.Steam = c.steamInfo()
	p.Standing = c.standingInfo()
	p.Accounts = []web.AccountInfo{}   // [] rather than null with no saved sign-in
	var saved []steamauth.SavedAccount // read once: each is a file
	if accts, err := steamauth.DefaultAccounts(); err == nil {
		saved = accts.List()
	}
	for _, a := range saved {
		p.Accounts = append(p.Accounts, web.AccountInfo{SteamID: steamauth.FormatSteamID(a.SteamID), Name: a.Label(), Default: a.Default, Expires: a.Expires, Damaged: a.Err != nil})
	}
	p.Extras = c.extrasInfo(cfg, saved)
	out.Panel = p
	return out
}

// knownName is a game's name as seen in the library ("" if unknown).
func (c *Controller) knownName(appID uint32) string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.names[appID]
}

// gameName is a game's name as seen in the library, or a placeholder.
func (c *Controller) gameName(appID uint32) string {
	return steam.NameOr(c.knownName(appID), appID)
}
