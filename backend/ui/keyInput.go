package ui

import (
	"time"
	"unicode/utf8"
)

// escapeWait is how long a lone Esc waits for the rest of an escape
// sequence (an arrow key split across reads, e.g. over a slow SSH link)
// before it counts as the Esc key itself.
const escapeWait = 50 * time.Millisecond

// maxEscapeLen bounds how long an unfinished escape sequence is held back.
const maxEscapeLen = 16

// keyBuffer turns console reads into whole keys. A read can end in the
// middle of an escape sequence or a UTF-8 character; that tail is held back
// until the next read completes it.
type keyBuffer struct{ pending []byte }

// add takes one read and returns the whole keys in it (plus any held-back
// start), keeping an unfinished tail for the next read.
func (k *keyBuffer) add(data []byte) []byte {
	k.pending = append(k.pending, data...)
	n := completeKeys(k.pending)
	keys := append([]byte(nil), k.pending[:n]...)
	k.pending = append(k.pending[:0], k.pending[n:]...)
	return keys
}

// waiting reports whether part of a key is held back.
func (k *keyBuffer) waiting() bool { return len(k.pending) > 0 }

// flush returns whatever is held back, as is: no more input came, so a lone
// Esc is the Esc key.
func (k *keyBuffer) flush() []byte {
	keys := k.pending
	k.pending = nil
	return keys
}

// next is the usual step after a read: the whole keys, and if a tail is held
// back and nothing more arrives within escapeWait, that tail too.
func (k *keyBuffer) next(data []byte) []byte {
	keys := k.add(data)
	if k.waiting() && !inputPending(escapeWait) {
		keys = append(keys, k.flush()...)
	}
	return keys
}

// completeKeys returns how many leading bytes of input are whole keys.
func completeKeys(input []byte) int {
	i := 0
	for i < len(input) {
		n := keyLength(input[i:])
		if n == 0 {
			break
		}
		i += n
	}
	return i
}

// keyLength is the length in bytes of the first key in input, or 0 when the
// key is cut off at the end of input.
func keyLength(input []byte) int {
	switch {
	case input[0] == escape:
		if len(input) == 1 {
			return 0 // Esc, or the start of a sequence: wait for more
		}
		if input[1] != '[' && input[1] != 'O' {
			return 1 // Esc followed by a plain key (alt+key)
		}
		for i := 2; i < len(input); i++ {
			if input[i] >= 0x40 && input[i] <= 0x7e { // final byte of the sequence
				return i + 1
			}
		}
		if len(input) >= maxEscapeLen {
			return len(input) // not a sequence we know; don't hold it forever
		}
		return 0
	case input[0] < utf8.RuneSelf:
		return 1
	case !utf8.FullRune(input):
		return 0
	default:
		_, size := utf8.DecodeRune(input)
		return size
	}
}
