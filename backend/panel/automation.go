package panel

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math/rand/v2"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"houridle/backend/config"
	"houridle/backend/idler"
	"houridle/backend/market"
	"houridle/backend/web"
)

// TickEvery is how often the background work (schedule, gradual
// achievements, weekly summary) checks whether it's due. A variable so
// tests needn't wait.
var TickEvery = 20 * time.Second

// GlobalAchievementsURL is Steam's public list of how many players have
// each achievement. A variable so tests can use a local fake.
var GlobalAchievementsURL = "https://api.steampowered.com/ISteamUserStats/GetGlobalAchievementPercentagesForApp/v2/"

// background runs the schedule, the gradual achievements and the weekly
// summary until ctx ends.
func (c *Controller) background(ctx context.Context) {
	t := time.NewTicker(TickEvery)
	defer t.Stop()
	for {
		c.scheduleTick(time.Now())
		c.dripTick(ctx, time.Now())
		c.weeklyTick(ctx, time.Now())
		c.updateTick(ctx, time.Now())
		c.steamTick(ctx, time.Now())
		c.standingTick(ctx, time.Now())
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
	}
}

// updateConfig changes config.json under a lock, so background work and
// page actions don't overwrite each other's changes.
func (c *Controller) updateConfig(fn func(*config.Config)) error {
	c.cfgMu.Lock()
	defer c.cfgMu.Unlock()
	cfg, err := config.Load(c.opts.AppDir)
	if err != nil {
		return err
	}
	fn(&cfg)
	if err := config.Save(c.opts.AppDir, cfg); err != nil {
		return err
	}
	c.cfgCache.forget()
	return nil
}

// config is config.json as it is now. It's read again only when the file
// changes (the page asks for the status every few seconds). Treat it as
// read-only: changes go through updateConfig.
func (c *Controller) config() (config.Config, error) {
	return c.cfgCache.get(filepath.Join(c.opts.AppDir, config.FileName), func() (config.Config, error) {
		return config.Load(c.opts.AppDir)
	})
}

// configCache keeps the last config.json read and the file it came from.
// Every save replaces the file with a new one, so a different file, size or
// time means it changed (houridle's own commands change it too).
type configCache struct {
	mu   sync.Mutex
	cfg  config.Config
	file os.FileInfo // nil: nothing kept
}

func (cc *configCache) get(path string, load func() (config.Config, error)) (config.Config, error) {
	st, statErr := os.Stat(path)
	cc.mu.Lock()
	defer cc.mu.Unlock()
	if statErr == nil && cc.file != nil && os.SameFile(st, cc.file) && st.Size() == cc.file.Size() && st.ModTime().Equal(cc.file.ModTime()) {
		return cc.cfg, nil
	}
	cfg, err := load()
	if err != nil {
		return cfg, err
	}
	cc.cfg, cc.file = cfg, nil
	if statErr == nil {
		cc.file = st
	}
	return cfg, nil
}

func (cc *configCache) forget() {
	cc.mu.Lock()
	cc.file = nil
	cc.mu.Unlock()
}

// ---------- counts (for the weekly summary) ----------

func (c *Controller) count(kind idler.EventKind, n int) {
	if n <= 0 {
		return
	}
	day := today()
	c.updateConfig(func(cfg *config.Config) {
		if cfg.Counts == nil {
			cfg.Counts = map[string]config.DayCounts{}
		}
		d := cfg.Counts[day]
		switch kind {
		case idler.KindDrop:
			d.Drops += n
		case idler.KindCraft:
			d.Crafted += n
		case idler.KindAchievements:
			d.Achievements += n
		default:
			return
		}
		cfg.Counts[day] = d
		for k := range cfg.Counts { // keep two months
			if t, err := time.Parse("2006-01-02", k); err == nil && time.Since(t) > 62*24*time.Hour {
				delete(cfg.Counts, k)
			}
		}
	})
}

// ---------- schedule ----------

