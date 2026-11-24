// Package parser is a port of Radarr's release-title parser
// (NzbDrone.Core.Parser): movie title and year, quality, languages, release
// group, edition, hardcoded subtitles and IMDb/TMDb ids.
package parser

import (
	"strconv"
	"strings"
	"unicode"

	"cinearr/internal/quality"
	"cinearr/internal/rx"
)

// ParsedMovie mirrors ParsedMovieInfo.
type ParsedMovie struct {
	MovieTitles        []string     `json:"movieTitles"`
	Year               int          `json:"year"`
	Edition            string       `json:"edition,omitempty"`
	Quality            QualityModel `json:"quality"`
	Languages          []string     `json:"languages"`
	ReleaseGroup       string       `json:"releaseGroup,omitempty"`
	ReleaseHash        string       `json:"releaseHash,omitempty"`
	HardcodedSubs      string       `json:"hardcodedSubs,omitempty"`
	ImdbID             string       `json:"imdbId,omitempty"`
	TmdbID             int          `json:"tmdbId,omitempty"`
	OriginalTitle      string       `json:"originalTitle"`
	ReleaseTitle       string       `json:"releaseTitle"`
	SimpleReleaseTitle string       `json:"simpleReleaseTitle"`
}

// PrimaryTitle returns the first parsed title.
func (p *ParsedMovie) PrimaryTitle() string {
	if len(p.MovieTitles) == 0 {
		return ""
	}
	return p.MovieTitles[0]
}

const editionPattern = `\(?\b(?<edition>(((Recut.|Extended.|Ultimate.)?(Director.?s|Collector.?s|Theatrical|Ultimate|Extended|Despecialized|(Special|Rouge|Final|Assembly|Imperial|Diamond|Signature|Hunter|Rekall)(?=(.(Cut|Edition|Version)))|\d{2,3}(th)?.Anniversary)(?:.(Cut|Edition|Version))?(.(Extended|Uncensored|Remastered|Unrated|Uncut|Open.?Matte|IMAX|Fan.?Edit))?|((Uncensored|Remastered|Unrated|Uncut|Open?.Matte|IMAX|Fan.?Edit|Restored|((2|3|4)in1))))))\b\)?`

