// Package subtitles searches, scores and downloads subtitles. Scoring is
// ported from Bazarr (subliminal_patch/score.py, movie weights) and the
// providers follow Bazarr's OpenSubtitles.com and SubDL implementations.
package subtitles

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"unicode/utf8"

	"cinearr/internal/i18n"
	"cinearr/internal/parser"
)

// Video describes the film a subtitle is wanted for.
type Video struct {
	Path        string
	Size        int64
	Hash        string // OpenSubtitles hash
	ImdbID      string
	TmdbID      int
	Title       string
	Year        int
	ReleaseName string
	// AltTitles are other titles of the film (local, English) for the
	// providers searched by title.
	AltTitles []string
	// Season and Episode are set for a series episode; ImdbID, TmdbID,
	// Title and Year are then those of the series.
	Season  int
	Episode int
	TvdbID  int // TheTVDB id of the series
}

// IsEpisode reports whether the video is a series episode.
func (v Video) IsEpisode() bool { return v.Episode > 0 }

// Titles returns the original title followed by the other titles.
func (v Video) Titles() []string {
	seen := map[string]bool{}
	var out []string
	for _, t := range append([]string{v.Title}, v.AltTitles...) {
		k := strings.ToLower(strings.TrimSpace(t))
		if k != "" && !seen[k] {
			seen[k] = true
			out = append(out, strings.TrimSpace(t))
		}
	}
	return out
}

// Candidate is a subtitle offered by a provider.
type Candidate struct {
	Provider  string `json:"provider"`
	ID        string `json:"id"`
	Release   string `json:"release"`
	Language  string `json:"language"` // ISO 639-1 (fr, en, pt-br)
	HI        bool   `json:"hi"`
	Forced    bool   `json:"forced"`
	HashMatch bool   `json:"hashMatch"`
	ImdbMatch bool   `json:"imdbMatch"`
	// EpisodeMatch: the provider says the subtitle is for this episode.
	EpisodeMatch bool     `json:"episodeMatch,omitempty"`
	Downloads    int      `json:"downloads"`
	Uploader     string   `json:"uploader,omitempty"`
	Score        int      `json:"score"`
	Percent      int      `json:"percent"`
	Matches      []string `json:"matches"`
	ref          string
}

// Provider is a subtitle source.
type Provider interface {
	Name() string
	Search(ctx context.Context, v Video, langs []string) ([]Candidate, error)
	Download(ctx context.Context, c Candidate) ([]byte, error)
}

// Movie scores (Bazarr DEFAULT_SCORES["movie"]).
var movieScores = map[string]int{
	"hash": 179, "title": 60, "year": 40, "source": 30, "edition": 30, "release_group": 15,
	"audio_codec": 1, "resolution": 1, "video_codec": 1, "hearing_impaired": 1, "streaming_service": 1,
}

// MaxScore is the best score reachable without a hash match.
var MaxScore = func() int {
	s := 0
	for k, v := range movieScores {
		if k != "hash" {
			s += v
		}
	}
	return s
}()

var equivalentGroups = [][]string{{"FRAMESTOR", "W4NK3R", "BHDSTUDIO"}, {"LOL", "DIMENSION"}, {"ASAP", "IMMERSE", "FLEET"}, {"AVS", "SVA"}}

func sameGroup(a, b string) bool {
	a, b = strings.ToUpper(a), strings.ToUpper(b)
	if a == "" || b == "" {
		return false
	}
	if a == b {
		return true
	}
	for _, g := range equivalentGroups {
		ina, inb := false, false
		for _, x := range g {
			ina = ina || x == a
			inb = inb || x == b
		}
		if ina && inb {
			return true
		}
	}
	return false
}

// Episode scores (Bazarr DEFAULT_SCORES["episode"]).
var episodeScores = map[string]int{
	"hash": 359, "series": 180, "year": 90, "season": 30, "episode": 30, "release_group": 14, "source": 7,
	"audio_codec": 3, "resolution": 2, "video_codec": 2, "streaming_service": 1, "hearing_impaired": 1,
}

// MaxEpisodeScore is the best episode score reachable without a hash match.
var MaxEpisodeScore = func() int {
	s := 0
	for k, v := range episodeScores {
		if k != "hash" {
			s += v
		}
	}
	return s
}()

