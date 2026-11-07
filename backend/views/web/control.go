package web

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"time"
)

// Controller is what the page can do beyond showing status, in `houridle
// serve`: everything the command line does. Its methods are called from
// request goroutines and must be safe for that.
type Controller interface {
	Start(Settings) error // start idling (or restart it with new settings)
	StopIdling()          // stop idling; houridle keeps serving the page
	Quit()                // exit houridle

	Library(ctx context.Context) ([]LibraryGame, error)
	Achievements(ctx context.Context, appID uint32) ([]Achievement, error)
	SetAchievements(ctx context.Context, appID uint32, unlock, lock []string) (map[string]string, error)
	SetAllAchievements(ctx context.Context, apps []uint32, unlock bool) ([]BulkAchievements, error)
	FreeGames(ctx context.Context, claim bool) ([]FreeGame, error)
	FreeToPlay(ctx context.Context, q FreeToPlayQuery) ([]FreeToPlay, error)
	Drops(ctx context.Context) (map[uint32]int, error)                   // card drops left, by app ID
	AddFreeGames(ctx context.Context, appIDs []uint32) ([]uint32, error) // returns what Steam added
	Badges(ctx context.Context) (BadgesInfo, error)
	SetCards(ctx context.Context, appID uint32) ([]CardInfo, error)
	CraftNow(ctx context.Context) ([]string, error) // what was crafted
	ExploreNow(ctx context.Context) (Explored, error)

	Shop(ctx context.Context) (Shop, error)
	Quote(ctx context.Context, appID uint32) (Quote, error)
	BuySet(ctx context.Context, appID uint32, expectedTotal int) (Bought, error)
	SetMarketCap(cents int) error
	GameDeals(ctx context.Context, q DealsQuery) (Deals, error)
	BuyGame(ctx context.Context, appID uint32, agreed int) (GameBought, error)

	UseSource(id string) error   // "client" or a SteamID
	MakeDefault(id string) error // a SteamID
	Logout(id string) error      // a SteamID
	BeginLogin() (Login, error)                                                   // a QR sign-in
	BeginPasswordLogin(ctx context.Context, user, password string) (Login, error) // a sign-in with a password
	SubmitLoginCode(ctx context.Context, code string) (Login, error)              // the Steam Guard code it asks for
	CancelLogin()                                                                 // drops a sign-in under way (or its outcome)
	LoginState() Login

	SetNotify(url string) error // "" turns alerts off
	TestNotify(ctx context.Context) error
	NewKey() (string, error) // a new page key; old links stop working

	More
}

// Settings is how to idle, as the page sends it.
type Settings struct {
	Mode   string   `json:"mode"`   // "games", "all" or "cards"
	Games  []uint32 `json:"games"`  // with "games"
	Until  string   `json:"until"`  // hours on record to stop each game at ("" none), e.g. "100" or "90m"
	Rotate string   `json:"rotate"` // how often games take turns past 32 ("" default)
	Status string   `json:"status"` // how friends see you ("" online)
	Claim  bool     `json:"claim"`  // add free giveaways
	Craft  bool     `json:"craft"`  // craft badges when a card set is complete
	Events bool     `json:"events"` // sale events: discovery queue and daily sticker
	Stop   string   `json:"stop"`   // stop by itself: hours, a time (23:30) or "" never

	Refundable bool `json:"refundable"` // also idle games that could still be refunded
}

// Panel is the controller's state, for the page.
type Panel struct {
	Idling      bool          `json:"idling"`
	Source      string        `json:"source"`   // "client", a SteamID, or "" if there's nothing to idle with
	SignedIn    bool          `json:"signedIn"` // Source is a saved Steam sign-in (not the Steam client)
	SteamClient bool          `json:"steamClient"`
	Settings    Settings      `json:"settings"` // the last ones used
	Accounts    []AccountInfo `json:"accounts"`
	Notify      bool          `json:"notify"`     // alerts are set up
	NotifyKind  string        `json:"notifyKind"` // "discord", "slack" or "text"
	WebAddr     string        `json:"webAddr"`
	Login       Login         `json:"login"`
	Schedule    ScheduleInfo  `json:"schedule"`
	Drip        []DripInfo    `json:"drip"`
	Extras      []ExtraInfo   `json:"extras"`
	Weekly      bool          `json:"weekly"` // the weekly summary is on (it needs alerts)
	Update      UpdateInfo    `json:"update"`
	Standing    *Standing     `json:"standing,omitempty"` // nil until checked
	Steam       SteamInfo     `json:"steam"`
}

