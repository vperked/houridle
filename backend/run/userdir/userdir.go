// Package userdir finds the per-user config and cache folders, like
// os.UserConfigDir and os.UserCacheDir, but also for a program started
// without $HOME: a systemd system service (which sets no HOME unless the
// unit says so) or a container. Then the home folder comes from the user
// database, so a service running as root finds the same sign-ins that
// `houridle login` saved from a root shell (/root/.config/houridle).
package userdir

import (
	"os"
	"os/user"
	"path/filepath"
	"runtime"
)

// Config is the per-user config folder (%AppData% on Windows, ~/.config
// elsewhere).
func Config() (string, error) {
	dir, err := os.UserConfigDir()
	if err == nil {
		return dir, nil
	}
	if home := homeFromUserDB(); home != "" {
		return filepath.Join(home, ".config"), nil
	}
	return "", err
}

// Cache is the per-user cache folder (%LocalAppData% on Windows, ~/.cache
// elsewhere).
func Cache() (string, error) {
	dir, err := os.UserCacheDir()
	if err == nil {
		return dir, nil
	}
	if home := homeFromUserDB(); home != "" {
		return filepath.Join(home, ".cache"), nil
	}
	return "", err
}

// homeFromUserDB is the current user's home folder from the user database
// (/etc/passwd), or "" if there is none. Only on Unix-like systems: Windows
// finds its folders without HOME.
func homeFromUserDB() string {
	if runtime.GOOS == "windows" {
		return ""
	}
	u, err := user.Current()
	if err != nil || u.HomeDir == "" || !filepath.IsAbs(u.HomeDir) {
		return ""
	}
	return u.HomeDir
}