// scoreEpisode is Score for a series episode.
func scoreEpisode(c *Candidate, v Video, wantHI bool) {
	matches := map[string]bool{}
	vp := parser.ParseEpisodeTitle(v.ReleaseName)
	sp := parser.ParseEpisodeTitle(c.Release)
	if c.ImdbMatch {
		matches["series"], matches["year"] = true, true
	}
	if sp != nil {
		for _, t := range v.Titles() {
			if parser.CleanSeriesTitle(sp.SeriesTitle) == parser.CleanSeriesTitle(t) {
				matches["series"] = true
			}
		}
		if sp.Year == 0 || sp.Year == v.Year {
			matches["year"] = matches["year"] || matches["series"]
		}
		if sp.Season == v.Season {
			matches["season"] = true
		}
		for _, e := range sp.Episodes {
			if e == v.Episode && sp.Season == v.Season {
				matches["episode"] = true
			}
		}
	} else if c.EpisodeMatch {
		matches["season"], matches["episode"] = true, true
	}
	if c.EpisodeMatch {
		matches["season"], matches["episode"] = true, true
	}
	if vp != nil && sp != nil {
		vq, sq := vp.Quality.Quality, sp.Quality.Quality
		if vq.Source != 0 && vq.Source == sq.Source {
			matches["source"] = true
		}
		if vq.Resolution != 0 && vq.Resolution == sq.Resolution {
			matches["resolution"] = true
		}
		if sameGroup(vp.ReleaseGroup, sp.ReleaseGroup) {
			matches["release_group"] = true
		}
	}
	codecMatches(matches, v.ReleaseName, c.Release)
	if c.HashMatch && (matches["source"] || vp == nil) {
		matches = map[string]bool{"hash": true}
	}
	if c.HI == wantHI {
		matches["hearing_impaired"] = true
	}
	score := 0
	c.Matches = c.Matches[:0]
	for m := range matches {
		score += episodeScores[m]
		c.Matches = append(c.Matches, m)
	}
	sort.Strings(c.Matches)
	c.Score = score
	c.Percent = score * 100 / MaxEpisodeScore
	if c.Percent > 100 {
		c.Percent = 100
	}
}

func codecMatches(matches map[string]bool, video, sub string) {
	lv, ls := strings.ToLower(video), strings.ToLower(sub)
	for _, codec := range []string{"x264", "h264", "x265", "h265", "hevc", "av1", "xvid"} {
		if strings.Contains(lv, codec) && strings.Contains(ls, codec) {
			matches["video_codec"] = true
		}
	}
	for _, codec := range []string{"ddp", "dd5", "ac3", "eac3", "dts", "truehd", "atmos", "aac", "flac"} {
		if strings.Contains(lv, codec) && strings.Contains(ls, codec) {
			matches["audio_codec"] = true
		}
	}
	for _, svc := range []string{"amzn", "nf", "dsnp", "atvp", "hmax", "max", "pcok", "hulu"} {
		if containsWord(lv, svc) && containsWord(ls, svc) {
			matches["streaming_service"] = true
		}
	}
}

