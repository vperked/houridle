// Package gamestore finds cheap Steam games that have trading cards and
// buys one with the Steam wallet when asked.
//
// It never decides to spend: the control panel shows the exact price and
// buys only when you confirm, within your daily limit (see package panel).
// A purchase stops before any money moves unless Steam's final total is no
// more than the price you agreed to.
package gamestore

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

// StoreURL and CheckoutURL are the Steam store and its checkout. Variables
// so tests can use a local fake.
var (
	StoreURL    = "https://store.steampowered.com"
	CheckoutURL = "https://checkout.steampowered.com"
)

const (
	fetchTimeout = 30 * time.Second
	maxBody      = 4 << 20
	pageSize     = 50
	maxPages     = 8
)

// Candidate is a cheap game with trading cards found on the store.
type Candidate struct {
	AppID    uint32
	Name     string
	Price    int // cents, in the store's currency for the country asked
	Discount int // percent off
	Positive int // percent of reviews positive (0 when too few)
	Reviews  int
}

var (
	resultRow = regexp.MustCompile(`(?s)<a [^>]*class="search_result_row[^"]*"[^>]*>.*?</a>`)
	appKey    = regexp.MustCompile(`data-ds-itemkey="App_(\d+)"`)
	rowPrice  = regexp.MustCompile(`data-price-final="(\d+)"`)
	rowOff    = regexp.MustCompile(`data-discount="(\d+)"`)
	rowTitle  = regexp.MustCompile(`class="title">([^<]*)<`)
	rowReview = regexp.MustCompile(`(\d+)% of the ([\d,]+) user reviews`)
)

// Search lists games (no DLC, demos or software) with Steam trading cards
// that cost something but no more than maxPrice cents, cheapest first,
// skipping the ones skip says to. It stops once it has n.
func Search(ctx context.Context, country string, maxPrice, n int, skip func(Candidate) bool) ([]Candidate, error) {
	client := &http.Client{Timeout: fetchTimeout}
	var out []Candidate
	seen := map[uint32]bool{}
	for page := 0; len(out) < n && page < maxPages; page++ {
		q := url.Values{
			"category1": {"998"}, // games
			"category2": {"29"},  // Steam Trading Cards
			"hidef2p":   {"1"},   // not free to play
			"sort_by":   {"Price_ASC"},
			"infinite":  {"1"}, "l": {"english"}, "cc": {country},
			"start": {strconv.Itoa(page * pageSize)}, "count": {strconv.Itoa(pageSize)},
		}
		var res struct {
			HTML  string `json:"results_html"`
			Total int    `json:"total_count"`
		}
		if err := getJSON(ctx, client, StoreURL+"/search/results/?"+q.Encode(), &res); err != nil {
			return out, fmt.Errorf("couldn't search the Steam store: %w", err)
		}
		rows := resultRow.FindAllString(res.HTML, -1)
		for _, row := range rows {
			key, price := appKey.FindStringSubmatch(row), rowPrice.FindStringSubmatch(row)
			if key == nil || price == nil {
				continue // a bundle or package, or no price shown
			}
			id, _ := strconv.ParseUint(key[1], 10, 32)
			cents, _ := strconv.Atoi(price[1])
			if cents > maxPrice {
				return out, nil // sorted by price: the rest cost more
			}
			if id == 0 || cents == 0 || seen[uint32(id)] {
				continue
			}
			seen[uint32(id)] = true
			c := Candidate{AppID: uint32(id), Price: cents}
			if t := rowTitle.FindStringSubmatch(row); t != nil {
				c.Name = strings.TrimSpace(html.UnescapeString(t[1]))
			}
			if d := rowOff.FindStringSubmatch(row); d != nil {
				c.Discount, _ = strconv.Atoi(d[1])
			}
			if r := rowReview.FindStringSubmatch(row); r != nil {
				c.Positive, _ = strconv.Atoi(r[1])
				c.Reviews, _ = strconv.Atoi(strings.ReplaceAll(r[2], ",", ""))
			}
			if skip != nil && skip(c) {
				continue
			}
			out = append(out, c)
			if len(out) == n {
				return out, nil
			}
		}
		if len(rows) == 0 || (page+1)*pageSize >= res.Total {
			break
		}
	}
	return out, nil
}

