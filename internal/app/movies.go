package app

import (
	"context"
	"fmt"
	"strings"
	"time"

	"cinearr/internal/backoff"
	"cinearr/internal/decision"
	"cinearr/internal/diag"
	"cinearr/internal/i18n"
	"cinearr/internal/indexer"
	"cinearr/internal/model"
	"cinearr/internal/tmdb"
)

// Films: library, searches, grabs and downloads (Radarr).

// AddMovie adds a film from TMDB; it is searched right away only when
// "search on add" is on.
func (a *App) AddMovie(ctx context.Context, tmdbID int, search bool) (model.Movie, error) {
	if m, ok := a.Store.Movie(tmdbID); ok {
		return m, nil
	}
	d, err := a.TMDB().Movie(ctx, tmdbID)
	if err != nil {
		return model.Movie{}, err
	}
	m := movieFromDetails(d)
	m.Status = model.StatusWanted
	m.Added = time.Now()
	if err := a.Store.PutMovie(m); err != nil {
		return model.Movie{}, err
	}
	a.history(m.TmdbID, "added", "%s (%d) added to the library", m.Title, m.Year)
	if search && a.Store.Settings().SearchOnAdd {
		diag.Go("automatic search", func() {
			ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
			defer cancel()
			if err := a.AutoSearch(ctx, m.TmdbID); err != nil {
				a.history(m.TmdbID, "warning", "%s: %v", m.Title, err)
			}
		})
	}
	return m, nil
}

func movieFromDetails(d *tmdb.Details) model.Movie {
	m := model.Movie{
		TmdbID: d.ID, ImdbID: d.ImdbID, Title: d.Title, OriginalTitle: d.OriginalTitle, EnglishTitle: d.EnglishTitle(),
		OriginalLanguage: d.OriginalLanguage, Year: d.Year, Runtime: d.Runtime, Overview: d.Overview,
		PosterPath: d.PosterPath, BackdropPath: d.BackdropPath, Rating: d.VoteAverage,
	}
	for _, g := range d.Genres {
		m.Genres = append(m.Genres, g.Name)
	}
	for _, t := range d.AllTitles() {
		if t != d.Title && t != d.OriginalTitle {
			m.AlternativeTitles = append(m.AlternativeTitles, t)
		}
	}
	return m
}

// RefreshMovie reloads metadata from TMDB.
func (a *App) RefreshMovie(ctx context.Context, id int) error {
	d, err := a.TMDB().Movie(ctx, id)
	if err != nil {
		return err
	}
	fresh := movieFromDetails(d)
	_, ok := a.Store.UpdateMovie(id, func(m *model.Movie) {
		m.ImdbID, m.Title, m.OriginalTitle, m.OriginalLanguage = fresh.ImdbID, fresh.Title, fresh.OriginalTitle, fresh.OriginalLanguage
		m.EnglishTitle = fresh.EnglishTitle
		m.AlternativeTitles, m.Year, m.Runtime, m.Overview = fresh.AlternativeTitles, fresh.Year, fresh.Runtime, fresh.Overview
		m.PosterPath, m.BackdropPath, m.Genres, m.Rating = fresh.PosterPath, fresh.BackdropPath, fresh.Genres, fresh.Rating
	})
	if !ok {
		return i18n.New("film not found")
	}
	return nil
}

// SearchReleases queries the indexers and evaluates every release.
func (a *App) SearchReleases(ctx context.Context, id int) (*SearchResult, error) {
	m, ok := a.Store.Movie(id)
	if !ok {
		return nil, i18n.New("film not found")
	}
	if m.EnglishTitle == "" && m.OriginalLanguage != "en" {
		// libraries created before the English title was stored
		if err := a.RefreshMovie(ctx, id); err == nil {
			m, _ = a.Store.Movie(id)
		}
	}
	enabled, err := a.enabledIndexers()
	if err != nil {
		return nil, err
	}
	s := a.Store.Settings()
	rels, results := a.Indexer.SearchMovie(ctx, enabled, indexer.MovieQuery{
		Title: m.Title, OriginalTitle: m.OriginalTitle, EnglishTitle: m.EnglishTitle, Year: m.Year, ImdbID: m.ImdbID, TmdbID: m.TmdbID,
	})
	ds := decision.Evaluate(rels, s.Profile, decision.Context{Movie: m, Blocklist: a.Store.Blocklist(), FreeSpace: usableSpace(s.LibraryPath, s.MinFreeSpaceMB)})
	return &SearchResult{Decisions: ds, Indexers: results}, nil
}

// AutoSearch grabs the best approved release (Radarr's automatic search).
func (a *App) AutoSearch(ctx context.Context, id int) error {
	if !a.setBusy(id, "search") {
		return i18n.New("a search is already running for this film")
	}
	defer a.setBusy(id, "")
	m, _ := a.Store.Movie(id)
	ctx = backoff.Automatic(ctx) // indexers that keep failing are left alone
	res, err := a.SearchReleases(ctx, id)
	if err != nil {
		return err
	}
	var lastErr error
	tried := 0
	for _, d := range res.Decisions {
		if !d.Approved {
			continue
		}
		if tried >= 3 {
			break
		}
		tried++
		if err := a.grab(ctx, id, d.Release, d); err != nil {
			lastErr = err
			a.history(id, "warning", "Failed with %s: %v", d.Release.Title, err)
			continue
		}
		return nil
	}
	if lastErr != nil {
		return lastErr
	}
	n := countResults(res.Indexers)
	a.Store.UpdateMovie(id, func(mv *model.Movie) {
		mv.StatusMessage = i18n.Tf("No acceptable release (%d results examined)", n)
	})
	return i18n.Errorf("no acceptable release for %s (%d results)", m.Title, n)
}

