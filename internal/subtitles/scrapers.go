package subtitles

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"

	"cinearr/internal/dom"
	"cinearr/internal/i18n"
)

// browserUA is sent to sites that refuse unknown clients.
const browserUA = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/131.0 Safari/537.36"

// get performs a GET with headers and returns the body (404 = nil, nil).
func get(ctx context.Context, u string, headers map[string]string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", userAgent)
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	resp, err := httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusNotFound {
		return nil, nil
	}
	if resp.StatusCode == http.StatusTooManyRequests {
		return nil, i18n.Errorf("%s: too many requests, try again later", req.URL.Host)
	}
	if resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden {
		return nil, i18n.Errorf("%s: access refused (HTTP %d), check the API key", req.URL.Host, resp.StatusCode)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("%s: HTTP %d", req.URL.Host, resp.StatusCode)
	}
	return io.ReadAll(io.LimitReader(resp.Body, 20<<20))
}

// languageNames maps English language names used by sites to ISO 639-1.
var languageNames = map[string]string{
	"english": "en", "french": "fr", "spanish": "es", "italian": "it", "german": "de", "portuguese": "pt",
	"brazilian portuguese": "pt-br", "brazilian_portuguese": "pt-br", "dutch": "nl", "arabic": "ar", "russian": "ru",
	"polish": "pl", "turkish": "tr", "japanese": "ja", "korean": "ko", "chinese": "zh", "greek": "el", "hebrew": "he",
	"swedish": "sv", "danish": "da", "norwegian": "no", "finnish": "fi", "czech": "cs", "hungarian": "hu",
	"romanian": "ro", "bulgarian": "bg", "croatian": "hr", "serbian": "sr", "slovenian": "sl", "ukrainian": "uk",
	"vietnamese": "vi", "thai": "th", "indonesian": "id", "malay": "ms", "farsi/persian": "fa", "persian": "fa",
}

func wanted(langs []string, code string) bool {
	for _, l := range langs {
		if strings.EqualFold(l, code) {
			return true
		}
	}
	return false
}

// ---------------------------------------------------------------- YIFY

// YIFY scrapes yifysubtitles.ch (port of Bazarr's yifysubtitles provider):
// films only, found by IMDb id, no account.
type YIFY struct {
	server string
}

// Name implements Provider.
func (y *YIFY) Name() string { return "yifysubtitles" }

func (y *YIFY) base() string {
	if y.server != "" {
		return y.server
	}
	return "https://yifysubtitles.ch"
}

var spaces = regexp.MustCompile(`\s+`)

// Search implements Provider.
func (y *YIFY) Search(ctx context.Context, v Video, langs []string) ([]Candidate, error) {
	if v.ImdbID == "" {
		return nil, nil
	}
	body, err := get(ctx, y.base()+"/movie-imdb/"+v.ImdbID, map[string]string{"User-Agent": browserUA, "Referer": y.base(),
		"Accept": "text/html,application/xhtml+xml,application/xml;q=0.9,*/*;q=0.8"})
	if err != nil || body == nil {
		return nil, err
	}
	doc := dom.Parse(string(body))
	rows, _ := dom.QuerySelectorAll(doc, "table.other-subs tbody tr")
	var out []Candidate
	for _, row := range rows {
		td, _ := dom.QuerySelectorAll(row, "td")
		if len(td) < 5 {
			continue
		}
		lang := languageNames[strings.ToLower(strings.TrimSpace(td[1].TextContent()))]
		if lang == "" || !wanted(langs, lang) {
			continue
		}
		a, _ := dom.QuerySelector(td[2], "a")
		if a == nil {
			continue
		}
		href, _ := a.Attr("href")
		link := resolve(y.base(), href)
		release := strings.TrimSpace(td[2].TextContent())
		release = strings.TrimSpace(strings.TrimPrefix(release, "subtitle"))
		release = spaces.ReplaceAllString(release, " ")
		hi, _ := dom.QuerySelector(td[3], "span.hi-subtitle")
		rating, _ := strconv.Atoi(strings.TrimSpace(td[0].TextContent()))
		out = append(out, Candidate{Provider: y.Name(), ID: link, Release: release, Language: lang, HI: hi != nil,
			ImdbMatch: true, Downloads: rating, Uploader: strings.TrimSpace(td[4].TextContent()), ref: link})
	}
	return out, nil
}

// Download implements Provider: the page holds the link to the archive.
func (y *YIFY) Download(ctx context.Context, c Candidate) ([]byte, error) {
	page, err := get(ctx, c.ref, map[string]string{"User-Agent": browserUA, "Referer": c.ref})
	if err != nil {
		return nil, err
	}
	a, _ := dom.QuerySelector(dom.Parse(string(page)), "a.download-subtitle")
	if a == nil {
		return nil, i18n.New("YIFY Subtitles: download link not found on the page")
	}
	href, _ := a.Attr("href")
	return get(ctx, resolve(y.base(), href), map[string]string{"User-Agent": browserUA, "Referer": c.ref})
}

