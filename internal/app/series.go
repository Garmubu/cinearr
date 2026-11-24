package app

import (
	"context"
	"fmt"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"cinearr/internal/backoff"
	"cinearr/internal/decision"
	"cinearr/internal/diag"
	"cinearr/internal/i18n"
	"cinearr/internal/indexer"
	"cinearr/internal/model"
	"cinearr/internal/tmdb"
)

// Series support follows Sonarr: a series is added without downloading
// anything; the user picks episodes (or whole seasons) and starts an
// automatic or a manual search. Season packs are used when every missing
// episode of the season is selected.

// AddSeries adds a series from TMDB with all its seasons and episodes.
// Nothing is downloaded.
func (a *App) AddSeries(ctx context.Context, tmdbID int) (model.Series, error) {
	if s, ok := a.Store.Series(tmdbID); ok {
		return s, nil
	}
	d, err := a.TMDB().TV(ctx, tmdbID)
	if err != nil {
		return model.Series{}, err
	}
	s := seriesFromDetails(d)
	seasons, err := a.loadSeasons(ctx, d, nil)
	if err != nil {
		return model.Series{}, err
	}
	s.Seasons = seasons
	s.Added = time.Now()
	s.Refreshed = time.Now()
	s.Downloads = []model.SeriesDownload{}
	if err := a.Store.PutSeries(s); err != nil {
		return model.Series{}, err
	}
	a.seriesHistory(s.TmdbID, "added", "%s (%d) added to the library", s.Title, s.Year)
	return s, nil
}

func seriesFromDetails(d *tmdb.TVDetails) model.Series {
	s := model.Series{
		TmdbID: d.ID, TvdbID: d.ExternalIDs.TvdbID, ImdbID: d.ExternalIDs.ImdbID, Title: d.Name, OriginalTitle: d.OriginalName,
		EnglishTitle: d.EnglishTitle(), OriginalLanguage: d.OriginalLanguage, Year: d.Year, Runtime: d.Runtime(),
		Overview: d.Overview, PosterPath: d.PosterPath, BackdropPath: d.BackdropPath, Rating: d.VoteAverage, State: d.Status,
	}
	for _, g := range d.Genres {
		s.Genres = append(s.Genres, g.Name)
	}
	if len(d.Networks) > 0 {
		s.Network = d.Networks[0].Name
	}
	for _, t := range d.AllTitles() {
		if t != d.Name && t != d.OriginalName {
			s.AlternativeTitles = append(s.AlternativeTitles, t)
		}
	}
	return s
}

// loadSeasons reads every season from TMDB, keeping the state (files,
// downloads, subtitles) of the episodes already known.
func (a *App) loadSeasons(ctx context.Context, d *tmdb.TVDetails, old *model.Series) ([]model.Season, error) {
	tm := a.TMDB()
	seasons := make([]model.Season, len(d.Seasons))
	errs := make([]error, len(d.Seasons))
	var wg sync.WaitGroup
	sem := make(chan struct{}, 4)
	for i, info := range d.Seasons {
		wg.Add(1)
		go func(i, n int) {
			defer wg.Done()
			defer diag.Recover("season " + fmt.Sprint(n))
			sem <- struct{}{}
			defer func() { <-sem }()
			sd, err := tm.Season(ctx, d.ID, n)
			if err != nil {
				errs[i] = err
				return
			}
			s := model.Season{Number: n, Name: sd.Name, Overview: sd.Overview, PosterPath: sd.PosterPath, AirDate: sd.AirDate}
			for _, e := range sd.Episodes {
				s.Episodes = append(s.Episodes, model.Episode{Season: n, Number: e.EpisodeNumber, Title: e.Name, Overview: e.Overview,
					AirDate: e.AirDate, Runtime: e.Runtime})
			}
			seasons[i] = s
		}(i, info.SeasonNumber)
	}
	wg.Wait()
	var out []model.Season
	for i, s := range seasons {
		if errs[i] != nil {
			if old != nil {
				if o := old.SeasonByNumber(d.Seasons[i].SeasonNumber); o != nil {
					out = append(out, *o)
					continue
				}
			}
			return nil, errs[i]
		}
		out = append(out, s)
	}
	if old != nil {
		for si := range out {
			for ei := range out[si].Episodes {
				e := &out[si].Episodes[ei]
				if o := old.Episode(e.Season, e.Number); o != nil {
					e.Status, e.StatusMessage, e.DownloadHash, e.File, e.Subtitles = o.Status, o.StatusMessage, o.DownloadHash, o.File, o.Subtitles
				}
			}
		}
		// episodes that disappeared from TMDB but have a file are kept
		for _, olds := range old.Seasons {
			for _, oe := range olds.Episodes {
				if oe.File == nil {
					continue
				}
				found := false
				for _, s := range out {
					for _, e := range s.Episodes {
						if e.Season == oe.Season && e.Number == oe.Number {
							found = true
						}
					}
				}
				if !found {
					for si := range out {
						if out[si].Number == oe.Season {
							out[si].Episodes = append(out[si].Episodes, oe)
							found = true
						}
					}
					if !found {
						out = append(out, model.Season{Number: oe.Season, Episodes: []model.Episode{oe}})
					}
				}
			}
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Number < out[j].Number })
	for i := range out {
		sort.Slice(out[i].Episodes, func(x, y int) bool { return out[i].Episodes[x].Number < out[i].Episodes[y].Number })
	}
	return out, nil
}

