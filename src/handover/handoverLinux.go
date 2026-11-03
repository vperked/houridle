//go:build linux

package handover

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"syscall"
	"time"
)

// TraySupported reports whether this OS has a system tray for houridle.
const TraySupported = false

const alreadyIdlingMessage = "houridle is already idling for this user (check tmux, or: systemctl --user status houridle)"

// lockPath is per user, in the runtime folder if there is one.
func lockPath() string {
	dir := os.Getenv("XDG_RUNTIME_DIR")
	if dir == "" {
		dir = os.TempDir()
	}
	return filepath.Join(dir, fmt.Sprintf("houridle-%d.lock", os.Getuid()))
}

// tryLock takes an exclusive flock on the lock file. It returns nil (and no
// error) if another copy holds it. The kernel drops the lock if we crash.
func tryLock() (release func(), err error) {
	f, err := os.OpenFile(lockPath(), os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, err
	}
	err = syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB)
	if errors.Is(err, syscall.EWOULDBLOCK) {
		f.Close()
		return nil, nil
	}
	if err != nil {
		f.Close()
		return nil, err
	}
	return func() {
		syscall.Flock(int(f.Fd()), syscall.LOCK_UN)
		f.Close()
	}, nil
}

// LaunchTray isn't available on Linux.
func LaunchTray(string, string, time.Time, []uint32) error { return ErrNoTray }

// LaunchDashboard isn't available on Linux (there's no tray to come from).
func LaunchDashboard(string, string, time.Time, []uint32) error { return ErrNoTray }
