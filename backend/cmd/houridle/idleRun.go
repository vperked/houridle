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

	"houridle/backend/client/steam"
	"houridle/backend/features/history"
	"houridle/backend/features/notify"
	"houridle/backend/online/steamauth"
	"houridle/backend/run/eventlog"
	"houridle/backend/run/handover"
	"houridle/backend/run/idler"
	"houridle/backend/run/stoptime"
	"houridle/backend/views/tray"
	"houridle/backend/views/ui"
	"houridle/backend/views/web"
)

const (
	recentEvents   = 30 // events the status page shows
	statusPageDays = 84 // days in the status page's History calendar (12 weeks)
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
	opts    idler.SessionOptions
	notify  *notify.Notifier // webhook alerts; nil for none
	history *history.History // hours per game per day; nil if it can't be kept
	webAddr string           // status page address; "" for none
	webKey  string           // the status page's key
	account string           // shown on the status page; "" through the Steam client

	recentMu sync.Mutex
	recent   []idler.Event // the last few events, for the status page

	alerts notify.Tracker // which events become alerts (needs notify set)
}

const sessionEndGrace = 3 * time.Second

// stopContext is cancelled on Ctrl+C, console close, logoff or a service
// stop (SIGTERM), at the automatic stop time, or by calling cancel.
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
		r.alert(e)
		r.remember(e)
		show(e)
	}
	if r.session != nil {
		return idler.NewSessionWith(r.games, r.session, r.opts, onEvent)
	}
	sup := idler.New(r.games, r.exePath, r.libPath, onEvent)
	if !steam.Running() {
		onEvent(idler.Event{At: time.Now(), Level: idler.EventWarn, Text: "Steam isn't running. Start it and sign in; idling begins once it's up."})
	}
	return sup
}

// alert passes an event to the webhook's rules (errors, recoveries, news).
func (r *idleRun) alert(e idler.Event) {
	r.alerts.Event(e.Text, e.Level == idler.EventBad, e.Level == idler.EventGood, e.Alert)
}

// untilFinished cancels ctx when the supervisor has nothing left to idle.
// It reports (after ctx ends) whether that's why idling stopped.
func untilFinished(ctx context.Context, sup idler.Runner, cancel context.CancelFunc) func() bool {
	done := make(chan struct{})
	finished := false
	go func() {
		defer close(done)
		if sup.Finished() == nil {
			return
		}
		select {
		case <-sup.Finished():
			finished = true
			cancel()
		case <-ctx.Done():
		}
	}()
	return func() bool {
		<-done
		return finished
	}
}

// remember keeps the last few events for the status page.
func (r *idleRun) remember(e idler.Event) {
	r.recentMu.Lock()
	defer r.recentMu.Unlock()
	if len(r.recent) == recentEvents {
		r.recent = append(r.recent[:0], r.recent[1:]...)
	}
	r.recent = append(r.recent, e)
}

// serveWeb runs the status page while ctx runs; its Stop button calls stop.
// The page's link (with its key) is never printed or logged here: `houridle
// web` shows it, so the key doesn't end up in a log or a service journal.
func (r *idleRun) serveWeb(ctx context.Context, sup idler.Runner, start time.Time, stop context.CancelFunc, show func(idler.Event)) {
	if r.webAddr == "" {
		return
	}
	srv := &web.Server{
		Key: r.webKey,
		Stop: func() {
			r.log.Printf("info   stopped from the status page")
			stop()
		},
		Status: func() web.Status {
			st := web.Status{Version: version, Account: r.account, Started: start, StopAt: r.stopAt, Views: sup.Views()}
			r.recentMu.Lock()
			st.Events = append([]idler.Event(nil), r.recent...)
			r.recentMu.Unlock()
			if r.history != nil {
				sum := r.history.Summarize(time.Now(), statusPageDays)
				st.Stats = &sum
			}
			st.Run = web.Run{SignedIn: r.session != nil, Cards: r.opts.Cards, All: r.opts.All, Claim: r.opts.Claim,
				Target: r.opts.Target, Rotate: r.opts.Rotate, Status: r.opts.Status}
			return st
		},
	}
	go func() {
		if err := srv.Serve(ctx, r.webAddr); err != nil {
			e := idler.Event{At: time.Now(), Level: idler.EventWarn, Text: "Couldn't start the status page: " + err.Error()}
			r.log.Printf("warn   %s", e.Text)
			r.remember(e)
			show(e)
		}
	}()
	e := idler.Event{At: time.Now(), Level: idler.EventInfo, Text: "Status page on " + r.webAddr + " (houridle web shows its link)"}
	r.log.Printf("info   %s", e.Text)
	r.remember(e)
	show(e)
}

