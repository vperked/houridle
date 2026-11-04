package steamcm

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"houridle/backend/steamauth"
)

const callTimeout = 30 * time.Second

// deliver hands a service reply to whoever is waiting for it.
func (s *Session) deliver(m Message) {
	id := m.Header.Uint(11) // jobid_target
	s.mu.Lock()
	ch, ok := s.jobs[id]
	delete(s.jobs, id)
	if !ok && m.EMsg != emsgServiceMethodResp {
		ch = s.byEmsg[m.EMsg] // a reply that didn't name its job
		delete(s.byEmsg, m.EMsg)
	}
	s.mu.Unlock()
	if ch != nil {
		ch <- m // buffered: never blocks the reader
	}
}

// Call runs one of Steam's service methods (such as "Player.GetOwnedGames#1")
// on the signed-in session and returns the reply body.
func (s *Session) Call(ctx context.Context, method string, body []byte) (*steamauth.Decoded, error) {
	ch := make(chan Message, 1)
	s.mu.Lock()
	s.nextJob++
	id := s.nextJob
	s.jobs[id] = ch
	s.mu.Unlock()
	forget := func() {
		s.mu.Lock()
		delete(s.jobs, id)
		s.mu.Unlock()
	}

	sendCtx, cancel := context.WithTimeout(ctx, callTimeout)
	defer cancel()
	err := s.c.send(sendCtx, emsgServiceMethodCall, header{
		steamID: s.cfg.SteamID, sessionID: s.sessionID, jobSource: id, jobName: method,
	}, body)
	if err != nil {
		forget()
		s.finish(err)
		return nil, fmt.Errorf("%w: %v", steamauth.ErrNetwork, err)
	}

	timer := time.NewTimer(callTimeout)
	defer timer.Stop()
	select {
	case m := <-ch:
		if code := int32(m.Header.Int(13)); m.Header.Has(13) && code != steamauth.EResultOK {
			return nil, &steamauth.Error{EResult: int(code), Message: m.Header.Str(14)}
		}
		return steamauth.Decode(m.Body)
	case <-s.done:
		forget()
		return nil, fmt.Errorf("%w: connection closed", steamauth.ErrNetwork)
	case <-timer.C:
		forget()
		return nil, fmt.Errorf("%w: Steam didn't answer", steamauth.ErrNetwork)
	case <-ctx.Done():
		forget()
		return nil, ctx.Err()
	}
}

// OwnedGame is a game or app the account owns.
type OwnedGame struct {
	AppID   uint32
	Name    string
	Minutes int  // total playtime
	Shared  bool // not the account's: a Steam family member shares it
}

// OwnedGames asks Steam which games the signed-in account owns, most played
// first. This works however the profile's privacy is set.
func (s *Session) OwnedGames(ctx context.Context) ([]OwnedGame, error) {
	req := new(steamauth.Encoder).
		Varint(1, s.cfg.SteamID).
		Bool(2, true).  // include names and playtime
		Bool(3, true).  // include free-to-play games you've played
		Bool(5, true).  // ...and free games you've added but not played yet
		Bool(6, false). // include apps Steam hasn't vetted
		String(7, "english")
	resp, err := s.Call(ctx, "Player.GetOwnedGames#1", req.Out())
	if err != nil {
		return nil, err
	}
	var out []OwnedGame
	for _, g := range resp.Messages(2) {
		id := g.Int(1)
		if id <= 0 {
			continue
		}
		out = append(out, OwnedGame{
			AppID:   uint32(id),
			Name:    strings.TrimSpace(g.Str(2)),
			Minutes: int(int32(g.Int(4))),
		})
	}
	if len(out) == 0 && resp.Uint(1) > 0 {
		return nil, errors.New("Steam listed games but houridle couldn't read them")
	}
	slices.SortFunc(out, func(a, b OwnedGame) int { return b.Minutes - a.Minutes })
	return out, nil
}

// RenewRefreshToken asks Steam, over this signed-in connection, for a
// replacement for the refresh token. Steam's web service doesn't accept
// this kind of token, but its servers do. The result is empty when Steam
// decides the token doesn't need replacing yet.
func (s *Session) RenewRefreshToken(ctx context.Context) (string, error) {
	req := new(steamauth.Encoder).
		String(1, s.cfg.RefreshToken).
		Fixed64(2, s.cfg.SteamID).
		Varint(3, 1) // allow renewal
	resp, err := s.Call(ctx, "Authentication.GenerateAccessTokenForApp#1", req.Out())
	if err != nil {
		return "", err
	}
	return resp.Str(2), nil
}

