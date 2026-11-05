package ui

import (
	"fmt"
	"time"

	"houridle/src/idler"
)

// PrintEvent prints one event as a log line. It's used instead of the
// dashboard when output is redirected to a file or another program.
func PrintEvent(e idler.Event) {
	Logf("%s %s", levelIcon(e.Level), e.Text)
}

// PrintSummary is shown on the normal screen after idling stops.
func PrintSummary(views []idler.View, session time.Duration) {
	width := maxContentWidth
	if styled {
		w, _ := termSize()
		width = contentWidth(w)
	}

	var total time.Duration
	nameW := 0
	for _, v := range views {
		total += v.Idled
		nameW = max(nameW, CellWidth(v.Game.Name))
	}
	nameW = min(nameW, 40, max(8, width-20))

	fmt.Println()
	fmt.Println(banner(fmt.Sprintf("stopped after %s · +%.2f game-hours", clock(session), total.Hours()), width))
	fmt.Println()
	for _, v := range views {
		icon, status := success("●"), clock(v.Idled)
		switch {
		case v.Note != "" && v.Idled == 0:
			icon, status = danger("✕"), danger(v.Note)
		case v.Idled == 0:
			icon, status = muted("○"), muted("never started")
		}
		fmt.Printf("  %s  %s  %s\n", icon, fit(v.Game.Name, nameW), status)
	}
	fmt.Println()
}
