package decision

import (
	"strings"
	"time"

	"cinearr/internal/model"
	"cinearr/internal/parser"
)

// EpisodeContext carries what the engine needs for a series search.
type EpisodeContext struct {
	Series model.Series
	// Wanted are the episodes the user asked for.
	Wanted    []model.EpisodeKey
	Blocklist []model.BlocklistEntry
	FreeSpace int64
	Now       time.Time
}

// EvaluateEpisodes checks series releases (Sonarr's DownloadDecisionMaker):
// the release must be the series, contain at least one wanted episode and
// no missing episode that was not asked for (a season pack is accepted when
// every missing episode of the season is selected).
func EvaluateEpisodes(releases []model.Release, p Profile, ctx EpisodeContext) []Decision {
	if ctx.Now.IsZero() {
		ctx.Now = time.Now()
	}
	out := make([]Decision, 0, len(releases))
	for _, r := range releases {
		out = append(out, evaluateEpisode(r, p, ctx))
	}
	Sort(out)
	return out
}

func evaluateEpisode(r model.Release, p Profile, ctx EpisodeContext) Decision {
	d := Decision{Release: r}
	pe := parser.ParseEpisodeTitle(r.Title)
	if pe == nil {
		d.reject("Not an episode or season release")
		d.Quality = parser.ParseQuality(r.Title).Quality
		return d
	}
	d.Episode = pe
	d.Label = pe.Label()
	d.Quality = pe.Quality.Quality
	d.Languages = pe.Languages
	d.rev = pe.Quality.Revision.Version*10 + pe.Quality.Revision.Real
	se := ctx.Series

	d.TitleMatched = seriesTitleMatches(pe, r, se)
	if !d.TitleMatched {
		d.reject("The title does not match the series (%s)", pe.SeriesTitle)
	}

	// episodes of the series contained in the release
	wanted := map[model.EpisodeKey]bool{}
	for _, k := range ctx.Wanted {
		wanted[k] = true
	}
	var covered, extra []model.EpisodeKey
	var files []*model.MovieFile
	known := 0
	runtime := 0
	for _, s := range se.Seasons {
		if s.Number == 0 && !pe.Special {
			continue // specials are never part of season packs
		}
		for _, e := range s.Episodes {
			if !pe.Covers(s.Number, e.Number) {
				continue
			}
			k := model.EpisodeKey{Season: s.Number, Episode: e.Number}
			if !e.Aired(ctx.Now) && !wanted[k] {
				continue
			}
			known++
			rt := e.Runtime
			if rt <= 0 {
				rt = se.Runtime
			}
			if rt <= 0 {
				rt = 45
			}
			runtime += rt
			switch {
			case wanted[k]:
				covered = append(covered, k)
				files = append(files, e.File)
			case e.File == nil:
				extra = append(extra, k)
			}
		}
	}
	d.Covered = covered
	switch {
	case pe.FullSeason:
		d.PackRank = 1000
	default:
		d.PackRank = 100 - len(pe.Episodes)
	}
	if !pe.FullSeason && known < len(pe.Episodes) {
		d.reject("Unknown episode for this series (%s)", pe.Label())
	}
	switch {
	case known == 0:
		d.reject("No episode of the series in this release (%s)", pe.Label())
	case len(covered) == 0:
		d.reject("None of the selected episodes (%s)", pe.Label())
	case len(extra) > 0:
		if pe.FullSeason {
			d.reject("Season pack: also contains %d missing episodes that are not selected", len(extra))
		} else {
			d.reject("Also contains episodes that are not selected (%s)", pe.Label())
		}
	}

	// upgrades: only when every selected episode of the release has a file
	var worst *model.MovieFile
	for _, f := range files {
		if f == nil {
			worst = nil
			break
		}
		if worst == nil || indexOf(p.Allowed, f.QualityID) < indexOf(p.Allowed, worst.QualityID) {
			worst = f
		}
	}
	d.checkUpgrade(p, worst, "The episode is already downloaded (upgrades disabled)")

	for _, b := range ctx.Blocklist {
		if b.SeriesID != se.TmdbID && (b.SeriesID != 0 || b.TmdbID != 0) {
			continue
		}
		if (b.Hash != "" && strings.EqualFold(b.Hash, r.InfoHash)) || (b.Title == r.Title && (b.Indexer == "" || b.Indexer == r.Indexer)) {
			d.reject("Release on the blocklist: %s", b.Reason)
			break
		}
	}

	if runtime <= 0 {
		runtime = 45
	}
	d.checkRelease(r, p, pe.Languages, pe.HardcodedSubs, runtime, ctx.FreeSpace)
	d.Approved = len(d.Rejections) == 0
	return d
}

func seriesTitleMatches(pe *parser.ParsedEpisode, r model.Release, se model.Series) bool {
	if se.ImdbID != "" && r.ImdbID != "" && strings.EqualFold(parser.NormalizeImdbID(r.ImdbID), se.ImdbID) {
		return true
	}
	if pe.Year != 0 && se.Year != 0 && abs(pe.Year-se.Year) > 1 {
		// "Title 2017" can be a remake: only an exact "title year" matches
		key := parser.CleanMovieTitle(pe.SeriesTitle + " " + itoa(pe.Year))
		for _, c := range append([]string{se.Title, se.OriginalTitle, se.EnglishTitle}, se.AlternativeTitles...) {
			if c != "" && parser.CleanMovieTitle(c) == key {
				return true
			}
		}
		return false
	}
	key := parser.CleanSeriesTitle(pe.SeriesTitle)
	for _, c := range append([]string{se.Title, se.OriginalTitle, se.EnglishTitle}, se.AlternativeTitles...) {
		if c != "" && parser.CleanSeriesTitle(c) == key {
			return true
		}
	}
	return false
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var b []byte
	for n > 0 {
		b = append([]byte{byte('0' + n%10)}, b...)
		n /= 10
	}
	return string(b)
}
