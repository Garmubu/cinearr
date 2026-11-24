package app

import (
	"io"
	"log"
	"os"
	"path/filepath"
	"testing"
	"time"

	"cinearr/internal/model"
	"cinearr/internal/store"
)

func TestRecycleBin(t *testing.T) {
	dir := t.TempDir()
	films := filepath.Join(dir, "Films")
	st, err := store.Open(filepath.Join(dir, "data"))
	if err != nil {
		t.Fatal(err)
	}
	s := st.Settings()
	s.LibraryPath = films
	_ = st.SaveSettings(s)
	a := New(st, log.New(io.Discard, "", 0))

	video := filepath.Join(films, "Film (2020)", "Film (2020).mkv")
	sub := filepath.Join(films, "Film (2020)", "Film (2020).fr.srt")
	touch(t, video)
	touch(t, sub)
	a.removeFile(&model.MovieFile{Path: video}, []model.SubtitleFile{{Path: sub}})
	day := time.Now().Format("2006-01-02")
	binned := filepath.Join(films, RecycleDir, day, "Film (2020)", "Film (2020).mkv")
	if _, err := os.Stat(binned); err != nil {
		t.Fatalf("video not in the recycle bin: %v", err)
	}
	if _, err := os.Stat(filepath.Join(films, "Film (2020)")); !os.IsNotExist(err) {
		t.Fatal("empty film folder kept")
	}
	// the same name deleted twice the same day is kept twice
	touch(t, video)
	a.recycle(video)
	if _, err := os.Stat(filepath.Join(films, RecycleDir, day, "Film (2020)", "Film (2020) (2).mkv")); err != nil {
		t.Fatalf("second copy: %v", err)
	}
	// a file outside the library folders is deleted
	outside := filepath.Join(dir, "elsewhere.mkv")
	touch(t, outside)
	a.recycle(outside)
	if _, err := os.Stat(outside); !os.IsNotExist(err) {
		t.Fatal("file outside the library kept")
	}

	// cleanup: old days go, recent ones stay
	old := filepath.Join(films, RecycleDir, time.Now().AddDate(0, 0, -30).Format("2006-01-02"))
	touch(t, filepath.Join(old, "Old.mkv"))
	a.cleanRecycle()
	if _, err := os.Stat(old); !os.IsNotExist(err) {
		t.Fatal("old day not erased")
	}
	if _, err := os.Stat(binned); err != nil {
		t.Fatal("recent day erased")
	}
	// disabled bin: files are deleted at once, the bin is emptied
	s.RecycleDays = 0
	_ = st.SaveSettings(s)
	touch(t, video)
	a.recycle(video)
	a.cleanRecycle()
	if _, err := os.Stat(video); !os.IsNotExist(err) {
		t.Fatal("file kept with the bin disabled")
	}
	if _, err := os.Stat(filepath.Join(films, RecycleDir)); !os.IsNotExist(err) {
		t.Fatal("bin not emptied")
	}
}
