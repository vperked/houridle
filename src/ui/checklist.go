package ui

import (
	"errors"
	"fmt"
	"os"
	"strings"
)

// ErrCancelled is returned when the user backs out of a checklist.
var ErrCancelled = errors.New("cancelled")

// Checklist is a searchable, scrollable list with checkboxes, driven by the
// keyboard. The game picker and the achievement picker are both Checklists
// with different settings (see NewGamePicker and NewAchievementPicker).
//
// It can be driven without a console, which is how the tests use it: Press
// keys, then look at Rows, Checked, Changes, Status or a Render.
//
// The code is split three ways: this file holds the list and its rules,
// checklistKeys.go turns key presses into actions, and checklistView.go draws it.
type Checklist struct {
	// Content and text.
	title       string
	placeholder string    // shown in the empty search box
	hints       [2]string // footer key hints: full, and short for narrow consoles
	items       []*checkItem

	// Behaviour.
	maxChecked int                                     // 0 = no limit, 1 = pick exactly one
	offerAdd   func(query string) (*checkItem, string) // optional: extra row for a search, e.g. "idle app 440"
	summary    func(c *Checklist) string               // footer status, e.g. "3 selected"

	// trackChanges shows, in the right column, how each row differs from when
	// the list opened: changeLabels are for newly checked, newly unchecked,
	// and still checked.
	trackChanges bool
	changeLabels [3]string

	// Interaction state.
	search   []rune
	cursor   int // index into rows()
	scroll   int // first visible row
	pageSize int // rows that fit on screen (set while drawing)
	message  string
}

type checkItem struct {
	id      string // app ID or achievement API name
	label   string
	detail  string // muted text after the label, e.g. an achievement's description
	column  string // optional fixed-width column, e.g. app ID
	meta    string // optional right column, e.g. hours played
	checked bool
	initial bool // checked state when the list opened (for change tracking)
	faded   bool // shown muted: not installed, hidden achievement
}

// checkRow is one visible line: an item, or an offer to add a new one.
type checkRow struct {
	item    *checkItem
	add     *checkItem
	addText string
}

// interactive reports whether a full-screen checklist can run here.
func interactive() bool { return styled && stdinIsTerminal() }

// run shows the checklist full-screen until the user confirms or cancels.
func (c *Checklist) run() error {
	restore, err := enableRawInput()
	if err != nil {
		return err
	}
	defer restore()
	os.Stdout.WriteString(altScreenOn)
	defer os.Stdout.WriteString(altScreenOff)

	buf := make([]byte, 256)
	for {
		w, h := termSize()
		os.Stdout.WriteString(c.Render(w, h))
		n, err := os.Stdin.Read(buf)
		if err != nil {
			return err
		}
		if done, err := c.handle(buf[:n]); done || err != nil {
			return err
		}
	}
}

// ---------- what's visible ----------

// rows returns the items matching the search, plus an "add" row if offered.
func (c *Checklist) rows() []checkRow {
	query := strings.ToLower(strings.TrimSpace(string(c.search)))
	var rows []checkRow
	for _, it := range c.items {
		if it.matches(query) {
			rows = append(rows, checkRow{item: it})
		}
	}
	if c.offerAdd != nil && query != "" {
		if add, text := c.offerAdd(query); add != nil && !c.hasID(add.id) {
			rows = append(rows, checkRow{add: add, addText: text})
		}
	}
	return rows
}

func (it *checkItem) matches(query string) bool {
	return query == "" ||
		strings.Contains(strings.ToLower(it.label), query) ||
		strings.Contains(strings.ToLower(it.detail), query) ||
		strings.HasPrefix(strings.ToLower(it.id), query)
}

func (c *Checklist) current() (checkRow, bool) {
	rows := c.rows()
	if c.cursor < 0 || c.cursor >= len(rows) {
		return checkRow{}, false
	}
	return rows[c.cursor], true
}