// Score computes Bazarr's score for a candidate.
func Score(c *Candidate, v Video, wantHI bool) {
	if v.IsEpisode() {
		scoreEpisode(c, v, wantHI)
		return
	}
	matches := map[string]bool{}
	vp := parser.ParseMovieTitle(v.ReleaseName, false)
	sp := parser.ParseMovieTitle(c.Release, false)
	if c.ImdbMatch {
		matches["title"], matches["year"] = true, true
	}
	if sp != nil {
		for _, t := range sp.MovieTitles {
			if parser.CleanMovieTitle(t) == parser.CleanMovieTitle(v.Title) {
				matches["title"] = true
			}
		}
		if sp.Year != 0 && sp.Year == v.Year {
			matches["year"] = true
		}
	}
	if vp != nil && sp != nil {
		vq, sq := vp.Quality.Quality, sp.Quality.Quality
		if vq.Source != 0 && vq.Source == sq.Source {
			matches["source"] = true
		}
		if vq.Resolution != 0 && vq.Resolution == sq.Resolution {
			matches["resolution"] = true
		}
		if sameGroup(vp.ReleaseGroup, sp.ReleaseGroup) {
			matches["release_group"] = true
		}
		if vp.Edition != "" && strings.EqualFold(vp.Edition, sp.Edition) {
			matches["edition"] = true
		} else if vp.Edition == "" && sp.Edition == "" {
			matches["edition"] = true
		}
		lv, ls := strings.ToLower(v.ReleaseName), strings.ToLower(c.Release)
		for _, codec := range []string{"x264", "h264", "x265", "h265", "hevc", "av1", "xvid"} {
			if strings.Contains(lv, codec) && strings.Contains(ls, codec) {
				matches["video_codec"] = true
			}
		}
		for _, codec := range []string{"ddp", "dd5", "ac3", "eac3", "dts", "truehd", "atmos", "aac", "flac"} {
			if strings.Contains(lv, codec) && strings.Contains(ls, codec) {
				matches["audio_codec"] = true
			}
		}
		for _, svc := range []string{"amzn", "nf", "dsnp", "atvp", "hmax", "max", "pcok", "hulu"} {
			if containsWord(lv, svc) && containsWord(ls, svc) {
				matches["streaming_service"] = true
			}
		}
	}
	if c.HashMatch {
		// "on hash match, discard everything else" (Bazarr), when the
		// provider's hash is trustworthy: video_codec and source must agree
		if matches["source"] || vp == nil {
			matches = map[string]bool{"hash": true}
		}
	}
	if c.HI == wantHI {
		matches["hearing_impaired"] = true
	}
	score := 0
	c.Matches = c.Matches[:0]
	for m := range matches {
		score += movieScores[m]
		c.Matches = append(c.Matches, m)
	}
	sort.Strings(c.Matches)
	c.Score = score
	c.Percent = score * 100 / MaxScore
	if c.Percent > 100 {
		c.Percent = 100
	}
}

func containsWord(s, w string) bool {
	for _, f := range strings.FieldsFunc(s, func(r rune) bool { return r == '.' || r == ' ' || r == '-' || r == '_' || r == '[' || r == ']' }) {
		if f == w {
			return true
		}
	}
	return false
}

// Hash computes the OpenSubtitles hash (file size + 64-bit sums of the first
// and last 64 KiB).
func Hash(path string) (string, int64, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", 0, err
	}
	defer f.Close()
	st, err := f.Stat()
	if err != nil {
		return "", 0, err
	}
	size := st.Size()
	const chunk = 65536
	if size < chunk*2 {
		return "", size, errors.New("file too small for the hash")
	}
	h := uint64(size)
	buf := make([]byte, chunk)
	for _, off := range []int64{0, size - chunk} {
		if _, err := f.ReadAt(buf, off); err != nil {
			return "", size, err
		}
		for i := 0; i < chunk; i += 8 {
			h += binary.LittleEndian.Uint64(buf[i:])
		}
	}
	return fmt.Sprintf("%016x", h), size, nil
}

// Rank sorts candidates by score, then hash match, then popularity.
func Rank(cs []Candidate) {
	sort.SliceStable(cs, func(i, j int) bool {
		if cs[i].Score != cs[j].Score {
			return cs[i].Score > cs[j].Score
		}
		if cs[i].HashMatch != cs[j].HashMatch {
			return cs[i].HashMatch
		}
		return cs[i].Downloads > cs[j].Downloads
	})
}

// ---------------------------------------------------------------------------
// Saving

var cp1252High = [32]rune{
	0x20AC, 0x81, 0x201A, 0x0192, 0x201E, 0x2026, 0x2020, 0x2021, 0x02C6, 0x2030, 0x0160, 0x2039, 0x0152, 0x8D, 0x017D, 0x8F,
	0x90, 0x2018, 0x2019, 0x201C, 0x201D, 0x2022, 0x2013, 0x2014, 0x02DC, 0x2122, 0x0161, 0x203A, 0x0153, 0x9D, 0x017E, 0x0178,
}

