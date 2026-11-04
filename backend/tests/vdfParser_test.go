package tests

import (
	"testing"

	"houridle/backend/steam"
)

func TestParseVDF(t *testing.T) {
	root, err := steam.ParseVDF(`
// comment
"AppState"
{
	"appid"		"252490"
	"Name"		"Rust \"Staging\""
	"UserConfig" { "language" "english" }
	"platform"	"win" [$WIN32]
}`)
	if err != nil {
		t.Fatal(err)
	}
	app := root.Get("appstate")
	if app.Str("APPID") != "252490" || app.Str("name") != `Rust "Staging"` {
		t.Fatalf("keys should be case-insensitive and escapes handled: %v", app)
	}
	if app.Get("userconfig").Str("language") != "english" {
		t.Fatal("nested block not parsed")
	}
	if app.Str("platform") != "win" {
		t.Fatal("a [$WIN32] condition after a value should be skipped")
	}
	if root.Get("missing", "path") != nil || app.Str("missing") != "" {
		t.Fatal("missing keys should give nil / empty")
	}
}

func TestParseVDFRejectsBrokenFiles(t *testing.T) {
	for _, broken := range []string{`"a" { "b" "c"`, `"a"`, `}`} {
		if _, err := steam.ParseVDF(broken); err == nil {
			t.Errorf("%q should fail to parse", broken)
		}
	}
}
