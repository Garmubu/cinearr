package store

import (
	"os"
	"path/filepath"
	"testing"

	"cinearr/internal/dlclient"
	"cinearr/internal/model"
)

func TestMigrateQBitSettings(t *testing.T) {
	dir := t.TempDir()
	old := `{"setupDone":true,"qbit":{"url":"http://192.168.1.5:8081","username":"admin","password":"pw","category":"films","ratioLimit":-2,"seedingTimeLimit":-2}}`
	if err := os.WriteFile(filepath.Join(dir, "settings.json"), []byte(old), 0o644); err != nil {
		t.Fatal(err)
	}
	st, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	c := st.Settings().Client
	if c.Kind != dlclient.QBittorrent || c.URL != "http://192.168.1.5:8081" || c.Username != "admin" || c.Category != "films" {
		t.Fatalf("migrated client = %+v", c)
	}
	if st.Settings().LegacyQBit != nil {
		t.Fatal("legacy block kept")
	}
	// a new file keeps the chosen client
	s := st.Settings()
	s.Client = dlclient.Settings{Kind: dlclient.Transmission, URL: "http://127.0.0.1:9091"}
	if err := st.SaveSettings(s); err != nil {
		t.Fatal(err)
	}
	st2, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	if k := st2.Settings().Client.Kind; k != dlclient.Transmission {
		t.Fatalf("kind = %q", k)
	}
}

// The settings of the clients that are not selected are kept, and so is
// the path of "Other player" while another player is selected.
func TestOtherClientsAndPlayerPath(t *testing.T) {
	st, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	s := st.Settings()
	qbit := dlclient.Settings{Kind: dlclient.QBittorrent, URL: "http://127.0.0.1:8080", Username: "admin", Password: "pw", Category: "cinearr"}
	s.Client = dlclient.Settings{Kind: dlclient.Transmission, URL: "http://127.0.0.1:9091"}
	s.OtherClients = map[string]dlclient.Settings{
		dlclient.QBittorrent:  qbit,
		dlclient.Transmission: {Kind: dlclient.Transmission, URL: "stale"}, // the selected one
		"unknown":             {URL: "x"},
	}
	s.Player = PlayerSettings{Kind: "vlc", Path: "/opt/my-player"}
	if err := st.SaveSettings(s); err != nil {
		t.Fatal(err)
	}
	got := st.Settings()
	if len(got.OtherClients) != 1 || got.OtherClients[dlclient.QBittorrent] != qbit {
		t.Fatalf("other clients = %+v", got.OtherClients)
	}
	if got.Client.URL != "http://127.0.0.1:9091" {
		t.Fatalf("selected client = %+v", got.Client)
	}
	if got.Player.Path != "/opt/my-player" {
		t.Fatalf("own player lost while VLC is selected: %q", got.Player.Path)
	}
}

func TestMigrateSubtitleSettings(t *testing.T) {
	dir := t.TempDir()
	old := `{"setupDone":true,"subtitles":{"auto":true,"languages":["fr"],"openSubtitlesUser":"garm","openSubtitlesPass":"pw","subdlKey":"sk"}}`
	if err := os.WriteFile(filepath.Join(dir, "settings.json"), []byte(old), 0o644); err != nil {
		t.Fatal(err)
	}
	st, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	p := st.Settings().Subtitles.Providers
	if len(p) != 2 || p[0].ID != "opensubtitles" || p[0].Settings["username"] != "garm" || p[1].ID != "subdl" || p[1].Settings["apiKey"] != "sk" {
		t.Fatalf("providers = %+v", p)
	}
	if st.Settings().Subtitles.OpenSubtitlesUser != "" {
		t.Fatal("old field kept")
	}
}

// A damaged library does not stop Cinearr: the readable entries are kept
// and the damaged file is set aside.
func TestDamagedLibrary(t *testing.T) {
	dir := t.TempDir()
	write := func(name, content string) {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	write("library.json", `[{"tmdbId":1,"title":"A"},{"tmdbId":2,"title":"B"},{"tmdbId":3,"ti`) // cut in the middle
	write("series.json", "\x00\x00garbage")
	write("history.json", "")
	st, err := Open(dir)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	if len(st.Movies()) != 2 {
		t.Fatalf("movies kept = %d", len(st.Movies()))
	}
	if len(st.AllSeries()) != 0 {
		t.Fatal("series from garbage")
	}
	rs := st.Recovered()
	if len(rs) != 3 || rs[0].File != "library.json" || rs[0].Kept != 2 || rs[0].Aside == "" || rs[1].File != "series.json" || rs[1].Kept != 0 {
		t.Fatalf("recovered = %+v", rs)
	}
	if _, err := os.Stat(filepath.Join(dir, "library.json")); !os.IsNotExist(err) {
		t.Fatal("damaged file not set aside")
	}
	if _, err := os.Stat(filepath.Join(dir, rs[0].Aside)); err != nil {
		t.Fatalf("damaged copy: %v", err)
	}
	// the next save writes a clean file
	if err := st.PutMovie(st.Movies()[0]); err != nil {
		t.Fatal(err)
	}
	st2, err := Open(dir)
	if err != nil || len(st2.Movies()) != 2 || len(st2.Recovered()) != 0 {
		t.Fatalf("reopen: %v %d %+v", err, len(st2.Movies()), st2.Recovered())
	}
}

// Reset returns to the first launch: settings, history and the lists of
// films and series are emptied, on disk too, and no video file is touched.
func TestReset(t *testing.T) {
	dir := t.TempDir()
	video := filepath.Join(dir, "Films", "Inception (2010)", "Inception (2010).mkv")
	if err := os.MkdirAll(filepath.Dir(video), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(video, []byte("video"), 0o644); err != nil {
		t.Fatal(err)
	}
	st, err := Open(filepath.Join(dir, "data"))
	if err != nil {
		t.Fatal(err)
	}
	s := st.Settings()
	s.SetupDone, s.LibraryPath, s.TMDBKey = true, filepath.Join(dir, "Films"), "key"
	if err := st.SaveSettings(s); err != nil {
		t.Fatal(err)
	}
	if err := st.PutMovie(model.Movie{TmdbID: 27205, Title: "Inception", File: &model.MovieFile{Path: video}}); err != nil {
		t.Fatal(err)
	}
	if err := st.PutSeries(model.Series{TmdbID: 1920, Title: "Twin Peaks"}); err != nil {
		t.Fatal(err)
	}
	st.AddHistory(model.HistoryEvent{Kind: "grabbed", Message: "x"})
	if err := st.Reset(); err != nil {
		t.Fatal(err)
	}
	reopened, err := Open(filepath.Join(dir, "data"))
	if err != nil {
		t.Fatal(err)
	}
	for _, x := range []*Store{st, reopened} {
		got := x.Settings()
		if got.SetupDone || got.LibraryPath != "" || got.TMDBKey != "" {
			t.Fatalf("settings kept: %+v", got)
		}
		if len(x.Movies()) != 0 || len(x.AllSeries()) != 0 || len(x.History()) != 0 {
			t.Fatalf("library or history kept: %d films, %d series, %d events", len(x.Movies()), len(x.AllSeries()), len(x.History()))
		}
	}
	if _, err := os.Stat(video); err != nil {
		t.Fatalf("video file touched: %v", err)
	}
}
