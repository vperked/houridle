// Package steamver keeps up with Steam: the values its sign-in expects can
// be updated without a new houridle, from steam.json in houridle's
// repository, and it reads which Steam client version Valve publishes now.
package steamver

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"regexp"
	"strings"
	"time"

	"houridle/backend/steamcm"
)

// ManifestURL is Valve's manifest of the current Steam client for Windows.
// A variable so tests can use a local fake.
var ManifestURL = "https://media.steampowered.com/client/steam_client_win32"

// FileName is the Steam settings file, in the repository and (the last one
// fetched) next to houridle.
const FileName = "steam.json"

// Settings is what Steam's sign-in expects, as steam.json says.
type Settings struct {
	ProtocolVersion uint64 `json:"protocolVersion"`
	ClientVersion   uint64 `json:"clientPackageVersion"`
	DirectoryURL    string `json:"directoryUrl,omitempty"`
	Note            string `json:"note,omitempty"`
}

// Parse reads steam.json and checks its values are believable, so a typo
// can't stop houridle signing in.
func Parse(data []byte) (Settings, error) {
	var s Settings
	if err := json.Unmarshal(data, &s); err != nil {
		return s, fmt.Errorf("%s isn't valid JSON: %w", FileName, err)
	}
	switch {
	case s.ProtocolVersion < 65000 || s.ProtocolVersion > 70000:
		return s, fmt.Errorf("%s: protocolVersion %d doesn't look right", FileName, s.ProtocolVersion)
	case s.ClientVersion == 0 || s.ClientVersion > 1<<32:
		return s, fmt.Errorf("%s: clientPackageVersion %d doesn't look right", FileName, s.ClientVersion)
	case s.DirectoryURL != "" && !strings.HasPrefix(s.DirectoryURL, "https://api.steampowered.com/"):
		return s, fmt.Errorf("%s: directoryUrl must be on api.steampowered.com", FileName)
	}
	return s, nil
}

// Apply makes houridle sign in with s from now on.
func Apply(s Settings) {
	steamcm.SetVersions(s.ProtocolVersion, s.ClientVersion)
	if s.DirectoryURL != "" {
		steamcm.SetDirectoryURL(s.DirectoryURL)
	}
}

// Defaults are the values built into this houridle.
func Defaults() Settings {
	return Settings{ProtocolVersion: steamcm.DefaultProtocolVersion, ClientVersion: steamcm.DefaultClientVersion}
}

// LoadSaved applies the steam.json last fetched (kept at path), if any.
func LoadSaved(path string) (Settings, bool) {
	data, err := os.ReadFile(path)
	if err != nil {
		return Settings{}, false
	}
	s, err := Parse(data)
	if err != nil {
		return Settings{}, false
	}
	Apply(s)
	return s, true
}

var manifestVersion = regexp.MustCompile(`"version"\s+"(\d+)"`)

// ClientBuild is the Steam client version Valve publishes now (a build
// number, which is also a Unix time).
func ClientBuild(ctx context.Context) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, ManifestURL, nil)
	if err != nil {
		return "", err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", errors.New("couldn't reach Steam's client manifest")
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("Steam's client manifest answered HTTP %d", resp.StatusCode)
	}
	data, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	m := manifestVersion.FindSubmatch(data)
	if m == nil {
		return "", errors.New("couldn't read Steam's client version")
	}
	return string(m[1]), nil
}
