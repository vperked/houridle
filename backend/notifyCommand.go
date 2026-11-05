//go:build windows || linux

package main

import (
	"context"
	"fmt"
	"strings"
	"time"

	"houridle/backend/config"
	"houridle/backend/notify"
	"houridle/backend/ui"
)

// runNotify is `houridle notify`: save a webhook for alerts and test it.
//
//	houridle notify https://...   save it and send a test message
//	houridle notify               send a test to the saved one
//	houridle notify off           stop sending alerts
func runNotify(args []string) int {
	ui.Init()
	appDir := config.AppDir()
	cfg, err := config.Load(appDir)
	if err != nil {
		return ui.Fatal(err)
	}
	switch {
	case len(args) > 1:
		return ui.Fatal(fmt.Errorf("pass one webhook URL, or off"))
	case len(args) == 1 && strings.EqualFold(args[0], "off"):
		cfg.Notify = ""
		if err := config.Save(appDir, cfg); err != nil {
			return ui.Fatal(err)
		}
		ui.Success("Alerts are off")
		return 0
	case len(args) == 1:
		if _, err := notify.New(args[0], ""); err != nil {
			return ui.Fatal(err)
		}
		cfg.Notify = args[0]
	case cfg.Notify == "":
		ui.Notice("no webhook saved. Add one: houridle notify https://discord.com/api/webhooks/... (or Slack, or ntfy: https://ntfy.sh/your-topic)")
		return 0
	}

	n, err := notify.New(cfg.Notify, "")
	if err != nil {
		return ui.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	ui.Progress("Sending a test message")
	if err := n.Send(ctx, "test message: alerts work. You'll hear from houridle if signing in fails, idling stops working, or every game is done."); err != nil {
		return ui.Fatal(fmt.Errorf("%w; nothing was saved", err))
	}
	if len(args) == 1 {
		if err := config.Save(appDir, cfg); err != nil {
			return ui.Fatal(err)
		}
		ui.Success(fmt.Sprintf("Sent a test (%s format) and saved the webhook in config.json", n.Kind()))
	} else {
		ui.Success("Sent a test message")
	}
	return 0
}