// AccessToken asks Steam, over this signed-in connection, for a short-lived
// access token for the account. Steam's websites take it as a sign-in
// cookie, which is how houridle reads your badge pages (card drops).
// The token is a secret; it is never logged or printed.
func (s *Session) AccessToken(ctx context.Context) (string, error) {
	req := new(steamauth.Encoder).
		String(1, s.cfg.RefreshToken).
		Fixed64(2, s.cfg.SteamID)
	resp, err := s.Call(ctx, "Authentication.GenerateAccessTokenForApp#1", req.Out())
	if err != nil {
		return "", err
	}
	token := resp.Str(1)
	if token == "" {
		return "", errors.New("Steam sent no access token")
	}
	return token, nil
}

// AddFreeGames adds free-to-play games to the account, as the Steam
// client's "Play Game" button does for a free game. It returns the app IDs
// Steam granted (a game already owned isn't granted again).
func (s *Session) AddFreeGames(ctx context.Context, appIDs []uint32) ([]uint32, error) {
	req := new(steamauth.Encoder)
	for _, id := range appIDs {
		req.Varint(2, uint64(id))
	}
	m, err := s.request(ctx, emsgClientRequestFreeLicense, req.Out(), emsgClientRequestFreeLicenseResp)
	if err != nil {
		return nil, err
	}
	resp, err := steamauth.Decode(m.Body)
	if err != nil {
		return nil, err
	}
	// CMsgClientRequestFreeLicenseResponse: eresult is field 1 (default Fail).
	if code := int(int32(resp.Uint(1))); !resp.Has(1) || code != steamauth.EResultOK {
		if !resp.Has(1) {
			code = steamauth.EResultFail
		}
		return nil, &steamauth.Error{EResult: code, Message: "couldn't add the free games"}
	}
	var granted []uint32
	for _, id := range resp.Uints(3) {
		granted = append(granted, uint32(id))
	}
	return granted, nil
}

// ExploreQueue goes through one Steam discovery queue, the way the store
// page does when you click "Next" through it. During Steam's sale events,
// that's what earns the event's cards or stickers. It returns how many
// games the queue had.
func (s *Session) ExploreQueue(ctx context.Context) (int, error) {
	s.mu.Lock()
	country := s.country
	s.mu.Unlock()
	if country == "" {
		country = "US" // Steam needs one; it only shapes which games are shown
	}
	req := new(steamauth.Encoder).
		Varint(1, 0).       // queue type: the default discovery queue
		String(2, country). // required
		Bool(3, true).      // a fresh queue
		Bool(6, true).      // ...or the current one rebuilt if stale
		Bool(9, true)       // no experimental results
	resp, err := s.Call(ctx, "Store.GetDiscoveryQueue#1", req.Out())
	if err != nil {
		return 0, err
	}
	apps := resp.Uints(1)
	for _, id := range apps {
		skip := new(steamauth.Encoder).Varint(1, 0).Varint(2, id)
		if _, err := s.Call(ctx, "Store.SkipDiscoveryQueueItem#1", skip.Out()); err != nil {
			return 0, err
		}
	}
	return len(apps), nil
}

const eresultNoMatch = 42 // Steam's answer when no sale event is running

// ErrNoSticker means there's no sale-event sticker to claim right now
// (no event running, or today's is already claimed).
var ErrNoSticker = errors.New("no sale-event item to claim right now")

// ClaimSaleItem claims the free daily item (a sticker) Steam hands out
// during sale events. It returns ErrNoSticker when there's none.
func (s *Session) ClaimSaleItem(ctx context.Context) error {
	lang := new(steamauth.Encoder).String(1, "english").Out()
	can, err := s.Call(ctx, "SaleItemRewards.CanClaimItem#1", lang)
	var se *steamauth.Error
	if errors.As(err, &se) && (se.EResult == eresultNoMatch || se.EResult == steamauth.EResultFail) {
		return ErrNoSticker // no event running
	}
	if err != nil {
		return err
	}
	if !can.Bool(1) {
		return ErrNoSticker
	}
	_, err = s.Call(ctx, "SaleItemRewards.ClaimItem#1", lang)
	return err
}

// StoreApp is an app as the Steam store describes it.
type StoreApp struct {
	AppID uint32
	Name  string
	Game  bool // a game (not DLC, a demo, a soundtrack or software)
}

const storeItemsBatch = 100

