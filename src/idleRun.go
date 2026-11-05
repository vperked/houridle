//go:build windows || linux

package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"strings"
	"sync"
	"syscall"
	"time"

	"houridle/src/eventlog"
	"houridle/src/handover"
	"houridle/src/idler"
	"houridle/src/steam"
	"houridle/src/steamauth"
	"houridle/src/stoptime"
	"houridle/src/tray"
	"houridle/src/ui"
)

const trayNotice = "houridle is idling in the tray, by the clock. Click its icon to open the dashboard or stop."

// idleRun is one idling session, shown in the console or the tray.
type idleRun struct {
	appIDs  []uint32
	games   []*steam.Game
	exePath string
	libPath string
	lock    *handover.Lock
	log     *eventlog.Log      // houridle.log; nil if it couldn't be opened
	stopAt  time.Time          // automatic stop; zero for none
	session *steamauth.Manager // set when idling through a saved sign-in (houridle start)
}

// sessionEndGrace is how long the tray waits for the helpers to stop when
// Windows is signing out or shutting down, which won't wait for long.
const sessionEndGrace = 3 * time.Second

// stopContext is cancelled on Ctrl+C, console close, logoff or a service
// stop (SIGTERM), the terminal closing (SIGHUP), at the automatic stop time
// (checked against the wall clock, so a suspend doesn't delay it), or by
// calling cancel.
func (r *idleRun) stopContext() (context.Context, context.CancelFunc) {
	sigCtx, stopSignals := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM, syscall.SIGHUP)
	ctx, cancel := stoptime.WithStop(sigCtx, r.stopAt)
	return ctx, func() {
		cancel()
		stopSignals()
	}
}

// reachedStopTime reports whether ctx ended because the stop time came.
func reachedStopTime(ctx context.Context) bool { return stoptime.Reached(ctx) }

// noteStopTime logs and announces that idling ended at the planned time.
func (r *idleRun) noteStopTime() {
	r.log.Printf("info   reached the stop time (%s)", stoptime.Describe(r.stopAt, time.Now()))
	ui.Notice("reached your stop time, " + stoptime.Describe(r.stopAt, time.Now()))
}

// newSupervisor creates the supervisor. Every event also goes to the log.
// Through the Steam client it warns early if Steam isn't running; with a
// saved sign-in it connects to Steam itself instead.
func (r *idleRun) newSupervisor(show func(idler.Event)) idler.Runner {
	onEvent := func(e idler.Event) {
		r.log.Printf("%-5s  %s", levelWord(e.Level), e.Text)
		show(e)
	}
	if r.session != nil {
		return idler.NewSession(r.games, r.session, onEvent)
	}
	sup := idler.New(r.games, r.exePath, r.libPath, onEvent)
	if !steam.Running() {
		onEvent(idler.Event{At: time.Now(), Level: idler.EventWarn, Text: "Steam isn't running. Start it and sign in; idling begins once it's up."})
	}
	return sup
}

// inConsole idles with the live dashboard until the user stops or sends it
// to the tray. Redirected output (a service, a pipe) gets plain lines instead.
func (r *idleRun) inConsole() {
	ctx, cancel := r.stopContext()
	defer cancel()
	start := time.Now()
	if !ui.Styled() {
		r.inPlainConsole(ctx, start)
		return
	}

	dash := ui.NewDashboard(start, version, handover.TraySupported)
	dash.SetStopTime(r.stopAt)
	sup := r.newSupervisor(dash.AddEvent)
	r.logStart("in the console")
	dash.Open(sup)
	sup.Start(ctx)

	action := ui.ActionStop
	select {
	case <-ctx.Done():
	case action = <-dash.Actions():
	}
	timeUp := reachedStopTime(ctx)
	cancel()
	dash.SetStopping()
	sup.Wait()
	dash.Close()

	if action == ui.ActionTray {
		r.log.Printf("info   moving to the tray")
		r.lock.Release()
		r.handOffToTray()
		return
	}
	if timeUp {
		r.noteStopTime()
	}
	r.logStop(sup.Views(), time.Since(start))
	ui.PrintSummary(sup.Views(), time.Since(start))
}

