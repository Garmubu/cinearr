// Package rx is a small backtracking regular-expression engine compatible with
// the .NET dialect used by Radarr, Sonarr and Prowlarr (and by the Cardigann
// indexer definitions). Go's standard regexp package (RE2) does not support
// lookahead, lookbehind, backreferences or atomic groups, which those patterns
// rely on heavily, so the original expressions are kept verbatim and run here.
//
// Supported: literals and escapes, character classes (with \d \w \s \p{..}),
// groups (capturing, named, non-capturing, atomic), lookahead / lookbehind
// (variable length), backreferences (\1, \k<name>), greedy and lazy
// quantifiers, anchors, inline options ((?i) (?-i:...) etc.) and the
// IgnoreCase / Multiline / Singleline / IgnorePatternWhitespace options.
package rx

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"unicode"
)

// Options mirror System.Text.RegularExpressions.RegexOptions.
type Options uint16

const (
	IgnoreCase Options = 1 << iota
	Multiline
	Singleline
	IgnorePatternWhitespace
	ExplicitCapture
)

// maxSteps bounds the work done by one search so that a pathological pattern
// cannot hang the application. A search that exceeds it reports "no match".
const maxSteps = 4_000_000

type kind uint8

const (
	kEmpty kind = iota
	kChar
	kAny
	kClass
	kBol
	kEol
	kBeginText
	kEndText
	kEndTextZ
	kWordB
	kNotWordB
	kConcat
	kAlt
	kRepeat
	kGroup
	kLook
	kAtomic
	kBackref
)

type node struct {
	kind      kind
	r         rune
	fold      bool
	cls       *charClass
	subs      []*node
	sub       *node
	min, max  int
	greedy    bool
	group     int
	name      string
	behind    bool
	neg       bool
	multiline bool
	dotall    bool
	refName   string
	refNum    int
}

// Regexp is a compiled expression. It is safe for concurrent use.
type Regexp struct {
	pattern    string
	opts       Options
	root       *node
	numGroups  int // including group 0
	names      map[string]int
	groupNames []string
	anchored   bool
}

// Compile parses pattern with the given options.
func Compile(pattern string, opts Options) (*Regexp, error) {
	p := &parser{src: []rune(pattern), flags: opts}
	root, err := p.parse()
	if err != nil {
		return nil, fmt.Errorf("rx: %w in %q", err, pattern)
	}
	re := &Regexp{pattern: pattern, opts: opts, root: root, names: map[string]int{}}
	// .NET numbering: unnamed groups first (left to right), then named groups.
	n := 1
	for _, g := range p.groups {
		if g.name == "" {
			g.group = n
			n++
		}
	}
	for _, g := range p.groups {
		if g.name == "" {
			continue
		}
		if num, err := strconv.Atoi(g.name); err == nil {
			g.group = num
			if num >= n {
				n = num + 1
			}
			continue
		}
		if idx, ok := re.names[g.name]; ok {
			g.group = idx
			continue
		}
		re.names[g.name] = n
		g.group = n
		n++
	}
	re.numGroups = n
	re.groupNames = make([]string, n)
	for name, idx := range re.names {
		re.groupNames[idx] = name
	}
	for _, b := range p.backrefs {
		if b.refName != "" {
			idx, ok := re.names[b.refName]
			if !ok {
				if num, err := strconv.Atoi(b.refName); err == nil {
					idx, ok = num, true
				}
			}
			if !ok {
				return nil, fmt.Errorf("rx: reference to undefined group %q in %q", b.refName, pattern)
			}
			b.refNum = idx
		}
		if b.refNum >= n {
			return nil, fmt.Errorf("rx: reference to undefined group %d in %q", b.refNum, pattern)
		}
	}
	re.anchored = startsAnchored(root)
	return re, nil
}

// MustCompile is like Compile but panics on error.
func MustCompile(pattern string, opts Options) *Regexp {
	re, err := Compile(pattern, opts)
	if err != nil {
		panic(err)
	}
	return re
}

var cache sync.Map

type cacheKey struct {
	p string
	o Options
}

// Cached compiles pattern once and reuses it (used for patterns coming from
// indexer definitions).
func Cached(pattern string, opts Options) (*Regexp, error) {
	k := cacheKey{pattern, opts}
	if v, ok := cache.Load(k); ok {
		return v.(*Regexp), nil
	}
	re, err := Compile(pattern, opts)
	if err != nil {
		return nil, err
	}
	cache.Store(k, re)
	return re, nil
}

func startsAnchored(n *node) bool {
	switch n.kind {
	case kBeginText:
		return true
	case kBol:
		return !n.multiline
	case kConcat:
		return len(n.subs) > 0 && startsAnchored(n.subs[0])
	case kGroup, kAtomic:
		return startsAnchored(n.sub)
	case kAlt:
		for _, s := range n.subs {
			if !startsAnchored(s) {
				return false
			}
		}
		return len(n.subs) > 0
	}
	return false
}

