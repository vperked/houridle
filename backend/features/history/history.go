// Package history keeps a running total of the time houridle idled, per
// game per day, in history.json next to houridle, so `houridle stats` can
// add it up across runs and restarts.
package history

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strconv"
	"sync"
	"time"
)

const (
	version  = 1
	dayStamp = "2006-01-02"
)

// Game is one game's idle time on one day.
type Game struct {
	Name    string `json:"name"`
	Seconds int64  `json:"seconds"`
}

// file is the stored form: days -> app ID -> time.
type file struct {
	Version int                        `json:"version"`
	Days    map[string]map[string]Game `json:"days"`
}

// History is history.json, loaded once and saved after changes. Safe for
// use from several goroutines.
type History struct {
	path string

	mu   sync.Mutex
	data file
	last map[uint32]time.Duration // each game's idle total at the last Record

	// The last Summarize, kept until something is recorded or the day
	// changes: the page asks every few seconds.
	sum    Summary
	sumKey string
	dirty  bool
}

// Open loads (or starts) the history file at path. A damaged file is kept
// aside as path+".bad" and a fresh one started, so a bad write never stops
// idling.
func Open(path string) (*History, error) {
	h := &History{path: path, data: file{Version: version, Days: map[string]map[string]Game{}}, last: map[uint32]time.Duration{}}
	raw, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return h, nil
	}
	if err != nil {
		return nil, err
	}
	if json.Unmarshal(raw, &h.data) != nil || h.data.Version != version {
		os.Rename(path, path+".bad")
		h.data = file{Version: version, Days: map[string]map[string]Game{}}
		return h, nil
	}
	if h.data.Days == nil {
		h.data.Days = map[string]map[string]Game{}
	}
	return h, nil
}

// Path is where the history is kept.
func (h *History) Path() string { return h.path }

// Sample is one game's idle time so far this run.
type Sample struct {
	AppID uint32
	Name  string
	Idled time.Duration // since the run started; only ever grows
}

// Record adds what each game idled since the previous Record to today's
// totals. Call it now and then while idling, and once more at the end.
func (h *History) Record(now time.Time, samples []Sample) {
	if h == nil {
		return
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	h.dirty = true
	day := now.Format(dayStamp)
	for _, s := range samples {
		delta := s.Idled - h.last[s.AppID]
		if delta <= 0 {
			continue
		}
		h.last[s.AppID] = s.Idled
		games := h.data.Days[day]
		if games == nil {
			games = map[string]Game{}
			h.data.Days[day] = games
		}
		key := appKey(s.AppID)
		g := games[key]
		g.Seconds += int64(delta.Round(time.Second) / time.Second)
		if s.Name != "" {
			g.Name = s.Name
		}
		games[key] = g
	}
}

// Save writes the history (atomically, so a reader never sees half a file).
func (h *History) Save() error {
	if h == nil {
		return nil
	}
	h.mu.Lock()
	data, err := json.MarshalIndent(h.data, "", " ")
	h.mu.Unlock()
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(h.path), ".history-*.tmp")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	if _, err := tmp.Write(append(data, '\n')); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), h.path)
}

// GameTotal is one game's time over a stretch of days.
type GameTotal struct {
	AppID uint32
	Name  string
	Time  time.Duration
}

// Summary adds the history up for `houridle stats`.
type Summary struct {
	Today, Week, Month, All time.Duration
	Since                   time.Time   // the first day on record (zero if none)
	Days                    []DayTotal  // the last N days, oldest first
	Games                   []GameTotal // all time, most first
	WeekGames               []GameTotal // the last 7 days, most first
}

// DayTotal is one day's idle time, all games together.
type DayTotal struct {
	Day  time.Time
	Time time.Duration
}

// Summarize totals the history as of now, with a day-by-day list for the
// last days days.
func (h *History) Summarize(now time.Time, days int) Summary {
	h.mu.Lock()
	defer h.mu.Unlock()
	key := fmt.Sprintf("%s/%d", now.Format(dayStamp), days)
	if !h.dirty && key == h.sumKey {
		return h.sum
	}
	s := h.summarize(now, days)
	h.sum, h.sumKey, h.dirty = s, key, false
	return s
}

// summarize adds up the history (call holding mu).
func (h *History) summarize(now time.Time, days int) Summary {
	var s Summary
	today := dayStart(now)
	all := map[string]*GameTotal{}
	week := map[string]*GameTotal{}
	perDay := map[string]time.Duration{}
	for day, games := range h.data.Days {
		d, err := time.ParseInLocation(dayStamp, day, now.Location())
		if err != nil {
			continue
		}
		if s.Since.IsZero() || d.Before(s.Since) {
			s.Since = d
		}
		age := int(today.Sub(d).Hours()/24 + 0.5) // days ago; DST-proof rounding
		for key, g := range games {
			t := time.Duration(g.Seconds) * time.Second
			perDay[day] += t
			s.All += t
			add(all, key, g.Name, t)
			if age == 0 {
				s.Today += t
			}
			if age >= 0 && age < 7 {
				s.Week += t
				add(week, key, g.Name, t)
			}
			if age >= 0 && age < 30 {
				s.Month += t
			}
		}
	}
	for i := days - 1; i >= 0; i-- {
		d := today.AddDate(0, 0, -i)
		s.Days = append(s.Days, DayTotal{Day: d, Time: perDay[d.Format(dayStamp)]})
	}
	s.Games = sorted(all)
	s.WeekGames = sorted(week)
	return s
}

func add(m map[string]*GameTotal, key, name string, t time.Duration) {
	g := m[key]
	if g == nil {
		g = &GameTotal{AppID: parseKey(key)}
		m[key] = g
	}
	if name != "" {
		g.Name = name
	}
	g.Time += t
}

func sorted(m map[string]*GameTotal) []GameTotal {
	out := make([]GameTotal, 0, len(m))
	for _, g := range m {
		out = append(out, *g)
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Time != out[j].Time {
			return out[i].Time > out[j].Time
		}
		return out[i].AppID < out[j].AppID
	})
	return slices.Clip(out)
}

func dayStart(t time.Time) time.Time {
	y, m, d := t.Date()
	return time.Date(y, m, d, 0, 0, 0, 0, t.Location())
}

func appKey(id uint32) string { return strconv.FormatUint(uint64(id), 10) }

func parseKey(key string) uint32 {
	n, _ := strconv.ParseUint(key, 10, 32)
	return uint32(n)
}

// Track records samples() into h every minute while ctx runs, and saves
// every few minutes (so a crash loses little). The returned func records
// the last stretch and saves; call it once idling has stopped. warn, if
// not nil, hears about failed saves.
func Track(ctx context.Context, h *History, samples func() []Sample, warn func(error)) (finish func()) {
	if h == nil {
		return func() {}
	}
	record := func() { h.Record(time.Now(), samples()) }
	save := func() {
		if err := h.Save(); err != nil && warn != nil {
			warn(err)
		}
	}
	done := make(chan struct{})
	go func() {
		defer close(done)
		tick := time.NewTicker(TrackEvery)
		defer tick.Stop()
		lastSave := time.Now()
		for {
			select {
			case <-ctx.Done():
				return
			case <-tick.C:
				record()
				if time.Since(lastSave) >= saveEvery {
					save()
					lastSave = time.Now()
				}
			}
		}
	}()
	return func() {
		<-done
		record()
		save()
	}
}

// TrackEvery is how often Track records. A variable so tests can run fast.
var TrackEvery = time.Minute

const saveEvery = 5 * time.Minute
