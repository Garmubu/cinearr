// Package decision evaluates indexer releases for a movie. It ports the
// main specifications of Radarr's DecisionEngine (quality allowed by
// profile, acceptable size, torrent seeding, blocklist, release
// restrictions, language, hardcoded subs, upgrade rules) and its
// DownloadDecisionComparer ranking, plus extra safety checks against fake
// or malicious torrents.
package decision

import (
	"fmt"
	"math"
	"regexp"
	"sort"
	"strings"
	"time"

	"cinearr/internal/i18n"
	"cinearr/internal/model"
	"cinearr/internal/parser"
	"cinearr/internal/quality"
)

// PreferredTerm adds Score to releases whose title contains Term
// (case-insensitive; "/regex/" for a regular expression).
type PreferredTerm struct {
	Term  string `json:"term"`
	Score int    `json:"score"`
}

// Profile is a quality profile (Radarr QualityProfile + restrictions).
type Profile struct {
	Allowed             []int           `json:"allowed"` // quality ids, worst to best
	Cutoff              int             `json:"cutoff"`
	UpgradesAllowed     bool            `json:"upgradesAllowed"`
	PreferredLanguages  []string        `json:"preferredLanguages"`
	RequiredLanguages   []string        `json:"requiredLanguages"`
	MultiCountsAs       []string        `json:"multiCountsAs"` // languages a MULTi release is assumed to contain
	MinSeeders          int             `json:"minSeeders"`
	MaxSizeGB           float64         `json:"maxSizeGB"`
	MustContain         []string        `json:"mustContain"`
	MustNotContain      []string        `json:"mustNotContain"`
	Preferred           []PreferredTerm `json:"preferred"`
	RejectHardcodedSubs bool            `json:"rejectHardcodedSubs"`
	AntiFakeMinSize     bool            `json:"antiFakeMinSize"`
}

// DefaultProfile favours 1080p, French or MULTi audio.
func DefaultProfile() Profile {
	return Profile{
		Allowed: []int{
			quality.HDTV720p.ID, quality.WEBRip720p.ID, quality.WEBDL720p.ID, quality.Bluray720p.ID,
			quality.HDTV1080p.ID, quality.WEBRip1080p.ID, quality.WEBDL1080p.ID, quality.Bluray1080p.ID,
		},
		Cutoff:              quality.Bluray1080p.ID,
		UpgradesAllowed:     true,
		PreferredLanguages:  []string{"French"},
		MultiCountsAs:       []string{"French", "Original"},
		MinSeeders:          1,
		MaxSizeGB:           0,
		MustNotContain:      []string{},
		Preferred:           []PreferredTerm{{Term: "VFF", Score: 5}, {Term: "MULTi", Score: 10}, {Term: "/\\bHDR10?\\b/", Score: 2}},
		RejectHardcodedSubs: true,
		AntiFakeMinSize:     true,
	}
}

// ProfileForLanguage returns the default profile for a user who prefers
// releases in the given language (name as in release titles: "French").
func ProfileForLanguage(release string) Profile {
	p := DefaultProfile()
	if release == "" || release == "French" {
		return p
	}
	p.PreferredLanguages = []string{release}
	p.MultiCountsAs = []string{release, "Original"}
	// VFF and MULTi are French conventions
	p.Preferred = []PreferredTerm{{Term: "/\\bHDR10?\\b/", Score: 2}}
	return p
}

// Decision is the outcome for one release.
type Decision struct {
	Release      model.Release       `json:"release"`
	Parsed       *parser.ParsedMovie `json:"parsed,omitempty"`
	Quality      quality.Quality     `json:"quality"`
	Languages    []string            `json:"languages"`
	Approved     bool                `json:"approved"`
	Rejections   []string            `json:"rejections"`
	Warnings     []string            `json:"warnings"`
	Score        int                 `json:"score"`
	QualityRank  int                 `json:"qualityRank"`
	TitleMatched bool                `json:"titleMatched"`

	// series releases
	Episode *parser.ParsedEpisode `json:"episode,omitempty"`
	// Covered lists the selected episodes the release contains.
	Covered []model.EpisodeKey `json:"covered,omitempty"`
	Label   string             `json:"label,omitempty"`
	// PackRank orders series releases like Sonarr: season packs first,
	// then releases with fewer episodes.
	PackRank int `json:"-"`
	rev      int
}

