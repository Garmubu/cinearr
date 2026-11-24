// Package app wires the services together: library, searches, grabs,
// download monitoring, import, subtitles and playback.
package app

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"sync"
	"time"

	"cinearr/internal/decision"
	"cinearr/internal/diag"
	"cinearr/internal/dlclient"
	"cinearr/internal/i18n"
	"cinearr/internal/indexer"
	"cinearr/internal/model"
	"cinearr/internal/proxy"
	"cinearr/internal/store"
	"cinearr/internal/subtitles"
	"cinearr/internal/tmdb"
)

// App is the application service.
type App struct {
	Store   *store.Store
	Indexer *indexer.Manager
	Log     *log.Logger

	mu              sync.Mutex
	dc              dlclient.Client
	dcKey           string
	tm              *tmdb.Client
	tmKey           string
	subProvs        []subtitles.Provider
	subKey          string
	busy            map[int]string       // movie id -> running task
	busySeries      map[int]string       // series id -> running task
	missing         map[string]int       // torrent hash -> consecutive polls not found
	imported        map[string]time.Time // torrent hash -> last import attempt
	lastSync        time.Time
	syncError       string
	lastProbe       time.Time // last connection check while idle
	lastSeriesCheck time.Time
	lastRecycle     time.Time  // last cleanup of the recycle bins
	scan            ScanReport // last library scan
	startScan       bool       // the scan of the launch is done
	probeKey        string
}

// New creates the service.
func New(st *store.Store, logger *log.Logger) *App {
	proxy.Set(st.Settings().Proxy)
	return &App{
		Store:      st,
		Indexer:    indexer.NewManager(indexer.NewRepository(st.Dir())),
		Log:        logger,
		busy:       map[int]string{},
		busySeries: map[int]string{},
		missing:    map[string]int{},
		imported:   map[string]time.Time{},
	}
}

// Downloader returns the torrent client built from the current settings.
func (a *App) Downloader() dlclient.Client {
	s := a.Store.Settings().Client
	key := fmt.Sprintf("%+v", s)
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.dc == nil || a.dcKey != key {
		a.dc = dlclient.New(s)
		a.dcKey = key
	}
	return a.dc
}

// TMDB returns a client built from the current settings.
func (a *App) TMDB() *tmdb.Client {
	s := a.Store.Settings()
	key := s.TMDBKey + "|" + s.Language
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.tm == nil || a.tmKey != key {
		a.tm = tmdb.New(s.TMDBKey, s.Language)
		a.tmKey = key
	}
	return a.tm
}

// SubtitleProviders returns the configured providers.
func (a *App) SubtitleProviders() []subtitles.Provider {
	cfgs := a.Store.Settings().Subtitles.Providers
	b, _ := json.Marshal(cfgs)
	key := string(b) + "|" + subtitles.BuiltinOpenSubtitlesKey
	a.mu.Lock()
	defer a.mu.Unlock()
	// providers are kept between searches (OpenSubtitles keeps its login)
	if a.subKey != key {
		a.subProvs, _ = subtitles.Build(cfgs)
		a.subKey = key
	}
	return a.subProvs
}

// SubtitleSetupProblem explains why no subtitle provider can be used.
func SubtitleSetupProblem(s store.SubtitleSettings) string {
	provs, errs := subtitles.Build(s.Providers)
	if len(provs) > 0 {
		return ""
	}
	if len(errs) > 0 {
		return errs[0].Error()
	}
	return i18n.T("No subtitle provider enabled: add one in Settings › Subtitles.")
}

// history records an event of a film (a series with seriesHistory).
func (a *App) history(id int, kind, format string, args ...any) {
	a.record(model.HistoryEvent{TmdbID: id, Kind: kind}, format, args...)
}

func (a *App) seriesHistory(id int, kind, format string, args ...any) {
	a.record(model.HistoryEvent{SeriesID: id, Kind: kind}, format, args...)
}

func (a *App) record(e model.HistoryEvent, format string, args ...any) {
	e.Message = i18n.Tf(format, args...)
	a.Store.AddHistory(e)
	a.Log.Printf("[%s] %s", e.Kind, e.Message)
}

// claim marks a task as running for a film or a series (one at a time);
// an empty task releases it.
func (a *App) claim(busy map[int]string, id int, task string) bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	if task == "" {
		delete(busy, id)
		return true
	}
	if _, running := busy[id]; running {
		return false
	}
	busy[id] = task
	return true
}

func (a *App) setBusy(id int, task string) bool       { return a.claim(a.busy, id, task) }
func (a *App) setSeriesBusy(id int, task string) bool { return a.claim(a.busySeries, id, task) }

func (a *App) tasks(busy map[int]string) map[int]string {
	a.mu.Lock()
	defer a.mu.Unlock()
	out := make(map[int]string, len(busy))
	for k, v := range busy {
		out[k] = v
	}
	return out
}

// Busy returns the tasks in progress per film.
func (a *App) Busy() map[int]string { return a.tasks(a.busy) }

// BusySeries returns the tasks in progress per series.
func (a *App) BusySeries() map[int]string { return a.tasks(a.busySeries) }

// SyncStatus reports the last download-client poll.
func (a *App) SyncStatus() (time.Time, string) {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.lastSync, a.syncError
}

// ---------------------------------------------------------------------------
// Indexers

// TestIndexer runs a search on one indexer configuration.
func (a *App) TestIndexer(ctx context.Context, cfg indexer.Config) (int, error) {
	a.Indexer.Forget(cfg.ID)
	s, err := a.Indexer.Get(ctx, cfg)
	if err != nil {
		return 0, err
	}
	rels, err := testSearch(ctx, s)
	return len(rels), err
}

// ---------------------------------------------------------------------------
// Background loop

// Run polls the download client until ctx is cancelled.
func (a *App) Run(ctx context.Context) {
	t := time.NewTicker(5 * time.Second)
	defer t.Stop()
	for {
		func() {
			defer diag.Recover("download monitoring")
			a.syncDownloads(ctx)
		}()
		if !a.startScan {
			a.startScan = true
			a.reportRecovery()
			if a.Store.Settings().SetupDone {
				// the folders are read at each launch: files added, moved or
				// deleted by hand while Cinearr was closed are taken into account
				a.StartScan()
			}
		}
		if time.Since(a.lastRecycle) > 6*time.Hour {
			a.lastRecycle = time.Now()
			diag.Go("recycle bin cleanup", a.cleanRecycle)
		}
		if time.Since(a.lastSeriesCheck) > time.Hour {
			a.lastSeriesCheck = time.Now()
			diag.Go("series refresh", func() { a.refreshStaleSeries(ctx) })
		}
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
	}
}

func errString(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}

// SearchResult is returned to the interactive search.
type SearchResult struct {
	Decisions []decision.Decision     `json:"decisions"`
	Indexers  []indexer.IndexerResult `json:"indexers"`
}

// enabledIndexers returns the indexers to query.
func (a *App) enabledIndexers() ([]indexer.Config, error) {
	var enabled []indexer.Config
	for _, c := range a.Store.Settings().Indexers {
		if c.Enabled {
			enabled = append(enabled, c)
		}
	}
	if len(enabled) == 0 {
		return nil, i18n.New("no indexer enabled: add one in Indexers")
	}
	return enabled, nil
}

func countResults(rs []indexer.IndexerResult) int {
	n := 0
	for _, r := range rs {
		n += r.Count
	}
	return n
}
