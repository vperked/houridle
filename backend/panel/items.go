package panel

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"sync"
	"time"

	"houridle/backend/cards"
	"houridle/backend/idler"
	"houridle/backend/inventory"
	"houridle/backend/steamcm"
	"houridle/backend/web"
)

const (
	levelTTL    = 20 * time.Minute
	pricedGames = 25 // games whose cards the inventory prices (the Market limits lookups)
)

type levelCache struct {
	at  time.Time
	val web.Level
	src string
}

// Level is the account's Steam level, XP, and the badges in reach. It's
// kept for a while: reading it means reading every badge page.
func (c *Controller) Level(ctx context.Context) (web.Level, error) {
	c.mu.Lock()
	cached, src := c.level, c.src.id()
	c.mu.Unlock()
	if cached.src == src && time.Since(cached.at) < levelTTL {
		return cached.val, nil
	}
	var out web.Level
	err := c.withToken(ctx, func(cs *steamcm.Session, id uint64, token string) error {
		badges, drops, lvl, err := cards.Overview(ctx, id, token)
		if err != nil {
			return err
		}
		out = web.Level{Level: lvl.Level, XP: lvl.XP, LevelXP: lvl.LevelXP, NextXP: lvl.NextXP, Badges: len(badges)}
		for _, b := range badges {
			switch {
			case b.Ready:
				out.Ready++
			case b.Collected > 0:
				out.Partial++
				out.Missing += b.Missing()
			}
		}
		out.DropsXP = 100 * len(drops)
		return nil
	})
	if err == nil {
		c.mu.Lock()
		c.level = levelCache{at: time.Now(), val: out, src: src}
		c.mu.Unlock()
	}
	return out, err
}

// forgetLevel makes the next Level read the badge pages again (after
// crafting, say).
func (c *Controller) forgetLevel() {
	c.mu.Lock()
	c.level = levelCache{}
	c.mu.Unlock()
}

// gemValue is what one of a game's cards turns into, in gems (fixed per
// game, so it's remembered).
func (c *Controller) gemValue(ctx context.Context, ic *inventory.Client, appID uint32, assetID string) int {
	c.mu.Lock()
	v, ok := c.gems[appID]
	c.mu.Unlock()
	if ok {
		return v
	}
	v, err := ic.GemValue(ctx, appID, assetID)
	if err != nil {
		return 0
	}
	c.mu.Lock()
	if c.gems == nil {
		c.gems = map[uint32]int{}
	}
	c.gems[appID] = v
	c.mu.Unlock()
	return v
}

// lookups is how many Steam lookups (prices, gem values...) run at once.
const lookups = 4

// parallel runs fn(0..n-1), at most workers at a time, and waits.
func parallel(n, workers int, fn func(i int)) {
	var wg sync.WaitGroup
	sem := make(chan struct{}, workers)
	for i := range n {
		wg.Add(1)
		sem <- struct{}{}
		go func() {
			defer wg.Done()
			defer func() { <-sem }()
			fn(i)
		}()
	}
	wg.Wait()
}

// cardStack is one card (by Market name) and the inventory's copies of it.
type cardStack struct {
	name   string
	foil   bool
	assets []string // one per copy
}

type gameCards struct {
	appID  uint32
	name   string
	stacks map[string]*cardStack
	count  int
}

func groupCards(items []inventory.Item) map[uint32]*gameCards {
	games := map[uint32]*gameCards{}
	for _, it := range items {
		if !it.Card || it.AppID == 0 {
			continue
		}
		g := games[it.AppID]
		if g == nil {
			g = &gameCards{appID: it.AppID, name: it.Game, stacks: map[string]*cardStack{}}
			games[it.AppID] = g
		}
		st := g.stacks[it.HashName]
		if st == nil {
			st = &cardStack{name: it.Name, foil: it.Foil}
			g.stacks[it.HashName] = st
		}
		for range it.Amount {
			st.assets = append(st.assets, it.AssetID)
		}
		g.count += it.Amount
	}
	return games
}