func (d *Decision) reject(format string, a ...any) {
	d.Rejections = append(d.Rejections, i18n.Tf(format, a...))
}

// Context carries what the engine needs besides the profile.
type Context struct {
	Movie     model.Movie
	Blocklist []model.BlocklistEntry
	// Approximate free space at the destination (bytes); 0 = unknown.
	FreeSpace int64
}

// suspicious titles: executables or archives announced in the name, or
// typical malware lures.
var suspiciousTitle = regexp.MustCompile(`(?i)(\.(exe|scr|lnk|bat|cmd|vbs|ps1|msi|zipx|com|pif|js)\b|\bpassword\b|\bcodec[ ._-]?(pack|install)|\bkeygen\b|\bcrack\b)`)

// Evaluate checks every release and returns them ranked (approved first).
func Evaluate(releases []model.Release, p Profile, ctx Context) []Decision {
	out := make([]Decision, 0, len(releases))
	for _, r := range releases {
		out = append(out, evaluateOne(r, p, ctx))
	}
	Sort(out)
	return out
}

func evaluateOne(r model.Release, p Profile, ctx Context) Decision {
	d := Decision{Release: r}
	parsed := parser.ParseMovieTitle(r.Title, false)
	if parsed == nil {
		d.reject("Unreadable release title")
		d.Quality = parser.ParseQuality(r.Title).Quality
		return d
	}
	d.Parsed = parsed
	d.Quality = parsed.Quality.Quality
	d.Languages = parsed.Languages
	movie := ctx.Movie

	// --- Mapping release -> movie (ParsingService) ---------------------
	d.TitleMatched = titleMatches(parsed, r, movie)
	if !d.TitleMatched {
		d.reject("The title does not match the film (%s %d)", parsed.PrimaryTitle(), parsed.Year)
	}

	// --- Upgrade specifications ----------------------------------------
	d.checkUpgrade(p, movie.File, "The film is already downloaded (upgrades disabled)")

	// --- BlocklistSpecification -----------------------------------------
	for _, b := range ctx.Blocklist {
		if b.SeriesID != 0 || (b.TmdbID != 0 && b.TmdbID != movie.TmdbID) {
			continue
		}
		if (b.Hash != "" && strings.EqualFold(b.Hash, r.InfoHash)) || (b.Title == r.Title && (b.Indexer == "" || b.Indexer == r.Indexer)) {
			d.reject("Release on the blocklist: %s", b.Reason)
			break
		}
	}

	runtime := movie.Runtime
	if runtime <= 0 {
		runtime = 110
	}
	d.rev = parsed.Quality.Revision.Version*10 + parsed.Quality.Revision.Real
	d.checkRelease(r, p, parsed.Languages, movie.OriginalLanguage, parsed.HardcodedSubs, runtime, ctx.FreeSpace)
	d.Approved = len(d.Rejections) == 0
	return d
}

// checkUpgrade applies the upgrade specifications against the current file.
func (d *Decision) checkUpgrade(p Profile, file *model.MovieFile, already string) {
	if file == nil {
		return
	}
	rank := indexOf(p.Allowed, d.Quality.ID)
	current := indexOf(p.Allowed, file.QualityID)
	cutoff := indexOf(p.Allowed, p.Cutoff)
	switch {
	case !p.UpgradesAllowed:
		d.reject(already)
	case cutoff >= 0 && current >= cutoff:
		d.reject("Target quality already reached (%s)", file.QualityName)
	case rank <= current:
		d.reject("Not better than the current file (%s)", file.QualityName)
	}
}

