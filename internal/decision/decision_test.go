package decision

import (
	"testing"

	"cinearr/internal/model"
	"cinearr/internal/quality"
)

func TestEvaluate(t *testing.T) {
	movie := model.Movie{TmdbID: 27205, ImdbID: "tt1375666", Title: "Inception", OriginalTitle: "Inception", Year: 2010, Runtime: 148}
	rel := func(title string, size int64, seeds int) model.Release {
		return model.Release{Title: title, Size: size, Seeders: seeds, DownloadURL: "http://x/" + title, Indexer: "t"}
	}
	rs := []model.Release{
		rel("Inception.2010.MULTi.1080p.BluRay.x264-QTZ", 9<<30, 200),
		rel("Inception.2010.720p.WEB-DL.x264-ABC", 3<<30, 500),
		rel("Inception.2010.1080p.BluRay.x264-FAKE", 300<<20, 900),
		rel("Inception.2010.HDCAM.x264", 1<<30, 50),
		rel("Interstellar.2014.1080p.BluRay.x264-AAA", 9<<30, 50),
		rel("Inception.2010.1080p.WEBRip.x264.exe", 2<<30, 50),
		rel("Inception.2010.1080p.BluRay.x264-NOSEED", 9<<30, 0),
	}
	ds := Evaluate(rs, DefaultProfile(), Context{Movie: movie})
	if !ds[0].Approved || ds[0].Release.Title != rs[0].Title {
		t.Fatalf("best = %s %v", ds[0].Release.Title, ds[0].Rejections)
	}
	approved := 0
	for _, d := range ds {
		if d.Approved {
			approved++
		}
	}
	if approved != 2 {
		for _, d := range ds {
			t.Logf("%v %s %v", d.Approved, d.Release.Title, d.Rejections)
		}
		t.Fatalf("approved %d, want 2", approved)
	}
	// an existing Bluray-1080p (the cutoff) blocks further grabs
	movie.File = &model.MovieFile{QualityID: quality.Bluray1080p.ID, QualityName: "Bluray-1080p"}
	for _, d := range Evaluate(rs, DefaultProfile(), Context{Movie: movie}) {
		if d.Approved {
			t.Fatalf("%s approved although cutoff reached", d.Release.Title)
		}
	}
}