func (re *Regexp) String() string { return re.pattern }

// GroupIndex returns the index of a named group or -1.
func (re *Regexp) GroupIndex(name string) int {
	if i, ok := re.names[name]; ok {
		return i
	}
	if i, err := strconv.Atoi(name); err == nil && i >= 0 && i < re.numGroups {
		return i
	}
	return -1
}

// ---------------------------------------------------------------------------
// Parser

type parser struct {
	src      []rune
	pos      int
	flags    Options
	groups   []*node
	backrefs []*node
}

var errUnexpectedEnd = errors.New("unexpected end of pattern")

func (p *parser) parse() (*node, error) {
	n, err := p.parseAlt()
	if err != nil {
		return nil, err
	}
	if p.pos < len(p.src) {
		return nil, fmt.Errorf("unexpected %q at %d", p.src[p.pos], p.pos)
	}
	return n, nil
}

func (p *parser) eof() bool { return p.pos >= len(p.src) }

func (p *parser) peek() rune {
	if p.pos < len(p.src) {
		return p.src[p.pos]
	}
	return 0
}

func (p *parser) skipWS() {
	if p.flags&IgnorePatternWhitespace == 0 {
		return
	}
	for p.pos < len(p.src) {
		c := p.src[p.pos]
		if unicode.IsSpace(c) {
			p.pos++
			continue
		}
		if c == '#' {
			for p.pos < len(p.src) && p.src[p.pos] != '\n' {
				p.pos++
			}
			continue
		}
		break
	}
}

func (p *parser) parseAlt() (*node, error) {
	saved := p.flags
	defer func() { p.flags = saved }()
	var branches []*node
	for {
		seq, err := p.parseSeq()
		if err != nil {
			return nil, err
		}
		branches = append(branches, seq)
		if p.peek() == '|' && !p.eof() {
			p.pos++
			continue
		}
		break
	}
	if len(branches) == 1 {
		return branches[0], nil
	}
	return &node{kind: kAlt, subs: branches}, nil
}

func (p *parser) parseSeq() (*node, error) {
	var items []*node
	for {
		p.skipWS()
		if p.eof() || p.peek() == '|' || p.peek() == ')' {
			break
		}
		atom, err := p.parseAtom()
		if err != nil {
			return nil, err
		}
		if atom == nil { // inline option or comment
			continue
		}
		for {
			p.skipWS()
			q, ok := p.parseQuantifier(atom)
			if !ok {
				break
			}
			atom = q
		}
		items = append(items, atom)
	}
	switch len(items) {
	case 0:
		return &node{kind: kEmpty}, nil
	case 1:
		return items[0], nil
	}
	return &node{kind: kConcat, subs: items}, nil
}

func (p *parser) parseQuantifier(atom *node) (*node, bool) {
	if p.eof() {
		return nil, false
	}
	var min, max int
	switch p.peek() {
	case '*':
		min, max = 0, -1
		p.pos++
	case '+':
		min, max = 1, -1
		p.pos++
	case '?':
		min, max = 0, 1
		p.pos++
	case '{':
		mn, mx, l, ok := p.scanBraces(p.pos)
		if !ok {
			return nil, false
		}
		min, max = mn, mx
		p.pos += l
	default:
		return nil, false
	}
	greedy := true
	if p.peek() == '?' && !p.eof() {
		greedy = false
		p.pos++
	}
	switch atom.kind {
	case kBol, kEol, kBeginText, kEndText, kEndTextZ, kWordB, kNotWordB:
		// quantified anchors are legal in .NET; {0,..} makes them optional
	}
	return &node{kind: kRepeat, sub: atom, min: min, max: max, greedy: greedy}, true
}

// scanBraces recognises {n}, {n,} and {n,m}. Anything else is a literal '{'.
func (p *parser) scanBraces(at int) (int, int, int, bool) {
	i := at + 1
	readInt := func() (int, bool) {
		start := i
		for i < len(p.src) && p.src[i] >= '0' && p.src[i] <= '9' {
			i++
		}
		if i == start {
			return 0, false
		}
		v, err := strconv.Atoi(string(p.src[start:i]))
		return v, err == nil
	}
	mn, ok := readInt()
	if !ok {
		return 0, 0, 0, false
	}
	mx := mn
	if i < len(p.src) && p.src[i] == ',' {
		i++
		if i < len(p.src) && p.src[i] == '}' {
			mx = -1
		} else {
			v, ok := readInt()
			if !ok {
				return 0, 0, 0, false
			}
			mx = v
		}
	}
	if i >= len(p.src) || p.src[i] != '}' {
		return 0, 0, 0, false
	}
	return mn, mx, i + 1 - at, true
}

