// Package dom is a forgiving HTML parser with a CSS selector engine,
// covering what Cardigann indexer definitions need (it replaces AngleSharp,
// used by Prowlarr). It handles void elements, raw-text elements, implied end
// tags and the implicit <tbody> that browsers insert in tables.
package dom

import (
	"html"
	"strings"
)

// NodeType distinguishes element, text and document nodes.
type NodeType int

const (
	DocumentNode NodeType = iota
	ElementNode
	TextNode
	CommentNode
)

// Attr is an element attribute.
type Attr struct {
	Name, Value string
}

// Node is a DOM node.
type Node struct {
	Type     NodeType
	Tag      string // lower-case
	Attrs    []Attr
	Text     string
	Parent   *Node
	Children []*Node
}

var voidElements = map[string]bool{
	"area": true, "base": true, "br": true, "col": true, "embed": true, "hr": true, "img": true,
	"input": true, "link": true, "meta": true, "param": true, "source": true, "track": true, "wbr": true,
	"keygen": true, "basefont": true, "frame": true,
}

var rawTextElements = map[string]bool{"script": true, "style": true, "textarea": true, "title": true, "xmp": true}

var blockClosesP = map[string]bool{
	"address": true, "article": true, "aside": true, "blockquote": true, "div": true, "dl": true,
	"fieldset": true, "footer": true, "form": true, "h1": true, "h2": true, "h3": true, "h4": true,
	"h5": true, "h6": true, "header": true, "hr": true, "menu": true, "nav": true, "ol": true, "p": true,
	"pre": true, "section": true, "table": true, "ul": true, "details": true, "figure": true, "main": true,
}

// Parse builds a DOM from HTML source. It never fails.
func Parse(src string) *Node {
	doc := &Node{Type: DocumentNode}
	b := &builder{doc: doc, stack: []*Node{doc}, xml: false}
	b.run(src)
	return doc
}

// ParseXML builds a DOM from XML (tag names keep their case-insensitive
// lower-case form; no HTML implied-tag rules).
func ParseXML(src string) *Node {
	doc := &Node{Type: DocumentNode}
	b := &builder{doc: doc, stack: []*Node{doc}, xml: true}
	b.run(src)
	return doc
}

type builder struct {
	doc   *Node
	stack []*Node
	xml   bool
}

func (b *builder) cur() *Node { return b.stack[len(b.stack)-1] }

func (b *builder) appendChild(n *Node) {
	p := b.cur()
	n.Parent = p
	p.Children = append(p.Children, n)
}

func (b *builder) push(n *Node) {
	b.appendChild(n)
	b.stack = append(b.stack, n)
}

func (b *builder) hasOpen(tag string, stopAt ...string) bool {
	for i := len(b.stack) - 1; i > 0; i-- {
		t := b.stack[i].Tag
		if t == tag {
			return true
		}
		for _, s := range stopAt {
			if t == s {
				return false
			}
		}
	}
	return false
}

func (b *builder) popTo(tag string) {
	for i := len(b.stack) - 1; i > 0; i-- {
		if b.stack[i].Tag == tag {
			b.stack = b.stack[:i]
			return
		}
	}
}

func (b *builder) popWhile(tags ...string) {
	for len(b.stack) > 1 {
		t := b.cur().Tag
		match := false
		for _, x := range tags {
			if t == x {
				match = true
			}
		}
		if !match {
			return
		}
		b.stack = b.stack[:len(b.stack)-1]
	}
}

func (b *builder) text(s string) {
	if s == "" {
		return
	}
	s = html.UnescapeString(s)
	p := b.cur()
	if n := len(p.Children); n > 0 && p.Children[n-1].Type == TextNode {
		p.Children[n-1].Text += s
		return
	}
	b.appendChild(&Node{Type: TextNode, Text: s})
}

