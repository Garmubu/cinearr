package app

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strings"
	"time"

	"cinearr/internal/diag"
	"cinearr/internal/i18n"
	"cinearr/internal/model"
	"cinearr/internal/parser"
	"cinearr/internal/quality"
	"cinearr/internal/store"
	"cinearr/internal/tmdb"
)

// Library scan (Radarr's and Sonarr's "rescan", Jellyfin's "scan library"):
// the films and series folders are read at each launch and on demand, so
// files added, moved or deleted by hand are taken into account. The films
// folder may hold one folder per film ("Title (Year)/…") or the videos
// directly; the series folder holds one folder per series.

// ScanReport sums up the last scan.
type ScanReport struct {
	Running   bool      `json:"running"`
	Started   time.Time `json:"started"`
	Finished  time.Time `json:"finished"`
	Added     []string  `json:"added"`     // films and series found in the folders
	Linked    int       `json:"linked"`    // files attached to an entry of the library
	Missing   []string  `json:"missing"`   // files of the library no longer on disk
	Unknown   []string  `json:"unknown"`   // videos that could not be identified
	Subtitles int       `json:"subtitles"` // subtitle files found next to the videos
	Errors    []string  `json:"errors"`
	Notes     []string  `json:"notes"` // library files that could not be read at startup
}

// LastScan returns the report of the running or last scan.
func (a *App) LastScan() ScanReport {
	a.mu.Lock()
	defer a.mu.Unlock()
	r := a.scan
	r.Added, r.Missing, r.Unknown, r.Errors = clone(r.Added), clone(r.Missing), clone(r.Unknown), clone(r.Errors)
	return r
}

func clone(s []string) []string { return append([]string{}, s...) }

// StartScan runs a scan in the background unless one is running.
func (a *App) StartScan() bool {
	a.mu.Lock()
	if a.scan.Running {
		a.mu.Unlock()
		return false
	}
	a.scan = ScanReport{Running: true, Started: time.Now()}
	a.mu.Unlock()
	diag.Go("library scan", func() {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Minute)
		defer cancel()
		r := a.scanLibrary(ctx)
		r.Running, r.Started, r.Finished = false, a.LastScan().Started, time.Now()
		a.mu.Lock()
		a.scan = r
		a.mu.Unlock()
		if len(r.Added)+r.Linked+len(r.Missing) > 0 {
			a.record(model.HistoryEvent{Kind: "scan"}, "Library scan: %d added, %d files found, %d missing, %d not recognised",
				len(r.Added), r.Linked, len(r.Missing), len(r.Unknown))
		}
	})
	return true
}

// reportRecovery records the data files that could not be read at
// startup (see store.Open): the library ones are rebuilt by the scan.
func (a *App) reportRecovery() {
	for _, rc := range a.Store.Recovered() {
		a.Log.Printf("[store] %s could not be read: %s", rc.File, diag.RedactText(rc.Err))
		if rc.File == "library.json" || rc.File == "series.json" {
			f, args := recoveryText(rc)
			a.record(model.HistoryEvent{Kind: "warning"}, f, args...)
		}
	}
}

// recoveryText explains a damaged library file, for the history and the
// report of the scan.
func recoveryText(rc store.Recovery) (string, []any) {
	if rc.Aside != "" {
		return "%s could not be read: %d entries recovered, the rest is rebuilt from the folders (damaged file kept as %s)", []any{rc.File, rc.Kept, rc.Aside}
	}
	return "%s could not be read: %d entries recovered, the rest is rebuilt from the folders", []any{rc.File, rc.Kept}
}

func (a *App) scanLibrary(ctx context.Context) ScanReport {
	var r ScanReport
	for _, rc := range a.Store.Recovered() {
		if rc.File == "library.json" || rc.File == "series.json" {
			f, args := recoveryText(rc)
			r.Notes = append(r.Notes, i18n.Tf(f, args...))
		}
	}
	st := a.Store.Settings()
	if st.LibraryPath != "" {
		a.scanFilms(ctx, st.LibraryPath, &r)
	}
	if st.SeriesPath != "" {
		a.scanSeries(ctx, st.SeriesPath, &r)
	}
	sort.Strings(r.Unknown)
	return r
}

