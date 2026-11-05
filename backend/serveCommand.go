//go:build windows || linux

package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"sync/atomic"
	"syscall"
	"time"

	"houridle/backend/config"
	"houridle/backend/eventlog"
	"houridle/backend/panel"
	"houridle/backend/ui"
	"houridle/backend/web"
)

// runServe is `houridle serve`: houridle keeps running and is controlled
// from its web page (start, stop and change idling, pick games,
// achievements, free games, accounts, alerts). It carries on where it left
// off after a restart, so it's what the server service runs.
func runServe(args []string) int {
	fs := flag.NewFlagSet("serve", flag.ContinueOnError)
	webFlag := fs.String("web", "", `the page's address, e.g. ":8080" (default: the one set with houridle web, else :8080)`)
	account := fs.String("account", "", "the saved account to start with (default: the last one used)")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	ui.Init()
	appDir := config.AppDir()
	cfg, err := config.Load(appDir)
	if err != nil {
		return ui.Fatal(err)
	}
	addrFlag := *webFlag
	if addrFlag == "" && (cfg.Web == "" || cfg.Web == "off") {
		addrFlag = defaultWebAddr
	}
	addr, key, err := webSettings(appDir, &cfg, addrFlag)
	if err != nil {
		return ui.Fatal(err)
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM, syscall.SIGHUP)
	defer stop()
	ctx, quit := context.WithCancel(ctx)
	defer quit()

	log, _ := eventlog.Open(appDir) // serving still works if the log can't be written
	defer log.Close()
	exe, err := os.Executable()
	if err != nil {
		return ui.Fatal(err)
	}
	var restart atomic.Bool
	ctl, err := panel.New(panel.Options{
		AppDir: appDir, ExePath: exe, Version: version, WebAddr: addr,
		Log: log, Print: ui.PrintEvent, Quit: quit, Selector: *account,
		Restart: func() { restart.Store(true); quit() },
	})
	if err != nil {
		return ui.Fatal(err)
	}
	defer ctl.Close()

	srv := &web.Server{Key: key, Status: ctl.Status, Stop: ctl.StopIdling, Control: ctl}
	serveErr := make(chan error, 1)
	go func() { serveErr <- srv.Serve(ctx, addr) }()

	log.Printf("info   control panel on %s", addr)
	ui.Notice(fmt.Sprintf("houridle is serving its control panel on %s. Run houridle web for the link.", addr))
	ctl.Resume()

	select {
	case <-ctx.Done():
	case err := <-serveErr:
		if err != nil {
			return ui.Fatal(fmt.Errorf("couldn't serve the page on %s: %w", addr, err))
		}
	}
	if restart.Load() {
		// After an update: stop idling (it's remembered, so the new
		// houridle resumes it), let the page's port go, then start again.
		ui.Logf("restarting with the update…")
		ctl.Close()
		select {
		case <-serveErr:
		case <-time.After(5 * time.Second):
		}
		log.Printf("info   restarting after an update")
		log.Close()
		if err := restartSelf(exe); err != nil {
			return ui.Fatal(fmt.Errorf("couldn't restart after the update (start houridle again): %w", err))
		}
		return 0
	}
	ui.Logf("stopping…")
	return 0
}
