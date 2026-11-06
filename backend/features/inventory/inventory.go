// Package inventory reads the Steam inventory's community items (trading
// cards, gems, booster packs) and, when asked, turns cards into gems and
// gems into booster packs.
//
// Turning cards into gems and making boosters use things up, so the control
// panel only does either when you confirm the exact items (see package
// panel).
package inventory

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"houridle/backend/online/steamweb"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// BaseURL is Steam Community. A variable so tests can use a local fake.
var BaseURL = "https://steamcommunity.com"

const (
	fetchTimeout = 30 * time.Second
	maxBody      = 16 << 20
	pageSize     = 2000
	maxPages     = 10
	steamApp     = 753 // community items live under the "Steam" app, context 6
)

// Item is one stack of a community item in the inventory.
type Item struct {
	AssetID  string
	AppID    uint32 // the game it belongs to
	Game     string
	Name     string
	HashName string // the Market's name for it
	Amount   int
	Card     bool // a trading card
	Foil     bool
	Gems     bool // gems (Amount of them) or a sack of 1000
	Sack     bool
	Booster  bool
}

// Client talks to Steam Community, signed in as one account.
type Client struct {
	steamID uint64
	http    *http.Client
	session string
}

// NewClient is a client signed in with an access token (a secret: it is
// only sent to Steam Community, as the sign-in cookie).
func NewClient(steamID uint64, accessToken string) (*Client, error) {
	wc, err := steamweb.New(steamID, accessToken, fetchTimeout, BaseURL)
	if err != nil {
		return nil, err
	}
	return &Client{steamID: steamID, http: wc.HTTP, session: wc.Session}, nil
}

// Items lists the inventory's community items.
func (c *Client) Items(ctx context.Context) ([]Item, error) {
	type desc struct {
		ClassID    string `json:"classid"`
		InstanceID string `json:"instanceid"`
		Name       string `json:"name"`
		HashName   string `json:"market_hash_name"`
		FeeApp     uint32 `json:"market_fee_app"`
		Tags       []struct {
			Category string `json:"category"`
			Internal string `json:"internal_name"`
			Name     string `json:"localized_tag_name"`
		} `json:"tags"`
	}
	var out []Item
	start := ""
	for page := 0; page < maxPages; page++ {
		q := url.Values{"l": {"english"}, "count": {strconv.Itoa(pageSize)}}
		if start != "" {
			q.Set("start_assetid", start)
		}
		var res struct {
			Success int `json:"success"`
			Assets  []struct {
				AssetID    string `json:"assetid"`
				ClassID    string `json:"classid"`
				InstanceID string `json:"instanceid"`
				Amount     string `json:"amount"`
			} `json:"assets"`
			Descriptions []desc `json:"descriptions"`
			More         int    `json:"more_items"`
			Last         string `json:"last_assetid"`
		}
		if err := c.getJSON(ctx, fmt.Sprintf("%s/inventory/%d/%d/6?%s", BaseURL, c.steamID, steamApp, q.Encode()), &res); err != nil {
			return nil, fmt.Errorf("couldn't read your inventory: %w", err)
		}
		descs := map[string]desc{}
		for _, d := range res.Descriptions {
			descs[d.ClassID+"_"+d.InstanceID] = d
		}
		for _, a := range res.Assets {
			d := descs[a.ClassID+"_"+a.InstanceID]
			n, _ := strconv.Atoi(a.Amount)
			it := Item{AssetID: a.AssetID, AppID: d.FeeApp, Name: d.Name, HashName: d.HashName, Amount: max(1, n)}
			for _, t := range d.Tags {
				switch {
				case t.Category == "item_class" && t.Internal == "item_class_2":
					it.Card = true
				case t.Category == "item_class" && t.Internal == "item_class_5":
					it.Booster = true
				case t.Category == "item_class" && t.Internal == "item_class_7":
					it.Gems = true
				case t.Category == "cardborder" && t.Internal == "cardborder_1":
					it.Foil = true
				case t.Category == "Game":
					it.Game = t.Name
				}
			}
			if it.Gems && strings.Contains(strings.ToLower(d.Name), "sack") {
				it.Sack = true
			}
			out = append(out, it)
		}
		if res.More == 0 || res.Last == "" {
			break
		}
		start = res.Last
	}
	return out, nil
}

// Gems is how many gems the inventory holds (a sack counts as 1000).
func Gems(items []Item) int {
	n := 0
	for _, it := range items {
		switch {
		case it.Sack:
			n += 1000 * it.Amount
		case it.Gems:
			n += it.Amount
		}
	}
	return n
}

// GemValue is how many gems one item turns into.
func (c *Client) GemValue(ctx context.Context, appID uint32, assetID string) (int, error) {
	q := url.Values{"sessionid": {c.session}, "appid": {strconv.FormatUint(uint64(appID), 10)}, "assetid": {assetID}, "contextid": {"6"}}
	var res struct {
		Success  int         `json:"success"`
		GooValue json.Number `json:"goo_value"`
	}
	if err := c.getJSON(ctx, fmt.Sprintf("%s/profiles/%d/ajaxgetgoovalue/?%s", BaseURL, c.steamID, q.Encode()), &res); err != nil {
		return 0, err
	}
	// Steam's answer may leave "success" out; the value is what matters.
	n, err := strconv.Atoi(res.GooValue.String())
	if res.Success > 1 || err != nil || n <= 0 {
		return 0, errors.New("Steam didn't say what that's worth in gems")
	}
	return n, nil
}

