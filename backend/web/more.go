package web

import (
	"context"
	"net/http"
	"strconv"
	"time"
)

// More is the rest of what the page can do: Steam level, inventory and
// gems, the idling schedule, gradual achievements, extra accounts and the
// weekly summary.
type More interface {
	Level(ctx context.Context) (Level, error)
	Inventory(ctx context.Context) (Inventory, error)
	GrindDuplicates(ctx context.Context, apps []uint32, expected int) (Ground, error)
	Boosters(ctx context.Context) (BoosterList, error)
	MakeBooster(ctx context.Context, appID uint32, price int) error
	SetSchedule(ScheduleInfo) error
	StartDrip(ctx context.Context, apps []uint32, minutes int) error
	StopDrip(appID uint32) error
	SetExtra(steamID, mode string, on bool) error
	SetWeekly(on bool) error
	SendWeekly(ctx context.Context) (string, error)
	CheckUpdate(ctx context.Context) (UpdateInfo, error)
	InstallUpdate(ctx context.Context) error
	SetAutoUpdate(on bool) error
	CheckStanding(ctx context.Context) (Standing, error)
}

// Standing is the account's record: bans, trade restrictions, whether it's
// limited, and whether the Market is open to it.
type Standing struct {
	VACBans      int       `json:"vacBans"`
	GameBans     int       `json:"gameBans"`
	DaysSinceBan int       `json:"daysSinceBan"` // -1: no ban
	TradeBan     string    `json:"tradeBan"`     // "None", "Probation" or "Banned"
	Limited      bool      `json:"limited"`
	Market       bool      `json:"market"` // the Market is open to it
	MarketReason string    `json:"marketReason,omitempty"`
	Checked      time.Time `json:"checked"`
	OK           bool      `json:"ok"`              // the values are a real reading (not just an error so far)
	Error        string    `json:"error,omitempty"` // the last check failed
}

// UpdateInfo is houridle's version and the newest release.
type UpdateInfo struct {
	Current    string    `json:"current"`
	Latest     string    `json:"latest,omitempty"`
	Available  bool      `json:"available"`
	Auto       bool      `json:"auto"`
	Checked    time.Time `json:"checked,omitzero"`
	Published  time.Time `json:"published,omitzero"`
	Notes      string    `json:"notes,omitempty"`
	Error      string    `json:"error,omitempty"`
	Installing bool      `json:"installing,omitempty"`
}

// SteamInfo is what houridle tells Steam when signing in, where that came
// from, and the Steam client version Valve publishes now.
type SteamInfo struct {
	Protocol uint64    `json:"protocol"`
	Client   uint64    `json:"client"`
	FromRepo bool      `json:"fromRepo"` // from steam.json rather than built in
	Build    string    `json:"build,omitempty"`
	Checked  time.Time `json:"checked,omitzero"`
	Error    string    `json:"error,omitempty"`
}

// Level is the account's Steam level and what's in reach.
type Level struct {
	Level    int `json:"level"`
	XP       int `json:"xp"`
	LevelXP  int `json:"levelXp"` // XP the current level started at
	NextXP   int `json:"nextXp"`  // XP the next level starts at
	Badges   int `json:"badges"`  // game badges with any progress
	Ready    int `json:"ready"`   // card sets complete: 100 XP each when crafted
	Partial  int `json:"partial"` // sets partly collected
	Missing  int `json:"missing"` // cards missing from those
	DropsXP  int `json:"dropsXp"` // XP from finishing sets of games still dropping cards
	Unlocked int `json:"unlocked,omitempty"`
}

// Inventory is the trading cards and gems in the Steam inventory.
type Inventory struct {
	Gems     int         `json:"gems"`
	Cards    int         `json:"cards"`
	Value    int         `json:"value"`    // cents (US): what the cards sell for, roughly
	Spare    int         `json:"spare"`    // extra copies (more than one of a card)
	Boosters int         `json:"boosters"` // unopened booster packs
	Games    []CardGroup `json:"games"`    // most valuable first
	Priced   bool        `json:"priced"`   // every game's cards could be priced
}

// CardGroup is one game's cards in the inventory.
type CardGroup struct {
	AppID  uint32 `json:"appId"`
	Name   string `json:"name"`
	Cards  int    `json:"cards"`
	Kinds  int    `json:"kinds"` // different cards
	Spare  int    `json:"spare"`
	Foils  int    `json:"foils"`
	Value  int    `json:"value"` // cents (US), normal cards only
	Gems   int    `json:"gems"`  // what each spare card turns into (0 = not checked)
	Market string `json:"market"`
}

// Ground is what turning spare cards into gems did.
type Ground struct {
	Cards int    `json:"cards"`
	Gems  int    `json:"gems"`
	Error string `json:"error,omitempty"`
}

// BoosterList is the gems on hand and the booster packs they can make.
type BoosterList struct {
	Gems     int           `json:"gems"`
	Boosters []BoosterInfo `json:"boosters"`
}

// BoosterInfo is one game's booster pack.
type BoosterInfo struct {
	AppID       uint32 `json:"appId"`
	Name        string `json:"name"`
	Price       int    `json:"price"` // gems
	Available   bool   `json:"available"`
	AvailableAt string `json:"availableAt,omitempty"`
}

