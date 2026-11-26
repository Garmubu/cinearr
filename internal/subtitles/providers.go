package subtitles

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"cinearr/internal/backoff"
	"cinearr/internal/diag"
	"cinearr/internal/i18n"
	"cinearr/internal/proxy"
)

const userAgent = "Cinearr v1.0"

var httpClient = proxy.Client(proxy.Metadata, http.Client{Timeout: 30 * time.Second})

// ---------------------------------------------------------------------------
// OpenSubtitles.com (REST API) — mirrors Bazarr's opensubtitlescom provider.

// BuiltinOpenSubtitlesKey is the application key given at build time
// (-ldflags "-X cinearr/internal/subtitles.BuiltinOpenSubtitlesKey=…", from
// the OPENSUBTITLES_API_KEY secret of the release workflow). With it, users
// only enter their OpenSubtitles.com username and password, as in Bazarr.
var BuiltinOpenSubtitlesKey = ""

// errOSRefused is returned when OpenSubtitles refuses the request (HTTP 401
// or 403): the account, an expired session or the application key.
var errOSRefused error = osRefused{}

type osRefused struct{}

func (osRefused) Error() string { return i18n.T("OpenSubtitles: API key or credentials refused") }

// errOSKey is returned when OpenSubtitles refuses the application key given
// by the user.
var errOSKey error = osKey{}

type osKey struct{}

func (osKey) Error() string {
	return i18n.T("OpenSubtitles.com: API key refused (check the key copied from opensubtitles.com › API consumers)")
}

// ErrBuiltinKeyRefused is returned when OpenSubtitles no longer accepts the
// built-in application key.
var ErrBuiltinKeyRefused error = osBuiltinKey{}

type osBuiltinKey struct{}

func (osBuiltinKey) Error() string {
	return i18n.T("OpenSubtitles.com no longer accepts the key provided with Cinearr (it may have been revoked or blocked). To keep using it, create your own free key on opensubtitles.com › API consumers and enter it in Settings › Subtitles.")
}

// builtinRefused is set when OpenSubtitles refuses the built-in key, and
// cleared by the next request it accepts.
var builtinRefused atomic.Bool

// BuiltinKeyRefused reports whether OpenSubtitles refused the built-in key
// at the last request made with it.
func BuiltinKeyRefused() bool { return builtinRefused.Load() }

// OpenSubtitles needs an API key (free, opensubtitles.com/consumers);
// username/password raise the daily download quota.
type OpenSubtitles struct {
	APIKey   string
	Username string
	Password string
	// Builtin is set when APIKey is BuiltinOpenSubtitlesKey.
	Builtin bool
	// IncludeAI keeps AI / machine translated subtitles (excluded by default
	// as in Bazarr).
	IncludeAI bool

	mu      sync.Mutex
	token   string
	baseURL string
	// name and fixed are set for another service exposing the same API
	// (custom provider): its address is then never replaced.
	name  string
	fixed bool
}

// Name implements Provider.
func (o *OpenSubtitles) Name() string {
	if o.name != "" {
		return o.name
	}
	return "opensubtitles"
}

func (o *OpenSubtitles) server() string {
	if o.baseURL != "" {
		return o.baseURL
	}
	return "https://api.opensubtitles.com/api/v1/"
}

// request calls the API. When it is refused, the key alone is checked to
// tell a refused application key from a refused account or session.
func (o *OpenSubtitles) request(ctx context.Context, method, path string, q url.Values, body any, out any) error {
	err := o.send(ctx, method, path, q, body, out, true)
	if err == nil {
		if o.Builtin {
			builtinRefused.Store(false)
		}
		return nil
	}
	if !errors.Is(err, errOSRefused) {
		return err
	}
	// an expired session is dropped: the next request logs in again
	o.mu.Lock()
	o.token = ""
	o.mu.Unlock()
	var formats struct {
		Data any `json:"data"`
	}
	if perr := o.send(ctx, http.MethodGet, "infos/formats", nil, nil, &formats, false); errors.Is(perr, errOSRefused) {
		if o.Builtin {
			builtinRefused.Store(true)
			return ErrBuiltinKeyRefused
		}
		return errOSKey
	}
	return err
}

