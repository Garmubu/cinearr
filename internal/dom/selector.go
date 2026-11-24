package dom

import (
	"fmt"
	"strconv"
	"strings"
)

// Selector is a compiled CSS selector list.
type Selector struct {
	src    string
	groups []*complexSel
}

type complexSel struct {
	lead  byte // leading combinator for relative selectors (:has)
	parts []*compound
	combs []byte // combs[i] links parts[i] and parts[i+1]
}

type compound struct {
	tag   string
	conds []func(*Node) bool
}

// Compile parses a selector list.
func Compile(sel string) (*Selector, error) {
	p := &selParser{s: sel}
	groups, err := p.list(false)
	if err != nil {
		return nil, fmt.Errorf("css: %w in %q", err, sel)
	}
	p.ws()
	if p.i < len(p.s) {
		return nil, fmt.Errorf("css: unexpected %q in %q", p.s[p.i:], sel)
	}
	return &Selector{src: sel, groups: groups}, nil
}

// MustCompile panics on error.
func MustCompile(sel string) *Selector {
	s, err := Compile(sel)
	if err != nil {
		panic(err)
	}
	return s
}

func (s *Selector) String() string { return s.src }

// Match reports whether n matches the selector.
func (s *Selector) Match(n *Node) bool {
	if n == nil || n.Type != ElementNode {
		return false
	}
	for _, g := range s.groups {
		if matchComplex(n, g, len(g.parts)-1, nil) {
			return true
		}
	}
	return false
}

// QueryAll returns matching descendants of root in document order.
func (s *Selector) QueryAll(root *Node) []*Node {
	var out []*Node
	var walk func(*Node)
	walk = func(x *Node) {
		for _, c := range x.Children {
			if c.Type != ElementNode {
				continue
			}
			if s.Match(c) {
				out = append(out, c)
			}
			walk(c)
		}
	}
	walk(root)
	return out
}

// Query returns the first matching descendant or nil.
func (s *Selector) Query(root *Node) *Node {
	var found *Node
	var walk func(*Node) bool
	walk = func(x *Node) bool {
		for _, c := range x.Children {
			if c.Type != ElementNode {
				continue
			}
			if s.Match(c) {
				found = c
				return true
			}
			if walk(c) {
				return true
			}
		}
		return false
	}
	walk(root)
	return found
}

// QuerySelector compiles sel and returns the first match under root.
func QuerySelector(root *Node, sel string) (*Node, error) {
	s, err := Compile(sel)
	if err != nil {
		return nil, err
	}
	return s.Query(root), nil
}

// QuerySelectorAll compiles sel and returns all matches under root.
func QuerySelectorAll(root *Node, sel string) ([]*Node, error) {
	s, err := Compile(sel)
	if err != nil {
		return nil, err
	}
	return s.QueryAll(root), nil
}

// Matches compiles sel and tests n.
func Matches(n *Node, sel string) (bool, error) {
	s, err := Compile(sel)
	if err != nil {
		return false, err
	}
	return s.Match(n), nil
}

func (c *compound) match(n *Node) bool {
	if n == nil || n.Type != ElementNode {
		return false
	}
	if c.tag != "" && c.tag != "*" && c.tag != n.Tag {
		return false
	}
	for _, f := range c.conds {
		if !f(n) {
			return false
		}
	}
	return true
}

// matchComplex matches parts[0..idx] ending at n. anchor is used by relative
// selectors (:has): the left-most part must relate to anchor via g.lead.
func matchComplex(n *Node, g *complexSel, idx int, anchor *Node) bool {
	if !g.parts[idx].match(n) {
		return false
	}
	if idx == 0 {
		if anchor == nil {
			return true
		}
		return related(anchor, n, g.lead)
	}
	switch g.combs[idx-1] {
	case '>':
		p := n.ParentElement()
		return p != nil && matchComplex(p, g, idx-1, anchor)
	case '+':
		p := n.PreviousElementSibling()
		return p != nil && matchComplex(p, g, idx-1, anchor)
	case '~':
		for p := n.PreviousElementSibling(); p != nil; p = p.PreviousElementSibling() {
			if matchComplex(p, g, idx-1, anchor) {
				return true
			}
		}
		return false
	default: // descendant
		for p := n.ParentElement(); p != nil; p = p.ParentElement() {
			if matchComplex(p, g, idx-1, anchor) {
				return true
			}
		}
		return false
	}
}

// related checks the relation between the :has subject and a matched node.
func related(anchor, x *Node, lead byte) bool {
	switch lead {
	case '>':
		return x.Parent == anchor
	case '+':
		return x.PreviousElementSibling() == anchor
	case '~':
		for p := x.PreviousElementSibling(); p != nil; p = p.PreviousElementSibling() {
			if p == anchor {
				return true
			}
		}
		return false
	default:
		for p := x.Parent; p != nil; p = p.Parent {
			if p == anchor {
				return true
			}
		}
		return false
	}
}

