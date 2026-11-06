package ui

import (
	"time"
	"unicode/utf8"
)

const escapeWait = 50 * time.Millisecond

const maxEscapeLen = 16

type keyBuffer struct{ pending []byte }

func (k *keyBuffer) add(data []byte) []byte {
	k.pending = append(k.pending, data...)
	n := completeKeys(k.pending)
	keys := append([]byte(nil), k.pending[:n]...)
	k.pending = append(k.pending[:0], k.pending[n:]...)
	return keys
}

func (k *keyBuffer) waiting() bool { return len(k.pending) > 0 }

func (k *keyBuffer) flush() []byte {
	keys := k.pending
	k.pending = nil
	return keys
}

func (k *keyBuffer) next(data []byte) []byte {
	keys := k.add(data)
	if k.waiting() && !inputPending(escapeWait) {
		keys = append(keys, k.flush()...)
	}
	return keys
}

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

func keyLength(input []byte) int {
	switch {
	case input[0] == escape:
		if len(input) == 1 {
			return 0
		}
		if input[1] != '[' && input[1] != 'O' {
			return 1
		}
		for i := 2; i < len(input); i++ {
			if input[i] >= 0x40 && input[i] <= 0x7e {
				return i + 1
			}
		}
		if len(input) >= maxEscapeLen {
			return len(input)
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
