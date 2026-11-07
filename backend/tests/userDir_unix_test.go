//go:build !windows

package tests

import (
	"os/user"
	"path/filepath"
	"testing"

	"houridle/backend/online/steamauth"
	"houridle/backend/run/userdir"
)

// A service started without $HOME (a systemd system service running as
// root) still has somewhere to keep sign-ins: the user's home from the user
// database, the same folder `houridle login` uses from a shell.
func TestUserDirWithoutHome(t *testing.T) {
	u, err := user.Current()
	if err != nil || u.HomeDir == "" {
		t.Skip("no user database entry here")
	}
	t.Setenv("HOME", "")
	t.Setenv("XDG_CONFIG_HOME", "")
	t.Setenv("XDG_CACHE_HOME", "")

	cfg, err := userdir.Config()
	if err != nil || cfg != filepath.Join(u.HomeDir, ".config") {
		t.Fatalf("Config() = %q, %v; want %q", cfg, err, filepath.Join(u.HomeDir, ".config"))
	}
	cache, err := userdir.Cache()
	if err != nil || cache != filepath.Join(u.HomeDir, ".cache") {
		t.Fatalf("Cache() = %q, %v; want %q", cache, err, filepath.Join(u.HomeDir, ".cache"))
	}
	store, err := steamauth.DefaultStore()
	if err != nil {
		t.Fatalf("DefaultStore without HOME: %v", err)
	}
	if want := filepath.Join(u.HomeDir, ".config", "houridle", "session.dat"); store.Path != want {
		t.Fatalf("store path = %q, want %q", store.Path, want)
	}
}

// With $HOME set, nothing changes.
func TestUserDirWithHome(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", "")
	if cfg, err := userdir.Config(); err != nil || cfg != filepath.Join(home, ".config") {
		t.Fatalf("Config() = %q, %v", cfg, err)
	}
}