// ---------------------------------------------------------------------------

type selParser struct {
	s string
	i int
}

func (p *selParser) ws() bool {
	start := p.i
	for p.i < len(p.s) && (p.s[p.i] == ' ' || p.s[p.i] == '\t' || p.s[p.i] == '\n' || p.s[p.i] == '\r') {
		p.i++
	}
	return p.i > start
}

func (p *selParser) peek() byte {
	if p.i < len(p.s) {
		return p.s[p.i]
	}
	return 0
}

func (p *selParser) list(relative bool) ([]*complexSel, error) {
	var out []*complexSel
	for {
		p.ws()
		c, err := p.complex(relative)
		if err != nil {
			return nil, err
		}
		out = append(out, c)
		p.ws()
		if p.peek() == ',' {
			p.i++
			continue
		}
		return out, nil
	}
}

func (p *selParser) complex(relative bool) (*complexSel, error) {
	cs := &complexSel{lead: ' '}
	if relative {
		p.ws()
		if c := p.peek(); c == '>' || c == '+' || c == '~' {
			cs.lead = c
			p.i++
			p.ws()
		}
	}
	for {
		comp, err := p.compound()
		if err != nil {
			return nil, err
		}
		cs.parts = append(cs.parts, comp)
		hadWS := p.ws()
		c := p.peek()
		switch {
		case c == '>' || c == '+' || c == '~':
			p.i++
			p.ws()
			cs.combs = append(cs.combs, c)
		case c == 0 || c == ',' || c == ')':
			return cs, nil
		case hadWS:
			cs.combs = append(cs.combs, ' ')
		default:
			return nil, fmt.Errorf("unexpected %q", c)
		}
	}
}

func isIdentChar(c byte) bool {
	return c == '-' || c == '_' || c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c >= 0x80
}

func (p *selParser) ident() string {
	var b strings.Builder
	for p.i < len(p.s) {
		c := p.s[p.i]
		if c == '\\' && p.i+1 < len(p.s) {
			b.WriteByte(p.s[p.i+1])
			p.i += 2
			continue
		}
		if !isIdentChar(c) {
			break
		}
		b.WriteByte(c)
		p.i++
	}
	return b.String()
}

func (p *selParser) str() (string, error) {
	q := p.s[p.i]
	p.i++
	var b strings.Builder
	for p.i < len(p.s) {
		c := p.s[p.i]
		if c == '\\' && p.i+1 < len(p.s) {
			b.WriteByte(p.s[p.i+1])
			p.i += 2
			continue
		}
		if c == q {
			p.i++
			return b.String(), nil
		}
		b.WriteByte(c)
		p.i++
	}
	return "", fmt.Errorf("unterminated string")
}

func (p *selParser) compound() (*compound, error) {
	c := &compound{}
	if p.peek() == '*' {
		c.tag = "*"
		p.i++
	} else if isIdentChar(p.peek()) && !(p.peek() >= '0' && p.peek() <= '9') {
		c.tag = strings.ToLower(p.ident())
	}
	for p.i < len(p.s) {
		switch p.peek() {
		case '#':
			p.i++
			id := p.ident()
			c.conds = append(c.conds, func(n *Node) bool { v, _ := n.Attr("id"); return v == id })
		case '.':
			p.i++
			cls := p.ident()
			c.conds = append(c.conds, func(n *Node) bool { return hasWord(attr(n, "class"), cls, false) })
		case '[':
			f, err := p.attribute()
			if err != nil {
				return nil, err
			}
			c.conds = append(c.conds, f)
		case ':':
			p.i++
			if p.peek() == ':' { // pseudo-element: ignore
				p.i++
				p.ident()
				continue
			}
			f, err := p.pseudo()
			if err != nil {
				return nil, err
			}
			c.conds = append(c.conds, f)
		default:
			if c.tag == "" && len(c.conds) == 0 {
				return nil, fmt.Errorf("expected selector at %d", p.i)
			}
			return c, nil
		}
	}
	if c.tag == "" && len(c.conds) == 0 {
		return nil, fmt.Errorf("empty selector")
	}
	return c, nil
}

func attr(n *Node, name string) string { v, _ := n.Attr(name); return v }

func hasWord(list, w string, fold bool) bool {
	for _, f := range strings.Fields(list) {
		if f == w || (fold && strings.EqualFold(f, w)) {
			return true
		}
	}
	return false
}