// Package is what buying a game adds: one of its store packages.
type Package struct {
	ID       uint32
	Name     string // the store's option text, e.g. "UBERMOSH - $0.99"
	Price    int    // cents
	Currency string // ISO code, e.g. "USD"
}

var (
	optionPrice = regexp.MustCompile(`\s+-\s+(<span.*</span>\s*)?[^-]*$`)
	tags        = regexp.MustCompile(`<[^>]+>`)
)

// GamePackage is the package the store sells a game in for the price on its
// page (with any discount), for buyers in country.
func GamePackage(ctx context.Context, appID uint32, country string) (Package, error) {
	client := &http.Client{Timeout: fetchTimeout}
	id := strconv.FormatUint(uint64(appID), 10)
	var res map[string]struct {
		Success bool `json:"success"`
		Data    struct {
			Name  string `json:"name"`
			Price *struct {
				Currency string `json:"currency"`
				Final    int    `json:"final"`
			} `json:"price_overview"`
			PackageGroups []struct {
				Name string `json:"name"`
				Subs []struct {
					PackageID     uint32 `json:"packageid"`
					Option        string `json:"option_text"`
					IsFreeLicense bool   `json:"is_free_license"`
					PriceCents    int    `json:"price_in_cents_with_discount"`
				} `json:"subs"`
			} `json:"package_groups"`
		} `json:"data"`
	}
	q := url.Values{"appids": {id}, "cc": {country}, "l": {"english"}}
	if err := getJSON(ctx, client, StoreURL+"/api/appdetails?"+q.Encode(), &res); err != nil {
		return Package{}, fmt.Errorf("couldn't read the store page for app %d: %w", appID, err)
	}
	app, ok := res[id]
	if !ok || !app.Success || app.Data.Price == nil {
		return Package{}, fmt.Errorf("app %d isn't for sale in your country", appID)
	}
	var best Package
	for _, g := range app.Data.PackageGroups {
		if g.Name != "default" {
			continue
		}
		for _, s := range g.Subs {
			if s.IsFreeLicense || s.PriceCents <= 0 || s.PackageID == 0 {
				continue
			}
			p := Package{ID: s.PackageID, Name: optionName(s.Option), Price: s.PriceCents, Currency: app.Data.Price.Currency}
			if s.PriceCents == app.Data.Price.Final {
				return p, nil // the price the store shows for the game
			}
			if best.ID == 0 || p.Price < best.Price {
				best = p
			}
		}
	}
	if best.ID == 0 {
		return best, fmt.Errorf("couldn't find how the store sells app %d", appID)
	}
	return best, nil
}

// optionName is a package's name without the price the store adds to it.
func optionName(option string) string {
	text := optionPrice.ReplaceAllString(option, "")
	return strings.TrimSpace(html.UnescapeString(tags.ReplaceAllString(text, "")))
}

// ProfileLimited reports whether the store page says the game is still
// "Profile Features Limited": such games don't drop trading cards.
func ProfileLimited(ctx context.Context, appID uint32) (bool, error) {
	client := &http.Client{Timeout: fetchTimeout}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, fmt.Sprintf("%s/app/%d/?l=english", StoreURL, appID), nil)
	if err != nil {
		return false, err
	}
	// Past the age gate, so the real page comes back.
	req.Header.Set("Cookie", "birthtime=0; lastagecheckage=1-0-1990; wants_mature_content=1")
	resp, err := client.Do(req)
	if err != nil {
		return false, shortErr(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return false, fmt.Errorf("the Steam store answered HTTP %d", resp.StatusCode)
	}
	page, _ := io.ReadAll(io.LimitReader(resp.Body, maxBody))
	text := strings.ToLower(string(page))
	return strings.Contains(text, "profile features limited") || strings.Contains(text, "learning_about"), nil
}

// Checkout is the Steam store's checkout, signed in as one account.
type Checkout struct {
	http    *http.Client
	session string
	country string
}

// NewCheckout is a checkout signed in with an access token (a secret: it is
// only sent to the Steam store and its checkout, as the sign-in cookie).
func NewCheckout(steamID uint64, accessToken, country string) (*Checkout, error) {
	wc, err := steamweb.New(steamID, accessToken, fetchTimeout, StoreURL, CheckoutURL)
	if err != nil {
		return nil, err
	}
	return &Checkout{http: wc.HTTP, session: wc.Session, country: country}, nil
}

