// Package config loads and saves houridle's config.json and parses app IDs.
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

// AppDir is where config.json and the DLL copy live: next to the exe, or the
// working directory under `go run` (which builds into a temp folder).
func AppDir() string {
	if exe, err := os.Executable(); err == nil {
		dir := filepath.Dir(exe)
		if !strings.Contains(strings.ToLower(dir), "go-build") {
			return dir
		}
	}
	wd, _ := os.Getwd()
	return wd
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

func Save(appDir string, cfg Config) error {
	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(appDir, fileName), append(data, '\n'), 0o644)
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
