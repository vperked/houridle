package cards

import (
	"context"
	"encoding/json"
	"fmt"
	"houridle/backend/client/steam"
	"houridle/backend/online/steamweb"
	"html"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"sync"
)

// Badge is a game badge's progress: the cards collected toward it, and
// whether a full set is there to craft.
type Badge struct {
	AppID     uint32
	Name      string
	Collected int  // different cards of the set you have
	Total     int  // cards in the set
	Ready     bool // a full set: it can be crafted now
	Foil      bool // a full foil set can be crafted
}

// Missing is how many cards the set still needs.
func (b Badge) Missing() int { return max(0, b.Total-b.Collected) }

var (
	cardsCollected = regexp.MustCompile(`(\d+) of (\d+) cards collected`)
	craftButton    = regexp.MustCompile(`class="badge_craft_button[^"]*"[^>]*href="[^"]*/gamecards/(\d+)/?(\?border=1)?`)
)

// Badges reads every game badge on the badge pages: progress toward each,
// and which are ready to craft.
func Badges(ctx context.Context, steamID uint64, accessToken string) ([]Badge, error) {
	var out []Badge
	err := eachBadgePage(ctx, steamID, accessToken, func(page string) {
		out = append(out, ParseBadges(page)...)
	})
	return out, err
}

// ParseBadges reads the game badges on one badge page.
func ParseBadges(page string) []Badge {
	var out []Badge
	starts := rowStart.FindAllStringIndex(page, -1)
	byApp := map[uint32]int{}
	for i, s := range starts {
		end := len(page)
		if i+1 < len(starts) {
			end = starts[i+1][0]
		}
		row := page[s[0]:end]
		app := appLink.FindStringSubmatch(row)
		if app == nil {
			continue // a community or event badge without cards
		}
		id64, err := strconv.ParseUint(app[1], 10, 32)
		if err != nil || id64 == 0 {
			continue
		}
		id := uint32(id64)
		b := Badge{AppID: id}
		if t := title.FindStringSubmatch(row); t != nil {
			b.Name = strings.TrimSpace(html.UnescapeString(t[1]))
		}
		if m := cardsCollected.FindStringSubmatch(row); m != nil {
			b.Collected, _ = strconv.Atoi(m[1])
			b.Total, _ = strconv.Atoi(m[2])
		}
		for _, m := range craftButton.FindAllStringSubmatch(row, -1) {
			if m[2] != "" {
				b.Foil = true
			} else {
				b.Ready = true
			}
		}
		if b.Ready && b.Total > 0 {
			b.Collected = b.Total
		}
		if j, ok := byApp[id]; ok { // the foil badge has its own row
			out[j].Foil = out[j].Foil || b.Foil
			out[j].Ready = out[j].Ready || b.Ready
			continue
		}
		if b.Name == "" {
			b.Name = steam.PlaceholderName(id)
		}
		byApp[id] = len(out)
		out = append(out, b)
	}
	return out
}

// Closest is the badges you could finish, fewest missing cards first (the
// cheapest to complete by buying cards).
func Closest(badges []Badge) []Badge {
	var out []Badge
	for _, b := range badges {
		if b.Total > 0 && !b.Ready && b.Collected > 0 {
			out = append(out, b)
		}
	}
	slices.SortStableFunc(out, func(a, b Badge) int { return a.Missing() - b.Missing() })
	return out
}

// Card is one card of a game's set.
type Card struct {
	Name   string
	Owned  int    // how many you have
	Market string // its Steam Market page
}

var (
	setCard  = regexp.MustCompile(`(?s)<div class="badge_card_set_card (owned|unowned)">(.*?)<div class="badge_card_set_text ellipsis">`)
	cardQty  = regexp.MustCompile(`badge_card_set_text_qty">\((\d+)\)<`)
	cardName = regexp.MustCompile(`(?s)badge_card_set_title[^>]*>\s*(?:<div class="badge_card_set_text_qty">[^<]*</div>)?\s*([^<]+?)\s*<`)
)