// Test implements Tester.
func (y *YIFY) Test(ctx context.Context) (string, error) {
	cs, err := y.Search(ctx, Video{ImdbID: "tt1375666"}, []string{"en"})
	if err != nil {
		return "", err
	}
	if len(cs) == 0 {
		return "", i18n.New("the site answered but returned no subtitles for a well-known film: it may have changed")
	}
	return i18n.T("site reachable"), nil
}

func resolve(base, href string) string {
	b, err := url.Parse(base)
	if err != nil {
		return href
	}
	r, err := b.Parse(href)
	if err != nil {
		return href
	}
	return r.String()
}

// ---------------------------------------------------------------- Subsource

// Subsource uses the api.subsource.net API (port of Bazarr's subsource
// provider); a free API key is required.
type Subsource struct {
	APIKey string
	server string
}

// Name implements Provider.
func (s *Subsource) Name() string { return "subsource" }

func (s *Subsource) api() string {
	if s.server != "" {
		return s.server
	}
	return "https://api.subsource.net/api/v1/"
}

var subsourceNames = map[string]string{
	"en": "english", "fr": "french", "es": "spanish", "it": "italian", "de": "german", "pt": "portuguese",
	"pt-br": "brazilian_portuguese", "nl": "dutch", "ar": "arabic", "ru": "russian", "pl": "polish", "tr": "turkish",
	"el": "greek", "he": "hebrew", "sv": "swedish", "da": "danish", "no": "norwegian", "fi": "finnish", "cs": "czech",
	"hu": "hungarian", "ro": "romanian", "bg": "bulgarian", "hr": "croatian", "sr": "serbian", "ko": "korean",
	"ja": "japanese", "vi": "vietnamese", "id": "indonesian",
}

func (s *Subsource) getJSON(ctx context.Context, path string, q url.Values, out any) error {
	q.Set("api_key", s.APIKey)
	body, err := get(ctx, s.api()+path+"?"+q.Encode(), nil)
	if err != nil {
		return err
	}
	if body == nil {
		return nil
	}
	return json.Unmarshal(body, out)
}

// movieID finds the Subsource id of the film (by IMDb id, then by title).
func (s *Subsource) movieID(ctx context.Context, v Video) (int, error) {
	type result struct {
		MovieID        int    `json:"movieId"`
		Title          string `json:"title"`
		AlternateTitle string `json:"alternateTitle"`
		ReleaseYear    any    `json:"releaseYear"`
	}
	var titles []string
	for _, t := range v.Titles() {
		titles = append(titles, strings.ToLower(t))
	}
	pick := func(list []result, byImdb bool) int {
		for _, r := range list {
			if byImdb && len(list) == 1 {
				return r.MovieID
			}
			names := []string{strings.ToLower(r.Title), strings.ToLower(r.AlternateTitle)}
			match := false
			for _, t := range titles {
				for _, n := range names {
					if n != "" && strings.Contains(n, t) {
						match = true
					}
				}
			}
			year, _ := strconv.Atoi(fmt.Sprint(r.ReleaseYear))
			if match && (v.Year == 0 || year == 0 || year == v.Year) {
				return r.MovieID
			}
		}
		return 0
	}
	if v.ImdbID != "" {
		var r struct {
			Data []result `json:"data"`
		}
		if err := s.getJSON(ctx, "movies/search", url.Values{"searchType": {"imdb"}, "imdb": {v.ImdbID}}, &r); err != nil {
			return 0, err
		}
		if id := pick(r.Data, true); id != 0 {
			return id, nil
		}
	}
	for _, t := range v.Titles() {
		var r struct {
			Data []result `json:"data"`
		}
		if err := s.getJSON(ctx, "movies/search", url.Values{"searchType": {"text"}, "q": {strings.ToLower(t)}}, &r); err != nil {
			return 0, err
		}
		if id := pick(r.Data, false); id != 0 {
			return id, nil
		}
	}
	return 0, nil
}

