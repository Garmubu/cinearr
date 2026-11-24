// Package store persists settings and the library as JSON files in the
// data folder (atomic writes, no database to install).
package store

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"sort"
	"sync"
	"time"

	"cinearr/internal/decision"
	"cinearr/internal/dlclient"
	"cinearr/internal/indexer"
	"cinearr/internal/model"
	"cinearr/internal/proxy"
	"cinearr/internal/subtitles"
)

// SubtitleSettings configure automatic subtitles.
type SubtitleSettings struct {
	Auto            bool     `json:"auto"`
	Languages       []string `json:"languages"` // ISO 639-1, by preference
	HearingImpaired bool     `json:"hearingImpaired"`
	MinPercent      int      `json:"minPercent"`
	// Providers chosen by the user, with their settings (as in Bazarr).
	Providers []subtitles.Config `json:"providers"`

	// fields of versions before the choice of providers, migrated on load
	OpenSubtitlesKey  string `json:"openSubtitlesKey,omitempty"`
	OpenSubtitlesUser string `json:"openSubtitlesUser,omitempty"`
	OpenSubtitlesPass string `json:"openSubtitlesPass,omitempty"`
	SubDLKey          string `json:"subdlKey,omitempty"`
	IncludeAI         bool   `json:"includeAI,omitempty"`
}

// PlayerSettings configure the video player.
type PlayerSettings struct {
	// Kind is the chosen player (vlc, mpc-hc, mpv), "custom" for Path, or
	// "" to use the first one found.
	Kind       string `json:"kind"`
	Path       string `json:"path"` // executable for "custom" (or a forced path)
	Fullscreen bool   `json:"fullscreen"`
	AudioLang  string `json:"audioLang"` // e.g. "fre,fra,fr,eng,en"
	ExtraArgs  string `json:"extraArgs"`
	Resume     bool   `json:"resume"`
}

// PathMapping translates a path reported by a torrent client running on
// another machine.
type PathMapping struct {
	Remote string `json:"remote"`
	Local  string `json:"local"`
}

// Settings is the whole configuration.
type Settings struct {
	SetupDone bool `json:"setupDone"`
	// UILanguage is the language of the interface (en, fr, es, it, de).
	UILanguage string `json:"uiLanguage"`
	// Language is the language of the film pages (TMDB, e.g. fr-FR).
	Language string            `json:"language"`
	TMDBKey  string            `json:"tmdbKey"`
	Client   dlclient.Settings `json:"downloadClient"`
	// LegacyQBit is the qBittorrent block of versions before the choice of
	// client; it is migrated on load.
	LegacyQBit  *dlclient.Settings `json:"qbit,omitempty"`
	LibraryPath string             `json:"libraryPath"`
	// SeriesPath is the root folder of the series (Sonarr's root folder).
	SeriesPath        string `json:"seriesPath"`
	ImportMode        string `json:"importMode"` // hardlink (falls back to copy), copy, move
	RemoveAfterImport bool   `json:"removeAfterImport"`
	// MinFreeSpaceMB is the space left free when a file is copied into the
	// library (Radarr's and Sonarr's minimum free space, 100 MB).
	MinFreeSpaceMB int `json:"minFreeSpaceMB"`
	// RecycleDays keeps the files deleted from the library in a recycle bin
	// for this many days (0 = deleted at once).
	RecycleDays int `json:"recycleDays"`
	// RejectedExtensions are refused in the downloads of films and series,
	// in addition to the built-in dangerous and executable lists (Sonarr's
	// "Additional Rejected File Extensions").
	RejectedExtensions []string      `json:"rejectedExtensions"`
	PathMappings       []PathMapping `json:"pathMappings"`
	// SearchOnAdd searches and downloads as soon as a film is added (off by
	// default: the user starts the search from the film page).
	SearchOnAdd bool `json:"searchOnAdd"`
	// AutoGrab looks for another release when a download fails.
	AutoGrab  bool             `json:"autoGrab"`
	Profile   decision.Profile `json:"profile"`
	Indexers  []indexer.Config `json:"indexers"`
	Subtitles SubtitleSettings `json:"subtitles"`
	Player    PlayerSettings   `json:"player"`
	// Proxy sends the indexer requests (and optionally TMDB and the
	// subtitles) through an HTTP or SOCKS5 proxy, for instance a VPN's.
	Proxy proxy.Settings `json:"proxy"`
	Port  int            `json:"port"`
	Debug bool           `json:"debug"` // verbose log
}