// StoreApps looks apps up in the Steam store (names, and whether each is a
// game). Apps the store doesn't show are left out.
func (s *Session) StoreApps(ctx context.Context, appIDs []uint32) ([]StoreApp, error) {
	s.mu.Lock()
	country := s.country
	s.mu.Unlock()
	if country == "" {
		country = "US"
	}
	var out []StoreApp
	for start := 0; start < len(appIDs); start += storeItemsBatch {
		req := new(steamauth.Encoder)
		for _, id := range appIDs[start:min(start+storeItemsBatch, len(appIDs))] {
			req.Message(1, new(steamauth.Encoder).Varint(1, uint64(id))) // StoreItemID{appid}
		}
		req.Message(2, new(steamauth.Encoder).String(1, "english").String(3, country)) // context
		resp, err := s.Call(ctx, "StoreBrowse.GetItems#1", req.Out())
		if err != nil {
			return out, err
		}
		for _, it := range resp.Messages(1) { // StoreItem
			name := it.Str(6)
			if name == "" {
				continue // not shown in the store
			}
			// item_type 0 is an app; its app type (field 10) 0 is a game.
			out = append(out, StoreApp{AppID: uint32(it.Uint(2)), Name: name, Game: it.Uint(1) == 0 && it.Uint(10) == 0})
		}
	}
	return out, nil
}

// Country is the account's country as Steam sees it ("US" if Steam hasn't
// said). Store prices and checkout use it.
func (s *Session) Country() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.country == "" {
		return "US"
	}
	return s.country
}

// CartLine is one thing in the account's Steam store cart.
type CartLine struct {
	ID        uint64
	PackageID uint32
	BundleID  uint32
}

// Cart is what's in the account's Steam store cart (the same cart the
// store website and the Steam client show).
func (s *Session) Cart(ctx context.Context) ([]CartLine, error) {
	resp, err := s.Call(ctx, "AccountCart.GetCart#1", new(steamauth.Encoder).String(1, s.Country()).Out())
	if err != nil {
		return nil, err
	}
	var out []CartLine
	for _, cart := range resp.Messages(1) { // AccountCartContents
		for _, l := range cart.Messages(1) { // AccountCartLineItem
			out = append(out, CartLine{ID: l.Uint(1), PackageID: uint32(l.Uint(3)), BundleID: uint32(l.Uint(4))})
		}
	}
	return out, nil
}

// AddToCart puts one package (bought for yourself, not as a gift) in the
// account's store cart and returns its line in the cart.
func (s *Session) AddToCart(ctx context.Context, packageID uint32) (uint64, error) {
	item := new(steamauth.Encoder).Varint(1, uint64(packageID))
	req := new(steamauth.Encoder).String(1, s.Country()).Message(2, item)
	resp, err := s.Call(ctx, "AccountCart.AddItemsToCart#1", req.Out())
	if err != nil {
		return 0, err
	}
	lines := resp.Uints(1)
	if len(lines) == 0 {
		return 0, errors.New("Steam didn't add the game to the cart")
	}
	return lines[0], nil
}

// RemoveFromCart takes one line out of the account's store cart.
func (s *Session) RemoveFromCart(ctx context.Context, line uint64) error {
	req := new(steamauth.Encoder).Varint(1, line).String(2, s.Country())
	_, err := s.Call(ctx, "AccountCart.RemoveItemFromCart#1", req.Out())
	return err
}

// SharedGame is a game another member of the account's Steam family owns
// and shares with it.
type SharedGame struct {
	AppID  uint32
	Name   string
	Owners []uint64 // the family members who own it
}

// SharedGames lists the games the account's Steam family shares with it:
// the other members' games it can play, not counting ones excluded from
// sharing, or that aren't games. It's empty if the account isn't in a
// family.
func (s *Session) SharedGames(ctx context.Context) ([]SharedGame, error) {
	who := new(steamauth.Encoder).Varint(1, s.cfg.SteamID)
	resp, err := s.Call(ctx, "FamilyGroups.GetFamilyGroupForUser#1", who.Out())
	if err != nil {
		return nil, err
	}
	group := resp.Uint(1) // family_groupid
	if group == 0 || resp.Bool(2) {
		return nil, nil // not in a family
	}
	req := new(steamauth.Encoder).
		Fixed64(1, group).
		Bool(2, false). // not the account's own games
		Bool(3, false). // not the ones excluded from sharing
		String(5, "english").
		Bool(7, false). // games only
		Fixed64(8, s.cfg.SteamID)
	resp, err = s.Call(ctx, "FamilyGroups.GetSharedLibraryApps#1", req.Out())
	if err != nil {
		return nil, err
	}
	var out []SharedGame
	for _, a := range resp.Messages(1) {
		if a.Int(10) != 0 { // exclude_reason: can't be borrowed
			continue
		}
		g := SharedGame{AppID: uint32(a.Uint(1)), Name: strings.TrimSpace(a.Str(6))}
		for _, o := range a.Uints(2) {
			if o != s.cfg.SteamID {
				g.Owners = append(g.Owners, o)
			}
		}
		if g.AppID != 0 && len(g.Owners) > 0 {
			out = append(out, g)
		}
	}
	return out, nil
}