// ErrUnclear means Steam's answer to the purchase couldn't be read: it may
// have gone through. Check your library before trying again.
var ErrUnclear = errors.New("Steam's answer was unclear; the game may have been bought, so check your library before trying again")

// Start begins paying for everything in the account's cart with the Steam
// wallet. No money moves until Finish; Cancel drops it.
func (c *Checkout) Start(ctx context.Context) (string, error) {
	form := url.Values{
		"gidShoppingCart": {"-1"}, "gidReplayOfTransID": {"-1"}, "bUseAccountCart": {"1"},
		"PaymentMethod": {"steamaccount"}, "abortPendingTransactions": {"0"},
		"bHasCardInfo": {"0"}, "CardNumber": {""}, "CardExpirationYear": {""}, "CardExpirationMonth": {""},
		"FirstName": {""}, "LastName": {""}, "Address": {""}, "AddressTwo": {""}, "Country": {c.country},
		"City": {""}, "State": {""}, "PostalCode": {""}, "Phone": {""},
		"ShippingFirstName": {""}, "ShippingLastName": {""}, "ShippingAddress": {""}, "ShippingAddressTwo": {""},
		"ShippingCountry": {c.country}, "ShippingCity": {""}, "ShippingState": {""}, "ShippingPostalCode": {""}, "ShippingPhone": {""},
		"bIsGift": {"0"}, "GifteeAccountID": {"0"}, "GifteeEmail": {""}, "GifteeName": {""}, "GiftMessage": {""},
		"Sentiment": {""}, "Signature": {""}, "ScheduledSendOnDate": {"0"},
		"BankAccount": {""}, "BankCode": {""}, "BankIBAN": {""}, "BankBIC": {""}, "TPBankID": {""}, "BankAccountID": {""},
		"bSaveBillingAddress": {"0"}, "gidPaymentID": {""}, "bUseRemainingSteamAccount": {"1"}, "bPreAuthOnly": {"0"},
		"sessionid": {c.session},
	}
	var res struct {
		Success int             `json:"success"`
		Detail  int             `json:"purchaseresultdetail"`
		TransID json.RawMessage `json:"transid"`
	}
	if err := c.post(ctx, "/checkout/inittransaction/", form, &res); err != nil {
		return "", err
	}
	id := strings.Trim(string(res.TransID), `"`)
	if res.Success != 1 || id == "" || id == "-1" {
		return "", purchaseError(res.Detail)
	}
	return id, nil
}

// Total is what Steam will take from the wallet for a started purchase,
// in cents, tax included.
func (c *Checkout) Total(ctx context.Context, transID string) (int, error) {
	q := url.Values{"count": {"1"}, "transid": {transID}, "purchasetype": {"self"}, "microtxnid": {"-1"}, "cart": {"-1"}, "gidReplayOfTransID": {"-1"}}
	var res struct {
		Success int         `json:"success"`
		Detail  int         `json:"purchaseresultdetail"`
		Total   json.Number `json:"total"`
	}
	if err := c.get(ctx, "/checkout/getfinalprice/?"+q.Encode(), &res); err != nil {
		return 0, err
	}
	if res.Success != 1 {
		return 0, purchaseError(res.Detail)
	}
	total, err := strconv.Atoi(res.Total.String())
	if err != nil || total <= 0 {
		return 0, errors.New("couldn't read Steam's final price")
	}
	return total, nil
}

// Cancel drops a started purchase (nothing has been paid).
func (c *Checkout) Cancel(ctx context.Context, transID string) {
	var ignore map[string]any
	c.post(ctx, "/checkout/canceltransaction/", url.Values{"transid": {transID}, "sessionid": {c.session}}, &ignore)
}

const (
	pollEvery = 2 * time.Second
	pollFor   = 2 * time.Minute
	pending   = 22 // Steam's "still working on it"
)

// PollEvery is how often Finish asks whether the purchase went through. A
// variable so tests needn't wait.
var PollEvery = pollEvery

