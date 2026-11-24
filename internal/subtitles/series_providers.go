package subtitles

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"

	"cinearr/internal/dom"
	"cinearr/internal/i18n"
	"cinearr/internal/parser"
)

// Providers specialised in series (ports of Bazarr's gestdown, tvsubtitles
// and betaseries providers).

// episodeRelease builds a release name the episode scorer can read when a
// site only gives the version ("KILLERS", "WEB.h264-ROBOTS").
func episodeRelease(v Video, version string) string {
	name := fmt.Sprintf("%s.S%02dE%02d", strings.ReplaceAll(v.Title, " ", "."), v.Season, v.Episode)
	if version = strings.TrimSpace(version); version != "" {
		name += "." + strings.ReplaceAll(version, " ", ".")
	}
	return name
}

// ------------------------------------------------------------ Gestdown

// Gestdown is the public API of Addic7ed's subtitles (api.gestdown.info),
// the largest source of series subtitles. No account needed.
type Gestdown struct {
	server string
}

// Name implements Provider.
func (g *Gestdown) Name() string { return "gestdown" }

// SupportsEpisodes implements the series capability.
func (g *Gestdown) SupportsEpisodes() bool { return true }

func (g *Gestdown) base() string {
	if g.server != "" {
		return g.server
	}
	return "https://api.gestdown.info"
}

// addic7edLanguages are the language names of Addic7ed.
var addic7edLanguages = map[string]string{
	"en": "English", "fr": "French", "es": "Spanish", "it": "Italian", "de": "German", "pt": "Portuguese",
	"pt-br": "Portuguese (Brazilian)", "nl": "Dutch", "ar": "Arabic", "ru": "Russian", "pl": "Polish", "tr": "Turkish",
	"el": "Greek", "he": "Hebrew", "sv": "Swedish", "da": "Danish", "no": "Norwegian", "fi": "Finnish", "cs": "Czech",
	"hu": "Hungarian", "ro": "Romanian", "bg": "Bulgarian", "hr": "Croatian", "sr": "Serbian (Latin)", "sl": "Slovenian",
	"uk": "Ukrainian", "zh": "Chinese (Simplified)", "ja": "Japanese", "ko": "Korean", "vi": "Vietnamese", "fa": "Persian",
	"id": "Indonesian",
}

type gestdownShow struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

func (g *Gestdown) getJSON(ctx context.Context, path string, out any) (int, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, g.base()+path, nil)
	if err != nil {
		return 0, err
	}
	req.Header.Set("User-Agent", userAgent)
	req.Header.Set("Accept", "application/json")
	resp, err := httpClient.Do(req)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return resp.StatusCode, nil
	}
	return resp.StatusCode, json.NewDecoder(io.LimitReader(resp.Body, 8<<20)).Decode(out)
}

func (g *Gestdown) shows(ctx context.Context, v Video) ([]gestdownShow, bool, error) {
	var r struct {
		Shows []gestdownShow `json:"shows"`
	}
	if v.TvdbID > 0 {
		code, err := g.getJSON(ctx, "/shows/external/tvdb/"+strconv.Itoa(v.TvdbID), &r)
		if err != nil {
			return nil, false, err
		}
		if code == http.StatusOK && len(r.Shows) > 0 {
			return r.Shows, true, nil
		}
	}
	if len([]rune(v.Title)) < 3 {
		return nil, false, nil
	}
	if _, err := g.getJSON(ctx, "/shows/search/"+url.PathEscape(v.Title), &r); err != nil {
		return nil, false, err
	}
	var out []gestdownShow
	for _, s := range r.Shows {
		for _, t := range v.Titles() {
			if parser.CleanSeriesTitle(s.Name) == parser.CleanSeriesTitle(t) {
				out = append(out, s)
				break
			}
		}
	}
	return out, false, nil
}