// RefreshSeries reloads the series and its episodes from TMDB.
func (a *App) RefreshSeries(ctx context.Context, id int) error {
	old, ok := a.Store.Series(id)
	if !ok {
		return i18n.New("series not found")
	}
	d, err := a.TMDB().TV(ctx, id)
	if err != nil {
		return err
	}
	seasons, err := a.loadSeasons(ctx, d, &old)
	if err != nil {
		return err
	}
	fresh := seriesFromDetails(d)
	a.Store.UpdateSeries(id, func(s *model.Series) {
		// state changed while TMDB was queried: take it from the store
		for si := range seasons {
			for ei := range seasons[si].Episodes {
				e := &seasons[si].Episodes[ei]
				if cur := s.Episode(e.Season, e.Number); cur != nil {
					e.Status, e.StatusMessage, e.DownloadHash, e.File, e.Subtitles = cur.Status, cur.StatusMessage, cur.DownloadHash, cur.File, cur.Subtitles
				}
			}
		}
		added, downloads := s.Added, s.Downloads
		*s = fresh
		s.Added, s.Downloads, s.Seasons, s.Refreshed = added, downloads, seasons, time.Now()
	})
	return nil
}

// refreshStaleSeries reloads, once a day, the series still on the air so
// new episodes and air dates appear (Sonarr refreshes every 12 hours).
func (a *App) refreshStaleSeries(ctx context.Context) {
	if !a.Store.Settings().SetupDone {
		return
	}
	for _, s := range a.Store.AllSeries() {
		ended := s.State == "Ended" || s.State == "Canceled"
		age := time.Since(s.Refreshed)
		if age < 24*time.Hour || (ended && age < 30*24*time.Hour) {
			continue
		}
		c, cancel := context.WithTimeout(ctx, 2*time.Minute)
		if err := a.RefreshSeries(c, s.TmdbID); err != nil {
			a.Log.Printf("series refresh (%s): %v", s.Title, err)
		}
		cancel()
	}
}

// DeleteSeries removes a series (and optionally its files and torrents).
func (a *App) DeleteSeries(ctx context.Context, id int, deleteFiles bool) error {
	s, ok := a.Store.Series(id)
	if !ok {
		return i18n.New("series not found")
	}
	dc := a.Downloader()
	for _, d := range s.Downloads {
		_ = dc.Remove(ctx, d.Hash, true)
	}
	if deleteFiles {
		hashes := map[string]bool{}
		dir := ""
		for _, se := range s.Seasons {
			for _, e := range se.Episodes {
				if e.File == nil {
					continue
				}
				a.removeFile(e.File, e.Subtitles)
				dir = filepath.Dir(filepath.Dir(e.File.Path))
				if e.File.TorrentHash != "" {
					hashes[e.File.TorrentHash] = true
				}
			}
		}
		if dir != "" {
			removeIfEmpty(dir) // the series folder, once its seasons are gone
		}
		for h := range hashes {
			_ = dc.Remove(ctx, h, true)
		}
	}
	a.seriesHistory(id, "deleted", "%s removed from the library", s.Title)
	return a.Store.DeleteSeries(id)
}

func tvQuery(s model.Series, season, episode int) indexer.TVQuery {
	return indexer.TVQuery{Title: s.Title, OriginalTitle: s.OriginalTitle, EnglishTitle: s.EnglishTitle, ImdbID: s.ImdbID,
		TvdbID: s.TvdbID, TmdbID: s.TmdbID, Season: season, Episode: episode}
}

func (a *App) seriesFreeSpace() int64 {
	s := a.Store.Settings()
	if s.SeriesPath != "" {
		return usableSpace(s.SeriesPath, s.MinFreeSpaceMB)
	}
	return 0
}

