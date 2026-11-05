// Package tray shows houridle as an icon in the notification area while it
// idles, with a menu listing each game's status. Windows only: Linux servers
// have no tray (trayLinux.go).
package tray

import "houridle/backend/idler"

// Choice is what the user picked from the tray menu.
type Choice int

const (
	ChoiceStop Choice = iota
	ChoiceOpenDashboard
)

// ViewSource supplies the per-game status the menu shows.
type ViewSource interface {
	Views() []idler.View
}
