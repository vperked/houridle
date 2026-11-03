package ui

import "fmt"

// PrintUsage is the -h screen. Tray commands are listed only where there's
// a tray (Windows).
func PrintUsage(version string, trayAvailable bool) {
	Init()
	cmd := func(c, desc string) {
		fmt.Println("    " + accent(fit(c, 28)) + muted(desc))
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
	cmd("houridle status", "show your sign-in and connection")
	cmd("houridle account", "show your Steam username and SteamID64")
	cmd("houridle logout", "forget the saved sign-in")
	cmd("houridle -stop 3h | 23:30", "stop by itself after 3 hours, or at 23:30")
	cmd("houridle -achievements", "unlock or lock achievements (pick a game)")
	cmd("houridle -achievements 730", "same, for this app ID")
	cmd("houridle -dll <path>", "use a specific Steam API library")
	fmt.Println()
	hint := "Steam must be running and signed in (not with start). While idling, q stops."
	if trayAvailable {
		hint = "Steam must be running and signed in (not with start). While idling: t sends it to the tray, q stops."
	}
	fmt.Println("  " + muted(hint))
	fmt.Println("  " + muted("Activity is logged to houridle.log next to houridle."))
	fmt.Println()
}