func (p *parser) fold() bool { return p.flags&IgnoreCase != 0 }

func (p *parser) parseAtom() (*node, error) {
	c := p.src[p.pos]
	switch c {
	case '(':
		p.pos++
		return p.parseGroup()
	case '[':
		p.pos++
		cls, err := p.parseClass()
		if err != nil {
			return nil, err
		}
		return &node{kind: kClass, cls: cls, fold: p.fold()}, nil
	case '.':
		p.pos++
		return &node{kind: kAny, dotall: p.flags&Singleline != 0}, nil
	case '^':
		p.pos++
		return &node{kind: kBol, multiline: p.flags&Multiline != 0}, nil
	case '$':
		p.pos++
		return &node{kind: kEol, multiline: p.flags&Multiline != 0}, nil
	case '\\':
		p.pos++
		return p.parseEscape()
	case '*', '+', '?':
		return nil, fmt.Errorf("quantifier %q without operand at %d", c, p.pos)
	}
	p.pos++
	return &node{kind: kChar, r: c, fold: p.fold()}, nil
}

func (p *parser) parseGroup() (*node, error) {
	if p.peek() != '?' || p.eof() {
		capture := p.flags&ExplicitCapture == 0
		sub, err := p.parseAlt()
		if err != nil {
			return nil, err
		}
		if err := p.expectClose(); err != nil {
			return nil, err
		}
		if !capture {
			return sub, nil
		}
		g := &node{kind: kGroup, sub: sub}
		p.groups = append(p.groups, g)
		return g, nil
	}
	p.pos++ // '?'
	if p.eof() {
		return nil, errUnexpectedEnd
	}
	c := p.src[p.pos]
	switch {
	case c == ':':
		p.pos++
		sub, err := p.parseAlt()
		if err != nil {
			return nil, err
		}
		return sub, p.expectClose()
	case c == '=' || c == '!':
		p.pos++
		sub, err := p.parseAlt()
		if err != nil {
			return nil, err
		}
		return &node{kind: kLook, sub: sub, neg: c == '!'}, p.expectClose()
	case c == '>':
		p.pos++
		sub, err := p.parseAlt()
		if err != nil {
			return nil, err
		}
		return &node{kind: kAtomic, sub: sub}, p.expectClose()
	case c == '#':
		for !p.eof() && p.src[p.pos] != ')' {
			p.pos++
		}
		if err := p.expectClose(); err != nil {
			return nil, err
		}
		return nil, nil
	case c == '<' && p.pos+1 < len(p.src) && (p.src[p.pos+1] == '=' || p.src[p.pos+1] == '!'):
		neg := p.src[p.pos+1] == '!'
		p.pos += 2
		sub, err := p.parseAlt()
		if err != nil {
			return nil, err
		}
		return &node{kind: kLook, sub: sub, neg: neg, behind: true}, p.expectClose()
	case c == '<' || c == '\'' || (c == 'P' && p.pos+1 < len(p.src) && p.src[p.pos+1] == '<'):
		if c == 'P' {
			p.pos++
			c = '<'
		}
		closer := '>'
		if c == '\'' {
			closer = '\''
		}
		p.pos++
		start := p.pos
		for !p.eof() && p.src[p.pos] != closer {
			p.pos++
		}
		if p.eof() {
			return nil, errUnexpectedEnd
		}
		name := string(p.src[start:p.pos])
		p.pos++
		if strings.Contains(name, "-") {
			return nil, fmt.Errorf("balancing groups are not supported (%s)", name)
		}
		sub, err := p.parseAlt()
		if err != nil {
			return nil, err
		}
		if err := p.expectClose(); err != nil {
			return nil, err
		}
		g := &node{kind: kGroup, sub: sub, name: name}
		p.groups = append(p.groups, g)
		return g, nil
	case c == '(':
		return nil, errors.New("conditional expressions are not supported")
	}
	// inline options: (?imsx-imsx) or (?imsx-imsx:...)
	flags := p.flags
	on := true
	for !p.eof() {
		c = p.src[p.pos]
		var bit Options
		switch c {
		case 'i':
			bit = IgnoreCase
		case 'm':
			bit = Multiline
		case 's':
			bit = Singleline
		case 'x':
			bit = IgnorePatternWhitespace
		case 'n':
			bit = ExplicitCapture
		case '-':
			on = false
			p.pos++
			continue
		case ')':
			p.pos++
			p.flags = flags // applies to the rest of the enclosing group
			return nil, nil
		case ':':
			p.pos++
			saved := p.flags
			p.flags = flags
			sub, err := p.parseAlt()
			p.flags = saved
			if err != nil {
				return nil, err
			}
			return sub, p.expectClose()
		default:
			return nil, fmt.Errorf("unknown group construct (?%c at %d", c, p.pos)
		}
		if on {
			flags |= bit
		} else {
			flags &^= bit
		}
		p.pos++
	}
	return nil, errUnexpectedEnd
}

