package tests

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"houridle/backend/features/cards"
	"houridle/backend/features/market"
	"houridle/backend/online/steamauth"
	"houridle/backend/run/config"
)

// fakeMarket stands in for Steam Community: badge and card pages, the
// Market's wallet, prices and listings, and buying.
type fakeMarket struct {
	*httptest.Server
	mu      sync.Mutex
	balance int
	blocked string         // why the Market is closed ("" open)
	price   map[string]int // hash name -> listing price (fee is 2 on top)
	owned   map[string]int // card name -> how many you have
	bought  []string       // listing IDs bought
	posts   []map[string]string

	// The account's record, as its profile shows it.
	vacBans, gameBans int
	tradeBan          string
	limited           bool
}

func newFakeMarket(t *testing.T) *fakeMarket {
	f := &fakeMarket{balance: 1000, price: map[string]int{"252490-Bye Zombies": 11, "252490-I love my rock (Trading Card)": 13}, owned: map[string]int{"Killer wood pile": 2}}
	sid := steamauth.FormatSteamID(alice)
	f.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		defer f.mu.Unlock()
		p := r.URL.Path
		switch {
		case p == "/profiles/"+sid+"/" && r.URL.Query().Get("xml") == "1":
			vac, lim, trade := 0, 0, f.tradeBan
			if f.vacBans > 0 {
				vac = 1
			}
			if f.limited {
				lim = 1
			}
			if trade == "" {
				trade = "None"
			}
			fmt.Fprintf(w, "<?xml version=\"1.0\"?><profile><steamID64>%s</steamID64><vacBanned>%d</vacBanned><tradeBanState>%s</tradeBanState><isLimitedAccount>%d</isLimitedAccount></profile>", sid, vac, trade, lim)
		case p == "/profiles/"+sid+"/":
			io.WriteString(w, "<html><body>")
			if f.vacBans+f.gameBans > 0 {
				io.WriteString(w, `<div class="profile_ban_status"><div class="profile_ban">`)
				if f.vacBans > 0 {
					fmt.Fprintf(w, "%d VAC ban%s on record ", f.vacBans, map[bool]string{true: "s"}[f.vacBans > 1])
				}
				if f.gameBans > 0 {
					fmt.Fprintf(w, "%d game ban on record ", f.gameBans)
				}
				io.WriteString(w, `<span class="profile_ban_info">| <a class="whiteLink" href="x">Info</a></span></div>
					12 day(s) since last ban</div>`)
			}
			io.WriteString(w, `<div class="profile_header">Rust fan</div></body></html>`)
		case strings.HasSuffix(p, "/badges/"):
			fmt.Fprintf(w, `<script>g_steamID = "%s";</script><div class="badge_row is_link"><a href="/gamecards/252490/"></a><div class="badge_title"> Rust &nbsp;</div><div class="badge_progress_info"> 1 of 3 cards collected </div></div>`, sid)
		case strings.HasSuffix(p, "/gamecards/252490/"):
			fmt.Fprintf(w, `<script>g_steamID = "%s";</script>`, sid)
			for _, name := range []string{"Killer wood pile", "Bye Zombies", "I love my rock"} {
				state, qty := "unowned", ""
				if n := f.owned[name]; n > 0 {
					state, qty = "owned", fmt.Sprintf(`<div class="badge_card_set_text_qty">(%d)</div>`, n)
				}
				fmt.Fprintf(w, `<div class="badge_card_set_card %s"><div class="badge_card_set_text badge_card_set_title ellipsis"> %s %s <div></div></div><div class="badge_card_set_text ellipsis"> x </div></div>`, state, qty, name)
			}
		case p == "/market/":
			fmt.Fprintf(w, `<script>g_steamID = "%s"; var g_rgWalletInfo = {"wallet_currency":1,"wallet_balance":"%d","success":1};</script>`, sid, f.balance)
			if f.blocked != "" {
				fmt.Fprintf(w, `<div class="market_headertip_container market_headertip_container_warning"><div class="market_warning_icon"></div><div> The Market is unavailable for the following reason(s): </div><ul><li>%s</li></ul> You can attempt to check again later. </div>`, f.blocked)
			}
		case p == "/market/search/render/":
			var res []map[string]any
			for _, name := range []string{"Killer wood pile", "Bye Zombies", "I love my rock (Trading Card)"} {
				res = append(res, map[string]any{"name": name, "hash_name": "252490-" + name, "sell_price": f.price["252490-"+name] + 2, "sell_listings": 100})
			}
			json.NewEncoder(w).Encode(map[string]any{"success": true, "results": res})
		case strings.HasPrefix(p, "/market/listings/753/"):
			hash := strings.TrimPrefix(p, "/market/listings/753/")
			// Like the new Market page: the listings ride in escaped JSON.
			fmt.Fprintf(w, `<html>{\"listings\":[{\"listingid\":\"9%d\",\"unPrice\":%d,\"unFee\":2,\"publisherFeeApp\":252490,\"eCurrency\":1,\"strSubtotal\":\"x\"}]}</html>`, len(hash), f.price[hash])
		case strings.HasPrefix(p, "/market/buylisting/"):
			r.ParseForm()
			sidc, _ := r.Cookie("sessionid")
			post := map[string]string{"id": strings.TrimPrefix(p, "/market/buylisting/"), "referer": r.Header.Get("Referer")}
			for k := range r.PostForm {
				post[k] = r.PostForm.Get(k)
			}
			f.posts = append(f.posts, post)
			if sidc == nil || r.PostForm.Get("sessionid") != sidc.Value || !strings.Contains(post["referer"], "/market/listings/753/") {
				w.WriteHeader(http.StatusForbidden)
				return
			}
			var total int
			fmt.Sscan(post["total"], &total)
			f.balance -= total
			f.bought = append(f.bought, post["id"])
			ref := post["referer"]
			for _, name := range []string{"Bye Zombies", "I love my rock"} {
				if strings.Contains(ref, strings.ReplaceAll(name, " ", "%20")) {
					f.owned[name]++
				}
			}
			io.WriteString(w, `{"wallet_info":{"success":1}}`)
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(f.Close)
	oldM, oldC := market.BaseURL, cards.BaseURL
	market.BaseURL, cards.BaseURL = f.URL, f.URL
	t.Cleanup(func() { market.BaseURL, cards.BaseURL = oldM, oldC })
	return f
}

func TestMarketWalletPricesAndBuying(t *testing.T) {
	f := newFakeMarket(t)
	mc, err := market.NewClient(alice, "tok")
	if err != nil {
		t.Fatal(err)
	}
	w, err := mc.Wallet(context.Background())
	if err != nil || w.Balance != 1000 || w.Currency != 1 || !w.Allowed {
		t.Fatalf("wallet: %+v %v", w, err)
	}
	f.blocked = "your purchase isn't 7 days old yet"
	if w, _ := mc.Wallet(context.Background()); w.Allowed || w.Reason != "your purchase isn't 7 days old yet" {
		t.Errorf("a closed Market: %+v", w)
	}
	f.blocked = ""

	cardsOnMarket, err := market.GameCards(context.Background(), 252490)
	if err != nil || len(cardsOnMarket) != 3 || cardsOnMarket[2].Name != "I love my rock" || cardsOnMarket[2].HashName != "252490-I love my rock (Trading Card)" {
		t.Fatalf("cards: %+v %v", cardsOnMarket, err)
	}
	l, err := mc.Cheapest(context.Background(), "252490-Bye Zombies")
	if err != nil || l.Price != 11 || l.Fee != 2 || l.Total() != 13 || l.Currency != 1 {
		t.Fatalf("listing: %+v %v", l, err)
	}
	if err := mc.Buy(context.Background(), l); err != nil {
		t.Fatal(err)
	}
	post := f.posts[0]
	if post["subtotal"] != "11" || post["fee"] != "2" || post["total"] != "13" || post["currency"] != "1" || post["quantity"] != "1" {
		t.Errorf("purchase form: %v", post)
	}
	if market.FormatPrice(1, 1234) != "$12.34" || market.FormatPrice(3, 5) != "€0.05" {
		t.Error("prices")
	}
}

func TestShopBuysOnlyWhatYouAgreedTo(t *testing.T) {
	ctl, _, appDir := newPanel(t)
	f := newFakeMarket(t)
	ctx := context.Background()

	shop, err := ctl.Shop(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if shop.Balance != 1000 || !shop.Allowed || shop.Cap != config.DefaultDailyCap || len(shop.Badges) != 1 || len(shop.Badges[0].Missing) != 2 || shop.Badges[0].Estimate != 28 {
		t.Fatalf("shop: %+v", shop)
	}
	q, err := ctl.Quote(ctx, 252490)
	if err != nil || q.Total != 28 || len(q.Items) != 2 {
		t.Fatalf("quote: %+v %v", q, err)
	}

	// The price went up since the quote: nothing is bought.
	f.mu.Lock()
	f.price["252490-Bye Zombies"] = 50
	f.mu.Unlock()
	if _, err := ctl.BuySet(ctx, 252490, q.Total); err == nil || !strings.Contains(err.Error(), "price went up") {
		t.Errorf("a higher price must be refused: %v", err)
	}
	f.mu.Lock()
	f.price["252490-Bye Zombies"] = 11
	f.mu.Unlock()

	// Over today's limit: nothing is bought.
	if err := ctl.SetMarketCap(20); err != nil {
		t.Fatal(err)
	}
	if _, err := ctl.BuySet(ctx, 252490, q.Total); err == nil || !strings.Contains(err.Error(), "limit") {
		t.Errorf("over the limit must be refused: %v", err)
	}
	// Not enough in the wallet: nothing is bought.
	ctl.SetMarketCap(500)
	f.mu.Lock()
	f.balance = 10
	f.mu.Unlock()
	if _, err := ctl.BuySet(ctx, 252490, q.Total); err == nil || !strings.Contains(err.Error(), "wallet") {
		t.Errorf("an empty wallet must be refused: %v", err)
	}
	f.mu.Lock()
	f.balance = 1000
	bought := len(f.bought)
	f.mu.Unlock()
	if bought != 0 {
		t.Fatalf("nothing should have been bought yet, got %d", bought)
	}

	res, err := ctl.BuySet(ctx, 252490, q.Total)
	if err != nil || len(res.Cards) != 2 || res.Spent != 28 || res.Error != "" {
		t.Fatalf("buy: %+v %v", res, err)
	}
	cfg, _ := config.Load(appDir)
	if spent := cfg.Market.SpentOn(time.Now().Format("2006-01-02")); spent != 28 {
		t.Errorf("spent today should be recorded: %d", spent)
	}
	// Bought out: the set is complete, so there's nothing left to buy.
	if _, err := ctl.Quote(ctx, 252490); err == nil {
		t.Error("a complete set has nothing to quote")
	}
	// Turning buying off refuses everything.
	ctl.SetMarketCap(0)
	if s, _ := ctl.Shop(ctx); s.Cap != 0 {
		t.Errorf("off: cap %d", s.Cap)
	}
}
