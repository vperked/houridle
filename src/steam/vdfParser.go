package steam

import (
	"fmt"
	"strings"
)

// VDF is a parsed block of Valve's KeyValues text format, used by Steam's
// .vdf and .acf files. Keys are lowercased; values are a string or a VDF.
type VDF map[string]any

// Get walks nested blocks by key (case-insensitive). It returns nil if any is missing.
func (k VDF) Get(path ...string) VDF {
	cur := k
	for _, p := range path {
		next, ok := cur[strings.ToLower(p)].(VDF)
		if !ok {
			return nil
		}
		cur = next
	}
	return cur
}

// Str returns a string value by key (case-insensitive), or "" if missing.
func (k VDF) Str(key string) string {
	s, _ := k[strings.ToLower(key)].(string)
	return s
}

// maxVDFDepth caps how deeply blocks may nest. Steam's own files stay well
// under 16; the cap keeps a damaged or hostile file from recursing until the
// Go stack limit kills the process.
const maxVDFDepth = 64

// ParseVDF parses the text of a .vdf or .acf file.
func ParseVDF(data string) (VDF, error) {
	p := &vdfParser{s: data}
	root := VDF{}
	if err := p.block(root, 0); err != nil {
		return nil, err
	}
	return root, nil
}

type vdfParser struct {
	s   string
	pos int
}

func (p *vdfParser) skip() {
	for p.pos < len(p.s) {
		ch := p.s[p.pos]
		switch {
		case ch == ' ' || ch == '\t' || ch == '\r' || ch == '\n' || ch == 0xEF || ch == 0xBB || ch == 0xBF:
			p.pos++
		case ch == '/' && p.pos+1 < len(p.s) && p.s[p.pos+1] == '/':
			for p.pos < len(p.s) && p.s[p.pos] != '\n' {
				p.pos++
			}
		default:
			return
		}
	}
}

// token returns the next token. Braces come back as "{" / "}" with quoted=false.
func (p *vdfParser) token() (tok string, quoted bool, ok bool) {
	p.skip()
	if p.pos >= len(p.s) {
		return "", false, false
	}
	ch := p.s[p.pos]
	if ch == '{' || ch == '}' {
		p.pos++
		return string(ch), false, true
	}
	if ch == '"' {
		p.pos++
		var b strings.Builder
		for p.pos < len(p.s) {
			c := p.s[p.pos]
			if c == '\\' && p.pos+1 < len(p.s) {
				p.pos++
				switch p.s[p.pos] {
				case 'n':
					b.WriteByte('\n')
				case 't':
					b.WriteByte('\t')
				default:
					b.WriteByte(p.s[p.pos])
				}
				p.pos++
				continue
			}
			if c == '"' {
				p.pos++
				return b.String(), true, true
			}
			b.WriteByte(c)
			p.pos++
		}
		return b.String(), true, true
	}
	start := p.pos
	for p.pos < len(p.s) && !strings.ContainsRune(" \t\r\n{}\"", rune(p.s[p.pos])) {
		p.pos++
	}
	return p.s[start:p.pos], false, true
}

// block reads key/value pairs into into until the closing brace (or the end
// of the file at depth 0).
func (p *vdfParser) block(into VDF, depth int) error {
	if depth > maxVDFDepth {
		return fmt.Errorf("vdf: blocks nested more than %d deep", maxVDFDepth)
	}
	nested := depth > 0
	for {
		key, quoted, ok := p.token()
		if !ok {
			if nested {
				return fmt.Errorf("vdf: unexpected end of file")
			}
			return nil
		}
		if key == "}" && !quoted {
			if !nested {
				return fmt.Errorf("vdf: unexpected '}'")
			}
			return nil
		}
		val, vquoted, ok := p.token()
		if !ok {
			return fmt.Errorf("vdf: missing value for %q", key)
		}
		// Skip platform conditionals like [$WIN32].
		if strings.HasPrefix(val, "[$") && !vquoted {
			val, vquoted, ok = p.token()
			if !ok {
				return fmt.Errorf("vdf: missing value for %q", key)
			}
		}
		k := strings.ToLower(key)
		if val == "{" && !vquoted {
			child := VDF{}
			if err := p.block(child, depth+1); err != nil {
				return err
			}
			if existing, ok := into[k].(VDF); ok {
				for ck, cv := range child {
					existing[ck] = cv
				}
			} else {
				into[k] = child
			}
		} else {
			into[k] = val
		}
		// Skip a trailing conditional after a value.
		save := p.pos
		if t, q, ok := p.token(); !(ok && !q && strings.HasPrefix(t, "[$")) {
			p.pos = save
		}
	}
}