var (
	reportEditionRegex = rx.MustCompile(`^.+?`+editionPattern, ic)
	hardcodedSubsRegex = rx.MustCompile(`\b((?<hcsub>(\w+(?<!SOFT|MULTI|HORRIBLE)SUBS?))|(?<hc>(HC|SUBBED)))\b`, icx)

	reportMovieTitleRegex = []*rx.Regexp{
		rx.MustCompile(`^(?:\[(?<subgroup>.+?)\][-_. ]?)(?<title>(?![(\[]).+?)\s*\((?<year>(1(8|9)|20)\d{2})\).*?(?<hash>\[\w{8}\])?(?:$|\.)`, ic),
		rx.MustCompile(`^(?:\[(?<subgroup>.+?)\][-_. ]?)(?<title>(?![(\[]).+?)?(?:(?:[-_\W](?<![)\[!]))*(?<year>(1(8|9)|20)\d{2}(?!p|i|x|\d+|\]|\W\d+)))+.*?(?<hash>\[\w{8}\])?(?:$|\.)`, ic),
		rx.MustCompile(`^(?:\[(?<subgroup>.+?)\][-_. ]?)(?<title>(?![(\[]).+?)((v)(?:\d{1,2})(?:([-_. ])))(\[.*)?(?:[\[(][^])])?.*?(?<hash>\[\w{8}\])(?:$|\.)`, ic),
		rx.MustCompile(`^(?:\[(?<subgroup>.+?)\][-_. ]?)(?<title>(?![(\[]).+?)(\[.*).*?(?<hash>\[\w{8}\])(?:$|\.)`, ic),
		rx.MustCompile(`^(?:\[(?<subgroup>.+?)\][-_. ]?)(?<title>(?![(\[]).+)(?:[\[(][^])]).*?(?<hash>\[\w{8}\])(?:$|\.)`, ic),
		rx.MustCompile(`^(?<title>(?![(\[]).+?)((\W|_))(`+editionPattern+`.{1,3})?(?:(?<!(19|20)\d{2}.*?)(?<!(?:Good|The)[_ .-])(German|TrueFrench))(.+?)(?=((19|20)\d{2}|$))(?<year>(19|20)\d{2}(?!p|i|\d+|\]|\W\d+))?(\W+|_|$)(?!\\)`, ic),
		rx.MustCompile(`^(?<title>(?![(\[]).+?)?(?:(?:[-_\W](?<![)\[!]))*`+editionPattern+`.{1,3}(?<year>(1(8|9)|20)\d{2}(?!p|i|\d+|\]|\W\d+)))+(\W+|_|$)(?!\\)`, ic),
		rx.MustCompile(`^(?<title>(?![(\[]).+?)?(?:(?:[-_\W](?<![)\[!]))*(?<year>(1(8|9)|20)\d{2}(?!p|i|(1(8|9)|20)\d{2}|\]|\W(1(8|9)|20)\d{2})))+(\W+|_|$)(?!\\)`, ic),
		rx.MustCompile(`^(?<title>.+?)?(?:(?:[-_\W](?<![()\[!]))*(?<year>(\[\w *\])))+(\W+|_|$)(?!\\)`, ic),
		rx.MustCompile(`^(?<title>(?![(\[]).+?)?(?:(?:[-_\W](?<![)!]))*(?<year>(1(8|9)|20)\d{2}(?!p|i|\d+|\W\d+)))+(\W+|_|$)(?!\\)`, ic),
		rx.MustCompile(`^(?<title>.+?)?(?:(?:[-_\W](?<![)\[!]))*(?<year>(1(8|9)|20)\d{2}(?!p|i|\d+|\]|\W\d+)))+(\W+|_|$)(?!\\)`, ic),
	}

	reportMovieTitleFolderRegex = []*rx.Regexp{
		rx.MustCompile(`^(?:(?:[-_\W](?<![)!]))*(?<year>(19|20)\d{2}(?!p|i|\d+|\W\d+)))+(\W+|_|$)(?<title>.+?)?$`, 0),
	}

	rejectHashedReleasesRegex = []*rx.Regexp{
		rx.MustCompile(`^[0-9a-zA-Z]{32}`, 0),
		rx.MustCompile(`^[a-z0-9]{24}$`, 0),
		rx.MustCompile(`^[A-Z]{11}\d{3}$`, 0),
		rx.MustCompile(`^[a-z]{12}\d{3}$`, 0),
		rx.MustCompile(`^Backup_\d{5,}S\d{2}-\d{2}$`, 0),
		rx.MustCompile(`^123$`, 0),
		rx.MustCompile(`^abc$`, ic),
		rx.MustCompile(`^abc[-_. ]xyz`, ic),
		rx.MustCompile(`^b00bs$`, ic),
	}

	reversedTitleRegex             = rx.MustCompile(`(?:^|[-._ ])(p027|p0801)[-._ ]`, 0)
	alternativeTitleRegex          = rx.MustCompile(`[ ]+(?:AKA|\/)[ ]+`, ic)
	bracketedAlternativeTitleRegex = rx.MustCompile(`(.*) \([ ]*AKA[ ]+(.*)\)`, ic)
	normalizeAlternativeTitleRegex = rx.MustCompile(`[ ]+(?:A\.K\.A\.)[ ]+`, ic)
	normalizeRegex                 = rx.MustCompile(`((?:\b|_)(?<!^|[^a-zA-Z0-9_']\w[^a-zA-Z0-9_'])([aà](?!$|[^a-zA-Z0-9_']\w[^a-zA-Z0-9_'])|an|the|and|or|of)(?!$)(?:\b|_))|\W|_`, ic)
	reportImdbID                   = rx.MustCompile(`(?<imdbid>tt\d{7,8})`, ic)
	reportTmdbID                   = rx.MustCompile(`tmdb(id)?-(?<tmdbid>\d+)`, ic)
	simpleTitleRegex               = rx.MustCompile(`(?:(480|540|576|720|1080|2160)[ip]|[xh][\W_]?26[45]|DD\W?5\W1|[<>?*]|848x480|1280x720|1920x1080|3840x2160|4096x2160|(8|10)b(it)?|10-bit)\s*?(?![a-b0-9])`, ic)
	simpleReleaseTitleRegex        = rx.MustCompile(`\s*(?:[<>?*|])`, ic)
	cleanQualityBracketsRegex      = rx.MustCompile(`\[[a-z0-9 ._-]+\]$`, ic)
	requestInfoRegex               = rx.MustCompile(`^(?:\[.+?\])+`, 0)
	multiRegex                     = rx.MustCompile(`[_. ](?<multi>multi)[_. ]`, ic)
	fileExtensionRegex             = rx.MustCompile(`\.[a-z0-9]{2,4}$`, ic)
	websitePrefixRegex             = rx.MustCompile(`^(?:(?:\[|\()\s*)?(?:www\.)?[-a-z0-9-]{1,256}\.(?<!Naruto-Kun\.)(?:[a-z]{2,6}\.[a-z]{2,6}|xn--[a-z0-9-]{4,}|[a-z]{2,})\b(?:\s*(?:\]|\))|[ -]{2,})[ -]*`, ic)
	websitePostfixRegex            = rx.MustCompile(`(?:\[\s*)?(?:www\.)?[-a-z0-9-]{1,256}\.(?:xn--[a-z0-9-]{4,}|[a-z]{2,6})\b(?:\s*\])$`, ic)
	cleanTorrentSuffixRegex        = rx.MustCompile(`\[(?:ettv|rartv|rarbg|cttv|publichd)\]$`, ic)
)

