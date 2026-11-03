//go:build windows

// Package steam reads the local Steam install: where it is, whether it's
// running, your games and playtime, and a steam_api64.dll to load.
package steam

import (
	"errors"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"golang.org/x/sys/windows"
	"golang.org/x/sys/windows/registry"
)

const (
	apiDLLName      = "steam_api64.dll"
	dllSearchDepth  = 3   // folders below steamapps/common/<game> to look in
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

// Running reports whether the Steam client is running and signed in. The
// registry PID can be stale after a crash, so the process is checked too.
func Running() bool {
	pid, user := activeProcess()
	if pid == 0 || user == 0 {
		return false
	}
	h, err := windows.OpenProcess(windows.PROCESS_QUERY_LIMITED_INFORMATION, false, pid)
	if err != nil {
		return false
	}
	defer windows.CloseHandle(h)
	var code uint32
	return windows.GetExitCodeProcess(h, &code) == nil && code == stillActiveCode
}

func readVDF(path string) (VDF, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	return ParseVDF(string(data))
}

// libraryFolders returns every Steam library folder, the main install first.
func libraryFolders(steamDir string) []string {
	seen := map[string]bool{}
	var out []string
	add := func(p string) {
		p = filepath.Clean(p)
		if key := strings.ToLower(p); !seen[key] {
			seen[key] = true
			out = append(out, p)
		}
	}
	add(steamDir)
	if root, err := readVDF(filepath.Join(steamDir, "steamapps", "libraryfolders.vdf")); err == nil {
		for _, v := range root.Get("libraryfolders") {
			if lib, ok := v.(VDF); ok && lib.Str("path") != "" {
				add(lib.Str("path"))
			}
		}
	}
	return out
}

// gameList is a set of games keyed by app ID, built up from several files.
type gameList map[uint32]*Game

// entry returns the game for id, adding it if it isn't there yet.
func (l gameList) entry(id uint32) *Game {
	g, ok := l[id]
	if !ok {
		g = &Game{AppID: id}
		l[id] = g
	}
	return g
}

// LocalGames lists your installed games plus every game you've played on
// this PC, with playtime where Steam has it.
func LocalGames(steamDir string) map[uint32]*Game {
	games := gameList{}
	addInstalledGames(games, steamDir)
	addPlaytime(games, steamDir)
	return games
}

// addInstalledGames reads the appmanifest_*.acf file Steam keeps for every
// installed game, in every library folder.
func addInstalledGames(games gameList, steamDir string) {
	for _, lib := range libraryFolders(steamDir) {
		manifests, _ := filepath.Glob(filepath.Join(lib, "steamapps", "appmanifest_*.acf"))
		for _, path := range manifests {
			root, err := readVDF(path)
			if err != nil {
				continue
			}
			app := root.Get("AppState")
			if id, ok := parseAppID(app.Str("appid")); ok {
				games.entry(id).Name = app.Str("name")
			}
		}
	}
}

// addPlaytime reads the signed-in user's localconfig.vdf, where Steam caches
// minutes played per game.
func addPlaytime(games gameList, steamDir string) {
	_, user := activeProcess()
	if user == 0 {
		return
	}
	path := filepath.Join(steamDir, "userdata", strconv.FormatUint(uint64(user), 10), "config", "localconfig.vdf")
	root, err := readVDF(path)
	if err != nil {
		return
	}
	for key, v := range root.Get("UserLocalConfigStore", "Software", "Valve", "Steam", "apps") {
		app, isBlock := v.(VDF)
		id, ok := parseAppID(key)
		if !isBlock || !ok {
			continue
		}
		minutes, _ := strconv.Atoi(app.Str("Playtime"))
		if minutes > 0 || games[id] != nil {
			games.entry(id).Minutes = minutes
		}
	}
}

func parseAppID(s string) (uint32, bool) {
	id, err := strconv.ParseUint(s, 10, 32)
	return uint32(id), err == nil && id != 0
}

// FindAPIDLL returns a steam_api64.dll to load. It prefers a copy next to
// houridle; otherwise it copies the newest one found in your installed games.
func FindAPIDLL(appDir, steamDir string) (string, error) {
	local := filepath.Join(appDir, apiDLLName)
	if _, err := os.Stat(local); err == nil {
		return local, nil
	}

	var best string
	var bestTime int64
	for _, lib := range libraryFolders(steamDir) {
		common := filepath.Join(lib, "steamapps", "common")
		baseDepth := strings.Count(common, string(os.PathSeparator))
		filepath.WalkDir(common, func(p string, d fs.DirEntry, err error) error {
			if err != nil {
				return nil // unreadable folder; keep going
			}
			if d.IsDir() {
				if strings.Count(p, string(os.PathSeparator))-baseDepth > dllSearchDepth+1 {
					return fs.SkipDir
				}
				return nil
			}
			if !strings.EqualFold(d.Name(), apiDLLName) {
				return nil
			}
			if info, err := d.Info(); err == nil && info.ModTime().Unix() > bestTime {
				best, bestTime = p, info.ModTime().Unix()
			}
			return nil
		})
	}
	if best == "" {
		return "", errors.New("couldn't find " + apiDLLName + " in your Steam games; put a copy next to houridle.exe or pass -dll")
	}
	if err := copyFile(best, local); err != nil {
		return best, nil // can't write next to the exe; load it in place
	}
	return local, nil
}

func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.Create(dst)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		os.Remove(dst)
		return err
	}
	return out.Close()
}