// implied end tags before opening tag t (HTML only)
func (b *builder) beforeOpen(t string) {
	if b.xml {
		return
	}
	switch {
	case blockClosesP[t]:
		if b.hasOpen("p", "table", "div", "td", "th", "li") {
			b.popTo("p")
		}
	}
	switch t {
	case "li":
		if b.hasOpen("li", "ul", "ol") {
			b.popTo("li")
		}
	case "dt", "dd":
		if b.hasOpen("dt", "dl") {
			b.popTo("dt")
		}
		if b.hasOpen("dd", "dl") {
			b.popTo("dd")
		}
	case "option":
		b.popWhile("option")
	case "optgroup":
		b.popWhile("option", "optgroup")
	case "tr":
		if b.hasOpen("tr", "table") {
			b.popTo("tr")
		}
		if c := b.cur().Tag; c == "table" {
			b.push(&Node{Type: ElementNode, Tag: "tbody"})
		}
	case "td", "th":
		if b.hasOpen("td", "table") {
			b.popTo("td")
		}
		if b.hasOpen("th", "table") {
			b.popTo("th")
		}
		switch b.cur().Tag {
		case "table":
			b.push(&Node{Type: ElementNode, Tag: "tbody"})
			b.push(&Node{Type: ElementNode, Tag: "tr"})
		case "tbody", "thead", "tfoot":
			b.push(&Node{Type: ElementNode, Tag: "tr"})
		}
	case "thead", "tbody", "tfoot":
		for b.hasOpen("tr", "table") || b.hasOpen("td", "table") || b.hasOpen("th", "table") {
			b.popWhile("td", "th", "tr")
			if t := b.cur().Tag; t != "td" && t != "th" && t != "tr" {
				break
			}
		}
		for _, s := range []string{"thead", "tbody", "tfoot"} {
			if b.hasOpen(s, "table") {
				b.popTo(s)
			}
		}
	}
}

func (b *builder) run(src string) {
	i := 0
	n := len(src)
	for i < n {
		lt := strings.IndexByte(src[i:], '<')
		if lt < 0 {
			b.text(src[i:])
			return
		}
		if lt > 0 {
			b.text(src[i : i+lt])
		}
		i += lt
		rest := src[i:]
		switch {
		case strings.HasPrefix(rest, "<!--"):
			end := strings.Index(rest[4:], "-->")
			if end < 0 {
				return
			}
			b.appendChild(&Node{Type: CommentNode, Text: rest[4 : 4+end]})
			i += 4 + end + 3
		case strings.HasPrefix(rest, "<![CDATA["):
			end := strings.Index(rest, "]]>")
			if end < 0 {
				b.appendChild(&Node{Type: TextNode, Text: rest[9:]})
				return
			}
			b.appendChild(&Node{Type: TextNode, Text: rest[9:end]})
			i += end + 3
		case strings.HasPrefix(rest, "<!") || strings.HasPrefix(rest, "<?"):
			end := strings.IndexByte(rest, '>')
			if end < 0 {
				return
			}
			i += end + 1
		case strings.HasPrefix(rest, "</"):
			end := strings.IndexByte(rest, '>')
			if end < 0 {
				return
			}
			name := strings.ToLower(strings.TrimSpace(rest[2:end]))
			if sp := strings.IndexAny(name, " \t\n"); sp >= 0 {
				name = name[:sp]
			}
			i += end + 1
			b.closeTag(name)
		default:
			if len(rest) < 2 || !isNameStart(rest[1]) {
				b.text("<")
				i++
				continue
			}
			consumed, tag, attrs, selfClosing := parseStartTag(rest)
			if consumed == 0 {
				b.text(rest)
				return
			}
			i += consumed
			tag = strings.ToLower(tag)
			b.beforeOpen(tag)
			el := &Node{Type: ElementNode, Tag: tag, Attrs: attrs}
			if (!b.xml && voidElements[tag]) || selfClosing {
				b.appendChild(el)
				continue
			}
			b.push(el)
			if !b.xml && rawTextElements[tag] {
				closeIdx := indexFold(src[i:], "</"+tag)
				if closeIdx < 0 {
					el.Children = append(el.Children, &Node{Type: TextNode, Text: src[i:], Parent: el})
					return
				}
				raw := src[i : i+closeIdx]
				if tag == "textarea" || tag == "title" {
					raw = html.UnescapeString(raw)
				}
				if raw != "" {
					el.Children = append(el.Children, &Node{Type: TextNode, Text: raw, Parent: el})
				}
				i += closeIdx
				gt := strings.IndexByte(src[i:], '>')
				if gt < 0 {
					return
				}
				i += gt + 1
				b.stack = b.stack[:len(b.stack)-1]
			}
		}
	}
}

func (b *builder) closeTag(name string) {
	if !b.xml {
		switch name {
		case "p":
			if !b.hasOpen("p", "div", "td", "th", "table", "li") {
				b.appendChild(&Node{Type: ElementNode, Tag: "p"}) // </p> without <p>
				return
			}
		case "br":
			b.appendChild(&Node{Type: ElementNode, Tag: "br"})
			return
		case "table":
			if b.hasOpen("table") {
				b.popTo("table")
			}
			return
		case "tr", "td", "th", "tbody", "thead", "tfoot":
			if b.hasOpen(name, "table") {
				b.popTo(name)
			}
			return
		}
	}
	if b.hasOpen(name) {
		b.popTo(name)
	}
}