// SearchEpisodeReleases is the manual search for one episode, or for a
// whole season when episode is 0 (season packs and single episodes).
func (a *App) SearchEpisodeReleases(ctx context.Context, id, season, episode int) (*SearchResult, error) {
	s, ok := a.Store.Series(id)
	if !ok {
		return nil, i18n.New("series not found")
	}
	enabled, err := a.enabledIndexers()
	if err != nil {
		return nil, err
	}
	var wanted []model.EpisodeKey
	if episode > 0 {
		if s.Episode(season, episode) == nil {
			return nil, i18n.New("episode not found")
		}
		wanted = []model.EpisodeKey{{Season: season, Episode: episode}}
	} else {
		se := s.SeasonByNumber(season)
		if se == nil {
			return nil, i18n.New("season not found")
		}
		// every aired episode: those already downloaded are then judged
		// as upgrades
		for _, e := range se.Episodes {
			if e.File != nil || e.Aired(time.Now()) {
				wanted = append(wanted, model.EpisodeKey{Season: season, Episode: e.Number})
			}
		}
	}
	rels, results := a.Indexer.SearchTV(ctx, enabled, tvQuery(s, season, episode))
	ds := decision.EvaluateEpisodes(rels, a.Store.Settings().Profile, decision.EpisodeContext{Series: s, Wanted: wanted,
		Blocklist: a.Store.Blocklist(), FreeSpace: a.seriesFreeSpace()})
	return &SearchResult{Decisions: ds, Indexers: results}, nil
}

// AutoSearchEpisodes finds and grabs releases for the selected episodes
// (Sonarr's episode and season searches): a season search when every
// missing aired episode of a season is selected, then one search per
// episode still missing.
func (a *App) AutoSearchEpisodes(ctx context.Context, id int, keys []model.EpisodeKey) error {
	if !a.setSeriesBusy(id, "search") {
		return i18n.New("a search is already running for this series")
	}
	defer a.setSeriesBusy(id, "")
	ctx = backoff.Automatic(ctx) // indexers that keep failing are left alone
	s, ok := a.Store.Series(id)
	if !ok {
		return i18n.New("series not found")
	}
	now := time.Now()
	want := map[model.EpisodeKey]bool{}
	for _, k := range keys {
		e := s.Episode(k.Season, k.Episode)
		if e == nil || e.DownloadHash != "" {
			continue
		}
		want[k] = true
	}
	if len(want) == 0 {
		return i18n.New("no episode to search (already downloading?)")
	}
	a.Store.UpdateSeries(id, func(x *model.Series) {
		for k := range want {
			if e := x.Episode(k.Season, k.Episode); e != nil {
				if e.File == nil {
					e.Status = model.StatusWanted
				}
				e.StatusMessage = ""
			}
		}
	})
	enabled, err := a.enabledIndexers()
	if err != nil {
		return err
	}
	p := a.Store.Settings().Profile
	block := a.Store.Blocklist()
	free := a.seriesFreeSpace()
	grabbed := map[model.EpisodeKey]bool{}
	examined := 0
	var lastErr error

	process := func(ds []decision.Decision) {
		for _, d := range ds {
			if !d.Approved {
				continue
			}
			overlap := false
			for _, k := range d.Covered {
				if grabbed[k] {
					overlap = true
				}
			}
			if overlap || len(d.Covered) == 0 {
				continue
			}
			if err := a.grabSeries(ctx, id, d.Release, d.Covered, d.Label, d); err != nil {
				lastErr = err
				a.seriesHistory(id, "warning", "Failed with %s: %v", d.Release.Title, err)
				continue
			}
			for _, k := range d.Covered {
				grabbed[k] = true
			}
		}
	}

	// group by season
	bySeason := map[int][]model.EpisodeKey{}
	for k := range want {
		bySeason[k.Season] = append(bySeason[k.Season], k)
	}
	var seasonNums []int
	for n := range bySeason {
		seasonNums = append(seasonNums, n)
	}
	sort.Ints(seasonNums)
	for _, n := range seasonNums {
		ks := bySeason[n]
		sort.Slice(ks, func(i, j int) bool { return ks[i].Episode < ks[j].Episode })
		se := s.SeasonByNumber(n)
		missing := 0
		for _, e := range se.Episodes {
			if e.File == nil && e.Aired(now) {
				missing++
			}
		}
		if n > 0 && len(ks) >= 2 && len(ks) >= missing {
			rels, res := a.Indexer.SearchTV(ctx, enabled, tvQuery(s, n, 0))
			examined += countResults(res)
			process(decision.EvaluateEpisodes(rels, p, decision.EpisodeContext{Series: s, Wanted: ks, Blocklist: block, FreeSpace: free, Now: now}))
		}
		for _, k := range ks {
			if grabbed[k] || ctx.Err() != nil {
				continue
			}
			rels, res := a.Indexer.SearchTV(ctx, enabled, tvQuery(s, k.Season, k.Episode))
			examined += countResults(res)
			// a release found here may also cover other selected episodes
			process(decision.EvaluateEpisodes(rels, p, decision.EpisodeContext{Series: s, Wanted: remaining(ks, grabbed), Blocklist: block, FreeSpace: free, Now: now}))
		}
	}

	var notFound []model.EpisodeKey
	for k := range want {
		if !grabbed[k] {
			notFound = append(notFound, k)
		}
	}
	if len(notFound) > 0 {
		msg := i18n.Tf("No acceptable release (%d results examined)", examined)
		a.Store.UpdateSeries(id, func(x *model.Series) {
			for _, k := range notFound {
				if e := x.Episode(k.Season, k.Episode); e != nil && e.DownloadHash == "" {
					e.StatusMessage = msg
				}
			}
		})
	}
	switch {
	case len(grabbed) == 0 && lastErr != nil:
		return lastErr
	case len(grabbed) == 0:
		return i18n.Errorf("no acceptable release for %s (%d results)", s.Title, examined)
	case len(notFound) > 0:
		a.seriesHistory(id, "warning", "%s: %d episodes found, %d without an acceptable release", s.Title, len(grabbed), len(notFound))
	}
	return nil
}

