package ui

import (
	"bytes"
	"unicode"
	"unicode/utf8"
)

// Single-byte keys, as the console sends them in raw mode.
const (
	ctrlA     = 0x01
	ctrlC     = 0x03
	backspace = 0x08
	ctrlU     = 0x15
	escape    = 0x1b
	deleteKey = 0x7f // what most consoles send for Backspace
)

// Multi-byte keys (VT escape sequences).
var (
	keyUp       = []byte("\x1b[A")
	keyDown     = []byte("\x1b[B")
	keyPageUp   = []byte("\x1b[5~")
	keyPageDown = []byte("\x1b[6~")
)

// handle applies a chunk of key input, which can hold several keys (e.g. a
// paste). It returns done=true when the user confirms, or ErrCancelled.
func (c *Checklist) handle(input []byte) (done bool, err error) {
	c.message = ""
	for len(input) > 0 {
		used, done, err := c.handleKey(input)
		if done || err != nil {
			return done, err
		}
		input = input[used:]
	}
	return false, nil
}

// handleKey applies the first key in input and returns how many bytes it used.
func (c *Checklist) handleKey(input []byte) (used int, done bool, err error) {
	moves := []struct {
		key   []byte
		delta int
	}{
		{keyUp, -1},
		{keyDown, 1},
		{keyPageUp, -c.pageSize},
		{keyPageDown, c.pageSize},
	}
	for _, m := range moves {
		if bytes.HasPrefix(input, m.key) {
			c.move(m.delta)
			return len(m.key), false, nil
		}
	}

	switch input[0] {
	case escape:
		if len(input) == 1 {
			return 1, true, ErrCancelled // Esc on its own
		}
		return len(input) - len(skipEscape(input)), false, nil // some other special key
	case ctrlC:
		return 1, true, ErrCancelled
	case '\r', '\n':
		return 1, c.confirm(), nil
	case ' ':
		c.toggle()
	case ctrlA:
		c.toggleAll()
	case backspace, deleteKey:
		if len(c.search) > 0 {
			c.setSearch(c.search[:len(c.search)-1])
		}
	case ctrlU:
		c.setSearch(nil)
	default:
		r, size := utf8.DecodeRune(input)
		if r != utf8.RuneError && unicode.IsPrint(r) {
			c.setSearch(append(c.search, r))
		}
		return size, false, nil
	}
	return 1, false, nil
}

// skipEscape drops one escape sequence we don't use (e.g. ctrl+arrow).
func skipEscape(input []byte) []byte {
	if len(input) < 2 || (input[1] != '[' && input[1] != 'O') {
		return input[1:]
	}
	for i := 2; i < len(input); i++ {
		if input[i] >= 0x40 && input[i] <= 0x7e { // final byte of the sequence
			return input[i+1:]
		}
	}
	return nil
}
