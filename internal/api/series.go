package api

import (
	"context"
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"time"

	"cinearr/internal/diag"
	"cinearr/internal/i18n"
	"cinearr/internal/model"
	"cinearr/internal/proxy"
	"cinearr/internal/subtitles"
)

type handle func(pattern string, fn func(w http.ResponseWriter, r *http.Request) (any, error))

// seriesSummary is the light view of a series polled by the interface.
type seriesSummary struct {
	TmdbID        int                    `json:"tmdbId"`
	Title         string                 `json:"title"`
	OriginalTitle string                 `json:"originalTitle,omitempty"`
	Year          int                    `json:"year"`
	PosterPath    string                 `json:"posterPath,omitempty"`
	Network       string                 `json:"network,omitempty"`
	State         string                 `json:"state,omitempty"`
	Added         time.Time              `json:"added"`
	Seasons       int                    `json:"seasons"`
	Episodes      int                    `json:"episodes"` // aired, specials excluded
	Available     int                    `json:"available"`
	Downloading   int                    `json:"downloading"`
	Wanted        int                    `json:"wanted"`
	Failed        int                    `json:"failed"`
	Downloads     []model.SeriesDownload `json:"downloads"`
}

func summarize(s model.Series) seriesSummary {
	out := seriesSummary{TmdbID: s.TmdbID, Title: s.Title, OriginalTitle: s.OriginalTitle, Year: s.Year, PosterPath: s.PosterPath,
		Network: s.Network, State: s.State, Added: s.Added, Downloads: s.Downloads}
	if out.Downloads == nil {
		out.Downloads = []model.SeriesDownload{}
	}
	now := time.Now()
	for _, se := range s.Seasons {
		if se.Number > 0 {
			out.Seasons++
		}
		for _, e := range se.Episodes {
			if se.Number > 0 && e.Aired(now) {
				out.Episodes++
			}
			switch {
			case e.File != nil:
				out.Available++
			case e.Status == model.StatusDownloading || e.Status == model.StatusImporting:
				out.Downloading++
			case e.Status == model.StatusWanted:
				out.Wanted++
			case e.Status == model.StatusFailed:
				out.Failed++
			}
		}
	}
	return out
}

func episodeArgs(r *http.Request) (int, int) {
	season, _ := strconv.Atoi(r.URL.Query().Get("season"))
	episode, _ := strconv.Atoi(r.URL.Query().Get("episode"))
	return season, episode
}

