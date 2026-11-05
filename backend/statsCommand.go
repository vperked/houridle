//go:build windows || linux

package main

import (
	"flag"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"houridle/backend/config"
	"houridle/backend/history"
	"houridle/backend/steam"
	"houridle/backend/steamauth"
	"houridle/backend/ui"
)

const maxStatsGames = 15

// runStats is `houridle stats`: the hours houridle has idled, from
// history.json.
func runStats(args []string) int {
	fs := flag.NewFlagSet("stats", flag.ContinueOnError)
	account := fs.String("account", "", "which saved account's history (default: the default account)")
	days := fs.Int("days", 14, "how many days to show one by one")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	ui.Init()
	acc := steamauth.SavedAccount{Default: true}
	if *account != "" {
		accts, err := steamauth.DefaultAccounts()
		if err != nil {
			return ui.Fatal(err)
		}
		if acc, err = accts.Find(*account); err != nil {
			return ui.Fatal(err)
		}
	}
	h, err := history.Open(filepath.Join(config.AppDir(), historyName(acc)))
	if err != nil {
		return ui.Fatal(err)
	}
	s := h.Summarize(time.Now(), max(1, min(*days, 366)))
	if s.All == 0 {
		ui.Notice("nothing idled yet (hours are added up from houridle v1.6 on, while it idles)")
		return 0
	}
	printStats(s)
	return 0
}

// hours formats a total: minutes under an hour ("25m"), else "3.4 h".
func hours(d time.Duration) string {
	if d < time.Hour {
		return fmt.Sprintf("%dm", int(d.Minutes()))
	}
	return fmt.Sprintf("%.1f h", d.Hours())
}

func printStats(s history.Summary) {
	fmt.Println()
	ui.Field("today", hours(s.Today))
	ui.Field("7 days", hours(s.Week))
	ui.Field("30 days", hours(s.Month))
	ui.Field("all time", hours(s.All)+ui.Dim("  since "+s.Since.Format("Jan 2, 2006")))

	fmt.Println()
	var most time.Duration
	for _, d := range s.Days {
		most = max(most, d.Time)
	}
	const barWidth = 30
	for _, d := range s.Days {
		if d.Day.Before(s.Since) {
			continue // before houridle kept a history: nothing to show
		}
		bar := 0
		if most > 0 {
			bar = int(float64(barWidth) * float64(d.Time) / float64(most))
		}
		if d.Time > 0 && bar == 0 {
			bar = 1
		}
		fmt.Printf("  %s  %s %s\n", ui.Dim(d.Day.Format("Mon Jan 02")), ui.Good(strings.Repeat("█", bar))+strings.Repeat(" ", barWidth-bar), hours(d.Time))
	}

	week := map[uint32]time.Duration{}
	for _, g := range s.WeekGames {
		week[g.AppID] = g.Time
	}
	fmt.Println()
	fmt.Println("  " + ui.Dim(fmt.Sprintf("%-34s %10s %10s", "GAME", "7 DAYS", "ALL TIME")))
	for i, g := range s.Games {
		if i == maxStatsGames {
			fmt.Printf("  %s\n", ui.Dim(fmt.Sprintf("+ %d more", len(s.Games)-maxStatsGames)))
			break
		}
		name := g.Name
		if name == "" {
			name = steam.PlaceholderName(g.AppID)
		}
		if len([]rune(name)) > 34 {
			name = string([]rune(name)[:33]) + "…"
		}
		fmt.Printf("  %-34s %10s %10s\n", name, hours(week[g.AppID]), hours(g.Time))
	}
	fmt.Println()
	ui.Notice("game-hours: idling 3 games for an hour counts as 3")
}
