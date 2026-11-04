//go:build windows || linux

// houridle: a Steam hour idler and achievement unlocker for Windows and Linux.
//
// It starts small hidden helper processes that load Valve's Steam API
// library as a given game, so your running Steam client treats them as that
// game. By default there is no login: it uses the Steam client you're signed
// in to. `houridle login` saves a Steam sign-in instead, and `houridle start`
// then idles over the network with it, no Steam client needed.
package main

import (
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"houridle/src/achievements"
	"houridle/src/autostart"
	"houridle/src/config"
	"houridle/src/eventlog"
	"houridle/src/handover"
	"houridle/src/idler"
	"houridle/src/steam"
	"houridle/src/steamauth"
	"houridle/src/stoptime"
	"houridle/src/ui"
)

const version = "1.4.4"

func main() {
	os.Exit(run())
}

// startCommand idles through the Steam sign-in saved by `houridle login`,
// without needing the Steam client.
const startCommand = "start"

func run() int {
	sessionMode := false
	if len(os.Args) > 1 {
		if cmd, ok := commands[os.Args[1]]; ok {
			return cmd(os.Args[2:])
		}
		if os.Args[1] == startCommand {
			sessionMode = true
			os.Args = append(os.Args[:1:1], os.Args[2:]...)
		}
	}
	listFlag := flag.Bool("list", false, "list your games")
	setupFlag := flag.Bool("setup", false, "pick games to idle")
	achievementsFlag := flag.Bool("achievements", false, "unlock or lock a game's achievements")
	trayFlag := flag.Bool("tray", false, "idle in the system tray instead of a window (Windows)")
	autostartFlag := flag.String("autostart", "", `"on" or "off": start in the tray when you sign in (Windows)`)
	stopFlag := flag.String("stop", "", "stop automatically: hours (3), a duration (90m), a time of day (23:30), or never")
	libFlag := flag.String("dll", "", "path to Steam's API library (steam_api64.dll or libsteam_api.so)")
	idleHelperFlag := flag.Uint(idler.HelperFlag, 0, "internal: run as the idle helper for this app ID")
	achievementsHelperFlag := flag.Uint(achievements.HelperFlag, 0, "internal: run as the achievements helper for this app ID")
	flag.Usage = func() { ui.PrintUsage(version, handover.TraySupported) }
	flag.Parse()

	// Helper processes started by houridle itself.
	switch {
	case *idleHelperFlag != 0:
		return idler.RunHelper(uint32(*idleHelperFlag), *libFlag)
	case *achievementsHelperFlag != 0:
		return achievements.RunHelper(uint32(*achievementsHelperFlag), *libFlag)
	}

	ui.Init()
	if *trayFlag && !handover.TraySupported {
		return ui.Fatal(handover.ErrNoTray)
	}
	if *autostartFlag != "" && !autostart.Supported {
		return ui.Fatal(autostart.ErrUnsupported)
	}
	inTrayProcess := *trayFlag && !ui.HasConsoleWindow()
	if inTrayProcess {
		ui.UseMessageBoxForErrors() // the tray process has nowhere to print
	}
	appDir := config.AppDir()

	var session *steamauth.Manager
	if sessionMode {
		if *achievementsFlag || *autostartFlag != "" {
			return ui.Fatal(errors.New("-achievements and -autostart work through the Steam client; run them without start"))
		}
		var err error
		if session, err = checkSession(); err != nil {
			return ui.Fatal(err)
		}
		handover.SessionMode = true
	}

	steamDir, err := steam.Path()
	if err != nil && session == nil {
		return ui.Fatal(err) // idling through the Steam client needs it
	}
	library := map[uint32]*steam.Game{} // with a saved sign-in, Steam may not be installed
	if err == nil {
		library = steam.LocalGames(steamDir)
	}

	var loadOwned func() error // with a saved sign-in: fetch the account's games from Steam
	if session != nil {
		loadOwned = func() error { return loadOwnedGames(session, library) }
	}

	switch {
	case *listFlag:
		if loadOwned != nil && len(library) == 0 {
			ui.Progress("Loading the games your Steam account owns")
			if err := loadOwned(); err != nil {
				return ui.Fatal(fmt.Errorf("couldn't load your games: %w", err))
			}
		}
		ui.PrintGameList(library, strings.Join(flag.Args(), " "))
		return 0
	case *achievementsFlag:
		return runAchievements(appDir, steamDir, *libFlag, library, flag.Args())
	case *autostartFlag != "":
		return setAutostart(appDir, steamDir, *libFlag, *autostartFlag)
	}

	appIDs, err := chooseGames(appDir, library, flag.Args(), *setupFlag, loadOwned)
	if errors.Is(err, ui.ErrCancelled) {
		ui.Notice("cancelled, nothing changed")
		return 0
	}
	if err != nil {
		return ui.Fatal(err)
	}
	stopAt, err := chooseStopTime(*stopFlag)
	if err != nil {
		return ui.Fatal(err)
	}
	var exePath, libPath string
	if session != nil {
		exePath, err = os.Executable() // no helper processes or Steam library needed
	} else {
		exePath, libPath, err = helperPaths(appDir, steamDir, *libFlag)
	}
	if err != nil {
		return ui.Fatal(err)
	}

	r := &idleRun{appIDs: appIDs, exePath: exePath, libPath: libPath, stopAt: stopAt, session: session}
	if *trayFlag && !inTrayProcess {
		// -tray from a console: hand off to a detached copy, give the console back.
		return r.handOffToTray()
	}

	if r.lock, err = handover.TakeLock(); err != nil {
		return ui.Fatal(err)
	}
	defer r.lock.Release()
	r.log, _ = eventlog.Open(appDir) // idling still works if the log can't be written
	defer r.log.Close()
	r.games = steam.ResolveGames(appIDs, library)
	if inTrayProcess {
		r.inTray()
	} else {
		r.inConsole()
	}
	return 0
}

