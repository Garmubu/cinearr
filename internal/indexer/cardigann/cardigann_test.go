package cardigann

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"
)

// Ported from Prowlarr ApplyGoTemplateTextFixture.
func TestTemplates(t *testing.T) {
	vars := Vars{
		".Config.sitelink": "https://somesite.com/",
		".True":            "True",
		".False":           nil,
		".Today.Year":      strconv.Itoa(time.Now().Year()),
		".Categories":      []string{"tv", "movies"},
		".Query.Keywords":  "abc def",
		".Config.sort":     "seeders",
	}
	cases := map[string]string{
		"{{ range .Categories}}&categories[]={{.}}{{end}}":                                                     "&categories[]=tv&categories[]=movies",
		"{{ range $i, $e := .Categories}}&categories[{{$i}}]={{.}}{{end}}":                                     "&categories[0]=tv&categories[1]=movies",
		"{{ range $index, $element := .Categories}}&categories[{{$index}}]={{.}}+postIndex[{{$index}}]{{end}}": "&categories[0]=tv+postIndex[0]&categories[1]=movies+postIndex[1]",
		`{{ re_replace .Query.Keywords "[^a-zA-Z0-9]+" "%" }}`:                                                 "abc%def",
		`{{ join .Categories ", " }}`:                                                                          "tv, movies",
		"{{ .Today.Year }}":                                                                                    strconv.Itoa(time.Now().Year()),
		"{{if .False }}0{{else}}1{{end}}":                                                                      "1",
		"{{if .True }}0{{else}}1{{end}}":                                                                       "0",
		`{{ if eq .Config.sort "seeders" }}S{{ else }}T{{ end }}`:                                              "S",
		`{{ if or .False .Query.Keywords }}Y{{ else }}N{{ end }}`:                                              "Y",
		`{{ if and .True .False }}Y{{ else }}N{{ end }}`:                                                       "N",
	}
	for tpl, want := range cases {
		got, err := ApplyTemplate(tpl, vars, nil)
		if err != nil {
			t.Fatalf("%s: %v", tpl, err)
		}
		if got != want {
			t.Errorf("%s => %q, want %q", tpl, got, want)
		}
	}
}

const htmlDef = `---
id: testtracker
name: Test Tracker
description: "HTML tracker used in tests"
language: fr-FR
type: public
encoding: UTF-8
links:
  - %s/
caps:
  categorymappings:
    - {id: 42, cat: Movies/HD, desc: "Films HD"}
    - {id: 7, cat: TV, desc: "Séries"}
  modes:
    search: [q]
    movie-search: [q, imdbid]
settings:
  - name: sort
    type: select
    label: Tri
    default: seeders
    options:
      seeders: seeders
      time: created
search:
  paths:
    - path: "{{ if .Query.IMDBID }}imdb/{{ .Query.IMDBID }}{{ else }}search/{{ .Keywords }}/{{ end }}"
  inputs:
    order: "{{ .Config.sort }}"
    cat: "{{ join .Categories \",\" }}"
  keywordsfilters:
    - name: re_replace
      args: ["\\s+", "."]
  rows:
    selector: table.list > tbody > tr:has(td.name)
  fields:
    category:
      selector: td.cat a
      attribute: href
      filters:
        - name: regexp
          args: "cat=(\\d+)"
    title:
      selector: td.name a:last-of-type
    details:
      selector: td.name a:last-of-type
      attribute: href
    download:
      selector: td.name a:last-of-type
      attribute: href
    size:
      selector: td.size
    seeders:
      selector: td.seeds
    leechers:
      selector: td.leech
    date:
      selector: td.date
      filters:
        - name: dateparse
          args: "2006-01-02 15:04"
    downloadvolumefactor:
      text: 0
    uploadvolumefactor:
      text: 1
download:
  selectors:
    - selector: a[href^="magnet:?"]
      attribute: href
`

const listPage = `<html><body><table class="list">
<tr><th>Cat</th><th>Name</th></tr>
<tr><td class="cat"><a href="/browse?cat=42">HD</a></td><td class="name"><a href="/c">x</a><a href="/t/1">Inception.2010.MULTi.1080p.BluRay.x264-GRP</a></td><td class="size">2,5 Go</td><td class="seeds">1 234</td><td class="leech">12</td><td class="date">2024-03-01 12:30</td></tr>
<tr><td class="cat"><a href="/browse?cat=7">TV</a></td><td class="name"><a href="/t/2">Show.S01E01.720p</a></td><td class="size">700 MB</td><td class="seeds">5</td><td class="leech">1</td><td class="date">2024-03-02 08:00</td></tr>
</table></body></html>`

const detailsPage = `<html><body><h1>Inception</h1><a href="magnet:?xt=urn:btih:0123456789ABCDEF0123456789ABCDEF01234567&dn=Inception">Magnet</a></body></html>`