// ToUTF8 converts subtitle bytes to UTF-8 (UTF-16 BOM, UTF-8, or
// Windows-1252 which most French subtitles use).
func ToUTF8(b []byte) []byte {
	switch {
	case bytes.HasPrefix(b, []byte{0xEF, 0xBB, 0xBF}):
		return b[3:]
	case bytes.HasPrefix(b, []byte{0xFF, 0xFE}) || bytes.HasPrefix(b, []byte{0xFE, 0xFF}):
		le := b[0] == 0xFF
		var sb strings.Builder
		for i := 2; i+1 < len(b); i += 2 {
			var u uint16
			if le {
				u = uint16(b[i]) | uint16(b[i+1])<<8
			} else {
				u = uint16(b[i])<<8 | uint16(b[i+1])
			}
			sb.WriteRune(rune(u))
		}
		return []byte(sb.String())
	case utf8.Valid(b):
		return b
	}
	var sb strings.Builder
	for _, c := range b {
		switch {
		case c < 0x80:
			sb.WriteByte(c)
		case c < 0xA0:
			sb.WriteRune(cp1252High[c-0x80])
		default:
			sb.WriteRune(rune(c))
		}
	}
	return []byte(sb.String())
}

// ExtractSubtitle returns the subtitle file inside a zip archive (or the
// data itself if it is not an archive).
func ExtractSubtitle(data []byte) ([]byte, string, error) { return ExtractSubtitleFor(data, 0, 0) }

// ExtractSubtitleFor is ExtractSubtitle for an episode: in a season archive
// the file named after the episode is taken.
func ExtractSubtitleFor(data []byte, season, episode int) ([]byte, string, error) {
	if !bytes.HasPrefix(data, []byte("PK")) {
		return data, ".srt", nil
	}
	zr, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return nil, "", err
	}
	var best, match *zip.File
	for _, f := range zr.File {
		ext := strings.ToLower(filepath.Ext(f.Name))
		if ext == ".srt" || ext == ".ass" || ext == ".ssa" || ext == ".vtt" || ext == ".sub" {
			if best == nil || f.UncompressedSize64 > best.UncompressedSize64 {
				best = f
			}
			if episode > 0 {
				if pe := parser.ParseEpisodeTitle(filepath.Base(f.Name)); pe != nil && pe.Season == season {
					for _, e := range pe.Episodes {
						if e == episode && (match == nil || f.UncompressedSize64 > match.UncompressedSize64) {
							match = f
						}
					}
				}
			}
		}
	}
	if match != nil {
		best = match
	} else if episode > 0 && best != nil && len(zr.File) > 3 {
		return nil, "", i18n.New("this archive has no subtitle for the episode")
	}
	if best == nil {
		return nil, "", i18n.New("no subtitle file in the archive")
	}
	if best.UncompressedSize64 > 10<<20 {
		return nil, "", i18n.New("abnormally large subtitle file")
	}
	rc, err := best.Open()
	if err != nil {
		return nil, "", err
	}
	defer rc.Close()
	b, err := io.ReadAll(io.LimitReader(rc, 10<<20))
	return b, strings.ToLower(filepath.Ext(best.Name)), err
}

// languageCode is an ISO 639 code, with an optional region or script
// ("fr", "fre", "pt-br", "zh-hans"); "und" is the unknown language.
var languageCode = regexp.MustCompile(`^[a-z]{2,3}(?:-[a-z0-9]{2,4})?$`)

// NormalizeLanguage lowers a language code ("pt_BR" → "pt-br") and
// refuses anything else than a language code: the code is part of the
// subtitle file name, a value such as "../x" must never reach it.
func NormalizeLanguage(code string) (string, error) {
	c := strings.ReplaceAll(strings.ToLower(strings.TrimSpace(code)), "_", "-")
	if c == "" {
		return "und", nil
	}
	if !languageCode.MatchString(c) {
		return "", i18n.Errorf("invalid language code: %q", code)
	}
	return c, nil
}

// SavePath returns "<video>.<lang>[.hi|.forced].<ext>".
func SavePath(videoPath, lang string, hi, forced bool, ext string) string {
	base := strings.TrimSuffix(videoPath, filepath.Ext(videoPath))
	name := base + "." + lang
	if forced {
		name += ".forced"
	} else if hi {
		name += ".hi"
	}
	if ext == "" {
		ext = ".srt"
	}
	return name + ext
}