// Defaults returns a first-run configuration.
func Defaults() Settings {
	return Settings{
		Language:           "en-US",
		Client:             dlclient.Settings{Kind: dlclient.QBittorrent, URL: "http://127.0.0.1:8080", Category: "cinearr", RatioLimit: -2, SeedingTimeLimit: -2},
		ImportMode:         "hardlink",
		MinFreeSpaceMB:     100,
		RecycleDays:        7,
		RejectedExtensions: []string{},
		AutoGrab:           true,
		Profile:            decision.ProfileForLanguage("English"),
		Subtitles:          SubtitleSettings{Auto: true, Languages: []string{"en"}, MinPercent: 50, Providers: []subtitles.Config{}},
		Player:             PlayerSettings{Fullscreen: false, AudioLang: "eng,en", Resume: true},
		Port:               7979,
		Proxy:              proxy.Defaults(),
	}
}

// migrate converts settings written by older versions.
func (st *Settings) migrate() {
	if st.LegacyQBit != nil {
		// files written before the client choice only had "qbit"
		if st.Client.URL == "" || st.Client == Defaults().Client {
			st.Client = *st.LegacyQBit
			st.Client.Kind = dlclient.QBittorrent
		}
		st.LegacyQBit = nil
	}
	if st.Client.Kind == "" {
		st.Client.Kind = dlclient.QBittorrent
	}
	// versions before the choice of language were in French only
	if st.SetupDone && st.UILanguage == "" {
		st.UILanguage = "fr"
	}
	sub := &st.Subtitles
	if sub.OpenSubtitlesKey != "" || sub.OpenSubtitlesUser != "" {
		sub.Providers = append(sub.Providers, subtitles.Config{ID: "opensubtitles", Enabled: true, Settings: map[string]string{
			"apiKey": sub.OpenSubtitlesKey, "username": sub.OpenSubtitlesUser, "password": sub.OpenSubtitlesPass,
			"includeAI": fmt.Sprint(sub.IncludeAI)}})
	}
	if sub.SubDLKey != "" {
		sub.Providers = append(sub.Providers, subtitles.Config{ID: "subdl", Enabled: true, Settings: map[string]string{"apiKey": sub.SubDLKey}})
	}
	sub.OpenSubtitlesKey, sub.OpenSubtitlesUser, sub.OpenSubtitlesPass, sub.SubDLKey, sub.IncludeAI = "", "", "", "", false
	if sub.Providers == nil {
		sub.Providers = []subtitles.Config{}
	}
}

// Store holds the persisted state.
type Store struct {
	dir       string
	mu        sync.RWMutex
	settings  Settings
	movies    map[int]*model.Movie
	series    map[int]*model.Series
	blocklist []model.BlocklistEntry
	history   []model.HistoryEvent
	recovered []Recovery // data files that could not be read at startup
}

// DataDir returns the data folder: "data" next to the executable when it
// exists (portable mode), otherwise the per-user configuration folder.
func DataDir() string {
	if exe, err := os.Executable(); err == nil {
		portable := filepath.Join(filepath.Dir(exe), "data")
		if st, err := os.Stat(portable); err == nil && st.IsDir() {
			return portable
		}
	}
	base, err := os.UserConfigDir()
	if err != nil {
		base = "."
	}
	name := "Cinearr"
	if runtime.GOOS == "linux" {
		name = "cinearr"
	}
	return filepath.Join(base, name)
}

// Open loads (or initialises) the store in dir.
func Open(dir string) (*Store, error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, err
	}
	s := &Store{dir: dir, settings: Defaults(), movies: map[int]*model.Movie{}, series: map[int]*model.Series{}}
	if err := readJSON(filepath.Join(dir, "settings.json"), &s.settings); err != nil && !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}
	s.settings.migrate()
	if s.settings.Profile.Allowed == nil {
		s.settings.Profile = decision.DefaultProfile()
	}
	// an unreadable library does not stop Cinearr: the damaged file is set
	// aside, the entries still readable are kept and the scan of the
	// launch rebuilds the rest from the films and series folders
	var movies []*model.Movie
	s.note(readList(dir, "library.json", &movies))
	for _, m := range movies {
		if m != nil && m.TmdbID != 0 {
			s.movies[m.TmdbID] = m
		}
	}
	var series []*model.Series
	s.note(readList(dir, "series.json", &series))
	for _, x := range series {
		if x != nil && x.TmdbID != 0 {
			s.series[x.TmdbID] = x
		}
	}
	s.note(readList(dir, "blocklist.json", &s.blocklist))
	s.note(readList(dir, "history.json", &s.history))
	return s, nil
}

// Recovery describes a data file that could not be read at startup.
type Recovery struct {
	File  string // "library.json", "series.json", …
	Err   string // why it could not be read
	Kept  int    // entries still readable, kept
	Aside string // name of the damaged copy kept in the data folder ("" if none)
}

// Recovered lists the data files that could not be read at startup.
func (s *Store) Recovered() []Recovery {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return slices.Clone(s.recovered)
}

