package app

import (
	"context"
	"strings"
	"time"

	"cinearr/internal/diag"
	"cinearr/internal/dlclient"
	"cinearr/internal/i18n"
	"cinearr/internal/model"
	"cinearr/internal/torrent"
)

// Monitoring of the torrent client, shared by films and series (Radarr's
// and Sonarr's download client polling).

// seriesDL is a download of a series being followed.
type seriesDL struct {
	id int
	d  model.SeriesDownload
}

// seedOwner tells which film or series an imported torrent belongs to.
type seedOwner struct{ movie, series int }

// syncDownloads polls the torrent client: progress, safety check of magnet
// content once its metadata is known, errors, torrents removed by the user,
// import, and the optional removal of torrents that finished seeding.
func (a *App) syncDownloads(ctx context.Context) {
	s := a.Store.Settings()
	if !s.SetupDone {
		return
	}
	var movies []model.Movie
	var series []seriesDL
	seeding := map[string]seedOwner{}
	for _, m := range a.Store.Movies() {
		if m.Download != nil && m.Download.Hash != "" {
			movies = append(movies, m)
		}
		if s.RemoveAfterImport && m.File != nil && m.File.TorrentHash != "" {
			seeding[strings.ToUpper(m.File.TorrentHash)] = seedOwner{movie: m.TmdbID}
		}
	}
	for _, x := range a.Store.AllSeries() {
		for _, d := range x.Downloads {
			if d.Hash != "" {
				series = append(series, seriesDL{id: x.TmdbID, d: d})
			}
		}
		if !s.RemoveAfterImport {
			continue
		}
		for _, se := range x.Seasons {
			for _, e := range se.Episodes {
				if e.File != nil && e.File.TorrentHash != "" {
					seeding[strings.ToUpper(e.File.TorrentHash)] = seedOwner{series: x.TmdbID}
				}
			}
		}
	}
	dc := a.Downloader()
	if len(movies)+len(series)+len(seeding) == 0 {
		a.probe(ctx, dc)
		return
	}

	cctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	hashes := make([]string, 0, len(movies)+len(series)+len(seeding))
	for _, m := range movies {
		hashes = append(hashes, m.Download.Hash)
	}
	for _, x := range series {
		hashes = append(hashes, x.d.Hash)
	}
	for h := range seeding {
		hashes = append(hashes, h)
	}
	list, err := dc.Torrents(cctx, hashes)
	a.mu.Lock()
	a.lastSync, a.syncError = time.Now(), errString(err)
	a.mu.Unlock()
	if err != nil {
		return
	}
	byHash := map[string]dlclient.Torrent{}
	for _, t := range list {
		byHash[strings.ToUpper(t.Hash)] = t
	}
	for _, m := range movies {
		a.syncMovie(cctx, dc, byHash, m)
	}
	for _, x := range series {
		a.syncSeries(cctx, dc, byHash, x)
	}
	a.cleanSeeding(cctx, dc, byHash, seeding)
}

// probe checks the client every minute (and right after its settings
// change) while nothing is followed, so the status shown stays true.
func (a *App) probe(ctx context.Context, dc dlclient.Client) {
	a.mu.Lock()
	due := time.Since(a.lastProbe) > time.Minute || a.probeKey != a.dcKey
	key := a.dcKey
	a.mu.Unlock()
	if !due {
		return
	}
	cctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	_, err := dc.Test(cctx)
	a.mu.Lock()
	a.lastProbe, a.probeKey = time.Now(), key
	a.lastSync, a.syncError = time.Now(), errString(err)
	a.mu.Unlock()
}

// gone reports that a followed torrent was removed by the user: it has been
// missing from the client for about a minute (12 polls).
func (a *App) gone(hash string, present bool) bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	if present {
		delete(a.missing, hash)
		return false
	}
	a.missing[hash]++
	if a.missing[hash] < 12 {
		return false
	}
	delete(a.missing, hash)
	return true
}

// importDue lets an import run at most every five minutes per torrent: a
// failed import is retried once the user fixed a folder or a path mapping,
// not on every poll.
func (a *App) importDue(hash string) bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	if last, ok := a.imported[hash]; ok && time.Since(last) < 5*time.Minute {
		return false
	}
	a.imported[hash] = time.Now()
	return true
}

// checkContent inspects the file list of a magnet download once the client
// knows it. checked is false while the list is not available yet.
func checkContent(ctx context.Context, dc dlclient.Client, hash string, rejected []string) (checked bool, problems string) {
	files, err := dc.Files(ctx, hash)
	if err != nil || len(files) == 0 {
		return false, ""
	}
	tf := make([]torrent.File, 0, len(files))
	for _, f := range files {
		tf = append(tf, torrent.File{Path: f.Path, Size: f.Size})
	}
	if rep := torrent.Inspect(tf, rejected); !rep.OK {
		return true, strings.Join(rep.Problems, " ; ")
	}
	return true, ""
}

func clientError(dc dlclient.Client, t dlclient.Torrent) string {
	msg := t.Message
	if msg == "" {
		msg = t.State
	}
	return i18n.Tf("%s reports an error (%s)", dc.Name(), msg)
}