// expectClose consumes the ')' that ends a group.
func (p *parser) expectClose() error {
	if p.eof() {
		return errors.New("missing ')'")
	}
	if p.src[p.pos] != ')' {
		return fmt.Errorf("expected ')' at %d, got %q", p.pos, p.src[p.pos])
	}
	p.pos++
	return nil
}

func (p *parser) parseEscape() (*node, error) {
	if p.eof() {
		return nil, errors.New("trailing backslash")
	}
	c := p.src[p.pos]
	p.pos++
	switch c {
	case 'd', 'D', 'w', 'W', 's', 'S':
		cls := &charClass{}
		cls.addEscape(c)
		return &node{kind: kClass, cls: cls}, nil
	case 'p', 'P':
		tab, err := p.parseUnicodeName()
		if err != nil {
			return nil, err
		}
		cls := &charClass{}
		cls.tables = append(cls.tables, classTable{tab: tab, neg: c == 'P'})
		return &node{kind: kClass, cls: cls, fold: p.fold()}, nil
	case 'b':
		return &node{kind: kWordB}, nil
	case 'B':
		return &node{kind: kNotWordB}, nil
	case 'A', 'G':
		return &node{kind: kBeginText}, nil
	case 'z':
		return &node{kind: kEndText}, nil
	case 'Z':
		return &node{kind: kEndTextZ}, nil
	case 'k':
		if p.eof() || (p.peek() != '<' && p.peek() != '\'') {
			return &node{kind: kChar, r: 'k', fold: p.fold()}, nil
		}
		closer := '>'
		if p.peek() == '\'' {
			closer = '\''
		}
		p.pos++
		start := p.pos
		for !p.eof() && p.src[p.pos] != closer {
			p.pos++
		}
		if p.eof() {
			return nil, errUnexpectedEnd
		}
		name := string(p.src[start:p.pos])
		p.pos++
		b := &node{kind: kBackref, refName: name, fold: p.fold()}
		p.backrefs = append(p.backrefs, b)
		return b, nil
	}
	if c >= '1' && c <= '9' {
		start := p.pos - 1
		for !p.eof() && p.peek() >= '0' && p.peek() <= '9' {
			p.pos++
		}
		num, _ := strconv.Atoi(string(p.src[start:p.pos]))
		b := &node{kind: kBackref, refNum: num, fold: p.fold()}
		p.backrefs = append(p.backrefs, b)
		return b, nil
	}
	r, err := p.charEscape(c)
	if err != nil {
		return nil, err
	}
	return &node{kind: kChar, r: r, fold: p.fold()}, nil
}

// charEscape decodes single-character escapes (c has already been consumed).
func (p *parser) charEscape(c rune) (rune, error) {
	switch c {
	case 't':
		return '\t', nil
	case 'n':
		return '\n', nil
	case 'r':
		return '\r', nil
	case 'f':
		return '\f', nil
	case 'v':
		return '\v', nil
	case 'a':
		return '\a', nil
	case 'e':
		return 0x1b, nil
	case '0':
		v := 0
		for i := 0; i < 2 && !p.eof() && p.peek() >= '0' && p.peek() <= '7'; i++ {
			v = v*8 + int(p.peek()-'0')
			p.pos++
		}
		return rune(v), nil
	case 'x':
		return p.hex(2)
	case 'u':
		return p.hex(4)
	case 'c':
		if p.eof() {
			return 0, errUnexpectedEnd
		}
		r := unicode.ToUpper(p.src[p.pos])
		p.pos++
		return r ^ 0x40, nil
	}
	return c, nil
}

func (p *parser) hex(n int) (rune, error) {
	if p.pos+n > len(p.src) {
		return 0, errUnexpectedEnd
	}
	v, err := strconv.ParseUint(string(p.src[p.pos:p.pos+n]), 16, 32)
	if err != nil {
		return 0, fmt.Errorf("invalid hex escape at %d", p.pos)
	}
	p.pos += n
	return rune(v), nil
}

func (p *parser) parseUnicodeName() (*unicode.RangeTable, error) {
	if p.peek() != '{' {
		return nil, errors.New(`expected '{' after \p`)
	}
	p.pos++
	start := p.pos
	for !p.eof() && p.src[p.pos] != '}' {
		p.pos++
	}
	if p.eof() {
		return nil, errUnexpectedEnd
	}
	name := string(p.src[start:p.pos])
	p.pos++
	if t, ok := unicode.Categories[name]; ok {
		return t, nil
	}
	if t, ok := unicode.Scripts[name]; ok {
		return t, nil
	}
	if t, ok := blockTable(name); ok {
		return t, nil
	}
	if strings.HasPrefix(name, "Is") {
		if t, ok := unicode.Scripts[strings.TrimPrefix(name, "Is")]; ok {
			return t, nil
		}
	}
	return nil, fmt.Errorf("unknown unicode class %q", name)
}

