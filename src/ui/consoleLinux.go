//go:build linux

package ui

import (
	"fmt"
	"os"

	"golang.org/x/term"
)

// enableColors reports whether output is a terminal that understands ANSI
// codes (not a pipe, a log file, or TERM=dumb).
func enableColors() bool {
	return term.IsTerminal(int(os.Stdout.Fd())) && os.Getenv("TERM") != "dumb"
}

func stdinIsTerminal() bool { return term.IsTerminal(int(os.Stdin.Fd())) }

// termSize returns the terminal size in cells.
func termSize() (width, height int) {
	w, h, err := term.GetSize(int(os.Stdout.Fd()))
	if err != nil {
		return 80, 24
	}
	return w, h
}

// enableRawInput switches the terminal to one-key-at-a-time input. It
// returns a func that restores it.
func enableRawInput() (restore func(), err error) {
	fd := int(os.Stdin.Fd())
	old, err := term.MakeRaw(fd)
	if err != nil {
		return nil, err
	}
	return func() { term.Restore(fd, old) }, nil
}

// launchedByDoubleClick is a Windows idea; on Linux houridle always runs in
// a terminal that stays open.
func launchedByDoubleClick() bool { return false }

// HasConsoleWindow is always true on Linux: there's no console-less tray mode.
func HasConsoleWindow() bool { return true }

func showErrorBox(msg string) { fmt.Fprintln(os.Stderr, "houridle: "+msg) }
