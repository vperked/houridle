package tests

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"houridle/backend/views/web"
)

func TestAppManifestNeedsTheKey(t *testing.T) {
	srv := &web.Server{Key: "s3cret", Status: func() web.Status { return web.Status{} }, Stop: func() {}}
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	resp, err := http.Get(ts.URL + "/manifest.webmanifest")
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("without the key: HTTP %d", resp.StatusCode)
	}

	req, _ := http.NewRequest("GET", ts.URL+"/manifest.webmanifest", nil)
	req.AddCookie(&http.Cookie{Name: "houridle_key", Value: "s3cret"})
	resp, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var raw map[string]any
	json.NewDecoder(resp.Body).Decode(&raw)
	icons, _ := raw["icons"].([]any)
	if resp.StatusCode != 200 || raw["start_url"] != "/?key=s3cret" || raw["display"] != "standalone" || len(icons) != 3 {
		t.Errorf("manifest: HTTP %d %v", resp.StatusCode, raw)
	}
	if ct := resp.Header.Get("Content-Type"); ct != "application/manifest+json" {
		t.Errorf("content type %q", ct)
	}
}
