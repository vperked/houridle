package ui

import (
	"errors"
	"fmt"
	"strings"

	"houridle/backend/achievements"
	"houridle/backend/steamworks"
)

// PickAchievements shows a game's achievements, checked = unlocked, and
// returns which ones the user wants to unlock and lock.
func PickAchievements(gameName string, list []steamworks.Achievement) (unlock, lock []string, err error) {
	if !interactive() {
		return nil, nil, errors.New("the achievement unlocker needs an interactive console")
	}
	c := NewAchievementPicker(gameName, list)
	if err := c.run(); err != nil {
		return nil, nil, err
	}
	unlock, lock = c.Changes()
	return unlock, lock, nil
}

// NewAchievementPicker is a Checklist of a game's achievements, checked =
// unlocked. The right column shows pending "+ unlock" / "− lock" changes.
func NewAchievementPicker(gameName string, list []steamworks.Achievement) *Checklist {
	c := &Checklist{
		title:        "achievements · " + gameName,
		placeholder:  "type to search achievements",
		hints:        [2]string{"space toggle · ctrl+a all · enter apply · esc cancel", "space · ^a · enter · esc"},
		trackChanges: true,
		changeLabels: [3]string{"+ unlock", "− lock", "unlocked"},
		summary:      achievementSummary,
	}
	for _, a := range list {
		detail := a.Description
		if detail == "" && a.Hidden {
			detail = "hidden achievement"
		}
		c.items = append(c.items, &checkItem{
			id:      a.ID,
			label:   a.Name,
			detail:  detail,
			checked: a.Unlocked,
			initial: a.Unlocked,
			faded:   a.Hidden,
		})
	}
	return c
}

// achievementSummary is the footer, e.g. "12/40 unlocked · +3 −1".
func achievementSummary(c *Checklist) string {
	unlock, lock := c.Changes()
	status := fmt.Sprintf("%d/%d unlocked", c.checkedCount(), len(c.items))
	switch {
	case len(unlock) > 0 && len(lock) > 0:
		return status + fmt.Sprintf(" · +%d −%d", len(unlock), len(lock))
	case len(unlock) > 0:
		return status + fmt.Sprintf(" · +%d", len(unlock))
	case len(lock) > 0:
		return status + fmt.Sprintf(" · −%d", len(lock))
	}
	return status
}

// PrintAchievementResult reports what changed after applying.
func PrintAchievementResult(gameName string, res achievements.Result, unlocked, locked int) {
	fmt.Println()
	parts := []string{}
	if unlocked > 0 {
		parts = append(parts, fmt.Sprintf("unlocked %d", unlocked))
	}
	if locked > 0 {
		parts = append(parts, fmt.Sprintf("locked %d", locked))
	}
	summary := "nothing changed"
	if len(parts) > 0 {
		summary = strings.Join(parts, " · ")
	}
	fmt.Println(banner(gameName+" · "+summary, maxContentWidth))
	for _, f := range res.Failed {
		fmt.Printf("  %s %s  %s\n", danger("✕"), f.ID, muted(f.Error))
	}
	fmt.Println()
}
