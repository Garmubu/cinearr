package app

import (
	"os"
	"path/filepath"
	"testing"

	"cinearr/internal/store"
)

func TestCheckFolders(t *testing.T) {
	dir := t.TempDir()
	data := filepath.Join(dir, "data")
	films, series := filepath.Join(dir, "Films"), filepath.Join(dir, "Series")
	home, _ := os.UserHomeDir()
	ok := store.Settings{LibraryPath: films, SeriesPath: series}
	ok.Client.SavePath = filepath.Join(dir, "Downloads")
	if err := CheckFolders(ok, data); err != nil {
		t.Fatalf("valid folders refused: %v", err)
	}
	bad := map[string]store.Settings{
		"relative":         {LibraryPath: "Films"},
		"system disk":      {LibraryPath: systemDrive()},
		"system folder":    {LibraryPath: filepath.Join(systemFolders()[0], "Films")},
		"home":             {LibraryPath: home},
		"data folder":      {LibraryPath: data},
		"inside data":      {LibraryPath: filepath.Join(data, "Films")},
		"contains data":    {LibraryPath: dir},
		"same as series":   {LibraryPath: films, SeriesPath: films},
		"series inside":    {LibraryPath: films, SeriesPath: filepath.Join(films, "Series")},
		"downloads":        {LibraryPath: films, Client: ok.Client},
		"downloads mapped": {LibraryPath: films, PathMappings: []store.PathMapping{{Remote: "/downloads", Local: films}}},
	}
	bad["downloads"] = func() store.Settings { s := bad["downloads"]; s.Client.SavePath = films; return s }()
	bad["downloads inside"] = func() store.Settings {
		s := store.Settings{LibraryPath: films}
		s.Client.SavePath = filepath.Join(films, "Torrents")
		return s
	}()
	bad["downloads mapped"] = func() store.Settings {
		s := bad["downloads mapped"]
		s.Client.SavePath = "/downloads/cinearr"
		return s
	}()
	for name, s := range bad {
		if err := CheckFolders(s, data); err == nil {
			t.Errorf("%s accepted: %+v", name, s.LibraryPath)
		}
	}
	// a library inside the downloads folder is accepted (Radarr's rule)
	in := store.Settings{LibraryPath: filepath.Join(dir, "Downloads", "Films")}
	in.Client.SavePath = filepath.Join(dir, "Downloads")
	if err := CheckFolders(in, data); err != nil {
		t.Fatalf("library inside downloads refused: %v", err)
	}
}
