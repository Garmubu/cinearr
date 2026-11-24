package tmdb

import (
	"context"
	"net/url"
	"strconv"
	"strings"

	"cinearr/internal/i18n"
)

// tvSummary is a series in TMDB lists; it is converted to Summary (name →
// title, first air date → release date) so films and series share the same
// shape in the interface.
type tvSummary struct {
	ID               int     `json:"id"`
	Name             string  `json:"name"`
	OriginalName     string  `json:"original_name"`
	OriginalLanguage string  `json:"original_language"`
	Overview         string  `json:"overview"`
	FirstAirDate     string  `json:"first_air_date"`
	PosterPath       string  `json:"poster_path"`
	BackdropPath     string  `json:"backdrop_path"`
	VoteAverage      float64 `json:"vote_average"`
	VoteCount        int     `json:"vote_count"`
	Popularity       float64 `json:"popularity"`
}

func (t tvSummary) summary() Summary {
	s := Summary{ID: t.ID, Title: t.Name, OriginalTitle: t.OriginalName, OriginalLanguage: t.OriginalLanguage, Overview: t.Overview,
		ReleaseDate: t.FirstAirDate, PosterPath: t.PosterPath, BackdropPath: t.BackdropPath, VoteAverage: t.VoteAverage,
		VoteCount: t.VoteCount, Popularity: t.Popularity}
	s.fill()
	return s
}

type tvPage struct {
	Results    []tvSummary `json:"results"`
	TotalPages int         `json:"total_pages"`
}

func (c *Client) tvList(ctx context.Context, path string, q url.Values, pageNum int) ([]Summary, bool, error) {
	if q == nil {
		q = url.Values{}
	}
	if pageNum < 1 {
		pageNum = 1
	}
	if pageNum > 500 {
		return nil, false, nil
	}
	if pageNum > 1 {
		q.Set("page", strconv.Itoa(pageNum))
	}
	var p tvPage
	if err := c.get(ctx, path, q, &p); err != nil {
		return nil, false, err
	}
	out := make([]Summary, 0, len(p.Results))
	for _, r := range p.Results {
		out = append(out, r.summary())
	}
	return out, pageNum < p.TotalPages && pageNum < 500, nil
}

// SearchTVPage returns one page of series matching query.
func (c *Client) SearchTVPage(ctx context.Context, query string, year, pageNum int) ([]Summary, bool, error) {
	q := url.Values{"query": {query}, "include_adult": {"false"}}
	if year > 0 {
		q.Set("first_air_date_year", strconv.Itoa(year))
	}
	return c.tvList(ctx, "/search/tv", q, pageNum)
}

// TrendingTV returns a page of the series trending this week.
func (c *Client) TrendingTV(ctx context.Context, pageNum int) ([]Summary, bool, error) {
	return c.tvList(ctx, "/trending/tv/week", nil, pageNum)
}

// PopularTV returns a page of popular series.
func (c *Client) PopularTV(ctx context.Context, pageNum int) ([]Summary, bool, error) {
	return c.tvList(ctx, "/tv/popular", nil, pageNum)
}