// Search implements Provider.
func (g *Gestdown) Search(ctx context.Context, v Video, langs []string) ([]Candidate, error) {
	if !v.IsEpisode() {
		return nil, nil
	}
	shows, byID, err := g.shows(ctx, v)
	if err != nil {
		return nil, err
	}
	var out []Candidate
	for _, s := range shows {
		for _, lang := range langs {
			name := addic7edLanguages[strings.ToLower(lang)]
			if name == "" {
				continue
			}
			var r struct {
				Matching []struct {
					SubtitleID      string   `json:"subtitleId"`
					Version         string   `json:"version"`
					Completed       bool     `json:"completed"`
					HearingImpaired bool     `json:"hearingImpaired"`
					DownloadURI     string   `json:"downloadUri"`
					Qualities       []string `json:"qualities"`
					Release         string   `json:"release"`
					DownloadCount   int      `json:"downloadCount"`
				} `json:"matchingSubtitles"`
			}
			path := fmt.Sprintf("/subtitles/get/%s/%d/%d/%s", s.ID, v.Season, v.Episode, url.PathEscape(name))
			code, err := g.getJSON(ctx, path, &r)
			if err != nil {
				return out, err
			}
			if code == http.StatusLocked {
				return out, i18n.New("Gestdown: the show is being refreshed, try again in a minute")
			}
			for _, m := range r.Matching {
				if !m.Completed || m.DownloadURI == "" {
					continue
				}
				version := m.Version
				if m.Release != "" {
					version = m.Release
				}
				if len(m.Qualities) > 0 {
					version += "." + strings.Join(m.Qualities, ".")
				}
				out = append(out, Candidate{Provider: g.Name(), ID: m.DownloadURI, Release: episodeRelease(v, version),
					Language: strings.ToLower(lang), HI: m.HearingImpaired, ImdbMatch: byID, EpisodeMatch: true,
					Downloads: m.DownloadCount, Uploader: "Addic7ed", ref: m.DownloadURI})
			}
		}
	}
	return out, nil
}

// Download implements Provider (the reference is the download path).
func (g *Gestdown) Download(ctx context.Context, c Candidate) ([]byte, error) {
	u := c.ref
	if !strings.HasPrefix(u, "http") {
		u = g.base() + u
	}
	return get(ctx, u, nil)
}

// Test checks that the API answers.
func (g *Gestdown) Test(ctx context.Context) (string, error) {
	var r struct {
		Shows []gestdownShow `json:"shows"`
	}
	code, err := g.getJSON(ctx, "/shows/search/"+url.PathEscape("Breaking Bad"), &r)
	if err != nil {
		return "", err
	}
	if code != http.StatusOK {
		return "", fmt.Errorf("Gestdown: HTTP %d", code)
	}
	return i18n.T("service reachable"), nil
}

// ---------------------------------------------------------- TVsubtitles

// TVSubtitles scrapes tvsubtitles.net (series only, no account).
type TVSubtitles struct {
	server string
}

// Name implements Provider.
func (t *TVSubtitles) Name() string { return "tvsubtitles" }

// SupportsEpisodes implements the series capability.
func (t *TVSubtitles) SupportsEpisodes() bool { return true }

func (t *TVSubtitles) base() string {
	if t.server != "" {
		return t.server
	}
	return "https://www.tvsubtitles.net/"
}

var (
	tvsShowRe    = regexp.MustCompile(`^/?tvshow-(\d+)(?:-\d+)?\.html$`)
	tvsYearsRe   = regexp.MustCompile(`\s*\((\d{4})(?:-\d{4})?\)\s*$`)
	tvsIDRe      = regexp.MustCompile(`-(\d+)\.html$`)
	tvsFlagRe    = regexp.MustCompile(`flags/([a-z]{2})\.`)
	tvsLanguages = map[string]string{"br": "pt-br", "gr": "el", "ua": "uk", "cn": "zh", "jp": "ja", "kr": "ko", "cz": "cs", "se": "sv", "dk": "da"}
)

func (t *TVSubtitles) showID(ctx context.Context, v Video) (int, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, t.base()+"search1.php", strings.NewReader(url.Values{"qs": {v.Title}}.Encode()))
	if err != nil {
		return 0, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("User-Agent", browserUA)
	resp, err := httpClient.Do(req)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return 0, fmt.Errorf("tvsubtitles.net: HTTP %d", resp.StatusCode)
	}
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	links, _ := dom.QuerySelectorAll(dom.Parse(string(body)), `a[href*="tvshow-"]`)
	for _, a := range links {
		href, _ := a.Attr("href")
		m := tvsShowRe.FindStringSubmatch(href)
		if m == nil {
			continue
		}
		text := strings.TrimSpace(a.TextContent())
		year := 0
		if y := tvsYearsRe.FindStringSubmatch(text); y != nil {
			year, _ = strconv.Atoi(y[1])
			text = tvsYearsRe.ReplaceAllString(text, "")
		}
		if v.Year > 0 && year > 0 && year != v.Year {
			continue
		}
		for _, title := range v.Titles() {
			if parser.CleanSeriesTitle(text) == parser.CleanSeriesTitle(title) {
				id, _ := strconv.Atoi(m[1])
				return id, nil
			}
		}
	}
	return 0, nil
}

