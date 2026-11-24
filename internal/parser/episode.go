package parser

import (
	"regexp"
	"strconv"
	"strings"
)

// ParsedEpisode is what a series release name tells (Sonarr's
// ParsedEpisodeInfo): series title, season(s), episode numbers and whether
// the release is a season or series pack.
type ParsedEpisode struct {
	SeriesTitle    string       `json:"seriesTitle"`
	Year           int          `json:"year,omitempty"`
	Season         int          `json:"season"`
	Seasons        []int        `json:"seasons,omitempty"` // multi-season packs
	Episodes       []int        `json:"episodes,omitempty"`
	FullSeason     bool         `json:"fullSeason"`
	MultiSeason    bool         `json:"multiSeason"`
	CompleteSeries bool         `json:"completeSeries"`
	Special        bool         `json:"special"`
	Quality        QualityModel `json:"quality"`
	Languages      []string     `json:"languages"`
	ReleaseGroup   string       `json:"releaseGroup,omitempty"`
	HardcodedSubs  string       `json:"hardcodedSubs,omitempty"`
}

// The expressions below cover the naming schemes Sonarr's ReportTitleRegex
// handles for standard series (S01E05, 1x05, S01E01E02, S01E01-03, season
// packs, multi-season packs, "Season 2 Episode 5" in the usual languages).
// Daily and anime absolute numbering are not supported.
var (
	seasonWord = `(?:S|Seasons?|Saison|Staffel|Stagione|Temporada|Seizoen|Sezon)`
	episodeRe  = regexp.MustCompile(`(?i)(?:^|[^a-z0-9])` + seasonWord + `[ ._-]?(\d{1,2})[ ._-]?(?:E|Ep|Episode|[ÉE]pisode|Episodio|Folge|Afl)[ ._-]?(\d{1,3})(?:[^0-9]|$)`)
	// extra episodes after the first one: E02, -E03, -03
	moreEpisodeRe = regexp.MustCompile(`(?i)^(?:([ ._]?-[ ._]?)(?:E|Ep)?(\d{1,3})|[ ._]?(?:E|Ep)(\d{1,3}))(?:[^0-9]|$)`)
	crossRe       = regexp.MustCompile(`(?i)(?:^|[^a-z0-9])(\d{1,2})x(\d{2,3})(?:[-x](?:\d{1,2}x)?(\d{2,3}))?(?:[^0-9a-z]|$)`)
	multiSeasonRe = regexp.MustCompile(`(?i)(?:^|[^a-z0-9])` + seasonWord + `[ ._-]?(\d{1,2})[ ._]?(?:-|to|à|a|bis|al)[ ._]?` + seasonWord + `?[ ._-]?(\d{1,2})(?:[^0-9]|$)`)
	seasonRe      = regexp.MustCompile(`(?i)(?:^|[^a-z0-9])` + seasonWord + `[ ._-]?(\d{1,2})(?:[^0-9]|$)`)
	completeRe    = regexp.MustCompile(`(?i)(?:^|[^a-z0-9])(?:the[ ._-])?(?:complete(?:[ ._-]series)?|int[eé]grale|l[' ._]?int[eé]grale)(?:[^a-z0-9]|$)`)
	leadingTagRe  = regexp.MustCompile(`^(?:\[[^\]]*\][ ._-]*)+`)
	titleYearRe   = regexp.MustCompile(`^(.+?)[ ._(\[-]+((?:19|20)\d{2})[)\]]?$`)
	titleCountry  = regexp.MustCompile(`(?i)[ ._(]+(US|UK|AU|NZ|CA)\)?$`)
	sepRe         = regexp.MustCompile(`[ ._]+`)
	notEpisodeRe  = regexp.MustCompile(`(?i)^[ ._-]*(?:E|Ep|Episode)[ ._-]?\d`)
)