// AccountInfo is one saved account.
type AccountInfo struct {
	SteamID string    `json:"steamId"`
	Name    string    `json:"name"`
	Default bool      `json:"default"`
	Expires time.Time `json:"expires,omitzero"`
	Damaged bool      `json:"damaged,omitempty"`
}

// Login is a sign-in in progress (or its outcome): a QR code the Steam app
// scans, or an account name and password and then Steam Guard.
type Login struct {
	State   string `json:"state"`            // "", "waiting", "done", "failed"
	Method  string `json:"method,omitempty"` // "qr" or "password"
	URL     string `json:"url,omitempty"`
	QR      string `json:"qr,omitempty"`      // SVG of URL
	Code    string `json:"code,omitempty"`    // password: Steam Guard wants a code from "app" or "email"
	Approve string `json:"approve,omitempty"` // password: it can be approved instead, in the "app" or from an "email"
	Hint    string `json:"hint,omitempty"`    // where Steam emailed the code (its hint, e.g. the domain)
	Account string `json:"account,omitempty"`
	Error   string `json:"error,omitempty"`
}

// LibraryGame is a game the page can pick.
type LibraryGame struct {
	AppID   uint32 `json:"appId"`
	Name    string `json:"name"`
	Minutes int    `json:"minutes"`
	Shared  bool   `json:"shared,omitempty"` // a Steam family member's game, shared with you
}

// Achievement is one of a game's achievements.
type Achievement struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
	Hidden      bool   `json:"hidden,omitempty"`
	Unlocked    bool   `json:"unlocked"`
	Protected   bool   `json:"protected,omitempty"` // only the game's servers can change it
}

// BulkAchievements is what unlocking (or locking) everything did in one game.
type BulkAchievements struct {
	AppID    uint32 `json:"appId"`
	Name     string `json:"name"`
	Total    int    `json:"total"`    // achievements the game has
	Changed  int    `json:"changed"`  // unlocked (or locked) now
	Already  int    `json:"already"`  // were already that way
	GameOnly int    `json:"gameOnly"` // only the game's servers can change them
	Failed   int    `json:"failed"`   // Steam refused
	Error    string `json:"error,omitempty"`
}

// FreeGame is a giveaway and what happened (or would happen) to it.
type FreeGame struct {
	Name    string `json:"name"`
	AppID   uint32 `json:"appId,omitempty"`
	Outcome string `json:"outcome"` // "added", "sent", "would-add", "owned", "needs-base", "failed"
	Error   string `json:"error,omitempty"`
	Review  Review `json:"review"`
}

// FreeToPlayQuery is which free-to-play games to list, and how.
type FreeToPlayQuery struct {
	Achievements bool
	Cards        bool
	Sort         string // "reviews", "popular", "newest", "name"
	Count        int    // how many (10-200)
}

// FreeToPlay is an always-free game the account can add.
type FreeToPlay struct {
	AppID  uint32 `json:"appId"`
	Name   string `json:"name"`
	Review Review `json:"review"`
	Owned  bool   `json:"owned"`
}

// BadgesInfo is the badges ready to craft and the ones closest to it.
type BadgesInfo struct {
	Ready   []BadgeInfo `json:"ready"`
	Closest []BadgeInfo `json:"closest"`
}

// BadgeInfo is one game badge's progress.
type BadgeInfo struct {
	AppID     uint32 `json:"appId"`
	Name      string `json:"name"`
	Collected int    `json:"collected"`
	Total     int    `json:"total"`
	Foil      bool   `json:"foil,omitempty"`
	Market    string `json:"market"` // the game's cards on the Steam Market
}