func (p *parser) parseClass() (*charClass, error) {
	cls := &charClass{}
	if p.peek() == '^' && !p.eof() {
		cls.neg = true
		p.pos++
	}
	first := true
	for {
		if p.eof() {
			return nil, errors.New("missing ']'")
		}
		c := p.src[p.pos]
		if c == ']' && !first {
			p.pos++
			return cls, nil
		}
		first = false
		p.pos++
		var lo rune
		if c == '\\' {
			if p.eof() {
				return nil, errUnexpectedEnd
			}
			e := p.src[p.pos]
			p.pos++
			switch e {
			case 'd', 'D', 'w', 'W', 's', 'S':
				cls.addEscape(e)
				continue
			case 'p', 'P':
				tab, err := p.parseUnicodeName()
				if err != nil {
					return nil, err
				}
				cls.tables = append(cls.tables, classTable{tab: tab, neg: e == 'P'})
				continue
			case 'b':
				lo = '\b'
			default:
				r, err := p.charEscape(e)
				if err != nil {
					return nil, err
				}
				lo = r
			}
		} else {
			// a POSIX-like "[:alpha:]" is not .NET: '[' is literal
			lo = c
		}
		// range?
		if p.pos+1 < len(p.src) && p.src[p.pos] == '-' && p.src[p.pos+1] != ']' {
			if p.src[p.pos+1] == '[' { // .NET class subtraction: not supported, treat '-' literally
				cls.ranges = append(cls.ranges, [2]rune{lo, lo})
				continue
			}
			save := p.pos
			p.pos++
			hc := p.src[p.pos]
			p.pos++
			var hi rune
			if hc == '\\' {
				if p.eof() {
					return nil, errUnexpectedEnd
				}
				e := p.src[p.pos]
				p.pos++
				if strings.ContainsRune("dDwWsSpP", e) {
					// "a-\d": the dash is literal
					p.pos = save
					cls.ranges = append(cls.ranges, [2]rune{lo, lo})
					continue
				}
				if e == 'b' {
					hi = '\b'
				} else {
					r, err := p.charEscape(e)
					if err != nil {
						return nil, err
					}
					hi = r
				}
			} else {
				hi = hc
			}
			if hi < lo {
				return nil, fmt.Errorf("invalid range %c-%c", lo, hi)
			}
			cls.ranges = append(cls.ranges, [2]rune{lo, hi})
			continue
		}
		cls.ranges = append(cls.ranges, [2]rune{lo, lo})
	}
}

// ---------------------------------------------------------------------------
// Character classes

type classTable struct {
	tab *unicode.RangeTable
	neg bool
}

type charClass struct {
	neg    bool
	ranges [][2]rune
	fns    []func(rune) bool
	tables []classTable
}

func isWordChar(r rune) bool {
	return r == '_' || unicode.IsLetter(r) || unicode.IsDigit(r) || unicode.Is(unicode.Mn, r) || unicode.Is(unicode.Pc, r)
}

func (c *charClass) addEscape(e rune) {
	switch e {
	case 'd':
		c.fns = append(c.fns, unicode.IsDigit)
	case 'D':
		c.fns = append(c.fns, func(r rune) bool { return !unicode.IsDigit(r) })
	case 'w':
		c.fns = append(c.fns, isWordChar)
	case 'W':
		c.fns = append(c.fns, func(r rune) bool { return !isWordChar(r) })
	case 's':
		c.fns = append(c.fns, unicode.IsSpace)
	case 'S':
		c.fns = append(c.fns, func(r rune) bool { return !unicode.IsSpace(r) })
	}
}

func (c *charClass) raw(r rune) bool {
	for _, rg := range c.ranges {
		if r >= rg[0] && r <= rg[1] {
			return true
		}
	}
	for _, f := range c.fns {
		if f(r) {
			return true
		}
	}
	for _, t := range c.tables {
		if unicode.Is(t.tab, r) != t.neg {
			return true
		}
	}
	return false
}

func (c *charClass) matches(r rune, fold bool) bool {
	ok := c.raw(r)
	if !ok && fold {
		for f := unicode.SimpleFold(r); f != r; f = unicode.SimpleFold(f) {
			if c.raw(f) {
				ok = true
				break
			}
		}
	}
	return ok != c.neg
}