func progress(d *model.Download, t dlclient.Torrent) {
	d.Progress, d.State, d.ETA, d.DlSpeed, d.Seeds = t.Progress, t.Status, t.ETA, t.DlSpeed, t.Seeds
	if t.Size > 0 {
		d.Size = t.Size
	}
}

func (a *App) syncMovie(ctx context.Context, dc dlclient.Client, byHash map[string]dlclient.Torrent, m model.Movie) {
	h := strings.ToUpper(m.Download.Hash)
	t, ok := byHash[h]
	if a.gone(h, ok) {
		a.forgetDownload(m, dc.Name())
		return
	}
	if !ok {
		return
	}
	checked := m.Download.Checked
	if !checked && t.Status != "metadata" {
		var problems string
		if checked, problems = checkContent(ctx, dc, h, a.Store.Settings().RejectedExtensions); problems != "" {
			_ = dc.Remove(ctx, h, true)
			a.failDownload(m, i18n.Tf("dangerous or suspicious content: %s", problems), true)
			return
		}
	}
	a.Store.UpdateMovie(m.TmdbID, func(mv *model.Movie) {
		if mv.Download != nil {
			progress(mv.Download, t)
			mv.Download.Checked = mv.Download.Checked || checked
		}
	})
	switch {
	case t.Status == "error":
		a.failDownload(m, clientError(dc, t), false)
	case t.Completed && checked && m.Status != model.StatusImporting && a.importDue(h):
		a.Store.UpdateMovie(m.TmdbID, func(mv *model.Movie) { mv.Status = model.StatusImporting })
		diag.Go("import", func() { a.importDownload(context.Background(), m.TmdbID, t) })
	}
}

func (a *App) syncSeries(ctx context.Context, dc dlclient.Client, byHash map[string]dlclient.Torrent, x seriesDL) {
	h := strings.ToUpper(x.d.Hash)
	t, ok := byHash[h]
	if a.gone(h, ok) {
		a.dropSeriesDownload(x.id, x.d.Hash, model.StatusWanted, "")
		a.seriesHistory(x.id, "removed", "%s: torrent deleted from %s, download dropped", x.d.ReleaseTitle, dc.Name())
		return
	}
	if !ok {
		return
	}
	checked := x.d.Checked
	if !checked && t.Status != "metadata" {
		var problems string
		if checked, problems = checkContent(ctx, dc, h, a.Store.Settings().RejectedExtensions); problems != "" {
			_ = dc.Remove(ctx, h, true)
			a.failSeriesDownload(x.id, x.d, i18n.Tf("dangerous or suspicious content: %s", problems), true)
			return
		}
	}
	a.Store.UpdateSeries(x.id, func(s *model.Series) {
		if d := s.DownloadByHash(h); d != nil {
			progress(&d.Download, t)
			d.Checked = d.Checked || checked
		}
	})
	switch {
	case t.Status == "error":
		a.failSeriesDownload(x.id, x.d, clientError(dc, t), false)
	case t.Completed && checked && !x.d.Importing && a.importDue(h):
		a.Store.UpdateSeries(x.id, func(s *model.Series) {
			if d := s.DownloadByHash(h); d != nil {
				d.Importing = true
			}
			for _, k := range x.d.Episodes {
				if e := s.Episode(k.Season, k.Episode); e != nil && strings.EqualFold(e.DownloadHash, h) {
					e.Status = model.StatusImporting
				}
			}
		})
		diag.Go("series import", func() { a.importSeriesDownload(context.Background(), x.id, h, t) })
	}
}

// cleanSeeding removes from the client the imported torrents that finished
// seeding, when "remove after seeding" is on. The library files are hard
// links or copies: removing the torrent data does not touch them.
func (a *App) cleanSeeding(ctx context.Context, dc dlclient.Client, byHash map[string]dlclient.Torrent, seeding map[string]seedOwner) {
	for h, owner := range seeding {
		t, ok := byHash[h]
		if ok && !t.SeedingDone {
			continue
		}
		if ok {
			if err := dc.Remove(ctx, t.Hash, true); err != nil {
				continue
			}
			if owner.movie != 0 {
				a.history(owner.movie, "cleanup", "Torrent removed from %s after seeding", dc.Name())
			} else {
				a.seriesHistory(owner.series, "cleanup", "Torrent removed from %s after seeding", dc.Name())
			}
		}
		// removed (by us, or by the user): the files no longer seed
		if owner.movie != 0 {
			a.Store.UpdateMovie(owner.movie, func(m *model.Movie) {
				if m.File != nil {
					m.File.TorrentHash = ""
				}
			})
			continue
		}
		a.Store.UpdateSeries(owner.series, func(s *model.Series) {
			for si := range s.Seasons {
				for ei := range s.Seasons[si].Episodes {
					if f := s.Seasons[si].Episodes[ei].File; f != nil && strings.EqualFold(f.TorrentHash, h) {
						f.TorrentHash = ""
					}
				}
			}
		})
	}
}
