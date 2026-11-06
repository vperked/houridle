package tests

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"houridle/backend/client/steam"
	"houridle/backend/run/idler"
	"houridle/backend/views/ui"
)

// fakeGames stands in for the idle supervisor.
type fakeGames []idler.View

func (f fakeGames) Views() []idler.View { return f }

func manyGames(n int) fakeGames {
	states := []idler.State{idler.StateIdling, idler.StateStarting, idler.StateWaiting, idler.StateFailing, idler.StateStopped}
	var games fakeGames
	for i := range n {
		games = append(games, idler.View{
			Game:  &steam.Game{AppID: uint32(1000000 + i), Name: strings.Repeat("Long Name ", 6)},
			State: states[i%len(states)],
			Idled: 99 * time.Hour,
			Note:  "steam not ready",
		})
	}
	return games
}

func TestDashboardLayoutFits(t *testing.T) {
	d := ui.NewDashboard(time.Now().Add(-90*time.Minute), "1.0.0", true)
	for i := range 8 {
		d.AddEvent(idler.Event{At: time.Now(), Level: idler.EventLevel(i % 4), Text: fmt.Sprintf("event %d %s", i, strings.Repeat("detail ", 20))})
	}
	for _, stopAt := range []time.Time{{}, time.Now().Add(3 * time.Hour)} {
		d.SetStopTime(stopAt) // the stop line takes a row when set
		for _, size := range consoleSizes {
			for _, n := range []int{1, 3, idler.MaxGames} {
				checkFrame(t, d.Preview(manyGames(n), size[0], size[1]), size[0], size[1])
			}
		}
	}

	d.SetStopping()
	for _, size := range consoleSizes {
		checkFrame(t, d.Preview(manyGames(idler.MaxGames), size[0], size[1]), size[0], size[1])
	}
}

func TestDashboardShowsGamesAndTotals(t *testing.T) {
	games := fakeGames{
		{Game: &steam.Game{AppID: 252490, Name: "Rust"}, State: idler.StateIdling, Idled: time.Hour},
		{Game: &steam.Game{AppID: 470220, Name: "UNO"}, State: idler.StateFailing, Note: "not owned"},
	}
	frame := ui.NewDashboard(time.Now(), "1.0.0", true).Preview(games, 100, 30)
	for _, want := range []string{"Rust", "01:00:00", "UNO", "not owned", "1/2 idling", "+1.00"} {
		if !strings.Contains(frame, want) {
			t.Errorf("dashboard should show %q", want)
		}
	}
}

func TestDashboardShowsStopTime(t *testing.T) {
	d := ui.NewDashboard(time.Now(), "1.0.0", true)
	if strings.Contains(d.Preview(fakeGames{}, 100, 30), "stops at") {
		t.Fatal("no stop time set, so none should show")
	}
	d.SetStopTime(time.Now().Add(2*time.Hour + 30*time.Minute))
	if frame := d.Preview(fakeGames{}, 100, 30); !strings.Contains(frame, "stops at") || !strings.Contains(frame, "· in 02:") {
		t.Fatalf("the stop time should show with a countdown:\n%s", frame)
	}
}

func TestDashboardDedupesRepeatedEvents(t *testing.T) {
	d := ui.NewDashboard(time.Now(), "1.0.0", true)
	const text = "Steam closed, waiting for it to come back"
	for range 3 {
		d.AddEvent(idler.Event{At: time.Now(), Level: idler.EventWarn, Text: text})
	}
	if n := strings.Count(d.Preview(fakeGames{}, 100, 30), text); n != 1 {
		t.Fatalf("a repeated event should show once, shows %d times", n)
	}
}

func TestDashboardLayoutFitsWideNames(t *testing.T) {
	games := fakeGames{
		{Game: &steam.Game{AppID: 1, Name: strings.Repeat("原神", 30)}, State: idler.StateIdling, Idled: time.Hour},
		{Game: &steam.Game{AppID: 2, Name: strings.Repeat("🎮", 40)}, State: idler.StateFailing, Note: strings.Repeat("失敗", 20)},
	}
	d := ui.NewDashboard(time.Now(), "1.0.0", true)
	d.AddEvent(idler.Event{At: time.Now(), Text: strings.Repeat("東方 ", 30)})
	for _, size := range consoleSizes {
		checkFrame(t, d.Preview(games, size[0], size[1]), size[0], size[1])
	}
}

func TestDashboardKeysIgnoreEscapeSequences(t *testing.T) {
	d := ui.NewDashboard(time.Now(), "1.0.0", true)
	d.Press("\x1bOQ\x1b[1;5Q\x1b[A\x1bOT")
	select {
	case a := <-d.Actions():
		t.Fatalf("an escape sequence triggered action %v", a)
	default:
	}
	d.Press("q")
	select {
	case a := <-d.Actions():
		if a != ui.ActionStop {
			t.Fatalf("q gave action %v", a)
		}
	default:
		t.Fatal("q should stop")
	}
	d.Press("t")
	if a := <-d.Actions(); a != ui.ActionTray {
		t.Fatalf("t gave action %v", a)
	}
}