func (r *idleRun) inPlainConsole(ctx context.Context, start time.Time) {
	sup := r.newSupervisor(ui.PrintEvent)
	r.logStart("without a dashboard")
	ui.PrintIdleHeader(r.games)
	if !r.stopAt.IsZero() {
		ui.Logf("will stop at %s", stoptime.Describe(r.stopAt, time.Now()))
	}
	sup.Start(ctx)
	<-ctx.Done()
	if reachedStopTime(ctx) {
		r.noteStopTime()
	}
	ui.Logf("stopping…")
	sup.Wait()
	r.logStop(sup.Views(), time.Since(start))
	ui.PrintSummary(sup.Views(), time.Since(start))
}

// inTray idles behind a tray icon until the user picks Stop idling, or Open
// dashboard (which hands over to a new console window).
func (r *idleRun) inTray() {
	ctx, cancel := r.stopContext()
	defer cancel()
	start := time.Now()

	sup := r.newSupervisor(func(idler.Event) {}) // the menu shows each game's state
	r.logStart("in the tray")
	sup.Start(ctx)

	var stopLogged sync.Once
	logStop := func() { stopLogged.Do(func() { r.logStop(sup.Views(), time.Since(start)) }) }
	sessionEnding := func() {
		// Windows ends this process as soon as this returns: stop the
		// helpers and write the stop line now.
		r.log.Printf("info   Windows is signing out or shutting down")
		cancel()
		waitAtMost(sup.Wait, sessionEndGrace)
		logStop()
	}
	choice := tray.Run(sup, start, r.stopAt, ctx.Done(), sessionEnding)
	timeUp := reachedStopTime(ctx)
	cancel()
	sup.Wait()

	if choice == tray.ChoiceOpenDashboard {
		r.log.Printf("info   opening the dashboard")
		r.lock.Release()
		if err := handover.LaunchDashboard(r.exePath, r.libPath, r.stopAt, r.appIDs); err != nil {
			ui.Fatal(err)
		}
		return
	}
	if timeUp {
		r.log.Printf("info   reached the stop time (%s)", stoptime.Describe(r.stopAt, time.Now()))
	}
	logStop()
}

// waitAtMost runs wait, giving up on it after d.
func waitAtMost(wait func(), d time.Duration) {
	done := make(chan struct{})
	go func() {
		wait()
		close(done)
	}()
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-done:
	case <-timer.C:
	}
}

// handOffToTray starts a detached tray copy with the same games and stop
// time. The caller must not hold the instance lock (the new copy needs it).
func (r *idleRun) handOffToTray() int {
	// Check the lock first, so "already idling" shows up here in the console
	// rather than as a message box from the tray copy after we've said it's fine.
	probe, err := handover.TakeLock()
	if err != nil {
		return ui.Fatal(err)
	}
	probe.Release()
	if err := handover.LaunchTray(r.exePath, r.libPath, r.stopAt, r.appIDs); err != nil {
		return ui.Fatal(err)
	}
	ui.Notice(trayNotice)
	return 0
}

// ---------- log lines ----------

func (r *idleRun) logStart(where string) {
	names := make([]string, len(r.games))
	for i, g := range r.games {
		names[i] = fmt.Sprintf("%s (%d)", g.Name, g.AppID)
	}
	r.log.Printf("start  idling %d game(s) %s: %s", len(r.games), where, strings.Join(names, ", "))
}

func (r *idleRun) logStop(views []idler.View, session time.Duration) {
	var total time.Duration
	for _, v := range views {
		total += v.Idled
	}
	r.log.Printf("stop   after %s, +%.2f game-hours", session.Round(time.Second), total.Hours())
}

func levelWord(level idler.EventLevel) string {
	switch level {
	case idler.EventGood:
		return "ok"
	case idler.EventWarn:
		return "warn"
	case idler.EventBad:
		return "error"
	default:
		return "info"
	}
}