// CardInfo is one card of a game's set.
type CardInfo struct {
	Name   string `json:"name"`
	Owned  int    `json:"owned"`
	Market string `json:"market"`
}

// Shop is the badge shop: your wallet, today's limit, and the badges that
// give the most Steam XP for the money.
type Shop struct {
	Balance    int         `json:"balance"`
	Currency   int         `json:"currency"`
	Allowed    bool        `json:"allowed"` // the Market is open to the account
	Reason     string      `json:"reason,omitempty"`
	Cap        int         `json:"cap"`        // daily limit, cents
	SpentToday int         `json:"spentToday"` // cents
	Badges     []ShopBadge `json:"badges"`     // best value first
}

// ShopBadge is a badge you could finish by buying cards.
type ShopBadge struct {
	AppID     uint32   `json:"appId"`
	Name      string   `json:"name"`
	Collected int      `json:"collected"`
	Total     int      `json:"total"`
	Missing   []string `json:"missing"`
	Estimate  int      `json:"estimate"`    // cents (US dollars; the quote has the exact cost)
	XPPerUnit float64  `json:"xpPerDollar"` // 100 XP / estimate in dollars
}

// Quote is the exact cost to finish a badge: the cheapest listing of each
// missing card right now.
type Quote struct {
	AppID    uint32      `json:"appId"`
	Name     string      `json:"name"`
	Items    []QuoteItem `json:"items"`
	Total    int         `json:"total"` // cents, in Currency
	Currency int         `json:"currency"`
}

// QuoteItem is one card to buy.
type QuoteItem struct {
	Card   string `json:"card"`
	Price  int    `json:"price"` // cents, fee included
	Market string `json:"market"`
}

// Bought is what a purchase did.
type Bought struct {
	Cards []string `json:"cards"` // bought
	Spent int      `json:"spent"` // cents
	Error string   `json:"error,omitempty"`
}

// DealsQuery is which cheap games to look for.
type DealsQuery struct {
	MaxPrice    int    // cents
	Count       int    // how many to list
	Sort        string // "xp" (Steam XP per dollar, the default), "price", "payback" or "reviews"
	GoodReviews bool   // only well-reviewed games
}

// Deals is cheap games with trading cards you don't own, with your wallet
// and today's limit (shared with buying cards).
type Deals struct {
	Balance    int    `json:"balance"`
	Currency   int    `json:"currency"`
	Cap        int    `json:"cap"`
	SpentToday int    `json:"spentToday"`
	Deals      []Deal `json:"deals"`
}

// Deal is one game you could buy for its cards.
type Deal struct {
	AppID       uint32  `json:"appId"`
	Name        string  `json:"name"`
	Package     string  `json:"package"`  // what's bought, as the store names it
	Price       int     `json:"price"`    // cents, what you'd pay before any tax
	Currency    string  `json:"currency"` // the store's, e.g. "USD"
	Discount    int     `json:"discount"` // percent off
	Review      Review  `json:"review"`
	Cards       int     `json:"cards"`       // cards in its set
	Drops       int     `json:"drops"`       // cards you get by idling it (half the set, rounded up)
	CardPrice   int     `json:"cardPrice"`   // average card price on the Market, cents (US)
	DropValue   int     `json:"dropValue"`   // what the drops sell for after fees, cents
	XPCost      int     `json:"xpCost"`      // the game plus the cards that don't drop: a level 1 badge
	XPPerDollar float64 `json:"xpPerDollar"` // 100 XP / XPCost in dollars
}

// GameBought is what buying a game did.
type GameBought struct {
	AppID    uint32 `json:"appId"`
	Name     string `json:"name"`
	Bought   bool   `json:"bought"`
	Spent    int    `json:"spent"` // cents, tax included
	Currency int    `json:"currency"`
	Error    string `json:"error,omitempty"`
}

