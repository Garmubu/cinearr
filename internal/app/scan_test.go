package app

import (
	"encoding/json"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"cinearr/internal/model"
	"cinearr/internal/store"
	"cinearr/internal/tmdb"
)

func fakeTMDB() *httptest.Server {
	films := []map[string]any{
		{"id": 27205, "title": "Inception", "original_title": "Inception", "release_date": "2010-07-15"},
		{"id": 603, "title": "Matrix", "original_title": "The Matrix", "release_date": "1999-03-30"},
	}
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		enc := json.NewEncoder(w)
		switch {
		case r.URL.Path == "/search/movie":
			q := strings.ToLower(r.URL.Query().Get("query"))
			var out []map[string]any
			for _, f := range films {
				if strings.Contains(strings.ToLower(f["original_title"].(string)), q) || strings.Contains(strings.ToLower(f["title"].(string)), q) {
					out = append(out, f)
				}
			}
			enc.Encode(map[string]any{"results": out, "total_pages": 1})
		case strings.HasPrefix(r.URL.Path, "/movie/"):
			for _, f := range films {
				if r.URL.Path == "/movie/"+itoa(f["id"].(int)) {
					enc.Encode(f)
					return
				}
			}
			http.NotFound(w, r)
		case r.URL.Path == "/search/tv":
			enc.Encode(map[string]any{"results": []map[string]any{{"id": 1920, "name": "Twin Peaks", "original_name": "Twin Peaks", "first_air_date": "1990-04-08"}}, "total_pages": 1})
		case r.URL.Path == "/tv/1920":
			enc.Encode(map[string]any{"id": 1920, "name": "Twin Peaks", "original_name": "Twin Peaks", "first_air_date": "1990-04-08",
				"seasons": []map[string]any{{"season_number": 2, "episode_count": 2}}})
		case r.URL.Path == "/tv/1920/season/2":
			enc.Encode(map[string]any{"season_number": 2, "episodes": []map[string]any{
				{"episode_number": 1, "season_number": 2, "name": "One", "air_date": "1990-09-30"},
				{"episode_number": 2, "season_number": 2, "name": "Two", "air_date": "1990-10-06"}}})
		default:
			http.NotFound(w, r)
		}
	}))
}

func itoa(n int) string { b, _ := json.Marshal(n); return string(b) }

func touch(t *testing.T, p string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
}

// The scan finds films in folders and at the root, files moved by hand,
// subtitles added next to the videos, deleted files, and series episodes.
func TestScanLibrary(t *testing.T) {
	srv := fakeTMDB()
	defer srv.Close()
	old := tmdb.APIBase
	tmdb.APIBase = srv.URL
	defer func() { tmdb.APIBase = old }()

	dir := t.TempDir()
	films, series := filepath.Join(dir, "Films"), filepath.Join(dir, "Series")
	touch(t, filepath.Join(films, "Inception (2010)", "Inception (2010) [Bluray-1080p].mkv"))
	touch(t, filepath.Join(films, "Inception (2010)", "Inception (2010) [Bluray-1080p].fr.srt"))
	touch(t, filepath.Join(films, "The.Matrix.1999.1080p.BluRay.x264-GRP.mkv")) // at the root
	touch(t, filepath.Join(films, "Unknown.Home.Video.2021.mkv"))
	touch(t, filepath.Join(series, "Twin Peaks (1990)", "Season 02", "Twin Peaks - S02E01 - One [HDTV-720p].mkv"))
	touch(t, filepath.Join(series, "Loose.Episode.S01E01.mkv")) // ignored: not in a series folder
	// left out: recycle bins, NAS thumbnails, bonus folders and videos
	touch(t, filepath.Join(films, "$RECYCLE.BIN", "S-1-5-21", "Deleted.Film.2012.mkv"))
	touch(t, filepath.Join(films, "@eaDir", "Thumb.Film.2015.mkv"))
	touch(t, filepath.Join(films, "Inception (2010)", "Featurettes", "Making.Of.2010.mkv"))
	touch(t, filepath.Join(films, "Inception (2010)", "Inception-trailer.mkv"))
	touch(t, filepath.Join(series, "Twin Peaks (1990)", "Extras", "Twin Peaks - S02E02 - Bonus.mkv"))
	touch(t, filepath.Join(series, ".Trash-1000", "Twin Peaks - S02E03.mkv"))

	st, err := store.Open(filepath.Join(dir, "data"))
	if err != nil {
		t.Fatal(err)
	}
	s := st.Settings()
	s.SetupDone, s.TMDBKey, s.LibraryPath, s.SeriesPath = true, "k", films, series
	_ = st.SaveSettings(s)
	// a film whose file was deleted, its folder still there
	touch(t, filepath.Join(films, "Gone (2001)", "keep.txt"))
	_ = st.PutMovie(model.Movie{TmdbID: 1, Title: "Gone", Year: 2001, Status: model.StatusAvailable,
		File: &model.MovieFile{Path: filepath.Join(films, "Gone (2001)", "Gone (2001).mkv")}})
	a := New(st, log.New(io.Discard, "", 0))

	if !a.StartScan() {
		t.Fatal("scan not started")
	}
	for i := 0; i < 100 && a.LastScan().Running; i++ {
		time.Sleep(20 * time.Millisecond)
	}
	r := a.LastScan()
	t.Logf("report: %+v", r)
	inc, ok := st.Movie(27205)
	if !ok || inc.File == nil || inc.File.QualityName != "Bluray-1080p" || len(inc.Subtitles) != 1 || inc.Subtitles[0].Language != "fr" {
		t.Fatalf("Inception = %+v", inc)
	}
	if m, ok := st.Movie(603); !ok || m.File == nil {
		t.Fatalf("film at the root not found: %+v", m)
	}
	if g, _ := st.Movie(1); g.File != nil || g.Status != model.StatusWanted {
		t.Fatalf("deleted file still attached: %+v", g)
	}
	if len(r.Unknown) != 1 || !strings.Contains(r.Unknown[0], "Unknown.Home.Video") {
		t.Fatalf("unknown = %v", r.Unknown)
	}
	tp, ok := st.Series(1920)
	if !ok || tp.Episode(2, 1).File == nil || tp.Episode(2, 1).Status != model.StatusAvailable || tp.Episode(2, 2).File != nil {
		t.Fatalf("series = %+v", tp)
	}

	// the films folder is renamed by the user: the paths follow
	newFilms := filepath.Join(dir, "Movies")
	if err := os.Rename(films, newFilms); err != nil {
		t.Fatal(err)
	}
	if n := a.MoveLibrary(films, newFilms, false); n != 2 {
		t.Fatalf("%d films moved", n)
	}
	inc, _ = st.Movie(27205)
	if _, err := os.Stat(inc.File.Path); err != nil || !strings.HasPrefix(inc.Subtitles[0].Path, newFilms) {
		t.Fatalf("paths not rewritten: %+v", inc)
	}
}

