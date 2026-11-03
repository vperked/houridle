// Package handover moves an idling session between the console window and
// the system tray (Windows), and makes sure only one houridle idles at a
// time (all OSes).
//
// Switching works by starting a fresh houridle in the other mode with the
// same games, then exiting. The lock is released first so the new copy can
// take it.
//
// The lock and the launching differ per OS (handoverWindows.go,
// handoverLinux.go); the rest is shared.
package handover

import (
	"errors"
	"strconv"
	"sync"
	"time"

	"houridle/src/stoptime"
)

const (
	lockWaitAttempts = 20 // × lockWaitInterval: covers a handover in progress
	lockWaitInterval = 250 * time.Millisecond
)

// ErrNoTray is returned on systems without a tray (Linux servers).
var ErrNoTray = errors.New("the tray is only available on Windows; on Linux, run houridle in tmux or as a service (see the README)")

// SessionMode is set when houridle idles through a saved Steam sign-in
// (houridle start) instead of the Steam client, so a handover keeps doing so.
var SessionMode bool

// Idling reports whether a houridle is idling right now, on this computer
// for this user.
func Idling() bool {
	release, err := tryLock()
	if err != nil {
		return false
	}
	if release == nil {
		return true
	}
	release()
	return false
}

// Args builds the command line that idles appIDs in tray or console mode,
// keeping the same automatic stop time. Flags come before the app IDs,
// because Go's flag parsing stops at the first plain argument.
func Args(toTray bool, libPath string, stopAt time.Time, appIDs []uint32) []string {
	var args []string
	if SessionMode {
		args = append(args, "start") // the new copy idles through the saved sign-in too
	}
	if toTray {
		args = append(args, "-tray")
	}
	if !SessionMode {
		args = append(args, "-dll", libPath)
	}
	args = append(args, "-stop", stoptime.Format(stopAt))
	for _, id := range appIDs {
		args = append(args, strconv.FormatUint(uint64(id), 10))
	}
	return args
}

// Lock is held while idling, so a second copy can't idle the same games
// twice (e.g. one in the tray and one started by double-click).
type Lock struct {
	once    sync.Once
	release func()
}

// TakeLock gets the idle lock, waiting a few seconds in case another copy is
// handing over to us. It fails if houridle is already idling.
func TakeLock() (*Lock, error) {
	for attempt := 0; ; attempt++ {
		release, err := tryLock()
		if err != nil {
			return nil, err
		}
		if release != nil {
			return &Lock{release: release}, nil
		}
		if attempt == lockWaitAttempts {
			return nil, errors.New(alreadyIdlingMessage)
		}
		time.Sleep(lockWaitInterval)
	}
}

// Release lets another copy take the lock. Safe to call more than once.
func (l *Lock) Release() { l.once.Do(l.release) }