// spare is a game's extra copies: every copy of a normal card past the
// first (one of each is kept, for crafting).
func (g *gameCards) spare() []string {
	var out []string
	for _, st := range g.stacks {
		if !st.foil && len(st.assets) > 1 {
			out = append(out, st.assets[1:]...)
		}
	}
	slices.Sort(out)
	return out
}

func (c *Controller) inventoryClient(ctx context.Context, cs *steamcm.Session, id uint64) (*inventory.Client, error) {
	token, err := cs.AccessToken(ctx)
	if err != nil {
		return nil, err
	}
	return inventory.NewClient(id, token)
}

// Inventory is the trading cards and gems in the inventory, with what the
// cards would sell for and what the spare copies would turn into.
func (c *Controller) Inventory(ctx context.Context) (web.Inventory, error) {
	out := web.Inventory{Games: []web.CardGroup{}, Priced: true}
	err := c.withConn(ctx, func(cs *steamcm.Session, id uint64) error {
		ic, err := c.inventoryClient(ctx, cs, id)
		if err != nil {
			return err
		}
		items, err := ic.Items(ctx)
		if err != nil {
			return err
		}
		out.Gems = inventory.Gems(items)
		for _, it := range items {
			if it.Booster {
				out.Boosters += it.Amount
			}
		}
		games := groupCards(items)
		list := make([]*gameCards, 0, len(games))
		for _, g := range games {
			list = append(list, g)
		}
		slices.SortFunc(list, func(a, b *gameCards) int { return b.count - a.count })
		groups := make([]web.CardGroup, len(list))
		priced := make([]bool, len(list))
		// The games with the most cards are priced, a few lookups at a time.
		parallel(min(len(list), pricedGames), lookups, func(i int) {
			g := list[i]
			if found, err := c.gameCards(ctx, g.appID); err == nil {
				prices := byName(found)
				for hash, st := range g.stacks {
					if !st.foil {
						card, ok := prices[strings.ToLower(hash)]
						if !ok {
							card = prices[strings.ToLower(st.name)]
						}
						groups[i].Value += card.Price * len(st.assets)
					}
				}
				priced[i] = true
			}
			if spare := g.spare(); len(spare) > 0 {
				groups[i].Gems = c.gemValue(ctx, ic, g.appID, spare[0])
			}
		})
		for i, g := range list {
			cg := &groups[i]
			cg.AppID, cg.Name, cg.Cards, cg.Kinds, cg.Market = g.appID, g.name, g.count, len(g.stacks), cards.MarketSearch(g.appID)
			for _, st := range g.stacks {
				if st.foil {
					cg.Foils += len(st.assets)
				}
			}
			cg.Spare = len(g.spare())
			out.Priced = out.Priced && priced[i]
			out.Cards += cg.Cards
			out.Spare += cg.Spare
			out.Value += cg.Value
		}
		out.Games = groups
		return nil
	})
	slices.SortStableFunc(out.Games, func(a, b web.CardGroup) int { return b.Value - a.Value })
	return out, err
}

