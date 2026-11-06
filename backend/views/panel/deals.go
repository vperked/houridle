package panel

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"time"

	"houridle/backend/features/freegames"
	"houridle/backend/features/gamestore"
	"houridle/backend/features/market"
	"houridle/backend/online/steamcm"
	"houridle/backend/run/idler"
	"houridle/backend/views/web"
)

const (
	defaultDeals    = 8
	maxDeals        = 20
	defaultMaxPrice = 100 // $1
	goodReviews     = 70  // percent positive, with at least minReviews
	minReviews      = 10
	sellerShare     = 0.87 // what a seller keeps of a Market price, after fees
)

// GameDeals lists cheap games with trading cards you don't own, priced
// for card drops and Steam XP, with your wallet and today's limit.
func (c *Controller) GameDeals(ctx context.Context, q web.DealsQuery) (web.Deals, error) {
	out := web.Deals{Deals: []web.Deal{}}
	cfg, _ := c.config()
	out.Cap, out.SpentToday = cfg.Market.Cap(), cfg.Market.SpentOn(today())
	if q.Count <= 0 {
		q.Count = defaultDeals
	}
	q.Count = min(q.Count, maxDeals)
	if q.MaxPrice <= 0 {
		q.MaxPrice = defaultMaxPrice
	}

	var country string
	var owned map[uint32]bool
	err := c.withConn(ctx, func(cs *steamcm.Session, id uint64) error {
		country = cs.Country()
		token, err := cs.AccessToken(ctx)
		if err != nil {
			return err
		}
		if mc, err := market.NewClient(id, token); err == nil {
			if w, err := mc.Wallet(ctx); err == nil {
				out.Balance, out.Currency = w.Balance, w.Currency
			}
		}
		apps, err := freegames.OwnedApps(ctx, id, token)
		if err != nil {
			return fmt.Errorf("couldn't read which games you own: %w", err)
		}
		owned = make(map[uint32]bool, len(apps))
		for _, a := range apps {
			owned[a] = true
		}
		return nil
	})
	if err != nil {
		return out, err
	}

	// More candidates than asked for: some turn out to have no cards on the
	// Market yet, or not to drop cards.
	found, err := gamestore.Search(ctx, country, q.MaxPrice, q.Count*2+4, func(g gamestore.Candidate) bool {
		return owned[g.AppID] || q.GoodReviews && (g.Positive < goodReviews || g.Reviews < minReviews)
	})
	if err != nil && len(found) == 0 {
		return out, err
	}

	deals := make([]*web.Deal, len(found))
	parallel(len(found), lookups, func(i int) { deals[i] = c.priceDeal(ctx, found[i], country) })
	for _, d := range deals {
		if d != nil {
			out.Deals = append(out.Deals, *d)
		}
	}
	sortDeals(out.Deals, q.Sort)
	if len(out.Deals) > q.Count {
		out.Deals = out.Deals[:q.Count]
	}
	c.mu.Lock()
	for _, d := range out.Deals {
		c.names[d.AppID] = d.Name
	}
	c.mu.Unlock()
	return out, nil
}

// priceDeal values one candidate: its package, whether it drops cards, and
// what its cards are worth. nil if it isn't a good buy for cards.
func (c *Controller) priceDeal(ctx context.Context, g gamestore.Candidate, country string) *web.Deal {
	if limited, err := gamestore.ProfileLimited(ctx, g.AppID); err != nil || limited {
		return nil // no card drops (or the store couldn't say)
	}
	pkg, err := gamestore.GamePackage(ctx, g.AppID, country)
	if err != nil {
		return nil
	}
	list, err := c.gameCards(ctx, g.AppID)
	if err != nil || len(list) == 0 {
		return nil // no cards on the Market yet (or it couldn't say)
	}
	avg := 0
	for _, card := range list {
		avg += card.Price
	}
	avg /= len(list)
	d := &web.Deal{
		AppID: g.AppID, Name: g.Name, Package: pkg.Name, Price: pkg.Price, Currency: pkg.Currency, Discount: g.Discount,
		Review: web.Review{Positive: g.Positive, Count: g.Reviews},
		Cards:  len(list), Drops: (len(list) + 1) / 2, CardPrice: avg,
	}
	d.DropValue = int(float64(d.Drops*avg) * sellerShare)
	d.XPCost = d.Price + (d.Cards-d.Drops)*avg // the game, plus the cards that don't drop
	if d.XPCost > 0 {
		d.XPPerDollar = 100 / (float64(d.XPCost) / 100)
	}
	return d
}

func sortDeals(deals []web.Deal, by string) {
	slices.SortStableFunc(deals, func(a, b web.Deal) int {
		switch by {
		case "price":
			return a.Price - b.Price
		case "reviews":
			if a.Review.Positive != b.Review.Positive {
				return b.Review.Positive - a.Review.Positive
			}
			return b.Review.Count - a.Review.Count
		case "payback": // drops cover the most of the price
			return (a.Price - a.DropValue) - (b.Price - b.DropValue)
		}
		switch { // "xp", the default
		case a.XPPerDollar > b.XPPerDollar:
			return -1
		case a.XPPerDollar < b.XPPerDollar:
			return 1
		}
		return a.Price - b.Price
	})
}

