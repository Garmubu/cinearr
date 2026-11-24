package indexer

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"cinearr/internal/backoff"
	"cinearr/internal/diag"
	"cinearr/internal/i18n"
	"cinearr/internal/indexer/cardigann"
	"cinearr/internal/model"
	"cinearr/internal/proxy"
)

// Config is a configured indexer (persisted in settings).
type Config struct {
	ID           string            `json:"id"`
	Kind         string            `json:"kind"` // "cardigann" or "torznab"
	DefinitionID string            `json:"definitionId,omitempty"`
	Name         string            `json:"name"`
	Enabled      bool              `json:"enabled"`
	Priority     int               `json:"priority"` // lower is preferred (Radarr: 1-50, default 25)
	BaseURL      string            `json:"baseUrl,omitempty"`
	APIKey       string            `json:"apiKey,omitempty"`
	Settings     map[string]string `json:"settings,omitempty"`
	// NoProxy reaches this site directly even when a proxy is set (some
	// private trackers refuse VPN addresses).
	NoProxy bool `json:"noProxy,omitempty"`
}

// Searcher is implemented by Cardigann and Torznab indexers.
type Searcher interface {
	Search(ctx context.Context, q cardigann.Query) ([]model.Release, error)
	Download(ctx context.Context, link string) (*cardigann.DownloadResult, error)
}

// Manager builds and caches indexer instances.
type Manager struct {
	Repo *Repository
	mu   sync.Mutex
	inst map[string]cachedInstance
	// Status pauses the indexers that keep failing (automatic searches).
	Status backoff.Book
}

type cachedInstance struct {
	cfg Config
	s   Searcher
}

// NewManager creates a manager.
func NewManager(repo *Repository) *Manager {
	return &Manager{Repo: repo, inst: map[string]cachedInstance{}}
}

func sameConfig(a, b Config) bool {
	if a.Kind != b.Kind || a.NoProxy != b.NoProxy || a.DefinitionID != b.DefinitionID || a.BaseURL != b.BaseURL || a.APIKey != b.APIKey || a.Priority != b.Priority || a.Name != b.Name || len(a.Settings) != len(b.Settings) {
		return false
	}
	for k, v := range a.Settings {
		if b.Settings[k] != v {
			return false
		}
	}
	return true
}

// Get returns (and caches) the instance for cfg.
func (m *Manager) Get(ctx context.Context, cfg Config) (Searcher, error) {
	m.mu.Lock()
	if c, ok := m.inst[cfg.ID]; ok && sameConfig(c.cfg, cfg) {
		m.mu.Unlock()
		return c.s, nil
	}
	m.mu.Unlock()
	var s Searcher
	switch cfg.Kind {
	case "torznab":
		t := NewTorznab(cfg.ID, cfg.Name, cfg.BaseURL, cfg.APIKey)
		t.Priority = cfg.Priority
		t.SetTransport(proxy.Transport(proxy.Indexers, !cfg.NoProxy))
		s = t
	case "cardigann", "":
		def, err := m.Repo.Get(ctx, cfg.DefinitionID)
		if err != nil {
			return nil, err
		}
		ix := cardigann.New(cfg.ID, def, cfg.Settings, cfg.BaseURL)
		if cfg.Name != "" {
			ix.Name = cfg.Name
		}
		ix.Priority = cfg.Priority
		ix.SetTransport(proxy.Transport(proxy.Indexers, !cfg.NoProxy))
		s = ix
	default:
		return nil, fmt.Errorf("unknown indexer type: %s", cfg.Kind)
	}
	m.mu.Lock()
	m.inst[cfg.ID] = cachedInstance{cfg, s}
	m.mu.Unlock()
	return s, nil
}

// Forget drops a cached instance (after a settings change or removal).
func (m *Manager) Forget(id string) {
	m.mu.Lock()
	delete(m.inst, id)
	m.mu.Unlock()
}

// IndexerResult summarises one indexer's answer.
type IndexerResult struct {
	ID       string        `json:"id"`
	Name     string        `json:"name"`
	Count    int           `json:"count"`
	Error    string        `json:"error,omitempty"`
	Paused   bool          `json:"paused,omitempty"` // skipped: too many failures in a row
	Duration time.Duration `json:"duration"`
}

