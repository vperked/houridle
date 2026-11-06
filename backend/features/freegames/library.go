package freegames

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"houridle/backend/online/steamcm"
	"houridle/backend/online/steamweb"
)

// OwnedApps lists every app the account owns (games, DLC, free games it
// has never played), from the Steam store's own data about the account.
// accessToken is a secret: it is only sent to the store, as the sign-in
// cookie.
func OwnedApps(ctx context.Context, steamID uint64, accessToken string) ([]uint32, error) {
	wc, err := steamweb.New(steamID, accessToken, fetchTimeout, StoreURL)
	if err != nil {
		return nil, err
	}
	client := wc.HTTP
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, StoreURL+"/dynamicstore/userdata/", nil)
	if err != nil {
		return nil, err
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, shortErr(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("the Steam store answered HTTP %d", resp.StatusCode)
	}
	var data struct {
		Apps []uint32 `json:"rgOwnedApps"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, maxBody)).Decode(&data); err != nil {
		return nil, fmt.Errorf("couldn't read the store's list of your games")
	}
	return data.Apps, nil
}

const libraryTimeout = time.Minute

// Library is every game the account can play: its own (see ownedLibrary)
// and the ones its Steam family shares with it, marked Shared.
func Library(ctx context.Context, cs *steamcm.Session, steamID uint64) ([]steamcm.OwnedGame, error) {
	owned, err := ownedLibrary(ctx, cs, steamID)
	if err != nil {
		return nil, err
	}
	shared, err := cs.SharedGames(ctx)
	if err != nil || len(shared) == 0 {
		return owned, nil // no family (or it couldn't say): the account's own games
	}
	have := make(map[uint32]bool, len(owned))
	for _, g := range owned {
		have[g.AppID] = true
	}
	for _, g := range shared {
		if !have[g.AppID] {
			owned = append(owned, steamcm.OwnedGame{AppID: g.AppID, Name: g.Name, Shared: true})
		}
	}
	return owned, nil
}

// ownedLibrary is every game the account owns: Steam's owned-games list (with
// playtime), plus the free games added but never played, which that list
// leaves out. If the store can't be asked, the owned-games list alone is
// returned.
func ownedLibrary(ctx context.Context, cs *steamcm.Session, steamID uint64) ([]steamcm.OwnedGame, error) {
	// Steam's list and the store's don't depend on each other.
	storeCtx, cancel := context.WithTimeout(ctx, libraryTimeout)
	defer cancel()
	var all []uint32
	var storeErr error
	done := make(chan struct{})
	go func() {
		defer close(done)
		token, err := cs.AccessToken(storeCtx)
		if err == nil {
			all, err = OwnedApps(storeCtx, steamID, token)
		}
		storeErr = err
	}()
	owned, err := cs.OwnedGames(ctx)
	<-done
	if err != nil {
		return nil, err
	}
	if storeErr != nil {
		return owned, nil // the owned-games list alone
	}
	ctx = storeCtx
	have := make(map[uint32]bool, len(owned))
	for _, g := range owned {
		have[g.AppID] = true
	}
	var extra []uint32
	for _, id := range all {
		if !have[id] {
			extra = append(extra, id)
		}
	}
	if len(extra) == 0 {
		return owned, nil
	}
	apps, err := cs.StoreApps(ctx, extra)
	if err != nil {
		return owned, nil
	}
	for _, a := range apps {
		if a.Game {
			owned = append(owned, steamcm.OwnedGame{AppID: a.AppID, Name: a.Name})
		}
	}
	return owned, nil
}