func (p *selParser) attribute() (func(*Node) bool, error) {
	p.i++ // [
	p.ws()
	name := strings.ToLower(p.ident())
	p.ws()
	if p.peek() == ']' {
		p.i++
		return func(n *Node) bool { _, ok := n.Attr(name); return ok }, nil
	}
	op := ""
	for p.i < len(p.s) && strings.IndexByte("~|^$*!=", p.s[p.i]) >= 0 {
		op += string(p.s[p.i])
		p.i++
	}
	p.ws()
	var val string
	if c := p.peek(); c == '"' || c == '\'' {
		v, err := p.str()
		if err != nil {
			return nil, err
		}
		val = v
	} else {
		start := p.i
		for p.i < len(p.s) && p.s[p.i] != ']' && p.s[p.i] != ' ' {
			p.i++
		}
		val = p.s[start:p.i]
	}
	p.ws()
	fold := false
	if c := p.peek(); c == 'i' || c == 'I' {
		fold = true
		p.i++
		p.ws()
	} else if c == 's' || c == 'S' {
		p.i++
		p.ws()
	}
	if p.peek() != ']' {
		return nil, fmt.Errorf("expected ']'")
	}
	p.i++
	norm := func(s string) string {
		if fold {
			return strings.ToLower(s)
		}
		return s
	}
	v := norm(val)
	return func(n *Node) bool {
		a, ok := n.Attr(name)
		if !ok {
			return op == "!="
		}
		a = norm(a)
		switch op {
		case "=":
			return a == v
		case "!=":
			return a != v
		case "~=":
			return hasWord(a, v, false)
		case "|=":
			return a == v || strings.HasPrefix(a, v+"-")
		case "^=":
			return v != "" && strings.HasPrefix(a, v)
		case "$=":
			return v != "" && strings.HasSuffix(a, v)
		case "*=":
			return v != "" && strings.Contains(a, v)
		}
		return false
	}, nil
}

// argument returns the raw text between parentheses (balanced).
func (p *selParser) argument() (string, error) {
	if p.peek() != '(' {
		return "", fmt.Errorf("expected '('")
	}
	p.i++
	start := p.i
	depth := 1
	for p.i < len(p.s) {
		c := p.s[p.i]
		switch c {
		case '"', '\'':
			if _, err := p.str(); err != nil {
				return "", err
			}
			continue
		case '(':
			depth++
		case ')':
			depth--
			if depth == 0 {
				arg := p.s[start:p.i]
				p.i++
				return arg, nil
			}
		}
		p.i++
	}
	return "", fmt.Errorf("unbalanced parentheses")
}

func unquote(s string) string {
	s = strings.TrimSpace(s)
	if len(s) >= 2 && (s[0] == '"' || s[0] == '\'') && s[len(s)-1] == s[0] {
		return s[1 : len(s)-1]
	}
	return s
}

