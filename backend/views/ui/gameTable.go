package ui

import (
	"fmt"
	"strconv"
	"strings"

	"houridle/backend/client/steam"
	"houridle/backend/run/config"
)

func printGameTable(games []*steam.Game) {
	fmt.Println("  " + muted(fit("APP ID", appIDWidth)+"  "+fmt.Sprintf("%7s", "HOURS")+"   GAME"))
	for _, g := range games {
		name := g.Name
		if name == "" {
			name = muted("not installed")
		}
		id := accent(fit(strconv.FormatUint(uint64(g.AppID), 10), appIDWidth))
		fmt.Printf("  %s  %7s   %s\n", id, hours(g.Minutes), name)
	}
}

// PrintGameList is the -list screen.
func PrintGameList(library map[uint32]*steam.Game, filter string) {
	filter = strings.ToLower(strings.TrimSpace(filter))
	var rows []*steam.Game
	for _, g := range steam.SortedGames(library) {
		if filter == "" || strings.Contains(strings.ToLower(g.Name), filter) || strconv.FormatUint(uint64(g.AppID), 10) == filter {
			rows = append(rows, g)
		}
	}
	fmt.Println()
	fmt.Println(banner("your games", maxContentWidth))
	fmt.Println()
	if len(rows) == 0 {
		fmt.Println("  " + muted("no games match "+strconv.Quote(filter)))
	} else {
		printGameTable(rows)
	}
	fmt.Println()
	fmt.Println("  " + muted(fmt.Sprintf("%d game(s) · installed or played on this PC", len(rows))))
	fmt.Println("  " + muted("any game you own can be idled by app ID, even if it's not installed"))
	fmt.Println()
}

// promptGames is the plain-text picker used when input isn't an interactive console.
func promptGames(library map[uint32]*steam.Game) ([]uint32, error) {
	top := steam.SortedGames(library)
	if len(top) > 25 {
		top = top[:25]
	}
	if len(top) > 0 {
		fmt.Println()
		printGameTable(top)
	}
	for {
		line, err := readLine("\n  app IDs to idle (comma separated): ")
		ids, parseErr := config.ParseAppIDs([]string{line})
		if parseErr == nil && len(ids) > 0 {
			return config.Dedupe(ids), nil
		}
		if err != nil {
			return nil, ErrCancelled // stdin closed
		}
		fmt.Println("  " + warning("enter at least one app ID, like 730 or 730, 440"))
	}
}

// PrintIdleHeader lists the games when there's no dashboard (output redirected).
func PrintIdleHeader(games []*steam.Game) {
	fmt.Println()
	fmt.Println(banner("idling "+strconv.Itoa(len(games))+" game(s)", maxContentWidth))
	fmt.Println()
	for _, g := range games {
		id := accent(fit(strconv.FormatUint(uint64(g.AppID), 10), appIDWidth))
		fmt.Printf("  %s  %s  %s\n", id, g.Name, muted(hours(g.Minutes)))
	}
	fmt.Println()
}