// Search implements Provider.
func (s *Subsource) Search(ctx context.Context, v Video, langs []string) ([]Candidate, error) {
	if s.APIKey == "" {
		return nil, i18n.New("Subsource: API key missing")
	}
	id, err := s.movieID(ctx, v)
	if err != nil || id == 0 {
		return nil, err
	}
	var out []Candidate
	for _, l := range langs {
		name := subsourceNames[strings.ToLower(l)]
		if name == "" {
			continue
		}
		var r struct {
			Success *bool `json:"success"`
			Data    []struct {
				SubtitleID      json.Number     `json:"subtitleId"`
				ReleaseInfo     json.RawMessage `json:"releaseInfo"`
				HearingImpaired bool            `json:"hearingImpaired"`
				ForeignParts    bool            `json:"foreignParts"`
				Commentary      string          `json:"commentary"`
				UploaderID      json.Number     `json:"uploaderId"`
				Contributors    []struct {
					ID          json.Number `json:"id"`
					DisplayName string      `json:"displayname"`
				} `json:"contributors"`
			} `json:"data"`
		}
		q := url.Values{"language": {name}, "limit": {"100"}, "movieId": {strconv.Itoa(id)}}
		if err := s.getJSON(ctx, "subtitles", q, &r); err != nil {
			return out, err
		}
		for _, it := range r.Data {
			var releases []string
			if json.Unmarshal(it.ReleaseInfo, &releases) != nil {
				var one string
				_ = json.Unmarshal(it.ReleaseInfo, &one)
				releases = []string{one}
			}
			uploader := ""
			for _, c := range it.Contributors {
				if c.ID == it.UploaderID {
					uploader = c.DisplayName
				}
			}
			sid := it.SubtitleID.String()
			out = append(out, Candidate{Provider: s.Name(), ID: sid, Release: strings.Join(releases, " "), Language: strings.ToLower(l),
				HI: it.HearingImpaired, Forced: it.ForeignParts, ImdbMatch: v.ImdbID != "", Uploader: uploader, ref: sid})
		}
	}
	return out, nil
}

// Download implements Provider.
func (s *Subsource) Download(ctx context.Context, c Candidate) ([]byte, error) {
	b, err := get(ctx, s.api()+"subtitles/"+url.PathEscape(c.ref)+"/download?api_key="+url.QueryEscape(s.APIKey), nil)
	if err == nil && b == nil {
		err = i18n.New("Subsource: subtitle not found")
	}
	return b, err
}

// Test implements Tester.
func (s *Subsource) Test(ctx context.Context) (string, error) {
	var r struct {
		Data []json.RawMessage `json:"data"`
	}
	if err := s.getJSON(ctx, "movies/search", url.Values{"searchType": {"imdb"}, "imdb": {"tt1375666"}}, &r); err != nil {
		return "", err
	}
	return i18n.T("valid key"), nil
}

// ---------------------------------------------------------------- Subsynchro

// Subsynchro queries subsynchro.com (port of Bazarr's subsynchro
// provider): French subtitles for films, by title and year.
type Subsynchro struct {
	server string
}

// Name implements Provider.
func (s *Subsynchro) Name() string { return "subsynchro" }

func (s *Subsynchro) base() string {
	if s.server != "" {
		return s.server
	}
	return "https://www.subsynchro.com"
}

// Search implements Provider.
func (s *Subsynchro) Search(ctx context.Context, v Video, langs []string) ([]Candidate, error) {
	if !wanted(langs, "fr") {
		return nil, nil
	}
	seen := map[string]bool{}
	var out []Candidate
	for _, title := range v.Titles() {
		q := url.Values{"title": {title}}
		if v.Year > 0 {
			q.Set("year", strconv.Itoa(v.Year))
		}
		body, err := get(ctx, s.base()+"/include/ajax/subMarin.php?"+q.Encode(), map[string]string{"User-Agent": browserUA, "Referer": s.base()})
		if err != nil {
			return out, err
		}
		var r struct {
			Status int `json:"status"`
			Data   []struct {
				Titre         string `json:"titre"`
				TitreOriginal string `json:"titre_original"`
				Release       string `json:"release"`
				Filename      string `json:"filename"`
				Download      string `json:"telechargement"`
			} `json:"data"`
		}
		if json.Unmarshal(body, &r) != nil || r.Status != 200 {
			continue
		}
		for _, it := range r.Data {
			if it.Download == "" || seen[it.Download] {
				continue
			}
			seen[it.Download] = true
			release := it.Release
			if release == "" || release == "n/a" {
				release = it.Filename
			}
			out = append(out, Candidate{Provider: s.Name(), ID: it.Download, Release: release, Language: "fr", ref: resolve(s.base(), it.Download)})
		}
		if len(out) > 0 {
			break
		}
	}
	return out, nil
}

// Download implements Provider.
func (s *Subsynchro) Download(ctx context.Context, c Candidate) ([]byte, error) {
	return get(ctx, resolve(s.base(), c.ref), map[string]string{"User-Agent": browserUA, "Referer": s.base()})
}

// Test implements Tester.
func (s *Subsynchro) Test(ctx context.Context) (string, error) {
	if _, err := get(ctx, s.base()+"/include/ajax/subMarin.php?title=Inception&year=2010", map[string]string{"User-Agent": browserUA, "Referer": s.base()}); err != nil {
		return "", err
	}
	return i18n.T("site reachable"), nil
}
