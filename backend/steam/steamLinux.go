//go:build linux

package steam

import (
	"debug/elf"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"syscall"
)

const (
	apiLibraryName = "libsteam_api.so"
	steamImage     = "steam" // the client's process name in /proc/<pid>/comm
)

// Where Steam lives on Linux, relative to your home folder: the usual
// install (Debian/Ubuntu/Arch packages and the official installer) first,
// then the Flatpak.
var (
	steamRoots = []string{
		".steam/steam",
		".local/share/Steam",
		".var/app/com.valvesoftware.Steam/.local/share/Steam",
	}
	registryFiles = []string{
		".steam/registry.vdf",
		".var/app/com.valvesoftware.Steam/.steam/registry.vdf",
	}
)

// Path returns the Steam install folder (the one holding steamapps).
func Path() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	for _, rel := range steamRoots {
		dir := filepath.Join(home, rel)
		if resolved, err := filepath.EvalSymlinks(dir); err == nil {
			dir = resolved
		}
		if info, err := os.Stat(filepath.Join(dir, "steamapps")); err == nil && info.IsDir() {
			return dir, nil
		}
	}
	return "", errors.New("couldn't find Steam: looked in ~/.steam/steam and ~/.local/share/Steam")
}

// activeProcess reads Steam's running PID and signed-in account ID from
// registry.vdf, Steam's stand-in for the Windows registry on Linux.
func activeProcess() (pid, accountID uint32) {
	home, err := os.UserHomeDir()
	if err != nil {
		return 0, 0
	}
	for _, rel := range registryFiles {
		root, err := readVDF(filepath.Join(home, rel))
		if err != nil {
			continue
		}
		active := root.Get("Registry", "HKCU", "Software", "Valve", "Steam", "ActiveProcess")
		p, _ := strconv.ParseUint(active.Str("pid"), 10, 32)
		u, _ := strconv.ParseUint(active.Str("ActiveUser"), 10, 32)
		if p != 0 {
			return uint32(p), uint32(u)
		}
	}
	return 0, 0
}

// steamProcess returns the PID of the running Steam client, or 0. The PID
// Steam recorded is checked to really be our own "steam" process: it can be
// stale and reused, belong to another user, or (Flatpak) be a PID from inside
// the sandbox. If it doesn't check out, /proc is searched for our Steam.
func steamProcess(pid uint32) uint32 {
	if pid != 0 && isSteamProcess(strconv.FormatUint(uint64(pid), 10)) {
		return pid
	}
	if pid == 0 {
		return 0 // Steam says it isn't running
	}
	entries, err := os.ReadDir("/proc")
	if err != nil {
		return 0
	}
	for _, e := range entries {
		found, err := strconv.ParseUint(e.Name(), 10, 32)
		if err == nil && found != 0 && isSteamProcess(e.Name()) {
			return uint32(found)
		}
	}
	return 0
}

// isSteamProcess reports whether /proc/<pid> is a process named "steam"
// running as this user.
func isSteamProcess(pid string) bool {
	dir := filepath.Join("/proc", pid)
	info, err := os.Stat(dir)
	if err != nil {
		return false
	}
	st, ok := info.Sys().(*syscall.Stat_t)
	if !ok || int(st.Uid) != os.Getuid() {
		return false
	}
	comm, err := os.ReadFile(filepath.Join(dir, "comm"))
	return err == nil && strings.TrimSpace(string(comm)) == steamImage
}

// pathKey makes paths comparable: Linux paths are case-sensitive.
func pathKey(p string) string { return p }

// loadable reports whether this libsteam_api.so matches houridle's CPU.
// Many games also ship a 32-bit copy, which can't be loaded.
func loadable(path string) bool {
	f, err := elf.Open(path)
	if err != nil {
		return false
	}
	defer f.Close()
	want := map[string]elf.Machine{"amd64": elf.EM_X86_64, "arm64": elf.EM_AARCH64}[runtime.GOARCH]
	return f.Class == elf.ELFCLASS64 && f.Machine == want
}

// trusted is always true on Linux: libsteam_api.so isn't signed.
func trusted(string) bool { return true }
