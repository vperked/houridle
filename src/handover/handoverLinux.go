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

// LockPath is the idle lock file: one fixed place per user, so every copy
// agrees on it whatever its environment (a systemd user service, cron, su,
// ssh). That is /run/user/<uid> when it exists and is ours, else the user's
// cache folder. Never /tmp, where another user could create the file first.
func LockPath() (string, error) {
	uid := os.Getuid()
	run := fmt.Sprintf("/run/user/%d", uid)
	if info, err := os.Stat(run); err == nil && info.IsDir() {
		if st, ok := info.Sys().(*syscall.Stat_t); ok && int(st.Uid) == uid {
			return filepath.Join(run, "houridle.lock"), nil
		}
	}
	cache, err := os.UserCacheDir()
	if err != nil {
		return "", fmt.Errorf("can't find a folder for houridle's lock: %w", err)
	}
	dir := filepath.Join(cache, "houridle")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", err
	}
	return filepath.Join(dir, "houridle.lock"), nil
}

// tryLock takes an exclusive flock on the lock file. It returns nil (and no
// error) if another copy holds it. The kernel drops the lock if we crash.
func tryLock() (release func(), err error) {
	path, err := LockPath()
	if err != nil {
		return nil, err
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR|syscall.O_NOFOLLOW, 0o600)
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