func (p *selParser) pseudo() (func(*Node) bool, error) {
	name := strings.ToLower(p.ident())
	switch name {
	case "first-child":
		return func(n *Node) bool { return n.PreviousElementSibling() == nil && n.Parent != nil }, nil
	case "last-child":
		return func(n *Node) bool { return nextElementSibling(n) == nil && n.Parent != nil }, nil
	case "only-child":
		return func(n *Node) bool { return n.PreviousElementSibling() == nil && nextElementSibling(n) == nil }, nil
	case "first-of-type":
		return func(n *Node) bool { i, _ := typeIndex(n); return i == 1 }, nil
	case "last-of-type":
		return func(n *Node) bool { i, t := typeIndex(n); return i == t }, nil
	case "only-of-type":
		return func(n *Node) bool { _, t := typeIndex(n); return t == 1 }, nil
	case "empty":
		return func(n *Node) bool {
			for _, c := range n.Children {
				if c.Type == ElementNode || (c.Type == TextNode && c.Text != "") {
					return false
				}
			}
			return true
		}, nil
	case "root":
		return func(n *Node) bool { return n.Parent != nil && n.Parent.Type == DocumentNode }, nil
	case "checked":
		return func(n *Node) bool {
			_, c := n.Attr("checked")
			_, s := n.Attr("selected")
			return c || (n.Tag == "option" && s)
		}, nil
	case "selected":
		return func(n *Node) bool { _, s := n.Attr("selected"); return s }, nil
	case "disabled":
		return func(n *Node) bool { _, d := n.Attr("disabled"); return d }, nil
	case "enabled":
		return func(n *Node) bool { _, d := n.Attr("disabled"); return !d }, nil
	case "link", "any-link":
		return func(n *Node) bool { _, h := n.Attr("href"); return n.Tag == "a" && h }, nil
	}
	arg, err := p.argument()
	if err != nil {
		return nil, fmt.Errorf(":%s %w", name, err)
	}
	switch name {
	case "not", "is", "matches", "where":
		sub := &selParser{s: arg}
		groups, err := sub.list(false)
		if err != nil {
			return nil, err
		}
		s := &Selector{groups: groups}
		if name == "not" {
			return func(n *Node) bool { return !s.Match(n) }, nil
		}
		return s.Match, nil
	case "has":
		sub := &selParser{s: arg}
		groups, err := sub.list(true)
		if err != nil {
			return nil, err
		}
		return func(n *Node) bool {
			for _, g := range groups {
				var cands []*Node
				switch g.lead {
				case '+', '~':
					for s := nextElementSibling(n); s != nil; s = nextElementSibling(s) {
						cands = append(cands, s)
						cands = append(cands, descendants(s)...)
					}
				default:
					cands = descendants(n)
				}
				for _, c := range cands {
					if matchComplex(c, g, len(g.parts)-1, n) {
						return true
					}
				}
			}
			return false
		}, nil
	case "contains", "-soup-contains":
		text := unquote(arg)
		return func(n *Node) bool { return strings.Contains(n.TextContent(), text) }, nil
	case "containsown", "-soup-contains-own":
		text := unquote(arg)
		return func(n *Node) bool {
			var b strings.Builder
			for _, c := range n.Children {
				if c.Type == TextNode {
					b.WriteString(c.Text)
				}
			}
			return strings.Contains(b.String(), text)
		}, nil
	case "nth-child", "nth-last-child", "nth-of-type", "nth-last-of-type":
		a, b, err := parseNth(arg)
		if err != nil {
			return nil, err
		}
		return func(n *Node) bool {
			var pos int
			switch name {
			case "nth-child":
				pos = childIndex(n, false)
			case "nth-last-child":
				pos = childIndex(n, true)
			case "nth-of-type":
				pos, _ = typeIndex(n)
			default:
				i, t := typeIndex(n)
				pos = t - i + 1
			}
			return nthMatch(a, b, pos)
		}, nil
	case "eq", "lt", "gt": // jQuery-style, sibling index among the same parent
		k, err := strconv.Atoi(strings.TrimSpace(arg))
		if err != nil {
			return nil, err
		}
		return func(n *Node) bool {
			i := childIndex(n, false) - 1
			switch name {
			case "eq":
				return i == k
			case "lt":
				return i < k
			}
			return i > k
		}, nil
	}
	return nil, fmt.Errorf("unsupported pseudo-class :%s", name)
}

func descendants(n *Node) []*Node {
	var out []*Node
	var walk func(*Node)
	walk = func(x *Node) {
		for _, c := range x.Children {
			if c.Type == ElementNode {
				out = append(out, c)
				walk(c)
			}
		}
	}
	walk(n)
	return out
}

func nextElementSibling(n *Node) *Node {
	if n.Parent == nil {
		return nil
	}
	found := false
	for _, c := range n.Parent.Children {
		if found && c.Type == ElementNode {
			return c
		}
		if c == n {
			found = true
		}
	}
	return nil
}

func childIndex(n *Node, fromEnd bool) int {
	if n.Parent == nil {
		return 1
	}
	els := n.Parent.ElementChildren()
	for i, c := range els {
		if c == n {
			if fromEnd {
				return len(els) - i
			}
			return i + 1
		}
	}
	return 0
}

func typeIndex(n *Node) (int, int) {
	if n.Parent == nil {
		return 1, 1
	}
	idx, total := 0, 0
	for _, c := range n.Parent.Children {
		if c.Type == ElementNode && c.Tag == n.Tag {
			total++
			if c == n {
				idx = total
			}
		}
	}
	return idx, total
}

func parseNth(s string) (int, int, error) {
	s = strings.ToLower(strings.ReplaceAll(strings.TrimSpace(s), " ", ""))
	if i := strings.Index(s, "of"); i > 0 { // ignore "of S"
		s = s[:i]
	}
	switch s {
	case "odd":
		return 2, 1, nil
	case "even":
		return 2, 0, nil
	}
	if !strings.Contains(s, "n") {
		b, err := strconv.Atoi(s)
		return 0, b, err
	}
	parts := strings.SplitN(s, "n", 2)
	var a int
	switch parts[0] {
	case "", "+":
		a = 1
	case "-":
		a = -1
	default:
		v, err := strconv.Atoi(parts[0])
		if err != nil {
			return 0, 0, err
		}
		a = v
	}
	b := 0
	if parts[1] != "" {
		v, err := strconv.Atoi(parts[1])
		if err != nil {
			return 0, 0, err
		}
		b = v
	}
	return a, b, nil
}

func nthMatch(a, b, pos int) bool {
	if a == 0 {
		return pos == b
	}
	d := pos - b
	return d%a == 0 && d/a >= 0
}
