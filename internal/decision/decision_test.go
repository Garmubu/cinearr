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

// The MULTi tag is found between any separators, and its bonus and the
// languages always agree (Jackie Brown, French profile).
func TestMultiTagAndScore(t *testing.T) {
	movie := model.Movie{TmdbID: 184, Title: "Jackie Brown", OriginalTitle: "Jackie Brown", Year: 1997, Runtime: 154, OriginalLanguage: "en"}
	cases := []struct {
		title string
		score int
		langs string
	}{
		{"Jackie.Brown.1997.MULTi.VF2.1080p.BluRay.x264-GRP", 30, "French, Original"},
		{"Jackie Brown 1997 [MULTi] 1080p BluRay x264", 30, "French, Original"},
		{"Jackie Brown 1997 1080p BluRay x264 MULTi-GRP", 30, "French, Original"},
		{"Jackie Brown (1997) [1080p] [MULTI] x265", 30, "French, Original"},
		{"MULTi.Jackie.Brown.1997.1080p.BluRay.x264", 30, "French, Original"},
		// no language tag: the original language of the film
		{"Jackie.Brown.1997.1080p.BluRay.x264-GRP", 0, "English, Original"},
		// "multi" inside a word is not the tag
		{"Jackie.Brown.1997.Multiverse.1080p.BluRay.x264-GRP", 0, "English, Original"},
	}
	for _, c := range cases {
		r := model.Release{Title: c.title, Size: 10 << 30, Seeders: 10, DownloadURL: "http://x/1", Indexer: "t"}
		d := Evaluate([]model.Release{r}, DefaultProfile(), Context{Movie: movie})[0]
		if d.Score != c.score || joinLangs(d.Languages) != c.langs {
			t.Errorf("%s: score %d languages %q, want %d %q", c.title, d.Score, joinLangs(d.Languages), c.score, c.langs)
		}
	}
}

// A release without any language tag is in the original language of the
// title; without it, the language stays unknown.
func TestOriginalLanguage(t *testing.T) {
	p := DefaultProfile()
	for _, c := range []struct{ original, want string }{{"en", "English, Original"}, {"fr", "French, Original"}, {"", "Unknown"}, {"xx", "Unknown"}} {
		if got := joinLangs(EffectiveLanguages("Film.2020.1080p.BluRay.x264-GRP", []string{"Unknown"}, p, c.original)); got != c.want {
			t.Errorf("original %q: %q, want %q", c.original, got, c.want)
		}
	}
	// a tagged release keeps its own languages
	if got := joinLangs(EffectiveLanguages("Film.2020.FRENCH.1080p.BluRay.x264-GRP", []string{"French"}, p, "en")); got != "French" {
		t.Errorf("tagged release: %q", got)
	}
}

func joinLangs(l []string) string {
	out := ""
	for i, x := range l {
		if i > 0 {
			out += ", "
		}
		out += x
	}
	return out
}