// Grab sends a release chosen by the user (interactive search).
func (a *App) Grab(ctx context.Context, id int, rel model.Release) error {
	if !a.setBusy(id, "grab") {
		return i18n.New("an operation is already running for this film")
	}
	defer a.setBusy(id, "")
	return a.grab(ctx, id, rel, decision.Decision{})
}

// grab downloads the release (torrent file or magnet), runs the safety
// checks and hands it to the torrent client (TorrentClientBase.Download).
func (a *App) grab(ctx context.Context, id int, rel model.Release, d decision.Decision) error {
	if _, ok := a.Store.Movie(id); !ok {
		return i18n.New("film not found")
	}
	dl, err := a.send(ctx, rel, fmt.Sprintf("cinearr-%d", id), func(hash string) bool { return a.isBlocklisted(id, hash) },
		func(hash string, problems string) {
			_ = a.Store.AddBlocklist(model.BlocklistEntry{TmdbID: id, Title: rel.Title, Hash: hash, Indexer: rel.Indexer, Reason: problems})
			a.history(id, "blocked", "Release refused before download (%s): %s", rel.Title, problems)
		},
		func(format string, args ...any) { a.history(id, "warning", format, args...) })
	if err != nil {
		return err
	}
	a.Store.UpdateMovie(id, func(mv *model.Movie) {
		mv.Download = dl
		mv.Status = model.StatusDownloading
		mv.StatusMessage = ""
	})
	extra := ""
	if d.Release.Title != "" {
		extra = fmt.Sprintf(" — %s, %d sources", d.Quality.Name, d.Release.Seeders)
	}
	a.history(id, "grabbed", "%s sent to %s from %s%s", rel.Title, a.Downloader().Name(), rel.Indexer, extra)
	return nil
}

func (a *App) isBlocklisted(id int, hash string) bool {
	if hash == "" {
		return false
	}
	for _, b := range a.Store.Blocklist() {
		if strings.EqualFold(b.Hash, hash) && b.SeriesID == 0 && (b.TmdbID == 0 || b.TmdbID == id) {
			return true
		}
	}
	return false
}

// forgetDownload drops a download that vanished from the torrent client.
func (a *App) forgetDownload(m model.Movie, client string) {
	a.mu.Lock()
	delete(a.missing, strings.ToUpper(m.Download.Hash))
	a.mu.Unlock()
	a.Store.UpdateMovie(m.TmdbID, func(mv *model.Movie) {
		mv.Download = nil
		if mv.File != nil {
			mv.Status = model.StatusAvailable
		} else {
			mv.Status = model.StatusWanted
		}
		mv.StatusMessage = ""
	})
	a.history(m.TmdbID, "removed", "%s: torrent deleted from %s, download dropped", m.Download.ReleaseTitle, client)
}

// failDownload marks the download as failed, blocklists the release and,
// with auto-grab on, looks for another release (failed download handling).
func (a *App) failDownload(m model.Movie, reason string, dangerous bool) {
	if m.Download == nil {
		return
	}
	_ = a.Store.AddBlocklist(model.BlocklistEntry{TmdbID: m.TmdbID, Title: m.Download.ReleaseTitle, Hash: m.Download.Hash,
		Indexer: m.Download.Indexer, Reason: reason})
	a.Store.UpdateMovie(m.TmdbID, func(mv *model.Movie) {
		mv.Download = nil
		if mv.File != nil {
			mv.Status = model.StatusAvailable
		} else {
			mv.Status = model.StatusFailed
		}
		mv.StatusMessage = reason
	})
	kind := "failed"
	if dangerous {
		kind = "blocked"
	}
	a.history(m.TmdbID, kind, "%s: %s", m.Download.ReleaseTitle, reason)
	if a.Store.Settings().AutoGrab {
		diag.Go("new search after a failure", func() {
			c, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
			defer cancel()
			if err := a.AutoSearch(c, m.TmdbID); err != nil {
				a.Log.Printf("new search after a failure (%d): %v", m.TmdbID, err)
			}
		})
	}
}

// DeleteMovie removes a film (and optionally its files and torrent).
func (a *App) DeleteMovie(ctx context.Context, id int, deleteFiles bool) error {
	m, ok := a.Store.Movie(id)
	if !ok {
		return i18n.New("film not found")
	}
	if m.Download != nil && m.Download.Hash != "" {
		_ = a.Downloader().Remove(ctx, m.Download.Hash, true)
	}
	if deleteFiles && m.File != nil {
		a.removeFile(m.File, m.Subtitles)
		if m.File.TorrentHash != "" {
			_ = a.Downloader().Remove(ctx, m.File.TorrentHash, true)
		}
	}
	a.history(id, "deleted", "%s removed from the library", m.Title)
	return a.Store.DeleteMovie(id)
}

// CancelDownload removes the torrent and returns the film to "wanted".
func (a *App) CancelDownload(ctx context.Context, id int, blocklist bool) error {
	m, ok := a.Store.Movie(id)
	if !ok || m.Download == nil {
		return i18n.New("no download in progress")
	}
	_ = a.Downloader().Remove(ctx, m.Download.Hash, true)
	if blocklist {
		_ = a.Store.AddBlocklist(model.BlocklistEntry{TmdbID: id, Title: m.Download.ReleaseTitle, Hash: m.Download.Hash,
			Indexer: m.Download.Indexer, Reason: i18n.T("removed manually")})
	}
	a.Store.UpdateMovie(id, func(mv *model.Movie) {
		mv.Download = nil
		if mv.File != nil {
			mv.Status = model.StatusAvailable
		} else {
			mv.Status = model.StatusWanted
		}
	})
	a.history(id, "cancelled", "Download of %s cancelled", m.Download.ReleaseTitle)
	return nil
}
