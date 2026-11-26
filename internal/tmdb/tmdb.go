// Package tmdb is a client for The Movie Database API v3, the metadata
// source used by Radarr. It also provides the IMDb identifier of every film,
// which indexers and subtitle providers use for exact matching.
package tmdb

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"cinearr/internal/i18n"

	"cinearr/internal/proxy"
)

// APIBase is the TMDB endpoint (a variable so tests can point it elsewhere).
var APIBase = "https://api.themoviedb.org/3"

// ImageBase is the CDN prefix for posters and backdrops.
const ImageBase = "https://image.tmdb.org/t/p/"

// BuiltinKey is the application key given at build time
// (-ldflags "-X cinearr/internal/tmdb.BuiltinKey=…", from the TMDB_API_KEY
// secret of the release workflow). It is used when the user has not entered
// a key of their own. TMDB limits requests per address, so every user has
// their own share.
var BuiltinKey = ""

// builtinRefused is set when TMDB refuses the built-in key (revoked or
// suspended), and cleared by the next request it accepts.
var builtinRefused atomic.Bool

// BuiltinRefused reports whether TMDB refused the built-in key at the last
// request made with it.
func BuiltinRefused() bool { return builtinRefused.Load() }

// ErrBuiltinRefused is returned when TMDB no longer accepts the built-in key.
var ErrBuiltinRefused error = builtinError{}

type builtinError struct{}

func (builtinError) Error() string {
	return i18n.T("TMDB no longer accepts the key provided with Cinearr (it may have been revoked or suspended). To keep using Cinearr, enter your own free TMDB key in Settings › General.")
}

// Client queries TMDB.
type Client struct {
	key      string
	builtin  bool // key is BuiltinKey
	language string
	http     *http.Client
	mu       sync.Mutex
	cache    map[string]cached
}

type cached struct {
	at   time.Time
	data []byte
}

// New creates a client. key is either a v3 API key or a v4 read token;
// without one, the built-in key is used.
func New(key, language string) *Client {
	if language == "" {
		language = "en-US"
	}
	key = strings.TrimSpace(key)
	builtin := false
	if key == "" && BuiltinKey != "" {
		key, builtin = strings.TrimSpace(BuiltinKey), true
	}
	return &Client{key: key, builtin: builtin, language: language, http: proxy.Client(proxy.Metadata, http.Client{Timeout: 20 * time.Second}), cache: map[string]cached{}}
}

// Configured reports whether a key is set (the user's or the built-in one).
func (c *Client) Configured() bool { return c.key != "" }

// Builtin reports whether the client uses the built-in key.
func (c *Client) Builtin() bool { return c.builtin }

