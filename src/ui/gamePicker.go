//go:build windows

package ui

import (
	"fmt"
	"strconv"

	"houridle/src/idler"
	"houridle/src/steam"
)

// PickGames asks which games to idle and returns their app IDs. It falls back
// to a plain prompt when the console can't do interactive input.
func PickGames(library map[uint32]*steam.Game, preselected []uint32) ([]uint32, error) {
	if !interactive() {
		return promptGames(library)
	}
	c := NewGamePicker(library, preselected, idler.MaxGames)
	if err := c.run(); err != nil {
		return nil, err
	}
	return appIDs(c.Checked()), nil
}

// PickGame asks for one game (for the achievement unlocker).
func PickGame(library map[uint32]*steam.Game) (uint32, error) {
	if !interactive() {
		ids, err := promptGames(library)
		if err != nil || len(ids) == 0 {
			return 0, err
		}
		return ids[0], nil
	}
	c := NewGamePicker(library, nil, 1)
	c.title = "pick a game"
	c.hints = [2]string{"↑↓ move · enter pick · esc cancel", "enter · esc"}
	c.summary = func(*Checklist) string { return "pick one" }
	if err := c.run(); err != nil {
		return 0, err
	}
	return appIDs(c.Checked())[0], nil
}

// NewGamePicker is a Checklist of games: preselected ones first (checked),
// then the rest by playtime. maxChecked limits the choice (1 = pick one).
// Typing an unlisted app ID offers to add it.
func NewGamePicker(library map[uint32]*steam.Game, preselected []uint32, maxChecked int) *Checklist {
	c := &Checklist{
		title:       "pick games to idle",
		placeholder: "type to search, or type an app ID",
		hints:       [2]string{"↑↓ move · space select · enter done · esc cancel", "↑↓ · space · enter · esc"},
		maxChecked:  maxChecked,
		offerAdd:    offerAppID,
		summary:     func(c *Checklist) string { return fmt.Sprintf("%d selected", c.checkedCount()) },
	}
	chosen := make(map[uint32]bool, len(preselected))
	for _, id := range preselected {
		if chosen[id] {
			continue
		}
		chosen[id] = true
		g, ok := library[id]
		if !ok {
			g = &steam.Game{AppID: id}
		}
		it := gameItem(g)
		it.checked = true
		c.items = append(c.items, it)
	}
	for _, g := range steam.SortedGames(library) {
		if !chosen[g.AppID] {
			c.items = append(c.items, gameItem(g))
		}
	}
	return c
}

func gameItem(g *steam.Game) *checkItem {
	id := strconv.FormatUint(uint64(g.AppID), 10)
	it := &checkItem{id: id, label: g.Name, column: id, meta: hours(g.Minutes)}
	if g.Name == "" {
		it.label, it.faded = "App "+id+" · not installed", true
	}
	return it
}

// offerAppID lets a typed app ID be added even if it isn't in the local list.
func offerAppID(query string) (*checkItem, string) {
	n, err := strconv.ParseUint(query, 10, 32)
	if err != nil || n == 0 {
		return nil, ""
	}
	id := strconv.FormatUint(n, 10)
	return &checkItem{id: id, label: "App " + id, column: id}, "idle app " + id
}

func appIDs(ids []string) []uint32 {
	out := make([]uint32, 0, len(ids))
	for _, s := range ids {
		if n, err := strconv.ParseUint(s, 10, 32); err == nil {
			out = append(out, uint32(n))
		}
	}
	return out
}