// RemoveFileExtension strips a known media/usenet extension.
func RemoveFileExtension(title string) string {
	return fileExtensionRegex.ReplaceAllStringFunc(title, func(m *rx.Match) string {
		ext := strings.ToLower(m.String())
		if quality.IsMediaExtension(ext) || ext == ".par2" || ext == ".nzb" {
			return ""
		}
		return m.String()
	})
}

func runeRemoveInsert(s string, index, length int, insert string) string {
	r := []rune(s)
	if index < 0 || index > len(r) {
		return s
	}
	end := index + length
	if end > len(r) {
		end = len(r)
	}
	return string(r[:index]) + insert + string(r[end:])
}

// ParseMovieTitle ports Parser.ParseMovieTitle. It returns nil when the title
// cannot be parsed.
func ParseMovieTitle(title string, isDir bool) *ParsedMovie {
	originalTitle := title
	if !validateBeforeParsing(title) {
		return nil
	}
	if reversedTitleRegex.MatchString(title) {
		noExt := []rune(RemoveFileExtension(title))
		for i, j := 0, len(noExt)-1; i < j; i, j = i+1, j-1 {
			noExt[i], noExt[j] = noExt[j], noExt[i]
		}
		title = string(noExt) + string([]rune(title)[len(noExt):])
	}

	releaseTitle := RemoveFileExtension(title)
	releaseTitle = strings.Trim(releaseTitle, "-_")
	releaseTitle = strings.NewReplacer("【", "[", "】", "]").Replace(releaseTitle)

	simpleTitle := simpleTitleRegex.ReplaceAllString(releaseTitle, "")
	simpleTitle = websitePrefixRegex.ReplaceAllString(simpleTitle, "")
	simpleTitle = websitePostfixRegex.ReplaceAllString(simpleTitle, "")
	simpleTitle = cleanTorrentSuffixRegex.ReplaceAllString(simpleTitle, "")
	simpleTitle = cleanQualityBracketsRegex.ReplaceAllStringFunc(simpleTitle, func(m *rx.Match) string {
		if ParseQualityName(m.String()).Quality.ID != quality.Unknown.ID {
			return ""
		}
		return m.String()
	})

	regexes := reportMovieTitleRegex
	if isDir {
		regexes = append(append([]*rx.Regexp{}, reportMovieTitleRegex...), reportMovieTitleFolderRegex...)
	}

	for _, re := range regexes {
		m := re.FindStringMatch(simpleTitle)
		if m == nil {
			continue
		}
		result := parseMovieMatch(m)
		if result == nil {
			continue
		}
		simpleReleaseTitle := simpleReleaseTitleRegex.ReplaceAllString(releaseTitle, "")
		tg := m.Group("title")
		replace := result.PrimaryTitle()
		if tg.Success {
			replace = tg.Value
		}
		if strings.TrimSpace(replace) != "" {
			placeholder := "A Movie"
			if strings.Contains(replace, ".") {
				placeholder = "A.Movie"
			}
			if tg.Success {
				simpleReleaseTitle = runeRemoveInsert(simpleReleaseTitle, tg.Index, tg.Length, placeholder)
			} else {
				simpleReleaseTitle = strings.ReplaceAll(simpleReleaseTitle, replace, placeholder)
			}
		}

		result.ReleaseGroup = ParseReleaseGroup(simpleReleaseTitle)
		if sg := m.Group("subgroup"); sg.Success && strings.TrimSpace(sg.Value) != "" {
			result.ReleaseGroup = sg.Value
		}
		result.HardcodedSubs = ParseHardcodeSubs(title)

		langTitle := simpleReleaseTitle
		if strings.TrimSpace(result.ReleaseGroup) != "" {
			langTitle = strings.ReplaceAll(simpleReleaseTitle, result.ReleaseGroup, "RlsGrp")
		}
		result.Languages = ParseLanguages(langTitle)
		result.Quality = ParseQuality(title)
		if strings.TrimSpace(result.Edition) == "" {
			result.Edition = ParseEdition(simpleReleaseTitle)
		}
		if h := m.Group("hash"); h.Success {
			v := strings.Trim(h.Value, "[]")
			if v != "1280x720" {
				result.ReleaseHash = v
			}
		}
		result.OriginalTitle = originalTitle
		result.ReleaseTitle = releaseTitle
		result.SimpleReleaseTitle = simpleReleaseTitle
		result.ImdbID = ParseImdbID(simpleReleaseTitle)
		result.TmdbID = ParseTmdbID(simpleReleaseTitle)
		return result
	}
	return nil
}

