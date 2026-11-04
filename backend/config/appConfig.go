// Package config loads and saves houridle's config.json and parses app IDs.
package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// FileName is the settings file, in the app folder.
const FileName = "config.json"

type Config struct {
	Games []uint32 `json:"games"`

	// Defaults for flags, so a service needs no long command line.
	Status string `json:"status,omitempty"` // -status: how you show to friends while idling
	Notify string `json:"notify,omitempty"` // -notify: webhook for alerts
	Web    string `json:"web,omitempty"`    // -web: status page address, e.g. ":8080"
	WebKey string `json:"webKey,omitempty"` // the status page's key (a secret; see houridle web)

	// Market is the spending limit for buying cards from the control panel,
	// and what's been spent (by day, in the wallet's cents).
	Market *MarketConfig `json:"market,omitempty"`

	// Panel is what `houridle serve` was last doing, so it carries on after
	// a restart.
	Panel *PanelConfig `json:"panel,omitempty"`

	// Accounts holds each saved Steam account's own game list, by SteamID,
	// for signed-in idling with more than one account. Games above is used
	// for any account without its own list.
	Accounts map[string]AccountConfig `json:"accounts,omitempty"`

	// Schedule limits idling to certain hours (houridle serve).
	Schedule *Schedule `json:"schedule,omitempty"`
	// Drip is the games whose achievements unlock one at a time.
	Drip []DripGame `json:"drip,omitempty"`
	// Extras is other saved accounts idling alongside the main one.
	Extras []ExtraAccount `json:"extras,omitempty"`
	// Weekly is the weekly summary alert.
	Weekly *WeeklyConfig `json:"weekly,omitempty"`
	// Standing is each account's record as last checked (by SteamID), to
	// alert when it changes.
	Standing map[string]StandingRecord `json:"standing,omitempty"`
	// Update is how houridle keeps itself up to date.
	Update *UpdateConfig `json:"update,omitempty"`
	// Counts is what happened each day (card drops, badges crafted,
	// achievements), for the weekly summary.
	Counts map[string]DayCounts `json:"counts,omitempty"`
}

// Schedule is when idling may run: from Start to End ("22:00" to "08:00"
// crosses midnight) on the days ticked (Days[0] is Monday).
type Schedule struct {
	On    bool    `json:"on"`
	Start string  `json:"start"`
	End   string  `json:"end"`
	Days  [7]bool `json:"days"`
}

// DripGame unlocks a game's achievements one at a time, every Every
// minutes (give or take), the next at Next.
type DripGame struct {
	AppID uint32    `json:"appId"`
	Name  string    `json:"name,omitempty"`
	Every int       `json:"every"`
	Next  time.Time `json:"next"`
	Left  int       `json:"left"` // locked achievements left, as of the last unlock
}

// ExtraAccount is a saved account idling alongside the main one.
type ExtraAccount struct {
	SteamID string `json:"steamId"`
	Mode    string `json:"mode"` // "cards" or "all"
	On      bool   `json:"on"`
}

// WeeklyConfig is the weekly summary alert.
type WeeklyConfig struct {
	Off  bool      `json:"off,omitempty"` // on by default once alerts are set up
	Last time.Time `json:"last,omitempty"`
}

// StandingRecord is an account's bans and restrictions as last seen.
type StandingRecord struct {
	VACBans  int    `json:"vacBans"`
	GameBans int    `json:"gameBans"`
	TradeBan string `json:"tradeBan"`
	Limited  bool   `json:"limited"`
}

// UpdateConfig is how houridle keeps itself up to date from GitHub.
type UpdateConfig struct {
	Manual bool   `json:"manual,omitempty"` // only update when asked (default: automatically)
	Repo   string `json:"repo,omitempty"`   // "owner/name"; default houridle's own
	Token  string `json:"token,omitempty"`  // a GitHub token for a private repository (a secret)
}

// DayCounts is what happened on one day.
type DayCounts struct {
	Drops        int `json:"drops,omitempty"`
	Crafted      int `json:"crafted,omitempty"`
	Achievements int `json:"achievements,omitempty"`
}

// MarketConfig limits buying cards on the Steam Market.
type MarketConfig struct {
	DailyCap int            `json:"dailyCap"`        // cents per day; 0 means the default, -1 no buying
	Spent    map[string]int `json:"spent,omitempty"` // YYYY-MM-DD -> cents spent that day
}

// DefaultDailyCap is the daily limit until you choose one: 5.00 in the
// wallet's currency.
const DefaultDailyCap = 500