func eqFold(a, b rune) bool {
	if a == b {
		return true
	}
	for f := unicode.SimpleFold(a); f != a; f = unicode.SimpleFold(f) {
		if f == b {
			return true
		}
	}
	return false
}

// ---------------------------------------------------------------------------
// Matcher

type span [2]int

type machine struct {
	in    []rune
	caps  [][]span
	steps int
}

type timeoutErr struct{}

func (m *machine) step() {
	m.steps++
	if m.steps > maxSteps {
		panic(timeoutErr{})
	}
}

func (m *machine) snap() []int {
	s := make([]int, len(m.caps))
	for i, c := range m.caps {
		s[i] = len(c)
	}
	return s
}

func (m *machine) restore(s []int) {
	for i, l := range s {
		m.caps[i] = m.caps[i][:l]
	}
}

func (m *machine) isWordAt(i int) bool { return i >= 0 && i < len(m.in) && isWordChar(m.in[i]) }

func (m *machine) single(n *node, i int) bool {
	if i >= len(m.in) {
		return false
	}
	r := m.in[i]
	switch n.kind {
	case kChar:
		if n.fold {
			return eqFold(r, n.r)
		}
		return r == n.r
	case kAny:
		return n.dotall || r != '\n'
	case kClass:
		return n.cls.matches(r, n.fold)
	}
	return false
}

func isSingle(n *node) bool { return n.kind == kChar || n.kind == kAny || n.kind == kClass }

func (m *machine) match(n *node, i int, k func(int) bool) bool {
	m.step()
	switch n.kind {
	case kEmpty:
		return k(i)
	case kChar, kAny, kClass:
		if m.single(n, i) {
			return k(i + 1)
		}
		return false
	case kBol:
		if i == 0 || (n.multiline && m.in[i-1] == '\n') {
			return k(i)
		}
		return false
	case kEol:
		l := len(m.in)
		if i == l || (i == l-1 && m.in[i] == '\n') || (n.multiline && i < l && m.in[i] == '\n') {
			return k(i)
		}
		return false
	case kBeginText:
		if i == 0 {
			return k(i)
		}
		return false
	case kEndText:
		if i == len(m.in) {
			return k(i)
		}
		return false
	case kEndTextZ:
		l := len(m.in)
		if i == l || (i == l-1 && m.in[i] == '\n') {
			return k(i)
		}
		return false
	case kWordB, kNotWordB:
		b := m.isWordAt(i-1) != m.isWordAt(i)
		if b == (n.kind == kWordB) {
			return k(i)
		}
		return false
	case kConcat:
		return m.seq(n.subs, 0, i, k)
	case kAlt:
		for _, s := range n.subs {
			if m.match(s, i, k) {
				return true
			}
		}
		return false
	case kGroup:
		g := n.group
		return m.match(n.sub, i, func(j int) bool {
			m.caps[g] = append(m.caps[g], span{i, j})
			if k(j) {
				return true
			}
			m.caps[g] = m.caps[g][:len(m.caps[g])-1]
			return false
		})
	case kRepeat:
		if isSingle(n.sub) {
			return m.repSingle(n, i, k)
		}
		return m.rep(n, 0, i, k)
	case kLook:
		s := m.snap()
		ok := false
		if n.behind {
			for st := i; st >= 0 && !ok; st-- {
				ok = m.match(n.sub, st, func(j int) bool { return j == i })
				if !ok {
					m.restore(s)
				}
			}
		} else {
			ok = m.match(n.sub, i, func(int) bool { return true })
		}
		if n.neg {
			m.restore(s)
			if ok {
				return false
			}
			return k(i)
		}
		if !ok {
			m.restore(s)
			return false
		}
		if k(i) {
			return true
		}
		m.restore(s)
		return false
	case kAtomic:
		s := m.snap()
		end := -1
		if m.match(n.sub, i, func(j int) bool { end = j; return true }) {
			if k(end) {
				return true
			}
		}
		m.restore(s)
		return false
	case kBackref:
		c := m.caps[n.refNum]
		if len(c) == 0 {
			return false
		}
		sp := c[len(c)-1]
		l := sp[1] - sp[0]
		if i+l > len(m.in) {
			return false
		}
		for x := 0; x < l; x++ {
			a, b := m.in[i+x], m.in[sp[0]+x]
			if a != b && !(n.fold && eqFold(a, b)) {
				return false
			}
		}
		return k(i + l)
	}
	return false
}

func (m *machine) seq(subs []*node, idx, i int, k func(int) bool) bool {
	if idx == len(subs) {
		return k(i)
	}
	// fast path for runs of single characters
	n := subs[idx]
	if isSingle(n) {
		if !m.single(n, i) {
			return false
		}
		return m.seq(subs, idx+1, i+1, k)
	}
	return m.match(n, i, func(j int) bool { return m.seq(subs, idx+1, j, k) })
}

