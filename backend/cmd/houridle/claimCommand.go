//go:build windows || linux

package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"strings"
	"time"

	"houridle/backend/features/freegames"
	"houridle/backend/online/steamauth"
	"houridle/backend/online/steamcm"
	"houridle/backend/views/ui"
)

const claimTimeout = 5 * time.Minute

// runClaim is `houridle claim`: add Steam's current giveaways (games 100%
// off to keep) to the saved account.
func runClaim(args []string) int {
	fs := flag.NewFlagSet("claim", flag.ContinueOnError)
	account := fs.String("account", "", "which saved account (default: the default account)")
	dryRun := fs.Bool("dry-run", false, "only show what would be added")
	freeToPlay := fs.Int("free-to-play", 0, "also add this many of the best-reviewed free-to-play games you don't have")
	with := fs.String("with", "", `with -free-to-play: only games with "achievements", "cards" (trading cards), or both ("achievements,cards")`)
	sortBy := fs.String("sort", "reviews", `with -free-to-play: "reviews" (best reviewed), "popular" (most reviews), "newest" or "name"`)
	if err := fs.Parse(args); err != nil {
		return 2
	}
	ui.Init()
	mgr, acc, err := steamauth.NewManagerFor(*account)
	if err != nil {
		return ui.Fatal(err)
	}
	if accountIdling(acc) {
		return ui.Fatal(errors.New("houridle is idling with this account; it can claim for you instead: start it with -claim"))
	}
	sess, err := mgr.Current()
	if err != nil {
		return ui.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), claimTimeout)
	defer cancel()
	ui.Progress("Looking for free games on the Steam store")
	cs, err := steamcm.Connect(ctx, steamcm.ConfigFor(sess))
	if err != nil {
		return ui.Fatal(err)
	}
	defer cs.Close()
	results, err := freegames.ClaimAll(ctx, cs, sess.SteamID, *dryRun)
	if err != nil {
		return ui.Fatal(err)
	}
	fmt.Println()
	exit := 0
	if *freeToPlay > 0 {
		want, err := parseFeatures(*with)
		if err != nil {
			return ui.Fatal(err)
		}
		switch *sortBy {
		case "reviews", "popular", "newest", "name":
			want.Sort = *sortBy
		default:
			return ui.Fatal(fmt.Errorf("-sort: use reviews, popular, newest or name"))
		}
		exit = addTopFreeToPlay(ctx, cs, *freeToPlay, want, *dryRun)
	}
	if len(results) == 0 {
		ui.Notice("Steam isn't giving any games away right now")
		return exit
	}
	for _, r := range results {
		name := r.Offer.String() + reviewText(r.Offer.Review)
		switch r.Outcome {
		case freegames.Added:
			ui.Success("Added " + name)
		case freegames.Sent:
			ui.Success("Added " + name + ui.Dim("  (Steam hasn't confirmed it yet)"))
		case freegames.WouldAdd:
			ui.Info("Would add " + name)
		case freegames.Owned:
			ui.Info(ui.Dim("Already yours: " + name))
		case freegames.NeedsBase:
			ui.Info(ui.Dim("Skipped " + name + ": you don't own the game"))
		case freegames.Failed:
			ui.Warn("Couldn't add " + name + ": " + r.Err.Error())
			exit = 1
		}
	}
	fmt.Println()
	return exit
}

func reviewText(r freegames.Review) string {
	if r.Summary == "" {
		return ""
	}
	return ui.Dim(fmt.Sprintf("  %s, %d%% of %d reviews", r.Summary, r.Positive, r.Count))
}

// addTopFreeToPlay adds the n best-reviewed free-to-play games the account
// hasn't played yet.
func addTopFreeToPlay(ctx context.Context, cs *steamcm.Session, n int, want freegames.Features, dryRun bool) int {
	top, err := freegames.TopFreeToPlay(ctx, n+50, want) // some will be owned already
	if err != nil {
		ui.Warn(err.Error())
		return 1
	}
	owned := map[uint32]bool{}
	if games, err := freegames.Library(ctx, cs, cs.SteamID()); err == nil {
		for _, g := range games {
			owned[g.AppID] = true
		}
	}
	var pick []freegames.FreeToPlayGame
	for _, g := range top {
		if !owned[g.AppID] && len(pick) < n {
			pick = append(pick, g)
		}
	}
	if len(pick) == 0 {
		ui.Notice("You already have the best-reviewed free-to-play games")
		return 0
	}
	if dryRun {
		for _, g := range pick {
			ui.Info("Would add " + g.Name + reviewText(g.Review))
		}
		return 0
	}
	ids := make([]uint32, len(pick))
	for i, g := range pick {
		ids[i] = g.AppID
	}
	granted, err := cs.AddFreeGames(ctx, ids)
	if err != nil {
		ui.Warn("Couldn't add free-to-play games: " + err.Error())
		return 1
	}
	got := map[uint32]bool{}
	for _, id := range granted {
		got[id] = true
	}
	for _, g := range pick {
		if got[g.AppID] {
			ui.Success("Added " + g.Name + reviewText(g.Review))
		} else {
			ui.Info(ui.Dim("Not added (already yours, or not addable): " + g.Name))
		}
	}
	return 0
}

// parseFeatures reads -with: "achievements", "cards", or both.
func parseFeatures(s string) (freegames.Features, error) {
	var f freegames.Features
	for _, part := range strings.FieldsFunc(strings.ToLower(s), func(r rune) bool { return r == ',' || r == ' ' }) {
		switch part {
		case "achievements", "achievement":
			f.Achievements = true
		case "cards", "card", "badges", "trading-cards":
			f.Cards = true
		default:
			return f, fmt.Errorf("-with: %q isn't achievements or cards", part)
		}
	}
	return f, nil
}
