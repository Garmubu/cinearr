// Package yaml is a compact YAML reader covering the subset used by
// Cardigann indexer definitions: block mappings and sequences (including
// indentless sequences), flow collections, plain / quoted / block scalars and
// comments. Mappings keep their key order and allow duplicate keys, which the
// definitions rely on (search.fields).
package yaml

import (
	"errors"
	"fmt"
	"reflect"
	"strconv"
	"strings"
)

// Map is an ordered mapping that tolerates duplicate keys.
type Map struct {
	Keys []string
	Vals []any
}

// Get returns the first value for key.
func (m *Map) Get(key string) (any, bool) {
	for i, k := range m.Keys {
		if k == key {
			return m.Vals[i], true
		}
	}
	return nil, false
}

// Parse reads a YAML document into nested *Map, []any, string and nil.
func Parse(data []byte) (any, error) {
	text := strings.TrimPrefix(string(data), "\uFEFF")
	text = strings.ReplaceAll(text, "\r\n", "\n")
	text = strings.ReplaceAll(text, "\t", "    ")
	var lines []string
	for _, l := range strings.Split(text, "\n") {
		t := strings.TrimSpace(l)
		if t == "---" || t == "..." || strings.HasPrefix(t, "%YAML") {
			lines = append(lines, "")
			continue
		}
		lines = append(lines, l)
	}
	p := &parser{lines: lines}
	p.skipBlank()
	if p.i >= len(p.lines) {
		return nil, nil
	}
	v, err := p.parseNode(indentOf(p.lines[p.i]))
	if err != nil {
		return nil, err
	}
	p.skipBlank()
	if p.i < len(p.lines) {
		return nil, fmt.Errorf("yaml: line %d: unexpected content %q", p.i+1, strings.TrimSpace(p.lines[p.i]))
	}
	return v, nil
}

type parser struct {
	lines []string
	i     int
}

func indentOf(l string) int { return len(l) - len(strings.TrimLeft(l, " ")) }

func isBlank(l string) bool {
	t := strings.TrimSpace(l)
	return t == "" || strings.HasPrefix(t, "#")
}

func (p *parser) skipBlank() {
	for p.i < len(p.lines) && isBlank(p.lines[p.i]) {
		p.i++
	}
}

func (p *parser) errf(format string, a ...any) error {
	return fmt.Errorf("yaml: line %d: %s", p.i+1, fmt.Sprintf(format, a...))
}

func isSeqItem(t string) bool { return t == "-" || strings.HasPrefix(t, "- ") }

func (p *parser) parseNode(ind int) (any, error) {
	p.skipBlank()
	if p.i >= len(p.lines) {
		return nil, nil
	}
	l := p.lines[p.i]
	t := strings.TrimSpace(l)
	if isSeqItem(t) {
		return p.parseSeq(indentOf(l))
	}
	if _, _, ok := splitKey(t); ok {
		return p.parseMap(indentOf(l))
	}
	// lone scalar / flow value
	p.i++
	return p.parseValue(t, ind)
}