func (m *machine) repSingle(n *node, i int, k func(int) bool) bool {
	if n.greedy {
		cnt := 0
		for (n.max < 0 || cnt < n.max) && m.single(n.sub, i+cnt) {
			cnt++
		}
		for c := cnt; c >= n.min; c-- {
			m.step()
			if k(i + c) {
				return true
			}
		}
		return false
	}
	for c := 0; c < n.min; c++ {
		if !m.single(n.sub, i+c) {
			return false
		}
	}
	for c := n.min; ; c++ {
		m.step()
		if k(i + c) {
			return true
		}
		if (n.max >= 0 && c >= n.max) || !m.single(n.sub, i+c) {
			return false
		}
	}
}

func (m *machine) rep(n *node, count, i int, k func(int) bool) bool {
	m.step()
	canMore := n.max < 0 || count < n.max
	if n.greedy {
		if canMore {
			if m.match(n.sub, i, func(j int) bool {
				if j == i {
					return k(j) // empty iteration: further iterations change nothing
				}
				return m.rep(n, count+1, j, k)
			}) {
				return true
			}
		}
		if count >= n.min {
			return k(i)
		}
		return false
	}
	if count >= n.min && k(i) {
		return true
	}
	if !canMore {
		return false
	}
	return m.match(n.sub, i, func(j int) bool {
		if j == i {
			if count < n.min {
				return k(j)
			}
			return false
		}
		return m.rep(n, count+1, j, k)
	})
}

// ---------------------------------------------------------------------------
// Public matching API

// Match is the result of a successful search.
type Match struct {
	re   *Regexp
	in   []rune
	caps [][]span
}

// Group describes one capturing group of a Match. Index and Length are in
// runes (characters), like .NET string indices.
type Group struct {
	Success  bool
	Value    string
	Index    int
	Length   int
	Captures []string
	// CaptureSpans holds [index, length] (runes) of every capture.
	CaptureSpans [][2]int
}

func (re *Regexp) exec(in []rune, start int) *Match {
	m := &machine{in: in, caps: make([][]span, re.numGroups)}
	var result *Match
	func() {
		defer func() {
			if r := recover(); r != nil {
				if _, ok := r.(timeoutErr); ok {
					result = nil
					return
				}
				panic(r)
			}
		}()
		for s := start; s <= len(in); s++ {
			for i := range m.caps {
				m.caps[i] = m.caps[i][:0]
			}
			end := -1
			if m.match(re.root, s, func(j int) bool { end = j; return true }) {
				caps := make([][]span, re.numGroups)
				for i := range m.caps {
					caps[i] = append([]span(nil), m.caps[i]...)
				}
				caps[0] = []span{{s, end}}
				result = &Match{re: re, in: in, caps: caps}
				return
			}
			if re.anchored && s == 0 {
				return
			}
		}
	}()
	return result
}

// MatchString reports whether s contains a match.
func (re *Regexp) MatchString(s string) bool { return re.exec([]rune(s), 0) != nil }

// FindStringMatch returns the first match or nil.
func (re *Regexp) FindStringMatch(s string) *Match { return re.exec([]rune(s), 0) }

// FindAllStringMatch returns all successive non-overlapping matches.
func (re *Regexp) FindAllStringMatch(s string) []*Match {
	in := []rune(s)
	var out []*Match
	pos := 0
	for pos <= len(in) {
		m := re.exec(in, pos)
		if m == nil {
			break
		}
		out = append(out, m)
		sp := m.caps[0][0]
		if sp[1] == sp[0] {
			pos = sp[1] + 1
		} else {
			pos = sp[1]
		}
		if re.anchored {
			break
		}
	}
	return out
}

// LastMatch returns the last of all matches, or nil.
func (re *Regexp) LastMatch(s string) *Match {
	all := re.FindAllStringMatch(s)
	if len(all) == 0 {
		return nil
	}
	return all[len(all)-1]
}

// Index returns the rune offset of the whole match.
func (m *Match) Index() int { return m.caps[0][0][0] }

// Length returns the rune length of the whole match.
func (m *Match) Length() int { return m.caps[0][0][1] - m.caps[0][0][0] }

// String returns the whole matched text.
func (m *Match) String() string {
	sp := m.caps[0][0]
	return string(m.in[sp[0]:sp[1]])
}

// GroupNum returns group i.
func (m *Match) GroupNum(i int) Group {
	if i < 0 || i >= len(m.caps) || len(m.caps[i]) == 0 {
		return Group{}
	}
	c := m.caps[i]
	last := c[len(c)-1]
	g := Group{Success: true, Value: string(m.in[last[0]:last[1]]), Index: last[0], Length: last[1] - last[0]}
	for _, sp := range c {
		g.Captures = append(g.Captures, string(m.in[sp[0]:sp[1]]))
		g.CaptureSpans = append(g.CaptureSpans, [2]int{sp[0], sp[1] - sp[0]})
	}
	return g
}

