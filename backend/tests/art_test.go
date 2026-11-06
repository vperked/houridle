package tests

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"houridle/backend/features/gamestore"
	"houridle/backend/views/web"
)

func TestGameArtIsLookedUpOnce(t *testing.T) {
	var calls atomic.Int32
	store := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		in := r.URL.Query().Get("input_json")
		if !strings.Contains(in, `"include_assets":true`) {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		// 4051150 keeps its pictures under a versioned folder; 999 isn't in the store.
		io.WriteString(w, `{"response":{"store_items":[{"appid":4051150,"assets":{"asset_url_format":"steam/apps/4051150/${FILENAME}?t=1","small_capsule":"8acd/capsule_231x87.jpg","header":"eb54/header.jpg"}},{"appid":999}]}}`)
	}))
	defer store.Close()
	old := gamestore.StoreAPI
	gamestore.StoreAPI = store.URL
	defer func() { gamestore.StoreAPI = old }()

	srv := &web.Server{Key: "k", Status: func() web.Status { return web.Status{} }, Stop: func() {}}
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()
	get := func(q string) (int, map[string]gamestore.Art) {
		req, _ := http.NewRequest("GET", ts.URL+"/api/art?apps="+q, nil)
		req.AddCookie(&http.Cookie{Name: "houridle_key", Value: "k"})
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		var out map[string]gamestore.Art
		json.NewDecoder(resp.Body).Decode(&out)
		return resp.StatusCode, out
	}
	code, art := get("4051150,999")
	want := "https://shared.akamai.steamstatic.com/store_item_assets/steam/apps/4051150/8acd/capsule_231x87.jpg?t=1"
	if code != 200 || art["4051150"].Capsule != want || !strings.HasSuffix(art["4051150"].Header, "/eb54/header.jpg?t=1") {
		t.Fatalf("art: %d %+v", code, art)
	}
	if a, ok := art["999"]; !ok || a.Capsule != "" {
		t.Errorf("a game the store doesn't show has no pictures: %+v", art)
	}
	get("4051150,999")
	if n := calls.Load(); n != 1 {
		t.Errorf("pictures should be looked up once, not %d times", n)
	}
	if code, _ := get("nope"); code != http.StatusBadRequest {
		t.Errorf("no games: HTTP %d", code)
	}
	if code, _ := get(strings.Repeat(fmt.Sprint(1, ","), 301)); code != http.StatusBadRequest {
		t.Errorf("too many: HTTP %d", code)
	}
}
