package api

import (
	"archive/zip"
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"cinearr/internal/app"
	"cinearr/internal/desktop"
	"cinearr/internal/diag"
	"cinearr/internal/dlclient"
	"cinearr/internal/player"
	"cinearr/internal/tmdb"
)

// redactSettings replaces secrets (and identifiers) in a JSON value.
func redactSettings(v any) any {
	switch x := v.(type) {
	case map[string]any:
		out := map[string]any{}
		for k, val := range x {
			lk := strings.ToLower(k)
			if s, ok := val.(string); ok && s != "" && (diag.IsSecret(k) || strings.Contains(lk, "user") || strings.Contains(lk, "login") || strings.Contains(lk, "email")) {
				out[k] = fmt.Sprintf("*** (%d characters)", len(s))
				continue
			}
			out[k] = redactSettings(val)
		}
		return out
	case []any:
		out := make([]any, len(x))
		for i, e := range x {
			out[i] = redactSettings(e)
		}
		return out
	}
	return v
}

func (s *Server) diagnosticInfo() string {
	st := s.App.Store.Settings()
	var b strings.Builder
	fmt.Fprintf(&b, "Cinearr %s\nDate: %s\n", Version, time.Now().Format(time.RFC3339))
	b.WriteString(diag.SystemInfo())
	fmt.Fprintf(&b, "Data folder: %s\n", s.App.Store.Dir())
	fmt.Fprintf(&b, "Detailed log: %v\n", diag.Debug())
	fmt.Fprintf(&b, "Setup done: %v\n", st.SetupDone)
	if p, err := player.Find(st.Player.Kind, st.Player.Path, s.App.Store.Dir()); err == nil {
		fmt.Fprintf(&b, "Player: %s (choice: %q, type: %s)\n", p, st.Player.Kind, player.Family(p))
	} else {
		fmt.Fprintf(&b, "Player: %v\n", err)
	}
	if br := desktop.FindBrowser(); br != "" {
		fmt.Fprintf(&b, "Window: %s\n", br)
	} else {
		b.WriteString("Window: no Chromium browser\n")
	}
	last, syncErr := s.App.SyncStatus()
	fmt.Fprintf(&b, "Torrent client: %s %s, last contact %s, error: %q\n", dlclient.Name(st.Client.Kind), diag.RedactURL(st.Client.URL), last.Format(time.RFC3339), syncErr)
	fmt.Fprintf(&b, "TMDB configured: %v, built-in key: %v (language %s)\n", st.TMDBKey != "", tmdb.BuiltinKey != "", st.Language)
	for _, p := range app.KeyProblems(st) {
		fmt.Fprintf(&b, "Built-in key refused: %s\n", p.Service)
	}
	fmt.Fprintf(&b, "Subtitle providers: %d\n", len(s.App.SubtitleProviders()))
	fmt.Fprintf(&b, "Library: %s, series: %s, import %s\n", st.LibraryPath, st.SeriesPath, st.ImportMode)
	fmt.Fprintf(&b, "Indexers: %d\n", len(st.Indexers))
	for _, c := range st.Indexers {
		fmt.Fprintf(&b, "  - %s [%s %s] enabled=%v priority=%d url=%s\n", c.Name, c.Kind, c.DefinitionID, c.Enabled, c.Priority, diag.RedactURL(c.BaseURL))
	}
	counts := map[string]int{}
	for _, m := range s.App.Store.Movies() {
		counts[m.Status]++
	}
	fmt.Fprintf(&b, "Films: %v\n", counts)
	fmt.Fprintf(&b, "Series: %d\n", len(s.App.Store.AllSeries()))
	if report, when := diag.CrashReport(); report != "" {
		fmt.Fprintf(&b, "Crash recorded: %s\n", when.Format(time.RFC3339))
	}
	return b.String()
}

