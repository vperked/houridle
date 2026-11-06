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

	"houridle/backend/client/achievements"
	"houridle/backend/client/steam"
	"houridle/backend/features/history"
	"houridle/backend/online/steamauth"
	"houridle/backend/run/autostart"
	"houridle/backend/run/config"
	"houridle/backend/run/eventlog"
	"houridle/backend/run/handover"
	"houridle/backend/run/idler"
	"houridle/backend/run/stoptime"
	"houridle/backend/views/ui"
)

// version is set at build time for releases (-ldflags "-X main.version=1.6.0").
var version = "1.6.0-dev"

func main() {
	os.Exit(run())
}

// startCommand idles through the Steam sign-in saved by `houridle login`,
// without needing the Steam client.
const startCommand = "start"

func run() int {
	if len(os.Args) > 1 {
		if cmd, ok := commands[os.Args[1]]; ok {
			return cmd(os.Args[2:])
		}
	}
	listFlag := flag.Bool("list", false, "list your games")
	setupFlag := flag.Bool("setup", false, "pick games to idle")
	achievementsFlag := flag.Bool("achievements", false, "unlock or lock a game's achievements")
	trayFlag := flag.Bool("tray", false, "idle in the system tray instead of a window (Windows)")
	autostartFlag := flag.String("autostart", "", `"on" or "off": start in the tray when you sign in (Windows)`)
	stopFlag := flag.String("stop", "", "stop automatically: hours (3), a duration (90m), a time of day (23:30), or never")
	libFlag := flag.String("dll", "", "path to Steam's API library (steam_api64.dll or libsteam_api.so)")
	accountFlag := flag.String("account", "", "with start: which saved Steam account to use (name or SteamID; default: the default account)")
	statusFlag := flag.String("status", "", "with start: how you show to friends while idling: online, away, busy, invisible or offline")
	untilFlag := flag.String("until", "", "with start: stop idling each game once it has this many hours on record (100, 100h, 90m)")
	rotateFlag := flag.String("rotate", "", "with start and more than 32 games: how often games take turns (default 1h)")
	cardsFlag := flag.Bool("cards", false, "with start: idle the games that still have trading card drops, until they've all dropped")
	allFlag := flag.Bool("all", false, "with start: idle every game you own (with -until/-under: those under that many hours); new ones join")
	craftFlag := flag.Bool("craft", false, "with start: craft game badges (Steam XP) whenever a full card set is there")
	eventsFlag := flag.Bool("events", false, "with start: during Steam sales, explore the discovery queue and claim the daily sticker")
	refundableFlag := flag.Bool("refundable", false, "with start: also idle games you could still refund (bought in the last 14 days, under 2 hours played); they're held back otherwise")
	claimFlag := flag.Bool("claim", false, "with start: add Steam's free giveaways (games 100% off to keep) to your account, checking every 12 hours")
	underFlag := flag.String("under", "", "with start -all: same as -until (idle each game until it has this many hours)")
	notifyFlag := flag.String("notify", "", `webhook URL for alerts (Discord, Slack, ntfy...), or "off"`)
	webFlag := flag.String("web", "", `serve a status page you can open on your phone, e.g. ":8080"; "off" for none (houridle web shows its link)`)
	idleHelperFlag := flag.Uint(idler.HelperFlag, 0, "internal: run as the idle helper for this app ID")
	achievementsHelperFlag := flag.Uint(achievements.HelperFlag, 0, "internal: run as the achievements helper for this app ID")
	flag.Usage = func() { ui.PrintUsage(version, handover.TraySupported) }
	flag.CommandLine.Parse(config.FlagsFirst(flag.CommandLine, os.Args[1:]))
	args := flag.Args()
	sessionMode := len(args) > 0 && args[0] == startCommand
	if sessionMode {
		args = args[1:]
	}

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

	cfg, cfgErr := config.Load(appDir)
	if cfgErr != nil {
		ui.Warn(cfgErr.Error())
	}

	var session *steamauth.Manager
	var account steamauth.SavedAccount
	var opts idler.SessionOptions
	if sessionMode {
		if *autostartFlag != "" {
			return ui.Fatal(errors.New("-autostart works through the Steam client; run it without start"))
		}
		var err error
		if session, account, err = checkSession(*accountFlag); err != nil {
			return ui.Fatal(err)
		}
		until := *untilFlag
		if *underFlag != "" {
			if until != "" {
				return ui.Fatal(errors.New("-under is another name for -until; use one"))
			}
			until = *underFlag
		}
		if opts, err = sessionOptions(cfg, *statusFlag, until, *rotateFlag, *cardsFlag, *allFlag); err != nil {
			return ui.Fatal(err)
		}
		opts.Claim, opts.Craft, opts.Events, opts.Refundable = *claimFlag, *craftFlag, *eventsFlag, *refundableFlag
		handover.SessionMode = true
		handover.LockSuffix = account.LockKey()
		handover.ExtraArgs = passOnFlags(map[string]string{"-account": *accountFlag, "-status": *statusFlag, "-until": until, "-rotate": *rotateFlag}, *cardsFlag, *allFlag)
		for name, on := range map[string]bool{"-claim": *claimFlag, "-craft": *craftFlag, "-events": *eventsFlag, "-refundable": *refundableFlag} {
			if on {
				handover.ExtraArgs = append(handover.ExtraArgs, name)
			}
		}
	} else if *accountFlag != "" || *statusFlag != "" || *untilFlag != "" || *underFlag != "" || *rotateFlag != "" || *cardsFlag || *allFlag || *claimFlag || *craftFlag || *eventsFlag || *refundableFlag {
		return ui.Fatal(errors.New("-account, -status, -until, -rotate, -cards, -all, -claim, -craft, -events and -refundable work with a saved sign-in: run houridle login once, then houridle start"))
	}
	notifier, err := newNotifier(cfg, *notifyFlag, account)
	if err != nil {
		return ui.Fatal(err)
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
		ui.PrintGameList(library, strings.Join(args, " "))
		return 0
	case *achievementsFlag && session != nil:
		return runSessionAchievements(session, library, args)
	case *achievementsFlag:
		return runAchievements(appDir, steamDir, *libFlag, library, args)
	case *autostartFlag != "":
		return setAutostart(appDir, steamDir, *libFlag, *autostartFlag)
	}

	var appIDs []uint32
	if opts.Cards || opts.All {
		if len(args) > 0 || *setupFlag {
			return ui.Fatal(errors.New("-cards and -all pick the games themselves; leave out app IDs and -setup"))
		}
	} else {
		pick := gameChoice{appDir: appDir, library: library, args: args, forceSetup: *setupFlag, loadOwned: loadOwned, many: session != nil}
		if session != nil {
			pick.steamID, pick.shared = steamauth.FormatSteamID(account.SteamID), account.Default
		}
		appIDs, err = pick.choose(cfg)
		if errors.Is(err, ui.ErrCancelled) {
			ui.Notice("cancelled, nothing changed")
			return 0
		}
		if err != nil {
			return ui.Fatal(err)
		}
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

	r := &idleRun{appIDs: appIDs, exePath: exePath, libPath: libPath, stopAt: stopAt, session: session, opts: opts, notify: notifier}
	r.alerts.Set(notifier)
	if r.webAddr, r.webKey, err = webSettings(appDir, &cfg, *webFlag); err != nil {
		return ui.Fatal(err)
	}
	if *webFlag != "" { // the address isn't secret (the key stays in config.json)
		handover.ExtraArgs = append(handover.ExtraArgs, "-web", *webFlag)
	}
	if session != nil {
		r.account = account.Label()
	}
	if *trayFlag && !inTrayProcess {
		// -tray from a console: hand off to a detached copy, give the console back.
		return r.handOffToTray()
	}

	if r.lock, err = handover.TakeLock(); err != nil {
		return ui.Fatal(err)
	}
	defer r.lock.Release()
	if r.log, err = eventlog.OpenNamed(appDir, logName(account)); err != nil {
		ui.Warn("couldn't open the log, idling without it: " + err.Error())
	}
	defer r.log.Close()
	if r.history, err = history.Open(filepath.Join(appDir, historyName(account))); err != nil {
		r.log.Printf("warn   hours won't be added to the history: %v", err) // idling still works
	}
	defer notifier.Flush()
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
func checkSession(selector string) (*steamauth.Manager, steamauth.SavedAccount, error) {
	mgr, acc, err := steamauth.NewManagerFor(selector)
	if err != nil {
		return nil, acc, err
	}
	sess, err := mgr.Current()
	if err != nil {
		return nil, acc, err
	}
	ui.Notice("signed in as " + describe(sess))
	return mgr, acc, nil
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

// gameChoice is how to decide which games to idle.
type gameChoice struct {
	appDir     string
	library    map[uint32]*steam.Game
	args       []string
	forceSetup bool
	loadOwned  func() error // with a saved sign-in: fill library from the Steam account
	many       bool         // more than MaxGames is fine (signed-in idling takes turns)
	steamID    string       // the signed-in account's SteamID, "" for the Steam client
	shared     bool         // the account uses config.json's shared game list
}

// choose returns the app IDs to idle: from the command line, config.json,
// or the picker (whose result is saved).
func (c gameChoice) choose(cfg config.Config) ([]uint32, error) {
	var appIDs []uint32
	if len(c.args) > 0 {
		var err error
		if appIDs, err = config.ParseAppIDs(c.args); err != nil {
			return nil, err
		}
	}
	shared := c.shared || c.steamID == ""
	saved := cfg.GamesFor(c.steamID, shared)
	if len(appIDs) == 0 && !c.forceSetup {
		appIDs = saved
	}
	if len(appIDs) == 0 || c.forceSetup {
		if !ui.CanPrompt() {
			// e.g. started by Windows at sign-in, or by systemd: nobody to ask.
			return nil, errors.New("no games to idle yet: run houridle -setup in a terminal first, or pass app IDs")
		}
		if len(c.library) == 0 && c.loadOwned != nil {
			if ui.ConfirmYes("No Steam library on this computer. Load the games your Steam account owns?") {
				ui.Progress("Loading your games from Steam")
				if err := c.loadOwned(); err != nil {
					return nil, fmt.Errorf("couldn't load your games: %w", err)
				}
			}
		}
		if len(c.library) == 0 {
			return nil, errors.New("no games to pick from on this computer: pass app IDs (houridle start 730 440), or install Steam here to choose from your library")
		}
		preselected := saved
		if len(appIDs) > 0 {
			preselected = appIDs
		}
		picked, err := ui.PickGames(c.library, preselected)
		if err != nil {
			return nil, err
		}
		appIDs = picked
		cfg.SetGamesFor(c.steamID, shared, picked)
		if err := config.Save(c.appDir, cfg); err != nil {
			ui.Warn("couldn't save config.json: " + err.Error())
		}
	}

	appIDs = config.Dedupe(appIDs)
	if len(appIDs) > idler.MaxGames {
		if c.many {
			ui.Notice(fmt.Sprintf("Steam tracks %d games at once; your %d games will take turns", idler.MaxGames, len(appIDs)))
		} else {
			ui.Warn(fmt.Sprintf("Steam tracks at most %d games at once, idling the first %d (with houridle start, more take turns)", idler.MaxGames, idler.MaxGames))
			appIDs = appIDs[:idler.MaxGames]
		}
	}
	return appIDs, nil
}
