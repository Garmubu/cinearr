package app

import (
	"io"
	"log"
	"path/filepath"
	"testing"
	"time"

	"cinearr/internal/model"
	"cinearr/internal/store"
	"cinearr/internal/tmdb"
)

// After a change of language, the pages stored in the library are reloaded.
func TestMetadataRefresh(t *testing.T) {
	srv := fakeTMDB()
	defer srv.Close()
	old := tmdb.APIBase
	tmdb.APIBase = srv.URL
	defer func() { tmdb.APIBase = old }()

	st, err := store.Open(filepath.Join(t.TempDir(), "data"))
	if err != nil {
		t.Fatal(err)
	}
	s := st.Settings()
	s.SetupDone, s.TMDBKey, s.Language = true, "k", "en-US"
	_ = st.SaveSettings(s)
	_ = st.PutMovie(model.Movie{TmdbID: 27205, Title: "Inception (titre français)", Overview: "Résumé en français", Status: model.StatusWanted})
	a := New(st, log.New(io.Discard, "", 0))
	a.StartMetadataRefresh()
	for i := 0; i < 200 && a.MetadataStatus().Running; i++ {
		time.Sleep(10 * time.Millisecond)
	}
	r := a.MetadataStatus()
	if r.Running || r.Done != 1 || r.Total != 1 || r.Language != "en-US" {
		t.Fatalf("report %+v", r)
	}
	if m, _ := st.Movie(27205); m.Title != "Inception" || m.Status != model.StatusWanted {
		t.Fatalf("not reloaded: %+v", m)
	}
	if h := st.History(); len(h) == 0 || h[len(h)-1].Kind != "refresh" && h[0].Kind != "refresh" {
		t.Fatalf("history %+v", h)
	}
}