// diagnostic streams a zip with everything useful to analyse a problem,
// secrets removed.
func (s *Server) diagnostic(w http.ResponseWriter, r *http.Request) {
	name := "cinearr-diagnostic-" + time.Now().Format("20060102-150405") + ".zip"
	w.Header().Set("Content-Type", "application/zip")
	w.Header().Set("Content-Disposition", `attachment; filename="`+name+`"`)
	zw := zip.NewWriter(w)
	defer zw.Close()
	add := func(n string, data []byte) {
		f, err := zw.Create(n)
		if err == nil {
			_, _ = f.Write(data)
		}
	}
	addJSON := func(n string, v any) {
		b, _ := json.MarshalIndent(v, "", "  ")
		add(n, b)
	}
	dir := s.App.Store.Dir()

	add("info.txt", []byte(s.diagnosticInfo()))

	for _, f := range []string{diag.PrevLogFile, diag.LogFile} {
		if b, err := os.ReadFile(filepath.Join(dir, f)); err == nil {
			add(f, []byte(redactLog(string(b))))
		}
	}
	crashes, _ := filepath.Glob(filepath.Join(dir, "crash*.log"))
	sort.Strings(crashes)
	if len(crashes) > 6 {
		crashes = crashes[len(crashes)-6:]
	}
	for _, f := range crashes {
		if b, err := os.ReadFile(f); err == nil && len(b) > 0 {
			add(filepath.Base(f), []byte(redactLog(string(b))))
		}
	}

	var settings any
	raw, _ := json.Marshal(s.App.Store.Settings())
	_ = json.Unmarshal(raw, &settings)
	addJSON("settings.json", redactSettings(settings))

	type movieSummary struct {
		TmdbID  int    `json:"tmdbId"`
		Title   string `json:"title"`
		Year    int    `json:"year"`
		Status  string `json:"status"`
		Message string `json:"statusMessage,omitempty"`
		Release string `json:"downloadRelease,omitempty"`
		Indexer string `json:"downloadIndexer,omitempty"`
		State   string `json:"downloadState,omitempty"`
		Checked bool   `json:"downloadChecked,omitempty"`
		Quality string `json:"fileQuality,omitempty"`
		Path    string `json:"filePath,omitempty"`
		Subs    int    `json:"subtitles"`
	}
	var lib []movieSummary
	for _, m := range s.App.Store.Movies() {
		ms := movieSummary{TmdbID: m.TmdbID, Title: m.Title, Year: m.Year, Status: m.Status, Message: m.StatusMessage, Subs: len(m.Subtitles)}
		if d := m.Download; d != nil {
			ms.Release, ms.Indexer, ms.State, ms.Checked = d.ReleaseTitle, d.Indexer, d.State, d.Checked
		}
		if f := m.File; f != nil {
			ms.Quality, ms.Path = f.QualityName, f.Path
		}
		lib = append(lib, ms)
	}
	addJSON("library.json", lib)
	type seriesDiag struct {
		Summary   seriesSummary    `json:"summary"`
		Problems  []map[string]any `json:"episodesWithMessages,omitempty"`
		Downloads int              `json:"downloads"`
	}
	var series []seriesDiag
	for _, x := range s.App.Store.AllSeries() {
		d := seriesDiag{Summary: summarize(x), Downloads: len(x.Downloads)}
		d.Summary.Downloads = x.Downloads
		for _, se := range x.Seasons {
			for _, e := range se.Episodes {
				if e.StatusMessage != "" {
					d.Problems = append(d.Problems, map[string]any{"season": e.Season, "episode": e.Number, "status": e.Status, "message": e.StatusMessage})
				}
			}
		}
		series = append(series, d)
	}
	addJSON("series.json", series)
	h := s.App.Store.History()
	if len(h) > 300 {
		h = h[:300]
	}
	addJSON("history.json", h)
	addJSON("blocklist.json", s.App.Store.Blocklist())
	diag.Printf("[diag] diagnostic exported (%s)", name)
}

func redactLog(s string) string {
	var b strings.Builder
	sc := bufio.NewScanner(strings.NewReader(s))
	sc.Buffer(make([]byte, 1<<20), 1<<20)
	for sc.Scan() {
		b.WriteString(diag.RedactText(sc.Text()))
		b.WriteByte('\n')
	}
	return b.String()
}

// tailLog returns the last n lines of the log file.
func (s *Server) tailLog(n int) []string {
	f, err := os.Open(filepath.Join(s.App.Store.Dir(), diag.LogFile))
	if err != nil {
		return nil
	}
	defer f.Close()
	st, _ := f.Stat()
	const max = 512 << 10
	if st != nil && st.Size() > max {
		_, _ = f.Seek(st.Size()-max, io.SeekStart)
	}
	b, _ := io.ReadAll(f)
	lines := strings.Split(strings.TrimRight(string(b), "\n"), "\n")
	if st != nil && st.Size() > max && len(lines) > 0 {
		lines = lines[1:] // partial first line
	}
	if len(lines) > n {
		lines = lines[len(lines)-n:]
	}
	for i, l := range lines {
		lines[i] = diag.RedactText(l)
	}
	return lines
}

// client-side errors (JavaScript), rate limited.
var (
	clientLogMu    sync.Mutex
	clientLogCount int
	clientLogReset time.Time
)

func (s *Server) clientLog(w http.ResponseWriter, r *http.Request) (any, error) {
	var e struct {
		Kind    string `json:"kind"`
		Message string `json:"message"`
		Source  string `json:"source"`
		Line    int    `json:"line"`
		Col     int    `json:"col"`
		Stack   string `json:"stack"`
		Page    string `json:"page"`
	}
	if err := json.NewDecoder(io.LimitReader(r.Body, 16<<10)).Decode(&e); err != nil {
		return nil, err
	}
	clientLogMu.Lock()
	if time.Since(clientLogReset) > time.Minute {
		clientLogCount, clientLogReset = 0, time.Now()
	}
	clientLogCount++
	over := clientLogCount > 30
	clientLogMu.Unlock()
	if over {
		return nil, nil
	}
	clip := func(v string, n int) string {
		if len(v) > n {
			return v[:n] + "…"
		}
		return v
	}
	loc := ""
	if e.Source != "" {
		loc = " (" + filepath.Base(e.Source) + ":" + strconv.Itoa(e.Line) + ":" + strconv.Itoa(e.Col) + ")"
	}
	diag.Printf("[interface] %s %s%s page=%s", e.Kind, clip(e.Message, 500), loc, clip(e.Page, 100))
	if e.Stack != "" {
		diag.Printf("[interface] stack: %s", strings.ReplaceAll(clip(e.Stack, 2000), "\n", " | "))
	}
	return nil, nil
}
