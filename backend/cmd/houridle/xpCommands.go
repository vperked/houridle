//go:build windows || linux

package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"time"

	"houridle/backend/features/cards"
	"houridle/backend/online/steamauth"
	"houridle/backend/online/steamcm"
	"houridle/backend/views/ui"
)

const xpTimeout = 5 * time.Minute

// signedInConn opens a Steam connection for a saved account, refusing if a
// houridle is idling with it (a second connection would knock that off).
func signedInConn(ctx context.Context, selector, busyHint string) (*steamcm.Session, steamauth.Session, error) {
	mgr, acc, err := steamauth.NewManagerFor(selector)
	if err != nil {
		return nil, steamauth.Session{}, err
	}
	if accountIdling(acc) {
		return nil, steamauth.Session{}, errors.New("houridle is idling with this account; " + busyHint)
	}
	sess, err := mgr.Current()
	if err != nil {
		return nil, sess, err
	}
	cs, err := steamcm.Connect(ctx, steamcm.ConfigFor(sess))
	return cs, sess, err
}

// runCraft is `houridle craft`: craft every badge whose card set is
// complete (Steam XP), and show the badges closest to one.
func runCraft(args []string) int {
	fs := flag.NewFlagSet("craft", flag.ContinueOnError)
	account := fs.String("account", "", "which saved account (default: the default account)")
	dryRun := fs.Bool("dry-run", false, "only show what could be crafted")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	ui.Init()
	ctx, cancel := context.WithTimeout(context.Background(), xpTimeout)
	defer cancel()
	ui.Progress("Reading your badges")
	cs, sess, err := signedInConn(ctx, *account, "it can craft for you instead: start it with -craft")
	if err != nil {
		return ui.Fatal(err)
	}
	defer cs.Close()
	token, err := cs.AccessToken(ctx)
	if err != nil {
		return ui.Fatal(err)
	}
	badges, err := cards.Badges(ctx, sess.SteamID, token)
	if err != nil {
		return ui.Fatal(err)
	}

	fmt.Println()
	crafted := 0
	ready := cards.ReadyToCraft(badges)
	if *dryRun {
		for _, c := range ready {
			ui.Info("Ready to craft: " + c.Label())
		}
	} else {
		cards.CraftReady(ctx, ready,
			func(ctx context.Context, appID uint32, foil bool) (cards.Crafted, error) {
				return cards.Craft(ctx, sess.SteamID, token, appID, foil)
			},
			func(c cards.Craftable, got cards.Crafted) {
				crafted++
				ui.Success(cards.CraftedText(c, got))
			},
			func(c cards.Craftable, err error) { ui.Warn("Couldn't craft " + c.Label() + ": " + err.Error()) })
	}
	if crafted == 0 && !*dryRun {
		ui.Notice("No badge has a full card set to craft yet")
	}

	closest := cards.Closest(badges)
	if len(closest) > 0 {
		fmt.Println()
		ui.Info("Closest to a badge (buy the missing cards on the Steam Market, then craft):")
		for i, b := range closest {
			if i == 10 {
				break
			}
			fmt.Printf("    %-34s %d of %d cards, %s\n", clip(b.Name, 34), b.Collected, b.Total, ui.Dim(fmt.Sprintf("%d missing", b.Missing())))
		}
		ui.Notice("The control panel (houridle serve) lists each missing card with a link to it on the Market.")
	}
	fmt.Println()
	return 0
}

// runEvents is `houridle events`: explore the discovery queue and claim
// the daily sale-event sticker (during Steam's sales).
func runEvents(args []string) int {
	fs := flag.NewFlagSet("events", flag.ContinueOnError)
	account := fs.String("account", "", "which saved account (default: the default account)")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	ui.Init()
	ctx, cancel := context.WithTimeout(context.Background(), xpTimeout)
	defer cancel()
	ui.Progress("Exploring your discovery queue")
	cs, _, err := signedInConn(ctx, *account, "it can do this for you instead: start it with -events")
	if err != nil {
		return ui.Fatal(err)
	}
	defer cs.Close()
	time.Sleep(time.Second) // Steam sends the account's country just after sign-in
	n, err := cs.ExploreQueue(ctx)
	if err != nil {
		return ui.Fatal(err)
	}
	fmt.Println()
	ui.Success(fmt.Sprintf("Explored the discovery queue (%d games)", n))
	switch err := cs.ClaimSaleItem(ctx); {
	case err == nil:
		ui.Success("Claimed today's sale-event sticker")
	case errors.Is(err, steamcm.ErrNoSticker):
		ui.Notice("No sale-event sticker to claim right now (they come during Steam's big sales)")
	default:
		ui.Warn("Couldn't claim the sale-event sticker: " + err.Error())
	}
	fmt.Println()
	return 0
}

func clip(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n-1]) + "…"
}