// SetCards lists a game's trading cards and how many of each you have,
// with each card's Market page (to buy the ones you're missing).
func SetCards(ctx context.Context, steamID uint64, accessToken string, appID uint32) ([]Card, error) {
	client, err := newClient(steamID, accessToken)
	if err != nil {
		return nil, err
	}
	page, err := fetch(ctx, client.HTTP, fmt.Sprintf("%s/profiles/%d/gamecards/%d/?l=english", BaseURL, steamID, appID))
	if err != nil {
		return nil, err
	}
	if m := signedIn.FindStringSubmatch(page); m == nil || m[1] != strconv.FormatUint(steamID, 10) {
		return nil, ErrNotSignedIn
	}
	return ParseSetCards(page, appID), nil
}

// ParseSetCards reads the cards on a game's card page.
func ParseSetCards(page string, appID uint32) []Card {
	var out []Card
	for _, m := range setCard.FindAllStringSubmatch(page, -1) {
		c := Card{}
		if n := cardName.FindStringSubmatch(m[2]); n != nil {
			c.Name = strings.TrimSpace(html.UnescapeString(n[1]))
		}
		if m[1] == "owned" {
			c.Owned = 1
			if q := cardQty.FindStringSubmatch(m[2]); q != nil {
				c.Owned, _ = strconv.Atoi(q[1])
			}
		}
		if c.Name != "" {
			// A trading card's Market name is "<app ID>-<card name>".
			c.Market = fmt.Sprintf("https://steamcommunity.com/market/listings/753/%s", url.PathEscape(fmt.Sprintf("%d-%s", appID, c.Name)))
		}
		out = append(out, c)
	}
	return out
}

// MarketSearch is the Steam Market page listing all of a game's trading
// cards (normal, not foil).
func MarketSearch(appID uint32) string {
	return fmt.Sprintf("https://steamcommunity.com/market/search?appid=753&category_753_Game%%5B%%5D=tag_app_%d&category_753_item_class%%5B%%5D=tag_item_class_2&category_753_cardborder%%5B%%5D=tag_cardborder_0", appID)
}

// Crafted is a badge that was crafted.
type Crafted struct {
	Level int // the badge's level now (0 if Steam didn't say)
	XP    int // XP the badge gives (0 if Steam didn't say)
}

