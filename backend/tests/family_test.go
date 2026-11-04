package tests

import (
	"context"
	"testing"
)

func TestFamilySharedGamesJoinTheLibrary(t *testing.T) {
	ctl, cm, _ := newPanel(t)
	ctx := context.Background()

	lib, err := ctl.Library(ctx)
	if err != nil || len(lib) != 2 {
		t.Fatalf("without a family, just the account's games: %+v %v", lib, err)
	}

	cm.mu.Lock()
	cm.family = []fakeGame{{ID: 4000, Name: "Bob's Game"}, {ID: 730, Name: "CS"}} // CS is the account's own too
	cm.mu.Unlock()
	lib, err = ctl.Library(ctx)
	if err != nil {
		t.Fatal(err)
	}
	shared := map[uint32]bool{}
	for _, g := range lib {
		shared[g.AppID] = g.Shared
	}
	if len(lib) != 3 || !shared[4000] || shared[730] || shared[440] {
		t.Errorf("Bob's game should join, marked shared; CS stays the account's own; the excluded one stays out: %+v", lib)
	}
}