func (c *Checklist) hasID(id string) bool {
	for _, it := range c.items {
		if it.id == id {
			return true
		}
	}
	return false
}

// ---------- results ----------

// Press applies key input as if typed, e.g. "rust", " " (toggle), "\r"
// (Enter) or "\x1b[B" (down arrow). It reports whether the user finished
// (done) or backed out (ErrCancelled).
func (c *Checklist) Press(keys string) (done bool, err error) {
	return c.handle([]byte(keys))
}

// Rows returns the text of each visible row, top to bottom.
func (c *Checklist) Rows() []string {
	var labels []string
	for _, r := range c.rows() {
		if r.add != nil {
			labels = append(labels, r.addText)
		} else {
			labels = append(labels, r.item.label)
		}
	}
	return labels
}

// Status is the footer text, e.g. "3 selected".
func (c *Checklist) Status() string { return c.summary(c) }

func (c *Checklist) checkedCount() int {
	return len(c.Checked())
}

// Checked returns the IDs of every checked item, in list order.
func (c *Checklist) Checked() []string {
	var ids []string
	for _, it := range c.items {
		if it.checked {
			ids = append(ids, it.id)
		}
	}
	return ids
}

// Changes returns the IDs checked and unchecked since the list opened.
func (c *Checklist) Changes() (checked, unchecked []string) {
	for _, it := range c.items {
		switch {
		case it.checked && !it.initial:
			checked = append(checked, it.id)
		case !it.checked && it.initial:
			unchecked = append(unchecked, it.id)
		}
	}
	return checked, unchecked
}

// ---------- editing ----------

func (c *Checklist) move(delta int) {
	if n := len(c.rows()); n > 0 {
		c.cursor = min(max(c.cursor+delta, 0), n-1)
	}
}

func (c *Checklist) setSearch(search []rune) {
	c.search = search
	c.cursor, c.scroll = 0, 0
}

// toggle flips the row under the cursor, or adds the offered item.
func (c *Checklist) toggle() {
	row, ok := c.current()
	switch {
	case !ok:
	case row.item != nil && row.item.checked:
		row.item.checked = false
	case c.maxChecked > 1 && c.checkedCount() >= c.maxChecked:
		c.message = fmt.Sprintf("you can pick at most %d", c.maxChecked)
	case c.maxChecked == 1:
		c.uncheckAll() // pick-one lists swap the choice
		c.check(row)
	default:
		c.check(row)
	}
}

func (c *Checklist) check(row checkRow) {
	if row.item != nil {
		row.item.checked = true
		return
	}
	row.add.checked = true
	c.items = append(c.items, row.add)
	c.setSearch(nil)
	c.cursor = len(c.items) - 1
}

func (c *Checklist) uncheckAll() {
	for _, it := range c.items {
		it.checked = false
	}
}

// toggleAll checks every visible item, or unchecks them all if they already are.
func (c *Checklist) toggleAll() {
	if c.maxChecked != 0 {
		c.message = fmt.Sprintf("you can pick at most %d", c.maxChecked)
		return
	}
	var visible []*checkItem
	allChecked := true
	for _, r := range c.rows() {
		if r.item != nil {
			visible = append(visible, r.item)
			allChecked = allChecked && r.item.checked
		}
	}
	for _, it := range visible {
		it.checked = !allChecked
	}
}

// confirm reports whether Enter should finish. When nothing is checked yet,
// Enter picks the row under the cursor; pick-one lists always take it.
func (c *Checklist) confirm() bool {
	if c.trackChanges {
		return true // "no changes" is a valid answer
	}
	if row, ok := c.current(); ok && c.maxChecked == 1 && (row.add != nil || !row.item.checked) {
		c.toggle()
	}
	if c.checkedCount() == 0 {
		c.toggle()
	}
	if c.checkedCount() == 0 {
		c.message = "select at least one with space"
		return false
	}
	return true
}