func clockMinutes(s string) (int, error) {
	t, err := time.Parse("15:04", strings.TrimSpace(s))
	if err != nil {
		return 0, fmt.Errorf("%q isn't a time like 22:30", s)
	}
	return t.Hour()*60 + t.Minute(), nil
}

// InSchedule reports whether idling may run at t. A window that ends
// before it starts runs past midnight; the day ticked is the day it starts.
func InSchedule(s *config.Schedule, t time.Time) bool {
	if s == nil || !s.On {
		return true
	}
	start, err1 := clockMinutes(s.Start)
	end, err2 := clockMinutes(s.End)
	if err1 != nil || err2 != nil {
		return true
	}
	now := t.Hour()*60 + t.Minute()
	day := (int(t.Weekday()) + 6) % 7 // Monday = 0
	yesterday := (day + 6) % 7
	switch {
	case start == end:
		return s.Days[day]
	case start < end:
		return s.Days[day] && now >= start && now < end
	default: // past midnight
		return s.Days[day] && now >= start || s.Days[yesterday] && now < end
	}
}

// nextChange is when InSchedule next flips, within a week. It can only
// flip when a window starts or ends, or at midnight (a day ticked or not),
// so only those times are checked: a few dozen, not every minute.
func nextChange(s *config.Schedule, t time.Time) time.Time {
	start, err1 := clockMinutes(s.Start)
	end, err2 := clockMinutes(s.End)
	if err1 != nil || err2 != nil {
		return time.Time{}
	}
	in := InSchedule(s, t)
	midnight := time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, t.Location())
	var times []time.Time
	for d := range 9 {
		day := midnight.AddDate(0, 0, d)
		for _, m := range []int{0, start, end} {
			times = append(times, day.Add(time.Duration(m)*time.Minute))
		}
	}
	slices.SortFunc(times, func(a, b time.Time) int { return a.Compare(b) })
	for _, u := range times {
		if u.After(t) && InSchedule(s, u) != in {
			return u
		}
	}
	return time.Time{}
}

// SetSchedule sets (or turns off) the hours idling may run.
func (c *Controller) SetSchedule(in web.ScheduleInfo) error {
	if in.On {
		if _, err := clockMinutes(in.Start); err != nil {
			return err
		}
		if _, err := clockMinutes(in.End); err != nil {
			return err
		}
		if !slices.Contains(in.Days[:], true) {
			return errors.New("pick at least one day")
		}
	}
	err := c.updateConfig(func(cfg *config.Config) {
		cfg.Schedule = &config.Schedule{On: in.On, Start: in.Start, End: in.End, Days: in.Days}
	})
	if err != nil {
		return err
	}
	c.mu.Lock()
	c.schedSeen = false // apply it on the next tick
	c.mu.Unlock()
	go c.scheduleTick(time.Now())
	return nil
}

// scheduleTick pauses idling when the schedule's hours end and resumes it
// when they start again. In between it leaves things alone, so Start and
// Stop on the page still work at any time.
func (c *Controller) scheduleTick(now time.Time) {
	cfg, _ := c.config()
	s := cfg.Schedule
	in := InSchedule(s, now)
	c.mu.Lock()
	changed := !c.schedSeen || c.schedIn != in
	c.schedSeen, c.schedIn = true, in
	running := c.run != nil
	c.mu.Unlock()
	if !changed || s == nil || !s.On {
		return
	}
	wanted := cfg.Panel != nil && cfg.Panel.Idling
	switch {
	case !in && running:
		c.op.Lock()
		c.stopRun() // remembered as idling, so it resumes when the hours start
		c.op.Unlock()
		c.emit(idler.Event{At: now, Level: idler.EventInfo, Text: "Paused by your schedule until " + nextChange(s, now).Format("Mon 15:04")})
	case in && !running && wanted:
		st := c.currentSettings()
		st.Games = cfg.GamesFor(c.gamesKey(), c.sharedGames())
		if err := c.Start(st); err != nil {
			c.emit(idler.Event{At: now, Level: idler.EventWarn, Text: "Your schedule couldn't start idling: " + err.Error()})
			return
		}
		c.emit(idler.Event{At: now, Level: idler.EventInfo, Text: "Started by your schedule"})
	}
}