// TVDetails is the full record of a series.
type TVDetails struct {
	tvSummary
	Year             int    `json:"year"`
	Status           string `json:"status"`
	EpisodeRunTime   []int  `json:"episode_run_time"`
	NumberOfSeasons  int    `json:"number_of_seasons"`
	NumberOfEpisodes int    `json:"number_of_episodes"`
	Genres           []struct {
		Name string `json:"name"`
	} `json:"genres"`
	Networks []struct {
		Name string `json:"name"`
	} `json:"networks"`
	CreatedBy []struct {
		Name string `json:"name"`
	} `json:"created_by"`
	Seasons []struct {
		SeasonNumber int    `json:"season_number"`
		Name         string `json:"name"`
		Overview     string `json:"overview"`
		AirDate      string `json:"air_date"`
		PosterPath   string `json:"poster_path"`
		EpisodeCount int    `json:"episode_count"`
	} `json:"seasons"`
	ExternalIDs struct {
		ImdbID string `json:"imdb_id"`
		TvdbID int    `json:"tvdb_id"`
	} `json:"external_ids"`
	AlternativeTitles struct {
		Results []struct {
			Title   string `json:"title"`
			Country string `json:"iso_3166_1"`
		} `json:"results"`
	} `json:"alternative_titles"`
	Translations struct {
		Translations []struct {
			Language string `json:"iso_639_1"`
			Country  string `json:"iso_3166_1"`
			Data     struct {
				Name string `json:"name"`
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
	// the same fields as a film summary, for the interface
	Title       string `json:"title"`
	ReleaseDate string `json:"release_date"`
}

// TV returns the details of a series with its external ids, alternative
// titles, translations, cast and videos.
func (c *Client) TV(ctx context.Context, id int) (*TVDetails, error) {
	var d TVDetails
	q := url.Values{"append_to_response": {"external_ids,alternative_titles,translations,credits,videos"}}
	if err := c.get(ctx, "/tv/"+strconv.Itoa(id), q, &d); err != nil {
		return nil, err
	}
	d.Title, d.ReleaseDate = d.Name, d.FirstAirDate
	if len(d.FirstAirDate) >= 4 {
		d.Year, _ = strconv.Atoi(d.FirstAirDate[:4])
	}
	return &d, nil
}

// AllTitles returns every known title of the series.
func (d *TVDetails) AllTitles() []string {
	seen := map[string]bool{}
	var out []string
	add := func(t string) {
		t = strings.TrimSpace(t)
		if t != "" && !seen[strings.ToLower(t)] {
			seen[strings.ToLower(t)] = true
			out = append(out, t)
		}
	}
	add(d.Name)
	add(d.OriginalName)
	for _, t := range d.AlternativeTitles.Results {
		add(t.Title)
	}
	for _, t := range d.Translations.Translations {
		add(t.Data.Name)
	}
	return out
}

// EnglishTitle returns the English title of the series.
func (d *TVDetails) EnglishTitle() string {
	best := ""
	for _, t := range d.Translations.Translations {
		if t.Language != "en" || strings.TrimSpace(t.Data.Name) == "" {
			continue
		}
		if t.Country == "US" {
			return t.Data.Name
		}
		if best == "" {
			best = t.Data.Name
		}
	}
	if best == "" && d.OriginalLanguage == "en" {
		best = d.OriginalName
	}
	if best == "" {
		alt := map[string]string{}
		for _, t := range d.AlternativeTitles.Results {
			if _, ok := alt[t.Country]; !ok {
				alt[t.Country] = t.Title
			}
		}
		best = englishAlternative(alt)
	}
	return best
}

// EpisodeCredits returns the cast (regular and guest stars) and the crew
// of an episode.
func (c *Client) EpisodeCredits(ctx context.Context, id, season, episode int) (*Credits, error) {
	var cr Credits
	path := "/tv/" + strconv.Itoa(id) + "/season/" + strconv.Itoa(season) + "/episode/" + strconv.Itoa(episode) + "/credits"
	if err := c.get(ctx, path, nil, &cr); err != nil {
		return nil, err
	}
	return &cr, nil
}

// Runtime returns the usual episode length in minutes.
func (d *TVDetails) Runtime() int {
	if len(d.EpisodeRunTime) == 0 {
		return 0
	}
	return d.EpisodeRunTime[0]
}

// SeasonDetails lists the episodes of a season.
type SeasonDetails struct {
	SeasonNumber int    `json:"season_number"`
	Name         string `json:"name"`
	Overview     string `json:"overview"`
	AirDate      string `json:"air_date"`
	PosterPath   string `json:"poster_path"`
	Episodes     []struct {
		EpisodeNumber int    `json:"episode_number"`
		SeasonNumber  int    `json:"season_number"`
		Name          string `json:"name"`
		Overview      string `json:"overview"`
		AirDate       string `json:"air_date"`
		Runtime       int    `json:"runtime"`
		StillPath     string `json:"still_path"`
	} `json:"episodes"`
}

// Season returns the episodes of season n of series id.
func (c *Client) Season(ctx context.Context, id, n int) (*SeasonDetails, error) {
	var d SeasonDetails
	if err := c.get(ctx, "/tv/"+strconv.Itoa(id)+"/season/"+strconv.Itoa(n), nil, &d); err != nil {
		return nil, err
	}
	return &d, nil
}

// FindTVByImdb resolves an IMDb id to a TMDB series.
func (c *Client) FindTVByImdb(ctx context.Context, imdb string) (*Summary, error) {
	var r struct {
		TVResults []tvSummary `json:"tv_results"`
	}
	if err := c.get(ctx, "/find/"+url.PathEscape(imdb), url.Values{"external_source": {"imdb_id"}}, &r); err != nil {
		return nil, err
	}
	if len(r.TVResults) == 0 {
		return nil, i18n.Errorf("no series for %s", imdb)
	}
	s := r.TVResults[0].summary()
	return &s, nil
}
