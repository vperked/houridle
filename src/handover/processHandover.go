//go:build windows

// Package handover moves an idling session between the console window and
// the system tray, and makes sure only one houridle idles at a time.
//
// Switching works by starting a fresh houridle in the other mode with the
// same games, then exiting. The lock is released first so the new copy can
// take it.
package handover

import (
	"errors"
	"os/exec"
	"strconv"
	"syscall"
	"time"

	"golang.org/x/sys/windows"
)

const (
	detachedProcess  = 0x00000008 // no console at all: the tray process
	createNewConsole = 0x00000010 // its own console window: the dashboard

	lockName         = `Local\houridle-idle`
	lockWaitAttempts = 20 // × lockWaitInterval: covers a handover in progress
	lockWaitInterval = 250 * time.Millisecond
)

// Args builds the command line that idles appIDs in tray or console mode.
// Flags come before the app IDs, because Go's flag parsing stops at the
// first plain argument.
func Args(toTray bool, dllPath string, appIDs []uint32) []string {
	var args []string
	if toTray {
		args = append(args, "-tray")
	}
	args = append(args, "-dll", dllPath)
	for _, id := range appIDs {
		args = append(args, strconv.FormatUint(uint64(id), 10))
	}
	return args
}

// LaunchTray starts houridle in the tray, detached from any console.
func LaunchTray(exePath, dllPath string, appIDs []uint32) error {
	return start(exePath, Args(true, dllPath, appIDs), detachedProcess|syscall.CREATE_NEW_PROCESS_GROUP)
}

// LaunchDashboard starts houridle with the dashboard in a new console window.
func LaunchDashboard(exePath, dllPath string, appIDs []uint32) error {
	return start(exePath, Args(false, dllPath, appIDs), createNewConsole)
}

func start(exePath string, args []string, flags uint32) error {
	cmd := exec.Command(exePath, args...)
	cmd.SysProcAttr = &syscall.SysProcAttr{CreationFlags: flags}
	if err := cmd.Start(); err != nil {
		return err
	}
	return cmd.Process.Release()
}

// Lock is held while idling, so a second copy can't idle the same games
// twice (e.g. one in the tray and one started by double-click).
type Lock struct{ handle windows.Handle }

// TakeLock gets the idle lock, waiting a few seconds in case another copy is
// handing over to us. It fails if houridle is already idling.
func TakeLock() (*Lock, error) {
	name, err := windows.UTF16PtrFromString(lockName)
	if err != nil {
		return nil, err
	}
	for attempt := 0; ; attempt++ {
		h, err := windows.CreateMutex(nil, false, name)
		if err == nil {
			return &Lock{handle: h}, nil
		}
		if h != 0 {
			windows.CloseHandle(h)
		}
		if !errors.Is(err, windows.ERROR_ALREADY_EXISTS) {
			return nil, err
		}
		if attempt == lockWaitAttempts {
			return nil, errors.New("houridle is already idling. Look for its icon in the tray, by the clock")
		}
		time.Sleep(lockWaitInterval)
	}
}

// Release lets another copy take the lock. Safe to call more than once.
func (l *Lock) Release() {
	if l.handle != 0 {
		windows.CloseHandle(l.handle)
		l.handle = 0
	}
}
