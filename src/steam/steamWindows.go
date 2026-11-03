//go:build windows

package steam

import (
	"errors"
	"path/filepath"
	"strings"

	"golang.org/x/sys/windows"
	"golang.org/x/sys/windows/registry"
)

const (
	apiLibraryName  = "steam_api64.dll"
	stillActiveCode = 259 // STILL_ACTIVE from GetExitCodeProcess
)

// Path returns the Steam install folder from the registry.
func Path() (string, error) {
	k, err := registry.OpenKey(registry.CURRENT_USER, `Software\Valve\Steam`, registry.QUERY_VALUE)
	if err != nil {
		return "", errors.New("couldn't find Steam: it doesn't seem to be installed")
	}
	defer k.Close()
	p, _, err := k.GetStringValue("SteamPath")
	if err != nil || p == "" {
		return "", errors.New("couldn't find Steam's install folder in the registry")
	}
	return filepath.Clean(p), nil
}

// activeProcess reads Steam's running PID and signed-in account ID.
func activeProcess() (pid, accountID uint32) {
	k, err := registry.OpenKey(registry.CURRENT_USER, `Software\Valve\Steam\ActiveProcess`, registry.QUERY_VALUE)
	if err != nil {
		return 0, 0
	}
	defer k.Close()
	p, _, _ := k.GetIntegerValue("pid")
	u, _, _ := k.GetIntegerValue("ActiveUser")
	return uint32(p), uint32(u)
}

func processAlive(pid uint32) bool {
	h, err := windows.OpenProcess(windows.PROCESS_QUERY_LIMITED_INFORMATION, false, pid)
	if err != nil {
		return false
	}
	defer windows.CloseHandle(h)
	var code uint32
	return windows.GetExitCodeProcess(h, &code) == nil && code == stillActiveCode
}

// pathKey makes paths comparable: Windows paths ignore case.
func pathKey(p string) string { return strings.ToLower(p) }

// loadable reports whether houridle can load this library. steam_api64.dll is
// always the 64-bit build.
func loadable(string) bool { return true }