// Explored is what exploring the discovery queue did.
type Explored struct {
	Games   int  `json:"games"`
	Sticker bool `json:"sticker"` // a sale-event sticker was claimed
}

// Review is a game's Steam user-review summary.
type Review struct {
	Summary  string `json:"summary,omitempty"` // e.g. "Very Positive"
	Positive int    `json:"positive,omitempty"`
	Count    int    `json:"count,omitempty"`
}

const (
	maxBody        = 1 << 20
	controlTimeout = 3 * time.Minute
)

// controlRoutes adds the control endpoints. Every one needs the key cookie
// and (for changes) the page's own header, like Stop.
func (s *Server) controlRoutes(mux *http.ServeMux) {
	c := s.Control
	post := func(path string, h func(w http.ResponseWriter, r *http.Request) (any, error)) {
		mux.HandleFunc("POST "+path, s.authed(s.changing(jsonHandler(h))))
	}
	get := func(path string, h func(w http.ResponseWriter, r *http.Request) (any, error)) {
		mux.HandleFunc("GET "+path, s.authed(jsonHandler(h)))
	}

	post("/api/idle/start", func(w http.ResponseWriter, r *http.Request) (any, error) {
		var st Settings
		if err := readJSON(r, &st); err != nil {
			return nil, err
		}
		return nil, c.Start(st)
	})
	post("/api/idle/stop", func(w http.ResponseWriter, r *http.Request) (any, error) {
		c.StopIdling()
		return nil, nil
	})
	post("/api/quit", func(w http.ResponseWriter, r *http.Request) (any, error) {
		go c.Quit() // answer first: quitting ends this server
		return nil, nil
	})
	get("/api/library", func(w http.ResponseWriter, r *http.Request) (any, error) {
		return c.Library(r.Context())
	})
	get("/api/achievements", func(w http.ResponseWriter, r *http.Request) (any, error) {
		id, err := appParam(r)
		if err != nil {
			return nil, err
		}
		return c.Achievements(r.Context(), id)
	})
	post("/api/achievements", func(w http.ResponseWriter, r *http.Request) (any, error) {
		var req struct {
			App    uint32   `json:"app"`
			Unlock []string `json:"unlock"`
			Lock   []string `json:"lock"`
		}
		if err := readJSON(r, &req); err != nil {
			return nil, err
		}
		failed, err := c.SetAchievements(r.Context(), req.App, req.Unlock, req.Lock)
		return map[string]any{"failed": failed}, err
	})
	post("/api/achievements/bulk", func(w http.ResponseWriter, r *http.Request) (any, error) {
		var req struct {
			Apps   []uint32 `json:"apps"`
			Unlock bool     `json:"unlock"`
		}
		if err := readJSON(r, &req); err != nil {
			return nil, err
		}
		if len(req.Apps) == 0 {
			return nil, badRequest("pick at least one game")
		}
		res, err := c.SetAllAchievements(r.Context(), req.Apps, req.Unlock)
		if res == nil {
			res = []BulkAchievements{}
		}
		return map[string]any{"games": res}, err
	})
	get("/api/freegames", func(w http.ResponseWriter, r *http.Request) (any, error) {
		return c.FreeGames(r.Context(), false)
	})
	post("/api/freegames/claim", func(w http.ResponseWriter, r *http.Request) (any, error) {
		return c.FreeGames(r.Context(), true)
	})
	get("/api/badges", func(w http.ResponseWriter, r *http.Request) (any, error) {
		return c.Badges(r.Context())
	})
	get("/api/badges/cards", func(w http.ResponseWriter, r *http.Request) (any, error) {
		id, err := appParam(r)
		if err != nil {
			return nil, err
		}
		return c.SetCards(r.Context(), id)
	})
	post("/api/badges/craft", func(w http.ResponseWriter, r *http.Request) (any, error) {
		crafted, err := c.CraftNow(r.Context())
		if crafted == nil {
			crafted = []string{}
		}
		return map[string]any{"crafted": crafted}, err
	})
	get("/api/shop", func(w http.ResponseWriter, r *http.Request) (any, error) {
		return c.Shop(r.Context())
	})
	get("/api/shop/quote", func(w http.ResponseWriter, r *http.Request) (any, error) {
		id, err := appParam(r)
		if err != nil {
			return nil, err
		}
		return c.Quote(r.Context(), id)
	})
	post("/api/shop/buy", func(w http.ResponseWriter, r *http.Request) (any, error) {
		var req struct {
			App   uint32 `json:"app"`
			Total int    `json:"total"` // the quoted total the buyer agreed to
		}
		if err := readJSON(r, &req); err != nil {
			return nil, err
		}
		if req.App == 0 || req.Total <= 0 {
			return nil, badRequest("get a quote first")
		}
		return c.BuySet(r.Context(), req.App, req.Total)
	})
	post("/api/shop/cap", func(w http.ResponseWriter, r *http.Request) (any, error) {
		var req struct {
			Cents int `json:"cents"`
		}
		if err := readJSON(r, &req); err != nil {
			return nil, err
		}
		if req.Cents < 0 || req.Cents > 100000 {
			return nil, badRequest("the daily limit should be between 0 and 1000")
		}
		return nil, c.SetMarketCap(req.Cents)
	})
	get("/api/deals", func(w http.ResponseWriter, r *http.Request) (any, error) {
		q := r.URL.Query()
		maxPrice, _ := strconv.Atoi(q.Get("max"))
		n, _ := strconv.Atoi(q.Get("count"))
		return c.GameDeals(r.Context(), DealsQuery{MaxPrice: maxPrice, Count: n, Sort: q.Get("sort"), GoodReviews: q.Get("reviews") != "0"})
	})
	post("/api/deals/buy", func(w http.ResponseWriter, r *http.Request) (any, error) {
		var req struct {
			App   uint32 `json:"app"`
			Price int    `json:"price"` // the price the buyer agreed to, cents
		}
		if err := readJSON(r, &req); err != nil {
			return nil, err
		}
		if req.App == 0 || req.Price <= 0 {
			return nil, badRequest("pick a game first")
		}
		return c.BuyGame(r.Context(), req.App, req.Price)
	})
	post("/api/events/explore", func(w http.ResponseWriter, r *http.Request) (any, error) {
		return c.ExploreNow(r.Context())
	})
	get("/api/freetoplay", func(w http.ResponseWriter, r *http.Request) (any, error) {
		q := r.URL.Query()
		n, _ := strconv.Atoi(q.Get("count"))
		return c.FreeToPlay(r.Context(), FreeToPlayQuery{Achievements: q.Get("achievements") == "1", Cards: q.Get("cards") == "1", Sort: q.Get("sort"), Count: n})
	})
	get("/api/drops", func(w http.ResponseWriter, r *http.Request) (any, error) {
		drops, err := c.Drops(r.Context())
		if drops == nil {
			drops = map[uint32]int{}
		}
		return drops, err
	})
	post("/api/freetoplay/add", func(w http.ResponseWriter, r *http.Request) (any, error) {
		var req struct {
			Apps []uint32 `json:"apps"`
		}
		if err := readJSON(r, &req); err != nil {
			return nil, err
		}
		if len(req.Apps) == 0 || len(req.Apps) > 100 {
			return nil, badRequest("pick between 1 and 100 games")
		}
		granted, err := c.AddFreeGames(r.Context(), req.Apps)
		if granted == nil {
			granted = []uint32{}
		}
		return map[string]any{"added": granted}, err
	})
	idAction := func(path string, do func(string) error) {
		post(path, func(w http.ResponseWriter, r *http.Request) (any, error) {
			var req struct {
				ID string `json:"id"`
			}
			if err := readJSON(r, &req); err != nil {
				return nil, err
			}
			return nil, do(req.ID)
		})
	}
	idAction("/api/accounts/use", c.UseSource)
	idAction("/api/accounts/default", c.MakeDefault)
	idAction("/api/accounts/logout", c.Logout)
	post("/api/login", func(w http.ResponseWriter, r *http.Request) (any, error) {
		return c.BeginLogin()
	})
	get("/api/login", func(w http.ResponseWriter, r *http.Request) (any, error) {
		return c.LoginState(), nil
	})
	post("/api/login/password", func(w http.ResponseWriter, r *http.Request) (any, error) {
		var req struct {
			User     string `json:"user"`
			Password string `json:"password"`
		}
		if err := readJSON(r, &req); err != nil {
			return nil, err
		}
		if strings.TrimSpace(req.User) == "" || req.Password == "" {
			return nil, badRequest("enter the account name and password")
		}
		return c.BeginPasswordLogin(r.Context(), req.User, req.Password)
	})
	post("/api/login/code", func(w http.ResponseWriter, r *http.Request) (any, error) {
		var req struct {
			Code string `json:"code"`
		}
		if err := readJSON(r, &req); err != nil {
			return nil, err
		}
		if strings.TrimSpace(req.Code) == "" {
			return nil, badRequest("enter the Steam Guard code")
		}
		return c.SubmitLoginCode(r.Context(), req.Code)
	})
	post("/api/login/cancel", func(w http.ResponseWriter, r *http.Request) (any, error) {
		c.CancelLogin()
		return c.LoginState(), nil
	})
	post("/api/notify", func(w http.ResponseWriter, r *http.Request) (any, error) {
		var req struct {
			URL string `json:"url"`
		}
		if err := readJSON(r, &req); err != nil {
			return nil, err
		}
		return nil, c.SetNotify(req.URL)
	})
	post("/api/notify/test", func(w http.ResponseWriter, r *http.Request) (any, error) {
		return nil, c.TestNotify(r.Context())
	})
	post("/api/webkey", func(w http.ResponseWriter, r *http.Request) (any, error) {
		key, err := c.NewKey()
		if err != nil {
			return nil, err
		}
		s.setKey(key)
		// Keep this browser signed in with the new key; other links stop working.
		http.SetCookie(w, s.keyCookie(key, r))
		return map[string]any{"links": Links(s.listenAddr(), key)}, nil
	})
	s.moreRoutes(post, get)
}