func parseMovieMatch(m *rx.Match) *ParsedMovie {
	tg := m.Group("title")
	if !tg.Success || tg.Value == "(" {
		return nil
	}
	movieName := strings.ReplaceAll(tg.Value, "_", " ")
	movieName = normalizeAlternativeTitleRegex.ReplaceAllString(movieName, " AKA ")
	movieName = strings.Trim(requestInfoRegex.ReplaceAllString(movieName, ""), " ")

	parts := strings.Split(movieName, ".")
	var b strings.Builder
	previousAcronym := false
	isInt := func(s string) bool { _, err := strconv.Atoi(s); return err == nil }
	for n, part := range parts {
		nextPart := ""
		if len(parts) >= n+2 {
			nextPart = parts[n+1]
		}
		plen := len([]rune(part))
		lower := strings.ToLower(part)
		switch {
		case plen == 1 && lower != "a" && !isInt(part) &&
			(previousAcronym || n < len(parts)-1) &&
			(previousAcronym || len([]rune(nextPart)) != 1 || !isInt(nextPart)):
			b.WriteString(part + ".")
			previousAcronym = true
		case lower == "a" && (previousAcronym || len([]rune(nextPart)) == 1):
			b.WriteString(part + ".")
			previousAcronym = true
		case lower == "dr":
			b.WriteString(part + ".")
			previousAcronym = true
		default:
			if previousAcronym {
				b.WriteString(" ")
				previousAcronym = false
			}
			b.WriteString(part + " ")
		}
	}
	movieName = strings.Trim(b.String(), " ")

	year, _ := strconv.Atoi(m.Value("year"))
	result := &ParsedMovie{Year: year}
	if eg := m.Group("edition"); eg.Success {
		result.Edition = strings.ReplaceAll(eg.Value, ".", " ")
	}
	titles := []string{movieName}
	unbracketed := bracketedAlternativeTitleRegex.ReplaceAllString(movieName, "$1 AKA $2")
	for _, alt := range alternativeTitleRegex.Split(unbracketed) {
		if strings.TrimSpace(alt) != "" && alt != movieName {
			dup := false
			for _, t := range titles {
				if t == alt {
					dup = true
				}
			}
			if !dup {
				titles = append(titles, alt)
			}
		}
	}
	result.MovieTitles = titles
	return result
}

func validateBeforeParsing(title string) bool {
	lower := strings.ToLower(title)
	if strings.Contains(lower, "password") && strings.Contains(lower, "yenc") {
		return false
	}
	hasAlnum := false
	for _, r := range title {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			hasAlnum = true
			break
		}
	}
	if !hasAlnum {
		return false
	}
	noExt := RemoveFileExtension(title)
	for _, re := range rejectHashedReleasesRegex {
		if re.MatchString(noExt) {
			return false
		}
	}
	return true
}

// ParseImdbID extracts an IMDb id (tt1234567).
func ParseImdbID(title string) string {
	if m := reportImdbID.FindStringMatch(title); m != nil {
		v := m.Value("imdbid")
		if l := len(v); l == 9 || l == 10 {
			return v
		}
	}
	return ""
}