// samePath compares paths (without case on Windows and macOS).
func samePath(a, b string) bool {
	a, b = filepath.Clean(a), filepath.Clean(b)
	if runtime.GOOS == "linux" {
		return a == b
	}
	return strings.EqualFold(a, b)
}

func pathKey(p string) string {
	p = filepath.Clean(p)
	if runtime.GOOS != "linux" {
		p = strings.ToLower(p)
	}
	return p
}

// reachable reports whether a missing file can really be considered gone:
// its folder exists, or the library root exists and holds it. A file on a
// disk that is not plugged in is left alone.
func reachable(file, root string) bool {
	if _, err := os.Stat(filepath.Dir(file)); err == nil {
		return true
	}
	if root == "" {
		return false
	}
	if _, err := os.Stat(root); err != nil {
		return false
	}
	_, ok := rebase(file, root, root)
	return ok
}

// Folders never read by the scan: recycle bins, system folders and the
// thumbnails of NAS (Radarr's DiskScanService rules plus those of Windows,
// Synology and QNAP). Hidden folders and "$..." folders are left out too.
var systemDirs = map[string]bool{
	"@eadir": true, "@recycle": true, "#recycle": true, "#snapshot": true, "@__thumb": true,
	"plex versions": true, "recycler": true, "recycled": true, "system volume information": true,
	"lost+found": true, "msdownld.tmp": true,
}

// Bonus folders inside a film or series folder (Radarr's extras rules).
var extrasDirs = map[string]bool{
	"extras": true, "extrafanart": true, "behind the scenes": true, "deleted scenes": true,
	"featurettes": true, "interviews": true, "other": true, "scenes": true, "sample": true,
	"samples": true, "shorts": true, "trailers": true,
}

// Bonus videos named the Plex way ("Film-trailer.mkv").
var extrasFile = regexp.MustCompile(`(?i)-(trailer|other|behindthescenes|deleted|featurette|interview|scene|short)\.[^.]+$`)

// skipDir reports whether the scan leaves a folder out; extras folders only
// count inside a film or series folder.
func skipDir(name string, inside bool) bool {
	n := strings.ToLower(name)
	if hidden(n) || systemDirs[n] {
		return true
	}
	// Windows system folders ($RECYCLE.BIN, $WINDOWS.~BT…), not a film
	// such as "$9.99 (2008)"
	if strings.HasPrefix(n, "$") && !strings.Contains(n, " ") {
		return true
	}
	return inside && extrasDirs[n]
}

// hidden reports a hidden file or folder (".name"), but not a title that
// starts with an ellipsis ("...And Justice for All (1979)").
func hidden(name string) bool {
	return strings.HasPrefix(name, ".") && !strings.HasPrefix(name, "..")
}

// libraryVideos lists the videos of a folder, samples, bonus videos,
// partial copies and system folders left out.
func libraryVideos(dir string) []video {
	var out []video
	_ = filepath.WalkDir(dir, func(p string, d os.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() {
			if p != dir && skipDir(d.Name(), true) {
				return filepath.SkipDir
			}
			return nil
		}
		base := strings.ToLower(d.Name())
		if hidden(base) || strings.HasSuffix(base, ".partial") || extrasFile.MatchString(base) ||
			!quality.IsMediaExtension(filepath.Ext(base)) {
			return nil
		}
		fi, err := d.Info()
		if err != nil || (strings.Contains(base, "sample") && fi.Size() < 400<<20) {
			return nil
		}
		out = append(out, video{p, fi.Size()})
		return nil
	})
	return out
}

// fileFor describes a video found in the library folders.
func fileFor(v video) *model.MovieFile {
	name := filepath.Base(v.path)
	q := parser.ParseQuality(name).Quality
	f := &model.MovieFile{Path: v.path, Size: v.size, QualityID: q.ID, QualityName: q.Name, ReleaseTitle: parser.RemoveFileExtension(name),
		ReleaseGroup: parser.ParseReleaseGroup(name), Imported: time.Now()}
	if m := parser.ParseMovieTitle(name, false); m != nil {
		f.Languages = m.Languages
	}
	return f
}

// subtitleName reads "<video>.<lang>[.hi|.forced].srt".
var subtitleName = regexp.MustCompile(`(?i)^(?:\.([a-z]{2,3}(?:-[a-z]{2})?))?(?:\.(hi|sdh|cc|forced))?\.(srt|ass|ssa|vtt|sub)$`)

