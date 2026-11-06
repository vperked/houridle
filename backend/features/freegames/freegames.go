// Package freegames finds games Steam is giving away to keep (100% off for
// a limited time) and adds them to your account, like pressing "Add to
// Account" on their store pages.
//
// It only adds licenses Steam offers for free. Free weekends (play free,
// don't keep) aren't 100% off, so they never match.
package freegames

import (
	"context"
	"errors"
	"fmt"
	"houridle/backend/online/steamweb"
	"html"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"
)

// StoreURL is the Steam store. A variable so tests can use a local fake.
var StoreURL = "https://store.steampowered.com"

const (
	pageSize     = 100
	maxPages     = 10
	maxBody      = 4 << 20
	fetchTimeout = 30 * time.Second
)

// Offer is something Steam is giving away right now.
type Offer struct {
	AppID     uint32
	SubID     uint32 // the free package that adds it
	Name      string
	DLC       bool
	BaseAppID uint32 // for DLC: the game it needs
	BaseName  string
	Review    Review
}

// Review is a game's Steam user-review summary.
type Review struct {
	Summary  string // e.g. "Very Positive"; "" if there aren't enough reviews
	Positive int    // percent positive
	Count    int    // number of reviews
}

// better orders reviews best first: higher share positive, then more reviews.
func (r Review) better(o Review) bool {
	if r.Positive != o.Positive {
		return r.Positive > o.Positive
	}
	return r.Count > o.Count
}

func (o Offer) String() string {
	if o.DLC && o.BaseName != "" {
		return o.Name + " (DLC for " + o.BaseName + ")"
	}
	return o.Name
}

var (
	resultRow = regexp.MustCompile(`(?s)<a [^>]*class="search_result_row[^"]*"[^>]*>.*?</a>`)
	itemKey   = regexp.MustCompile(`data-ds-itemkey="(App|Sub|Bundle)_(\d+)"`)
	fullOff   = regexp.MustCompile(`data-discount="100"`)
	freePrice = regexp.MustCompile(`data-price-final="0"`)
	rowTitle  = regexp.MustCompile(`class="title">([^<]*)<`)
	rowReview = regexp.MustCompile(`search_review_summary[^"]*"\s+data-tooltip-html="([^"<&]*)(?:&lt;|<)br(?:&gt;|>)(\d+)% of the ([\d,]+) user reviews`)
)

// parseReview reads the review summary a store search row carries.
func parseReview(row string) Review {
	m := rowReview.FindStringSubmatch(row)
	if m == nil {
		return Review{}
	}
	pct, _ := strconv.Atoi(m[2])
	count, _ := strconv.Atoi(strings.ReplaceAll(m[3], ",", ""))
	return Review{Summary: strings.TrimSpace(html.UnescapeString(m[1])), Positive: pct, Count: count}
}