// ParseTmdbID extracts "tmdbid-123".
func ParseTmdbID(title string) int {
	if m := reportTmdbID.FindStringMatch(title); m != nil {
		v, _ := strconv.Atoi(m.Value("tmdbid"))
		return v
	}
	return 0
}

// ParseEdition extracts an edition (Director's Cut, Extended, ...).
func ParseEdition(title string) string {
	if m := reportEditionRegex.FindStringMatch(title); m != nil {
		if v := m.Value("edition"); strings.TrimSpace(v) != "" {
			return strings.ReplaceAll(v, ".", " ")
		}
	}
	return ""
}

// ParseHardcodeSubs detects hardcoded subtitles.
func ParseHardcodeSubs(title string) string {
	m := hardcodedSubsRegex.LastMatch(title)
	if m == nil {
		return ""
	}
	if g := m.Group("hcsub"); g.Success {
		return g.Value
	}
	if m.Success("hc") {
		return "Generic Hardcoded Subs"
	}
	return ""
}

// HasMultipleLanguages detects the MULTI tag.
func HasMultipleLanguages(title string) bool { return multiRegex.MatchString(title) }

// NormalizeImdbID returns tt + zero padded id, or "".
func NormalizeImdbID(id string) string {
	id = strings.TrimSpace(id)
	if id == "" {
		return ""
	}
	digits := strings.TrimPrefix(id, "tt")
	if _, err := strconv.Atoi(digits); err != nil || len(digits) > 10 {
		return ""
	}
	for len(digits) < 7 {
		digits = "0" + digits
	}
	return "tt" + digits
}

// CleanMovieTitle produces the comparison key used to match release titles
// against movie titles (Parser.CleanMovieTitle).
func CleanMovieTitle(title string) string {
	if strings.TrimSpace(title) == "" {
		return title
	}
	if _, err := strconv.ParseInt(title, 10, 64); err == nil {
		return title
	}
	return RemoveAccent(replaceGermanUmlauts(strings.ToLower(normalizeRegex.ReplaceAllString(title, ""))))
}

func replaceGermanUmlauts(s string) string {
	return strings.NewReplacer("ä", "ae", "ö", "oe", "ü", "ue", "Ä", "Ae", "Ö", "Oe", "Ü", "Ue", "ß", "ss").Replace(s)
}

// RemoveAccent strips diacritics (without golang.org/x/text): it covers the
// Latin-1 Supplement and Latin Extended-A blocks, which is what release
// titles use in practice.
func RemoveAccent(s string) string {
	var b strings.Builder
	for _, r := range s {
		if rep, ok := accentMap[r]; ok {
			b.WriteString(rep)
			continue
		}
		if unicode.Is(unicode.Mn, r) {
			continue
		}
		b.WriteRune(r)
	}
	return b.String()
}

var accentMap = func() map[rune]string {
	m := map[rune]string{}
	add := func(base string, chars string) {
		for _, r := range chars {
			m[r] = base
		}
	}
	add("a", "àáâãäåāăą")
	add("A", "ÀÁÂÃÄÅĀĂĄ")
	add("c", "çćĉċč")
	add("C", "ÇĆĈĊČ")
	add("d", "ď")
	add("D", "Ď")
	add("e", "èéêëēĕėęě")
	add("E", "ÈÉÊËĒĔĖĘĚ")
	add("g", "ĝğġģ")
	add("G", "ĜĞĠĢ")
	add("h", "ĥ")
	add("H", "Ĥ")
	add("i", "ìíîïĩīĭį")
	add("I", "ÌÍÎÏĨĪĬĮİ")
	add("j", "ĵ")
	add("J", "Ĵ")
	add("k", "ķ")
	add("K", "Ķ")
	add("l", "ĺļľ")
	add("L", "ĹĻĽ")
	add("n", "ñńņň")
	add("N", "ÑŃŅŇ")
	add("o", "òóôõöōŏő")
	add("O", "ÒÓÔÕÖŌŎŐ")
	add("r", "ŕŗř")
	add("R", "ŔŖŘ")
	add("s", "śŝşš")
	add("S", "ŚŜŞŠ")
	add("t", "ţť")
	add("T", "ŢŤ")
	add("u", "ùúûüũūŭůűų")
	add("U", "ÙÚÛÜŨŪŬŮŰŲ")
	add("w", "ŵ")
	add("W", "Ŵ")
	add("y", "ýÿŷ")
	add("Y", "ÝŸŶ")
	add("z", "źżž")
	add("Z", "ŹŻŽ")
	return m
}()
