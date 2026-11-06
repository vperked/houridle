//go:build windows

package tray

import (
	"fmt"
	"time"

	"fyne.io/systray"

	"houridle/backend/run/idler"
	"houridle/backend/run/stoptime"
)

const (
	refreshEvery  = time.Second
	maxTooltipLen = 127 // Windows' limit for tray tooltips
)

// Run shows the tray icon and blocks until the user picks Open dashboard or
// Stop idling, or stop is closed (e.g. on logoff or at the stop time).
// stopAt is shown in the menu when set.
func Run(src ViewSource, start, stopAt time.Time, stop <-chan struct{}, sessionEnding func()) Choice {
	choice := make(chan Choice, 1) // filled before systray.Quit, so it's ready when Run returns
	onReady := func() {
		systray.SetIcon(Icon())
		m := buildMenu(len(src.Views()))
		m.stopAt = stopAt
		go func() {
			choice <- m.waitForChoice(src, start, stop)
			systray.Quit()
		}()
	}
	onExit := func() {
		if len(choice) == 0 && sessionEnding != nil {
			sessionEnding()
		}
	}
	systray.Run(onReady, onExit)

	select {
	case c := <-choice:
		return c
	default:
		return ChoiceStop // the tray closed without a choice
	}
}

// menu is the tray's right-click menu.
type menu struct {
	header *systray.MenuItem   // "houridle · 2/3 idling · 01:02:03"
	games  []*systray.MenuItem // one status line per game
	open   *systray.MenuItem
	quit   *systray.MenuItem

	titles  map[*systray.MenuItem]string // what each item shows now, to skip unchanged updates
	tooltip string
	stopAt  time.Time // automatic stop; zero for none
}

func buildMenu(gameCount int) *menu {
	m := &menu{titles: map[*systray.MenuItem]string{}}
	m.header = systray.AddMenuItem("houridle", "")
	m.header.Disable()
	for range gameCount {
		item := systray.AddMenuItem("", "")
		item.Disable() // status lines aren't clickable
		m.games = append(m.games, item)
	}
	systray.AddSeparator()
	m.open = systray.AddMenuItem("Open dashboard", "Show houridle in a console window")
	m.quit = systray.AddMenuItem("Stop idling", "Stop every game and close houridle")
	return m
}

// waitForChoice keeps the menu up to date until the user picks an action.
func (m *menu) waitForChoice(src ViewSource, start time.Time, stop <-chan struct{}) Choice {
	ticker := time.NewTicker(refreshEvery)
	defer ticker.Stop()
	for {
		m.refresh(src.Views(), time.Since(start))
		select {
		case <-m.open.ClickedCh:
			return ChoiceOpenDashboard
		case <-m.quit.ClickedCh:
			return ChoiceStop
		case <-stop:
			return ChoiceStop
		case <-ticker.C:
		}
	}
}

// refresh updates the header, game lines and tooltip.
func (m *menu) refresh(views []idler.View, session time.Duration) {
	idling := 0
	for _, v := range views {
		if v.State == idler.StateIdling {
			idling++
		}
	}
	summary := fmt.Sprintf("%d/%d idling · %s", idling, len(views), clock(session))
	if !m.stopAt.IsZero() {
		summary += " · stops " + stoptime.Describe(m.stopAt, time.Now())
	}

	m.setTitle(m.header, "houridle · "+summary)
	for i, v := range views {
		if i < len(m.games) {
			m.setTitle(m.games[i], gameLine(v))
		}
	}
	if tip := "houridle\n" + summary; tip != m.tooltip {
		m.tooltip = tip
		systray.SetTooltip(clip(tip, maxTooltipLen))
	}
}

func (m *menu) setTitle(item *systray.MenuItem, title string) {
	if m.titles[item] != title {
		m.titles[item] = title
		item.SetTitle(title)
	}
}

// gameLine is one game's menu line, e.g. "●  Rust    01:02:03".
func gameLine(v idler.View) string {
	name := clip(v.Game.Name, 40)
	switch {
	case v.State == idler.StateIdling:
		return "●  " + name + "    " + clock(v.Idled)
	case v.State == idler.StateStarting:
		return "○  " + name + "    starting…"
	case v.State == idler.StateWaiting && v.Note != "":
		return "○  " + name + "    " + v.Note
	case v.State == idler.StateWaiting:
		return "○  " + name + "    waiting for Steam"
	case v.State == idler.StateFailing:
		return "✕  " + name + "    " + v.Note
	case v.State == idler.StateQueued:
		return "◷  " + name + "    queued"
	case v.State == idler.StateDone:
		return "✓  " + name + "    " + v.Note
	case v.State == idler.StateHeld:
		return "$  " + name + "    " + v.Note
	default:
		return "○  " + name + "    stopped"
	}
}

func clock(d time.Duration) string {
	s := int(d.Seconds())
	return fmt.Sprintf("%02d:%02d:%02d", s/3600, (s%3600)/60, s%60)
}

func clip(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n-1]) + "…"
}
