// Package market reads the Steam Community Market for trading cards (your
// wallet, card prices, the cheapest listings) and buys a listing when asked.
//
// It never decides to spend: the control panel shows the exact cost and
// buys only when you confirm, within your daily limit (see package panel).
package market

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"houridle/backend/steamweb"
	"html"
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
	maxBody      = 4 << 20
	cardsApp     = 753 // the "Steam" app: trading cards live under it on the Market
)

// Wallet is your Steam wallet, and whether the Market lets you buy.
type Wallet struct {
	Currency int    // Steam currency code (1 = USD, 2 = GBP, 3 = EUR...)
	Balance  int    // in cents (hundredths of the currency)
	Allowed  bool   // the Market is open to the account
	Reason   string // why not, in Steam's words
}

// Price formats cents in the wallet's currency.
func (w Wallet) Price(cents int) string { return FormatPrice(w.Currency, cents) }

// FormatPrice formats cents in a Steam currency.
func FormatPrice(currency, cents int) string {
	sym, after := map[int]string{1: "$", 2: "£", 3: "€", 5: "₽", 7: "R$", 8: "¥", 10: "Rp", 16: "₩", 20: "CA$", 21: "A$"}[currency], false
	if sym == "" {
		sym, after = "", true
	}
	amount := fmt.Sprintf("%d.%02d", cents/100, cents%100)
	if after {
		return amount + " (currency " + strconv.Itoa(currency) + ")"
	}
	return sym + amount
}

// Card is a trading card on the Market.
type Card struct {
	Name     string // as the game names it
	HashName string // the Market's name for it (what listings are under)
	Price    int    // lowest listing, fee included, in cents (US dollars: an estimate for ranking)
	Listings int    // how many are for sale
}

// Listing is one card for sale.
type Listing struct {
	ID       string
	HashName string
	Price    int // what the seller gets, in cents
	Fee      int // Steam's and the game's cut, in cents
	Currency int
}

// Total is what buying it costs.
func (l Listing) Total() int { return l.Price + l.Fee }

// Client talks to the Market, signed in as one account.
type Client struct {
	steamID uint64
	http    *http.Client
	session string // the sessionid the Market's forms are checked against
}

// NewClient is a Market client signed in with an access token (a secret:
// it is only sent to Steam Community, as the sign-in cookie).
func NewClient(steamID uint64, accessToken string) (*Client, error) {
	wc, err := steamweb.New(steamID, accessToken, fetchTimeout, BaseURL)
	if err != nil {
		return nil, err
	}
	return &Client{steamID: steamID, http: wc.HTTP, session: wc.Session}, nil
}

var (
	walletInfo    = regexp.MustCompile(`g_rgWalletInfo = (\{[^;]*\});`)
	tags          = regexp.MustCompile(`<[^>]+>`)
	spaces        = regexp.MustCompile(`\s+`)
	ssrListing    = regexp.MustCompile(`\\?"listingid\\?":\\?"(\d+)\\?",\\?"unPrice\\?":(\d+),\\?"unFee\\?":(\d+),.*?\\?"eCurrency\\?":(\d+)`)
	steamIDOnPage = regexp.MustCompile(`g_steamID = "(\d+)"`)
)

// Wallet reads the wallet and whether the Market is open to the account.
func (c *Client) Wallet(ctx context.Context) (Wallet, error) {
	page, err := c.get(ctx, BaseURL+"/market/?l=english")
	if err != nil {
		return Wallet{}, err
	}
	if m := steamIDOnPage.FindStringSubmatch(page); m == nil || m[1] != strconv.FormatUint(c.steamID, 10) {
		return Wallet{}, errors.New("Steam Community didn't accept the sign-in")
	}
	var w Wallet
	m := walletInfo.FindStringSubmatch(page)
	if m == nil {
		return w, errors.New("couldn't read your wallet from the Market")
	}
	var info struct {
		Currency int    `json:"wallet_currency"`
		Balance  string `json:"wallet_balance"`
	}
	if err := json.Unmarshal([]byte(m[1]), &info); err != nil {
		return w, errors.New("couldn't read your wallet from the Market")
	}
	w.Currency = info.Currency
	w.Balance, _ = strconv.Atoi(info.Balance)
	w.Allowed = true
	if i := strings.Index(page, "market_headertip_container_warning"); i >= 0 {
		w.Allowed = false
		w.Reason = blockReason(page[i:min(len(page), i+3000)])
	}
	return w, nil
}

// blockReason is Steam's reason the Market is closed, as plain text: the
// sentences after "...for the following reason(s):", without the advice
// that follows them.
func blockReason(chunk string) string {
	if j := strings.Index(chunk, ">"); j >= 0 {
		chunk = chunk[j+1:]
	}
	text := strings.TrimSpace(spaces.ReplaceAllString(html.UnescapeString(tags.ReplaceAllString(chunk, " ")), " "))
	if k := strings.Index(text, "reason(s):"); k >= 0 {
		text = strings.TrimSpace(text[k+len("reason(s):"):])
	}
	for _, stop := range []string{" More info", " You can ", " Learn more", " For more"} {
		if k := strings.Index(text, stop); k > 0 {
			text = text[:k]
		}
	}
	if len(text) > 300 {
		text = text[:300] + "…"
	}
	return text
}