func remaining(ks []model.EpisodeKey, grabbed map[model.EpisodeKey]bool) []model.EpisodeKey {
	var out []model.EpisodeKey
	for _, k := range ks {
		if !grabbed[k] {
			out = append(out, k)
		}
	}
	return out
}

// GrabEpisodes sends a release chosen in the manual search. keys are the
// episodes the search was for; the release keeps the ones it contains.
func (a *App) GrabEpisodes(ctx context.Context, id int, rel model.Release, keys []model.EpisodeKey) error {
	if !a.setSeriesBusy(id, "grab") {
		return i18n.New("an operation is already running for this series")
	}
	defer a.setSeriesBusy(id, "")
	s, ok := a.Store.Series(id)
	if !ok {
		return i18n.New("series not found")
	}
	ds := decision.EvaluateEpisodes([]model.Release{rel}, a.Store.Settings().Profile, decision.EpisodeContext{Series: s, Wanted: keys})
	d := ds[0]
	covered := d.Covered
	if len(covered) == 0 && d.Episode != nil {
		// forced: take every episode of the series the release contains
		for _, se := range s.Seasons {
			for _, e := range se.Episodes {
				if d.Episode.Covers(se.Number, e.Number) && (se.Number > 0 || d.Episode.Special) {
					covered = append(covered, model.EpisodeKey{Season: se.Number, Episode: e.Number})
				}
			}
		}
	}
	if len(covered) == 0 {
		return i18n.New("this release does not contain any episode of the series")
	}
	return a.grabSeries(ctx, id, rel, covered, d.Label, decision.Decision{})
}

func (a *App) isSeriesBlocklisted(id int, hash string) bool {
	if hash == "" {
		return false
	}
	for _, b := range a.Store.Blocklist() {
		if strings.EqualFold(b.Hash, hash) && (b.SeriesID == id || (b.SeriesID == 0 && b.TmdbID == 0)) {
			return true
		}
	}
	return false
}

func (a *App) grabSeries(ctx context.Context, id int, rel model.Release, covered []model.EpisodeKey, label string, d decision.Decision) error {
	s, ok := a.Store.Series(id)
	if !ok {
		return i18n.New("series not found")
	}
	dl, err := a.send(ctx, rel, fmt.Sprintf("cinearr-tv-%d", id), func(hash string) bool { return a.isSeriesBlocklisted(id, hash) },
		func(hash, problems string) {
			_ = a.Store.AddBlocklist(model.BlocklistEntry{SeriesID: id, Title: rel.Title, Hash: hash, Indexer: rel.Indexer, Reason: problems})
			a.seriesHistory(id, "blocked", "Release refused before download (%s): %s", rel.Title, problems)
		},
		func(format string, args ...any) { a.seriesHistory(id, "warning", format, args...) })
	if err != nil {
		return err
	}
	if label == "" {
		label = episodeLabel(covered)
	}
	a.Store.UpdateSeries(id, func(x *model.Series) {
		// a torrent already followed (same hash) is replaced
		x.Downloads = append(withoutDownload(x.Downloads, dl.Hash), model.SeriesDownload{Download: *dl, Episodes: covered, Label: label})
		for _, k := range covered {
			if e := x.Episode(k.Season, k.Episode); e != nil {
				e.DownloadHash = dl.Hash
				e.Status = model.StatusDownloading
				e.StatusMessage = ""
			}
		}
	})
	extra := ""
	if d.Release.Title != "" {
		extra = fmt.Sprintf(" — %s, %d sources", d.Quality.Name, d.Release.Seeders)
	}
	a.seriesHistory(id, "grabbed", "%s (%s) sent to %s from %s%s", rel.Title, s.Title+" "+label, a.Downloader().Name(), rel.Indexer, extra)
	return nil
}