func (c *Controller) scheduleInfo(cfg config.Config, running bool) web.ScheduleInfo {
	s := cfg.Schedule
	if s == nil {
		return web.ScheduleInfo{Start: "22:00", End: "08:00", Days: [7]bool{true, true, true, true, true, true, true}}
	}
	out := web.ScheduleInfo{On: s.On, Start: s.Start, End: s.End, Days: s.Days}
	if s.On {
		now := time.Now()
		out.Active = InSchedule(s, now)
		out.Paused = !out.Active && !running && cfg.Panel != nil && cfg.Panel.Idling
		out.Next = nextChange(s, now)
	}
	return out
}

// ---------- gradual achievements ----------

// StartDrip unlocks the games' achievements one at a time, about every
// minutes. The most common ones (most players have them) go first, so the
// profile looks like normal play.
func (c *Controller) StartDrip(ctx context.Context, apps []uint32, minutes int) error {
	type found struct {
		app  uint32
		name string
		left int
	}
	var add []found
	var skipped []string
	for _, app := range apps {
		list, err := c.Achievements(ctx, app)
		if err != nil {
			return err
		}
		left := 0
		for _, a := range list {
			if !a.Unlocked && !a.Protected {
				left++
			}
		}
		name := c.gameName(app)
		if left == 0 {
			skipped = append(skipped, name)
			continue
		}
		add = append(add, found{app, name, left})
	}
	if len(add) == 0 {
		return errors.New("nothing left to unlock in " + strings.Join(skipped, ", "))
	}
	now := time.Now()
	err := c.updateConfig(func(cfg *config.Config) {
		for i, f := range add {
			cfg.Drip = slices.DeleteFunc(cfg.Drip, func(d config.DripGame) bool { return d.AppID == f.app })
			// The first unlocks are spread out a little, not all at once.
			first := now.Add(time.Duration(i) * time.Minute).Add(jitter(minutes) / 4)
			cfg.Drip = append(cfg.Drip, config.DripGame{AppID: f.app, Name: f.name, Every: minutes, Next: first, Left: f.left})
		}
	})
	if err == nil {
		c.emit(idler.Event{At: now, Level: idler.EventInfo, Text: fmt.Sprintf("Unlocking achievements gradually in %d game(s), about every %s", len(add), (time.Duration(minutes) * time.Minute).String())})
	}
	return err
}

// StopDrip stops unlocking a game's achievements gradually.
func (c *Controller) StopDrip(appID uint32) error {
	return c.updateConfig(func(cfg *config.Config) {
		cfg.Drip = slices.DeleteFunc(cfg.Drip, func(d config.DripGame) bool { return d.AppID == appID })
	})
}

// jitter is about minutes, give or take 30%.
func jitter(minutes int) time.Duration {
	base := time.Duration(minutes) * time.Minute
	return base*7/10 + time.Duration(rand.Int64N(int64(base*6/10)+1))
}

// dripTick unlocks the next achievement of one game that's due.
func (c *Controller) dripTick(ctx context.Context, now time.Time) {
	c.mu.Lock()
	busy, signedIn := c.dripping, c.src.id() != ""
	if !busy && signedIn {
		c.dripping = true
	}
	c.mu.Unlock()
	if busy || !signedIn {
		return
	}
	defer func() {
		c.mu.Lock()
		c.dripping = false
		c.mu.Unlock()
	}()
	cfg, _ := c.config()
	i := slices.IndexFunc(cfg.Drip, func(d config.DripGame) bool { return !now.Before(d.Next) })
	if i < 0 {
		return
	}
	d := cfg.Drip[i]
	left, err := c.unlockNext(ctx, d.AppID)
	c.updateConfig(func(cfg *config.Config) {
		j := slices.IndexFunc(cfg.Drip, func(x config.DripGame) bool { return x.AppID == d.AppID })
		if j < 0 {
			return // stopped meanwhile
		}
		if err == nil && left == 0 {
			cfg.Drip = slices.Delete(cfg.Drip, j, j+1)
			return
		}
		cfg.Drip[j].Next = time.Now().Add(jitter(d.Every))
		if err == nil {
			cfg.Drip[j].Left = left
		}
	})
	switch {
	case err != nil:
		c.emit(idler.Event{At: now, Level: idler.EventWarn, Text: fmt.Sprintf("Couldn't unlock the next achievement in %s: %v. Trying again later.", d.Name, err)})
	case left == 0:
		c.emit(idler.Event{At: now, Level: idler.EventGood, Text: fmt.Sprintf("Every achievement in %s is unlocked now (gradually)", d.Name), Alert: true, Kind: idler.KindAllUnlocked})
	}
}