// MovieQuery describes the film to look for.
type MovieQuery struct {
	Title         string
	OriginalTitle string
	EnglishTitle  string
	Year          int
	ImdbID        string
	TmdbID        int
}

// SearchMovie queries every enabled indexer concurrently. Indexers that
// support IMDb ids are searched by id; the others by "title year" (local
// title and original title when they differ), as Radarr does.
func (m *Manager) SearchMovie(ctx context.Context, cfgs []Config, mq MovieQuery) ([]model.Release, []IndexerResult) {
	return m.searchAll(ctx, cfgs, func(ctx context.Context, s Searcher) ([]model.Release, error) { return searchOne(ctx, s, mq) })
}

// TVQuery describes the episode (or the season, Episode 0) to look for.
type TVQuery struct {
	Title         string
	OriginalTitle string
	EnglishTitle  string
	ImdbID        string
	TvdbID        int
	TmdbID        int
	Season        int
	Episode       int
}

// SearchTV queries every enabled indexer for an episode or a season, by id
// when the site supports it, otherwise by "title S02E05" (Sonarr through
// Prowlarr).
func (m *Manager) SearchTV(ctx context.Context, cfgs []Config, tq TVQuery) ([]model.Release, []IndexerResult) {
	return m.searchAll(ctx, cfgs, func(ctx context.Context, s Searcher) ([]model.Release, error) { return searchOneTV(ctx, s, tq) })
}

func (m *Manager) searchAll(ctx context.Context, cfgs []Config, one func(context.Context, Searcher) ([]model.Release, error)) ([]model.Release, []IndexerResult) {
	var (
		mu      sync.Mutex
		all     []model.Release
		results []IndexerResult
		wg      sync.WaitGroup
	)
	for _, cfg := range cfgs {
		if !cfg.Enabled {
			continue
		}
		if st, paused := m.Status.Paused(cfg.ID); paused && backoff.IsAutomatic(ctx) {
			mu.Lock()
			results = append(results, IndexerResult{ID: cfg.ID, Name: cfg.Name, Paused: true,
				Error: i18n.Tf("paused until %s after %d failures in a row", st.PausedUntil.Format("15:04"), st.Failures)})
			mu.Unlock()
			continue
		}
		wg.Add(1)
		go func(cfg Config) {
			defer wg.Done()
			defer diag.Recover("indexer " + cfg.Name)
			start := time.Now()
			res := IndexerResult{ID: cfg.ID, Name: cfg.Name}
			cctx, cancel := context.WithTimeout(ctx, 60*time.Second)
			defer cancel()
			s, err := m.Get(cctx, cfg)
			var rels []model.Release
			if err == nil {
				rels, err = one(cctx, s)
			}
			res.Duration = time.Since(start)
			switch {
			case err != nil && ctx.Err() != nil:
				res.Error = err.Error() // search cancelled: not the site's fault
			case err != nil:
				res.Error = err.Error()
				st := m.Status.Fail(cfg.ID, err.Error())
				diag.Printf("[indexer] %s: %s (failure %d in a row, automatic searches paused until %s)", cfg.Name,
					diag.RedactText(err.Error()), st.Failures, st.PausedUntil.Format("15:04:05"))
			default:
				m.Status.OK(cfg.ID)
				diag.Debugf("[indexer] %s: %d results in %s", cfg.Name, len(rels), res.Duration.Round(time.Millisecond))
			}
			res.Count = len(rels)
			mu.Lock()
			all = append(all, rels...)
			results = append(results, res)
			mu.Unlock()
		}(cfg)
	}
	wg.Wait()
	return dedupe(all), results
}