// Finish pays for a started purchase and waits for Steam to say it's done.
// This is the step that spends the money.
func (c *Checkout) Finish(ctx context.Context, transID string) error {
	browser := `{"language":"en-US","javaEnabled":"false","colorDepth":24,"screenHeight":1080,"screenWidth":1920}`
	var res struct {
		Success int `json:"success"`
		Detail  int `json:"purchaseresultdetail"`
	}
	if err := c.post(ctx, "/checkout/finalizetransaction/", url.Values{"transid": {transID}, "CardCVV2": {""}, "browserInfo": {browser}}, &res); err != nil {
		return c.wait(ctx, transID) // it may have reached Steam: ask how it went
	}
	if res.Success == pending && res.Detail == 0 {
		return c.wait(ctx, transID)
	}
	return result(res.Success, res.Detail)
}

func (c *Checkout) wait(ctx context.Context, transID string) error {
	deadline := time.Now().Add(pollFor)
	for n := 1; time.Now().Before(deadline); n++ {
		select {
		case <-ctx.Done():
			return ErrUnclear
		case <-time.After(PollEvery):
		}
		var res struct {
			Success int `json:"success"`
			Detail  int `json:"purchaseresultdetail"`
		}
		q := url.Values{"count": {strconv.Itoa(n)}, "transid": {transID}}
		if err := c.get(ctx, "/checkout/transactionstatus/?"+q.Encode(), &res); err != nil {
			continue
		}
		if res.Success == pending && res.Detail == 0 {
			continue
		}
		return result(res.Success, res.Detail)
	}
	return ErrUnclear
}

func result(success, detail int) error {
	switch {
	case success == 1:
		return nil
	case success == pending && detail == 29:
		return errors.New("Steam needs this purchase approved (family settings) before it goes through")
	case success == pending && detail == 86:
		return errors.New("Steam wants you to confirm this purchase in a browser")
	}
	return purchaseError(detail)
}

// purchaseError is Steam's reason a purchase didn't go through.
func purchaseError(detail int) error {
	reasons := map[int]string{
		2:  "your Steam wallet doesn't have enough",
		4:  "Steam timed out; nothing was charged",
		5:  "Steam says that game can't be bought",
		8:  "another purchase is still in progress on Steam; finish or cancel it first",
		9:  "you already own that",
		10: "the price changed; nothing was charged",
		13: "it can't be bought in your country",
		20: "this account can't make purchases",
		21: "this account needs to be verified (email) before buying",
		31: "the price isn't in your wallet's currency",
		32: "your email address needs verifying on Steam before buying",
		44: "the account is locked",
		45: "another purchase is still in progress on Steam; finish or cancel it first",
		46: "Steam's purchase limit was reached; try again later",
		48: "this account has no Steam wallet",
		53: "Steam is limiting purchases; try again in a while",
	}
	if r, ok := reasons[detail]; ok {
		return errors.New(r)
	}
	return fmt.Errorf("Steam didn't take the purchase (reason %d); nothing was charged", detail)
}

func (c *Checkout) post(ctx context.Context, path string, form url.Values, into any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, CheckoutURL+path, strings.NewReader(form.Encode()))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded; charset=UTF-8")
	req.Header.Set("Origin", CheckoutURL)
	req.Header.Set("Referer", CheckoutURL+"/checkout/?accountcart=1")
	return c.do(req, into)
}

func (c *Checkout) get(ctx context.Context, path string, into any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, CheckoutURL+path, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Referer", CheckoutURL+"/checkout/?accountcart=1")
	return c.do(req, into)
}

func (c *Checkout) do(req *http.Request, into any) error {
	resp, err := c.http.Do(req)
	if err != nil {
		return shortErr(err)
	}
	defer resp.Body.Close()
	switch {
	case resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden:
		return errors.New("the Steam checkout didn't accept the sign-in")
	case resp.StatusCode != http.StatusOK:
		return fmt.Errorf("the Steam checkout answered HTTP %d", resp.StatusCode)
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, maxBody)).Decode(into); err != nil {
		return errors.New("couldn't read the Steam checkout's answer (it may not have accepted the sign-in)")
	}
	return nil
}

func getJSON(ctx context.Context, client *http.Client, u string, into any) error {
	return steamweb.GetJSON(ctx, client, u, into, maxBody)
}

func shortErr(err error) error { return steamweb.ShortErr(err) }