// Grind turns one item into gems, if it's still worth what was expected.
// It returns the gems received.
func (c *Client) Grind(ctx context.Context, appID uint32, assetID string, expected int) (int, error) {
	form := url.Values{"sessionid": {c.session}, "appid": {strconv.FormatUint(uint64(appID), 10)}, "assetid": {assetID}, "contextid": {"6"}, "goo_value_expected": {strconv.Itoa(expected)}}
	var res struct {
		Success   int         `json:"success"`
		Received  json.Number `json:"goo_value_received "` // Steam's key really ends in a space
		Received2 json.Number `json:"goo_value_received"`
	}
	if err := c.postJSON(ctx, fmt.Sprintf("%s/profiles/%d/ajaxgrindintogoo/", BaseURL, c.steamID), form, &res); err != nil {
		return 0, err
	}
	got, err := strconv.Atoi(res.Received.String())
	if err != nil {
		got, _ = strconv.Atoi(res.Received2.String())
	}
	if res.Success != 1 && got <= 0 {
		return 0, errors.New("Steam didn't turn it into gems")
	}
	if got <= 0 {
		got = expected
	}
	return got, nil
}

// Booster is a game whose booster pack can be made from gems.
type Booster struct {
	AppID       uint32
	Name        string
	Series      int
	Price       int    // gems
	Available   bool   // Steam lets one be made now (one per game per day)
	AvailableAt string // when, if not now, in Steam's words
}

var (
	boosterInit = regexp.MustCompile(`(?s)CBoosterCreatorPage\.Init\(\s*(\[.*?\])\s*,\s*parseFloat\(\s*"(\d+)"`)
	boosterNone = regexp.MustCompile(`(?s)CBoosterCreatorPage\.Init\(\s*\[\s*\]`)
)

// Boosters lists the games whose booster packs can be made from gems, and
// the gems on hand.
func (c *Client) Boosters(ctx context.Context) ([]Booster, int, error) {
	page, err := c.get(ctx, BaseURL+"/tradingcards/boostercreator/?l=english")
	if err != nil {
		return nil, 0, err
	}
	m := boosterInit.FindStringSubmatch(page)
	if m == nil {
		if strings.Contains(page, "g_steamID = false") {
			return nil, 0, errors.New("Steam Community didn't accept the sign-in")
		}
		if boosterNone.MatchString(page) {
			return []Booster{}, 0, nil // no games eligible
		}
		return nil, 0, errors.New("couldn't read the booster pack creator")
	}
	gems, _ := strconv.Atoi(m[2])
	var raw []struct {
		AppID       uint32      `json:"appid"`
		Name        string      `json:"name"`
		Series      int         `json:"series"`
		Price       json.Number `json:"price"`
		Unavailable bool        `json:"unavailable"`
		AvailableAt string      `json:"available_at_time"`
	}
	if err := json.Unmarshal([]byte(m[1]), &raw); err != nil {
		return nil, 0, errors.New("couldn't read the booster pack creator")
	}
	out := make([]Booster, 0, len(raw))
	for _, r := range raw {
		price, _ := strconv.Atoi(r.Price.String())
		out = append(out, Booster{AppID: r.AppID, Name: r.Name, Series: r.Series, Price: price, Available: !r.Unavailable, AvailableAt: r.AvailableAt})
	}
	return out, gems, nil
}

// MakeBooster turns gems into one booster pack for a game.
func (c *Client) MakeBooster(ctx context.Context, appID uint32, series int) error {
	form := url.Values{"sessionid": {c.session}, "appid": {strconv.FormatUint(uint64(appID), 10)}, "series": {strconv.Itoa(series)}, "tradability_preference": {"1"}}
	var res struct {
		Result *struct {
			Success int `json:"success"`
		} `json:"purchase_result"`
		EResult int `json:"purchase_eresult"`
	}
	if err := c.postJSON(ctx, BaseURL+"/tradingcards/ajaxcreatebooster/", form, &res); err != nil {
		return err
	}
	if res.Result == nil || res.Result.Success != 1 {
		if res.EResult != 0 {
			return fmt.Errorf("Steam didn't make the booster pack (error %d): you may not have enough gems, or made one for this game today", res.EResult)
		}
		return errors.New("Steam didn't make the booster pack")
	}
	return nil
}

func (c *Client) get(ctx context.Context, u string) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return "", err
	}
	return c.do(req)
}

func (c *Client) getJSON(ctx context.Context, u string, into any) error {
	body, err := c.get(ctx, u)
	if err != nil {
		return err
	}
	if err := json.Unmarshal([]byte(body), into); err != nil {
		return errors.New("Steam Community's answer couldn't be read (it may not have accepted the sign-in)")
	}
	return nil
}

func (c *Client) postJSON(ctx context.Context, u string, form url.Values, into any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, u, strings.NewReader(form.Encode()))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded; charset=UTF-8")
	req.Header.Set("Referer", fmt.Sprintf("%s/profiles/%d/inventory/", BaseURL, c.steamID))
	body, err := c.do(req)
	if err != nil {
		return err
	}
	if err := json.Unmarshal([]byte(body), into); err != nil {
		return errors.New("Steam Community's answer couldn't be read")
	}
	return nil
}

func (c *Client) do(req *http.Request) (string, error) {
	resp, err := c.http.Do(req)
	if err != nil {
		return "", fmt.Errorf("couldn't reach Steam Community: %w", steamweb.ShortErr(err))
	}
	defer resp.Body.Close()
	switch {
	case resp.StatusCode == http.StatusTooManyRequests:
		return "", errors.New("Steam Community is limiting requests; try again in a minute")
	case resp.StatusCode == http.StatusForbidden || resp.StatusCode == http.StatusUnauthorized:
		return "", errors.New("Steam Community didn't accept the sign-in (or the inventory is private)")
	case resp.StatusCode != http.StatusOK:
		return "", fmt.Errorf("Steam Community answered HTTP %d", resp.StatusCode)
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, maxBody))
	return string(data), err
}