// changing guards requests that change something: they must carry the
// page's own header, which a page on another site can't send.
func (s *Server) changing(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get(stopHeader) != "stop" && r.Header.Get(stopHeader) != "1" {
			http.Error(w, "missing "+stopHeader+" header", http.StatusForbidden)
			return
		}
		next(w, r)
	}
}

// appParam is the ?app= game a request is about.
func appParam(r *http.Request) (uint32, error) {
	id, err := strconv.ParseUint(r.URL.Query().Get("app"), 10, 32)
	if err != nil || id == 0 {
		return 0, badRequest("pick a game")
	}
	return uint32(id), nil
}

// requestError is an error the page caused (shown as HTTP 400).
type requestError struct{ msg string }

func (e *requestError) Error() string { return e.msg }

func badRequest(msg string) error { return &requestError{msg} }

// jsonHandler runs h with a time limit and answers with its result as JSON,
// or {"error": "..."} and a fitting status.
func jsonHandler(h func(w http.ResponseWriter, r *http.Request) (any, error)) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ctx, cancel := context.WithTimeout(r.Context(), controlTimeout)
		defer cancel()
		out, err := h(w, r.WithContext(ctx))
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Cache-Control", "no-store")
		if err != nil {
			code := http.StatusConflict // the request was fine; houridle couldn't do it now
			var re *requestError
			if errors.As(err, &re) {
				code = http.StatusBadRequest
			}
			w.WriteHeader(code)
			json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
			return
		}
		if out == nil {
			out = map[string]bool{"ok": true}
		}
		json.NewEncoder(w).Encode(out)
	}
}

func readJSON(r *http.Request, into any) error {
	if err := json.NewDecoder(http.MaxBytesReader(nil, r.Body, maxBody)).Decode(into); err != nil {
		return badRequest("couldn't read the request: " + err.Error())
	}
	return nil
}