func (s *Store) note(r *Recovery) {
	if r != nil {
		s.recovered = append(s.recovered, *r)
	}
}

// readList reads a JSON array. A file that cannot be read or decoded is
// set aside ("<name>.unreadable-<date>") and the elements decoded before
// the damaged part are kept.
func readList[T any](dir, name string, out *[]T) *Recovery {
	path := filepath.Join(dir, name)
	b, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err == nil {
		if err = json.Unmarshal(b, out); err == nil {
			return nil
		}
		*out = salvage[T](b)
	}
	r := &Recovery{File: name, Err: err.Error(), Kept: len(*out)}
	aside := path + ".unreadable-" + time.Now().Format("20060102-150405")
	if os.Rename(path, aside) == nil || (b != nil && os.WriteFile(aside, b, 0o600) == nil) {
		r.Aside = filepath.Base(aside)
	}
	return r
}

// salvage decodes the elements of a damaged JSON array up to the first
// one that cannot be read.
func salvage[T any](b []byte) []T {
	dec := json.NewDecoder(bytes.NewReader(b))
	if t, err := dec.Token(); err != nil || t != json.Delim('[') {
		return nil
	}
	var out []T
	for dec.More() {
		var v T
		if dec.Decode(&v) != nil {
			break
		}
		out = append(out, v)
	}
	return out
}

// Dir returns the data folder.
func (s *Store) Dir() string { return s.dir }

func readJSON(path string, v any) error {
	b, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	return json.Unmarshal(b, v)
}

func writeJSON(path string, v any) error {
	b, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, b, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

// Settings returns a copy of the settings.
func (s *Store) Settings() Settings {
	s.mu.RLock()
	defer s.mu.RUnlock()
	b, _ := json.Marshal(s.settings)
	var c Settings
	_ = json.Unmarshal(b, &c)
	return c
}

// SaveSettings replaces the settings.
func (s *Store) SaveSettings(v Settings) error {
	v.migrate()
	s.mu.Lock()
	defer s.mu.Unlock()
	s.settings = v
	return writeJSON(filepath.Join(s.dir, "settings.json"), v)
}

// Movies returns copies of all movies, most recently added first.
func (s *Store) Movies() []model.Movie {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]model.Movie, 0, len(s.movies))
	for _, m := range s.movies {
		out = append(out, cloneMovie(m))
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Added.After(out[j].Added) })
	return out
}

// Movie returns a copy of one movie.
func (s *Store) Movie(id int) (model.Movie, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	m, ok := s.movies[id]
	if !ok {
		return model.Movie{}, false
	}
	return cloneMovie(m), true
}

func (s *Store) saveMoviesLocked() error {
	list := make([]*model.Movie, 0, len(s.movies))
	for _, m := range s.movies {
		list = append(list, m)
	}
	sort.Slice(list, func(i, j int) bool { return list[i].TmdbID < list[j].TmdbID })
	return writeJSON(filepath.Join(s.dir, "library.json"), list)
}

// PutMovie inserts or replaces a movie.
func (s *Store) PutMovie(m model.Movie) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.movies[m.TmdbID] = &m
	return s.saveMoviesLocked()
}

// UpdateMovie applies fn to a stored movie atomically.
func (s *Store) UpdateMovie(id int, fn func(*model.Movie)) (model.Movie, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	m, ok := s.movies[id]
	if !ok {
		return model.Movie{}, false
	}
	if changed(m, fn) {
		_ = s.saveMoviesLocked()
	}
	return cloneMovie(m), true
}

// DeleteMovie removes a movie from the library.
func (s *Store) DeleteMovie(id int) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.movies, id)
	return s.saveMoviesLocked()
}

// AllSeries returns copies of all series, most recently added first.
func (s *Store) AllSeries() []model.Series {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]model.Series, 0, len(s.series))
	for _, x := range s.series {
		out = append(out, cloneSeries(x))
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Added.After(out[j].Added) })
	return out
}

// Series returns a copy of one series.
func (s *Store) Series(id int) (model.Series, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	x, ok := s.series[id]
	if !ok {
		return model.Series{}, false
	}
	return cloneSeries(x), true
}

// cloneSeries deep-copies a series (seasons and episodes are slices).
func cloneSeries(x *model.Series) model.Series {
	c := *x
	c.AlternativeTitles, c.Genres = slices.Clone(x.AlternativeTitles), slices.Clone(x.Genres)
	c.Seasons = make([]model.Season, len(x.Seasons))
	for i, se := range x.Seasons {
		se.Episodes = slices.Clone(se.Episodes)
		for j := range se.Episodes {
			e := &se.Episodes[j]
			e.File = cloneFile(e.File)
			e.Subtitles = slices.Clone(e.Subtitles)
		}
		c.Seasons[i] = se
	}
	c.Downloads = make([]model.SeriesDownload, len(x.Downloads))
	for i, d := range x.Downloads {
		d.Episodes = slices.Clone(d.Episodes)
		c.Downloads[i] = d
	}
	return c
}

