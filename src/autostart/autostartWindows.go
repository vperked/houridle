//go:build windows

package autostart

import (
	"errors"
	"io/fs"

	"golang.org/x/sys/windows/registry"
)

// Programs listed here start when you sign in to Windows.
const (
	runKey    = `Software\Microsoft\Windows\CurrentVersion\Run`
	valueName = "houridle"
)

// Enable makes Windows run houridle in the tray at sign-in, idling the games
// saved in config.json.
func Enable(exePath, libPath string) error {
	k, _, err := registry.CreateKey(registry.CURRENT_USER, runKey, registry.SET_VALUE)
	if err != nil {
		return err
	}
	defer k.Close()
	return k.SetStringValue(valueName, Command(exePath, libPath))
}

// Disable removes houridle from sign-in. It's fine if it wasn't there.
func Disable() error {
	k, err := registry.OpenKey(registry.CURRENT_USER, runKey, registry.SET_VALUE)
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	defer k.Close()
	if err := k.DeleteValue(valueName); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	return nil
}

// Command is what Windows runs at sign-in.
func Command(exePath, libPath string) string {
	return `"` + exePath + `" -tray -dll "` + libPath + `"`
}

// Supported reports whether houridle can set up autostart on this OS.
const Supported = true
