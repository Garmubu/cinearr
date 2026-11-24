package app

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"cinearr/internal/subtitles"
)

// A copy that would leave less than the minimum free space is refused
// before writing anything; a hard link needs no space.
func TestImportFreeSpace(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "dl", "Film.mkv")
	touch(t, src)
	lib := filepath.Join(dir, "lib")
	if err := os.MkdirAll(lib, 0o755); err != nil {
		t.Fatal(err)
	}
	huge := int64(1) << 62
	err := transfer(src, filepath.Join(lib, "a.mkv"), "copy", huge)
	if err == nil || !strings.Contains(err.Error(), "free space") {
		t.Fatalf("copy with no room: %v", err)
	}
	if _, err := os.Stat(filepath.Join(lib, "a.mkv.partial")); !os.IsNotExist(err) {
		t.Fatal("partial file left")
	}
	if err := transfer(src, filepath.Join(lib, "b.mkv"), "copy", 100<<20); err != nil {
		t.Fatalf("normal copy: %v", err)
	}
	if err := transfer(src, filepath.Join(lib, "c.mkv"), "hardlink", huge); err != nil {
		t.Fatalf("hard link checked for space: %v", err)
	}
	if usableSpace(lib, 0) <= 0 || usableSpace(lib, 1<<40) != 1 || usableSpace("", 100) != 0 {
		t.Fatal("usableSpace")
	}
}

// Extensions refused by the user stop the import of a download.
func TestScanDownloadRejected(t *testing.T) {
	dir := t.TempDir()
	touch(t, filepath.Join(dir, "Film", "Film.mkv"))
	touch(t, filepath.Join(dir, "Film", "Visit.url"))
	if vs, problem := scanDownload(filepath.Join(dir, "Film"), 0, nil); problem != "" || len(vs) != 1 {
		t.Fatalf("without the setting: %v %q", vs, problem)
	}
	if _, problem := scanDownload(filepath.Join(dir, "Film"), 0, []string{".url"}); !strings.Contains(problem, "Visit.url") {
		t.Fatalf("refused extension imported: %q", problem)
	}
}

// A language code coming from a provider or a request can never place a
// subtitle outside the folder of the video.
func TestSubtitleLanguageTraversal(t *testing.T) {
	dir := t.TempDir()
	video := filepath.Join(dir, "Films", "Film (2020)", "Film (2020).mkv")
	touch(t, video)
	srt := []byte("1\n00:00:01,000 --> 00:00:02,000\nHi\n")
	for _, lang := range []string{"../../../evil", `..\..\evil`, "fr/../../evil"} {
		if _, err := writeSubtitle(subtitles.Video{Path: video}, srt, "x.srt", lang, false, false); err == nil {
			t.Errorf("language %q accepted", lang)
		}
	}
	entries, _ := os.ReadDir(dir)
	if len(entries) != 1 {
		t.Fatalf("file written outside the film folder: %v", entries)
	}
	sf, err := writeSubtitle(subtitles.Video{Path: video}, srt, "x.srt", "PT_br", false, false)
	if err != nil || filepath.Base(sf.Path) != "Film (2020).pt-br.srt" {
		t.Fatalf("%+v %v", sf, err)
	}
}