// sideSubtitles lists the subtitles next to a video (those saved by
// Cinearr and those added by hand), merged with the known ones.
func sideSubtitles(videoPath string, known []model.SubtitleFile) ([]model.SubtitleFile, int) {
	var out []model.SubtitleFile
	seen := map[string]bool{}
	for _, s := range known {
		if _, err := os.Stat(s.Path); err == nil {
			out = append(out, s)
			seen[pathKey(s.Path)] = true
		}
	}
	base := strings.TrimSuffix(filepath.Base(videoPath), filepath.Ext(videoPath))
	entries, err := os.ReadDir(filepath.Dir(videoPath))
	if err != nil {
		return out, 0
	}
	added := 0
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || len(name) <= len(base) || !strings.EqualFold(name[:len(base)], base) {
			continue
		}
		m := subtitleName.FindStringSubmatch(name[len(base):])
		if m == nil {
			continue
		}
		p := filepath.Join(filepath.Dir(videoPath), name)
		if seen[pathKey(p)] {
			continue
		}
		lang := strings.ToLower(m[1])
		if lang == "" {
			lang = "und"
		}
		flag := strings.ToLower(m[2])
		out = append(out, model.SubtitleFile{Path: p, Language: lang, Provider: "manual", HI: flag == "hi" || flag == "sdh" || flag == "cc", Forced: flag == "forced"})
		added++
	}
	return out, added
}

// ---------------------------------------------------------------------------
// Films

// bracketTail removes "(2010) [Bluray-1080p] {tmdb-1}" from a name.
var bracketTail = regexp.MustCompile(`[\[{(].*$`)

// movieName extracts the title, year and ids from a folder or file name
// ("Inception (2010)", "Inception.2010.1080p.BluRay.mkv", "{tmdb-27205}").
func movieName(name string) (title string, year int, imdb string, tmdbID int) {
	imdb, tmdbID = parser.ParseImdbID(name), parser.ParseTmdbID(name)
	if p := parser.ParseMovieTitle(name, false); p != nil && p.PrimaryTitle() != "" {
		return p.PrimaryTitle(), p.Year, imdb, tmdbID
	}
	t := bracketTail.ReplaceAllString(parser.RemoveFileExtension(name), "")
	t = strings.Join(strings.Fields(strings.NewReplacer(".", " ", "_", " ").Replace(t)), " ")
	return t, 0, imdb, tmdbID
}

// matchMovie finds the film of the library a name designates.
func matchMovie(movies []model.Movie, title string, year int) *model.Movie {
	key := parser.CleanMovieTitle(title)
	for i := range movies {
		m := &movies[i]
		if year > 0 && m.Year > 0 && abs(year-m.Year) > 1 {
			continue
		}
		for _, t := range append([]string{m.Title, m.OriginalTitle, m.EnglishTitle}, m.AlternativeTitles...) {
			if t != "" && parser.CleanMovieTitle(t) == key {
				return m
			}
		}
	}
	return nil
}

func abs(x int) int {
	if x < 0 {
		return -x
	}
	return x
}

// identifyMovie asks TMDB which film a name designates.
func (a *App) identifyMovie(ctx context.Context, title string, year int, imdb string, tmdbID int) (int, error) {
	if tmdbID > 0 {
		return tmdbID, nil
	}
	c := a.TMDB()
	if imdb != "" {
		if s, err := c.FindByImdb(ctx, imdb); err == nil {
			return s.ID, nil
		}
	}
	if strings.TrimSpace(title) == "" {
		return 0, nil
	}
	list, _, err := c.SearchPage(ctx, title, year, 1)
	if err != nil {
		return 0, err
	}
	return bestMatch(list, title, year), nil
}

// bestMatch picks the TMDB result with the same title (and year, ±1).
func bestMatch(list []tmdb.Summary, title string, year int) int {
	key := parser.CleanMovieTitle(title)
	for _, s := range list {
		if year > 0 && s.Year > 0 && abs(year-s.Year) > 1 {
			continue
		}
		if parser.CleanMovieTitle(s.Title) == key || parser.CleanMovieTitle(s.OriginalTitle) == key {
			return s.ID
		}
	}
	if year > 0 {
		for _, s := range list {
			if s.Year > 0 && abs(year-s.Year) <= 1 {
				return s.ID
			}
		}
	}
	return 0
}