// searchOne searches a film by IMDb id when the site supports it and, in
// any case, by "title year" for the local, English and original titles.
func searchOne(ctx context.Context, s Searcher, mq MovieQuery) ([]model.Release, error) {
	cats := []int{2000}
	var idQueries []cardigann.Query
	if ix, ok := s.(*cardigann.Indexer); ok {
		if mq.ImdbID != "" && ix.Def.SupportsImdbMovieSearch() {
			idQueries = append(idQueries, cardigann.Query{ImdbID: mq.ImdbID, TmdbID: mq.TmdbID, Categories: cats})
		}
	} else if _, ok := s.(*Torznab); ok && mq.ImdbID != "" {
		idQueries = append(idQueries, cardigann.Query{ImdbID: mq.ImdbID, TmdbID: mq.TmdbID, Keywords: kw(mq.Title, mq.Year), Categories: cats})
	}
	var keywordQueries []cardigann.Query
	seen := map[string]bool{}
	for _, t := range []string{mq.Title, mq.EnglishTitle, mq.OriginalTitle} {
		k := kw(t, mq.Year)
		if t == "" || seen[strings.ToLower(k)] {
			continue
		}
		seen[strings.ToLower(k)] = true
		keywordQueries = append(keywordQueries, cardigann.Query{Keywords: k, Categories: cats})
	}
	return runQueries(ctx, s, idQueries, keywordQueries)
}

// tvCategories are the Newznab series categories (TV and its sub-categories
// are mapped by the definitions).
var tvCategories = []int{5000}

func searchOneTV(ctx context.Context, s Searcher, tq TVQuery) ([]model.Release, error) {
	base := cardigann.Query{TV: true, Season: tq.Season, Episode: tq.Episode, Categories: tvCategories}
	var idQueries []cardigann.Query
	if ix, ok := s.(*cardigann.Indexer); ok {
		q := base
		byID := false
		if tq.ImdbID != "" && ix.Def.SupportsTVSearch("imdbid") {
			q.ImdbID, byID = tq.ImdbID, true
		}
		if tq.TvdbID > 0 && ix.Def.SupportsTVSearch("tvdbid") {
			q.TvdbID, byID = tq.TvdbID, true
		}
		if byID {
			idQueries = append(idQueries, q)
		}
	} else if _, ok := s.(*Torznab); ok {
		q := base
		q.ImdbID, q.TvdbID, q.TmdbID, q.Keywords = tq.ImdbID, tq.TvdbID, tq.TmdbID, kw(tq.Title, 0)
		idQueries = append(idQueries, q)
	}
	var keywordQueries []cardigann.Query
	seen := map[string]bool{}
	for _, t := range []string{tq.Title, tq.EnglishTitle, tq.OriginalTitle} {
		k := kw(t, 0)
		if t == "" || seen[strings.ToLower(k)] {
			continue
		}
		seen[strings.ToLower(k)] = true
		q := base
		q.Keywords = k
		keywordQueries = append(keywordQueries, q)
	}
	return runQueries(ctx, s, idQueries, keywordQueries)
}

// runQueries runs the id queries and the title queries: a site may index
// only some releases under the IMDb id (the others, often those named with
// the English title, are found by title), or ignore an id it does not know
// and return unrelated releases.
func runQueries(ctx context.Context, s Searcher, idQueries, keywordQueries []cardigann.Query) ([]model.Release, error) {
	var out []model.Release
	var errs []error
	run := func(qs []cardigann.Query) {
		for _, q := range qs {
			r, err := s.Search(ctx, q)
			if err != nil {
				errs = append(errs, err)
				continue
			}
			out = append(out, r...)
		}
	}
	run(idQueries)
	run(keywordQueries)
	if len(out) == 0 && len(errs) > 0 {
		return nil, errors.Join(errs...)
	}
	return out, nil
}

func kw(title string, year int) string {
	// characters most trackers choke on
	t := strings.NewReplacer(":", " ", "'", " ", "’", " ", "&", "and", "!", "", "?", "").Replace(title)
	t = strings.Join(strings.Fields(t), " ")
	if year > 0 {
		return fmt.Sprintf("%s %d", t, year)
	}
	return t
}

func dedupe(rels []model.Release) []model.Release {
	seen := map[string]bool{}
	var out []model.Release
	for _, r := range rels {
		key := r.IndexerID + "|" + r.GUID
		if r.InfoHash != "" {
			key = "hash|" + strings.ToUpper(r.InfoHash)
		}
		if seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, r)
	}
	return out
}
