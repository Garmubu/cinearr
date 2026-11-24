package yaml

import "testing"

const sample = `---
id: demo
name: Demo Tracker # comment
description: "Demo: a \"public\" tracker"
language: fr-FR
type: public
encoding: UTF-8
followredirect: true
links:
- https://demo.example/
legacylinks:
  - https://old.example/
caps:
  categorymappings:
    - {id: 1, cat: Movies, desc: "Films"}
    - {id: 2, cat: Movies/HD, desc: "Films HD", default: true}
  modes:
    search: [q]
    movie-search: [q, imdbid]
settings:
  - name: sort
    type: select
    label: Sort
    default: time
    options:
      time: created
      seeders: seeders
search:
  paths:
    - path: "{{ if .Keywords }}search/{{ .Keywords }}/1/{{ else }}top/{{ end }}"
      response:
        type: json
  keywordsfilters:
    - name: re_replace
      args: ["\\s+", "+"]
  rows:
    selector: table > tbody > tr
  fields:
    title:
      selector: td.name a
    title|append:
      text: " [x]"
    description: |
      line one
      line two
    folded: >-
      a
      b
    size:
      selector: td.size
      filters:
        - name: replace
          args: [",", ""]
        - name: re_replace
          args: ['(\d)\.(\d)', '$1$2']
`

func TestParseDefinition(t *testing.T) {
	tree, err := Parse([]byte(sample))
	if err != nil {
		t.Fatal(err)
	}
	m := tree.(*Map)
	if v, _ := m.Get("name"); v != "Demo Tracker" {
		t.Fatalf("name %q", v)
	}
	if v, _ := m.Get("description"); v != `Demo: a "public" tracker` {
		t.Fatalf("desc %q", v)
	}
	links, _ := m.Get("links")
	if l := links.([]any); len(l) != 1 || l[0] != "https://demo.example/" {
		t.Fatalf("links %v", links)
	}
	caps, _ := m.Get("caps")
	cm, _ := caps.(*Map).Get("categorymappings")
	first := cm.([]any)[1].(*Map)
	if d, _ := first.Get("desc"); d != "Films HD" {
		t.Fatalf("cat desc %v", d)
	}
	modes, _ := caps.(*Map).Get("modes")
	ms, _ := modes.(*Map).Get("movie-search")
	if len(ms.([]any)) != 2 {
		t.Fatalf("modes %v", ms)
	}
	search, _ := m.Get("search")
	fields, _ := search.(*Map).Get("fields")
	fm := fields.(*Map)
	if len(fm.Keys) != 5 || fm.Keys[1] != "title|append" {
		t.Fatalf("fields %v", fm.Keys)
	}
	desc, _ := fm.Get("description")
	if desc != "line one\nline two\n" {
		t.Fatalf("block %q", desc)
	}
	folded, _ := fm.Get("folded")
	if folded != "a b" {
		t.Fatalf("folded %q", folded)
	}
	size, _ := fm.Get("size")
	filters, _ := size.(*Map).Get("filters")
	args, _ := filters.([]any)[1].(*Map).Get("args")
	if a := args.([]any); a[0] != `(\d)\.(\d)` || a[1] != "$1$2" {
		t.Fatalf("args %v", args)
	}
	kf, _ := search.(*Map).Get("keywordsfilters")
	a2, _ := kf.([]any)[0].(*Map).Get("args")
	if a2.([]any)[0] != `\s+` {
		t.Fatalf("escape %q", a2.([]any)[0])
	}
	paths, _ := search.(*Map).Get("paths")
	p0 := paths.([]any)[0].(*Map)
	resp, _ := p0.Get("response")
	if typ, _ := resp.(*Map).Get("type"); typ != "json" {
		t.Fatalf("response %v", resp)
	}

	type def struct {
		ID             string
		FollowRedirect bool
		Links          []string
	}
	var d def
	if err := Decode(tree, &d); err != nil {
		t.Fatal(err)
	}
	if d.ID != "demo" || !d.FollowRedirect || d.Links[0] != "https://demo.example/" {
		t.Fatalf("decode %+v", d)
	}
}