func isNameStart(c byte) bool { return c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' }

func indexFold(s, sub string) int {
	return strings.Index(strings.ToLower(s), strings.ToLower(sub))
}

// parseStartTag parses "<tag attr=... >" and returns bytes consumed.
func parseStartTag(s string) (int, string, []Attr, bool) {
	i := 1
	start := i
	for i < len(s) && !isSpace(s[i]) && s[i] != '>' && s[i] != '/' {
		i++
	}
	tag := s[start:i]
	var attrs []Attr
	self := false
	for i < len(s) {
		for i < len(s) && isSpace(s[i]) {
			i++
		}
		if i >= len(s) {
			return 0, "", nil, false
		}
		if s[i] == '>' {
			return i + 1, tag, attrs, self
		}
		if s[i] == '/' {
			self = i+1 < len(s) && s[i+1] == '>'
			i++
			continue
		}
		ns := i
		for i < len(s) && !isSpace(s[i]) && s[i] != '=' && s[i] != '>' && !(s[i] == '/' && i+1 < len(s) && s[i+1] == '>') {
			i++
		}
		name := strings.ToLower(s[ns:i])
		for i < len(s) && isSpace(s[i]) {
			i++
		}
		val := ""
		if i < len(s) && s[i] == '=' {
			i++
			for i < len(s) && isSpace(s[i]) {
				i++
			}
			if i < len(s) && (s[i] == '"' || s[i] == '\'') {
				q := s[i]
				end := strings.IndexByte(s[i+1:], q)
				if end < 0 {
					return 0, "", nil, false
				}
				val = s[i+1 : i+1+end]
				i += end + 2
			} else {
				vs := i
				for i < len(s) && !isSpace(s[i]) && s[i] != '>' {
					i++
				}
				val = s[vs:i]
			}
			val = html.UnescapeString(val)
		}
		if name != "" {
			dup := false
			for _, a := range attrs {
				if a.Name == name {
					dup = true
				}
			}
			if !dup {
				attrs = append(attrs, Attr{name, val})
			}
		}
	}
	return 0, "", nil, false
}

func isSpace(c byte) bool { return c == ' ' || c == '\t' || c == '\n' || c == '\r' || c == '\f' }

// ---------------------------------------------------------------------------
// Node helpers

// Attr returns an attribute value and whether it exists.
func (n *Node) Attr(name string) (string, bool) {
	name = strings.ToLower(name)
	for _, a := range n.Attrs {
		if a.Name == name {
			return a.Value, true
		}
	}
	return "", false
}

// TextContent concatenates descendant text (like DOM textContent).
func (n *Node) TextContent() string {
	var b strings.Builder
	var walk func(*Node)
	walk = func(x *Node) {
		if x.Type == TextNode {
			b.WriteString(x.Text)
			return
		}
		for _, c := range x.Children {
			walk(c)
		}
	}
	walk(n)
	return b.String()
}

// ElementChildren returns child elements.
func (n *Node) ElementChildren() []*Node {
	var out []*Node
	for _, c := range n.Children {
		if c.Type == ElementNode {
			out = append(out, c)
		}
	}
	return out
}

// ParentElement returns the parent element or nil.
func (n *Node) ParentElement() *Node {
	if n.Parent != nil && n.Parent.Type == ElementNode {
		return n.Parent
	}
	return nil
}

// PreviousElementSibling returns the previous element sibling or nil.
func (n *Node) PreviousElementSibling() *Node {
	if n.Parent == nil {
		return nil
	}
	var prev *Node
	for _, c := range n.Parent.Children {
		if c == n {
			return prev
		}
		if c.Type == ElementNode {
			prev = c
		}
	}
	return nil
}

// Remove detaches n from its parent.
func (n *Node) Remove() {
	if n.Parent == nil {
		return
	}
	p := n.Parent
	for i, c := range p.Children {
		if c == n {
			p.Children = append(p.Children[:i:i], p.Children[i+1:]...)
			break
		}
	}
	n.Parent = nil
}

// AppendChildren moves nodes under n.
func (n *Node) AppendChildren(nodes []*Node) {
	for _, c := range nodes {
		c.Remove()
		c.Parent = n
		n.Children = append(n.Children, c)
	}
}

// Root returns the top-most ancestor.
func (n *Node) Root() *Node {
	for n.Parent != nil {
		n = n.Parent
	}
	return n
}
