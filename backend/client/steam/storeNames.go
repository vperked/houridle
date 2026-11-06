package steam

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"sync"
	"time"
)

const storeLookupTimeout = 5 * time.Second

// ResolveGames returns a copy of each app ID's game, looking up names on the
// Steam store for games that aren't installed locally. library is not modified.
func ResolveGames(appIDs []uint32, library map[uint32]*Game) []*Game {
	games := make([]*Game, len(appIDs))
	client := &http.Client{Timeout: storeLookupTimeout}
	var wg sync.WaitGroup
	for i, id := range appIDs {
		g := &Game{AppID: id}
		if known, ok := library[id]; ok {
			*g = *known
		}
		games[i] = g
		if g.Name != "" {
			continue
		}
		wg.Add(1)
		go func() {
			defer wg.Done()
			if g.Name = storeName(client, id); g.Name == "" {
				g.Name = PlaceholderName(id)
			}
		}()
	}
	wg.Wait()
	return games
}

// storeName looks up a game's name on the Steam store. Best effort.
func storeName(client *http.Client, appID uint32) string {
	resp, err := client.Get(fmt.Sprintf("https://store.steampowered.com/api/appdetails?appids=%d&filters=basic", appID))
	if err != nil {
		return ""
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "" // e.g. rate limited
	}
	var body map[string]struct {
		Data struct {
			Name string `json:"name"`
		} `json:"data"`
	}
	if json.NewDecoder(resp.Body).Decode(&body) != nil {
		return ""
	}
	return body[strconv.FormatUint(uint64(appID), 10)].Data.Name
}
