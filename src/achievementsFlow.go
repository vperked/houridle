//go:build windows

package main

import (
	"errors"

	"houridle/src/achievements"
	"houridle/src/config"
	"houridle/src/steam"
	"houridle/src/ui"
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
