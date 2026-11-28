package decision

import (
	"testing"
	"time"

	"cinearr/internal/model"
)

func twinPeaks() model.Series {
	s := model.Series{TmdbID: 1920, Title: "Twin Peaks", Year: 1990, Runtime: 47}
	s1 := model.Season{Number: 1}
	for e := 1; e <= 8; e++ {
		s1.Episodes = append(s1.Episodes, model.Episode{Season: 1, Number: e, AirDate: "1990-04-08"})
	}
	s2 := model.Season{Number: 2}
	for e := 1; e <= 22; e++ {
		s2.Episodes = append(s2.Episodes, model.Episode{Season: 2, Number: e, AirDate: "1990-09-30"})
	}
	s.Seasons = []model.Season{{Number: 0, Episodes: []model.Episode{{Season: 0, Number: 1, AirDate: "1992-01-01"}}}, s1, s2}
	return s
}

func rel(title string, gb float64) model.Release {
	return model.Release{Title: title, Size: int64(gb * (1 << 30)), Seeders: 20, MagnetURL: "magnet:?xt=urn:btih:abc", Indexer: "t", PublishDate: time.Now()}
}

func TestEvaluateEpisodes(t *testing.T) {
	p := DefaultProfile()
	p.RequiredLanguages = nil
	wanted := []model.EpisodeKey{{Season: 2, Episode: 1}, {Season: 2, Episode: 2}, {Season: 2, Episode: 3}}
	ctx := EpisodeContext{Series: twinPeaks(), Wanted: wanted}
	ds := EvaluateEpisodes([]model.Release{
		rel("Twin.Peaks.S02E01.1080p.BluRay.x264-GRP", 2),
		rel("Twin.Peaks.S02E02.1080p.BluRay.x264-GRP", 2),
		rel("Twin.Peaks.S02E01E02.1080p.BluRay.x264-GRP", 4),
		rel("Twin.Peaks.S02E04.1080p.BluRay.x264-GRP", 2),
		rel("Twin.Peaks.S02.1080p.BluRay.x264-GRP", 40),
		rel("Fargo.S02E01.1080p.BluRay.x264-GRP", 2),
		rel("Twin.Peaks.S02E30.1080p.BluRay.x264-GRP", 2),
	}, p, ctx)
	got := map[string]bool{}
	for _, d := range ds {
		got[d.Release.Title] = d.Approved
		if !d.Approved {
			t.Logf("%s: %v", d.Release.Title, d.Rejections)
		}
	}
	want := map[string]bool{
		"Twin.Peaks.S02E01.1080p.BluRay.x264-GRP": true, "Twin.Peaks.S02E02.1080p.BluRay.x264-GRP": true,
		"Twin.Peaks.S02E01E02.1080p.BluRay.x264-GRP": true, "Twin.Peaks.S02E04.1080p.BluRay.x264-GRP": false,
		"Twin.Peaks.S02.1080p.BluRay.x264-GRP": false, "Fargo.S02E01.1080p.BluRay.x264-GRP": false,
		"Twin.Peaks.S02E30.1080p.BluRay.x264-GRP": false,
	}
	for k, v := range want {
		if got[k] != v {
			t.Errorf("%s: approved=%v, want %v", k, got[k], v)
		}
	}

	// the whole season selected: the pack is accepted and ranked first
	var all []model.EpisodeKey
	for e := 1; e <= 22; e++ {
		all = append(all, model.EpisodeKey{Season: 2, Episode: e})
	}
	ctx.Wanted = all
	ds = EvaluateEpisodes([]model.Release{rel("Twin.Peaks.S02E01.1080p.BluRay.x264-GRP", 2), rel("Twin.Peaks.S02.1080p.BluRay.x264-GRP", 40)}, p, ctx)
	if !ds[0].Approved || ds[0].Episode == nil || !ds[0].Episode.FullSeason || len(ds[0].Covered) != 22 {
		t.Fatalf("season pack not first: %+v", ds[0])
	}
}

// An untagged series release is in the original language of the series;
// a MULTi one counts as French and original (French profile).
func TestEpisodeLanguages(t *testing.T) {
	s := twinPeaks()
	s.OriginalLanguage = "en"
	ctx := EpisodeContext{Series: s, Wanted: []model.EpisodeKey{{Season: 2, Episode: 1}}}
	for title, want := range map[string]string{
		"Twin.Peaks.S02E01.1080p.BluRay.x264-GRP":       "English, Original",
		"Twin Peaks S02E01 [MULTi] 1080p BluRay x264":   "French, Original",
		"Twin.Peaks.S02E01.FRENCH.1080p.BluRay.x264-GR": "French",
	} {
		d := EvaluateEpisodes([]model.Release{rel(title, 2)}, DefaultProfile(), ctx)[0]
		if got := joinLangs(d.Languages); got != want {
			t.Errorf("%s: %q, want %q", title, got, want)
		}
	}
}
