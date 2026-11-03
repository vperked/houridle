//go:build windows

// houridle: a Steam hour idler and achievement unlocker.
//
// It starts small hidden helper processes that load Valve's steam_api64.dll
// as a given game, so your running Steam client treats them as that game.
// No login, no password: it uses the Steam client you're already signed in to.
package main

import (
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"houridle/src/achievements"
	"houridle/src/config"
	"houridle/src/handover"
	"houridle/src/idler"
	"houridle/src/steam"
	"houridle/src/ui"
)

const version = "1.2.0"

func main() {
	os.Exit(run())
}

func run() int {
	listFlag := flag.Bool("list", false, "list your games")
	setupFlag := flag.Bool("setup", false, "pick games to idle")
	achievementsFlag := flag.Bool("achievements", false, "unlock or lock a game's achievements")
	trayFlag := flag.Bool("tray", false, "idle in the system tray instead of a window")
	dllFlag := flag.String("dll", "", "path to steam_api64.dll")
	idleHelperFlag := flag.Uint(idler.HelperFlag, 0, "internal: run as the idle helper for this app ID")
	achievementsHelperFlag := flag.Uint(achievements.HelperFlag, 0, "internal: run as the achievements helper for this app ID")
	flag.Usage = func() { ui.PrintUsage(version) }
	flag.Parse()

	// Helper processes started by houridle itself.
	switch {
	case *idleHelperFlag != 0:
		return idler.RunHelper(uint32(*idleHelperFlag), *dllFlag)
	case *achievementsHelperFlag != 0:
		return achievements.RunHelper(uint32(*achievementsHelperFlag), *dllFlag)
	}

	ui.Init()
	inTrayProcess := *trayFlag && !ui.HasConsoleWindow()
	if inTrayProcess {
		ui.UseMessageBoxForErrors() // the tray process has nowhere to print
	}
	appDir := config.AppDir()

	steamDir, err := steam.Path()
	if err != nil {
		return ui.Fatal(err)
	}
	library := steam.LocalGames(steamDir)

	switch {
	case *listFlag:
		ui.PrintGameList(library, strings.Join(flag.Args(), " "))
		return 0
	case *achievementsFlag:
		return runAchievements(appDir, steamDir, *dllFlag, library, flag.Args())
	}

	appIDs, err := chooseGames(appDir, library, flag.Args(), *setupFlag)
	if errors.Is(err, ui.ErrCancelled) {
		ui.Notice("cancelled, nothing changed")
		return 0
	}
	if err != nil {
		return ui.Fatal(err)
	}
	exePath, dllPath, err := helperPaths(appDir, steamDir, *dllFlag)
	if err != nil {
		return ui.Fatal(err)
	}

	r := &idleRun{appIDs: appIDs, exePath: exePath, dllPath: dllPath}
	if *trayFlag && !inTrayProcess {
		// -tray from a console: hand off to a detached copy, give the console back.
		return r.handOffToTray()
	}

	if r.lock, err = handover.TakeLock(); err != nil {
		return ui.Fatal(err)
	}
	defer r.lock.Release()
	r.games = steam.ResolveGames(appIDs, library)
	if inTrayProcess {
		r.inTray()
	} else {
		r.inConsole()
	}
	return 0
}

// helperPaths returns houridle's own path (re-run as helpers) and the DLL to load.
func helperPaths(appDir, steamDir, dllFlag string) (exePath, dllPath string, err error) {
	dllPath = dllFlag
	if dllPath == "" {
		if dllPath, err = steam.FindAPIDLL(appDir, steamDir); err != nil {
			return "", "", err
		}
	}
	if dllPath, err = filepath.Abs(dllPath); err != nil {
		return "", "", err
	}
	exePath, err = os.Executable()
	return exePath, dllPath, err
}

// chooseGames returns the app IDs to idle: from the command line, config.json,
// or the picker (whose result is saved).
func chooseGames(appDir string, library map[uint32]*steam.Game, args []string, forceSetup bool) ([]uint32, error) {
	var appIDs []uint32
	if len(args) > 0 {
		var err error
		if appIDs, err = config.ParseAppIDs(args); err != nil {
			return nil, err
		}
	}

	cfg, err := config.Load(appDir)
	if err != nil {
		ui.Warn(err.Error())
	}
	if len(appIDs) == 0 && !forceSetup {
		appIDs = cfg.Games
	}
	if len(appIDs) == 0 || forceSetup {
		picked, err := ui.PickGames(library, cfg.Games)
		if err != nil {
			return nil, err
		}
		appIDs = picked
		cfg.Games = picked
		if err := config.Save(appDir, cfg); err != nil {
			ui.Warn("couldn't save config.json: " + err.Error())
		}
	}

	appIDs = config.Dedupe(appIDs)
	if len(appIDs) > idler.MaxGames {
		ui.Warn(fmt.Sprintf("Steam tracks at most %d games at once, idling the first %d", idler.MaxGames, idler.MaxGames))
		appIDs = appIDs[:idler.MaxGames]
	}
	return appIDs, nil
}
