// Package config loads and saves houridle's config.json, finds the folder it
// lives in, and reads app IDs and flags from the command line.
package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

const fileName = "config.json"

type Config struct {
	Games []uint32 `json:"games"`
}

// AppDir is where config.json, houridle.log and the DLL copy live: next to
// the exe, or the working directory under `go run` (which builds into a temp
// folder). When the exe's folder can't be written (e.g. Program Files), it's
// houridle's folder in the user config directory instead.
func AppDir() string {
	exe, err := os.Executable()
	if err != nil {
		wd, _ := os.Getwd()
		return wd
	}
	fallback := ""
	if dir, err := os.UserConfigDir(); err == nil {
		fallback = filepath.Join(dir, "houridle")
	}
	return AppDirFor(exe, fallback)
}

// AppDirFor is AppDir for an exe at exePath, with fallback as the folder to
// use when the exe's own folder isn't writable ("" for none).
func AppDirFor(exePath, fallback string) string {
	dir := filepath.Dir(exePath)
	if isGoRunBuild(dir) {
		wd, _ := os.Getwd()
		return wd
	}
	if writable(dir) || fallback == "" {
		return dir
	}
	if err := os.MkdirAll(fallback, 0o755); err != nil {
		return dir
	}
	return fallback
}

// isGoRunBuild reports whether dir is one of the temporary folders `go run`
// builds into (go-build… under the temp directory).
func isGoRunBuild(dir string) bool {
	tmp := os.Getenv("GOTMPDIR")
	if tmp == "" {
		tmp = os.TempDir()
	}
	rel, err := filepath.Rel(resolve(tmp), resolve(dir)) // case-insensitive on Windows
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return false
	}
	for _, part := range strings.Split(rel, string(filepath.Separator)) {
		if strings.HasPrefix(part, "go-build") {
			return true
		}
	}
	return false
}

// resolve follows symlinks (and, on Windows, expands short 8.3 names), so
// two spellings of one folder compare equal.
func resolve(path string) string {
	if real, err := filepath.EvalSymlinks(path); err == nil {
		return real
	}
	return path
}

// writable reports whether files can be created in dir.
func writable(dir string) bool {
	f, err := os.CreateTemp(dir, ".houridle-write-test-*")
	if err != nil {
		return false
	}
	f.Close()
	os.Remove(f.Name())
	return true
}

// Load reads config.json. A missing file is not an error.
func Load(appDir string) (Config, error) {
	var cfg Config
	data, err := os.ReadFile(filepath.Join(appDir, fileName))
	if errors.Is(err, fs.ErrNotExist) {
		return cfg, nil
	}
	if err != nil {
		return cfg, err
	}
	if err := json.Unmarshal(data, &cfg); err != nil {
		return Config{}, fmt.Errorf("%s isn't valid JSON (%v), ignoring it", fileName, err)
	}
	return cfg, nil
}

// Save writes config.json. It writes a temporary file and renames it into
// place, so a crash or power cut leaves either the old file or the new one,
// never a half-written one.
func Save(appDir string, cfg Config) (err error) {
	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp(appDir, fileName+".tmp-*")
	if err != nil {
		return err
	}
	defer func() {
		if err != nil {
			tmp.Close()
			os.Remove(tmp.Name())
		}
	}()
	if _, err = tmp.Write(append(data, '\n')); err != nil {
		return err
	}
	if err = tmp.Sync(); err != nil {
		return err
	}
	if err = tmp.Close(); err != nil {
		return err
	}
	if err = os.Chmod(tmp.Name(), 0o644); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), filepath.Join(appDir, fileName))
}

// ParseAppIDs accepts IDs separated by spaces and/or commas.
func ParseAppIDs(parts []string) ([]uint32, error) {
	var ids []uint32
	for _, part := range parts {
		for _, field := range strings.FieldsFunc(part, isSeparator) {
			n, err := strconv.ParseUint(field, 10, 32)
			if err != nil || n == 0 {
				return nil, fmt.Errorf("%q isn't an app ID", field)
			}
			ids = append(ids, uint32(n))
		}
	}
	return ids, nil
}

func isSeparator(r rune) bool {
	return r == ',' || r == ' ' || r == '\t' || r == '\r' || r == '\n'
}

// Dedupe drops repeated IDs, keeping the first occurrence's position.
func Dedupe(ids []uint32) []uint32 {
	seen := make(map[uint32]bool, len(ids))
	out := make([]uint32, 0, len(ids))
	for _, id := range ids {
		if !seen[id] {
			seen[id] = true
			out = append(out, id)
		}
	}
	return out
}