// Group returns the named group (or numbered, if name is numeric).
func (m *Match) Group(name string) Group { return m.GroupNum(m.re.GroupIndex(name)) }

// Value is shorthand for Group(name).Value.
func (m *Match) Value(name string) string { return m.Group(name).Value }

// Success is shorthand for Group(name).Success.
func (m *Match) Success(name string) bool { return m.Group(name).Success }

// ReplaceAllString replaces every match using .NET substitution syntax
// ($1, ${name}, $0, $&, $$, $`, $', $+, $_).
func (re *Regexp) ReplaceAllString(s, repl string) string {
	return re.ReplaceAllStringFunc(s, func(m *Match) string { return m.expand(repl) })
}

// ReplaceAllStringFunc replaces every match by the result of f.
func (re *Regexp) ReplaceAllStringFunc(s string, f func(*Match) string) string {
	in := []rune(s)
	var b strings.Builder
	last := 0
	pos := 0
	for pos <= len(in) {
		m := re.exec(in, pos)
		if m == nil {
			break
		}
		sp := m.caps[0][0]
		b.WriteString(string(in[last:sp[0]]))
		b.WriteString(f(m))
		last = sp[1]
		if sp[1] == sp[0] {
			if sp[1] < len(in) {
				b.WriteRune(in[sp[1]])
			}
			last = sp[1] + 1
			pos = sp[1] + 1
		} else {
			pos = sp[1]
		}
		if re.anchored {
			break
		}
	}
	if last < len(in) {
		b.WriteString(string(in[last:]))
	}
	return b.String()
}

// Split splits s around matches (captured groups are included, like .NET).
func (re *Regexp) Split(s string) []string {
	in := []rune(s)
	var out []string
	last := 0
	for _, m := range re.FindAllStringMatch(s) {
		sp := m.caps[0][0]
		out = append(out, string(in[last:sp[0]]))
		for g := 1; g < len(m.caps); g++ {
			if len(m.caps[g]) > 0 {
				out = append(out, m.GroupNum(g).Value)
			}
		}
		last = sp[1]
	}
	return append(out, string(in[last:]))
}

func (m *Match) expand(repl string) string {
	if !strings.ContainsRune(repl, '$') {
		return repl
	}
	r := []rune(repl)
	var b strings.Builder
	for i := 0; i < len(r); i++ {
		if r[i] != '$' || i+1 >= len(r) {
			b.WriteRune(r[i])
			continue
		}
		c := r[i+1]
		switch {
		case c == '$':
			b.WriteRune('$')
			i++
		case c == '&':
			b.WriteString(m.String())
			i++
		case c == '`':
			b.WriteString(string(m.in[:m.Index()]))
			i++
		case c == '\'':
			b.WriteString(string(m.in[m.Index()+m.Length():]))
			i++
		case c == '_':
			b.WriteString(string(m.in))
			i++
		case c == '+':
			b.WriteString(m.GroupNum(len(m.caps) - 1).Value)
			i++
		case c == '{':
			end := i + 2
			for end < len(r) && r[end] != '}' {
				end++
			}
			if end >= len(r) {
				b.WriteRune('$')
				continue
			}
			name := string(r[i+2 : end])
			idx := m.re.GroupIndex(name)
			if idx < 0 {
				b.WriteRune('$')
				continue
			}
			b.WriteString(m.GroupNum(idx).Value)
			i = end
		case c >= '0' && c <= '9':
			// longest prefix of digits that names an existing group
			end := i + 1
			for end < len(r) && r[end] >= '0' && r[end] <= '9' {
				end++
			}
			found := false
			for e := end; e > i+1; e-- {
				num, _ := strconv.Atoi(string(r[i+1 : e]))
				if num < len(m.caps) {
					b.WriteString(m.GroupNum(num).Value)
					i = e - 1
					found = true
					break
				}
			}
			if !found {
				b.WriteRune('$')
			}
		default:
			b.WriteRune('$')
		}
	}
	return b.String()
}

// Escape escapes all regex metacharacters in s (like Regex.Escape).
func Escape(s string) string {
	var b strings.Builder
	for _, r := range s {
		switch r {
		case '\\', '*', '+', '?', '|', '{', '[', '(', ')', '^', '$', '.', '#', ' ':
			b.WriteRune('\\')
			b.WriteRune(r)
		case '\t':
			b.WriteString(`\t`)
		case '\n':
			b.WriteString(`\n`)
		default:
			b.WriteRune(r)
		}
	}
	return b.String()
}