func (a *App) scanFilms(ctx context.Context, root string, r *ScanReport) {
	entries, err := os.ReadDir(root)
	if err != nil {
		r.Errors = append(r.Errors, i18n.Tf("films folder not readable (%s): %v", root, err))
		return
	}
	// one candidate per folder (its main video) and per video at the root
	var found []video
	for _, e := range entries {
		p := filepath.Join(root, e.Name())
		if e.IsDir() {
			if skipDir(e.Name(), false) || ctx.Err() != nil {
				continue
			}
			if vs := libraryVideos(p); len(vs) > 0 {
				main := vs[0]
				for _, v := range vs[1:] {
					if v.size > main.size {
						main = v
					}
				}
				found = append(found, main)
			}
			continue
		}
		if vs := libraryVideos(p); len(vs) == 1 {
			found = append(found, vs[0])
		}
	}

	movies := a.Store.Movies()
	known := map[string]int{}
	byName := map[string][]video{}
	for _, v := range found {
		byName[strings.ToLower(filepath.Base(v.path))] = append(byName[strings.ToLower(filepath.Base(v.path))], v)
	}
	// files of the library: still there, moved (same name), or gone
	for _, m := range movies {
		if m.File == nil || m.Status == model.StatusImporting {
			continue
		}
		if _, err := os.Stat(m.File.Path); err == nil {
			known[pathKey(m.File.Path)] = m.TmdbID
			continue
		}
		if vs := byName[strings.ToLower(filepath.Base(m.File.Path))]; len(vs) == 1 {
			np, size := vs[0].path, vs[0].size
			known[pathKey(np)] = m.TmdbID
			a.Store.UpdateMovie(m.TmdbID, func(x *model.Movie) {
				if x.File != nil {
					x.File.Path, x.File.Size = np, size
				}
			})
			r.Linked++
			continue
		}
		if !reachable(m.File.Path, root) {
			continue
		}
		r.Missing = append(r.Missing, m.File.Path)
		a.Store.UpdateMovie(m.TmdbID, func(x *model.Movie) {
			x.File, x.Subtitles = nil, nil
			if x.Download == nil {
				x.Status = model.StatusWanted
			}
			x.StatusMessage = i18n.T("The file is no longer in the films folder")
		})
	}

	for _, v := range found {
		if ctx.Err() != nil {
			return
		}
		if _, ok := known[pathKey(v.path)]; ok {
			continue
		}
		name := filepath.Base(v.path)
		if !samePath(filepath.Dir(v.path), root) {
			name = filepath.Base(filepath.Dir(v.path)) // "Title (Year)"
		}
		title, year, imdb, tmdbID := movieName(name)
		if title == "" && tmdbID == 0 && imdb == "" {
			title, year, imdb, tmdbID = movieName(filepath.Base(v.path))
		}
		movies = a.Store.Movies()
		var target *model.Movie
		if tmdbID > 0 {
			for i := range movies {
				if movies[i].TmdbID == tmdbID {
					target = &movies[i]
				}
			}
		}
		if target == nil {
			target = matchMovie(movies, title, year)
		}
		if target == nil {
			id, err := a.identifyMovie(ctx, title, year, imdb, tmdbID)
			if err != nil {
				r.Errors = append(r.Errors, err.Error())
				r.Unknown = append(r.Unknown, v.path)
				continue
			}
			if id == 0 {
				r.Unknown = append(r.Unknown, v.path)
				continue
			}
			if m, ok := a.Store.Movie(id); ok {
				target = &m
			} else {
				d, err := a.TMDB().Movie(ctx, id)
				if err != nil {
					r.Errors = append(r.Errors, err.Error())
					r.Unknown = append(r.Unknown, v.path)
					continue
				}
				m := movieFromDetails(d)
				m.Status, m.Added, m.File = model.StatusAvailable, time.Now(), fileFor(v)
				m.Subtitles, _ = sideSubtitles(v.path, nil)
				if err := a.Store.PutMovie(m); err != nil {
					r.Errors = append(r.Errors, err.Error())
					continue
				}
				known[pathKey(v.path)] = m.TmdbID
				r.Added = append(r.Added, fmt.Sprintf("%s (%d)", m.Title, m.Year))
				a.history(m.TmdbID, "added", "%s (%d) found in the films folder", m.Title, m.Year)
				continue
			}
		}
		// a film of the library: its file is attached unless it already
		// has one that exists (a second copy is left alone)
		if target.File != nil || target.Download != nil {
			continue
		}
		f := fileFor(v)
		subs, _ := sideSubtitles(v.path, nil)
		a.Store.UpdateMovie(target.TmdbID, func(x *model.Movie) {
			if x.File == nil {
				x.File, x.Subtitles, x.Status, x.StatusMessage = f, subs, model.StatusAvailable, ""
			}
		})
		known[pathKey(v.path)] = target.TmdbID
		r.Linked++
	}

	// subtitles added by hand next to the videos
	for _, m := range a.Store.Movies() {
		if m.File == nil {
			continue
		}
		subs, n := sideSubtitles(m.File.Path, m.Subtitles)
		if n > 0 || len(subs) != len(m.Subtitles) {
			r.Subtitles += n
			a.Store.UpdateMovie(m.TmdbID, func(x *model.Movie) { x.Subtitles = subs })
		}
	}
}