func (o *OpenSubtitles) send(ctx context.Context, method, path string, q url.Values, body any, out any, session bool) error {
	u := o.server() + path
	if len(q) > 0 {
		u += "?" + q.Encode()
	}
	var rd io.Reader
	if body != nil {
		b, _ := json.Marshal(body)
		rd = bytes.NewReader(b)
	}
	req, err := http.NewRequestWithContext(ctx, method, u, rd)
	if err != nil {
		return err
	}
	req.Header.Set("Api-Key", o.APIKey)
	req.Header.Set("User-Agent", userAgent)
	req.Header.Set("Accept", "application/json")
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	o.mu.Lock()
	if session && o.token != "" {
		req.Header.Set("Authorization", "Bearer "+o.token)
	}
	o.mu.Unlock()
	resp, err := httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	if resp.StatusCode == http.StatusTooManyRequests || resp.StatusCode == 406 {
		return i18n.New("OpenSubtitles: download quota reached for today")
	}
	if resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden {
		return errOSRefused
	}
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("OpenSubtitles: HTTP %d", resp.StatusCode)
	}
	return json.Unmarshal(data, out)
}

func (o *OpenSubtitles) login(ctx context.Context) error {
	if o.Username == "" || o.Password == "" {
		return nil
	}
	o.mu.Lock()
	has := o.token != ""
	o.mu.Unlock()
	if has {
		return nil
	}
	var r struct {
		Token   string `json:"token"`
		BaseURL string `json:"base_url"`
	}
	if err := o.request(ctx, http.MethodPost, "login", nil, map[string]string{"username": o.Username, "password": o.Password}, &r); err != nil {
		return err
	}
	o.mu.Lock()
	o.token = r.Token
	if r.BaseURL != "" && !o.fixed && !strings.Contains(r.BaseURL, "api.opensubtitles.com") {
		o.baseURL = "https://" + strings.TrimSuffix(r.BaseURL, "/") + "/api/v1/"
	}
	o.mu.Unlock()
	return nil
}

// Test checks the API key and, when given, the account; it returns a
// short description (level and daily quota).
func (o *OpenSubtitles) Test(ctx context.Context) (string, error) {
	if o.APIKey == "" {
		return "", i18n.New("OpenSubtitles.com: API key missing")
	}
	var formats struct {
		Data any `json:"data"`
	}
	if err := o.request(ctx, http.MethodGet, "infos/formats", nil, nil, &formats); err != nil {
		return "", err
	}
	if o.Username == "" || o.Password == "" {
		return i18n.T("valid key (without account: 5 downloads a day)"), nil
	}
	if err := o.login(ctx); err != nil {
		if errors.Is(err, errOSRefused) {
			return "", i18n.New("OpenSubtitles.com: username or password refused")
		}
		return "", err
	}
	var info struct {
		Data struct {
			Level              string `json:"level"`
			AllowedDownloads   int    `json:"allowed_downloads"`
			RemainingDownloads int    `json:"remaining_downloads"`
			VIP                bool   `json:"vip"`
		} `json:"data"`
	}
	if err := o.request(ctx, http.MethodGet, "infos/user", nil, nil, &info); err != nil {
		return i18n.T("valid account"), nil
	}
	return i18n.Tf("valid account (%s), %d downloads left out of %d today", info.Data.Level,
		info.Data.RemainingDownloads, info.Data.AllowedDownloads), nil
}