// Find lists what Steam is giving away to keep right now.
func Find(ctx context.Context) ([]Offer, error) {
	client := &http.Client{Timeout: fetchTimeout}
	type candidate struct {
		kind   string
		id     uint32
		name   string
		review Review
	}
	var found []candidate
	seen := map[string]bool{}
	for page := 0; page < maxPages; page++ {
		q := url.Values{"maxprice": {"free"}, "specials": {"1"}, "infinite": {"1"}, "l": {"english"},
			"start": {strconv.Itoa(page * pageSize)}, "count": {strconv.Itoa(pageSize)}}
		var res struct {
			HTML  string `json:"results_html"`
			Total int    `json:"total_count"`
		}
		if err := getJSON(ctx, client, StoreURL+"/search/results/?"+q.Encode(), &res); err != nil {
			return nil, fmt.Errorf("couldn't search the Steam store: %w", err)
		}
		for _, row := range resultRow.FindAllString(res.HTML, -1) {
			key := itemKey.FindStringSubmatch(row)
			if key == nil || !fullOff.MatchString(row) || !freePrice.MatchString(row) {
				continue // free-to-play, or not a 100%-off promotion
			}
			id, err := strconv.ParseUint(key[2], 10, 32)
			if err != nil || seen[key[1]+key[2]] {
				continue
			}
			seen[key[1]+key[2]] = true
			name := ""
			if t := rowTitle.FindStringSubmatch(row); t != nil {
				name = strings.TrimSpace(html.UnescapeString(t[1]))
			}
			found = append(found, candidate{kind: key[1], id: uint32(id), name: name, review: parseReview(row)})
		}
		if (page+1)*pageSize >= res.Total {
			break
		}
	}

	var offers []Offer
	for _, c := range found {
		switch c.kind {
		case "Sub":
			offers = append(offers, Offer{SubID: c.id, Name: c.name, Review: c.review})
		case "App":
			o, ok, err := appOffer(ctx, client, c.id)
			if err != nil {
				return offers, err
			}
			if ok {
				o.Review = c.review
				offers = append(offers, o)
			}
		}
		// Bundles can't be added as one free license; their games show up
		// as their own results when they're free.
	}
	// Best reviewed first.
	slices.SortStableFunc(offers, func(a, b Offer) int {
		switch {
		case a.Review.better(b.Review):
			return -1
		case b.Review.better(a.Review):
			return 1
		}
		return 0
	})
	return offers, nil
}

// FreeToPlayGame is an always-free game on the Steam store.
type FreeToPlayGame struct {
	AppID  uint32
	Name   string
	Review Review
}

// Features narrows the free-to-play list to games with Steam features, and
// says how to order it.
type Features struct {
	Achievements bool   // has Steam achievements
	Cards        bool   // has Steam trading cards (they drop only after spending money in a free game)
	Sort         string // "reviews" (best reviewed, the default), "popular" (most reviews), "newest" or "name"
}

// storeSort is the store search order for a Sort.
func (f Features) storeSort() string {
	switch f.Sort {
	case "newest":
		return "Released_DESC"
	case "name":
		return "Name_ASC"
	case "popular":
		return "" // the store's own relevance, which favours popular games; re-sorted by reviews below
	}
	return "Reviews_DESC"
}

func (f Features) category2() string {
	var ids []string
	if f.Achievements {
		ids = append(ids, "22")
	}
	if f.Cards {
		ids = append(ids, "29")
	}
	return strings.Join(ids, ",") // the store requires all of them
}

// TopFreeToPlay lists the best-reviewed free-to-play games (games only:
// no demos, DLC or software) with the features asked for, best first.
func TopFreeToPlay(ctx context.Context, n int, want Features) ([]FreeToPlayGame, error) {
	client := &http.Client{Timeout: fetchTimeout}
	var out []FreeToPlayGame
	seen := map[uint32]bool{}
	for page := 0; len(out) < n && page < maxPages; page++ {
		q := url.Values{"maxprice": {"free"}, "category1": {"998"}, "infinite": {"1"}, "l": {"english"},
			"start": {strconv.Itoa(page * pageSize)}, "count": {strconv.Itoa(pageSize)}}
		if sort := want.storeSort(); sort != "" {
			q.Set("sort_by", sort)
		}
		if c := want.category2(); c != "" {
			q.Set("category2", c)
		}
		var res struct {
			HTML  string `json:"results_html"`
			Total int    `json:"total_count"`
		}
		if err := getJSON(ctx, client, StoreURL+"/search/results/?"+q.Encode(), &res); err != nil {
			return nil, fmt.Errorf("couldn't search the Steam store: %w", err)
		}
		rows := resultRow.FindAllString(res.HTML, -1)
		for _, row := range rows {
			key := itemKey.FindStringSubmatch(row)
			if key == nil || key[1] != "App" || !freePrice.MatchString(row) && strings.Contains(row, "data-price-final=") {
				continue
			}
			id, err := strconv.ParseUint(key[2], 10, 32)
			if err != nil || seen[uint32(id)] {
				continue
			}
			seen[uint32(id)] = true
			name := ""
			if t := rowTitle.FindStringSubmatch(row); t != nil {
				name = strings.TrimSpace(html.UnescapeString(t[1]))
			}
			out = append(out, FreeToPlayGame{AppID: uint32(id), Name: name, Review: parseReview(row)})
			if len(out) == n {
				break
			}
		}
		if len(rows) == 0 || (page+1)*pageSize >= res.Total {
			break
		}
	}
	if want.Sort == "popular" {
		slices.SortStableFunc(out, func(a, b FreeToPlayGame) int { return b.Review.Count - a.Review.Count })
	}
	return out, nil
}