// Cap is the daily limit in cents.
func (m *MarketConfig) Cap() int {
	switch {
	case m == nil || m.DailyCap == 0:
		return DefaultDailyCap
	case m.DailyCap < 0:
		return 0 // buying is off
	}
	return m.DailyCap
}

// SpentOn is what was spent on a day (YYYY-MM-DD).
func (m *MarketConfig) SpentOn(day string) int {
	if m == nil {
		return 0
	}
	return m.Spent[day]
}

// PanelConfig is `houridle serve`'s last state. Its game list is the
// account's own (see GamesFor).
type PanelConfig struct {
	Source string `json:"source,omitempty"` // "client" or a SteamID
	Idling bool   `json:"idling,omitempty"` // start idling again on the next start
	Mode   string `json:"mode,omitempty"`   // "games", "all" or "cards"
	Until  string `json:"until,omitempty"`
	Rotate string `json:"rotate,omitempty"`
	Status string `json:"status,omitempty"`
	Claim  bool   `json:"claim,omitempty"`
	Craft  bool   `json:"craft,omitempty"`
	Events bool   `json:"events,omitempty"`
}

// AccountConfig is one account's settings.
type AccountConfig struct {
	Games []uint32 `json:"games"`
}

// GamesFor returns an account's game list (by SteamID). shared says the
// account may use the shared list: it's the Steam client (""), the only
// saved account, or the default one, whose list config.json always kept.
func (c Config) GamesFor(steamID string, shared bool) []uint32 {
	if a, ok := c.Accounts[steamID]; ok && steamID != "" {
		return a.Games
	}
	if shared {
		return c.Games
	}
	return nil
}

// SetGamesFor saves an account's game list: in the shared list when shared
// (see GamesFor), otherwise under the account.
func (c *Config) SetGamesFor(steamID string, shared bool, games []uint32) {
	if steamID == "" || shared {
		c.Games = games
		delete(c.Accounts, steamID)
		return
	}
	if c.Accounts == nil {
		c.Accounts = map[string]AccountConfig{}
	}
	c.Accounts[steamID] = AccountConfig{Games: games}
}

// AppDir is where config.json and the DLL copy live: next to the exe, or the
// working directory under `go run` (which builds into a temp folder).
func AppDir() string {
	if exe, err := os.Executable(); err == nil {
		dir := filepath.Dir(exe)
		if !strings.Contains(strings.ToLower(dir), "go-build") {
			return dir
		}
	}
	wd, _ := os.Getwd()
	return wd
}

// Load reads config.json. A missing file is not an error.
func Load(appDir string) (Config, error) {
	var cfg Config
	data, err := os.ReadFile(filepath.Join(appDir, FileName))
	if errors.Is(err, fs.ErrNotExist) {
		return cfg, nil
	}
	if err != nil {
		return cfg, err
	}
	if err := json.Unmarshal(data, &cfg); err != nil {
		return Config{}, fmt.Errorf("%s isn't valid JSON (%v), ignoring it", FileName, err)
	}
	return cfg, nil
}

func Save(appDir string, cfg Config) error {
	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}
	// Owner-only: it can hold secrets (the status page key, a webhook URL).
	// WriteFile's mode only applies to a new file, so fix an older one too.
	// Written to a temporary file that then replaces it, so a reader never
	// sees it half written.
	path := filepath.Join(appDir, FileName)
	tmp, err := os.CreateTemp(appDir, FileName+".*.tmp")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name()) // gone already after the rename
	if err := tmp.Chmod(0o600); err != nil {
		tmp.Close()
		return err
	}
	if _, err := tmp.Write(append(data, '\n')); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), path)
}

// ParseAppIDs accepts IDs separated by spaces and/or commas.
func ParseAppIDs(parts []string) ([]uint32, error) {
	var ids []uint32
	for _, part := range parts {
		for _, field := range strings.FieldsFunc(part, isSeparator) {
			n, err := strconv.ParseUint(field, 10, 32)
			if err != nil || n == 0 {
				return nil, fmt.Errorf("%q isn't an app ID", field)
			}
			ids = append(ids, uint32(n))
		}
	}
	return ids, nil
}

func isSeparator(r rune) bool {
	return r == ',' || r == ' ' || r == '\t' || r == '\r' || r == '\n'
}

// Dedupe drops repeated IDs, keeping the first occurrence's position.
func Dedupe(ids []uint32) []uint32 {
	seen := make(map[uint32]bool, len(ids))
	out := make([]uint32, 0, len(ids))
	for _, id := range ids {
		if !seen[id] {
			seen[id] = true
			out = append(out, id)
		}
	}
	return out
}