// Search implements Provider.
func (o *OpenSubtitles) Search(ctx context.Context, v Video, langs []string) ([]Candidate, error) {
	if o.APIKey == "" {
		return nil, i18n.New("OpenSubtitles.com: API key missing")
	}
	q := url.Values{}
	if v.IsEpisode() {
		// the ids are those of the series (Bazarr: parent_imdb_id)
		switch {
		case v.ImdbID != "":
			n, _ := strconv.Atoi(strings.TrimLeft(strings.TrimPrefix(v.ImdbID, "tt"), "0"))
			q.Set("parent_imdb_id", strconv.Itoa(n))
		case v.TmdbID > 0:
			q.Set("parent_tmdb_id", strconv.Itoa(v.TmdbID))
		default:
			q.Set("query", v.Title)
		}
		q.Set("season_number", strconv.Itoa(v.Season))
		q.Set("episode_number", strconv.Itoa(v.Episode))
	} else if v.ImdbID != "" {
		n, _ := strconv.Atoi(strings.TrimLeft(strings.TrimPrefix(v.ImdbID, "tt"), "0"))
		q.Set("imdb_id", strconv.Itoa(n))
	} else if v.TmdbID > 0 {
		q.Set("tmdb_id", strconv.Itoa(v.TmdbID))
	} else {
		q.Set("query", v.Title)
		if v.Year > 0 {
			q.Set("year", strconv.Itoa(v.Year))
		}
	}
	if v.Hash != "" {
		q.Set("moviehash", v.Hash)
	}
	ls := append([]string{}, langs...)
	sort.Strings(ls)
	q.Set("languages", strings.ToLower(strings.Join(ls, ",")))
	if v.IsEpisode() {
		q.Set("type", "episode")
	} else {
		q.Set("type", "movie")
	}
	if !o.IncludeAI {
		q.Set("ai_translated", "exclude")
	}
	var r struct {
		Data []struct {
			ID         string `json:"id"`
			Attributes struct {
				Language          string `json:"language"`
				Release           string `json:"release"`
				HearingImpaired   bool   `json:"hearing_impaired"`
				ForeignPartsOnly  bool   `json:"foreign_parts_only"`
				MoviehashMatch    bool   `json:"moviehash_match"`
				DownloadCount     int    `json:"download_count"`
				AITranslated      bool   `json:"ai_translated"`
				MachineTranslated bool   `json:"machine_translated"`
				Uploader          struct {
					Name string `json:"name"`
				} `json:"uploader"`
				FeatureDetails struct {
					ImdbID        int `json:"imdb_id"`
					ParentImdbID  int `json:"parent_imdb_id"`
					SeasonNumber  int `json:"season_number"`
					EpisodeNumber int `json:"episode_number"`
				} `json:"feature_details"`
				Files []struct {
					FileID   int    `json:"file_id"`
					FileName string `json:"file_name"`
				} `json:"files"`
			} `json:"attributes"`
		} `json:"data"`
	}
	if err := o.request(ctx, http.MethodGet, "subtitles", q, nil, &r); err != nil {
		return nil, err
	}
	var out []Candidate
	wantImdb, _ := strconv.Atoi(strings.TrimLeft(strings.TrimPrefix(v.ImdbID, "tt"), "0"))
	for _, d := range r.Data {
		a := d.Attributes
		if len(a.Files) == 0 || (!o.IncludeAI && (a.AITranslated || a.MachineTranslated)) {
			continue
		}
		fd := a.FeatureDetails
		if v.IsEpisode() && fd.EpisodeNumber > 0 && (fd.SeasonNumber != v.Season || fd.EpisodeNumber != v.Episode) {
			continue
		}
		imdbMatch := wantImdb > 0 && fd.ImdbID == wantImdb
		if v.IsEpisode() {
			imdbMatch = wantImdb > 0 && fd.ParentImdbID == wantImdb
		}
		c := Candidate{
			Provider: o.Name(), ID: strconv.Itoa(a.Files[0].FileID), Release: a.Release, Language: strings.ToLower(a.Language),
			HI: a.HearingImpaired, Forced: a.ForeignPartsOnly && !a.HearingImpaired, HashMatch: a.MoviehashMatch,
			ImdbMatch: imdbMatch, EpisodeMatch: v.IsEpisode() && fd.EpisodeNumber == v.Episode, Downloads: a.DownloadCount,
			Uploader: a.Uploader.Name, ref: strconv.Itoa(a.Files[0].FileID),
		}
		if c.Release == "" {
			c.Release = a.Files[0].FileName
		}
		out = append(out, c)
	}
	return out, nil
}