// ---------------------------------------------------------------------------
// Series

func (a *App) identifySeries(ctx context.Context, title string, year int, imdb string) (int, error) {
	c := a.TMDB()
	if imdb != "" {
		if s, err := c.FindTVByImdb(ctx, imdb); err == nil {
			return s.ID, nil
		}
	}
	list, _, err := c.SearchTVPage(ctx, title, year, 1)
	if err != nil {
		return 0, err
	}
	key := parser.CleanSeriesTitle(title)
	for _, s := range list {
		if year > 0 && s.Year > 0 && abs(year-s.Year) > 1 {
			continue
		}
		if parser.CleanSeriesTitle(s.Title) == key || parser.CleanSeriesTitle(s.OriginalTitle) == key {
			return s.ID, nil
		}
	}
	return 0, nil
}

func (a *App) scanSeries(ctx context.Context, root string, r *ScanReport) {
	entries, err := os.ReadDir(root)
	if err != nil {
		r.Errors = append(r.Errors, i18n.Tf("series folder not readable (%s): %v", root, err))
		return
	}
	all := a.Store.AllSeries()
	inFolders := map[int][]video{} // series id -> videos of its folder
	for _, e := range entries {
		if !e.IsDir() || skipDir(e.Name(), false) || ctx.Err() != nil {
			continue // videos at the root of the series folder are ignored
		}
		dir := filepath.Join(root, e.Name())
		vs := libraryVideos(dir)
		if len(vs) == 0 {
			continue
		}
		title, year, imdb, _ := movieName(e.Name())
		var id int
		for _, s := range all {
			if year > 0 && s.Year > 0 && abs(year-s.Year) > 1 {
				continue
			}
			for _, t := range append([]string{s.Title, s.OriginalTitle, s.EnglishTitle}, s.AlternativeTitles...) {
				if t != "" && parser.CleanSeriesTitle(t) == parser.CleanSeriesTitle(title) {
					id = s.TmdbID
				}
			}
		}
		if id == 0 {
			found, err := a.identifySeries(ctx, title, year, imdb)
			if err != nil {
				r.Errors = append(r.Errors, err.Error())
			}
			if found == 0 {
				r.Unknown = append(r.Unknown, dir)
				continue
			}
			if _, ok := a.Store.Series(found); !ok {
				s, err := a.AddSeries(ctx, found)
				if err != nil {
					r.Errors = append(r.Errors, err.Error())
					r.Unknown = append(r.Unknown, dir)
					continue
				}
				r.Added = append(r.Added, fmt.Sprintf("%s (%d)", s.Title, s.Year))
			}
			id = found
		}
		inFolders[id] = append(inFolders[id], vs...)
	}

	for _, s := range a.Store.AllSeries() {
		vs := inFolders[s.TmdbID]
		byName := map[string][]video{}
		byEpisode := map[model.EpisodeKey]video{}
		for _, v := range vs {
			byName[strings.ToLower(filepath.Base(v.path))] = append(byName[strings.ToLower(filepath.Base(v.path))], v)
			if pe := parser.ParseEpisodeTitle(filepath.Base(v.path)); pe != nil {
				for _, ep := range pe.Episodes {
					byEpisode[model.EpisodeKey{Season: pe.Season, Episode: ep}] = v
				}
			}
		}
		linked, missing, subsAdded := 0, []string{}, 0
		a.Store.UpdateSeries(s.TmdbID, func(x *model.Series) {
			for si := range x.Seasons {
				for ei := range x.Seasons[si].Episodes {
					e := &x.Seasons[si].Episodes[ei]
					k := model.EpisodeKey{Season: e.Season, Episode: e.Number}
					switch {
					case e.File != nil:
						if _, err := os.Stat(e.File.Path); err == nil {
							break
						}
						if same := byName[strings.ToLower(filepath.Base(e.File.Path))]; len(same) == 1 {
							e.File.Path, e.File.Size = same[0].path, same[0].size
							linked++
							break
						}
						if v, ok := byEpisode[k]; ok {
							e.File = fileFor(v)
							linked++
							break
						}
						if reachable(e.File.Path, root) {
							missing = append(missing, e.File.Path)
							e.File, e.Subtitles, e.Status = nil, nil, ""
							e.StatusMessage = i18n.T("The file is no longer in the series folder")
						}
					case e.DownloadHash == "" && e.Status != model.StatusImporting:
						if v, ok := byEpisode[k]; ok {
							e.File, e.Status, e.StatusMessage = fileFor(v), model.StatusAvailable, ""
							linked++
						}
					}
					if e.File != nil {
						var n int
						e.Subtitles, n = sideSubtitles(e.File.Path, e.Subtitles)
						subsAdded += n
						if e.Status != model.StatusDownloading && e.Status != model.StatusImporting {
							e.Status = model.StatusAvailable
						}
					}
				}
			}
		})
		r.Linked += linked
		r.Missing = append(r.Missing, missing...)
		r.Subtitles += subsAdded
	}
}

