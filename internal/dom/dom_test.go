package dom

import (
	"strings"
	"testing"
)

const page = `<!DOCTYPE html><html><head><title>T &amp; co</title>
<script>var x = "<td>not a cell</td>";</script></head>
<body><div id="main" class="box wide">
<table class="table-list">
<tr><th>Name</th><th>Size</th></tr>
<tr class="row"><td class="name"><a href="/cat">Movies</a><a href="/torrent/1/Movie.2020.1080p">Movie.2020.1080p.WEB-DL</a></td><td class="size">1.4 GB<span>x</span></td><td>12</td>
<tr class="row new"><td class="name"><a href="/cat">Movies</a><a href="/torrent/2/Other">Other.2019.720p</a></td><td class="size">700 MB</td><td>3</td>
</table>
<ul><li>a<li>b<li data-x="1">c</ul>
<p>one<p>two
<a class="magnet" href="magnet:?xt=urn:btih:ABC">Magnet</a>
</div></body></html>`

func TestParseAndSelect(t *testing.T) {
	doc := Parse(page)
	q := func(sel string) []*Node {
		n, err := QuerySelectorAll(doc, sel)
		if err != nil {
			t.Fatalf("%s: %v", sel, err)
		}
		return n
	}
	if n := q("table.table-list > tbody > tr"); len(n) != 3 {
		t.Fatalf("implicit tbody rows: %d", len(n))
	}
	rows := q("tr.row")
	if len(rows) != 2 {
		t.Fatalf("rows %d", len(rows))
	}
	a, _ := QuerySelector(rows[0], "td.name a:nth-child(2)")
	if href, _ := a.Attr("href"); href != "/torrent/1/Movie.2020.1080p" {
		t.Fatalf("href %q", href)
	}
	if n := q("tr:has(td:contains(\"700 MB\"))"); len(n) != 1 {
		t.Fatalf(":has/:contains %d", len(n))
	}
	if n := q("tr.row:not(.new)"); len(n) != 1 {
		t.Fatalf(":not %d", len(n))
	}
	if n := q("li"); len(n) != 3 || strings.TrimSpace(n[2].TextContent()) != "c" {
		t.Fatalf("li implied close %d", len(n))
	}
	if n := q("li[data-x=\"1\"]"); len(n) != 1 {
		t.Fatalf("attr")
	}
	if n := q("a[href^=\"magnet:\"]"); len(n) != 1 {
		t.Fatalf("prefix attr")
	}
	if n := q("p"); len(n) != 2 {
		t.Fatalf("p implied close %d", len(n))
	}
	if n := q("td"); len(n) != 6 {
		t.Fatalf("script content must not create cells: %d", len(n))
	}
	if n := q("tr > td:last-child"); len(n) != 2 || n[0].TextContent() != "12" {
		t.Fatalf("last-child")
	}
	if n := q("div#main.box.wide > table tr:nth-child(odd)"); len(n) != 2 {
		t.Fatalf("odd %d", len(n))
	}
	if n := q("tr:has(> td.size)"); len(n) != 2 {
		t.Fatalf("relative has %d", len(n))
	}
	if n := q("th + th"); len(n) != 1 {
		t.Fatalf("sibling")
	}
	title, _ := QuerySelector(doc, "title")
	if title.TextContent() != "T & co" {
		t.Fatalf("title %q", title.TextContent())
	}
	size, _ := QuerySelector(rows[0], "td.size")
	span, _ := QuerySelector(size, "span")
	span.Remove()
	if strings.TrimSpace(size.TextContent()) != "1.4 GB" {
		t.Fatalf("remove %q", size.TextContent())
	}
}
