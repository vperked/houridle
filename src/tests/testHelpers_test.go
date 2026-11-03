//go:build windows

package tests

import (
	"regexp"
	"strings"
	"testing"
	"unicode/utf8"

	"houridle/src/steam"
	"houridle/src/steamworks"
)

// Key presses, as the console sends them.
const (
	keyDown     = "\x1b[B"
	keyUp       = "\x1b[A"
	keyPageDown = "\x1b[6~"
	keyEnter    = "\r"
	keyEsc      = "\x1b"
	keySpace    = " "
	ctrlA       = "\x01"
	ctrlU       = "\x15"
	backspace   = "\x7f"
)

// consoleSizes are the width × height combinations layouts must fit.
var consoleSizes = [][2]int{{30, 12}, {45, 14}, {60, 16}, {80, 24}, {120, 40}}

func testLibrary() map[uint32]*steam.Game {
	return map[uint32]*steam.Game{
		252490:  {AppID: 252490, Name: "Rust", Minutes: 12},
		284160:  {AppID: 284160, Name: "BeamNG.drive"},
		1623730: {AppID: 1623730, Name: "Palworld"},
		730:     {AppID: 730}, // played but not installed: no name
	}
}

func testAchievements() []steamworks.Achievement {
	return []steamworks.Achievement{
		{ID: "ACH_A", Name: "Apex Predator", Description: "Kill 3 sharks with a speargun", Unlocked: true},
		{ID: "ACH_B", Name: "Battle Bus", Description: "Drive a modular car with 4 passengers"},
		{ID: "ACH_C", Name: "Secret", Hidden: true},
	}
}

var ansiCodes = regexp.MustCompile(`\x1b\[[0-9;?]*[A-Za-z]`)

// checkFrame fails if any line is wider than the console, or the frame is
// taller than it (a full last line would scroll the screen and flicker).
func checkFrame(t *testing.T, frame string, width, height int) {
	t.Helper()
	plain := ansiCodes.ReplaceAllString(frame, "")
	lines := strings.Split(strings.TrimSuffix(plain, "\n"), "\n")
	if len(lines) > height-1 {
		t.Errorf("%dx%d: frame has %d lines, console fits %d", width, height, len(lines), height-1)
	}
	for _, line := range lines {
		if n := utf8.RuneCountInString(line); n > width {
			t.Errorf("%dx%d: line is %d wide: %q", width, height, n, line)
		}
	}
}

func press(t *testing.T, c interface {
	Press(string) (bool, error)
}, keys ...string) {
	t.Helper()
	for _, k := range keys {
		c.Press(k)
	}
}
