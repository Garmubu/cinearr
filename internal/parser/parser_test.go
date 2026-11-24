package parser

import (
	"encoding/json"
	"os"
	"strings"
	"testing"
)

// The cases below are extracted automatically from Radarr's own NUnit
// fixtures (NzbDrone.Core.Test/ParserTests) by tools/gen_testdata.py.
type radarrCases struct {
	Quality []struct {
		Method     string
		Title      string
		Source     int
		Resolution int
		Modifier   int
		Proper     bool
	}
	ReleaseGroup []struct{ Method, Title, Expected string }
	Language     []struct {
		Method string
		Title  string
		Langs  []string
		Exact  bool
	}
	MovieTitle []struct{ Title, Expected string }
	Folder     []struct{ Title, Expected string }
	Year       []struct {
		Title string
		Year  int
	}
	AltTitles []struct {
		Title  string
		Titles []string
	}
	HcSubs []struct{ Title, Expected string }
	Imdb   []struct{ Title, Expected string }
	Tmdb   []struct {
		Title    string
		Expected int
	}
	Edition []struct{ Method, Title, Expected string }
	Anime   []struct {
		Title, Expected, Group string
		Year                   int
	}
	German []struct {
		Title, Expected, Edition string
		Year                     int
	}
	WrongLanguage []struct{ Title string }
}

func load(t *testing.T) radarrCases {
	b, err := os.ReadFile("testdata/radarr_cases.json")
	if err != nil {
		t.Fatal(err)
	}
	var c radarrCases
	if err := json.Unmarshal(b, &c); err != nil {
		t.Fatal(err)
	}
	return c
}

type tally struct {
	t          *testing.T
	name       string
	pass, fail int
	failures   []string
}

func (s *tally) check(ok bool, msg string) {
	if ok {
		s.pass++
		return
	}
	s.fail++
	s.failures = append(s.failures, msg)
}

func (s *tally) done() {
	s.t.Logf("%s: %d/%d passed", s.name, s.pass, s.pass+s.fail)
	for _, f := range s.failures {
		s.t.Errorf("%s: %s", s.name, f)
	}
}

func TestRadarrQuality(t *testing.T) {
	c := load(t)
	s := &tally{t: t, name: "quality"}
	for _, q := range c.Quality {
		r := ParseQuality(q.Title)
		ok := int(r.Quality.Source) == q.Source
		if q.Resolution != 0 && r.Quality.Resolution != q.Resolution {
			ok = false
		}
		if q.Modifier != 0 && int(r.Quality.Modifier) != q.Modifier {
			ok = false
		}
		want := 1
		if q.Proper {
			want = 2
		}
		if r.Revision.Version != want {
			ok = false
		}
		s.check(ok, q.Method+" "+q.Title+" => "+r.Quality.Name)
	}
	s.done()
}

func TestRadarrReleaseGroup(t *testing.T) {
	c := load(t)
	s := &tally{t: t, name: "release group"}
	for _, q := range c.ReleaseGroup {
		got := ParseReleaseGroup(q.Title)
		s.check(got == q.Expected, q.Title+" => "+got+" (want "+q.Expected+")")
	}
	s.done()
}

func TestRadarrLanguages(t *testing.T) {
	c := load(t)
	s := &tally{t: t, name: "languages"}
	for _, q := range c.Language {
		r := ParseMovieTitle(q.Title, true)
		if r == nil {
			s.check(false, q.Title+" => unparsed")
			continue
		}
		ok := true
		for _, l := range q.Langs {
			if !contains(r.Languages, l) {
				ok = false
			}
		}
		if q.Exact && len(r.Languages) != len(q.Langs) {
			ok = false
		}
		s.check(ok, q.Method+" "+q.Title+" => "+strings.Join(r.Languages, ","))
	}
	for _, q := range c.WrongLanguage {
		r := ParseMovieTitle(q.Title, true)
		s.check(r != nil && len(r.Languages) == 1 && r.Languages[0] == "Unknown", "wrong-language "+q.Title)
	}
	s.done()
}

func TestRadarrMovieTitles(t *testing.T) {
	c := load(t)
	s := &tally{t: t, name: "titles"}
	for _, q := range c.MovieTitle {
		r := ParseMovieTitle(q.Title, false)
		got := "<nil>"
		if r != nil {
			got = r.PrimaryTitle()
		}
		s.check(got == q.Expected, q.Title+" => "+got+" (want "+q.Expected+")")
	}
	for _, q := range c.Folder {
		r := ParseMovieTitle(q.Title, true)
		s.check(r != nil && r.PrimaryTitle() == q.Expected, "folder "+q.Title)
	}
	for _, q := range c.Year {
		r := ParseMovieTitle(q.Title, false)
		s.check(r != nil && r.Year == q.Year, "year "+q.Title)
	}
	for _, q := range c.AltTitles {
		r := ParseMovieTitle(q.Title, true)
		ok := r != nil && len(r.MovieTitles) == len(q.Titles)
		if ok {
			for i := range q.Titles {
				if r.MovieTitles[i] != q.Titles[i] {
					ok = false
				}
			}
		}
		got := ""
		if r != nil {
			got = strings.Join(r.MovieTitles, " | ")
		}
		s.check(ok, "alt "+q.Title+" => "+got)
	}
	for _, q := range c.HcSubs {
		r := ParseMovieTitle(q.Title, false)
		s.check(r != nil && r.HardcodedSubs == q.Expected, "hcsubs "+q.Title)
	}
	for _, q := range c.Imdb {
		r := ParseMovieTitle(q.Title, true)
		s.check(r != nil && r.ImdbID == q.Expected, "imdb "+q.Title)
	}
	for _, q := range c.Tmdb {
		r := ParseMovieTitle(q.Title, false)
		s.check(r != nil && r.TmdbID == q.Expected, "tmdb "+q.Title)
	}
	for _, q := range c.Edition {
		r := ParseMovieTitle(q.Title, false)
		got := "<nil>"
		if r != nil {
			got = r.Edition
		}
		s.check(got == q.Expected, "edition "+q.Title+" => "+got+" (want "+q.Expected+")")
	}
	for _, q := range c.Anime {
		r := ParseMovieTitle(q.Title, false)
		s.check(r != nil && r.PrimaryTitle() == q.Expected && r.ReleaseGroup == q.Group && r.Year == q.Year, "anime "+q.Title)
	}
	for _, q := range c.German {
		r := ParseMovieTitle(q.Title, false)
		ok := r != nil && r.PrimaryTitle() == q.Expected && r.Edition == q.Edition && r.Year == q.Year
		got := ""
		if r != nil {
			got = r.PrimaryTitle() + " / " + r.Edition
		}
		s.check(ok, "german "+q.Title+" => "+got)
	}
	s.done()
}
