package subtitles

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

var twinPeaks = Video{Title: "Twin Peaks", Year: 1990, Season: 2, Episode: 1, TvdbID: 70533, ReleaseName: "Twin.Peaks.S02E01.1080p.BluRay.x264-SHORTBREHD"}

func TestGestdown(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/shows/external/tvdb/70533":
			fmt.Fprint(w, `{"shows":[{"id":"6a1b","name":"Twin Peaks","nbSeasons":3}]}`)
		case "/subtitles/get/6a1b/2/1/French":
			fmt.Fprint(w, `{"matchingSubtitles":[{"subtitleId":"s1","version":"SHORTBREHD","completed":true,"hearingImpaired":false,"language":"French","downloadUri":"/subtitles/download/s1","qualities":["1080p"],"downloadCount":42},
				{"subtitleId":"s2","version":"WIP","completed":false,"downloadUri":"/subtitles/download/s2"}]}`)
		case "/subtitles/get/6a1b/2/1/English":
			w.WriteHeader(http.StatusNotFound)
		case "/subtitles/download/s1":
			fmt.Fprint(w, srt)
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()
	g := &Gestdown{server: srv.URL}
	cs, err := g.Search(context.Background(), twinPeaks, []string{"fr", "en"})
	if err != nil || len(cs) != 1 {
		t.Fatalf("candidates = %+v, %v", cs, err)
	}
	c := cs[0]
	if c.Language != "fr" || !c.EpisodeMatch || !strings.Contains(c.Release, "S02E01") {
		t.Fatalf("candidate = %+v", c)
	}
	Score(&c, twinPeaks, false)
	if c.Percent < 80 {
		t.Errorf("score %d%% (%v)", c.Percent, c.Matches)
	}
	data, err := g.Download(context.Background(), WithRef(cs[0]))
	if err != nil || !strings.Contains(string(data), "Bonjour") {
		t.Fatalf("download = %q, %v", data, err)
	}
	if films, _ := g.Search(context.Background(), Video{Title: "Inception"}, []string{"fr"}); films != nil {
		t.Error("films searched on a series-only provider")
	}
}

func TestTVSubtitles(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/search1.php":
			if r.FormValue("qs") != "Twin Peaks" {
				t.Errorf("query %q", r.FormValue("qs"))
			}
			fmt.Fprint(w, `<div class="left"><ul><li><div><a href="/tvshow-140.html">Twin Peaks (1990-1991)</a></div></li><li><div><a href="/tvshow-9.html">Twin Peaks: The Return (2017-2017)</a></div></li></ul></div>`)
		case "/tvshow-140-2.html":
			fmt.Fprint(w, `<table id="table5"><tr><td>2x01</td><td><a href="episode-2001.html">May the Giant Be with You</a></td></tr><tr><td>2x02</td><td><a href="episode-2002.html">Coma</a></td></tr></table>`)
		case "/episode-2001.html":
			fmt.Fprint(w, `<a href="/subtitle-555.html"><div class="subtitlen"><h5><img src="images/flags/fr.gif"> DVDRip</h5><p title="rip">SHORTBREHD</p></div></a>
				<a href="/subtitle-556.html"><div class="subtitlen"><h5><img src="images/flags/de.gif"> HDTV</h5></div></a>`)
		case "/download-555.html":
			fmt.Fprint(w, srt)
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()
	p := &TVSubtitles{server: srv.URL + "/"}
	cs, err := p.Search(context.Background(), twinPeaks, []string{"fr"})
	if err != nil || len(cs) != 1 || cs[0].Language != "fr" || cs[0].ref != "555" {
		t.Fatalf("candidates = %+v, %v", cs, err)
	}
	data, err := p.Download(context.Background(), cs[0])
	if err != nil || !strings.Contains(string(data), "Bonjour") {
		t.Fatalf("download = %q, %v", data, err)
	}
}

func TestBetaSeries(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("key") != "k" {
			w.WriteHeader(http.StatusBadRequest)
			fmt.Fprint(w, `{"errors":[{"code":1001,"text":"Invalid key"}]}`)
			return
		}
		fmt.Fprintf(w, `{"episodes":[{"subtitles":[{"id":1,"language":"VF","source":"addic7ed","file":"Twin.Peaks.S02E01.DVDRip.srt","url":"%[1]s/dl/1"},{"id":2,"language":"VO","source":"seriessub","file":"x.srt","url":"%[1]s/dl/2"}]}],"errors":[]}`, "http://"+r.Host)
	}))
	defer srv.Close()
	b := &BetaSeries{Key: "k", server: srv.URL + "/"}
	cs, err := b.Search(context.Background(), twinPeaks, []string{"fr", "en"})
	if err != nil || len(cs) != 1 || cs[0].Language != "fr" {
		t.Fatalf("candidates = %+v, %v", cs, err)
	}
	if _, err := (&BetaSeries{Key: "bad", server: srv.URL + "/"}).Test(context.Background()); err == nil {
		t.Error("bad key accepted")
	}
}

func TestCustomProviders(t *testing.T) {
	link := SearchLink("https://subs.example.org/search?q={query}&imdb={imdb}&ep={s00e00}", LinkParams{Query: "Twin Peaks S02E01", ImdbID: "tt0098936", Season: 2, Episode: 1})
	if link != "https://subs.example.org/search?q=Twin+Peaks+S02E01&imdb=tt0098936&ep=S02E01" {
		t.Fatal(link)
	}
	cfgs := []Config{{ID: "custom-link", Enabled: true, Settings: map[string]string{"name": "Mon site", "url": "https://x.org/?q={query}"}},
		{ID: "custom-api", Enabled: true, Settings: map[string]string{"name": "Hub", "url": "https://hub.example.org/api/v1", "apiKey": "k"}}}
	if l := Links(cfgs, LinkParams{Query: "Inception 2010"}); len(l) != 1 || l[0].Name != "Mon site" {
		t.Fatalf("links = %+v", l)
	}
	provs, errs := Build(cfgs)
	if len(errs) != 0 || len(provs) != 1 || provs[0].Name() != "custom:Hub" {
		t.Fatalf("providers = %v, %v", provs, errs)
	}
	if o := provs[0].(*OpenSubtitles); o.server() != "https://hub.example.org/api/v1/" {
		t.Fatal(o.server())
	}
}