// Search implements Provider.
func (t *TVSubtitles) Search(ctx context.Context, v Video, langs []string) ([]Candidate, error) {
	if !v.IsEpisode() {
		return nil, nil
	}
	show, err := t.showID(ctx, v)
	if err != nil || show == 0 {
		return nil, err
	}
	hdr := map[string]string{"User-Agent": browserUA}
	page, err := get(ctx, t.base()+fmt.Sprintf("tvshow-%d-%d.html", show, v.Season), hdr)
	if err != nil || page == nil {
		return nil, err
	}
	episodeID := 0
	rows, _ := dom.QuerySelectorAll(dom.Parse(string(page)), "table#table5 tr")
	for _, row := range rows {
		cells, _ := dom.QuerySelectorAll(row, "td")
		if len(cells) < 2 {
			continue
		}
		num := strings.TrimSpace(cells[0].TextContent())
		parts := strings.Split(num, "x")
		if len(parts) != 2 {
			continue
		}
		if e, _ := strconv.Atoi(strings.TrimSpace(parts[1])); e != v.Episode {
			continue
		}
		if a, _ := dom.QuerySelector(cells[1], "a"); a != nil {
			href, _ := a.Attr("href")
			if m := tvsIDRe.FindStringSubmatch(href); m != nil {
				episodeID, _ = strconv.Atoi(m[1])
			}
		}
	}
	if episodeID == 0 {
		return nil, nil
	}
	page, err = get(ctx, t.base()+fmt.Sprintf("episode-%d.html", episodeID), hdr)
	if err != nil || page == nil {
		return nil, err
	}
	var out []Candidate
	subs, _ := dom.QuerySelectorAll(dom.Parse(string(page)), ".subtitlen")
	for _, s := range subs {
		link := s.ParentElement()
		if link == nil || link.Tag != "a" {
			continue
		}
		href, _ := link.Attr("href")
		m := tvsIDRe.FindStringSubmatch(href)
		if m == nil {
			continue
		}
		lang := ""
		if img, _ := dom.QuerySelector(s, "h5 img"); img != nil {
			src, _ := img.Attr("src")
			if f := tvsFlagRe.FindStringSubmatch(src); f != nil {
				lang = f[1]
				if mapped, ok := tvsLanguages[lang]; ok {
					lang = mapped
				}
			}
		}
		if !wanted(langs, lang) {
			continue
		}
		release := ""
		if h5, _ := dom.QuerySelector(s, "h5"); h5 != nil {
			release = strings.TrimSpace(h5.TextContent())
		}
		if rip, _ := dom.QuerySelector(s, `p[title="rip"]`); rip != nil {
			release += " " + strings.TrimSpace(rip.TextContent())
		}
		out = append(out, Candidate{Provider: t.Name(), ID: m[1], Release: episodeRelease(v, release), Language: lang,
			EpisodeMatch: true, ref: m[1]})
	}
	return out, nil
}

// Download implements Provider.
func (t *TVSubtitles) Download(ctx context.Context, c Candidate) ([]byte, error) {
	return get(ctx, t.base()+"download-"+c.ref+".html", map[string]string{"User-Agent": browserUA})
}

// ----------------------------------------------------------- BetaSeries

// BetaSeries is the French series site (French and original-language
// subtitles), with a free API key.
type BetaSeries struct {
	Key    string
	server string
}

// Name implements Provider.
func (b *BetaSeries) Name() string { return "betaseries" }

// SupportsEpisodes implements the series capability.
func (b *BetaSeries) SupportsEpisodes() bool { return true }

func (b *BetaSeries) base() string {
	if b.server != "" {
		return b.server
	}
	return "https://api.betaseries.com/"
}

var errBetaSeriesKey = errors.New("betaseries key")

func (b *BetaSeries) call(ctx context.Context, path string, q url.Values, out any) error {
	q.Set("key", b.Key)
	q.Set("v", "3.0")
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, b.base()+path+"?"+q.Encode(), nil)
	if err != nil {
		return err
	}
	req.Header.Set("User-Agent", userAgent)
	resp, err := httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	if resp.StatusCode == http.StatusBadRequest || resp.StatusCode == http.StatusUnauthorized {
		var e struct {
			Errors []struct {
				Code int `json:"code"`
			} `json:"errors"`
		}
		_ = json.Unmarshal(data, &e)
		for _, x := range e.Errors {
			if x.Code == 1001 {
				return errBetaSeriesKey
			}
		}
		return nil // 4001: unknown show
	}
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("BetaSeries: HTTP %d", resp.StatusCode)
	}
	return json.Unmarshal(data, out)
}