// SupportsEpisodes reports that OpenSubtitles.com has series subtitles.
func (o *OpenSubtitles) SupportsEpisodes() bool { return true }

// Download implements Provider.
func (o *OpenSubtitles) Download(ctx context.Context, c Candidate) ([]byte, error) {
	if err := o.login(ctx); err != nil {
		return nil, err
	}
	id, _ := strconv.Atoi(c.ref)
	var r struct {
		Link      string `json:"link"`
		Remaining int    `json:"remaining"`
	}
	if err := o.request(ctx, http.MethodPost, "download", nil, map[string]any{"file_id": id, "sub_format": "srt"}, &r); err != nil {
		return nil, err
	}
	if r.Link == "" {
		return nil, i18n.New("OpenSubtitles: download link missing")
	}
	return fetch(ctx, r.Link)
}

// ---------------------------------------------------------------------------
// SubDL — mirrors Bazarr's subdl provider (free API key on subdl.com).

// SubDL provider.
type SubDL struct {
	APIKey string
}

// Name implements Provider.
func (s *SubDL) Name() string { return "subdl" }

var subdlLang = map[string]string{"fr": "FR", "en": "EN", "es": "ES", "de": "DE", "it": "IT", "pt": "PT", "pt-br": "BR_PT", "ar": "AR", "nl": "NL", "ru": "RU", "ja": "JA", "ko": "KO", "zh": "ZH", "tr": "TR", "pl": "PL"}

// Test checks the SubDL key with a known film.
func (s *SubDL) Test(ctx context.Context) (string, error) {
	if _, err := s.Search(ctx, Video{ImdbID: "tt1375666", Title: "Inception", Year: 2010}, []string{"en"}); err != nil {
		return "", err
	}
	return i18n.T("valid key"), nil
}