// checkRelease runs the specifications shared by films and series:
// safety, quality, seeds, size for the runtime, restrictions, language,
// hardcoded subtitles and score.
func (d *Decision) checkRelease(r model.Release, p Profile, parsedLangs []string, original, hcSubs string, runtime int, freeSpace int64) {
	// --- Security ------------------------------------------------------
	if suspiciousTitle.MatchString(r.Title) {
		d.reject("Suspicious title (executable, protected archive or known decoy)")
	}
	if r.DownloadURL == "" && r.MagnetURL == "" && r.InfoHash == "" {
		d.reject("No download link")
	}

	// --- QualityAllowedByProfileSpecification ---------------------------
	rank := indexOf(p.Allowed, d.Quality.ID)
	d.QualityRank = rank
	if rank < 0 {
		d.reject("Quality %s not allowed by the profile", d.Quality.Name)
	}

	// --- TorrentSeedingSpecification -------------------------------------
	if r.Seeders < p.MinSeeders {
		d.reject("Not enough seeds (%d < %d)", r.Seeders, p.MinSeeders)
	}

	// --- AcceptableSize / MaximumSize ------------------------------------
	def := quality.DefinitionFor(d.Quality)
	if r.Size > 0 {
		mb := float64(r.Size) / (1 << 20)
		if def.MaxSize > 0 && mb > def.MaxSize*float64(runtime) {
			d.reject("Too large for %s (%s)", d.Quality.Name, HumanSize(r.Size))
		}
		if def.MinSize > 0 && mb < def.MinSize*float64(runtime) {
			d.reject("Too small for %s (%s)", d.Quality.Name, HumanSize(r.Size))
		}
		if p.AntiFakeMinSize {
			if min := antiFakeMinMBPerMinute(d.Quality); min > 0 && mb < min*float64(runtime) {
				d.reject("Abnormally small for %s (%s): probably a fake", d.Quality.Name, HumanSize(r.Size))
			}
		}
		if p.MaxSizeGB > 0 && float64(r.Size) > p.MaxSizeGB*(1<<30) {
			d.reject("Exceeds the maximum size (%s > %.0f GB)", HumanSize(r.Size), p.MaxSizeGB)
		}
		if freeSpace > 0 && r.Size > freeSpace {
			d.reject("Not enough disk space")
		}
	} else {
		d.Warnings = append(d.Warnings, i18n.T("Unknown size"))
	}

	// --- ReleaseRestrictionsSpecification ---------------------------------
	if len(nonEmpty(p.MustContain)) > 0 {
		found := false
		for _, t := range nonEmpty(p.MustContain) {
			if termMatches(t, r.Title) {
				found = true
			}
		}
		if !found {
			d.reject("Contains none of the required terms")
		}
	}
	for _, t := range nonEmpty(p.MustNotContain) {
		if termMatches(t, r.Title) {
			d.reject("Contains a forbidden term (%s)", t)
		}
	}

	// --- LanguageSpecification ---------------------------------------------
	langs := effectiveLanguages(r.Title, parsedLangs, p, original)
	d.Languages = langs
	if len(p.RequiredLanguages) > 0 && !anyIn(p.RequiredLanguages, langs) {
		d.reject("Language not accepted (%s)", strings.Join(langs, ", "))
	}

	// --- HardcodeSubsSpecification -------------------------------------------
	if hcSubs != "" && p.RejectHardcodedSubs {
		d.reject("Hardcoded subtitles (%s)", hcSubs)
	}

	// --- Score ("custom formats" light) ----------------------------------------
	score := 0
	for i, l := range p.PreferredLanguages {
		if contains(langs, l) {
			score += 20 - i*5
		}
	}
	for _, t := range p.Preferred {
		if t.Term != "" && termMatches(t.Term, r.Title) {
			score += t.Score
		}
	}
	if r.DownloadVolumeFactor == 0 && r.Private {
		score += 1 // freeleech
	}
	d.Score = score

	if r.PublishDate.After(time.Now().Add(24 * time.Hour)) {
		d.Warnings = append(d.Warnings, i18n.T("Publication date in the future"))
	}
}

