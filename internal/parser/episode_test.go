package parser

import (
	"reflect"
	"testing"
)

func TestParseEpisodeTitle(t *testing.T) {
	cases := []struct {
		in       string
		title    string
		year     int
		season   int
		episodes []int
		full     bool
		multi    []int
	}{
		{"Twin.Peaks.S02E01.1080p.BluRay.x264-SHORTBREHD", "Twin Peaks", 0, 2, []int{1}, false, nil},
		{"Twin Peaks - S02E03 - Episode 10 [Bluray-1080p].mkv", "Twin Peaks", 0, 2, []int{3}, false, nil},
		{"twin.peaks.s02e01e02.720p.bluray.x264-demand", "twin peaks", 0, 2, []int{1, 2}, false, nil},
		{"Twin.Peaks.S02E01-E03.MULTi.1080p.WEB.H264-FW", "Twin Peaks", 0, 2, []int{1, 2, 3}, false, nil},
		{"Twin.Peaks.S02E01-03.FRENCH.720p.HDTV", "Twin Peaks", 0, 2, []int{1, 2, 3}, false, nil},
		{"Twin.Peaks.S02.1080p.BluRay.x264-SHORTBREHD", "Twin Peaks", 0, 2, nil, true, nil},
		{"Twin Peaks Season 2 Complete 720p", "Twin Peaks", 0, 2, nil, true, nil},
		{"Twin.Peaks.Saison.2.FRENCH.DVDRip.XviD", "Twin Peaks", 0, 2, nil, true, nil},
		{"Twin.Peaks.S01-S03.COMPLETE.1080p.BluRay", "Twin Peaks", 0, 1, nil, true, []int{1, 2, 3}},
		{"Twin.Peaks.2017.S01E05.1080p.WEB-DL.DD5.1.H264", "Twin Peaks", 2017, 1, []int{5}, false, nil},
		{"The.Office.US.S05E14.720p.HDTV.x264", "The Office US", 0, 5, []int{14}, false, nil},
		{"Breaking Bad 1x05 Gray Matter", "Breaking Bad", 0, 1, []int{5}, false, nil},
		{"Dark.S03E08.German.DL.1080p.WEB.x264", "Dark", 0, 3, []int{8}, false, nil},
		{"[ www.Torrenting.com ] - Fargo.S04E02.720p.HDTV", "Fargo", 0, 4, []int{2}, false, nil},
		{"Doctor.Who.2005.S00E01.Christmas.Special.720p", "Doctor Who", 2005, 0, []int{1}, false, nil},
		{"1883.S01E01.1080p.WEB.H264", "1883", 0, 1, []int{1}, false, nil},
		{"Lupin.S01.Episode.3.FRENCH.1080p", "Lupin", 0, 1, []int{3}, false, nil},
		{"Game of Thrones S08E06 1080p 10bit", "Game of Thrones", 0, 8, []int{6}, false, nil},
	}
	for _, c := range cases {
		p := ParseEpisodeTitle(c.in)
		if p == nil {
			t.Errorf("%q: not parsed", c.in)
			continue
		}
		if p.SeriesTitle != c.title || p.Year != c.year || p.Season != c.season || !reflect.DeepEqual(p.Episodes, c.episodes) || p.FullSeason != c.full || !reflect.DeepEqual(p.Seasons, c.multi) {
			t.Errorf("%q: got title=%q year=%d season=%d episodes=%v full=%v seasons=%v", c.in, p.SeriesTitle, p.Year, p.Season, p.Episodes, p.FullSeason, p.Seasons)
		}
	}
	for _, in := range []string{"Inception.2010.1080p.BluRay.x264-SPARKS", "Spider-Man.No.Way.Home.2021.MULTi.2160p", "Dune Part Two 2024 720p"} {
		if p := ParseEpisodeTitle(in); p != nil {
			t.Errorf("%q: parsed as an episode (%+v)", in, p)
		}
	}
	if p := ParseEpisodeTitle("Twin.Peaks.Integrale.MULTi.1080p"); p == nil || !p.CompleteSeries || !p.Covers(3, 4) {
		t.Errorf("complete series not recognised: %+v", p)
	}
}

func TestCleanSeriesTitle(t *testing.T) {
	if CleanSeriesTitle("The Office (US)") != CleanSeriesTitle("The Office") {
		t.Error("country code not ignored")
	}
	if CleanSeriesTitle("Twin Peaks (2017)") != CleanSeriesTitle("Twin.Peaks") {
		t.Error("year not ignored")
	}
}