func (p *parser) parseSeq(ind int) (any, error) {
	var out []any
	for {
		p.skipBlank()
		if p.i >= len(p.lines) {
			break
		}
		l := p.lines[p.i]
		if indentOf(l) != ind || !isSeqItem(strings.TrimSpace(l)) {
			break
		}
		rest := strings.TrimPrefix(strings.TrimSpace(l), "-")
		if strings.TrimSpace(rest) == "" || strings.HasPrefix(strings.TrimSpace(rest), "#") {
			p.i++
			p.skipBlank()
			if p.i < len(p.lines) && indentOf(p.lines[p.i]) > ind {
				v, err := p.parseNode(indentOf(p.lines[p.i]))
				if err != nil {
					return nil, err
				}
				out = append(out, v)
			} else {
				out = append(out, nil)
			}
			continue
		}
		// "- key: value" or "- - x": re-indent the line and parse the nested
		// node at the column of its content.
		col := ind + 1 + (len(rest) - len(strings.TrimLeft(rest, " ")))
		content := strings.TrimSpace(rest)
		if _, _, ok := splitKey(content); ok || isSeqItem(content) {
			p.lines[p.i] = strings.Repeat(" ", col) + content
			v, err := p.parseNode(col)
			if err != nil {
				return nil, err
			}
			out = append(out, v)
			continue
		}
		p.i++
		v, err := p.parseValue(content, ind)
		if err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	return out, nil
}

func (p *parser) parseMap(ind int) (any, error) {
	m := &Map{}
	for {
		p.skipBlank()
		if p.i >= len(p.lines) {
			break
		}
		l := p.lines[p.i]
		if indentOf(l) != ind {
			if indentOf(l) > ind {
				return nil, p.errf("bad indentation")
			}
			break
		}
		t := strings.TrimSpace(l)
		if isSeqItem(t) {
			break
		}
		key, rest, ok := splitKey(t)
		if !ok {
			return nil, p.errf("expected a mapping key, got %q", t)
		}
		p.i++
		rest = strings.TrimSpace(stripComment(rest))
		var val any
		var err error
		if rest == "" {
			p.skipBlank()
			if p.i < len(p.lines) {
				nl := p.lines[p.i]
				ni := indentOf(nl)
				switch {
				case ni > ind:
					val, err = p.parseNode(ni)
				case ni == ind && isSeqItem(strings.TrimSpace(nl)):
					val, err = p.parseSeq(ind) // indentless sequence
				}
			}
		} else {
			val, err = p.parseValue(rest, ind)
		}
		if err != nil {
			return nil, err
		}
		m.Keys = append(m.Keys, key)
		m.Vals = append(m.Vals, val)
	}
	return m, nil
}

// parseValue parses the value found after "key:" (or "- "). Continuation
// lines (block scalars, multi-line flow / quoted / plain scalars) are read
// from p.lines starting at p.i.
func (p *parser) parseValue(v string, ind int) (any, error) {
	// strip tags and anchors we do not interpret
	for strings.HasPrefix(v, "!") || strings.HasPrefix(v, "&") {
		sp := strings.IndexByte(v, ' ')
		if sp < 0 {
			v = ""
			break
		}
		v = strings.TrimSpace(v[sp:])
	}
	if v == "" {
		return nil, nil
	}
	switch v[0] {
	case '|', '>':
		return p.blockScalar(v, ind)
	case '[', '{':
		full := v
		for !flowBalanced(full) && p.i < len(p.lines) {
			full += " " + strings.TrimSpace(p.lines[p.i])
			p.i++
		}
		fp := &flowParser{s: full}
		val, err := fp.value()
		if err != nil {
			return nil, p.errf("%v", err)
		}
		return val, nil
	case '"', '\'':
		full := v
		for !quoteClosed(full) && p.i < len(p.lines) {
			next := strings.TrimSpace(p.lines[p.i])
			if next == "" {
				full += "\n"
			} else {
				full += " " + next
			}
			p.i++
		}
		fp := &flowParser{s: full}
		s, err := fp.quoted()
		if err != nil {
			return nil, p.errf("%v", err)
		}
		return s, nil
	}
	// plain scalar, possibly continued on more-indented lines
	s := strings.TrimSpace(stripComment(v))
	for p.i < len(p.lines) {
		nl := p.lines[p.i]
		if isBlank(nl) || indentOf(nl) <= ind {
			break
		}
		nt := strings.TrimSpace(nl)
		if _, _, ok := splitKey(nt); ok || isSeqItem(nt) {
			break
		}
		s += " " + strings.TrimSpace(stripComment(nt))
		p.i++
	}
	if s == "~" || s == "null" || s == "Null" || s == "NULL" {
		return nil, nil
	}
	return s, nil
}

func (p *parser) blockScalar(header string, ind int) (any, error) {
	style := header[0]
	chomp := byte(0)
	for _, c := range header[1:] {
		if c == '-' || c == '+' {
			chomp = byte(c)
		}
	}
	var raw []string
	blockInd := -1
	for p.i < len(p.lines) {
		l := p.lines[p.i]
		if strings.TrimSpace(l) == "" {
			raw = append(raw, "")
			p.i++
			continue
		}
		li := indentOf(l)
		if li <= ind {
			break
		}
		if blockInd < 0 {
			blockInd = li
		}
		if li < blockInd {
			break
		}
		raw = append(raw, l[blockInd:])
		p.i++
	}
	// trailing blank lines are handled by chomping
	trailing := 0
	for len(raw) > 0 && raw[len(raw)-1] == "" {
		raw = raw[:len(raw)-1]
		trailing++
	}
	var s string
	if style == '|' {
		s = strings.Join(raw, "\n")
	} else {
		var b strings.Builder
		for i, l := range raw {
			if i > 0 {
				if l == "" || raw[i-1] == "" || strings.HasPrefix(l, " ") {
					b.WriteString("\n")
				} else {
					b.WriteString(" ")
				}
			}
			b.WriteString(l)
		}
		s = b.String()
	}
	switch chomp {
	case '-':
	case '+':
		s += "\n" + strings.Repeat("\n", trailing)
	default:
		if len(raw) > 0 {
			s += "\n"
		}
	}
	return s, nil
}

// splitKey splits "key: value" (outside quotes and brackets).
func splitKey(t string) (string, string, bool) {
	if t == "" || t[0] == '[' || t[0] == '{' || t[0] == '#' || isSeqItem(t) {
		return "", "", false
	}
	if t[0] == '"' || t[0] == '\'' {
		fp := &flowParser{s: t}
		k, err := fp.quoted()
		if err != nil {
			return "", "", false
		}
		r := strings.TrimLeft(fp.s[fp.i:], " ")
		if strings.HasPrefix(r, ":") && (len(r) == 1 || r[1] == ' ') {
			return k, r[1:], true
		}
		return "", "", false
	}
	for i := 0; i < len(t); i++ {
		if t[i] == '#' && i > 0 && t[i-1] == ' ' {
			return "", "", false
		}
		if t[i] == ':' && (i+1 == len(t) || t[i+1] == ' ') {
			return strings.TrimSpace(t[:i]), t[i+1:], true
		}
	}
	return "", "", false
}

// stripComment removes a trailing " # comment" from a plain value.
func stripComment(s string) string {
	inS, inD := false, false
	for i := 0; i < len(s); i++ {
		switch s[i] {
		case '\'':
			if !inD {
				inS = !inS
			}
		case '"':
			if !inS && (i == 0 || s[i-1] != '\\') {
				inD = !inD
			}
		case '#':
			if !inS && !inD && (i == 0 || s[i-1] == ' ') {
				return s[:i]
			}
		}
	}
	return s
}

func quoteClosed(s string) bool {
	fp := &flowParser{s: s}
	_, err := fp.quoted()
	return err == nil
}

func flowBalanced(s string) bool {
	depth := 0
	inS, inD := false, false
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case inD:
			switch c {
			case '\\':
				i++
			case '"':
				inD = false
			}
		case inS:
			if c == '\'' {
				if i+1 < len(s) && s[i+1] == '\'' {
					i++
				} else {
					inS = false
				}
			}
		case c == '"':
			inD = true
		case c == '\'':
			inS = true
		case c == '[' || c == '{':
			depth++
		case c == ']' || c == '}':
			depth--
		case c == '#' && i > 0 && s[i-1] == ' ':
			return depth <= 0
		}
	}
	return depth <= 0
}

