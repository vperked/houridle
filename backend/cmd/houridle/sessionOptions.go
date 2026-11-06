//go:build windows || linux

package main

import (
	"errors"
	"fmt"
	"os"
	"strings"
	"time"

	"houridle/backend/features/notify"
	"houridle/backend/online/steamauth"
	"houridle/backend/online/steamcm"
	"houridle/backend/run/config"
	"houridle/backend/run/idler"
	"houridle/backend/run/stoptime"
)

// sessionOptions turns the signed-in idling flags (falling back to
// config.json) into options for the supervisor.
func sessionOptions(cfg config.Config, status, until, rotate string, cards, all bool) (idler.SessionOptions, error) {
	var opts idler.SessionOptions
	if status == "" {
		status = cfg.Status
	}
	var err error
	if opts.Status, err = steamcm.ParseStatus(status); err != nil {
		return opts, err
	}
	if until != "" {
		if opts.Target, err = stoptime.ParseHours(until); err != nil {
			return opts, fmt.Errorf("-until: %w", err)
		}
	}
	if rotate != "" {
		if opts.Rotate, err = stoptime.ParseHours(rotate); err != nil {
			return opts, fmt.Errorf("-rotate: %w", err)
		}
		if opts.Rotate < 10*time.Minute {
			return opts, errors.New("-rotate: take turns no more often than every 10 minutes (Steam counts playtime in chunks)")
		}
	}
	if cards && opts.Target > 0 {
		return opts, errors.New("-cards and -until both decide when a game is done; use one")
	}
	if cards && all {
		return opts, errors.New("-cards and -all both choose the games; use one")
	}
	opts.Cards, opts.All = cards, all
	return opts, nil
}

// passOnFlags rebuilds the flags a handover (console ↔ tray) must keep.
func passOnFlags(values map[string]string, cards, all bool) []string {
	var args []string
	for _, name := range []string{"-account", "-status", "-until", "-rotate"} {
		if v := values[name]; v != "" {
			args = append(args, name, v)
		}
	}
	if cards {
		args = append(args, "-cards")
	}
	if all {
		args = append(args, "-all")
	}
	return args
}

// envNotify carries a -notify webhook to a copy of houridle started by a
// handover (console <-> tray). The URL holds a secret token, so it goes in
// the environment, which only this user can read, not on a command line,
// which any process can.
const envNotify = "HOURIDLE_NOTIFY"

// newNotifier is the webhook for alerts, from -notify, a handover, or
// config.json. With several accounts, messages start with the account's name.
func newNotifier(cfg config.Config, flagURL string, account steamauth.SavedAccount) (*notify.Notifier, error) {
	url := flagURL
	if url != "" {
		os.Setenv(envNotify, url) // inherited by a handover's new copy
	} else if url = os.Getenv(envNotify); url == "" {
		url = cfg.Notify
	}
	if url == "" || strings.EqualFold(url, "off") {
		return nil, nil
	}
	prefix := ""
	if accts, err := steamauth.DefaultAccounts(); err == nil && account.SteamID != 0 && accts.Multiple() {
		prefix = account.Label()
	}
	n, err := notify.New(url, prefix)
	if err != nil {
		return nil, fmt.Errorf("-notify: %w", err)
	}
	return n, nil
}

// logName is houridle.log, or houridle-<account>.log for an account other
// than the default, so accounts idling side by side keep separate logs.
func logName(account steamauth.SavedAccount) string { return account.FileName("houridle", ".log") }

// historyName is history.json, or history-<account>.json (see logName).
func historyName(account steamauth.SavedAccount) string { return account.FileName("history", ".json") }