func (s *Server) seriesRoutes(h handle) {
	h("GET /api/series", func(w http.ResponseWriter, r *http.Request) (any, error) {
		list := s.App.Store.AllSeries()
		out := make([]seriesSummary, 0, len(list))
		for _, x := range list {
			out = append(out, summarize(x))
		}
		return out, nil
	})
	h("POST /api/series", func(w http.ResponseWriter, r *http.Request) (any, error) {
		var v struct {
			TmdbID int `json:"tmdbId"`
		}
		if err := json.NewDecoder(r.Body).Decode(&v); err != nil {
			return nil, err
		}
		ctx, cancel := context.WithTimeout(r.Context(), 2*time.Minute)
		defer cancel()
		x, err := s.App.AddSeries(ctx, v.TmdbID)
		if err != nil {
			return nil, err
		}
		return summarize(x), nil
	})
	h("GET /api/series/{id}", s.withID(func(ctx context.Context, id int, r *http.Request) (any, error) {
		x, ok := s.App.Store.Series(id)
		if !ok {
			return nil, i18n.New("series not found")
		}
		return x, nil
	}))
	h("DELETE /api/series/{id}", s.withID(func(ctx context.Context, id int, r *http.Request) (any, error) {
		return nil, s.App.DeleteSeries(ctx, id, r.URL.Query().Get("files") == "1")
	}))
	h("POST /api/series/{id}/refresh", s.withID(func(ctx context.Context, id int, r *http.Request) (any, error) {
		ctx, cancel := context.WithTimeout(ctx, 2*time.Minute)
		defer cancel()
		return nil, s.App.RefreshSeries(ctx, id)
	}))
	h("POST /api/series/{id}/autosearch", s.withID(func(_ context.Context, id int, r *http.Request) (any, error) {
		var v struct {
			Episodes []model.EpisodeKey `json:"episodes"`
		}
		if err := json.NewDecoder(r.Body).Decode(&v); err != nil {
			return nil, err
		}
		if len(v.Episodes) == 0 {
			return nil, i18n.New("select at least one episode")
		}
		if _, ok := s.App.Store.Series(id); !ok {
			return nil, i18n.New("series not found")
		}
		diag.Go("series automatic search", func() {
			ctx, cancel := context.WithTimeout(context.Background(), 15*time.Minute)
			defer cancel()
			if err := s.App.AutoSearchEpisodes(ctx, id, v.Episodes); err != nil {
				msg := err.Error()
				s.App.Store.UpdateSeries(id, func(x *model.Series) {
					for _, k := range v.Episodes {
						// "no acceptable release" is already set per episode
						if e := x.Episode(k.Season, k.Episode); e != nil && e.DownloadHash == "" && e.StatusMessage == "" {
							e.StatusMessage = msg
						}
					}
				})
			}
		})
		return nil, nil
	}))
	h("POST /api/series/{id}/search", s.withID(func(ctx context.Context, id int, r *http.Request) (any, error) {
		season, episode := episodeArgs(r)
		ctx, cancel := context.WithTimeout(ctx, 2*time.Minute)
		defer cancel()
		return s.App.SearchEpisodeReleases(ctx, id, season, episode)
	}))
	h("POST /api/series/{id}/grab", s.withID(func(ctx context.Context, id int, r *http.Request) (any, error) {
		var v struct {
			Release  model.Release      `json:"release"`
			Episodes []model.EpisodeKey `json:"episodes"`
		}
		if err := json.NewDecoder(r.Body).Decode(&v); err != nil {
			return nil, err
		}
		ctx, cancel := context.WithTimeout(ctx, 90*time.Second)
		defer cancel()
		return nil, s.App.GrabEpisodes(ctx, id, v.Release, v.Episodes)
	}))
	h("POST /api/series/{id}/cancel", s.withID(func(ctx context.Context, id int, r *http.Request) (any, error) {
		return nil, s.App.CancelSeriesDownload(ctx, id, r.URL.Query().Get("hash"), r.URL.Query().Get("blocklist") == "1")
	}))
	h("POST /api/series/{id}/play", s.withID(func(ctx context.Context, id int, r *http.Request) (any, error) {
		season, episode := episodeArgs(r)
		return nil, s.App.PlayEpisode(id, season, episode, r.URL.Query().Get("live") == "1")
	}))
	h("POST /api/series/{id}/open-folder", s.withID(func(ctx context.Context, id int, r *http.Request) (any, error) {
		x, ok := s.App.Store.Series(id)
		if !ok {
			return nil, i18n.New("series not found")
		}
		season, episode := episodeArgs(r)
		if e := x.Episode(season, episode); e != nil && e.File != nil {
			return nil, openFolder(filepath.Dir(e.File.Path))
		}
		for _, se := range x.Seasons {
			for _, e := range se.Episodes {
				if e.File != nil {
					return nil, openFolder(filepath.Dir(filepath.Dir(e.File.Path)))
				}
			}
		}
		return nil, i18n.New("no file")
	}))
	h("GET /api/series/{id}/subtitles", s.withID(func(ctx context.Context, id int, r *http.Request) (any, error) {
		season, episode := episodeArgs(r)
		ctx, cancel := context.WithTimeout(ctx, time.Minute)
		defer cancel()
		cs, errs, err := s.App.SearchEpisodeSubtitles(ctx, id, season, episode)
		return s.subtitleResult(cs, errs, err, s.App.EpisodeSubtitleLinks(id, season, episode))
	}))
	h("POST /api/series/{id}/subtitles/import", s.withID(func(ctx context.Context, id int, r *http.Request) (any, error) {
		season, episode := episodeArgs(r)
		data, err := readUpload(r)
		if err != nil {
			return nil, err
		}
		return nil, s.App.ImportEpisodeSubtitle(id, season, episode, data, r.URL.Query().Get("name"), r.URL.Query().Get("lang"))
	}))
	// details shown when an episode is unfolded: cast and crew (TMDB) and
	// the search pages of the sites added by the user
	h("GET /api/series/{id}/episode", s.withID(func(ctx context.Context, id int, r *http.Request) (any, error) {
		season, episode := episodeArgs(r)
		out := map[string]any{"links": s.App.EpisodeSubtitleLinks(id, season, episode)}
		cr, err := s.App.TMDB().EpisodeCredits(ctx, id, season, episode)
		if err == nil {
			out["credits"] = cr
		} else {
			out["creditsError"] = err.Error()
		}
		return out, nil
	}))
	h("POST /api/series/{id}/subtitles", s.withID(func(ctx context.Context, id int, r *http.Request) (any, error) {
		season, episode := episodeArgs(r)
		var c subtitles.Candidate
		if err := json.NewDecoder(r.Body).Decode(&c); err != nil {
			return nil, err
		}
		ctx, cancel := context.WithTimeout(ctx, time.Minute)
		defer cancel()
		return nil, s.App.DownloadEpisodeSubtitle(ctx, id, season, episode, c)
	}))
	// library scan (also run at each launch)
	h("GET /api/library/scan", func(w http.ResponseWriter, r *http.Request) (any, error) { return s.App.LastScan(), nil })
	h("POST /api/library/scan", func(w http.ResponseWriter, r *http.Request) (any, error) {
		return map[string]bool{"started": s.App.StartScan()}, nil
	})
	h("GET /api/tmdb/tv/{id}", s.withID(func(ctx context.Context, id int, r *http.Request) (any, error) {
		return s.App.TMDB().TV(ctx, id)
	}))
	h("POST /api/reset", func(w http.ResponseWriter, r *http.Request) (any, error) {
		var v struct {
			Players bool `json:"players"`
		}
		if err := json.NewDecoder(r.Body).Decode(&v); err != nil {
			return nil, err
		}
		var indexers []string
		for _, c := range s.App.Store.Settings().Indexers {
			indexers = append(indexers, c.ID)
		}
		if err := s.App.Store.Reset(); err != nil {
			return nil, err
		}
		s.App.ForgetState(indexers)
		proxy.Set(s.App.Store.Settings().Proxy)
		if v.Players {
			_ = os.RemoveAll(filepath.Join(s.App.Store.Dir(), "players"))
		}
		i18n.SetLanguage("")
		diag.Printf("[app] configuration reset (library emptied, players removed: %v)", v.Players)
		return nil, nil
	})
}