type flowParser struct {
	s string
	i int
}

func (f *flowParser) ws() {
	for f.i < len(f.s) && (f.s[f.i] == ' ' || f.s[f.i] == '\n') {
		f.i++
	}
}

func (f *flowParser) value() (any, error) {
	f.ws()
	if f.i >= len(f.s) {
		return nil, errors.New("unexpected end of flow collection")
	}
	switch f.s[f.i] {
	case '[':
		f.i++
		var out []any
		for {
			f.ws()
			if f.i < len(f.s) && f.s[f.i] == ']' {
				f.i++
				return out, nil
			}
			v, err := f.value()
			if err != nil {
				return nil, err
			}
			out = append(out, v)
			f.ws()
			if f.i < len(f.s) && f.s[f.i] == ',' {
				f.i++
				continue
			}
			if f.i < len(f.s) && f.s[f.i] == ']' {
				f.i++
				return out, nil
			}
			return nil, fmt.Errorf("expected ',' or ']' in %q", f.s)
		}
	case '{':
		f.i++
		m := &Map{}
		for {
			f.ws()
			if f.i < len(f.s) && f.s[f.i] == '}' {
				f.i++
				return m, nil
			}
			var key string
			if f.s[f.i] == '"' || f.s[f.i] == '\'' {
				k, err := f.quoted()
				if err != nil {
					return nil, err
				}
				key = k
			} else {
				start := f.i
				for f.i < len(f.s) && !(f.s[f.i] == ':' && (f.i+1 >= len(f.s) || f.s[f.i+1] == ' ' || f.s[f.i+1] == ',' || f.s[f.i+1] == '}')) && f.s[f.i] != ',' && f.s[f.i] != '}' {
					f.i++
				}
				key = strings.TrimSpace(f.s[start:f.i])
			}
			f.ws()
			var val any
			if f.i < len(f.s) && f.s[f.i] == ':' {
				f.i++
				f.ws()
				if f.i < len(f.s) && f.s[f.i] != ',' && f.s[f.i] != '}' {
					v, err := f.value()
					if err != nil {
						return nil, err
					}
					val = v
				}
			}
			m.Keys = append(m.Keys, key)
			m.Vals = append(m.Vals, val)
			f.ws()
			if f.i < len(f.s) && f.s[f.i] == ',' {
				f.i++
				continue
			}
			if f.i < len(f.s) && f.s[f.i] == '}' {
				f.i++
				return m, nil
			}
			return nil, fmt.Errorf("expected ',' or '}' in %q", f.s)
		}
	case '"', '\'':
		return f.quoted()
	}
	start := f.i
	for f.i < len(f.s) && f.s[f.i] != ',' && f.s[f.i] != ']' && f.s[f.i] != '}' {
		if f.s[f.i] == '#' && f.i > 0 && f.s[f.i-1] == ' ' {
			break
		}
		f.i++
	}
	v := strings.TrimSpace(f.s[start:f.i])
	if v == "~" || v == "null" {
		return nil, nil
	}
	return v, nil
}

