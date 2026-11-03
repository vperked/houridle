package ui

import (
	"os"
	"slices"
	"sync"
	"time"

	"houridle/src/idler"
)

const (
	frameInterval = 200 * time.Millisecond // how often the dashboard redraws
	maxEvents     = 5                      // lines in the recent-events feed
)

// ViewSource supplies the per-game status the dashboard draws.
type ViewSource interface {
	Views() []idler.View
}

// Action is a key command from the dashboard.
type Action int

const (
	ActionStop Action = iota // q or ctrl+c
	ActionTray               // t: move to the system tray
)

// Dashboard is the live full-screen view shown while idling.
//
// Lifecycle: NewDashboard, Open, then read Actions until the user acts,
// SetStopping while helpers shut down, and Close.
type Dashboard struct {
	src     ViewSource
	start   time.Time
	version string

	mu       sync.Mutex // guards events, stopping and stopAt
	events   []idler.Event
	stopping bool
	stopAt   time.Time // automatic stop; zero for none

	trayOn       bool   // "t" sends houridle to the tray (Windows)
	keysOn       bool   // keyboard commands available (input is a console)
	restoreInput func() // puts the console back to normal line input
	actions      chan Action
	done         chan struct{} // closed by Close to stop drawing
	exited       chan struct{} // closed when the draw loop has stopped
}

// NewDashboard prepares the dashboard. trayAvailable offers "t" to move to
// the tray (only where there is one).
func NewDashboard(start time.Time, version string, trayAvailable bool) *Dashboard {
	return &Dashboard{
		start:   start,
		version: version,
		trayOn:  trayAvailable,
		actions: make(chan Action, 1),
		done:    make(chan struct{}),
		exited:  make(chan struct{}),
	}
}

// Open switches to the full-screen view and starts drawing src.
func (d *Dashboard) Open(src ViewSource) {
	d.src = src
	if stdinIsTerminal() {
		if restore, err := enableRawInput(); err == nil {
			d.keysOn, d.restoreInput = true, restore
			go d.readKeys()
		}
	}
	os.Stdout.WriteString(altScreenOn)
	go d.drawLoop()
}

// Close stops drawing and restores the normal screen and input mode.
func (d *Dashboard) Close() {
	close(d.done)
	<-d.exited
	os.Stdout.WriteString(altScreenOff)
	if d.restoreInput != nil {
		d.restoreInput()
	}
}

// Actions delivers key commands (t, q, ctrl+c) while the dashboard is open.
func (d *Dashboard) Actions() <-chan Action { return d.actions }

// AddEvent records an event for the feed. A repeat of the last event just
// refreshes its time, so retries don't flood the feed.
func (d *Dashboard) AddEvent(e idler.Event) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if n := len(d.events); n > 0 && d.events[n-1].Text == e.Text {
		d.events[n-1] = e
		return
	}
	if len(d.events) == maxEvents {
		d.events = slices.Delete(d.events, 0, 1)
	}
	d.events = append(d.events, e)
}

// SetStopTime shows when houridle will stop by itself (zero hides it).
func (d *Dashboard) SetStopTime(at time.Time) {
	d.mu.Lock()
	d.stopAt = at
	d.mu.Unlock()
}

// SetStopping switches the footer to "stopping…" while helpers shut down.
func (d *Dashboard) SetStopping() {
	d.mu.Lock()
	d.stopping = true
	d.mu.Unlock()
}

// drawLoop redraws on a timer, writing only frames that changed.
func (d *Dashboard) drawLoop() {
	defer close(d.exited)
	ticker := time.NewTicker(frameInterval)
	defer ticker.Stop()
	var last string
	for frame := 0; ; frame++ {
		w, h := termSize()
		if out := d.render(w, h, frame); out != last {
			os.Stdout.WriteString(out)
			last = out
		}
		select {
		case <-d.done:
			return
		case <-ticker.C:
		}
	}
}

// readKeys turns console key presses into actions. It runs until the
// process exits; Close restores line input so nothing is read after that.
func (d *Dashboard) readKeys() {
	buf := make([]byte, 64)
	for {
		n, err := os.Stdin.Read(buf)
		if err != nil {
			return
		}
		for _, key := range buf[:n] {
			switch key {
			case 't', 'T':
				if d.trayOn {
					d.sendAction(ActionTray)
				}
			case 'q', 'Q', ctrlC:
				d.sendAction(ActionStop)
			}
		}
	}
}

func (d *Dashboard) sendAction(a Action) {
	select {
	case d.actions <- a:
	default: // an action is already waiting to be handled
	}
}
