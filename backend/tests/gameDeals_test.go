package tests

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"houridle/backend/config"
	"houridle/backend/freegames"
	"houridle/backend/gamestore"
	"houridle/backend/web"
)

// fakeShop stands in for the Steam store and its checkout.
type fakeShop struct {
	mu        sync.Mutex
	cm        *fakeCM
	owned     map[uint32]bool
	tax       int // added to the total at checkout
	started   int
	cancelled int
	finished  int
	refuse    int // purchaseresultdetail to refuse finalizing with (0 = buy)
}

type shopGame struct {
	app      uint32
	price    int
	positive int
	reviews  int
	limited  bool
}

var shopGames = []shopGame{
	{app: 100, price: 49, positive: 90, reviews: 50},
	{app: 200, price: 59, positive: 50, reviews: 40},                // poorly reviewed
	{app: 300, price: 69, positive: 95, reviews: 900},               // already owned
	{app: 400, price: 79, positive: 88, reviews: 60, limited: true}, // doesn't drop cards
	{app: 500, price: 150, positive: 99, reviews: 1000},             // over the price limit
}

func shopGameByApp(app uint32) (shopGame, bool) {
	for _, g := range shopGames {
		if g.app == app {
			return g, true
		}
	}
	return shopGame{}, false
}

func newFakeShop(t *testing.T, cm *fakeCM) *fakeShop {
	f := &fakeShop{cm: cm, owned: map[uint32]bool{300: true}}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		defer f.mu.Unlock()
		p := r.URL.Path
		signedIn := false
		if c, err := r.Cookie("steamLoginSecure"); err == nil {
			v, _ := url.QueryUnescape(c.Value)
			signedIn = strings.HasPrefix(v, strconv.FormatUint(alice, 10)+"||")
		}
		switch {
		case p == "/search/results/":
			var rows strings.Builder
			for _, g := range shopGames {
				fmt.Fprintf(&rows, `<a href="x" data-ds-appid="%d" data-ds-itemkey="App_%d" class="search_result_row ds_collapse_flag"><span class="title">Game %d</span>`+
					`<span class="search_review_summary positive" data-tooltip-html="Positive&lt;br&gt;%d%% of the %d user reviews for this game are positive."></span>`+
					`<div class="search_price_discount_combined" data-price-final="%d"><div class="discount_block" data-discount="50"></div></div></a>`,
					g.app, g.app, g.app, g.positive, g.reviews, g.price)
			}
			json.NewEncoder(w).Encode(map[string]any{"results_html": rows.String(), "total_count": len(shopGames)})
		case p == "/api/appdetails":
			id, _ := strconv.Atoi(r.URL.Query().Get("appids"))
			g, ok := shopGameByApp(uint32(id))
			if !ok {
				fmt.Fprintf(w, `{"%d":{"success":false}}`, id)
				return
			}
			fmt.Fprintf(w, `{"%d":{"success":true,"data":{"name":"Game %d","price_overview":{"currency":"USD","final":%d},"package_groups":[{"name":"default","subs":[`+
				`{"packageid":%d,"option_text":"Game %d Soundtrack Edition - $9.99","price_in_cents_with_discount":999,"is_free_license":false},`+
				`{"packageid":%d,"option_text":"Game %d - <span class=\"discount_original_price\">$0.98</span> $%d.%02d","price_in_cents_with_discount":%d,"is_free_license":false}]}]}}}`,
				id, id, g.price, id*10+1, id, id*10, id, g.price/100, g.price%100, g.price)
		case strings.HasPrefix(p, "/app/"):
			id, _ := strconv.Atoi(strings.Trim(strings.TrimPrefix(p, "/app/"), "/"))
			g, _ := shopGameByApp(uint32(id))
			if g.limited {
				fmt.Fprint(w, `<div class="learning_about"><h2>Profile Features Limited</h2></div>`)
				return
			}
			fmt.Fprint(w, `<html>Steam Trading Cards</html>`)
		case p == "/dynamicstore/userdata/":
			var apps []uint32
			for a := range f.owned {
				apps = append(apps, a)
			}
			json.NewEncoder(w).Encode(map[string]any{"rgOwnedApps": apps})
		case strings.HasPrefix(p, "/checkout/"):
			if !signedIn {
				w.WriteHeader(http.StatusForbidden)
				return
			}
			r.ParseForm()
			switch p {
			case "/checkout/inittransaction/":
				sid, _ := r.Cookie("sessionid")
				if sid == nil || r.PostForm.Get("sessionid") != sid.Value || r.PostForm.Get("PaymentMethod") != "steamaccount" || r.PostForm.Get("bUseAccountCart") != "1" || r.PostForm.Get("bIsGift") != "0" {
					fmt.Fprint(w, `{"success":2,"purchaseresultdetail":7}`)
					return
				}
				f.started++
				fmt.Fprint(w, `{"success":1,"purchaseresultdetail":0,"transid":"777"}`)
			case "/checkout/getfinalprice/":
				fmt.Fprintf(w, `{"success":1,"base":"0","total":"%d","formattedTotal":"x"}`, f.cartTotal()+f.tax)
			case "/checkout/canceltransaction/":
				f.cancelled++
				fmt.Fprint(w, `{"success":1}`)
			case "/checkout/finalizetransaction/":
				if f.refuse != 0 {
					fmt.Fprintf(w, `{"success":2,"purchaseresultdetail":%d}`, f.refuse)
					return
				}
				f.finished++
				fmt.Fprint(w, `{"success":22,"purchaseresultdetail":0}`)
			case "/checkout/transactionstatus/":
				f.cm.mu.Lock()
				for _, pkg := range f.cm.cart {
					if pkg != 0 {
						f.owned[pkg/10] = true
					}
				}
				f.cm.cart = nil
				f.cm.mu.Unlock()
				fmt.Fprint(w, `{"success":1,"purchaseresultdetail":0}`)
			default:
				http.NotFound(w, r)
			}
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)
	oldS, oldC, oldF, oldP := gamestore.StoreURL, gamestore.CheckoutURL, freegames.StoreURL, gamestore.PollEvery
	gamestore.StoreURL, gamestore.CheckoutURL, freegames.StoreURL, gamestore.PollEvery = srv.URL, srv.URL, srv.URL, time.Millisecond
	t.Cleanup(func() {
		gamestore.StoreURL, gamestore.CheckoutURL, freegames.StoreURL, gamestore.PollEvery = oldS, oldC, oldF, oldP
	})
	return f
}