// unlockNext unlocks one locked achievement in a game: the one most
// players have. It returns how many are left after it.
func (c *Controller) unlockNext(ctx context.Context, appID uint32) (int, error) {
	list, err := c.Achievements(ctx, appID)
	if err != nil {
		return 0, err
	}
	c.mu.Lock()
	skip := c.dripSkip[appID]
	c.mu.Unlock()
	var locked []web.Achievement
	for _, a := range list {
		if !a.Unlocked && !a.Protected && !skip[a.ID] {
			locked = append(locked, a)
		}
	}
	if len(locked) == 0 {
		return 0, nil
	}
	pct := c.globalPercents(ctx, appID)
	slices.SortStableFunc(locked, func(a, b web.Achievement) int {
		switch pa, pb := pct[a.ID], pct[b.ID]; {
		case pa > pb:
			return -1
		case pa < pb:
			return 1
		}
		return 0
	})
	next := locked[0]
	failed, err := c.setAchievements(ctx, appID, []string{next.ID}, nil)
	if err != nil {
		return len(locked), err
	}
	if _, refused := failed[next.ID]; refused {
		c.mu.Lock()
		if c.dripSkip == nil {
			c.dripSkip = map[uint32]map[string]bool{}
		}
		if c.dripSkip[appID] == nil {
			c.dripSkip[appID] = map[string]bool{}
		}
		c.dripSkip[appID][next.ID] = true
		c.mu.Unlock()
		return len(locked) - 1, nil
	}
	game := c.gameName(appID)
	c.emit(idler.Event{At: time.Now(), Level: idler.EventInfo, Kind: idler.KindAchievements, N: 1,
		Text: fmt.Sprintf("Unlocked “%s” in %s (%d left)", next.Name, game, len(locked)-1)})
	return len(locked) - 1, nil
}

// globalPercents is how many players have each achievement of a game (by
// achievement ID), from Steam's public statistics; empty if unknown.
func (c *Controller) globalPercents(ctx context.Context, appID uint32) map[string]float64 {
	c.mu.Lock()
	p, ok := c.percents[appID]
	c.mu.Unlock()
	if ok {
		return p
	}
	p = map[string]float64{}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, GlobalAchievementsURL+"?gameid="+strconv.FormatUint(uint64(appID), 10), nil)
	if err == nil {
		if resp, err := (&http.Client{Timeout: 20 * time.Second}).Do(req); err == nil {
			var res struct {
				List struct {
					Achievements []struct {
						Name    string      `json:"name"`
						Percent json.Number `json:"percent"`
					} `json:"achievements"`
				} `json:"achievementpercentages"`
			}
			if resp.StatusCode == http.StatusOK && json.NewDecoder(resp.Body).Decode(&res) == nil {
				for _, a := range res.List.Achievements {
					f, _ := a.Percent.Float64()
					p[a.Name] = f
				}
			}
			resp.Body.Close()
		}
	}
	c.mu.Lock()
	if c.percents == nil {
		c.percents = map[uint32]map[string]float64{}
	}
	c.percents[appID] = p
	c.mu.Unlock()
	return p
}