func (c *Client) get(ctx context.Context, path string, q url.Values, out any) error {
	if c.key == "" {
		return i18n.New("TMDB key missing: enter it in Settings")
	}
	if q == nil {
		q = url.Values{}
	}
	if q.Get("language") == "" {
		q.Set("language", c.language)
	}
	bearer := strings.HasPrefix(c.key, "eyJ")
	if !bearer {
		q.Set("api_key", c.key)
	}
	u := APIBase + path + "?" + q.Encode()
	c.mu.Lock()
	if e, ok := c.cache[u]; ok && time.Since(e.at) < 30*time.Minute {
		c.mu.Unlock()
		return json.Unmarshal(e.data, out)
	}
	c.mu.Unlock()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Accept", "application/json")
	if bearer {
		req.Header.Set("Authorization", "Bearer "+c.key)
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return i18n.Errorf("TMDB unreachable: %w", err)
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	if err != nil {
		return err
	}
	switch resp.StatusCode {
	case http.StatusOK:
		if c.builtin {
			builtinRefused.Store(false)
		}
	case http.StatusUnauthorized:
		// invalid, revoked or suspended key (TMDB status codes 3, 7, 10)
		if c.builtin {
			builtinRefused.Store(true)
			return ErrBuiltinRefused
		}
		return i18n.New("TMDB key refused")
	case http.StatusNotFound:
		return i18n.New("not found on TMDB")
	default:
		return fmt.Errorf("TMDB: HTTP %d", resp.StatusCode)
	}
	c.mu.Lock()
	if len(c.cache) > 500 {
		c.cache = map[string]cached{}
	}
	c.cache[u] = cached{time.Now(), data}
	c.mu.Unlock()
	return json.Unmarshal(data, out)
}

// Summary is a search result.
type Summary struct {
	ID               int     `json:"id"`
	Title            string  `json:"title"`
	OriginalTitle    string  `json:"original_title"`
	OriginalLanguage string  `json:"original_language"`
	Overview         string  `json:"overview"`
	ReleaseDate      string  `json:"release_date"`
	PosterPath       string  `json:"poster_path"`
	BackdropPath     string  `json:"backdrop_path"`
	VoteAverage      float64 `json:"vote_average"`
	VoteCount        int     `json:"vote_count"`
	Popularity       float64 `json:"popularity"`
	Year             int     `json:"year"`
}

func (s *Summary) fill() {
	if len(s.ReleaseDate) >= 4 {
		s.Year, _ = strconv.Atoi(s.ReleaseDate[:4])
	}
}

type page struct {
	Results    []Summary `json:"results"`
	Page       int       `json:"page"`
	TotalPages int       `json:"total_pages"`
}

// Search finds films by title (optionally restricted to a year).
func (c *Client) Search(ctx context.Context, query string, year int) ([]Summary, error) {
	list, _, err := c.SearchPage(ctx, query, year, 1)
	return list, err
}

// SearchPage returns one page (20 films) of search results and whether
// more pages exist.
func (c *Client) SearchPage(ctx context.Context, query string, year, pageNum int) ([]Summary, bool, error) {
	q := url.Values{"query": {query}, "include_adult": {"false"}}
	if year > 0 {
		q.Set("year", strconv.Itoa(year))
	}
	return c.list(ctx, "/search/movie", q, pageNum)
}

// Trending returns a page of the films trending this week.
func (c *Client) Trending(ctx context.Context, pageNum int) ([]Summary, bool, error) {
	return c.list(ctx, "/trending/movie/week", nil, pageNum)
}

// Popular returns a page of popular films.
func (c *Client) Popular(ctx context.Context, pageNum int) ([]Summary, bool, error) {
	return c.list(ctx, "/movie/popular", nil, pageNum)
}

func (c *Client) list(ctx context.Context, path string, q url.Values, pageNum int) ([]Summary, bool, error) {
	if q == nil {
		q = url.Values{}
	}
	if pageNum < 1 {
		pageNum = 1
	}
	if pageNum > 500 { // TMDB limit
		return nil, false, nil
	}
	if pageNum > 1 {
		q.Set("page", strconv.Itoa(pageNum))
	}
	var p page
	if err := c.get(ctx, path, q, &p); err != nil {
		return nil, false, err
	}
	for i := range p.Results {
		p.Results[i].fill()
	}
	return p.Results, pageNum < p.TotalPages && pageNum < 500, nil
}

// Released reports whether the film has come out (its release date, the
// first cinema release for most films, is today or earlier).
func (s Summary) Released(now time.Time) bool {
	if len(s.ReleaseDate) < 10 {
		return false
	}
	d, err := time.Parse("2006-01-02", s.ReleaseDate[:10])
	return err == nil && !d.After(now)
}

// Details is the full record of a film.
type Details struct {
	Summary
	ImdbID  string `json:"imdb_id"`
	Runtime int    `json:"runtime"`
	Tagline string `json:"tagline"`
	Status  string `json:"status"`
	Genres  []struct {
		Name string `json:"name"`
	} `json:"genres"`
	AlternativeTitles struct {
		Titles []struct {
			Title   string `json:"title"`
			Country string `json:"iso_3166_1"`
		} `json:"titles"`
	} `json:"alternative_titles"`
	Translations struct {
		Translations []struct {
			Language string `json:"iso_639_1"`
			Country  string `json:"iso_3166_1"`
			Data     struct {
				Title string `json:"title"`
			} `json:"data"`
		} `json:"translations"`
	} `json:"translations"`
	Credits Credits `json:"credits"`
	Videos  struct {
		Results []struct {
			Key  string `json:"key"`
			Site string `json:"site"`
			Type string `json:"type"`
		} `json:"results"`
	} `json:"videos"`
}

// Person is an actor or a crew member, with the path of the photo.
type Person struct {
	ID          int    `json:"id"`
	Name        string `json:"name"`
	Character   string `json:"character,omitempty"`
	Job         string `json:"job,omitempty"`
	ProfilePath string `json:"profile_path,omitempty"`
}

// Credits are the cast and crew of a film or an episode.
type Credits struct {
	Cast       []Person `json:"cast"`
	GuestStars []Person `json:"guest_stars,omitempty"`
	Crew       []Person `json:"crew"`
}

// englishAlternative returns the title known in an English-speaking
// country, used when TMDB has no English translation title.
func englishAlternative(titles map[string]string) string {
	for _, c := range []string{"US", "GB", "CA", "AU"} {
		if t := strings.TrimSpace(titles[c]); t != "" {
			return t
		}
	}
	return ""
}

// Movie returns the details of a film, with alternative titles,
// translations, credits and videos.
func (c *Client) Movie(ctx context.Context, id int) (*Details, error) {
	var d Details
	q := url.Values{"append_to_response": {"alternative_titles,translations,credits,videos"}}
	if err := c.get(ctx, "/movie/"+strconv.Itoa(id), q, &d); err != nil {
		return nil, err
	}
	d.fill()
	return &d, nil
}

// AllTitles returns every known title (local, original, alternative,
// translations), used to match release names.
func (d *Details) AllTitles() []string {
	seen := map[string]bool{}
	var out []string
	add := func(t string) {
		t = strings.TrimSpace(t)
		if t != "" && !seen[strings.ToLower(t)] {
			seen[strings.ToLower(t)] = true
			out = append(out, t)
		}
	}
	add(d.Title)
	add(d.OriginalTitle)
	for _, t := range d.AlternativeTitles.Titles {
		add(t.Title)
	}
	for _, t := range d.Translations.Translations {
		add(t.Data.Title)
	}
	return out
}

// EnglishTitle returns the English title (the one most trackers use).
func (d *Details) EnglishTitle() string {
	best := ""
	for _, t := range d.Translations.Translations {
		if t.Language != "en" || strings.TrimSpace(t.Data.Title) == "" {
			continue
		}
		if t.Country == "US" {
			return t.Data.Title
		}
		if best == "" {
			best = t.Data.Title
		}
	}
	if best == "" && d.OriginalLanguage == "en" {
		best = d.OriginalTitle
	}
	if best == "" {
		// "La vie de Jésus" is "The Life of Jesus" in the alternative titles
		alt := map[string]string{}
		for _, t := range d.AlternativeTitles.Titles {
			if _, ok := alt[t.Country]; !ok {
				alt[t.Country] = t.Title
			}
		}
		best = englishAlternative(alt)
	}
	return best
}

// FindByImdb resolves an IMDb id (tt...) to a TMDB film.
func (c *Client) FindByImdb(ctx context.Context, imdb string) (*Summary, error) {
	var r struct {
		MovieResults []Summary `json:"movie_results"`
	}
	if err := c.get(ctx, "/find/"+url.PathEscape(imdb), url.Values{"external_source": {"imdb_id"}}, &r); err != nil {
		return nil, err
	}
	if len(r.MovieResults) == 0 {
		return nil, i18n.Errorf("no film for %s", imdb)
	}
	r.MovieResults[0].fill()
	return &r.MovieResults[0], nil
}
