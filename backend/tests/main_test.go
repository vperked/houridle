package tests

import (
	"fmt"
	"os"
	"testing"
)

// TestMain points every per-user folder at a throwaway one, so no test can
// read or overwrite the real saved sign-ins, config or caches (on Windows
// %AppData% and %LocalAppData%, elsewhere the XDG folders and HOME).
func TestMain(m *testing.M) {
	os.Exit(runIsolated(m))
}

func runIsolated(m *testing.M) int {
	root, err := os.MkdirTemp("", "houridle-tests-")
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	defer os.RemoveAll(root)
	if err := isolateUserDirs(root); err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	return m.Run()
}

// userDirVars are the variables Go's os.UserConfigDir, os.UserCacheDir and
// os.UserHomeDir read, on every OS.
var userDirVars = []string{"APPDATA", "LOCALAPPDATA", "USERPROFILE", "XDG_CONFIG_HOME", "XDG_CACHE_HOME", "HOME"}

func isolateUserDirs(root string) error {
	for _, name := range userDirVars {
		dir, err := os.MkdirTemp(root, name+"-")
		if err != nil {
			return err
		}
		if err := os.Setenv(name, dir); err != nil {
			return err
		}
	}
	return nil
}

// isolateUserDirsFor gives one test its own per-user folders, so what it
// saves (accounts, config) can't leak into another test.
func isolateUserDirsFor(t *testing.T) {
	t.Helper()
	for _, name := range userDirVars {
		t.Setenv(name, t.TempDir())
	}
}