// antiFakeMinMBPerMinute returns conservative lower bounds: a 2h 1080p
// encode below ~600 MB is almost always fake or mislabelled.
func antiFakeMinMBPerMinute(q quality.Quality) float64 {
	switch {
	case q.Modifier == quality.ModRemux && q.Resolution >= 2160:
		return 120
	case q.Modifier == quality.ModRemux:
		return 80
	case q.Resolution >= 2160:
		return 12
	case q.Resolution >= 1080:
		return 5
	case q.Resolution >= 720:
		return 2.5
	}
	return 0
}

// EffectiveLanguages returns the parsed languages, completed for MULTi
// releases with the languages the profile assumes they contain; a release
// without any language tag is taken to be in the original language of the
// film or series (original: ISO 639-1 code from TMDB), as Radarr does.
func EffectiveLanguages(title string, langs []string, p Profile, original string) []string {
	return effectiveLanguages(title, langs, p, original)
}

// multiTag finds the MULTi tag between any separators ("[MULTi]",
// "MULTi-GRP", "(MULTI)", ".MULTi.", at the start or the end). Radarr's
// MultiRegex (parser.HasMultipleLanguages) only accepts "_", "." and spaces
// around it, which misses most French release names; it is kept as ported
// and this wider test is used for the decisions.
var multiTag = regexp.MustCompile(`(?i)(?:^|[\W_])multi(?:[\W_]|$)`)

// HasMulti reports whether a release title carries the MULTi tag.
func HasMulti(title string) bool {
	return multiTag.MatchString(title) || parser.HasMultipleLanguages(" "+title+" ")
}

// isoLanguages maps the ISO 639-1 codes of TMDB (original_language) to the
// language names of the release parser.
var isoLanguages = map[string]string{
	"en": "English", "fr": "French", "es": "Spanish", "de": "German", "it": "Italian", "pt": "Portuguese",
	"nl": "Dutch", "da": "Danish", "sv": "Swedish", "no": "Norwegian", "nb": "Norwegian", "fi": "Finnish",
	"is": "Icelandic", "pl": "Polish", "cs": "Czech", "sk": "Slovak", "hu": "Hungarian", "ro": "Romanian",
	"bg": "Bulgarian", "el": "Greek", "ru": "Russian", "uk": "Ukrainian", "lt": "Lithuanian", "lv": "Latvian",
	"tr": "Turkish", "he": "Hebrew", "ar": "Arabic", "fa": "Persian", "hi": "Hindi", "bn": "Bengali",
	"ta": "Tamil", "te": "Telugu", "ml": "Malayalam", "kn": "Kannada", "mr": "Marathi", "ur": "Urdu",
	"th": "Thai", "vi": "Vietnamese", "ja": "Japanese", "ko": "Korean", "zh": "Chinese", "cn": "Chinese",
	"ca": "Catalan", "sq": "Albanian", "af": "Afrikaans", "tl": "Tagalog", "ga": "Irish", "ka": "Georgian",
	"mn": "Mongolian", "rm": "Romansh", "fl": "Flemish",
}

// LanguageName returns the release language name of an ISO 639-1 code, or "".
func LanguageName(iso string) string { return isoLanguages[strings.ToLower(strings.TrimSpace(iso))] }

func effectiveLanguages(title string, langs []string, p Profile, original string) []string {
	out := append([]string(nil), langs...)
	if HasMulti(title) {
		out = removeValue(out, "Unknown")
		for _, l := range p.MultiCountsAs {
			if !contains(out, l) {
				out = append(out, l)
			}
		}
	}
	if len(out) == 0 {
		out = []string{"Unknown"}
	}
	// no language tag: the release is in the original language (Radarr's
	// behaviour for untagged releases)
	if len(out) == 1 && out[0] == "Unknown" {
		if name := LanguageName(original); name != "" {
			out = []string{name, "Original"}
		}
	}
	return out
}

