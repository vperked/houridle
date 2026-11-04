package panel

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"houridle/backend/cards"
	"houridle/backend/config"
	"houridle/backend/idler"
	"houridle/backend/market"
	"houridle/backend/steamcm"
	"houridle/backend/web"
)

// shopGames is how many of the closest badges the shop prices (each costs
// a couple of Market lookups, and the Market limits how many it answers).
const shopGames = 6

// Shop ranks the badges you could finish by buying cards, by Steam XP per
// dollar, with your wallet and today's limit.
func (c *Controller) Shop(ctx context.Context) (web.Shop, error) {
	out := web.Shop{Badges: []web.ShopBadge{}}
	cfg, _ := c.config()
	out.Cap, out.SpentToday = cfg.Market.Cap(), cfg.Market.SpentOn(today())

	err := c.withToken(ctx, func(cs *steamcm.Session, id uint64, token string) error {
		mc, err := market.NewClient(id, token)
		if err != nil {
			return err
		}
		// The wallet and the badge pages don't depend on each other.
		var w market.Wallet
		var walletErr error
		done := make(chan struct{})
		go func() {
			defer close(done)
			w, walletErr = mc.Wallet(ctx)
		}()
		badges, err := cards.Badges(ctx, id, token)
		<-done
		if walletErr != nil {
			return walletErr
		}
		if err != nil {
			return err
		}
		out.Balance, out.Currency, out.Allowed, out.Reason = w.Balance, w.Currency, w.Allowed, w.Reason

		closest := cards.Closest(badges)
		closest = closest[:min(len(closest), shopGames)]
		priced := make([]*web.ShopBadge, len(closest))
		parallel(len(closest), lookups, func(i int) {
			b := closest[i]
			missing, est, err := c.missingCards(ctx, id, token, b.AppID)
			if err != nil || len(missing) == 0 {
				return // a lookup failed (or nothing's missing): skip this game
			}
			sb := &web.ShopBadge{AppID: b.AppID, Name: b.Name, Collected: b.Collected, Total: b.Total, Estimate: est}
			for _, m := range missing {
				sb.Missing = append(sb.Missing, m.Name)
			}
			if est > 0 {
				sb.XPPerUnit = 100 / (float64(est) / 100)
			}
			priced[i] = sb
		})
		for _, sb := range priced {
			if sb != nil {
				out.Badges = append(out.Badges, *sb)
			}
		}
		return nil
	})
	slices.SortStableFunc(out.Badges, func(a, b web.ShopBadge) int {
		switch {
		case a.XPPerUnit > b.XPPerUnit:
			return -1
		case a.XPPerUnit < b.XPPerUnit:
			return 1
		}
		return 0
	})
	return out, err
}

// missingCards is the cards of a game's set you don't have, matched to
// their Market names, and roughly what they cost together (US cents).
func (c *Controller) missingCards(ctx context.Context, steamID uint64, token string, appID uint32) ([]market.Card, int, error) {
	set, err := cards.SetCards(ctx, steamID, token, appID)
	if err != nil {
		return nil, 0, err
	}
	onMarket, err := c.gameCards(ctx, appID)
	if err != nil {
		return nil, 0, err
	}
	known := byName(onMarket)
	var missing []market.Card
	total := 0
	for _, card := range set {
		if card.Owned > 0 {
			continue
		}
		m, ok := known[strings.ToLower(card.Name)]
		if !ok {
			return nil, 0, fmt.Errorf("%s isn't on the Market", card.Name)
		}
		missing = append(missing, m)
		total += m.Price
	}
	return missing, total, nil
}

// Quote is the exact cost to finish a game's badge now: the cheapest
// listing of each missing card, in the wallet's currency.
func (c *Controller) Quote(ctx context.Context, appID uint32) (web.Quote, error) {
	q, _, err := c.quote(ctx, appID)
	return q, err
}

func (c *Controller) quote(ctx context.Context, appID uint32) (web.Quote, []market.Listing, error) {
	out := web.Quote{AppID: appID, Items: []web.QuoteItem{}}
	var listings []market.Listing
	err := c.withToken(ctx, func(cs *steamcm.Session, id uint64, token string) error {
		mc, err := market.NewClient(id, token)
		if err != nil {
			return err
		}
		missing, _, err := c.missingCards(ctx, id, token, appID)
		if err != nil {
			return err
		}
		if len(missing) == 0 {
			return errors.New("you already have every card of this set")
		}
		for _, m := range missing {
			l, err := mc.Cheapest(ctx, m.HashName)
			if err != nil {
				return err
			}
			if out.Currency != 0 && l.Currency != out.Currency {
				return errors.New("the Market listed these cards in different currencies; try again")
			}
			out.Currency = l.Currency
			out.Total += l.Total()
			out.Items = append(out.Items, web.QuoteItem{Card: m.Name, Price: l.Total(), Market: market.ListingURL(m.HashName)})
			listings = append(listings, l)
		}
		return nil
	})
	out.Name = c.knownName(appID) // the page falls back to the badge's name
	return out, listings, err
}

