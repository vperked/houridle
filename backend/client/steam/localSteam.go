// Package steam reads the local Steam install: where it is, whether it's
// running, your games and playtime, and a Steam API library to load.
//
// Finding Steam differs per OS (steamWindows.go, steamLinux.go); reading
// its files is shared and lives here.
package steam

import (
	"cmp"
	"errors"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
)

// apiSearchDepth is how many folders below steamapps/common/<game> to look
// for the Steam API library.
const apiSearchDepth = 3

// Running reports whether the Steam client is running and signed in. The
// recorded PID can be stale after a crash, so the process is checked too.
func Running() bool { return RunningPID() != 0 }

func RunningPID() uint32 {
	pid, user := activeProcess()
	if user == 0 {
		return 0
	}
	return steamProcess(pid)
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
		if key := pathKey(p); !seen[key] {
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

// ---------- games ----------

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

// ---------- the Steam API library ----------

// FindAPILibrary returns the Steam API library to load (steam_api64.dll or
// libsteam_api.so). It prefers a copy next to houridle; otherwise it copies
// the newest usable one found in your installed games.
func FindAPILibrary(appDir, steamDir string) (string, error) {
	local := filepath.Join(appDir, apiLibraryName)
	_, statErr := os.Stat(local)
	localExists := statErr == nil
	if localExists && trusted(local) {
		return local, nil
	}

	best := newestAPILibrary(steamDir)
	if best == "" {
		return "", errors.New("couldn't find a usable " + apiLibraryName + " in your Steam games; put a copy next to houridle or pass -dll")
	}
	if localExists {
		return best, nil
	}
	if err := copyFile(best, local); err != nil {
		return best, nil // can't write next to houridle; load it in place
	}
	return local, nil
}

// newestAPILibrary searches installed games for the most recently updated
// copy of the library that this computer can load.
func newestAPILibrary(steamDir string) string {
	type candidate struct {
		path    string
		modTime int64
	}
	var found []candidate
	for _, lib := range libraryFolders(steamDir) {
		common := filepath.Join(lib, "steamapps", "common")
		baseDepth := strings.Count(common, string(os.PathSeparator))
		filepath.WalkDir(common, func(p string, d fs.DirEntry, err error) error {
			if err != nil {
				return nil // unreadable folder; keep going
			}
			if d.IsDir() {
				if strings.Count(p, string(os.PathSeparator))-baseDepth > apiSearchDepth+1 {
					return fs.SkipDir
				}
				return nil
			}
			if !strings.EqualFold(d.Name(), apiLibraryName) || !loadable(p) {
				return nil
			}
			if info, err := d.Info(); err == nil {
				found = append(found, candidate{p, info.ModTime().Unix()})
			}
			return nil
		})
	}
	slices.SortStableFunc(found, func(a, b candidate) int { return cmp.Compare(b.modTime, a.modTime) })
	for _, c := range found {
		if trusted(c.path) {
			return c.path
		}
	}
	return ""
}

func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	tmp, err := os.CreateTemp(filepath.Dir(dst), filepath.Base(dst)+".*.tmp")
	if err != nil {
		return err
	}
	_, err = io.Copy(tmp, in)
	if closeErr := tmp.Close(); err == nil {
		err = closeErr
	}
	if err == nil {
		err = os.Rename(tmp.Name(), dst)
	}
	if err != nil {
		os.Remove(tmp.Name())
	}
	return err
}