// Craft crafts a game's badge (foil for the foil one) from a full set of
// its cards, as the badge page's Craft button does. The cards are used up.
func Craft(ctx context.Context, steamID uint64, accessToken string, appID uint32, foil bool) (Crafted, error) {
	client, err := newClient(steamID, accessToken)
	if err != nil {
		return Crafted{}, err
	}

	border := "0"
	if foil {
		border = "1"
	}
	form := url.Values{"appid": {strconv.FormatUint(uint64(appID), 10)}, "series": {"1"}, "border_color": {border}, "sessionid": {client.Session}}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, fmt.Sprintf("%s/profiles/%d/ajaxcraftbadge/", BaseURL, steamID), strings.NewReader(form.Encode()))
	if err != nil {
		return Crafted{}, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	resp, err := client.HTTP.Do(req)
	if err != nil {
		return Crafted{}, fmt.Errorf("couldn't reach Steam Community: %w", steamweb.ShortErr(err))
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(io.LimitReader(resp.Body, maxPageBytes))
	if resp.StatusCode != http.StatusOK {
		return Crafted{}, fmt.Errorf("Steam Community answered HTTP %d", resp.StatusCode)
	}
	var res struct {
		Success json.RawMessage `json:"success"`
		Badge   struct {
			Level json.Number `json:"level"`
			XP    json.Number `json:"xp"`
		} `json:"Badge"`
	}
	if err := json.Unmarshal(data, &res); err != nil {
		return Crafted{}, fmt.Errorf("Steam Community's answer wasn't readable")
	}
	if s := strings.Trim(string(res.Success), `"`); s != "1" && s != "true" {
		return Crafted{}, fmt.Errorf("Steam didn't craft it (code %s): the set may not be complete any more", s)
	}
	level, _ := res.Badge.Level.Int64()
	xp, _ := res.Badge.XP.Int64()
	return Crafted{Level: int(level), XP: int(xp)}, nil
}

// eachBadgePage fetches every badge page and hands each to fn.
func eachBadgePage(ctx context.Context, steamID uint64, accessToken string, fn func(page string)) error {
	client, err := newClient(steamID, accessToken)
	if err != nil {
		return err
	}
	read := func(p int) (string, error) {
		page, err := fetch(ctx, client.HTTP, fmt.Sprintf("%s/profiles/%d/badges/?l=english&p=%d", BaseURL, steamID, p))
		if err != nil {
			return "", err
		}
		if m := signedIn.FindStringSubmatch(page); m == nil || m[1] != strconv.FormatUint(steamID, 10) {
			return "", ErrNotSignedIn
		}
		return page, nil
	}
	first, err := read(1)
	if err != nil {
		return err
	}
	fn(first)
	// The pages each page links to are fetched a few at a time, then handed
	// to fn in order. (A page may link further than the first one did.)
	last, next := lastLinked(first, 1), 2
	for next <= last && next <= maxPages {
		batch := make([]string, min(last, maxPages)-next+1)
		errs := make([]error, len(batch))
		var wg sync.WaitGroup
		sem := make(chan struct{}, pageFetches)
		for i := range batch {
			wg.Add(1)
			go func() {
				defer wg.Done()
				sem <- struct{}{}
				defer func() { <-sem }()
				batch[i], errs[i] = read(next + i)
			}()
		}
		wg.Wait()
		for i, page := range batch {
			if errs[i] != nil {
				return errs[i]
			}
			fn(page)
			last = lastLinked(page, last)
		}
		next += len(batch)
	}
	return nil
}

// pageFetches is how many badge pages are fetched at once.
const pageFetches = 4

// lastLinked is the highest page number a badge page links to, or atLeast.
func lastLinked(page string, atLeast int) int {
	for _, m := range pageLink.FindAllStringSubmatch(page, -1) {
		if n, _ := strconv.Atoi(m[1]); n > atLeast {
			atLeast = n
		}
	}
	return atLeast
}

// Craftable is a badge with a full card set: the normal one, or a foil.
type Craftable struct {
	AppID uint32
	Name  string
	Foil  bool
}

// Label is the badge's name, "foil" first for a foil badge.
func (c Craftable) Label() string {
	if c.Foil {
		return "foil " + c.Name
	}
	return c.Name
}

// ReadyToCraft lists the badges that can be crafted now.
func ReadyToCraft(badges []Badge) []Craftable {
	var out []Craftable
	for _, b := range badges {
		if b.Ready {
			out = append(out, Craftable{b.AppID, b.Name, false})
		}
		if b.Foil {
			out = append(out, Craftable{b.AppID, b.Name, true})
		}
	}
	return out
}

// MaxLevels is the most levels a game badge has (a foil badge has one).
const MaxLevels = 5

// CraftReady crafts each ready badge once per full card set, up to its
// last level, with craft doing each craft. It tells crafted about each one,
// and failed about a badge that couldn't be crafted at all.
func CraftReady(ctx context.Context, ready []Craftable, craft func(ctx context.Context, appID uint32, foil bool) (Crafted, error), crafted func(Craftable, Crafted), failed func(Craftable, error)) {
	for _, c := range ready {
		levels := MaxLevels
		if c.Foil {
			levels = 1
		}
		for i := 0; i < levels && ctx.Err() == nil; i++ {
			got, err := craft(ctx, c.AppID, c.Foil)
			if err != nil {
				if i == 0 {
					failed(c, err)
				}
				break // no more full sets
			}
			crafted(c, got)
		}
	}
}

// CraftedText says what crafting did: "Crafted the Rust badge (level 2)".
func CraftedText(c Craftable, got Crafted) string {
	text := "Crafted the " + c.Label() + " badge"
	if got.Level > 0 {
		text += fmt.Sprintf(" (level %d)", got.Level)
	}
	return text
}
