package steamcm

import (
	"encoding/binary"
	"errors"
	"math"
	"strconv"
)

// KV is a node of Valve's binary KeyValues format, which Steam uses for
// achievement schemas. A node has either a value or children.
type KV struct {
	Key      string
	Value    string
	Children []*KV
}

// Child returns the first child with this key (case-sensitive, as Steam
// writes them), or nil.
func (k *KV) Child(key string) *KV {
	if k == nil {
		return nil
	}
	for _, c := range k.Children {
		if c.Key == key {
			return c
		}
	}
	return nil
}

// Str is the value of a child, or "".
func (k *KV) Str(key string) string {
	if c := k.Child(key); c != nil {
		return c.Value
	}
	return ""
}

// Binary KeyValues type tags.
const (
	kvNone    = 0
	kvString  = 1
	kvInt32   = 2
	kvFloat32 = 3
	kvPointer = 4
	kvWide    = 5
	kvColor   = 6
	kvUint64  = 7
	kvEnd     = 8
	kvInt64   = 10
	kvEndAlt  = 11
)

const maxKVDepth = 32

// ParseKV reads binary KeyValues into a root node holding the top-level
// entries as children.
func ParseKV(data []byte) (*KV, error) {
	root := &KV{}
	rest, err := parseKVChildren(root, data, 0)
	if err != nil {
		return nil, err
	}
	_ = rest // trailing bytes (a final end marker, padding) are fine
	return root, nil
}

func parseKVChildren(parent *KV, data []byte, depth int) ([]byte, error) {
	if depth > maxKVDepth {
		return nil, errors.New("achievement schema nested too deeply")
	}
	for len(data) > 0 {
		typ := data[0]
		data = data[1:]
		if typ == kvEnd || typ == kvEndAlt {
			return data, nil
		}
		key, rest, err := cString(data)
		if err != nil {
			return nil, err
		}
		data = rest
		node := &KV{Key: key}
		switch typ {
		case kvNone:
			if data, err = parseKVChildren(node, data, depth+1); err != nil {
				return nil, err
			}
		case kvString:
			if node.Value, data, err = cString(data); err != nil {
				return nil, err
			}
		case kvInt32, kvPointer, kvColor:
			if len(data) < 4 {
				return nil, errShortKV
			}
			node.Value = strconv.FormatInt(int64(int32(binary.LittleEndian.Uint32(data))), 10)
			data = data[4:]
		case kvFloat32:
			if len(data) < 4 {
				return nil, errShortKV
			}
			node.Value = strconv.FormatFloat(float64(math.Float32frombits(binary.LittleEndian.Uint32(data))), 'g', -1, 32)
			data = data[4:]
		case kvUint64:
			if len(data) < 8 {
				return nil, errShortKV
			}
			node.Value = strconv.FormatUint(binary.LittleEndian.Uint64(data), 10)
			data = data[8:]
		case kvInt64:
			if len(data) < 8 {
				return nil, errShortKV
			}
			node.Value = strconv.FormatInt(int64(binary.LittleEndian.Uint64(data)), 10)
			data = data[8:]
		case kvWide:
			return nil, errors.New("achievement schema uses wide strings, which houridle can't read")
		default:
			return nil, errors.New("achievement schema has an unknown value type")
		}
		parent.Children = append(parent.Children, node)
	}
	return data, nil
}

var errShortKV = errors.New("achievement schema is cut short")

func cString(data []byte) (string, []byte, error) {
	for i, b := range data {
		if b == 0 {
			return string(data[:i]), data[i+1:], nil
		}
	}
	return "", nil, errShortKV
}
