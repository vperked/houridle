//go:build windows

package tests

import (
	"testing"

	"houridle/src/autostart"
)

// Windows runs this command line at sign-in; paths with spaces must be quoted.
func TestAutostartCommandQuotesPaths(t *testing.T) {
	got := autostart.Command(`C:\Program Files\houridle\houridle.exe`, `C:\Program Files\houridle\steam_api64.dll`)
	want := `"C:\Program Files\houridle\houridle.exe" -tray -dll "C:\Program Files\houridle\steam_api64.dll" -stop never`
	if got != want {
		t.Fatalf("got  %s\nwant %s", got, want)
	}
}
