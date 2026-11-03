//go:build windows

package main

import (
	"context"
	"os"
	"os/signal"
	"syscall"
	"time"

	"houridle/src/handover"
	"houridle/src/idler"
	"houridle/src/steam"
	"houridle/src/tray"
	"houridle/src/ui"
)

const trayNotice = "houridle is idling in the tray, by the clock. Click its icon to open the dashboard or stop."

// idleRun is one idling session, shown in the console or the tray.
type idleRun struct {
	appIDs  []uint32
	games   []*steam.Game
	exePath string
	dllPath string
	lock    *handover.Lock
}

// stopContext is cancelled on Ctrl+C, console close or logoff, or by cancel.
func stopContext() (context.Context, context.CancelFunc) {
	sigCtx, stopSignals := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	ctx, cancel := context.WithCancel(sigCtx)
	return ctx, func() {
		cancel()
		stopSignals()
	}
}

// newSupervisor creates the helpers' supervisor and warns early if Steam is down.
func (r *idleRun) newSupervisor(onEvent func(idler.Event)) *idler.Supervisor {
	sup := idler.New(r.games, r.exePath, r.dllPath, onEvent)
	if !steam.Running() {
		onEvent(idler.Event{At: time.Now(), Level: idler.EventWarn, Text: "Steam isn't running. Start it and sign in; idling begins once it's up."})
	}
	return sup
}

// inConsole idles with the live dashboard until the user stops or sends it
// to the tray. Redirected output gets plain log lines instead.
func (r *idleRun) inConsole() {
	ctx, cancel := stopContext()
	defer cancel()
	start := time.Now()
	if !ui.Styled() {
		r.inPlainConsole(ctx, start)
		return
	}

	dash := ui.NewDashboard(start, version)
	sup := r.newSupervisor(dash.AddEvent)
	dash.Open(sup)
	sup.Start(ctx)

	action := ui.ActionStop
	select {
	case <-ctx.Done():
	case action = <-dash.Actions():
	}
	cancel()
	dash.SetStopping()
	sup.Wait()
	dash.Close()

	if action == ui.ActionTray {
		r.lock.Release()
		r.handOffToTray()
		return
	}
	ui.PrintSummary(sup.Views(), time.Since(start))
}

func (r *idleRun) inPlainConsole(ctx context.Context, start time.Time) {
	sup := r.newSupervisor(ui.PrintEvent)
	ui.PrintIdleHeader(r.games)
	sup.Start(ctx)
	<-ctx.Done()
	ui.Logf("stopping…")
	sup.Wait()
	ui.PrintSummary(sup.Views(), time.Since(start))
}

// inTray idles behind a tray icon until the user picks Stop idling, or Open
// dashboard (which hands over to a new console window).
func (r *idleRun) inTray() {
	ctx, cancel := stopContext()
	defer cancel()

	sup := r.newSupervisor(func(idler.Event) {}) // the menu shows each game's state instead
	sup.Start(ctx)
	choice := tray.Run(sup, time.Now(), ctx.Done())
	cancel()
	sup.Wait()

	if choice == tray.ChoiceOpenDashboard {
		r.lock.Release()
		if err := handover.LaunchDashboard(r.exePath, r.dllPath, r.appIDs); err != nil {
			ui.Fatal(err)
		}
	}
}

// handOffToTray starts a detached tray copy with the same games. The caller
// must not hold the instance lock (the new copy needs it).
func (r *idleRun) handOffToTray() int {
	if err := handover.LaunchTray(r.exePath, r.dllPath, r.appIDs); err != nil {
		return ui.Fatal(err)
	}
	ui.Notice(trayNotice)
	return 0
}
