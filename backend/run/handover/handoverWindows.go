//go:build windows

package handover

import (
	"errors"
	"os/exec"
	"syscall"
	"time"

	"golang.org/x/sys/windows"
)

// TraySupported reports whether this OS has a system tray for houridle.
const TraySupported = true

const (
	detachedProcess  = 0x00000008 // no console at all: the tray process
	createNewConsole = 0x00000010 // its own console window: the dashboard

	lockName             = `Local\houridle-idle`
	alreadyIdlingMessage = "houridle is already idling. Look for its icon in the tray, by the clock"
)

// tryLock takes a named mutex. It returns nil (and no error) if another
// copy holds it.
func tryLock(key string) (release func(), err error) {
	full := lockName
	if key != "" {
		full += "-" + key
	}
	name, err := windows.UTF16PtrFromString(full)
	if err != nil {
		return nil, err
	}
	h, err := windows.CreateMutex(nil, false, name)
	if errors.Is(err, windows.ERROR_ALREADY_EXISTS) {
		windows.CloseHandle(h)
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return func() { windows.CloseHandle(h) }, nil
}

// LaunchTray starts houridle in the tray, detached from any console.
func LaunchTray(exePath, libPath string, stopAt time.Time, appIDs []uint32) error {
	return start(exePath, Args(true, libPath, stopAt, appIDs), detachedProcess|syscall.CREATE_NEW_PROCESS_GROUP)
}

// LaunchDashboard starts houridle with the dashboard in a new console window.
func LaunchDashboard(exePath, libPath string, stopAt time.Time, appIDs []uint32) error {
	return start(exePath, Args(false, libPath, stopAt, appIDs), createNewConsole)
}

func start(exePath string, args []string, flags uint32) error {
	cmd := exec.Command(exePath, args...)
	cmd.SysProcAttr = &syscall.SysProcAttr{CreationFlags: flags}
	if err := cmd.Start(); err != nil {
		return err
	}
	return cmd.Process.Release()
}
