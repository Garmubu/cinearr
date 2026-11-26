package subtitles

import (
	"archive/zip"
	"bytes"
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func zipped(name, content string) []byte {
	var buf bytes.Buffer
	w := zip.NewWriter(&buf)
	f, _ := w.Create(name)
	_, _ = f.Write([]byte(content))
	w.Close()
	return buf.Bytes()
}

const srt = "1\r\n00:00:01,000 --> 00:00:02,000\r\nBonjour\r\n"

func TestYIFY(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/movie-imdb/tt1375666":
			fmt.Fprint(w, `<html><body><table class="other-subs"><thead><tr><th>Rating</th></tr></thead><tbody>
<tr><td><span class="label">7</span></td><td class="flag-cell"><span class="sub-lang">French</span></td>
<td><a href="/subtitles/inception-2010-french-yify-1">subtitle Inception.2010.1080p.BluRay.x264-SPARKS
Inception.2010.720p.BluRay</a></td><td><span class="hi-subtitle"></span></td><td class="uploader-cell"><a>sub_man</a></td><td></td></tr>
<tr><td>3</td><td>Spanish</td><td><a href="/subtitles/inception-es">subtitle Inception.2010</a></td><td></td><td>x</td></tr>
</tbody></table></body></html>`)
		case "/subtitles/inception-2010-french-yify-1":
			fmt.Fprint(w, `<a class="btn download-subtitle" href="/subtitle/inception-2010-french-yify-1.zip">Download</a>`)
		case "/subtitle/inception-2010-french-yify-1.zip":
			_, _ = w.Write(zipped("Inception.fr.srt", srt))
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()
	y := &YIFY{server: srv.URL}
	cs, err := y.Search(context.Background(), Video{ImdbID: "tt1375666"}, []string{"fr", "en"})
	if err != nil || len(cs) != 1 {
		t.Fatalf("search: %v %+v", err, cs)
	}
	c := cs[0]
	if c.Language != "fr" || !c.HI || c.Downloads != 7 || !strings.Contains(c.Release, "SPARKS") || strings.HasPrefix(c.Release, "subtitle") {
		t.Fatalf("candidate = %+v", c)
	}
	data, err := y.Download(context.Background(), WithRef(c))
	if err != nil {
		t.Fatal(err)
	}
	if b, _, err := ExtractSubtitle(data); err != nil || string(b) != srt {
		t.Fatalf("extract: %v %q", err, b)
	}
	none, err := y.Search(context.Background(), Video{ImdbID: "tt0000001"}, []string{"fr"})
	if err != nil || len(none) != 0 {
		t.Fatalf("unknown film: %v %v", err, none)
	}
}

func TestSubsource(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("api_key") != "k" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		switch r.URL.Path {
		case "/api/v1/movies/search":
			fmt.Fprint(w, `{"success":true,"data":[{"movieId":4242,"title":"Inception","releaseYear":2010}]}`)
		case "/api/v1/subtitles":
			if r.URL.Query().Get("movieId") != "4242" || r.URL.Query().Get("language") != "french" {
				fmt.Fprint(w, `{"success":true,"data":[]}`)
				return
			}
			fmt.Fprint(w, `{"success":true,"data":[{"subtitleId":99,"releaseInfo":["Inception.2010.1080p.BluRay.x264-SPARKS"],"language":"french",
				"hearingImpaired":false,"foreignParts":false,"uploaderId":5,"contributors":[{"id":5,"displayname":"Gwen"}]}]}`)
		case "/api/v1/subtitles/99/download":
			_, _ = w.Write(zipped("a.srt", srt))
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()
	s := &Subsource{APIKey: "k", server: srv.URL + "/api/v1/"}
	cs, err := s.Search(context.Background(), Video{ImdbID: "tt1375666", Title: "Inception", Year: 2010}, []string{"fr"})
	if err != nil || len(cs) != 1 || cs[0].ID != "99" || cs[0].Uploader != "Gwen" || cs[0].Release != "Inception.2010.1080p.BluRay.x264-SPARKS" {
		t.Fatalf("search: %v %+v", err, cs)
	}
	if b, err := s.Download(context.Background(), WithRef(cs[0])); err != nil || !bytes.HasPrefix(b, []byte("PK")) {
		t.Fatalf("download: %v", err)
	}
	if _, err := (&Subsource{APIKey: "bad", server: srv.URL + "/api/v1/"}).Test(context.Background()); err == nil {
		t.Fatal("bad key accepted")
	}
}

func TestSubsynchro(t *testing.T) {
	var srv *httptest.Server // the handler needs its URL
	srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/include/ajax/subMarin.php":
			if r.URL.Query().Get("title") != "Inception" {
				fmt.Fprint(w, `{"status":404,"data":[]}`)
				return
			}
			fmt.Fprintf(w, `{"status":200,"data":[{"titre":"Inception","titre_original":"Inception","release":"Inception.2010.MULTi.1080p","filename":"x.srt","telechargement":"%s/dl/1.zip"}]}`, srv.URL)
		case "/dl/1.zip":
			_, _ = w.Write(zipped("x.srt", srt))
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()
	s := &Subsynchro{server: srv.URL}
	// the original title is unknown to the site: the local one is tried next
	cs, err := s.Search(context.Background(), Video{Title: "盗梦空间", AltTitles: []string{"Inception"}, Year: 2010}, []string{"fr"})
	if err != nil || len(cs) != 1 || cs[0].Language != "fr" {
		t.Fatalf("search: %v %+v", err, cs)
	}
	if b, err := s.Download(context.Background(), WithRef(cs[0])); err != nil || !bytes.HasPrefix(b, []byte("PK")) {
		t.Fatalf("download: %v", err)
	}
	if cs, _ := s.Search(context.Background(), Video{Title: "Inception"}, []string{"en"}); len(cs) != 0 {
		t.Fatal("French-only provider answered for English")
	}
}

func TestBuildCatalog(t *testing.T) {
	old := BuiltinOpenSubtitlesKey
	BuiltinOpenSubtitlesKey = ""
	defer func() { BuiltinOpenSubtitlesKey = old }()
	provs, errs := Build([]Config{
		{ID: "opensubtitles", Enabled: true, Settings: map[string]string{"username": "u", "password": "p"}}, // no key
		{ID: "yifysubtitles", Enabled: true},
		{ID: "subdl", Enabled: false, Settings: map[string]string{"apiKey": "x"}},
		{ID: "unknown", Enabled: true},
	})
	if len(provs) != 1 || provs[0].Name() != "yifysubtitles" || len(errs) != 1 {
		t.Fatalf("provs %v errs %v", provs, errs)
	}
	BuiltinOpenSubtitlesKey = "builtin"
	provs, _ = Build([]Config{{ID: "opensubtitles", Enabled: true, Settings: map[string]string{"username": "u", "password": "p"}}})
	if len(provs) != 1 || provs[0].(*OpenSubtitles).APIKey != "builtin" || !provs[0].(*OpenSubtitles).Builtin {
		t.Fatalf("builtin key not used: %v", provs)
	}
	provs, _ = Build([]Config{{ID: "opensubtitles", Enabled: true, Settings: map[string]string{"apiKey": "mine"}}})
	if len(provs) != 1 || provs[0].(*OpenSubtitles).APIKey != "mine" || provs[0].(*OpenSubtitles).Builtin {
		t.Fatalf("own key not used: %v", provs)
	}
	for _, p := range Catalog() {
		if p.build == nil && !p.Link {
			t.Errorf("%s has no constructor", p.ID)
		}
	}
}