func (f *flowParser) quoted() (string, error) {
	f.ws()
	if f.i >= len(f.s) {
		return "", errors.New("expected quoted string")
	}
	q := f.s[f.i]
	f.i++
	var b strings.Builder
	for f.i < len(f.s) {
		c := f.s[f.i]
		if q == '\'' {
			if c == '\'' {
				if f.i+1 < len(f.s) && f.s[f.i+1] == '\'' {
					b.WriteByte('\'')
					f.i += 2
					continue
				}
				f.i++
				return b.String(), nil
			}
			b.WriteByte(c)
			f.i++
			continue
		}
		switch c {
		case '"':
			f.i++
			return b.String(), nil
		case '\\':
			if f.i+1 >= len(f.s) {
				return "", errors.New("bad escape")
			}
			e := f.s[f.i+1]
			f.i += 2
			switch e {
			case 'n':
				b.WriteByte('\n')
			case 't':
				b.WriteByte('\t')
			case 'r':
				b.WriteByte('\r')
			case '0':
				b.WriteByte(0)
			case ' ', '/', '\\', '"':
				b.WriteByte(e)
			case 'x', 'u', 'U':
				n := map[byte]int{'x': 2, 'u': 4, 'U': 8}[e]
				if f.i+n > len(f.s) {
					return "", errors.New("bad unicode escape")
				}
				v, err := strconv.ParseUint(f.s[f.i:f.i+n], 16, 32)
				if err != nil {
					return "", err
				}
				b.WriteRune(rune(v))
				f.i += n
			case 'N':
				b.WriteString("\u0085")
			case '_':
				b.WriteString(" ")
			default:
				b.WriteByte('\\')
				b.WriteByte(e)
			}
		default:
			b.WriteByte(c)
			f.i++
		}
	}
	return "", errors.New("unterminated quoted string")
}

// ---------------------------------------------------------------------------
// Decoding into Go values

// Unmarshaler lets a type decode itself from the raw tree.
type Unmarshaler interface {
	UnmarshalYAMLNode(node any) error
}

