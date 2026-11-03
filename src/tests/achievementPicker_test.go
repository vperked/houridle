package tests

import (
	"fmt"
	"strings"
	"testing"

	"houridle/src/steamworks"
	"houridle/src/ui"
)

func TestAchievementChangesAreTracked(t *testing.T) {
	c := ui.NewAchievementPicker("Rust", testAchievements())
	press(t, c, keySpace)          // lock Apex Predator (was unlocked)
	press(t, c, keyDown, keySpace) // unlock Battle Bus

	unlock, lock := c.Changes()
	if fmt.Sprint(unlock, lock) != "[ACH_B] [ACH_A]" {
		t.Fatalf("changes = %v %v", unlock, lock)
	}
	if got := c.Status(); got != "1/3 unlocked · +1 −1" {
		t.Fatalf("status = %q", got)
	}

	press(t, c, keyUp, keySpace) // unlock Apex again: no longer a change
	if unlock, lock := c.Changes(); len(lock) != 0 || len(unlock) != 1 {
		t.Fatalf("toggling back should undo the change, got %v %v", unlock, lock)
	}
}

func TestAchievementSelectAllAndSearch(t *testing.T) {
	c := ui.NewAchievementPicker("Rust", testAchievements())
	press(t, c, ctrlA) // unlock everything
	if unlock, _ := c.Changes(); fmt.Sprint(unlock) != "[ACH_B ACH_C]" {
		t.Fatalf("ctrl+a should unlock all, got %v", unlock)
	}
	press(t, c, ctrlA) // again: lock everything
	if _, lock := c.Changes(); fmt.Sprint(lock) != "[ACH_A]" {
		t.Fatalf("a second ctrl+a should lock all, got %v", lock)
	}

	press(t, c, "speargun") // search matches descriptions too
	if rows := c.Rows(); fmt.Sprint(rows) != "[Apex Predator]" {
		t.Fatalf("description search gave %v", rows)
	}
	if done, err := c.Press(keyEnter); !done || err != nil {
		t.Fatal("enter should finish even with no changes")
	}
}

func TestAchievementPickerLayoutFits(t *testing.T) {
	var list []steamworks.Achievement
	for i := range 60 {
		list = append(list, steamworks.Achievement{
			ID:          fmt.Sprint("ACH_", i),
			Name:        strings.Repeat("Long Achievement ", 4),
			Description: strings.Repeat("a very long description ", 6),
			Unlocked:    i%3 == 0,
		})
	}
	for _, size := range consoleSizes {
		c := ui.NewAchievementPicker(strings.Repeat("Long Game ", 8), list)
		press(t, c, keyPageDown, keyPageDown, keySpace)
		checkFrame(t, c.Render(size[0], size[1]), size[0], size[1])
	}
}