// ScheduleInfo is when idling may run, and where that leaves things now.
type ScheduleInfo struct {
	On     bool      `json:"on"`
	Start  string    `json:"start"`
	End    string    `json:"end"`
	Days   [7]bool   `json:"days"`             // Monday first
	Active bool      `json:"active,omitempty"` // inside the hours now
	Paused bool      `json:"paused,omitempty"` // waiting for the hours to start
	Next   time.Time `json:"next,omitzero"`    // when that next changes
}

// DripInfo is a game whose achievements are unlocking one at a time.
type DripInfo struct {
	AppID uint32    `json:"appId"`
	Name  string    `json:"name"`
	Every int       `json:"every"` // minutes
	Next  time.Time `json:"next"`
	Left  int       `json:"left"`
}

// ExtraInfo is another account idling alongside the main one.
type ExtraInfo struct {
	SteamID string `json:"steamId"`
	Name    string `json:"name"`
	Mode    string `json:"mode"`
	On      bool   `json:"on"`
	Idling  int    `json:"idling"` // games idling now
	Games   int    `json:"games"`
	Idled   int64  `json:"idled"` // seconds this run, all games
	Note    string `json:"note,omitempty"`
}

func (s *Server) moreRoutes(post, get func(string, func(http.ResponseWriter, *http.Request) (any, error))) {
	c := s.Control
	get("/api/level", func(w http.ResponseWriter, r *http.Request) (any, error) { return c.Level(r.Context()) })
	get("/api/inventory", func(w http.ResponseWriter, r *http.Request) (any, error) { return c.Inventory(r.Context()) })
	post("/api/gems/grind", func(w http.ResponseWriter, r *http.Request) (any, error) {
		var req struct {
			Apps []uint32 `json:"apps"`
			Gems int      `json:"gems"` // what the page showed
		}
		if err := readJSON(r, &req); err != nil {
			return nil, err
		}
		if len(req.Apps) == 0 {
			return nil, badRequest("pick at least one game")
		}
		return c.GrindDuplicates(r.Context(), req.Apps, req.Gems)
	})
	get("/api/boosters", func(w http.ResponseWriter, r *http.Request) (any, error) { return c.Boosters(r.Context()) })
	post("/api/boosters/make", func(w http.ResponseWriter, r *http.Request) (any, error) {
		var req struct {
			App   uint32 `json:"app"`
			Price int    `json:"price"`
		}
		if err := readJSON(r, &req); err != nil {
			return nil, err
		}
		if req.App == 0 || req.Price <= 0 {
			return nil, badRequest("pick a booster pack")
		}
		return nil, c.MakeBooster(r.Context(), req.App, req.Price)
	})
	post("/api/schedule", func(w http.ResponseWriter, r *http.Request) (any, error) {
		var req ScheduleInfo
		if err := readJSON(r, &req); err != nil {
			return nil, err
		}
		return nil, c.SetSchedule(req)
	})
	post("/api/drip", func(w http.ResponseWriter, r *http.Request) (any, error) {
		var req struct {
			Apps    []uint32 `json:"apps"`
			Minutes int      `json:"minutes"`
		}
		if err := readJSON(r, &req); err != nil {
			return nil, err
		}
		if len(req.Apps) == 0 || req.Minutes < 5 || req.Minutes > 7*24*60 {
			return nil, badRequest("pick games and an interval between 5 minutes and a week")
		}
		return nil, c.StartDrip(r.Context(), req.Apps, req.Minutes)
	})
	post("/api/drip/stop", func(w http.ResponseWriter, r *http.Request) (any, error) {
		var req struct {
			App uint32 `json:"app"`
		}
		if err := readJSON(r, &req); err != nil {
			return nil, err
		}
		return nil, c.StopDrip(req.App)
	})
	post("/api/extras", func(w http.ResponseWriter, r *http.Request) (any, error) {
		var req struct {
			SteamID string `json:"steamId"`
			Mode    string `json:"mode"`
			On      bool   `json:"on"`
		}
		if err := readJSON(r, &req); err != nil {
			return nil, err
		}
		if _, err := strconv.ParseUint(req.SteamID, 10, 64); err != nil {
			return nil, badRequest("pick an account")
		}
		return nil, c.SetExtra(req.SteamID, req.Mode, req.On)
	})
	post("/api/weekly", func(w http.ResponseWriter, r *http.Request) (any, error) {
		var req struct {
			On bool `json:"on"`
		}
		if err := readJSON(r, &req); err != nil {
			return nil, err
		}
		return nil, c.SetWeekly(req.On)
	})
	post("/api/update/check", func(w http.ResponseWriter, r *http.Request) (any, error) { return c.CheckUpdate(r.Context()) })
	post("/api/update/install", func(w http.ResponseWriter, r *http.Request) (any, error) { return nil, c.InstallUpdate(r.Context()) })
	post("/api/update/auto", func(w http.ResponseWriter, r *http.Request) (any, error) {
		var req struct {
			On bool `json:"on"`
		}
		if err := readJSON(r, &req); err != nil {
			return nil, err
		}
		return nil, c.SetAutoUpdate(req.On)
	})
	post("/api/standing/check", func(w http.ResponseWriter, r *http.Request) (any, error) { return c.CheckStanding(r.Context()) })
	post("/api/weekly/send", func(w http.ResponseWriter, r *http.Request) (any, error) {
		text, err := c.SendWeekly(r.Context())
		return map[string]any{"text": text}, err
	})
}