// currencies maps the store's currency codes to Steam's wallet currencies.
var currencies = map[string]int{
	"USD": 1, "GBP": 2, "EUR": 3, "CHF": 4, "RUB": 5, "PLN": 6, "BRL": 7, "JPY": 8, "NOK": 9, "IDR": 10,
	"MYR": 11, "PHP": 12, "SGD": 13, "THB": 14, "VND": 15, "KRW": 16, "UAH": 18, "MXN": 19, "CAD": 20,
	"AUD": 21, "NZD": 22, "CNY": 23, "INR": 24, "CLP": 25, "PEN": 26, "COP": 27, "ZAR": 28, "HKD": 29,
	"TWD": 30, "SAR": 31, "AED": 32, "ILS": 35, "KZT": 37, "KWD": 38, "QAR": 39, "CRC": 40, "UYU": 41,
}

// BuyGame buys one game with the Steam wallet, if Steam's final total is no
// more than the price you agreed to and it fits today's limit and your
// wallet. It's only ever called when you press Buy on the page.
//
// To be sure nothing else is bought, it refuses when your Steam cart
// already has something in it, and it stops (before any money moves) if
// the total Steam asks for isn't what you agreed to.
func (c *Controller) BuyGame(ctx context.Context, appID uint32, agreed int) (web.GameBought, error) {
	c.buying.Lock() // one purchase at a time: a double tap can't buy twice
	defer c.buying.Unlock()
	out := web.GameBought{AppID: appID, Name: c.gameName(appID)}

	lim, err := c.todaysLimit()
	if err != nil {
		return out, err
	}
	day := lim.day

	err = c.withConn(ctx, func(cs *steamcm.Session, id uint64) error {
		country := cs.Country()
		token, err := cs.AccessToken(ctx)
		if err != nil {
			return err
		}
		mc, err := market.NewClient(id, token)
		if err != nil {
			return err
		}
		w, err := mc.Wallet(ctx)
		if err != nil {
			return err
		}
		out.Currency = w.Currency
		pkg, err := gamestore.GamePackage(ctx, appID, country)
		if err != nil {
			return err
		}
		if cur, ok := currencies[pkg.Currency]; !ok || cur != w.Currency {
			return fmt.Errorf("the store's price is in %s, not your wallet's currency", pkg.Currency)
		}
		if pkg.Price > agreed {
			return fmt.Errorf("the price went up to %s (you agreed to %s); look again", w.Price(pkg.Price), w.Price(agreed))
		}
		if err := lim.check(pkg.Price, w.Currency); err != nil {
			return err
		}
		if w.Balance < pkg.Price {
			return fmt.Errorf("your wallet has %s; this costs %s. Add funds on Steam first", w.Price(w.Balance), w.Price(pkg.Price))
		}
		if apps, err := freegames.OwnedApps(ctx, id, token); err == nil && slices.Contains(apps, appID) {
			return errors.New("you already own that game")
		}

		cart, err := cs.Cart(ctx)
		if err != nil {
			return fmt.Errorf("couldn't read your Steam cart: %w", err)
		}
		if len(cart) > 0 {
			return fmt.Errorf("your Steam cart has %d thing(s) in it. houridle only buys with an empty cart, so it can't buy something else by mistake: empty it on Steam first", len(cart))
		}
		line, err := cs.AddToCart(ctx, pkg.ID)
		if err != nil {
			return fmt.Errorf("couldn't add it to your cart: %w", err)
		}
		bought := false
		defer func() {
			if !bought {
				cs.RemoveFromCart(context.WithoutCancel(ctx), line) // leave the cart as it was
			}
		}()
		if cart, err := cs.Cart(ctx); err != nil || len(cart) != 1 || cart[0].PackageID != pkg.ID {
			return errors.New("your Steam cart didn't hold just this game; nothing was bought")
		}

		co, err := gamestore.NewCheckout(id, token, country)
		if err != nil {
			return err
		}
		trans, err := co.Start(ctx)
		if err != nil {
			return err
		}
		total, err := co.Total(ctx, trans)
		if err == nil {
			switch {
			case total > agreed:
				err = fmt.Errorf("Steam's total is %s with tax (you agreed to %s); nothing was bought", w.Price(total), w.Price(agreed))
			case total > w.Balance:
				err = fmt.Errorf("Steam's total is %s with tax and your wallet has %s; nothing was bought", w.Price(total), w.Price(w.Balance))
			default:
				err = lim.check(total, w.Currency)
			}
		}
		if err != nil {
			co.Cancel(context.WithoutCancel(ctx), trans)
			return err
		}

		// The one step that spends money.
		err = co.Finish(ctx, trans)
		if err == nil || errors.Is(err, gamestore.ErrUnclear) {
			// Counted against the limit even when unclear, to be safe.
			c.recordSpend(day, total)
			out.Spent = total
		}
		if errors.Is(err, gamestore.ErrUnclear) && c.ownsNow(ctx, id, token, appID) {
			err = nil
		}
		if err != nil {
			return err
		}
		bought = true // Steam emptied the cart
		return nil
	})
	if err != nil {
		out.Error = err.Error()
		return out, err
	}
	out.Bought = true
	c.emit(idler.Event{At: time.Now(), Level: idler.EventGood, Alert: true,
		Text: fmt.Sprintf("Bought %s from the page for %s", out.Name, market.FormatPrice(out.Currency, out.Spent))})
	return out, nil
}

// ownsNow checks the store (a few times) for a game after an unclear answer.
func (c *Controller) ownsNow(ctx context.Context, steamID uint64, token string, appID uint32) bool {
	for range 3 {
		if apps, err := freegames.OwnedApps(ctx, steamID, token); err == nil && slices.Contains(apps, appID) {
			return true
		}
		select {
		case <-ctx.Done():
			return false
		case <-time.After(3 * time.Second):
		}
	}
	return false
}
