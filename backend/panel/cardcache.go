package panel

import (
	"context"
	"strings"
	"time"

	"houridle/backend/market"
)

// cardsTTL is how long a game's Market card prices are kept: the Market
// limits how often it's asked.
const cardsTTL = 6 * time.Hour

type cachedCards struct {
	at   time.Time
	list []market.Card
}

// gameCards is a game's normal trading cards on the Market (names, Market
// names and lowest prices, US cents), kept for a few hours. A failed
// lookup isn't kept.
func (c *Controller) gameCards(ctx context.Context, appID uint32) ([]market.Card, error) {
	c.mu.Lock()
	hit, ok := c.cards[appID]
	c.mu.Unlock()
	if ok && time.Since(hit.at) < cardsTTL {
		return hit.list, nil
	}
	list, err := market.GameCards(ctx, appID)
	if err != nil {
		return nil, err
	}
	now := time.Now()
	c.mu.Lock()
	if c.cards == nil {
		c.cards = map[uint32]cachedCards{}
	}
	for id, e := range c.cards { // drop the stale ones while here
		if now.Sub(e.at) >= cardsTTL {
			delete(c.cards, id)
		}
	}
	c.cards[appID] = cachedCards{at: now, list: list}
	c.mu.Unlock()
	return list, nil
}

// byName indexes cards by lower-case card name and Market name.
func byName(list []market.Card) map[string]market.Card {
	out := make(map[string]market.Card, 2*len(list))
	for _, card := range list {
		out[strings.ToLower(card.Name)] = card
		out[strings.ToLower(card.HashName)] = card
	}
	return out
}