// ParseEpisodeTitle parses a series release or file name. It returns nil
// when no season or episode marker is found.
func ParseEpisodeTitle(title string) *ParsedEpisode {
	raw := strings.TrimSpace(title)
	if raw == "" {
		return nil
	}
	name := RemoveFileExtension(raw)
	name = websitePrefixRegex.ReplaceAllString(name, "")
	name = cleanTorrentSuffixRegex.ReplaceAllString(name, "")
	name = leadingTagRe.ReplaceAllString(name, "")

	p := &ParsedEpisode{}
	var cut int // where the series title ends

	if loc := episodeRe.FindStringSubmatchIndex(name); loc != nil {
		season, _ := strconv.Atoi(name[loc[2]:loc[3]])
		first, _ := strconv.Atoi(name[loc[4]:loc[5]])
		p.Season = season
		p.Episodes = []int{first}
		rest := name[loc[5]:]
		last := first
		for {
			m := moreEpisodeRe.FindStringSubmatchIndex(rest)
			if m == nil {
				break
			}
			var n, end int
			if m[4] >= 0 { // "-03" or "-E03": a range
				n, _ = strconv.Atoi(rest[m[4]:m[5]])
				end = m[5]
				if n <= last || n-last > 50 {
					break
				}
				for e := last + 1; e <= n; e++ {
					p.Episodes = append(p.Episodes, e)
				}
			} else { // "E02": a list
				n, _ = strconv.Atoi(rest[m[6]:m[7]])
				end = m[7]
				if n <= last {
					break
				}
				p.Episodes = append(p.Episodes, n)
			}
			last = n
			rest = rest[end:]
		}
		cut = loc[0]
	} else if loc := crossRe.FindStringSubmatchIndex(name); loc != nil {
		season, _ := strconv.Atoi(name[loc[2]:loc[3]])
		first, _ := strconv.Atoi(name[loc[4]:loc[5]])
		p.Season = season
		p.Episodes = []int{first}
		if loc[6] >= 0 {
			last, _ := strconv.Atoi(name[loc[6]:loc[7]])
			for e := first + 1; e <= last && e-first < 50; e++ {
				p.Episodes = append(p.Episodes, e)
			}
		}
		cut = loc[0]
	} else if loc := multiSeasonRe.FindStringSubmatchIndex(name); loc != nil {
		a, _ := strconv.Atoi(name[loc[2]:loc[3]])
		b, _ := strconv.Atoi(name[loc[4]:loc[5]])
		if b < a || b-a > 60 {
			return nil
		}
		for s := a; s <= b; s++ {
			p.Seasons = append(p.Seasons, s)
		}
		p.Season, p.FullSeason, p.MultiSeason = a, true, true
		cut = loc[0]
	} else if loc := seasonPack(name); loc != nil {
		p.Season, _ = strconv.Atoi(name[loc[2]:loc[3]])
		p.FullSeason = true
		cut = loc[0]
	} else if loc := completeRe.FindStringIndex(name); loc != nil && loc[0] > 0 {
		p.CompleteSeries, p.FullSeason, p.MultiSeason = true, true, true
		cut = loc[0]
	} else {
		return nil
	}
	if p.Season > 100 {
		return nil
	}
	for _, e := range p.Episodes {
		if e > 2000 {
			return nil
		}
	}
	p.Special = p.Season == 0 && !p.CompleteSeries

	t := strings.Trim(sepRe.ReplaceAllString(name[:cut], " "), " -–([")
	if m := titleYearRe.FindStringSubmatch(t); m != nil && strings.TrimSpace(m[1]) != "" {
		t = strings.TrimSpace(m[1])
		p.Year, _ = strconv.Atoi(m[2])
	}
	t = strings.TrimSpace(strings.TrimRight(t, " -–"))
	if t == "" {
		return nil
	}
	p.SeriesTitle = t
	p.Quality = ParseQuality(raw)
	p.Languages = ParseLanguages(raw)
	p.ReleaseGroup = ParseReleaseGroup(raw)
	p.HardcodedSubs = ParseHardcodeSubs(raw)
	return p
}

// seasonPack finds "S02" / "Season 2" that is not followed by an episode.
func seasonPack(name string) []int {
	off := 0
	for off < len(name) {
		loc := seasonRe.FindStringSubmatchIndex(name[off:])
		if loc == nil {
			return nil
		}
		for i := range loc {
			if loc[i] >= 0 {
				loc[i] += off
			}
		}
		if !notEpisodeRe.MatchString(name[loc[3]:]) {
			return loc
		}
		off = loc[3]
	}
	return nil
}

// CleanSeriesTitle is the comparison key for series titles: CleanMovieTitle
// without a trailing year or country code ("The Office (US)").
func CleanSeriesTitle(title string) string {
	t := strings.TrimSpace(title)
	if m := titleYearRe.FindStringSubmatch(t); m != nil && strings.TrimSpace(m[1]) != "" {
		t = m[1]
	}
	if m := titleCountry.FindStringIndex(t); m != nil && m[0] > 0 {
		t = t[:m[0]]
	}
	return CleanMovieTitle(t)
}

// Covers reports whether the release contains the given episode.
func (p *ParsedEpisode) Covers(season, episode int) bool {
	switch {
	case p.CompleteSeries:
		return season > 0
	case p.MultiSeason:
		for _, s := range p.Seasons {
			if s == season {
				return true
			}
		}
		return false
	case p.FullSeason:
		return season == p.Season
	}
	if season != p.Season {
		return false
	}
	for _, e := range p.Episodes {
		if e == episode {
			return true
		}
	}
	return false
}

// Label is a short description: S02E01, S02E01-E03, S02, S01-S03.
func (p *ParsedEpisode) Label() string {
	switch {
	case p.CompleteSeries:
		return "Complete"
	case p.MultiSeason && len(p.Seasons) > 0:
		return "S" + pad2(p.Seasons[0]) + "-S" + pad2(p.Seasons[len(p.Seasons)-1])
	case p.FullSeason:
		return "S" + pad2(p.Season)
	}
	s := "S" + pad2(p.Season)
	if len(p.Episodes) > 0 {
		s += "E" + pad2(p.Episodes[0])
		if len(p.Episodes) > 1 {
			s += "-E" + pad2(p.Episodes[len(p.Episodes)-1])
		}
	}
	return s
}

func pad2(n int) string {
	if n < 10 {
		return "0" + strconv.Itoa(n)
	}
	return strconv.Itoa(n)
}