// trackHistory adds what the games idle to history.json while ctx runs.
// The returned func records the last stretch and saves; call it after the
// supervisor has stopped.
func (r *idleRun) trackHistory(ctx context.Context, sup idler.Runner) func() {
	return history.Track(ctx, r.history, func() []history.Sample { return historySamples(sup.Views()) }, func(err error) {
		r.log.Printf("warn   couldn't save %s: %v", r.history.Path(), err)
	})
}

func historySamples(views []idler.View) []history.Sample {
	out := make([]history.Sample, 0, len(views))
	for _, v := range views {
		out = append(out, history.Sample{AppID: v.Game.AppID, Name: v.Game.Name, Idled: v.Idled})
	}
	return out
}

// noteFinished logs, alerts and announces that every game is done.
func (r *idleRun) noteFinished() {
	text := "every game reached its target"
	if r.opts.Cards {
		text = "every card drop has been received"
	}
	r.log.Printf("info   %s; stopped", text)
	r.notify.Alert("Finished: " + text)
	ui.Notice("finished: " + text)
}

// inConsole idles with the live dashboard until the user stops or sends it
// to the tray. Redirected output (a service, a pipe) gets plain lines instead.
func (r *idleRun) inConsole() {
	ctx, cancel := r.stopContext()
	defer cancel()
	start := time.Now()
	if !ui.Styled() {
		r.inPlainConsole(ctx, cancel, start)
		return
	}

	dash := ui.NewDashboard(start, version, handover.TraySupported)
	dash.SetStopTime(r.stopAt)
	sup := r.newSupervisor(dash.AddEvent)
	r.logStart("in the console")
	dash.Open(sup)
	sup.Start(ctx)
	finished := untilFinished(ctx, sup, cancel)
	saveHistory := r.trackHistory(ctx, sup)
	r.serveWeb(ctx, sup, start, cancel, dash.AddEvent)

	action := ui.ActionStop
	select {
	case <-ctx.Done():
	case action = <-dash.Actions():
	}
	timeUp := reachedStopTime(ctx)
	cancel()
	dash.SetStopping()
	sup.Wait()
	saveHistory()
	dash.Close()

	if action == ui.ActionTray {
		r.log.Printf("info   moving to the tray")
		r.lock.Release()
		r.handOffToTray()
		return
	}
	if finished() {
		r.noteFinished()
	} else if timeUp {
		r.noteStopTime()
	}
	r.logStop(sup.Views(), time.Since(start))
	ui.PrintSummary(sup.Views(), time.Since(start))
}

func (r *idleRun) inPlainConsole(ctx context.Context, cancel context.CancelFunc, start time.Time) {
	sup := r.newSupervisor(ui.PrintEvent)
	r.logStart("without a dashboard")
	switch {
	case r.opts.Cards:
		ui.Logf("idling the games with card drops left (reading your badge pages)")
	case r.opts.All:
		ui.Logf("idling every game you own (reading your library from Steam)")
	default:
		ui.PrintIdleHeader(r.games)
	}
	if !r.stopAt.IsZero() {
		ui.Logf("will stop at %s", stoptime.Describe(r.stopAt, time.Now()))
	}
	sup.Start(ctx)
	finished := untilFinished(ctx, sup, cancel)
	saveHistory := r.trackHistory(ctx, sup)
	r.serveWeb(ctx, sup, start, cancel, ui.PrintEvent)
	<-ctx.Done()
	if finished() {
		r.noteFinished()
	} else if reachedStopTime(ctx) {
		r.noteStopTime()
	}
	ui.Logf("stopping…")
	sup.Wait()
	saveHistory()
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
	finished := untilFinished(ctx, sup, cancel)
	saveHistory := sync.OnceFunc(r.trackHistory(ctx, sup))
	r.serveWeb(ctx, sup, start, cancel, func(idler.Event) {})
	var stopLogged sync.Once
	logStop := func() { stopLogged.Do(func() { r.logStop(sup.Views(), time.Since(start)) }) }
	sessionEnding := func() {
		r.log.Printf("info   Windows is signing out or shutting down")
		cancel()
		waitAtMost(sup.Wait, sessionEndGrace)
		saveHistory()
		logStop()
	}
	choice := tray.Run(sup, start, r.stopAt, ctx.Done(), sessionEnding)
	timeUp := reachedStopTime(ctx)
	cancel()
	sup.Wait()
	saveHistory()
	if finished() {
		r.noteFinished()
		timeUp = false
	}

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
	switch {
	case r.opts.Cards:
		r.log.Printf("start  idling the games with card drops left %s", where)
		return
	case r.opts.All:
		r.log.Printf("start  idling every game you own %s", where)
		return
	}
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
