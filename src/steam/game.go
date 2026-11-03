package steam

import (
	"slices"
	"strings"
)

type Game struct {
	AppID   uint32
	Name    string // empty for games played on this PC but not installed
	Minutes int    // playtime from Steam's local cache (can lag a little)
}

// SortedGames orders games by playtime (most first), named games before
// unnamed ones, then alphabetically.
func SortedGames(library map[uint32]*Game) []*Game {
	out := make([]*Game, 0, len(library))
	for _, g := range library {
		out = append(out, g)
	}
	slices.SortFunc(out, func(a, b *Game) int {
		if a.Minutes != b.Minutes {
			return b.Minutes - a.Minutes
		}
		if (a.Name == "") != (b.Name == "") {
			if a.Name == "" {
				return 1
			}
			return -1
		}
		if c := strings.Compare(strings.ToLower(a.Name), strings.ToLower(b.Name)); c != 0 {
			return c
		}
		return int(a.AppID) - int(b.AppID)
	})
	return out
}