// cartTotal is the price of what's in the fake cart (f.mu held).
func (f *fakeShop) cartTotal() int {
	f.cm.mu.Lock()
	defer f.cm.mu.Unlock()
	total := 0
	for _, pkg := range f.cm.cart {
		if g, ok := shopGameByApp(pkg / 10); ok && pkg%10 == 0 {
			total += g.price
		}
	}
	return total
}

func (f *fakeShop) cartLen() int {
	f.cm.mu.Lock()
	defer f.cm.mu.Unlock()
	n := 0
	for _, pkg := range f.cm.cart {
		if pkg != 0 {
			n++
		}
	}
	return n
}

func TestGameDealsFindsCheapCardGames(t *testing.T) {
	ctl, cm, _ := newPanel(t)
	newFakeMarket(t)
	newFakeShop(t, cm)
	ctx := context.Background()

	deals, err := ctl.GameDeals(ctx, web.DealsQuery{MaxPrice: 100, GoodReviews: true})
	if err != nil {
		t.Fatal(err)
	}
	// 200 is poorly reviewed, 300 owned, 400 doesn't drop cards, 500 costs too much.
	if len(deals.Deals) != 1 || deals.Balance != 1000 || deals.Currency != 1 || deals.Cap != config.DefaultDailyCap {
		t.Fatalf("deals: %+v", deals)
	}
	d := deals.Deals[0]
	if d.AppID != 100 || d.Price != 49 || d.Currency != "USD" || d.Package != "Game 100" || d.Cards != 3 || d.Drops != 2 || d.CardPrice != 10 || d.XPCost != 59 || d.Review.Positive != 90 {
		t.Errorf("deal: %+v", d)
	}
	if d.XPPerDollar < 169 || d.XPPerDollar > 170 {
		t.Errorf("XP per dollar: %v", d.XPPerDollar)
	}

	all, err := ctl.GameDeals(ctx, web.DealsQuery{MaxPrice: 100, Sort: "price"})
	if err != nil || len(all.Deals) != 2 || all.Deals[0].AppID != 100 || all.Deals[1].AppID != 200 {
		t.Fatalf("with every review: %+v %v", all.Deals, err)
	}
}

