//go:build windows

package ui

import (
	"fmt"
	"slices"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"houridle/src/idler"
)

const (
	statusWidth     = 17 // widest status: "waiting for steam"
	dashboardChrome = 10 // lines that aren't game rows or events: banner, header, totals, hint, spacing
)

var spinnerFrames = []string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"}

// dashboardState is everything one frame needs, copied out under the lock.
type dashboardState struct {
	views    []idler.View
	events   []idler.Event
	stopping bool
	frame    int // animation step for spinners
}

func (d *Dashboard) state(frame int) dashboardState {
	s := dashboardState{views: d.src.Views(), frame: frame}
	d.mu.Lock()
	defer d.mu.Unlock()
	s.events = slices.Clone(d.events)
	s.stopping = d.stopping
	return s
}

// Preview draws one frame for src at the given console size without
// opening the dashboard (no full screen, no key input). src becomes the
// dashboard's source, as with Open. The tests use it to check layouts.
func (d *Dashboard) Preview(src ViewSource, termW, termH int) string {
	d.src = src
	return d.render(termW, termH, 0)
}

// render draws one frame of the dashboard for a console of termW × termH.
func (d *Dashboard) render(termW, termH, frame int) string {
	s := d.state(frame)
	width := contentWidth(termW)
	maxRows := s.fitToHeight(termH)

	var out screen
	out.line("")
	out.line(d.bannerLine(width))
	out.line("")
	s.writeGameTable(&out, width, maxRows)
	out.line("")
	out.line("  " + muted(strings.Repeat("─", width)))
	out.line("  " + s.totalsLine(width, time.Since(d.start)))
	s.writeEvents(&out, width)
	out.line("")
	out.line("  " + s.hintLine(width, d.keysOn))
	return out.String()
}

// fitToHeight drops the oldest events until the games fit, and returns how
// many game rows there's room for.
func (s *dashboardState) fitToHeight(termH int) int {
	room := func() int {
		eventLines := 0
		if len(s.events) > 0 {
			eventLines = len(s.events) + 2 // blank line before, spare line after
		}
		return termH - dashboardChrome - eventLines
	}
	for room() < len(s.views) && len(s.events) > 0 {
		s.events = s.events[1:]
	}
	return max(2, room())
}

// ---------- game table ----------

// gameColumns sizes the game table. On narrow consoles the app ID column is
// dropped first, then the status column shrinks.
type gameColumns struct {
	nameW, statusW int
	showID         bool
}

func dashboardColumns(width int) gameColumns {
	c := gameColumns{statusW: statusWidth, showID: width >= 50}
	fixed := 1 + 2 + 2 // icon, gap, gap
	if c.showID {
		fixed += appIDWidth + 2
	}
	c.nameW = width - fixed - c.statusW
	if c.nameW < 8 {
		c.statusW = max(8, width-fixed-8)
		c.nameW = max(1, width-fixed-c.statusW)
	}
	return c
}

func (s dashboardState) writeGameTable(out *screen, width, maxRows int) {
	cols := dashboardColumns(width)

	header := "     " + fit("GAME", cols.nameW) + "  "
	if cols.showID {
		header += fit("APP ID", appIDWidth) + "  "
	}
	out.line(muted(header + clip("STATUS", cols.statusW)))

	shown := s.views
	if len(shown) > maxRows {
		shown = shown[:maxRows-1] // leave a line for "+ N more"
	}
	for _, v := range shown {
		out.line(s.gameRow(v, cols))
	}
	if hidden := len(s.views) - len(shown); hidden > 0 {
		out.line("     " + muted(fmt.Sprintf("+ %d more", hidden)))
	}
}

