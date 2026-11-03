//go:build windows

package tests

import (
	"errors"
	"fmt"
	"strings"
	"testing"

	"houridle/src/idler"
	"houridle/src/steam"
	"houridle/src/ui"
)

func TestGamePickerOrder(t *testing.T) {
	rows := ui.NewGamePicker(testLibrary(), nil, idler.MaxGames).Rows()
	if rows[0] != "Rust" {
		t.Fatalf("most played game should be first, got %q", rows[0])
	}
	if last := rows[len(rows)-1]; last != "App 730 · not installed" {
		t.Fatalf("games without a name should be last, got %q", last)
	}
}

func TestGamePickerSelectAndConfirm(t *testing.T) {
	c := ui.NewGamePicker(testLibrary(), nil, idler.MaxGames)
	press(t, c, keyDown, keySpace) // select BeamNG
	press(t, c, keyUp, keySpace)   // select Rust
	if got := c.Status(); got != "2 selected" {
		t.Fatalf("status = %q", got)
	}
	done, err := c.Press(keyEnter)
	if !done || err != nil {
		t.Fatalf("enter should confirm, got done=%v err=%v", done, err)
	}
	if ids := fmt.Sprint(c.Checked()); ids != "[252490 284160]" {
		t.Fatalf("selected %v", ids)
	}
}

func TestGamePickerSearchAndAddByID(t *testing.T) {
	c := ui.NewGamePicker(testLibrary(), nil, idler.MaxGames)
	press(t, c, "pal")
	if rows := c.Rows(); fmt.Sprint(rows) != "[Palworld]" {
		t.Fatalf("searching 'pal' gave %v", rows)
	}

	press(t, c, ctrlU, "440") // clear the search, type an unlisted app ID
	rows := c.Rows()
	if last := rows[len(rows)-1]; last != "idle app 440" {
		t.Fatalf("expected an 'idle app 440' row, got %q", last)
	}
	press(t, c, strings.Repeat(keyDown, len(rows)))
	if done, _ := c.Press(keyEnter); !done {
		t.Fatal("enter on the add row should add it and confirm")
	}
	if ids := fmt.Sprint(c.Checked()); ids != "[440]" {
		t.Fatalf("selected %v", ids)
	}
}

func TestGamePickerPreselectedAndCancel(t *testing.T) {
	c := ui.NewGamePicker(testLibrary(), []uint32{1623730, 999}, idler.MaxGames)
	if ids := fmt.Sprint(c.Checked()); ids != "[1623730 999]" {
		t.Fatalf("preselected games should come first, checked, got %v", ids)
	}
	if _, err := c.Press(keyEsc); !errors.Is(err, ui.ErrCancelled) {
		t.Fatalf("esc should cancel, got %v", err)
	}
}

func TestGamePickerIgnoresUnknownKeys(t *testing.T) {
	c := ui.NewGamePicker(testLibrary(), nil, idler.MaxGames)
	before := len(c.Rows())
	press(t, c, "\x1b[1;5C") // ctrl+right isn't used; it mustn't end up in the search
	if after := len(c.Rows()); after != before {
		t.Fatalf("an unknown key changed the list from %d to %d rows", before, after)
	}
}

func TestGamePickerEnterPicksCursorWhenNothingSelected(t *testing.T) {
	c := ui.NewGamePicker(testLibrary(), nil, idler.MaxGames)
	press(t, c, keyDown)
	if done, _ := c.Press(keyEnter); !done || fmt.Sprint(c.Checked()) != "[284160]" {
		t.Fatalf("enter should pick the game under the cursor, got %v", c.Checked())
	}
}

func TestGamePickerCapsSelection(t *testing.T) {
	library := map[uint32]*steam.Game{}
	for i := range idler.MaxGames + 5 {
		library[uint32(i+1)] = &steam.Game{AppID: uint32(i + 1), Name: fmt.Sprintf("Game %02d", i)}
	}
	c := ui.NewGamePicker(library, nil, idler.MaxGames)
	for range idler.MaxGames + 5 {
		press(t, c, keySpace, keyDown)
	}
	if n := len(c.Checked()); n != idler.MaxGames {
		t.Fatalf("selected %d games, the cap is %d", n, idler.MaxGames)
	}
}

func TestSingleGamePickTakesCursorRow(t *testing.T) {
	c := ui.NewGamePicker(testLibrary(), nil, 1)
	press(t, c, keySpace, keyDown) // check Rust, then move to BeamNG
	if done, _ := c.Press(keyEnter); !done || fmt.Sprint(c.Checked()) != "[284160]" {
		t.Fatalf("pick-one should take the row under the cursor, got %v", c.Checked())
	}
}

func TestGamePickerLayoutFits(t *testing.T) {
	library := testLibrary()
	for i := uint32(1); i <= 60; i++ {
		library[100000+i] = &steam.Game{AppID: 100000 + i, Name: strings.Repeat("Very Long Game Name ", 4), Minutes: int(i)}
	}
	for _, size := range consoleSizes {
		for _, maxChecked := range []int{idler.MaxGames, 1} {
			c := ui.NewGamePicker(library, nil, maxChecked)
			press(t, c, keyPageDown, keyPageDown, keySpace, "x", backspace)
			checkFrame(t, c.Render(size[0], size[1]), size[0], size[1])
		}
	}
}