// appOffer reads a store app's free package from the store's app details.
func appOffer(ctx context.Context, client *http.Client, appID uint32) (Offer, bool, error) {
	id := strconv.FormatUint(uint64(appID), 10)
	var res map[string]struct {
		Success bool `json:"success"`
		Data    struct {
			Type     string `json:"type"`
			Name     string `json:"name"`
			FullGame struct {
				AppID string `json:"appid"`
				Name  string `json:"name"`
			} `json:"fullgame"`
			PackageGroups []struct {
				Subs []struct {
					PackageID     uint32 `json:"packageid"`
					IsFreeLicense bool   `json:"is_free_license"`
					PriceCents    int    `json:"price_in_cents_with_discount"`
				} `json:"subs"`
			} `json:"package_groups"`
		} `json:"data"`
	}
	if err := getJSON(ctx, client, StoreURL+"/api/appdetails?l=english&appids="+id, &res); err != nil {
		return Offer{}, false, fmt.Errorf("couldn't read the store page for app %d: %w", appID, err)
	}
	app, ok := res[id]
	if !ok || !app.Success {
		return Offer{}, false, nil
	}
	o := Offer{AppID: appID, Name: app.Data.Name, DLC: app.Data.Type == "dlc", BaseName: app.Data.FullGame.Name}
	if base, err := strconv.ParseUint(app.Data.FullGame.AppID, 10, 32); err == nil {
		o.BaseAppID = uint32(base)
	}
	for _, g := range app.Data.PackageGroups {
		for _, sub := range g.Subs {
			if sub.IsFreeLicense && sub.PriceCents == 0 && sub.PackageID != 0 {
				o.SubID = sub.PackageID
				return o, true, nil
			}
		}
	}
	return o, false, nil // free to play rather than a giveaway: nothing to add
}

// ErrNotSignedIn means the store didn't take the sign-in.
var ErrNotSignedIn = errors.New("the Steam store didn't accept the sign-in")

// Claim adds one free package to the account, as the store's "Add to
// Account" button does. accessToken is a Steam access token for steamID
// (a secret: it is only sent to the Steam store, as the sign-in cookie).
func Claim(ctx context.Context, steamID uint64, accessToken string, subID uint32) error {
	wc, err := steamweb.New(steamID, accessToken, fetchTimeout, StoreURL)
	if err != nil {
		return err
	}
	client := wc.HTTP
	form := url.Values{"ajax": {"true"}, "sessionid": {wc.Session}}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, fmt.Sprintf("%s/checkout/addfreelicense/%d", StoreURL, subID), strings.NewReader(form.Encode()))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	resp, err := client.Do(req)
	if err != nil {
		return shortErr(err)
	}
	defer resp.Body.Close()
	io.Copy(io.Discard, io.LimitReader(resp.Body, maxBody))
	switch {
	case resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden:
		return ErrNotSignedIn
	case resp.StatusCode != http.StatusOK:
		return fmt.Errorf("the Steam store answered HTTP %d", resp.StatusCode)
	}
	return nil
}

func getJSON(ctx context.Context, client *http.Client, u string, into any) error {
	return steamweb.GetJSON(ctx, client, u, into, maxBody)
}

func shortErr(err error) error { return steamweb.ShortErr(err) }