func (s dashboardState) gameRow(v idler.View, cols gameColumns) string {
	icon, status := s.gameStatus(v, cols.statusW)
	name := fit(v.Game.Name, cols.nameW)
	if v.State == idler.StateFailing || v.State == idler.StateStopped {
		name = muted(name)
	}
	row := "  " + icon + "  " + name + "  "
	if cols.showID {
		row += muted(fit(strconv.FormatUint(uint64(v.Game.AppID), 10), appIDWidth)) + "  "
	}
	return row + status
}

// gameStatus returns the colored icon and status text for one game.
func (s dashboardState) gameStatus(v idler.View, width int) (icon, status string) {
	spinner := spinnerFrames[s.frame%len(spinnerFrames)]
	switch {
	case v.State == idler.StateIdling && s.stopping:
		return warning(spinner), warning(clip("stopping", width))
	case v.State == idler.StateIdling:
		return success("●"), success(clip(clock(v.Idled), width))
	case v.State == idler.StateStarting:
		return warning(spinner), warning(clip("starting", width))
	case v.State == idler.StateWaiting:
		return warning("◌"), warning(clip("waiting for steam", width))
	case v.State == idler.StateFailing:
		return danger("✕"), danger(clip(v.Note, width))
	default:
		return muted("○"), muted(clip("stopped", width))
	}
}

// ---------- totals, events and hint ----------

// totalsLine summarizes the session, shortening itself on narrow consoles.
func (s dashboardState) totalsLine(width int, session time.Duration) string {
	idling, waiting := 0, 0
	var total time.Duration
	for _, v := range s.views {
		total += v.Idled
		switch v.State {
		case idler.StateIdling:
			idling++
		case idler.StateWaiting:
			waiting++
		}
	}

	var state string
	switch {
	case idling == 0 && waiting > 0:
		state = "◌ waiting for steam"
	case idling == 0:
		state = "○ none idling"
	default:
		state = fmt.Sprintf("● %d/%d idling", idling, len(s.views))
	}
	colored := success
	if idling == 0 {
		colored = warning
	}

	gameHours := fmt.Sprintf("%.2f", total.Hours())
	switch {
	case width >= 56:
		return colored(state) + "     " + muted("session ") + clock(session) + "     " + success("+"+gameHours) + muted(" game-hours")
	case width >= 40:
		return colored(state) + muted("  ·  ") + clock(session) + muted("  ·  +") + gameHours + muted(" h")
	default:
		short, _, _ := strings.Cut(state, " idling") // "● 2/3"
		return colored(clip(short, width-10)) + "  " + clock(session)
	}
}

func (s dashboardState) writeEvents(out *screen, width int) {
	if len(s.events) == 0 {
		return
	}
	out.line("")
	for _, e := range s.events {
		out.line("  " + muted(e.At.Format("15:04")) + "  " + levelIcon(e.Level) + " " + clip(e.Text, width-9))
	}
}

func (s dashboardState) hintLine(width int, keysOn bool) string {
	switch {
	case s.stopping:
		return warning(clip(spinnerFrames[s.frame%len(spinnerFrames)]+" stopping, closing helpers…", width))
	case keysOn && width >= 22:
		return accent("t") + muted(" send to tray   ") + accent("q") + muted(" stop")
	case keysOn:
		return accent("t") + muted(" tray · ") + accent("q") + muted(" stop")
	default:
		return muted(clip("ctrl+c to stop", width))
	}
}

// bannerLine is the heading, with the version on the right when there's room.
func (d *Dashboard) bannerLine(width int) string {
	const subtitle = "steam hour idler"
	version := "v" + d.version
	pad := width - utf8.RuneCountInString("◆ houridle  "+subtitle) - len(version)
	if pad < 2 {
		return banner(subtitle, width)
	}
	return banner(subtitle, width) + strings.Repeat(" ", pad) + muted(version)
}

func levelIcon(level idler.EventLevel) string {
	switch level {
	case idler.EventGood:
		return success("●")
	case idler.EventWarn:
		return warning("!")
	case idler.EventBad:
		return danger("✕")
	default:
		return muted("·")
	}
}
