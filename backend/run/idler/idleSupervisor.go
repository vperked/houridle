// Package idler keeps games "running" in Steam. Each game gets a small hidden
// helper process (houridle re-run with -child <appid>); a worker per game
// starts its helper and restarts it if it stops.
//
// Files: idleSupervisor.go is the public API, idleWorker.go is one game's
// retry loop, helperProcess.go runs one helper, idleHelper.go is the helper.
package idler

import (
	"context"
	"sync"
	"time"

	"houridle/backend/client/steam"
)

// MaxGames is how many games Steam tracks as "playing" at once.
const MaxGames = 32

// State is where one game is in its lifecycle.
type State int

const (
	StateStarting State = iota
	StateIdling
	StateWaiting // for Steam to start
	StateFailing // Steam refused the app; retrying
	StateStopped
	StateQueued // more than MaxGames: waiting for a turn
	StateDone   // reached its target, or every card dropped
	StateHeld   // not idled: it could still be refunded
)

// EventLevel is how an event is shown: neutral, good news, warning or error.
type EventLevel int

const (
	EventInfo EventLevel = iota
	EventGood
	EventWarn
	EventBad
)

// Event is something worth showing in the activity feed.
type Event struct {
	At    time.Time
	Level EventLevel
	Text  string
	Alert bool      // worth an alert even though it isn't an error (e.g. a free game added)
	Kind  EventKind // what happened, for counting and celebrating ("" for most events)
	N     int       // how many (cards dropped, badges crafted, achievements unlocked...)
}

// EventKind says what an event is about, beyond its text.
type EventKind string

const (
	KindDrop         EventKind = "drop"         // N trading cards dropped
	KindItem         EventKind = "item"         // N new inventory items (likely card drops)
	KindCraft        EventKind = "craft"        // N badges crafted
	KindAchievements EventKind = "achievements" // N achievements unlocked
	KindAllUnlocked  EventKind = "all-unlocked" // every achievement of a game unlocked
)

// View is a point-in-time copy of one game's status.
type View struct {
	Game  *steam.Game
	State State
	Idled time.Duration
	Note  string // short reason when failing, e.g. "not owned"

	// Signed-in idling (houridle start) only; zero otherwise.
	Total  time.Duration // playtime on record at Steam (estimated between reads)
	Target time.Duration // when the game counts as done (-until)
	Drops  int           // card drops left (-cards); -1 unknown, 0 otherwise
}

// Supervisor runs one worker per game.
type Supervisor struct {
	workers []*worker
	onEvent func(Event)
	wg      sync.WaitGroup

	mu              sync.Mutex
	closedAnnounced bool
	restartPID      uint32
}

// New prepares a supervisor. exePath is houridle itself (re-run as the
// helper) and onEvent receives activity-feed events.
func New(games []*steam.Game, exePath, dllPath string, onEvent func(Event)) *Supervisor {
	s := &Supervisor{workers: make([]*worker, len(games)), onEvent: onEvent}
	for i, g := range games {
		s.workers[i] = &worker{game: g, exePath: exePath, dllPath: dllPath, sup: s}
	}
	return s
}

// Start launches every worker, slightly staggered, and returns immediately.
// Cancel ctx to stop them.
func (s *Supervisor) Start(ctx context.Context) {
	for i, w := range s.workers {
		s.wg.Add(1)
		go func() {
			defer s.wg.Done()
			w.run(ctx, time.Duration(i)*startStagger)
		}()
	}
}

// Wait blocks until every helper has stopped (after ctx is cancelled).
func (s *Supervisor) Wait() { s.wg.Wait() }

// Finished is never closed: through the Steam client, games idle until stopped.
func (s *Supervisor) Finished() <-chan struct{} { return nil }

// Views returns the current status of every game, in the order given to New.
func (s *Supervisor) Views() []View {
	out := make([]View, len(s.workers))
	for i, w := range s.workers {
		out[i] = w.view()
	}
	return out
}

func (s *Supervisor) emit(level EventLevel, text string) {
	s.onEvent(Event{At: time.Now(), Level: level, Text: text})
}

func (s *Supervisor) steamClosed() {
	s.mu.Lock()
	announced := s.closedAnnounced
	s.closedAnnounced = true
	s.mu.Unlock()
	if !announced {
		s.emit(EventWarn, "Steam closed, waiting for it to come back")
	}
}

func (s *Supervisor) steamRestarted(pid uint32) {
	s.mu.Lock()
	announced := s.restartPID == pid
	s.restartPID = pid
	s.mu.Unlock()
	if !announced {
		s.emit(EventWarn, "Steam restarted, reconnecting the games")
	}
}

func (s *Supervisor) steamUp(pid uint32) {
	s.mu.Lock()
	s.closedAnnounced = false
	s.restartPID = pid
	s.mu.Unlock()
}
