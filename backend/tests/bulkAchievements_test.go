package tests

import (
	"context"
	"strings"
	"testing"
)

func TestUnlockEverythingInSeveralGames(t *testing.T) {
	ctl, cm, _ := newPanel(t)
	cm.schema = achievementSchema()
	cm.statValues = map[uint32]uint32{1: 0b10} // "Secret" unlocked, "First Steps" locked
	ctx := context.Background()

	// The fake keeps one set of stats for every game, so the second game
	// finds the first one's changes already made.
	res, err := ctl.SetAllAchievements(ctx, []uint32{252490, 440}, true)
	if err != nil || len(res) != 2 {
		t.Fatalf("unlock: %+v %v", res, err)
	}
	if r := res[0]; r.AppID != 252490 || r.Total != 3 || r.Changed != 1 || r.Already != 1 || r.GameOnly != 1 || r.Error != "" {
		t.Errorf("first game: %+v", r)
	}
	if r := res[1]; r.Changed != 0 || r.Already != 2 || r.GameOnly != 1 || r.Name != "TF2" && r.Name != "App 440" {
		t.Errorf("second game: %+v", r)
	}
	list, _ := ctl.Achievements(ctx, 252490)
	for _, a := range list {
		if !a.Protected && !a.Unlocked {
			t.Errorf("%s should be unlocked", a.ID)
		}
		if a.Protected && a.Unlocked {
			t.Errorf("%s is game-only and must be left alone", a.ID)
		}
	}

	res, err = ctl.SetAllAchievements(ctx, []uint32{252490}, false)
	if err != nil || res[0].Changed != 2 {
		t.Fatalf("lock: %+v %v", res, err)
	}

	events := 0
	for _, e := range ctl.Status().Events {
		if strings.Contains(e.Text, "achievement(s) in") {
			events++
		}
	}
	if events != 2 {
		t.Errorf("one activity line per run, got %d", events)
	}

	if _, err := ctl.SetAllAchievements(ctx, nil, true); err == nil {
		t.Error("no games should be refused")
	}
	if _, err := ctl.SetAllAchievements(ctx, make([]uint32, 101), true); err == nil {
		t.Error("too many games should be refused")
	}
}
