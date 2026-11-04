package steamauth

import (
	"math"

	"google.golang.org/protobuf/encoding/protowire"
)

// Steam's messages are protobufs. Only a handful of fields are used, so they
// are written and read directly with protowire (the protobuf library's
// low-level API) instead of generating code for Valve's large .proto files.

// Encoder builds one protobuf message.
type Encoder struct{ b []byte }

func (e *Encoder) Varint(num protowire.Number, v uint64) *Encoder {
	e.b = protowire.AppendTag(e.b, num, protowire.VarintType)
	e.b = protowire.AppendVarint(e.b, v)
	return e
}

func (e *Encoder) Bool(num protowire.Number, v bool) *Encoder {
	if v {
		return e.Varint(num, 1)
	}
	return e.Varint(num, 0)
}

func (e *Encoder) Fixed64(num protowire.Number, v uint64) *Encoder {
	e.b = protowire.AppendTag(e.b, num, protowire.Fixed64Type)
	e.b = protowire.AppendFixed64(e.b, v)
	return e
}

func (e *Encoder) String(num protowire.Number, s string) *Encoder {
	return e.Bytes(num, []byte(s))
}

func (e *Encoder) Bytes(num protowire.Number, v []byte) *Encoder {
	e.b = protowire.AppendTag(e.b, num, protowire.BytesType)
	e.b = protowire.AppendBytes(e.b, v)
	return e
}

func (e *Encoder) Message(num protowire.Number, m *Encoder) *Encoder { return e.Bytes(num, m.b) }

// Out returns the encoded message.
func (e *Encoder) Out() []byte { return e.b }

// Decoded is a parsed message: every field's values, by field number.
type Decoded struct {
	fields map[protowire.Number][]field
}

type field struct {
	u uint64 // varint, fixed32 or fixed64
	b []byte // length-delimited
}

// Decode parses a protobuf message. Unknown fields are kept, not an error.
func Decode(data []byte) (*Decoded, error) {
	d := &Decoded{fields: map[protowire.Number][]field{}}
	for len(data) > 0 {
		num, typ, n := protowire.ConsumeTag(data)
		if n < 0 {
			return nil, protowire.ParseError(n)
		}
		data = data[n:]
		var f field
		switch typ {
		case protowire.VarintType:
			f.u, n = protowire.ConsumeVarint(data)
		case protowire.Fixed32Type:
			var v uint32
			v, n = protowire.ConsumeFixed32(data)
			f.u = uint64(v)
		case protowire.Fixed64Type:
			f.u, n = protowire.ConsumeFixed64(data)
		case protowire.BytesType:
			f.b, n = protowire.ConsumeBytes(data)
		default:
			n = protowire.ConsumeFieldValue(num, typ, data)
		}
		if n < 0 {
			return nil, protowire.ParseError(n)
		}
		data = data[n:]
		d.fields[num] = append(d.fields[num], f)
	}
	return d, nil
}

func (d *Decoded) first(num protowire.Number) field {
	if fs := d.fields[num]; len(fs) > 0 {
		return fs[0]
	}
	return field{}
}

// Uint returns a varint or fixed-width field, or 0 when absent.
func (d *Decoded) Uint(num protowire.Number) uint64 { return d.first(num).u }

// Int returns a signed varint field (int32 values arrive sign-extended).
func (d *Decoded) Int(num protowire.Number) int64 { return int64(d.first(num).u) }

func (d *Decoded) Bool(num protowire.Number) bool { return d.first(num).u != 0 }

// Float returns a 32-bit float field.
func (d *Decoded) Float(num protowire.Number) float32 {
	return math.Float32frombits(uint32(d.first(num).u))
}

func (d *Decoded) Str(num protowire.Number) string { return string(d.first(num).b) }

func (d *Decoded) Raw(num protowire.Number) []byte { return d.first(num).b }

// Has reports whether the field was present.
func (d *Decoded) Has(num protowire.Number) bool { return len(d.fields[num]) > 0 }

// Messages returns every value of a repeated message field, parsed.
func (d *Decoded) Messages(num protowire.Number) []*Decoded {
	var out []*Decoded
	for _, f := range d.fields[num] {
		if m, err := Decode(f.b); err == nil {
			out = append(out, m)
		}
	}
	return out
}

// Uints returns every value of a repeated varint field, whether sent one
// per entry or packed together.
func (d *Decoded) Uints(num protowire.Number) []uint64 {
	var out []uint64
	for _, f := range d.fields[num] {
		if f.b == nil {
			out = append(out, f.u)
			continue
		}
		for b := f.b; len(b) > 0; { // packed
			v, n := protowire.ConsumeVarint(b)
			if n < 0 {
				break
			}
			out = append(out, v)
			b = b[n:]
		}
	}
	return out
}
