// Package autostart starts houridle in the tray whenever you sign in to
// Windows. On Linux, use a systemd user service instead (see the README).
//
// The OS-specific part is in autostartWindows.go and autostartLinux.go.
package autostart

import "errors"

// ErrUnsupported is returned where houridle can't set this up itself.
var ErrUnsupported = errors.New("-autostart is for Windows; on Linux, set up the systemd service from the README")
