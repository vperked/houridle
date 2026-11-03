package idler

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"houridle/src/steam"
	"houridle/src/steamworks"
)

const (
	startStagger    = 150 * time.Millisecond // gap between games starting
	steamPollEvery  = 10 * time.Second       // while waiting for Steam to start
	retryAfterFail  = time.Minute            // after Steam refused the game
	retryAfterCrash = 5 * time.Second        // after a helper exited on its own
)

// Short labels for failure codes, shown in the game's status column.
var failureLabels = map[string]string{
	steamworks.CodeNotOwned: "not owned",
	steamworks.CodeNoSteam:  "steam not ready",
	steamworks.CodeOutdated: "steam outdated",
	steamworks.CodeLoad:     "dll failed",
	steamworks.CodeRefused:  "refused",
	failTimeout:             "timed out",
	failNoReply:             "no reply",
	failStart:               "can't start",
}

// worker keeps one game's helper running, retrying when Steam is closed or
// the helper fails.
type worker struct {
	game    *steam.Game
	exePath string
	dllPath string
	emit    func(EventLevel, string)

	mu      sync.Mutex // guards the fields below
	state   State
	since   time.Time     // when the current idling stretch began
	banked  time.Duration // idle time from earlier stretches
	note    string        // short failure label
	lastErr string        // last failure message, to avoid repeating it
}

// run is the worker's loop: wait for Steam, run the helper, retry. It
// returns when ctx is cancelled.
func (w *worker) run(ctx context.Context, initialDelay time.Duration) {
	defer w.stop()
	if !sleepCtx(ctx, initialDelay) {
		return
	}
	for {
		var retryAfter time.Duration
		if steam.Running() {
			retryAfter = w.runHelper(ctx)
		} else {
			retryAfter = w.waitForSteam()
		}
		if !sleepCtx(ctx, retryAfter) {
			return
		}
	}
}

func (w *worker) waitForSteam() time.Duration {
	if w.view().State == StateIdling {
		w.emit(EventWarn, "Steam closed, waiting for it to come back")
	}
	w.setState(StateWaiting, "")
	return steamPollEvery
}

// runHelper runs one helper until it stops or ctx ends, and returns how long
// to wait before trying again.
func (w *worker) runHelper(ctx context.Context) (retryAfter time.Duration) {
	w.setState(StateStarting, "")
	h, err := startHelper(w.exePath, w.dllPath, w.game.AppID)
	if err != nil {
		w.reportFailure(failStart, "couldn't start helper: "+err.Error())
		return retryAfterFail
	}

	if err := h.waitReady(ctx); err != nil {
		h.kill()
		if ctx.Err() != nil {
			return 0 // shutting down
		}
		var failure *helperFailure
		if errors.As(err, &failure) {
			w.reportFailure(failure.code, failure.msg)
		}
		return retryAfterFail
	}
	w.markIdling()

	select {
	case <-ctx.Done():
		h.stop()
		return 0
	case <-h.exited:
		w.setState(StateStarting, "")
		w.emit(EventWarn, w.game.Name+" helper stopped unexpectedly, restarting")
		return retryAfterCrash
	}
}

func (w *worker) markIdling() {
	w.mu.Lock()
	w.lastErr = ""
	w.mu.Unlock()
	w.setState(StateIdling, "")
	w.emit(EventGood, w.game.Name+" is idling")
}

// reportFailure shows a short label on the game's row and logs the full
// message once per distinct failure, so a refused game doesn't flood the feed.
func (w *worker) reportFailure(code, msg string) {
	label, ok := failureLabels[code]
	if !ok {
		label = "failed"
	}
	w.mu.Lock()
	repeat := w.lastErr == msg
	w.lastErr = msg
	w.mu.Unlock()

	w.setState(StateFailing, label)
	if !repeat {
		w.emit(EventBad, fmt.Sprintf("%s: %s. Retrying every minute.", w.game.Name, msg))
	}
}

// ---------- state ----------

// setState moves to a new state, keeping the idle-time total up to date.
func (w *worker) setState(state State, note string) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.state == StateIdling && state != StateIdling {
		w.banked += time.Since(w.since)
	}
	if state == StateIdling && w.state != StateIdling {
		w.since = time.Now()
	}
	w.state = state
	w.note = note
}

func (w *worker) view() View {
	w.mu.Lock()
	defer w.mu.Unlock()
	v := View{Game: w.game, State: w.state, Idled: w.banked, Note: w.note}
	if w.state == StateIdling {
		v.Idled += time.Since(w.since)
	}
	return v
}

// stop marks the worker stopped, keeping its failure note for the summary.
func (w *worker) stop() { w.setState(StateStopped, w.view().Note) }

// sleepCtx waits for d, returning false early if ctx is cancelled.
func sleepCtx(ctx context.Context, d time.Duration) bool {
	if d <= 0 {
		return ctx.Err() == nil
	}
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-timer.C:
		return true
	}
}