// cloneMovie deep-copies a movie: callers get data they can read while the
// monitoring updates the store.
func cloneMovie(m *model.Movie) model.Movie {
	c := *m
	c.AlternativeTitles, c.Genres, c.Subtitles = slices.Clone(m.AlternativeTitles), slices.Clone(m.Genres), slices.Clone(m.Subtitles)
	c.File = cloneFile(m.File)
	if m.Download != nil {
		d := *m.Download
		c.Download = &d
	}
	return c
}

func cloneFile(f *model.MovieFile) *model.MovieFile {
	if f == nil {
		return nil
	}
	c := *f
	c.Languages = slices.Clone(f.Languages)
	return &c
}

// changed reports whether fn modified v (to skip writing unchanged files:
// a paused download is polled every five seconds).
func changed[T any](v *T, fn func(*T)) bool {
	before, _ := json.Marshal(v)
	fn(v)
	after, _ := json.Marshal(v)
	return string(before) != string(after)
}

func (s *Store) saveSeriesLocked() error {
	list := make([]*model.Series, 0, len(s.series))
	for _, x := range s.series {
		list = append(list, x)
	}
	sort.Slice(list, func(i, j int) bool { return list[i].TmdbID < list[j].TmdbID })
	return writeJSON(filepath.Join(s.dir, "series.json"), list)
}

// PutSeries inserts or replaces a series.
func (s *Store) PutSeries(x model.Series) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.series[x.TmdbID] = &x
	return s.saveSeriesLocked()
}

// UpdateSeries applies fn to a stored series atomically.
func (s *Store) UpdateSeries(id int, fn func(*model.Series)) (model.Series, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	x, ok := s.series[id]
	if !ok {
		return model.Series{}, false
	}
	if changed(x, fn) {
		_ = s.saveSeriesLocked()
	}
	return cloneSeries(x), true
}

// DeleteSeries removes a series from the library.
func (s *Store) DeleteSeries(id int) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.series, id)
	return s.saveSeriesLocked()
}

// ResetOptions choose what Reset removes besides the settings.
type ResetOptions struct {
	// Library also empties the lists of films and series (the files on
	// disk are never deleted).
	Library bool `json:"library"`
}

// Reset returns to a first-run state: default settings (the setup wizard
// starts again), no indexers, empty history and blocklist. The films and
// series stay in the library unless opts.Library is set.
func (s *Store) Reset(opts ResetOptions) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.settings = Defaults()
	if err := writeJSON(filepath.Join(s.dir, "settings.json"), s.settings); err != nil {
		return err
	}
	s.history, s.blocklist = nil, nil
	_ = writeJSON(filepath.Join(s.dir, "history.json"), []model.HistoryEvent{})
	_ = writeJSON(filepath.Join(s.dir, "blocklist.json"), []model.BlocklistEntry{})
	if opts.Library {
		s.movies = map[int]*model.Movie{}
		s.series = map[int]*model.Series{}
		_ = s.saveMoviesLocked()
		_ = s.saveSeriesLocked()
	}
	return nil
}

// Blocklist returns the blocklist.
func (s *Store) Blocklist() []model.BlocklistEntry {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return append([]model.BlocklistEntry{}, s.blocklist...)
}

// AddBlocklist appends an entry.
func (s *Store) AddBlocklist(e model.BlocklistEntry) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if e.Date.IsZero() {
		e.Date = time.Now()
	}
	s.blocklist = append(s.blocklist, e)
	return writeJSON(filepath.Join(s.dir, "blocklist.json"), s.blocklist)
}

// RemoveBlocklist deletes entry i.
func (s *Store) RemoveBlocklist(i int) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if i < 0 || i >= len(s.blocklist) {
		return errors.New("entry not found")
	}
	s.blocklist = append(s.blocklist[:i], s.blocklist[i+1:]...)
	return writeJSON(filepath.Join(s.dir, "blocklist.json"), s.blocklist)
}

// AddHistory records an event (the last 500 are kept).
func (s *Store) AddHistory(e model.HistoryEvent) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if e.Date.IsZero() {
		e.Date = time.Now()
	}
	s.history = append(s.history, e)
	if len(s.history) > 500 {
		s.history = s.history[len(s.history)-500:]
	}
	_ = writeJSON(filepath.Join(s.dir, "history.json"), s.history)
}

// History returns events, newest first.
func (s *Store) History() []model.HistoryEvent {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]model.HistoryEvent, len(s.history))
	for i, e := range s.history {
		out[len(s.history)-1-i] = e
	}
	return out
}