// checkSession makes sure there is a saved sign-in worth starting with. It
// only looks at the saved file; whether Steam still accepts it is found out
// by signing in, which idling does and retries.
func checkSession() (*steamauth.Manager, error) {
	mgr, err := steamauth.NewManager()
	if err != nil {
		return nil, err
	}
	sess, err := mgr.Current()
	if err != nil {
		return nil, err
	}
	ui.Notice("signed in as " + describe(sess))
	return mgr, nil
}

// helperPaths returns houridle's own path (re-run as helpers) and the Steam
// API library to load.
func helperPaths(appDir, steamDir, libFlag string) (exePath, libPath string, err error) {
	libPath = libFlag
	if libPath == "" {
		if libPath, err = steam.FindAPILibrary(appDir, steamDir); err != nil {
			return "", "", err
		}
	}
	if libPath, err = filepath.Abs(libPath); err != nil {
		return "", "", err
	}
	exePath, err = os.Executable()
	return exePath, libPath, err
}

// chooseStopTime returns when to stop idling automatically: from -stop, or
// by asking when someone is at the console. Otherwise it never stops.
func chooseStopTime(stopFlag string) (time.Time, error) {
	if stopFlag != "" {
		return stoptime.Parse(stopFlag, time.Now())
	}
	if ui.CanPrompt() && ui.Styled() {
		return ui.AskStopTime(), nil
	}
	return stoptime.Never, nil
}

// chooseGames returns the app IDs to idle: from the command line, config.json,
// or the picker (whose result is saved).
//
// loadOwned, when not nil, can fill library from the Steam account (used when
// this computer has no Steam to read the games from).
func chooseGames(appDir string, library map[uint32]*steam.Game, args []string, forceSetup bool, loadOwned func() error) ([]uint32, error) {
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
		if !ui.CanPrompt() {
			// e.g. started by Windows at sign-in, or by systemd: nobody to ask.
			return nil, errors.New("no games to idle yet: run houridle -setup in a terminal first, or pass app IDs")
		}
		if len(library) == 0 && loadOwned != nil {
			if ui.ConfirmYes("No Steam library on this computer. Load the games your Steam account owns?") {
				ui.Progress("Loading your games from Steam")
				if err := loadOwned(); err != nil {
					return nil, fmt.Errorf("couldn't load your games: %w", err)
				}
			}
		}
		if len(library) == 0 {
			return nil, errors.New("no games to pick from on this computer: pass app IDs (houridle start 730 440), or install Steam here to choose from your library")
		}
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
