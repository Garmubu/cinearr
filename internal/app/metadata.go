package app

import (
	"context"
	"os"
	"path/filepath"
	"time"

	"cinearr/internal/diag"
	"cinearr/internal/model"
)

// MetadataReport follows the reload of the film and series pages (titles,
// summaries, genres, episode names) after a change of their language.
type MetadataReport struct {
	Running  bool   `json:"running"`
	Done     int    `json:"done"`
	Total    int    `json:"total"`
	Language string `json:"language"`
}

// MetadataStatus returns the state of the reload.
func (a *App) MetadataStatus() MetadataReport {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.meta
}

// StartMetadataRefresh reloads, in the background, every film and series of
// the library from TMDB in the language of the settings. Asked again while
// running, it starts over once the current pass ends.
func (a *App) StartMetadataRefresh() {
	a.mu.Lock()
	if a.meta.Running {
		a.metaAgain = true
		a.mu.Unlock()
		return
	}
	a.meta = MetadataReport{Running: true, Language: a.Store.Settings().Language}
	a.mu.Unlock()
	diag.Go("metadata refresh", func() {
		for {
			a.refreshMetadata()
			a.mu.Lock()
			again := a.metaAgain
			a.metaAgain = false
			if !again {
				a.meta.Running = false
				a.mu.Unlock()
				return
			}
			a.meta = MetadataReport{Running: true, Language: a.Store.Settings().Language}
			a.mu.Unlock()
		}
	})
}

func (a *App) refreshMetadata() {
	movies, series := a.Store.Movies(), a.Store.AllSeries()
	a.mu.Lock()
	a.meta.Total = len(movies) + len(series)
	a.mu.Unlock()
	ok := 0
	step := func(err error, what string) {
		if err != nil {
			a.Log.Printf("[metadata] %s: %v", what, err)
		} else {
			ok++
		}
		a.mu.Lock()
		a.meta.Done++
		a.mu.Unlock()
	}
	for _, m := range movies {
		ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
		step(a.RefreshMovie(ctx, m.TmdbID), m.Title)
		cancel()
	}
	for _, s := range series {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
		step(a.RefreshSeries(ctx, s.TmdbID), s.Title)
		cancel()
	}
	if total := len(movies) + len(series); total > 0 {
		a.record(model.HistoryEvent{Kind: "refresh"}, "Film and series pages reloaded in %s: %d of %d", a.MetadataStatus().Language, ok, total)
	}
}

// postersMarker records that the posters of the library are in the original
// language of each title (localised posters were kept before 0.4.2).
const postersMarker = "posters-original"

// originalPosters gives the films and series of the library the poster of
// their original language, once; a title TMDB could not answer for keeps
// its poster until the next refresh, and the pass is tried again at the
// next launch.
func (a *App) originalPosters() {
	marker := filepath.Join(a.Store.Dir(), postersMarker)
	if _, err := os.Stat(marker); err == nil {
		return
	}
	c := a.TMDB()
	if !c.Configured() {
		return
	}
	failed := 0
	poster := func(tv bool, id int, original, current string) string {
		if original == "" {
			return current
		}
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		p, err := c.LookupOriginalPoster(ctx, tv, id, original)
		if err != nil {
			failed++
			return current
		}
		if p == "" {
			return current
		}
		return p
	}
	for _, m := range a.Store.Movies() {
		if p := poster(false, m.TmdbID, m.OriginalLanguage, m.PosterPath); p != m.PosterPath {
			a.Store.UpdateMovie(m.TmdbID, func(x *model.Movie) { x.PosterPath = p })
		}
	}
	for _, s := range a.Store.AllSeries() {
		if p := poster(true, s.TmdbID, s.OriginalLanguage, s.PosterPath); p != s.PosterPath {
			a.Store.UpdateSeries(s.TmdbID, func(x *model.Series) { x.PosterPath = p })
		}
	}
	if failed == 0 {
		_ = os.WriteFile(marker, []byte("1\n"), 0o644)
	}
}
