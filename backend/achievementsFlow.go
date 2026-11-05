//go:build windows || linux

package main

import (
	"context"
	"errors"
	"fmt"
	"time"

	"houridle/backend/achievements"
	"houridle/backend/config"
	"houridle/backend/handover"
	"houridle/backend/steam"
	"houridle/backend/steamauth"
	"houridle/backend/steamcm"
	"houridle/backend/steamworks"
	"houridle/backend/ui"
)

// runAchievements is the -achievements flow: pick a game, pick changes, apply.
func runAchievements(appDir, steamDir, dllFlag string, library map[uint32]*steam.Game, args []string) int {
	appID, err := achievementsTarget(library, args)
	if errors.Is(err, ui.ErrCancelled) {
		ui.Notice("cancelled, nothing changed")
		return 0
	}
	if err != nil {
		return ui.Fatal(err)
	}

	if !steam.Running() {
		return ui.Fatal(errors.New("start Steam and sign in first"))
	}
	exePath, dllPath, err := helperPaths(appDir, steamDir, dllFlag)
	if err != nil {
		return ui.Fatal(err)
	}
	game := steam.ResolveGames([]uint32{appID}, library)[0]

	ui.Progress("loading achievements for " + game.Name)
	helper, list, err := achievements.Open(exePath, dllPath, appID)
	if err != nil {
		return ui.Fatal(err)
	}
	defer helper.Close()
	if len(list) == 0 {
		ui.Notice(game.Name + " has no achievements")
		return 0
	}

	unlock, lock, err := ui.PickAchievements(game.Name, list)
	if errors.Is(err, ui.ErrCancelled) {
		ui.Notice("cancelled, nothing changed")
		return 0
	}
	if err != nil {
		return ui.Fatal(err)
	}
	if len(unlock)+len(lock) == 0 {
		ui.Notice("no changes")
		return 0
	}

	res, err := helper.Apply(unlock, lock)
	if err != nil {
		return ui.Fatal(err)
	}
	failed := make(map[string]bool, len(res.Failed))
	for _, f := range res.Failed {
		failed[f.ID] = true
	}
	ui.PrintAchievementResult(game.Name, res, countOK(unlock, failed), countOK(lock, failed))
	return 0
}

// achievementsTarget is the app ID from the command line, or picked by the user.
func achievementsTarget(library map[uint32]*steam.Game, args []string) (uint32, error) {
	if len(args) == 0 {
		return ui.PickGame(library)
	}
	ids, err := config.ParseAppIDs(args)
	if err != nil {
		return 0, err
	}
	if len(ids) != 1 {
		return 0, errors.New("pass one app ID, like: houridle -achievements 252490")
	}
	return ids[0], nil
}

func countOK(ids []string, failed map[string]bool) int {
	n := 0
	for _, id := range ids {
		if !failed[id] {
			n++
		}
	}
	return n
}

// runSessionAchievements is -achievements with a saved sign-in (houridle
// start -achievements): the same picker, but over houridle's own Steam
// connection, so no Steam client is needed.
func runSessionAchievements(mgr *steamauth.Manager, library map[uint32]*steam.Game, args []string) int {
	if handover.Idling() {
		// The same sign-in can only hold one connection; this one would
		// knock idling off for a minute.
		return ui.Fatal(errors.New("houridle is idling with this sign-in; stop it first, then change achievements"))
	}
	if len(args) == 0 && len(library) == 0 {
		ui.Progress("Loading the games your Steam account owns")
		if err := loadOwnedGames(mgr, library); err != nil {
			return ui.Fatal(fmt.Errorf("couldn't load your games: %w", err))
		}
	}
	appID, err := achievementsTarget(library, args)
	if errors.Is(err, ui.ErrCancelled) {
		ui.Notice("cancelled, nothing changed")
		return 0
	}
	if err != nil {
		return ui.Fatal(err)
	}
	game := steam.ResolveGames([]uint32{appID}, library)[0]

	sess, err := mgr.Current()
	if err != nil {
		return ui.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), achievementsTimeout)
	defer cancel()
	ui.Progress("loading achievements for " + game.Name)
	cs, err := steamcm.Connect(ctx, steamcm.ConfigFor(sess))
	if err != nil {
		return ui.Fatal(err)
	}
	defer cs.Close()
	stats, err := cs.UserStats(ctx, appID)
	if err != nil {
		return ui.Fatal(err)
	}
	if len(stats.Achievements) == 0 {
		ui.Notice(game.Name + " has no achievements")
		return 0
	}
	list := make([]steamworks.Achievement, len(stats.Achievements))
	for i, a := range stats.Achievements {
		list[i] = steamworks.Achievement{ID: a.ID, Name: a.Name, Description: a.Description, Hidden: a.Hidden, Unlocked: a.Unlocked}
	}

	unlock, lock, err := ui.PickAchievements(game.Name, list)
	if errors.Is(err, ui.ErrCancelled) {
		ui.Notice("cancelled, nothing changed")
		return 0
	}
	if err != nil {
		return ui.Fatal(err)
	}
	if len(unlock)+len(lock) == 0 {
		ui.Notice("no changes")
		return 0
	}
	// The picker may have taken a while; give saving its own time.
	saveCtx, cancelSave := context.WithTimeout(context.Background(), achievementsTimeout)
	defer cancelSave()
	failedByID, err := cs.SetAchievements(saveCtx, stats, unlock, lock)
	if err != nil {
		return ui.Fatal(err)
	}
	res := achievements.Result{}
	failed := map[string]bool{}
	for id, why := range failedByID {
		res.Failed = append(res.Failed, achievements.Failure{ID: id, Error: why})
		failed[id] = true
	}
	res.Changed = len(unlock) + len(lock) - len(res.Failed)
	ui.PrintAchievementResult(game.Name, res, countOK(unlock, failed), countOK(lock, failed))
	return 0
}

const achievementsTimeout = 2 * time.Minute
