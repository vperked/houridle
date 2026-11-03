//go:build windows || linux

package main

import (
	"errors"
	"fmt"

	"houridle/src/autostart"
	"houridle/src/config"
	"houridle/src/ui"
)

// setAutostart handles -autostart on|off.
func setAutostart(appDir, steamDir, libFlag, mode string) int {
	switch mode {
	case "on":
		cfg, err := config.Load(appDir)
		if err != nil {
			return ui.Fatal(err)
		}
		if len(cfg.Games) == 0 {
			return ui.Fatal(errors.New("pick your games first with houridle -setup, then turn on -autostart"))
		}
		exePath, libPath, err := helperPaths(appDir, steamDir, libFlag)
		if err != nil {
			return ui.Fatal(err)
		}
		if err := autostart.Enable(exePath, libPath); err != nil {
			return ui.Fatal(err)
		}
		ui.Notice(fmt.Sprintf("houridle will start in the tray when you sign in, idling %d game(s).\n  Turn it off with: houridle -autostart off", len(cfg.Games)))
	case "off":
		if err := autostart.Disable(); err != nil {
			return ui.Fatal(err)
		}
		ui.Notice("houridle won't start when you sign in any more")
	default:
		return ui.Fatal(errors.New(`use "-autostart on" or "-autostart off"`))
	}
	return 0
}
