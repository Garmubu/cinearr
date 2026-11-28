package tmdb

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
)

// Posters are those of the original language of each title, else one
// without text, else the localised one; titles stay in the interface
// language.
func TestOriginalPosters(t *testing.T) {
	posters.reset()
	defer posters.reset()
	images := map[string]string{
		// Spirited Away (Japanese): a French, a Japanese and a textless poster
		"/movie/129/images": `{"posters":[{"file_path":"/fr.jpg","iso_639_1":"fr","vote_average":9},
			{"file_path":"/ja-low.jpg","iso_639_1":"ja","vote_average":4},{"file_path":"/ja.jpg","iso_639_1":"ja","vote_average":6},
			{"file_path":"/none.jpg","iso_639_1":null,"vote_average":8}]}`,
		// Inception (English): only a textless poster
		"/movie/27205/images": `{"posters":[{"file_path":"/textless.jpg","iso_639_1":null}]}`,
		// a series with no poster at all
		"/tv/1920/images": `{"posters":[]}`,
	}
	var mu sync.Mutex
	var asked []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		asked = append(asked, r.URL.Path+"?"+r.URL.Query().Get("include_image_language"))
		mu.Unlock()
		if body, ok := images[r.URL.Path]; ok {
			_, _ = w.Write([]byte(body))
			return
		}
		if r.URL.Path == "/movie/129" {
			_ = json.NewEncoder(w).Encode(map[string]any{"id": 129, "title": "Le Voyage de Chihiro", "original_language": "ja", "poster_path": "/fr.jpg"})
			return
		}
		http.NotFound(w, r)
	}))
	defer srv.Close()
	old := APIBase
	APIBase = srv.URL
	defer func() { APIBase = old }()
	c := New("key", "fr-FR")
	ctx := context.Background()

	list := []Summary{
		{ID: 129, Title: "Le Voyage de Chihiro", OriginalLanguage: "ja", PosterPath: "/fr.jpg"},
		{ID: 27205, Title: "Inception", OriginalLanguage: "en", PosterPath: "/inception-fr.jpg"},
		{ID: 680, Title: "Le Fabuleux Destin d'Amélie Poulain", OriginalLanguage: "fr", PosterPath: "/amelie.jpg"},
		{ID: 1, Title: "Unknown images", OriginalLanguage: "de", PosterPath: "/de-fr.jpg"},
	}
	c.OriginalPosters(ctx, false, list)
	want := []string{"/ja.jpg", "/textless.jpg", "/amelie.jpg", "/de-fr.jpg"}
	for i, w := range want {
		if list[i].PosterPath != w {
			t.Errorf("%s: poster %s, want %s", list[i].Title, list[i].PosterPath, w)
		}
	}
	if list[0].Title != "Le Voyage de Chihiro" {
		t.Errorf("title changed: %s", list[0].Title)
	}
	mu.Lock()
	seen := append([]string(nil), asked...)
	mu.Unlock()
	for _, a := range seen {
		if a == "/movie/680/images?fr,null" {
			t.Error("images asked for a film already in the interface language")
		}
	}
	if s, err := c.TV(ctx, 1920); err == nil && s.PosterPath != "" {
		t.Errorf("series without poster: %q", s.PosterPath)
	}
	// the details of a film carry the original poster too
	d, err := c.Movie(ctx, 129)
	if err != nil || d.PosterPath != "/ja.jpg" || d.Title != "Le Voyage de Chihiro" {
		t.Fatalf("details: %+v %v", d, err)
	}
	// remembered: no new request for a title already seen
	mu.Lock()
	n := len(asked)
	mu.Unlock()
	c.OriginalPosters(ctx, false, list[:1])
	mu.Lock()
	defer mu.Unlock()
	if len(asked) != n {
		t.Errorf("poster asked again: %v", asked[n:])
	}
}