// episodeLabel describes a set of episodes: S02E01, S02E01-E03, S02 (3).
func episodeLabel(keys []model.EpisodeKey) string {
	if len(keys) == 0 {
		return ""
	}
	ks := append([]model.EpisodeKey(nil), keys...)
	sort.Slice(ks, func(i, j int) bool {
		if ks[i].Season != ks[j].Season {
			return ks[i].Season < ks[j].Season
		}
		return ks[i].Episode < ks[j].Episode
	})
	first, last := ks[0], ks[len(ks)-1]
	if len(ks) == 1 {
		return fmt.Sprintf("S%02dE%02d", first.Season, first.Episode)
	}
	if first.Season == last.Season {
		if last.Episode-first.Episode == len(ks)-1 {
			return fmt.Sprintf("S%02dE%02d-E%02d", first.Season, first.Episode, last.Episode)
		}
		return fmt.Sprintf("S%02d (%d)", first.Season, len(ks))
	}
	return fmt.Sprintf("S%02d-S%02d (%d)", first.Season, last.Season, len(ks))
}

// CancelSeriesDownload removes a torrent of the series; its episodes go
// back to "wanted".
func (a *App) CancelSeriesDownload(ctx context.Context, id int, hash string, blocklist bool) error {
	s, ok := a.Store.Series(id)
	if !ok {
		return i18n.New("series not found")
	}
	d := s.DownloadByHash(hash)
	if d == nil {
		return i18n.New("no download in progress")
	}
	_ = a.Downloader().Remove(ctx, d.Hash, true)
	if blocklist {
		_ = a.Store.AddBlocklist(model.BlocklistEntry{SeriesID: id, Title: d.ReleaseTitle, Hash: d.Hash, Indexer: d.Indexer, Reason: i18n.T("removed manually")})
	}
	a.dropSeriesDownload(id, d.Hash, model.StatusWanted, "")
	a.seriesHistory(id, "cancelled", "Download of %s cancelled", d.ReleaseTitle)
	return nil
}

// dropSeriesDownload forgets a download and gives its episodes status
// (unless they have a file).
func (a *App) dropSeriesDownload(id int, hash, status, msg string) {
	a.Store.UpdateSeries(id, func(x *model.Series) {
		x.Downloads = withoutDownload(x.Downloads, hash)
		for si := range x.Seasons {
			for ei := range x.Seasons[si].Episodes {
				e := &x.Seasons[si].Episodes[ei]
				if !strings.EqualFold(e.DownloadHash, hash) {
					continue
				}
				e.DownloadHash = ""
				if e.File != nil {
					e.Status = model.StatusAvailable
					e.StatusMessage = ""
				} else {
					e.Status = status
					e.StatusMessage = msg
				}
			}
		}
	})
}

// withoutDownload returns the downloads except the one with hash.
func withoutDownload(list []model.SeriesDownload, hash string) []model.SeriesDownload {
	keep := make([]model.SeriesDownload, 0, len(list))
	for _, d := range list {
		if !strings.EqualFold(d.Hash, hash) {
			keep = append(keep, d)
		}
	}
	return keep
}

// failSeriesDownload blocklists the release and, with auto-grab on, looks
// for another release for the same episodes.
func (a *App) failSeriesDownload(id int, d model.SeriesDownload, reason string, dangerous bool) {
	_ = a.Store.AddBlocklist(model.BlocklistEntry{SeriesID: id, Title: d.ReleaseTitle, Hash: d.Hash, Indexer: d.Indexer, Reason: reason})
	a.dropSeriesDownload(id, d.Hash, model.StatusFailed, reason)
	kind := "failed"
	if dangerous {
		kind = "blocked"
	}
	a.seriesHistory(id, kind, "%s: %s", d.ReleaseTitle, reason)
	if a.Store.Settings().AutoGrab {
		keys := d.Episodes
		diag.Go("new series search after a failure", func() {
			c, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
			defer cancel()
			if err := a.AutoSearchEpisodes(c, id, keys); err != nil {
				a.Log.Printf("new search after a failure (series %d): %v", id, err)
			}
		})
	}
}