// ---------------------------------------------------------------------------
// Moving the library folders

// rebase replaces the root old of a path by new.
func rebase(p, old, new string) (string, bool) {
	cp, co := filepath.Clean(p), filepath.Clean(old)
	if !samePath(cp, co) {
		prefix := co + string(filepath.Separator)
		if len(cp) <= len(prefix) || !samePath(cp[:len(prefix)], prefix) {
			return p, false
		}
	}
	return filepath.Join(new, cp[len(co):]), true
}

// MoveLibrary rewrites the paths of the films (series when series is true)
// stored under the folder old so they point to new: the files themselves
// are not moved. It returns the number of entries changed.
func (a *App) MoveLibrary(old, new string, series bool) int {
	if old == "" || new == "" || samePath(old, new) {
		return 0
	}
	moveFile := func(f *model.MovieFile, subs []model.SubtitleFile) bool {
		if f == nil {
			return false
		}
		np, ok := rebase(f.Path, old, new)
		if !ok {
			return false
		}
		f.Path = np
		for i := range subs {
			if sp, ok := rebase(subs[i].Path, old, new); ok {
				subs[i].Path = sp
			}
		}
		return true
	}
	n := 0
	if !series {
		for _, m := range a.Store.Movies() {
			a.Store.UpdateMovie(m.TmdbID, func(x *model.Movie) {
				if moveFile(x.File, x.Subtitles) {
					n++
				}
			})
		}
		if n > 0 {
			a.record(model.HistoryEvent{Kind: "moved"}, "Films folder changed to %s: paths of %d films updated", new, n)
		}
		return n
	}
	for _, s := range a.Store.AllSeries() {
		a.Store.UpdateSeries(s.TmdbID, func(x *model.Series) {
			for si := range x.Seasons {
				for ei := range x.Seasons[si].Episodes {
					e := &x.Seasons[si].Episodes[ei]
					if moveFile(e.File, e.Subtitles) {
						n++
					}
				}
			}
		})
	}
	if n > 0 {
		a.record(model.HistoryEvent{Kind: "moved"}, "Series folder changed to %s: paths of %d episodes updated", new, n)
	}
	return n
}