func TestSkipDir(t *testing.T) {
	for _, c := range []struct {
		name   string
		inside bool
		want   bool
	}{
		{"$RECYCLE.BIN", false, true}, {"System Volume Information", false, true}, {"@eaDir", false, true},
		{".@__thumb", false, true}, {"#recycle", false, true}, {".hidden", false, true},
		{"Trailers", true, true}, {"Behind The Scenes", true, true}, {"Season 01", true, false},
		{"Specials", true, false}, {"Trailers", false, false}, {"Inception (2010)", false, false},
		{"$WINDOWS.~BT", false, true}, {"$9.99 (2008)", false, false}, {"...And Justice for All (1979)", false, false},
	} {
		if got := skipDir(c.name, c.inside); got != c.want {
			t.Errorf("skipDir(%q, %v) = %v", c.name, c.inside, got)
		}
	}
	if !extrasFile.MatchString("inception-trailer.mkv") || extrasFile.MatchString("the-other-side.2018.mkv") {
		t.Error("extras file rule")
	}
}

// A damaged library.json is rebuilt from the films folder.
func TestRebuildDamagedLibrary(t *testing.T) {
	srv := fakeTMDB()
	defer srv.Close()
	old := tmdb.APIBase
	tmdb.APIBase = srv.URL
	defer func() { tmdb.APIBase = old }()

	dir := t.TempDir()
	films, data := filepath.Join(dir, "Films"), filepath.Join(dir, "data")
	touch(t, filepath.Join(films, "Inception (2010)", "Inception (2010) [Bluray-1080p].mkv"))
	st, err := store.Open(data)
	if err != nil {
		t.Fatal(err)
	}
	s := st.Settings()
	s.SetupDone, s.TMDBKey, s.LibraryPath = true, "k", films
	_ = st.SaveSettings(s)
	if err := os.WriteFile(filepath.Join(data, "library.json"), []byte(`[{"tmdbId":603,"title":"Matrix","status":"wanted"},{"tmd`), 0o600); err != nil {
		t.Fatal(err)
	}

	st, err = store.Open(data)
	if err != nil {
		t.Fatalf("Open refused the damaged library: %v", err)
	}
	a := New(st, log.New(io.Discard, "", 0))
	a.reportRecovery()
	a.StartScan()
	for i := 0; i < 100 && a.LastScan().Running; i++ {
		time.Sleep(20 * time.Millisecond)
	}
	if _, ok := st.Movie(603); !ok {
		t.Fatal("readable entry lost")
	}
	if m, ok := st.Movie(27205); !ok || m.File == nil {
		t.Fatalf("film not rebuilt from the folder: %+v", m)
	}
	if r := a.LastScan(); len(r.Notes) != 1 || !strings.Contains(r.Notes[0], "library.json") {
		t.Fatalf("notes = %v", r.Notes)
	}
	h := st.History()
	found := false
	for _, e := range h {
		found = found || (e.Kind == "warning" && strings.Contains(e.Message, "library.json"))
	}
	if !found {
		t.Fatalf("no warning in the history: %+v", h)
	}
}
