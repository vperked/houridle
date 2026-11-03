//go:build windows

package ui

import "fmt"

// PrintUsage is the -h screen.
func PrintUsage(version string) {
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
	cmd("houridle -tray", "idle in the system tray, by the clock")
	cmd("houridle -achievements", "unlock or lock achievements (pick a game)")
	cmd("houridle -achievements 730", "same, for this app ID")
	cmd("houridle -dll <path>", "use a specific steam_api64.dll")
	fmt.Println()
	fmt.Println("  " + muted("Steam must be running and signed in. While idling: t sends it to the tray, q stops."))
	fmt.Println()
}