func TestHTMLSearchAndDownload(t *testing.T) {
	var lastQuery string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasPrefix(r.URL.Path, "/search/"):
			lastQuery = r.URL.Path + "?" + r.URL.RawQuery
			fmt.Fprint(w, listPage)
		case r.URL.Path == "/t/1":
			fmt.Fprint(w, detailsPage)
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()
	def, err := ParseDefinition([]byte(fmt.Sprintf(htmlDef, srv.URL)))
	if err != nil {
		t.Fatal(err)
	}
	ix := New("t1", def, nil, "")
	ix.delay = 0
	rels, err := ix.Search(context.Background(), Query{Keywords: "Inception 2010", Categories: []int{2000}})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(lastQuery, "/search/Inception.2010/") || !strings.Contains(lastQuery, "order=seeders") || !strings.Contains(lastQuery, "cat=42") {
		t.Fatalf("query %q", lastQuery)
	}
	if len(rels) != 2 {
		t.Fatalf("releases %d", len(rels))
	}
	r := rels[0]
	if r.Title != "Inception.2010.MULTi.1080p.BluRay.x264-GRP" || r.Seeders != 1234 || r.Peers != 1246 {
		t.Fatalf("release %+v", r)
	}
	if r.Size != int64(2.5*float64(1<<30)) {
		t.Fatalf("size %d", r.Size)
	}
	if len(r.Categories) != 1 || r.Categories[0] != 2040 {
		t.Fatalf("categories %v", r.Categories)
	}
	if r.PublishDate.Year() != 2024 || r.DownloadVolumeFactor != 0 {
		t.Fatalf("date/factor %v %v", r.PublishDate, r.DownloadVolumeFactor)
	}
	res, err := ix.Download(context.Background(), r.DownloadURL)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(res.Magnet, "magnet:?xt=urn:btih:0123456789ABCDEF") {
		t.Fatalf("magnet %q", res.Magnet)
	}
}

const jsonDef = `---
id: jsontracker
name: JSON Tracker
type: public
links:
  - %s/
caps:
  categories:
    movies: Movies
  modes:
    movie-search: [q, imdbid]
search:
  paths:
    - path: api/list.json
      response:
        type: json
  inputs:
    query_term: "{{ if .Query.IMDBID }}{{ .Query.IMDBID }}{{ else }}{{ .Keywords }}{{ end }}"
  rows:
    selector: data.movies
    attribute: torrents
    multiple: true
  fields:
    category:
      text: movies
    title_base:
      selector: ..title
    quality:
      selector: quality
    title:
      text: "{{ .Result.title_base }} {{ .Result.quality }}"
    infohash:
      selector: hash
    size:
      selector: size
    seeders:
      selector: seeds
    leechers:
      selector: peers
    date:
      selector: date_uploaded_unix
`

func TestJSONSearch(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("query_term") != "tt1375666" {
			fmt.Fprint(w, `{"data":{"movies":[]}}`)
			return
		}
		fmt.Fprint(w, `{"data":{"movies":[{"title":"Inception (2010)","torrents":[
			{"quality":"1080p","hash":"AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA","size":"1.85 GB","seeds":100,"peers":5,"date_uploaded_unix":1700000000},
			{"quality":"2160p","hash":"BBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBB","size":"5.2 GB","seeds":40,"peers":2,"date_uploaded_unix":1700000100}]}]}}`)
	}))
	defer srv.Close()
	def, err := ParseDefinition([]byte(fmt.Sprintf(jsonDef, srv.URL)))
	if err != nil {
		t.Fatal(err)
	}
	ix := New("j1", def, nil, "")
	ix.delay = 0
	rels, err := ix.Search(context.Background(), Query{ImdbID: "tt1375666"})
	if err != nil {
		t.Fatal(err)
	}
	if len(rels) != 2 {
		t.Fatalf("releases %d", len(rels))
	}
	if rels[1].Title != "Inception (2010) 2160p" || rels[1].Seeders != 40 || rels[1].MagnetURL == "" {
		t.Fatalf("release %+v", rels[1])
	}
	if rels[0].PublishDate.Unix() != 1700000000 {
		t.Fatalf("unix date %v", rels[0].PublishDate)
	}
}

func TestSizesAndDates(t *testing.T) {
	f := func(v float64, shift uint) int64 { return int64(v * float64(int64(1)<<shift)) }
	sizes := map[string]int64{"1.4 GB": f(1.4, 30), "700 MiB": 700 << 20, "2,5 Go": f(2.5, 30), "1,234.5 MB": f(1234.5, 20), "512 KB": 512 << 10}
	for in, want := range sizes {
		if got := GetBytes(in); got != want {
			t.Errorf("GetBytes(%q)=%d want %d", in, got, want)
		}
	}
	for _, in := range []string{"2 hours ago", "Today 12:30", "yesterday", "2024-03-01 10:00:00", "Mar 1, 2024", "1700000000", "01/03/2024"} {
		if _, err := FromUnknown(in, ""); err != nil {
			t.Errorf("FromUnknown(%q): %v", in, err)
		}
	}
}