// BuySet buys the missing cards of a game's set, if the cost now is no more
// than the total you agreed to, fits today's limit and your wallet. It's
// only ever called when you press Buy on the page.
func (c *Controller) BuySet(ctx context.Context, appID uint32, agreed int) (web.Bought, error) {
	c.buying.Lock() // one purchase at a time: a double tap can't buy twice
	defer c.buying.Unlock()
	out := web.Bought{Cards: []string{}}

	q, listings, err := c.quote(ctx, appID)
	if err != nil {
		return out, err
	}
	if q.Total > agreed {
		return out, fmt.Errorf("the price went up to %s (you agreed to %s); get a new quote", market.FormatPrice(q.Currency, q.Total), market.FormatPrice(q.Currency, agreed))
	}
	lim, err := c.todaysLimit()
	if err != nil {
		return out, err
	}
	if err := lim.check(q.Total, q.Currency); err != nil {
		return out, err
	}
	day := lim.day

	err = c.withToken(ctx, func(cs *steamcm.Session, id uint64, token string) error {
		mc, err := market.NewClient(id, token)
		if err != nil {
			return err
		}
		w, err := mc.Wallet(ctx)
		if err != nil {
			return err
		}
		if !w.Allowed {
			return errors.New("the Market isn't open to this account: " + w.Reason)
		}
		if w.Currency != q.Currency {
			return errors.New("the listings aren't in your wallet's currency; try again")
		}
		if w.Balance < q.Total {
			return fmt.Errorf("your wallet has %s; this costs %s. Add funds on Steam first", w.Price(w.Balance), w.Price(q.Total))
		}
		for i, l := range listings {
			err := mc.Buy(ctx, l)
			if errors.Is(err, market.ErrUnclear) && c.nowOwned(ctx, id, token, appID, q.Items[i].Card) {
				err = nil // it went through after all
			}
			if err != nil {
				return fmt.Errorf("stopped at %s: %w", q.Items[i].Card, err)
			}
			out.Cards = append(out.Cards, q.Items[i].Card)
			out.Spent += l.Total()
			c.recordSpend(day, l.Total())
		}
		return nil
	})
	if len(out.Cards) > 0 {
		text := fmt.Sprintf("Bought %d card(s) for %s from the page, for %s", len(out.Cards), q.Name, market.FormatPrice(q.Currency, out.Spent))
		if len(out.Cards) == len(listings) {
			text += ": the set is complete"
		}
		c.emit(idler.Event{At: time.Now(), Level: idler.EventGood, Text: text, Alert: true})
	}
	if err != nil {
		out.Error = err.Error()
		if len(out.Cards) == 0 {
			return out, err
		}
	}
	return out, nil
}

// nowOwned checks whether a card arrived (after an unclear purchase answer).
func (c *Controller) nowOwned(ctx context.Context, steamID uint64, token string, appID uint32, card string) bool {
	set, err := cards.SetCards(ctx, steamID, token, appID)
	if err != nil {
		return false
	}
	for _, s := range set {
		if strings.EqualFold(s.Name, card) {
			return s.Owned > 0
		}
	}
	return false
}

// spendLimit is what's been spent today and the daily limit, which covers
// everything houridle buys (cards and games together).
type spendLimit struct {
	day          string
	spent, limit int // cents
}

func (c *Controller) todaysLimit() (spendLimit, error) {
	cfg, err := c.config()
	if err != nil {
		return spendLimit{}, err
	}
	day := today()
	return spendLimit{day: day, spent: cfg.Market.SpentOn(day), limit: cfg.Market.Cap()}, nil
}

// check refuses spending cents more today if that goes over the limit.
func (l spendLimit) check(cents, currency int) error {
	if l.spent+cents <= l.limit {
		return nil
	}
	return fmt.Errorf("that's over today's limit: %s spent of %s. Raise the limit to buy it", market.FormatPrice(currency, l.spent), market.FormatPrice(currency, l.limit))
}

func (c *Controller) recordSpend(day string, cents int) {
	err := c.updateConfig(func(cfg *config.Config) {
		if cfg.Market == nil {
			cfg.Market = &config.MarketConfig{}
		}
		if cfg.Market.Spent == nil {
			cfg.Market.Spent = map[string]int{}
		}
		cfg.Market.Spent[day] += cents
		for d := range cfg.Market.Spent { // keep a month
			if t, err := time.Parse("2006-01-02", d); err == nil && time.Since(t) > 31*24*time.Hour {
				delete(cfg.Market.Spent, d)
			}
		}
	})
	if err != nil {
		c.logf("warn", "couldn't save what was spent: "+err.Error())
	}
}

// SetMarketCap sets the daily limit for buying cards (cents).
func (c *Controller) SetMarketCap(cents int) error {
	return c.updateConfig(func(cfg *config.Config) {
		if cfg.Market == nil {
			cfg.Market = &config.MarketConfig{}
		}
		cfg.Market.DailyCap = cents
		if cents == 0 {
			cfg.Market.DailyCap = -1 // 0 means "no buying"; stored as -1 so it isn't the default
		}
	})
}

func today() string { return time.Now().Format("2006-01-02") }
