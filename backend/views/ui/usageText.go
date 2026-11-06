package ui

import "fmt"

// PrintUsage is the -h screen. Tray commands are listed only where there's
// a tray (Windows).
func PrintUsage(version string, trayAvailable bool) {
	Init()
	cmd := func(c, desc string) {
		fmt.Println("    " + accent(fit(c, 32)) + muted(desc))
	}
	fmt.Println()
	fmt.Println(banner("steam hour idler · v"+version, maxContentWidth))
	fmt.Println()
	fmt.Println("  " + bold("usage"))
	cmd("houridle", "idle the games in config.json (first run lets you pick)")
	cmd("houridle 730 440", "idle these app IDs this run")
	cmd("houridle -list [name]", "show your games with hours and app IDs")
	cmd("houridle -setup", "pick the games to idle")
	if trayAvailable {
		cmd("houridle -tray", "idle in the system tray, by the clock")
		cmd("houridle -autostart on|off", "start in the tray when you sign in")
	}
	cmd("houridle login", "sign in to Steam (asks: QR code or password)")
	cmd("houridle login -qr|-password", "pick the method up front")
	cmd("houridle start [ids]", "idle with that sign-in, no Steam client needed")
	cmd("houridle start -until 100h", "stop each game once it has 100 hours on record")
	cmd("houridle start -cards", "idle the games with card drops left, until they drop")
	cmd("houridle start -status away", "show as online, away, busy, invisible or offline")
	cmd("houridle start -rotate 2h", "over 32 games: how often they take turns (1h)")
	cmd("houridle start -all -until 10h", "idle every game you own with under 10 hours")
	cmd("houridle start -claim", "add Steam's free giveaways (every 12 hours)")
	cmd("houridle start -refundable", "also idle games you could still refund (held back otherwise)")
	cmd("houridle start -craft -events", "craft badges, do sale events while idling")
	cmd("houridle start -achievements", "achievements without the Steam client")
	cmd("houridle status", "show your sign-in and connection")
	cmd("houridle account", "show your Steam username and SteamID64")
	cmd("houridle accounts", "list saved accounts (log in again to add one)")
	cmd("houridle start -account name", "idle another saved account (one houridle each)")
	cmd("houridle account -default name", "make a saved account the default")
	cmd("houridle logout [-account n]", "forget a saved sign-in")
	cmd("houridle notify <url>|off", "alerts to Discord, Slack or ntfy; test it")
	cmd("houridle serve", "run as a control panel: do everything from the page")
	cmd("houridle web [addr|off]", "the page's address and link (open it on your phone)")
	cmd("houridle stats", "hours idled: today, this week, per game")
	cmd("houridle claim [-dry-run]", "add Steam's free giveaways to your account")
	cmd("houridle craft [-dry-run]", "craft badges with full card sets (Steam XP)")
	cmd("houridle events", "sale events: discovery queue and daily sticker")
	cmd("houridle -stop 3h | 23:30", "stop by itself after 3 hours, or at 23:30")
	cmd("houridle -achievements", "unlock or lock achievements (pick a game)")
	cmd("houridle -achievements 730", "same, for this app ID")
	cmd("houridle -dll <path>", "use a specific Steam API library")
	cmd("houridle -notify <url>", "alerts for this run only (or set with notify)")
	cmd("houridle -web :8080", "status page for this run only (or set with web)")
	fmt.Println()
	hint := "Steam must be running and signed in (not with start). While idling, q stops."
	if trayAvailable {
		hint = "Steam must be running and signed in (not with start). While idling: t sends it to the tray, q stops."
	}
	fmt.Println("  " + muted(hint))
	fmt.Println("  " + muted("Activity is logged to houridle.log next to houridle."))
	fmt.Println()
}