// Search implements Provider.
func (s *SubDL) Search(ctx context.Context, v Video, langs []string) ([]Candidate, error) {
	if s.APIKey == "" {
		return nil, i18n.New("SubDL: API key missing")
	}
	var codes []string
	rev := map[string]string{}
	for _, l := range langs {
		if c, ok := subdlLang[strings.ToLower(l)]; ok {
			codes = append(codes, c)
			rev[c] = strings.ToLower(l)
		}
	}
	q := url.Values{"api_key": {s.APIKey}, "type": {"movie"}, "languages": {strings.Join(codes, ",")}, "subs_per_page": {"30"}}
	if v.IsEpisode() {
		q.Set("type", "tv")
		q.Set("season_number", strconv.Itoa(v.Season))
		q.Set("episode_number", strconv.Itoa(v.Episode))
	}
	if v.ImdbID != "" {
		q.Set("imdb_id", v.ImdbID)
	} else if v.TmdbID > 0 {
		q.Set("tmdb_id", strconv.Itoa(v.TmdbID))
	} else {
		q.Set("film_name", v.Title)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "https://api.subdl.com/api/v1/subtitles?"+q.Encode(), nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", userAgent)
	resp, err := httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	var r struct {
		Status    bool   `json:"status"`
		Error     string `json:"error"`
		Subtitles []struct {
			ReleaseName string `json:"release_name"`
			Name        string `json:"name"`
			Lang        string `json:"lang"`
			Language    string `json:"language"`
			Author      string `json:"author"`
			URL         string `json:"url"`
			HI          bool   `json:"hi"`
			Season      int    `json:"season"`
			Episode     int    `json:"episode"`
		} `json:"subtitles"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 8<<20)).Decode(&r); err != nil {
		return nil, i18n.New("SubDL: invalid answer")
	}
	if !r.Status {
		if r.Error != "" {
			return nil, fmt.Errorf("SubDL: %s", r.Error)
		}
		return nil, nil
	}
	var out []Candidate
	for _, sub := range r.Subtitles {
		lang := rev[strings.ToUpper(sub.Language)]
		if lang == "" {
			lang = strings.ToLower(sub.Lang)
			if len(lang) > 2 {
				for k, v := range subdlLang {
					if strings.EqualFold(v, sub.Language) {
						lang = k
					}
				}
			}
		}
		out = append(out, Candidate{Provider: s.Name(), ID: sub.URL, Release: firstNonEmpty(sub.ReleaseName, sub.Name),
			Language: lang, HI: sub.HI, ImdbMatch: v.ImdbID != "", Uploader: sub.Author, ref: sub.URL,
			EpisodeMatch: v.IsEpisode() && sub.Episode == v.Episode && (sub.Season == 0 || sub.Season == v.Season)})
	}
	return out, nil
}

// SupportsEpisodes reports that SubDL has series subtitles.
func (s *SubDL) SupportsEpisodes() bool { return true }

// Download implements Provider.
func (s *SubDL) Download(ctx context.Context, c Candidate) ([]byte, error) {
	u := c.ref
	if !strings.HasPrefix(u, "http") {
		u = "https://dl.subdl.com" + u
	}
	return fetch(ctx, u)
}

func firstNonEmpty(v ...string) string {
	for _, s := range v {
		if strings.TrimSpace(s) != "" {
			return s
		}
	}
	return ""
}

func fetch(ctx context.Context, u string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", userAgent)
	resp, err := httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, i18n.Errorf("subtitle download: HTTP %d", resp.StatusCode)
	}
	return io.ReadAll(io.LimitReader(resp.Body, 20<<20))
}

// SearchAll queries every provider concurrently, scores and ranks results.
// Pauses pauses the providers that keep failing (quota reached, site
// down): automatic searches and downloads skip them, as Bazarr's
// throttled providers.
var Pauses backoff.Book

// Failed records a provider failure (search or download), unless the
// caller cancelled.
func Failed(ctx context.Context, name string, err error) {
	if ctx.Err() == nil {
		st := Pauses.Fail(name, err.Error())
		diag.Printf("[subtitles] %s: %s (failure %d in a row, automatic searches paused until %s)", name,
			diag.RedactText(err.Error()), st.Failures, st.PausedUntil.Format("15:04:05"))
	}
}

// Skipped returns the error of a provider paused for an automatic search
// (nil when it may be used).
func Skipped(ctx context.Context, name string) error {
	if st, paused := Pauses.Paused(name); paused && backoff.IsAutomatic(ctx) {
		return i18n.Errorf("%s: paused until %s after %d failures in a row", name, st.PausedUntil.Format("15:04"), st.Failures)
	}
	return nil
}

func SearchAll(ctx context.Context, providers []Provider, v Video, langs []string, wantHI bool) ([]Candidate, []error) {
	var (
		mu   sync.Mutex
		all  []Candidate
		errs []error
		wg   sync.WaitGroup
	)
	for _, p := range providers {
		if v.IsEpisode() {
			if sc, ok := p.(interface{ SupportsEpisodes() bool }); !ok || !sc.SupportsEpisodes() {
				continue // films only (YIFY, Subsource, Subsynchro)
			}
		}
		if err := Skipped(ctx, p.Name()); err != nil {
			mu.Lock()
			errs = append(errs, err)
			mu.Unlock()
			continue
		}
		wg.Add(1)
		go func(p Provider) {
			defer wg.Done()
			defer diag.Recover("subtitles " + p.Name())
			cs, err := p.Search(ctx, v, langs)
			if err != nil {
				Failed(ctx, p.Name(), err)
			} else {
				Pauses.OK(p.Name())
			}
			mu.Lock()
			defer mu.Unlock()
			if err != nil {
				errs = append(errs, err)
				return
			}
			all = append(all, cs...)
		}(p)
	}
	wg.Wait()
	for i := range all {
		Score(&all[i], v, wantHI)
	}
	Rank(all)
	return all, errs
}

// Find returns the provider named name.
func Find(providers []Provider, name string) Provider {
	for _, p := range providers {
		if p.Name() == name {
			return p
		}
	}
	return nil
}

// WithRef rebuilds a candidate received from the UI: for every provider the
// download reference is the candidate ID (file id or archive path).
func WithRef(c Candidate) Candidate {
	c.ref = c.ID
	return c
}
