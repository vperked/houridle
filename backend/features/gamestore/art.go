package gamestore

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"houridle/backend/online/steamweb"
)

// StoreAPI is Steam's web API (its store service answers without a key).
// A variable so tests can use a local fake.
var StoreAPI = "https://api.steampowered.com"

const (
	assetBase = "https://shared.akamai.steamstatic.com/store_item_assets/"
	artBatch  = 100
)

// Art is where a game's store pictures are. Newer games keep them under
// an address with a version in it, so it has to be looked up.
type Art struct {
	Capsule string `json:"capsule,omitempty"` // small, for lists (231×87)
	Header  string `json:"header,omitempty"`  // 460×215
}

// GameArt looks up games' store pictures. Games the store doesn't show
// come back with no pictures.
func GameArt(ctx context.Context, appIDs []uint32) (map[uint32]Art, error) {
	client := &http.Client{Timeout: fetchTimeout}
	out := make(map[uint32]Art, len(appIDs))
	for start := 0; start < len(appIDs); start += artBatch {
		batch := appIDs[start:min(start+artBatch, len(appIDs))]
		ids := make([]string, len(batch))
		for i, id := range batch {
			ids[i] = fmt.Sprintf(`{"appid":%d}`, id)
			out[id] = Art{}
		}
		input := `{"ids":[` + strings.Join(ids, ",") + `],"context":{"language":"english","country_code":"US"},"data_request":{"include_assets":true}}`
		var res struct {
			Response struct {
				Items []struct {
					AppID  uint32 `json:"appid"`
					Assets struct {
						Format string `json:"asset_url_format"`
						Small  string `json:"small_capsule"`
						Main   string `json:"main_capsule"`
						Header string `json:"header"`
					} `json:"assets"`
				} `json:"store_items"`
			} `json:"response"`
		}
		u := StoreAPI + "/IStoreBrowseService/GetItems/v1/?input_json=" + url.QueryEscape(input)
		if err := steamweb.GetJSON(ctx, client, u, &res, maxBody); err != nil {
			return out, fmt.Errorf("couldn't look up game pictures: %w", err)
		}
		for _, it := range res.Response.Items {
			a := it.Assets
			if a.Format == "" {
				continue
			}
			pic := func(file string) string {
				if file == "" {
					return ""
				}
				return assetBase + strings.Replace(a.Format, "${FILENAME}", file, 1)
			}
			small := a.Small
			if small == "" {
				small = a.Main
			}
			out[it.AppID] = Art{Capsule: pic(small), Header: pic(a.Header)}
		}
	}
	return out, nil
}

// ArtCache keeps looked-up pictures for a day. Its zero value is ready to
// use; don't copy it.
type ArtCache struct {
	mu   sync.Mutex
	art  map[uint32]Art
	when map[uint32]time.Time
}

const artTTL = 24 * time.Hour

// Get returns pictures for the games, looking up the ones not known yet.
func (c *ArtCache) Get(ctx context.Context, appIDs []uint32) map[uint32]Art {
	c.mu.Lock()
	if c.art == nil {
		c.art, c.when = map[uint32]Art{}, map[uint32]time.Time{}
	}
	out := make(map[uint32]Art, len(appIDs))
	var missing []uint32
	for _, id := range appIDs {
		if t, ok := c.when[id]; ok && time.Since(t) < artTTL {
			out[id] = c.art[id]
		} else {
			missing = append(missing, id)
		}
	}
	c.mu.Unlock()
	if len(missing) == 0 {
		return out
	}
	found, err := GameArt(ctx, missing)
	now := time.Now()
	c.mu.Lock()
	defer c.mu.Unlock()
	for id, a := range found {
		out[id] = a
		if err == nil || a.Capsule != "" {
			c.art[id], c.when[id] = a, now // a failed lookup isn't kept
		}
	}
	return out
}