// GrindDuplicates turns the spare copies of the chosen games' cards into
// gems, keeping one of each card. It goes ahead only if that still comes to
// the gems you were shown (otherwise your cards changed: look again).
func (c *Controller) GrindDuplicates(ctx context.Context, apps []uint32, expected int) (web.Ground, error) {
	c.buying.Lock() // one change to the inventory at a time
	defer c.buying.Unlock()
	var out web.Ground
	err := c.withConn(ctx, func(cs *steamcm.Session, id uint64) error {
		ic, err := c.inventoryClient(ctx, cs, id)
		if err != nil {
			return err
		}
		items, err := ic.Items(ctx)
		if err != nil {
			return err
		}
		games := groupCards(items)
		type job struct {
			app    uint32
			assets []string
			each   int
		}
		var jobs []job
		total := 0
		for _, app := range apps {
			g := games[app]
			if g == nil {
				continue
			}
			spare := g.spare()
			if len(spare) == 0 {
				continue
			}
			each := c.gemValue(ctx, ic, app, spare[0])
			if each == 0 {
				return fmt.Errorf("couldn't find out what %s's cards are worth in gems", g.name)
			}
			jobs = append(jobs, job{app, spare, each})
			total += each * len(spare)
		}
		if total == 0 {
			return errors.New("no spare cards to turn into gems")
		}
		if total != expected {
			return fmt.Errorf("that now comes to %d gems, not %d: your cards changed, so look again", total, expected)
		}
		for _, j := range jobs {
			for _, asset := range j.assets {
				got, err := ic.Grind(ctx, j.app, asset, j.each)
				if err != nil {
					return fmt.Errorf("stopped after %d card(s): %w", out.Cards, err)
				}
				out.Cards++
				out.Gems += got
			}
		}
		return nil
	})
	if out.Cards > 0 {
		c.emit(idler.Event{At: time.Now(), Level: idler.EventGood, Text: fmt.Sprintf("Turned %d spare card(s) into %d gems from the page", out.Cards, out.Gems)})
	}
	if err != nil {
		out.Error = err.Error()
		if out.Cards == 0 {
			return out, err
		}
	}
	return out, nil
}

// Boosters is the gems on hand and the booster packs they can make.
func (c *Controller) Boosters(ctx context.Context) (web.BoosterList, error) {
	out := web.BoosterList{Boosters: []web.BoosterInfo{}}
	err := c.withConn(ctx, func(cs *steamcm.Session, id uint64) error {
		ic, err := c.inventoryClient(ctx, cs, id)
		if err != nil {
			return err
		}
		list, gems, err := ic.Boosters(ctx)
		if err != nil {
			return err
		}
		out.Gems = gems
		for _, b := range list {
			out.Boosters = append(out.Boosters, web.BoosterInfo{AppID: b.AppID, Name: b.Name, Price: b.Price, Available: b.Available, AvailableAt: b.AvailableAt})
		}
		return nil
	})
	slices.SortStableFunc(out.Boosters, func(a, b web.BoosterInfo) int {
		if a.Available != b.Available {
			if a.Available {
				return -1
			}
			return 1
		}
		return a.Price - b.Price
	})
	return out, err
}

// MakeBooster turns gems into a game's booster pack, if it still costs no
// more than the gems you were shown.
func (c *Controller) MakeBooster(ctx context.Context, appID uint32, price int) error {
	c.buying.Lock()
	defer c.buying.Unlock()
	name := ""
	err := c.withConn(ctx, func(cs *steamcm.Session, id uint64) error {
		ic, err := c.inventoryClient(ctx, cs, id)
		if err != nil {
			return err
		}
		list, gems, err := ic.Boosters(ctx)
		if err != nil {
			return err
		}
		i := slices.IndexFunc(list, func(b inventory.Booster) bool { return b.AppID == appID })
		if i < 0 {
			return errors.New("that game's booster pack can't be made")
		}
		b := list[i]
		name = b.Name
		switch {
		case !b.Available:
			return fmt.Errorf("one was made for %s recently; the next can be made %s", b.Name, b.AvailableAt)
		case b.Price > price:
			return fmt.Errorf("it now costs %d gems, not %d; look again", b.Price, price)
		}
		if gems < b.Price {
			return fmt.Errorf("you have %d gems; it takes %d", gems, b.Price)
		}
		return ic.MakeBooster(ctx, appID, b.Series)
	})
	if err == nil {
		c.emit(idler.Event{At: time.Now(), Level: idler.EventGood, Text: fmt.Sprintf("Made a %s booster pack for %d gems from the page", name, price)})
	}
	return err
}
