//go:build windows || linux

package main

import (
	"context"
	"errors"
	"fmt"
	"time"

	"houridle/backend/handover"
	"houridle/backend/steam"
	"houridle/backend/steamauth"
	"houridle/backend/steamcm"
)

const ownedGamesTimeout = time.Minute

// loadOwnedGames asks Steam, over the saved sign-in, which games the account
// owns and adds them to library. It is how houridle can show a game list on
// a computer with no Steam installed.
func loadOwnedGames(mgr *steamauth.Manager, library map[uint32]*steam.Game) error {
	if handover.Idling() {
		// A second connection for the same account would knock the running
		// one off Steam.
		return errors.New("houridle is already idling; stop it first, or pass app IDs")
	}
	sess, err := mgr.Current()
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), ownedGamesTimeout)
	defer cancel()

	cs, err := steamcm.Connect(ctx, steamcm.ConfigFor(sess))
	if err != nil {
		var le *steamcm.LogonError
		if errors.As(err, &le) && le.AuthFailed() {
			return fmt.Errorf("Steam refused the saved sign-in: %w. Run houridle status for details, or houridle login again", err)
		}
		return err
	}
	defer cs.Close()

	games, err := cs.OwnedGames(ctx)
	if err != nil {
		return err
	}
	if len(games) == 0 {
		return errors.New("Steam says this account owns no games")
	}
	for _, g := range games {
		name := g.Name
		if name == "" {
			name = steam.PlaceholderName(g.AppID)
		}
		library[g.AppID] = &steam.Game{AppID: g.AppID, Name: name, Minutes: g.Minutes}
	}
	return nil
}
