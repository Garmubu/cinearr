package app

import (
	"context"
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