// Search implements Provider.
func (b *BetaSeries) Search(ctx context.Context, v Video, langs []string) ([]Candidate, error) {
	if !v.IsEpisode() || v.TvdbID == 0 {
		return nil, nil
	}
	var r struct {
		Episodes []struct {
			Subtitles []struct {
				ID       int    `json:"id"`
				Language string `json:"language"`
				Source   string `json:"source"`
				File     string `json:"file"`
				URL      string `json:"url"`
			} `json:"subtitles"`
		} `json:"episodes"`
	}
	q := url.Values{"thetvdb_id": {strconv.Itoa(v.TvdbID)}, "season": {strconv.Itoa(v.Season)}, "episode": {strconv.Itoa(v.Episode)}, "subtitles": {"1"}}
	if err := b.call(ctx, "shows/episodes", q, &r); err != nil {
		if errors.Is(err, errBetaSeriesKey) {
			return nil, i18n.New("BetaSeries: API key refused")
		}
		return nil, err
	}
	var out []Candidate
	for _, e := range r.Episodes {
		for _, s := range e.Subtitles {
			lang := map[string]string{"VF": "fr", "VO": "en"}[strings.ToUpper(s.Language)]
			if lang == "" || !wanted(langs, lang) || s.URL == "" || s.Source == "seriessub" {
				continue
			}
			release := s.File
			if parser.ParseEpisodeTitle(release) == nil {
				release = episodeRelease(v, s.File)
			}
			out = append(out, Candidate{Provider: b.Name(), ID: s.URL, Release: release, Language: lang,
				ImdbMatch: true, EpisodeMatch: true, Uploader: s.Source, ref: s.URL})
		}
	}
	return out, nil
}

// Download implements Provider.
func (b *BetaSeries) Download(ctx context.Context, c Candidate) ([]byte, error) {
	return get(ctx, c.ref, nil)
}

// Test checks the key.
func (b *BetaSeries) Test(ctx context.Context) (string, error) {
	var r map[string]any
	if err := b.call(ctx, "shows/display", url.Values{"id": {"1"}}, &r); err != nil {
		if errors.Is(err, errBetaSeriesKey) {
			return "", i18n.New("BetaSeries: API key refused")
		}
		return "", err
	}
	return i18n.T("valid key"), nil
}

// ------------------------------------------------------- your own sites

// LinkParams fills the search link of a site added by the user.
type LinkParams struct {
	Title, Original, Query string
	Year, Season, Episode  int
	ImdbID                 string
	TmdbID, TvdbID         int
	Lang                   string
}

// SearchLink replaces the placeholders of a link template: {query},
// {title}, {original}, {year}, {imdb}, {imdbnum}, {tmdb}, {tvdb},
// {season}, {episode}, {s00e00}, {lang}.
func SearchLink(template string, p LinkParams) string {
	num := func(n int) string {
		if n == 0 {
			return ""
		}
		return strconv.Itoa(n)
	}
	sxe := ""
	if p.Episode > 0 {
		sxe = fmt.Sprintf("S%02dE%02d", p.Season, p.Episode)
	}
	r := strings.NewReplacer(
		"{query}", url.QueryEscape(p.Query), "{title}", url.QueryEscape(p.Title), "{original}", url.QueryEscape(p.Original),
		"{year}", num(p.Year), "{imdb}", p.ImdbID, "{imdbnum}", strings.TrimLeft(strings.TrimPrefix(p.ImdbID, "tt"), "0"),
		"{tmdb}", num(p.TmdbID), "{tvdb}", num(p.TvdbID), "{season}", num(p.Season), "{episode}", num(p.Episode),
		"{s00e00}", sxe, "{lang}", p.Lang)
	return r.Replace(strings.TrimSpace(template))
}

// Link is a site added by the user, opened in the browser.
type Link struct {
	Name string `json:"name"`
	URL  string `json:"url"`
}

// Links returns the search pages of the sites added by the user.
func Links(cfgs []Config, p LinkParams) []Link {
	out := []Link{}
	for _, c := range cfgs {
		if !c.Enabled || c.ID != "custom-link" {
			continue
		}
		u := SearchLink(c.Settings["url"], p)
		if !strings.HasPrefix(u, "http://") && !strings.HasPrefix(u, "https://") {
			continue
		}
		out = append(out, Link{Name: strings.TrimSpace(c.Settings["name"]), URL: u})
	}
	return out
}