func titleMatches(parsed *parser.ParsedMovie, r model.Release, m model.Movie) bool {
	if m.TmdbID != 0 && r.TmdbID == m.TmdbID {
		return true
	}
	if m.ImdbID != "" && r.ImdbID != "" && strings.EqualFold(parser.NormalizeImdbID(r.ImdbID), m.ImdbID) {
		return true
	}
	if m.ImdbID != "" && parsed.ImdbID != "" && strings.EqualFold(parsed.ImdbID, m.ImdbID) {
		return true
	}
	candidates := append([]string{m.Title, m.OriginalTitle, m.EnglishTitle}, m.AlternativeTitles...)
	yearOK := parsed.Year == 0 || m.Year == 0 || abs(parsed.Year-m.Year) <= 1
	if !yearOK {
		return false
	}
	for _, pt := range parsed.MovieTitles {
		ct := parser.CleanMovieTitle(pt)
		for _, c := range candidates {
			if c != "" && parser.CleanMovieTitle(c) == ct {
				return true
			}
		}
	}
	return false
}

func termMatches(term, title string) bool {
	term = strings.TrimSpace(term)
	// the MULTi term follows the same detection as the languages, so its
	// bonus and the languages always agree
	if strings.EqualFold(term, "multi") {
		return HasMulti(title)
	}
	if len(term) > 2 && strings.HasPrefix(term, "/") && strings.HasSuffix(term, "/") {
		re, err := regexp.Compile("(?i)" + term[1:len(term)-1])
		return err == nil && re.MatchString(title)
	}
	return strings.Contains(strings.ToLower(title), strings.ToLower(term))
}

// Sort ranks decisions like Radarr's DownloadDecisionComparer: approved
// first, then quality (profile order), revision, score, seeders (log10),
// indexer priority and size.
func Sort(ds []Decision) {
	sort.SliceStable(ds, func(i, j int) bool {
		a, b := ds[i], ds[j]
		if a.Approved != b.Approved {
			return a.Approved
		}
		if a.QualityRank != b.QualityRank {
			return a.QualityRank > b.QualityRank
		}
		av, bv := revision(a), revision(b)
		if av != bv {
			return av > bv
		}
		if a.Score != b.Score {
			return a.Score > b.Score
		}
		if a.PackRank != b.PackRank {
			return a.PackRank > b.PackRank
		}
		if la, lb := seedBucket(a.Release.Seeders), seedBucket(b.Release.Seeders); la != lb {
			return la > lb
		}
		if a.Release.IndexerPriority != b.Release.IndexerPriority {
			return a.Release.IndexerPriority < b.Release.IndexerPriority
		}
		return a.Release.Size > b.Release.Size
	})
}

func revision(d Decision) int { return d.rev }

func seedBucket(s int) float64 {
	if s <= 0 {
		return 0
	}
	return math.Round(math.Log10(float64(s)))
}

// HumanSize formats bytes.
func HumanSize(n int64) string {
	f := float64(n)
	switch {
	case f >= 1<<40:
		return fmt.Sprintf("%.2f %s", f/(1<<40), i18n.T("TB"))
	case f >= 1<<30:
		return fmt.Sprintf("%.2f %s", f/(1<<30), i18n.T("GB"))
	case f >= 1<<20:
		return fmt.Sprintf("%.0f %s", f/(1<<20), i18n.T("MB"))
	case f >= 1<<10:
		return fmt.Sprintf("%.0f %s", f/(1<<10), i18n.T("KB"))
	}
	return fmt.Sprintf("%d %s", n, i18n.T("B"))
}

func indexOf(list []int, v int) int {
	for i, x := range list {
		if x == v {
			return i
		}
	}
	return -1
}

func contains(list []string, v string) bool {
	for _, x := range list {
		if strings.EqualFold(x, v) {
			return true
		}
	}
	return false
}

func anyIn(want, have []string) bool {
	for _, w := range want {
		if contains(have, w) {
			return true
		}
	}
	return false
}

func removeValue(list []string, v string) []string {
	var out []string
	for _, x := range list {
		if x != v {
			out = append(out, x)
		}
	}
	return out
}

func nonEmpty(list []string) []string {
	var out []string
	for _, x := range list {
		if strings.TrimSpace(x) != "" {
			out = append(out, x)
		}
	}
	return out
}

func abs(x int) int {
	if x < 0 {
		return -x
	}
	return x
}
