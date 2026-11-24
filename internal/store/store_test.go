package store

import (
	"os"
	"path/filepath"
	"testing"

	"cinearr/internal/dlclient"
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
