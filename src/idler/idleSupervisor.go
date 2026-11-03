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

	"houridle/src/steam"
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
}

// View is a point-in-time copy of one game's status.
type View struct {
	Game  *steam.Game
	State State
	Idled time.Duration
	Note  string // short reason when failing, e.g. "not owned"
}

// Supervisor runs one worker per game.
type Supervisor struct {
	workers []*worker
	onEvent func(Event)
	wg      sync.WaitGroup
}

// New prepares a supervisor. exePath is houridle itself (re-run as the
// helper) and onEvent receives activity-feed events.
func New(games []*steam.Game, exePath, dllPath string, onEvent func(Event)) *Supervisor {
	s := &Supervisor{workers: make([]*worker, len(games)), onEvent: onEvent}
	for i, g := range games {
		s.workers[i] = &worker{game: g, exePath: exePath, dllPath: dllPath, emit: s.emit}
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