func TestBuyGameOnlyBuysWhatYouAgreedTo(t *testing.T) {
	ctl, cm, appDir := newPanel(t)
	fm := newFakeMarket(t)
	shop := newFakeShop(t, cm)
	ctx := context.Background()
	if _, err := ctl.GameDeals(ctx, web.DealsQuery{MaxPrice: 100}); err != nil {
		t.Fatal(err)
	}
	refused := func(name string, agreed int, want string) {
		t.Helper()
		res, err := ctl.BuyGame(ctx, 100, agreed)
		if err == nil || res.Bought || !strings.Contains(err.Error(), want) {
			t.Errorf("%s: want a refusal about %q, got %+v %v", name, want, res, err)
		}
		shop.mu.Lock()
		finished := shop.finished
		shop.mu.Unlock()
		if finished != 0 {
			t.Fatalf("%s: a purchase went through", name)
		}
	}

	refused("price went up", 40, "price went up")

	ctl.SetMarketCap(30)
	refused("over the limit", 49, "limit")
	ctl.SetMarketCap(500)

	fm.mu.Lock()
	fm.balance = 20
	fm.mu.Unlock()
	refused("empty wallet", 49, "wallet")
	fm.mu.Lock()
	fm.balance = 1000
	fm.mu.Unlock()

	// Something already in the cart: nothing is touched.
	cm.mu.Lock()
	cm.cart = []uint32{9990}
	cm.mu.Unlock()
	refused("a full cart", 49, "cart")
	if shop.cartLen() != 1 {
		t.Fatal("houridle must leave your own cart alone")
	}
	cm.mu.Lock()
	cm.cart = nil
	cm.mu.Unlock()

	// Tax takes the total past what was agreed: the purchase is dropped.
	shop.mu.Lock()
	shop.tax = 5
	shop.mu.Unlock()
	refused("tax over the price", 49, "with tax")
	shop.mu.Lock()
	if shop.started != 1 || shop.cancelled != 1 {
		t.Errorf("the started purchase should be cancelled: started %d cancelled %d", shop.started, shop.cancelled)
	}
	shop.tax = 0
	shop.refuse = 2
	shop.mu.Unlock()
	if shop.cartLen() != 0 {
		t.Error("the game should be taken back out of the cart")
	}

	// Steam refuses: nothing is counted as spent.
	refused("Steam refuses", 49, "enough")
	shop.mu.Lock()
	shop.refuse = 0
	shop.mu.Unlock()
	cfg, _ := config.Load(appDir)
	if spent := cfg.Market.SpentOn(time.Now().Format("2006-01-02")); spent != 0 {
		t.Fatalf("nothing was bought, but %d was counted", spent)
	}

	res, err := ctl.BuyGame(ctx, 100, 49)
	if err != nil || !res.Bought || res.Spent != 49 || res.Currency != 1 || res.Name != "Game 100" {
		t.Fatalf("buy: %+v %v", res, err)
	}
	shop.mu.Lock()
	if shop.finished != 1 || !shop.owned[100] {
		t.Errorf("the game should be bought once: %d", shop.finished)
	}
	shop.mu.Unlock()
	cfg, _ = config.Load(appDir)
	if spent := cfg.Market.SpentOn(time.Now().Format("2006-01-02")); spent != 49 {
		t.Errorf("spent today should be recorded: %d", spent)
	}
	if _, err := ctl.BuyGame(ctx, 100, 49); err == nil || !strings.Contains(err.Error(), "already own") {
		t.Errorf("buying it again must be refused: %v", err)
	}
	ctl.SetMarketCap(0)
	if _, err := ctl.BuyGame(ctx, 200, 59); err == nil || !strings.Contains(err.Error(), "limit") {
		t.Errorf("buying off must refuse: %v", err)
	}
}
