package freegames

import (
	"context"
	"time"

	"houridle/backend/online/steamcm"
)

// Outcome is what happened to one offer.
type Outcome int

const (
	Added     Outcome = iota // added to the account (Steam confirmed it)
	Sent                     // the store took it, but Steam hasn't confirmed it yet
	WouldAdd                 // dry run: would be added
	Owned                    // already on the account
	NeedsBase                // DLC for a game the account doesn't own
	Failed                   // the store refused or couldn't be reached
)

// Result is one offer and what happened to it.
type Result struct {
	Offer   Offer
	Outcome Outcome
	Err     error // with Failed
}

const (
	licensesTimeout = 20 * time.Second
	confirmTimeout  = 20 * time.Second
)

// ClaimAll finds Steam's giveaways and adds the ones worth having to the
// signed-in account: not already owned, and not DLC for a game it doesn't
// own. With dryRun it only reports what it would add.
func ClaimAll(ctx context.Context, cs *steamcm.Session, steamID uint64, dryRun bool) ([]Result, error) {
	offers, err := Find(ctx)
	if err != nil {
		return nil, err
	}
	if len(offers) == 0 {
		return nil, nil
	}
	wait, cancel := context.WithTimeout(ctx, licensesTimeout)
	owned, err := cs.Licenses(wait)
	cancel()
	if err != nil {
		return nil, err
	}
	games, err := cs.OwnedGames(ctx)
	if err != nil {
		return nil, err
	}
	ownedApps := make(map[uint32]bool, len(games))
	for _, g := range games {
		ownedApps[g.AppID] = true
	}

	var results []Result
	token := ""
	for _, o := range offers {
		r := Result{Offer: o}
		switch {
		case owned[o.SubID] || o.AppID != 0 && !o.DLC && ownedApps[o.AppID]:
			r.Outcome = Owned
		case o.DLC && o.BaseAppID != 0 && !ownedApps[o.BaseAppID]:
			r.Outcome = NeedsBase
		case dryRun:
			r.Outcome = WouldAdd
		default:
			if token == "" {
				if token, err = cs.AccessToken(ctx); err != nil {
					return results, err
				}
			}
			r.Outcome, r.Err = claimOne(ctx, cs, steamID, token, o.SubID)
		}
		results = append(results, r)
	}
	return results, nil
}

// claimOne adds one package and waits for Steam to send the license list
// with it in.
func claimOne(ctx context.Context, cs *steamcm.Session, steamID uint64, token string, subID uint32) (Outcome, error) {
	changed := cs.LicensesChanged()
	if err := Claim(ctx, steamID, token, subID); err != nil {
		return Failed, err
	}
	deadline := time.NewTimer(confirmTimeout)
	defer deadline.Stop()
	for {
		select {
		case <-changed:
			owned, _ := cs.Licenses(ctx)
			if owned[subID] {
				return Added, nil
			}
			changed = cs.LicensesChanged()
		case <-deadline.C:
			return Sent, nil
		case <-ctx.Done():
			return Sent, nil
		}
	}
}