// Unmarshal parses data and decodes it into out (a pointer).
func Unmarshal(data []byte, out any) error {
	tree, err := Parse(data)
	if err != nil {
		return err
	}
	return Decode(tree, out)
}

// Decode maps a parsed tree onto out. Struct fields match keys
// case-insensitively (or the `yaml:"name"` tag); unknown keys are ignored.
func Decode(node any, out any) error {
	rv := reflect.ValueOf(out)
	if rv.Kind() != reflect.Pointer || rv.IsNil() {
		return errors.New("yaml: Decode needs a non-nil pointer")
	}
	return decode(node, rv.Elem())
}

var unmarshalerType = reflect.TypeOf((*Unmarshaler)(nil)).Elem()

func decode(node any, v reflect.Value) error {
	if v.CanAddr() && v.Addr().Type().Implements(unmarshalerType) {
		return v.Addr().Interface().(Unmarshaler).UnmarshalYAMLNode(node)
	}
	if node == nil {
		return nil
	}
	switch v.Kind() {
	case reflect.Pointer:
		if v.IsNil() {
			v.Set(reflect.New(v.Type().Elem()))
		}
		return decode(node, v.Elem())
	case reflect.Interface:
		v.Set(reflect.ValueOf(node))
		return nil
	case reflect.String:
		s, ok := node.(string)
		if !ok {
			return fmt.Errorf("yaml: expected scalar for %s", v.Type())
		}
		v.SetString(s)
	case reflect.Bool:
		s, _ := node.(string)
		switch strings.ToLower(s) {
		case "true", "yes", "on", "y":
			v.SetBool(true)
		default:
			v.SetBool(false)
		}
	case reflect.Int, reflect.Int64, reflect.Int32:
		s, _ := node.(string)
		n, err := strconv.ParseInt(strings.TrimSpace(s), 10, 64)
		if err != nil {
			f, ferr := strconv.ParseFloat(strings.TrimSpace(s), 64)
			if ferr != nil {
				return fmt.Errorf("yaml: invalid integer %q", s)
			}
			n = int64(f)
		}
		v.SetInt(n)
	case reflect.Float64, reflect.Float32:
		s, _ := node.(string)
		f, err := strconv.ParseFloat(strings.TrimSpace(s), 64)
		if err != nil {
			return fmt.Errorf("yaml: invalid number %q", s)
		}
		v.SetFloat(f)
	case reflect.Slice:
		items, ok := node.([]any)
		if !ok {
			items = []any{node} // a scalar where a list is expected
		}
		sl := reflect.MakeSlice(v.Type(), len(items), len(items))
		for i, it := range items {
			if err := decode(it, sl.Index(i)); err != nil {
				return err
			}
		}
		v.Set(sl)
	case reflect.Map:
		m, ok := node.(*Map)
		if !ok {
			return fmt.Errorf("yaml: expected mapping for %s", v.Type())
		}
		if v.IsNil() {
			v.Set(reflect.MakeMap(v.Type()))
		}
		for i, k := range m.Keys {
			ev := reflect.New(v.Type().Elem()).Elem()
			if err := decode(m.Vals[i], ev); err != nil {
				return err
			}
			v.SetMapIndex(reflect.ValueOf(k).Convert(v.Type().Key()), ev)
		}
	case reflect.Struct:
		m, ok := node.(*Map)
		if !ok {
			return fmt.Errorf("yaml: expected mapping for %s", v.Type())
		}
		t := v.Type()
		for i, k := range m.Keys {
			lk := strings.ToLower(strings.ReplaceAll(k, "_", ""))
			for f := 0; f < t.NumField(); f++ {
				sf := t.Field(f)
				if !sf.IsExported() {
					continue
				}
				name := strings.ToLower(sf.Name)
				if tag := sf.Tag.Get("yaml"); tag != "" {
					name = strings.ToLower(strings.Split(tag, ",")[0])
				}
				if name == lk {
					if err := decode(m.Vals[i], v.Field(f)); err != nil {
						return fmt.Errorf("%s: %w", k, err)
					}
					break
				}
			}
		}
	}
	return nil
}
