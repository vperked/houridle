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
	retryAfterFail  = time.Minute            // after Steam refused the game
	crashBackoffMin = 5 * time.Second        // after a helper exited on its own...
	crashBackoffMax = 5 * time.Minute        // ...doubling each time, up to this
	stableAfter     = 10 * time.Minute       // a helper up this long resets the backoff
)

// Package hooks, swapped by tests.
var (
	// steamPollEvery is how often to check on Steam, both while waiting for
	// it to start and while idling (to notice it closing or restarting).
	steamPollEvery = 10 * time.Second
	// steamPID returns the running Steam client's PID, or 0.
	steamPID = steam.RunningPID
	// launchHelper starts one game's helper process.
	launchHelper = startHelper
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
	sup     *Supervisor

	mu      sync.Mutex // guards the fields below
	state   State
	since   time.Time     // when the current idling stretch began
	banked  time.Duration // idle time from earlier stretches
	note    string        // short failure label
	lastErr string        // last failure message, to avoid repeating it
	crashes int           // helper exits in a row, for the restart backoff
}

func (w *worker) emit(level EventLevel, text string) { w.sup.emit(level, text) }

// run is the worker's loop: wait for Steam, run the helper, retry. It
// returns when ctx is cancelled.
func (w *worker) run(ctx context.Context, initialDelay time.Duration) {
	defer w.stop()
	if !sleepCtx(ctx, initialDelay) {
		return
	}
	for {
		var retryAfter time.Duration
		if pid := steamPID(); pid != 0 {
			retryAfter = w.runHelper(ctx, pid)
		} else {
			retryAfter = w.waitForSteam()
		}
		if !sleepCtx(ctx, retryAfter) {
			return
		}
	}
}

// waitForSteam notes that Steam isn't running. A worker still marked idling
// got here because Steam closed under it, which the supervisor announces
// once for all games.
func (w *worker) waitForSteam() time.Duration {
	if w.view().State == StateIdling {
		w.sup.steamClosed()
	}
	w.setState(StateWaiting, "")
	return steamPollEvery
}

// runHelper runs one helper until it stops, Steam (process pid) closes or
// restarts, or ctx ends, and returns how long to wait before trying again.
func (w *worker) runHelper(ctx context.Context, pid uint32) (retryAfter time.Duration) {
	w.setState(StateStarting, "")
	h, err := launchHelper(w.exePath, w.dllPath, w.game.AppID)
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
	w.markIdling(pid)
	upSince := time.Now()

	poll := time.NewTicker(steamPollEvery)
	defer poll.Stop()
	for {
		select {
		case <-ctx.Done():
			h.stop()
			return 0
		case <-h.exited:
			if w.steamChanged(pid) {
				return 0
			}
			return w.helperCrashed(time.Since(upSince))
		case <-poll.C:
			if w.steamChanged(pid) {
				h.stop()
				return 0
			}
		}
	}
}

// steamChanged reports whether Steam has closed or restarted since the
// helper started, so the helper has to go. A closed Steam leaves the worker
// marked idling, so run's next waitForSteam reports it.
func (w *worker) steamChanged(pid uint32) bool {
	switch now := steamPID(); now {
	case pid:
		return false
	case 0:
		return true
	default:
		w.setState(StateStarting, "")
		w.sup.steamRestarted(now)
		return true
	}
}

// helperCrashed handles a helper that exited on its own while Steam kept
// running. Restarts back off exponentially, and only the first two in a row
// are announced, so a helper that keeps dying can't flood the feed and log.
func (w *worker) helperCrashed(up time.Duration) time.Duration {
	w.mu.Lock()
	if up >= stableAfter {
		w.crashes = 0
	}
	w.crashes++
	n := w.crashes
	w.mu.Unlock()

	switch n {
	case 1:
		w.setState(StateStarting, "")
		w.emit(EventWarn, w.game.Name+" helper stopped unexpectedly, restarting")
	case 2:
		w.setState(StateFailing, "keeps stopping")
		w.emit(EventWarn, fmt.Sprintf("%s helper keeps stopping; retrying less often (up to every %s)", w.game.Name, crashBackoffMax))
	default:
		w.setState(StateFailing, "keeps stopping")
	}
	return crashBackoff(n)
}

// crashBackoff is the wait before restart number n (1-based) in a row.
func crashBackoff(n int) time.Duration {
	if n < 1 {
		n = 1
	}
	if n > 16 {
		return crashBackoffMax
	}
	return min(crashBackoffMin<<(n-1), crashBackoffMax)
}

// markIdling records a helper as running under Steam process pid.
func (w *worker) markIdling(pid uint32) {
	w.mu.Lock()
	w.lastErr = ""
	quiet := w.crashes > 1 // later restarts in a crash streak go unannounced
	w.mu.Unlock()
	w.setState(StateIdling, "")
	w.sup.steamUp(pid)
	if !quiet {
		w.emit(EventGood, w.game.Name+" is idling")
	}
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