// GameCards lists a game's (normal, not foil) trading cards on the Market
// with their lowest prices.
func GameCards(ctx context.Context, appID uint32) ([]Card, error) {
	q := url.Values{
		"query": {""}, "start": {"0"}, "count": {"50"}, "norender": {"1"}, "appid": {strconv.Itoa(cardsApp)}, "currency": {"1"},
		"category_753_Game[]":       {fmt.Sprintf("tag_app_%d", appID)},
		"category_753_item_class[]": {"tag_item_class_2"},
		"category_753_cardborder[]": {"tag_cardborder_0"},
	}
	client := &http.Client{Timeout: fetchTimeout}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, BaseURL+"/market/search/render/?"+q.Encode(), nil)
	if err != nil {
		return nil, err
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, shortErr(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusTooManyRequests {
		return nil, errors.New("the Market is limiting requests; try again in a minute")
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("the Market answered HTTP %d", resp.StatusCode)
	}
	var res struct {
		Success bool `json:"success"`
		Results []struct {
			Name     string `json:"name"`
			HashName string `json:"hash_name"`
			Price    int    `json:"sell_price"`
			Listings int    `json:"sell_listings"`
		} `json:"results"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, maxBody)).Decode(&res); err != nil || !res.Success {
		return nil, errors.New("couldn't read the Market's prices")
	}
	var out []Card
	for _, r := range res.Results {
		out = append(out, Card{Name: strings.TrimSuffix(r.Name, " (Trading Card)"), HashName: r.HashName, Price: r.Price, Listings: r.Listings})
	}
	return out, nil
}

// ListingURL is a card's Market page.
func ListingURL(hashName string) string {
	return fmt.Sprintf("%s/market/listings/%d/%s", BaseURL, cardsApp, url.PathEscape(hashName))
}

// Cheapest is the cheapest listing for a card, in the wallet's currency.
func (c *Client) Cheapest(ctx context.Context, hashName string) (Listing, error) {
	page, err := c.get(ctx, ListingURL(hashName)+"?l=english")
	if err != nil {
		return Listing{}, err
	}
	var best Listing
	for _, m := range ssrListing.FindAllStringSubmatch(page, -1) {
		price, _ := strconv.Atoi(m[2])
		fee, _ := strconv.Atoi(m[3])
		cur, _ := strconv.Atoi(m[4])
		l := Listing{ID: m[1], HashName: hashName, Price: price, Fee: fee, Currency: cur}
		if best.ID == "" || l.Total() < best.Total() {
			best = l
		}
	}
	if best.ID == "" {
		return best, fmt.Errorf("no %s for sale right now", hashName)
	}
	return best, nil
}

// ErrUnclear means Steam's answer to a purchase couldn't be read: it may
// have gone through. Check the inventory before trying again.
var ErrUnclear = errors.New("Steam's answer was unclear; the card may have been bought, so check your inventory before trying again")

// Buy buys a listing at exactly its price. Steam refuses if the price
// changed or it was sold; no money moves then.
func (c *Client) Buy(ctx context.Context, l Listing) error {
	form := url.Values{
		"sessionid":       {c.session},
		"currency":        {strconv.Itoa(l.Currency)},
		"subtotal":        {strconv.Itoa(l.Price)},
		"fee":             {strconv.Itoa(l.Fee)},
		"total":           {strconv.Itoa(l.Total())},
		"quantity":        {"1"},
		"billing_state":   {""},
		"save_my_address": {"0"},
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, fmt.Sprintf("%s/market/buylisting/%s", BaseURL, l.ID), strings.NewReader(form.Encode()))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Referer", ListingURL(l.HashName)) // the Market checks purchases come from the card's page
	resp, err := c.http.Do(req)
	if err != nil {
		return ErrUnclear // it may have reached Steam
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(io.LimitReader(resp.Body, maxBody))
	var res struct {
		WalletInfo *struct {
			Success int `json:"success"`
		} `json:"wallet_info"`
		Message string `json:"message"`
	}
	if json.Unmarshal(data, &res) != nil {
		return ErrUnclear
	}
	switch {
	case res.WalletInfo != nil && res.WalletInfo.Success == 1:
		return nil
	case res.Message != "":
		return errors.New(strings.TrimSpace(html.UnescapeString(tags.ReplaceAllString(res.Message, " "))))
	}
	return ErrUnclear
}

func (c *Client) get(ctx context.Context, u string) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return "", err
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return "", shortErr(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusTooManyRequests {
		return "", errors.New("the Market is limiting requests; try again in a minute")
	}
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("the Market answered HTTP %d", resp.StatusCode)
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, maxBody))
	return string(data), err
}

func shortErr(err error) error {
	return fmt.Errorf("couldn't reach the Steam Market: %w", steamweb.ShortErr(err))
}
