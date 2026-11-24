package app

import (
	"context"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"testing"

	"cinearr/internal/dlclient"
	"cinearr/internal/model"
	"cinearr/internal/store"
)

// A torrent deleted by the user in the client is forgotten: the film goes
// back to "wanted", without blocklist nor new search.
func TestTorrentRemovedFromClient(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v2/auth/login":
			_, _ = io.WriteString(w, "Ok.")
		case "/api/v2/app/webapiVersion":
			_, _ = io.WriteString(w, "2.11.2")
		case "/api/v2/torrents/info":
			_, _ = io.WriteString(w, "[]")
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()
	st, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	s := st.Settings()
	s.SetupDone = true
	s.AutoGrab = true
	s.Client = dlclient.Settings{Kind: dlclient.QBittorrent, URL: srv.URL}
	if err := st.SaveSettings(s); err != nil {
		t.Fatal(err)
	}
	_ = st.PutMovie(model.Movie{TmdbID: 1, Title: "Film", Status: model.StatusDownloading,
		Download: &model.Download{Hash: "0123456789ABCDEF0123456789ABCDEF01234567", ReleaseTitle: "Film.2020.1080p"}})
	a := New(st, log.New(io.Discard, "", 0))
	for i := 0; i < 12; i++ {
		a.syncDownloads(context.Background())
	}
	m, _ := st.Movie(1)
	if m.Download != nil || m.Status != model.StatusWanted {
		t.Fatalf("movie = %+v", m)
	}
	if len(st.Blocklist()) != 0 {
		t.Fatal("release blocklisted")
	}
}