func dripInfo(cfg config.Config) []web.DripInfo {
	out := []web.DripInfo{}
	for _, d := range cfg.Drip {
		out = append(out, web.DripInfo{AppID: d.AppID, Name: d.Name, Every: d.Every, Next: d.Next, Left: d.Left})
	}
	return out
}

// ---------- weekly summary ----------

// SetWeekly turns the weekly summary on or off.
func (c *Controller) SetWeekly(on bool) error {
	return c.updateConfig(func(cfg *config.Config) {
		if cfg.Weekly == nil {
			cfg.Weekly = &config.WeeklyConfig{}
		}
		cfg.Weekly.Off = !on
	})
}

// weeklyDue: Sunday evening, once.
func weeklyDue(w *config.WeeklyConfig, now time.Time) bool {
	if w != nil && w.Off {
		return false
	}
	if now.Weekday() != time.Sunday || now.Hour() < 18 {
		return false
	}
	return w == nil || now.Sub(w.Last) > 24*time.Hour
}

func (c *Controller) weeklyTick(ctx context.Context, now time.Time) {
	c.mu.Lock()
	n := c.notifier
	c.mu.Unlock()
	if n == nil {
		return
	}
	cfg, _ := c.config()
	if !weeklyDue(cfg.Weekly, now) {
		return
	}
	c.updateConfig(func(cfg *config.Config) {
		if cfg.Weekly == nil {
			cfg.Weekly = &config.WeeklyConfig{}
		}
		cfg.Weekly.Last = now
	})
	go func() {
		if _, err := c.SendWeekly(context.WithoutCancel(ctx)); err != nil {
			c.logf("warn", "couldn't send the weekly summary: "+err.Error())
		}
	}()
}

// WeeklyText is the week in a few lines: hours, card drops, badges,
// achievements, money spent, and the Steam level.
func (c *Controller) WeeklyText(ctx context.Context) string {
	cfg, _ := c.config()
	var lines []string
	c.mu.Lock()
	h := c.history
	c.mu.Unlock()
	if h != nil {
		sum := h.Summarize(time.Now(), 7)
		lines = append(lines, fmt.Sprintf("%.1f game-hours idled across %d game(s)", sum.Week.Hours(), len(sum.WeekGames)))
		if len(sum.WeekGames) > 0 {
			top := sum.WeekGames[0]
			lines = append(lines, fmt.Sprintf("Most idled: %s (%.1f h)", top.Name, top.Time.Hours()))
		}
	}
	var week config.DayCounts
	spent := 0
	for i := range 7 {
		day := time.Now().AddDate(0, 0, -i).Format("2006-01-02")
		d := cfg.Counts[day]
		week.Drops += d.Drops
		week.Crafted += d.Crafted
		week.Achievements += d.Achievements
		spent += cfg.Market.SpentOn(day)
	}
	lines = append(lines, fmt.Sprintf("%d card drop(s), %d badge(s) crafted, %d achievement(s) unlocked", week.Drops, week.Crafted, week.Achievements))
	if spent > 0 {
		lines = append(lines, "Spent "+market.FormatPrice(1, spent)+" from the page")
	}
	lctx, cancel := context.WithTimeout(ctx, 90*time.Second)
	defer cancel()
	if lvl, err := c.Level(lctx); err == nil && lvl.Level > 0 {
		lines = append(lines, fmt.Sprintf("Steam level %d (%d XP to level %d)", lvl.Level, lvl.NextXP-lvl.XP, lvl.Level+1))
	}
	return "Your week with houridle:\n" + strings.Join(lines, "\n")
}

// SendWeekly sends the weekly summary now, through the alerts webhook.
func (c *Controller) SendWeekly(ctx context.Context) (string, error) {
	c.mu.Lock()
	n := c.notifier
	c.mu.Unlock()
	if n == nil {
		return "", errors.New("set up alerts first: the summary goes to your alerts webhook")
	}
	text := c.WeeklyText(ctx)
	return text, n.Send(ctx, text)
}
