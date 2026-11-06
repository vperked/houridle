//go:build windows || linux

package main

import (
	"flag"
	"fmt"
	"strings"

	"houridle/backend/run/config"
	"houridle/backend/views/ui"
	"houridle/backend/views/web"
)

const defaultWebAddr = ":8080"

// webSettings returns the status page's address and key for this run, from
// -web or config.json, making (and saving) a key the first time.
func webSettings(appDir string, cfg *config.Config, flagAddr string) (addr, key string, err error) {
	addr = flagAddr
	if addr == "" {
		addr = cfg.Web
	}
	if addr == "" || strings.EqualFold(addr, "off") {
		return "", "", nil
	}
	if addr, err = web.ParseAddr(addr); err != nil {
		return "", "", fmt.Errorf("-web: %w", err)
	}
	if cfg.WebKey == "" {
		// Re-read before saving: the game picker may have saved config.json
		// since cfg was loaded, and its choice must not be lost.
		fresh, err := config.Load(appDir)
		if err != nil {
			return "", "", err
		}
		if fresh.WebKey == "" {
			fresh.WebKey = web.NewKey()
			if err := config.Save(appDir, fresh); err != nil {
				return "", "", fmt.Errorf("couldn't save the status page's key: %w", err)
			}
		}
		cfg.WebKey = fresh.WebKey
	}
	return addr, cfg.WebKey, nil
}

// runWeb is `houridle web`: turn the status page on or off and show its link.
//
//	houridle web            show the link (turns it on at :8080 if it's off)
//	houridle web :9000      serve it on another address
//	houridle web off        stop serving it
//	houridle web -new-key   make a new link (the old one stops working)
func runWeb(args []string) int {
	fs := flag.NewFlagSet("web", flag.ContinueOnError)
	newKey := fs.Bool("new-key", false, "make a new key, so old links stop working")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	ui.Init()
	appDir := config.AppDir()
	cfg, err := config.Load(appDir)
	if err != nil {
		return ui.Fatal(err)
	}
	switch {
	case fs.NArg() > 1:
		return ui.Fatal(fmt.Errorf("pass one address (like :8080), or off"))
	case fs.NArg() == 1 && strings.EqualFold(fs.Arg(0), "off"):
		cfg.Web = ""
		if err := config.Save(appDir, cfg); err != nil {
			return ui.Fatal(err)
		}
		ui.Success("The status page is off (from the next start)")
		return 0
	case fs.NArg() == 1:
		if cfg.Web, err = web.ParseAddr(fs.Arg(0)); err != nil {
			return ui.Fatal(err)
		}
	case cfg.Web == "":
		cfg.Web = defaultWebAddr
	}
	if *newKey || cfg.WebKey == "" {
		cfg.WebKey = web.NewKey()
	}
	if err := config.Save(appDir, cfg); err != nil {
		return ui.Fatal(err)
	}

	fmt.Println()
	ui.Info("Open on your phone (same network as this computer):")
	for _, link := range web.Links(cfg.Web, cfg.WebKey) {
		fmt.Println("    " + link)
	}
	fmt.Println()
	ui.Notice("The link includes the page's key: anyone with it can see and stop idling. Make a new one with houridle web -new-key.")
	ui.Notice("It's plain HTTP: use it at home or over a VPN (e.g. Tailscale), not open to the internet.")
	ui.Notice("A running houridle picks up changes when it next starts.")
	return 0
}
