// Package api exposes the application to the embedded interface over a
// local HTTP JSON API.
package api

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync/atomic"
	"time"

	"cinearr/internal/app"
	"cinearr/internal/decision"
	"cinearr/internal/diag"
	"cinearr/internal/dlclient"
	"cinearr/internal/i18n"
	"cinearr/internal/indexer"
	"cinearr/internal/model"
	"cinearr/internal/player"
	"cinearr/internal/proxy"
	"cinearr/internal/quality"
	"cinearr/internal/store"
	"cinearr/internal/subtitles"
	"cinearr/internal/tmdb"
)

// pollingPaths are called every few seconds by the interface: not logged
// in debug mode to keep the log readable.
var pollingPaths = map[string]bool{"/api/status": true, "/api/movies": true, "/api/series": true, "/api/logs": true, "/api/ping": true}

// Version is set at build time (-ldflags "-X cinearr/internal/api.Version=...").
var Version = "dev"

// Server serves the API and the interface.
type Server struct {
	App          *app.App
	UI           fs.FS
	Quit         func()
	AllowAnyHost bool
	ForceDebug   bool // -debug on the command line
	lastSeen     atomic.Int64
}

// LastSeen is the time of the last API call made by the interface.
func (s *Server) LastSeen() time.Time {
	v := s.lastSeen.Load()
	if v == 0 {
		return time.Time{}
	}
	return time.Unix(0, v)
}

// Handler returns the HTTP handler.
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	h := func(pattern string, fn func(w http.ResponseWriter, r *http.Request) (any, error)) {
		mux.HandleFunc(pattern, func(w http.ResponseWriter, r *http.Request) {
			start := time.Now()
			defer func() {
				if rec := recover(); rec != nil {
					diag.ReportPanic(r.Method+" "+r.URL.Path, rec)
					writeJSON(w, http.StatusInternalServerError, map[string]string{"error": i18n.T("internal error (details in the log, Activity › Log)")})
				}
			}()
			v, err := fn(w, r)
			if err != nil {
				diag.Printf("[api] %s %s: %s (%s)", r.Method, r.URL.Path, diag.RedactText(err.Error()), time.Since(start).Round(time.Millisecond))
				writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
				return
			}
			if !pollingPaths[r.URL.Path] {
				diag.Debugf("[api] %s %s (%s)", r.Method, diag.RedactURL(r.URL.RequestURI()), time.Since(start).Round(time.Millisecond))
			}
			if v == nil {
				v = map[string]bool{"ok": true}
			}
			writeJSON(w, http.StatusOK, v)
		})
	}

	h("GET /api/ping", func(w http.ResponseWriter, r *http.Request) (any, error) {
		return map[string]string{"app": "cinearr", "version": Version}, nil
	})
	h("GET /api/status", s.status)
	h("POST /api/quit", func(w http.ResponseWriter, r *http.Request) (any, error) {
		if s.Quit != nil {
			go func() { time.Sleep(300 * time.Millisecond); s.Quit() }()
		}
		return nil, nil
	})

	// diagnostics
	h("POST /api/client-log", s.clientLog)
	mux.HandleFunc("GET /api/diagnostic", s.diagnostic)
	h("GET /api/logs", func(w http.ResponseWriter, r *http.Request) (any, error) {
		n, _ := strconv.Atoi(r.URL.Query().Get("lines"))
		if n <= 0 || n > 2000 {
			n = 400
		}
		lines := s.tailLog(n)
		if lines == nil {
			lines = []string{}
		}
		return map[string]any{"lines": lines, "debug": diag.Debug(), "file": filepath.Join(s.App.Store.Dir(), diag.LogFile)}, nil
	})
	h("POST /api/logs/open", func(w http.ResponseWriter, r *http.Request) (any, error) {
		return nil, openFolder(s.App.Store.Dir())
	})
	h("GET /api/crash", func(w http.ResponseWriter, r *http.Request) (any, error) {
		report, when := diag.CrashReport()
		if len(report) > 20000 {
			report = report[len(report)-20000:]
		}
		return map[string]any{"report": diag.RedactText(report), "date": when}, nil
	})
	h("POST /api/crash/ack", func(w http.ResponseWriter, r *http.Request) (any, error) { return nil, diag.AckCrash() })
	h("POST /api/debug", func(w http.ResponseWriter, r *http.Request) (any, error) {
		var v struct {
			On bool `json:"on"`
		}
		if err := json.NewDecoder(r.Body).Decode(&v); err != nil {
			return nil, err
		}
		st := s.App.Store.Settings()
		st.Debug = v.On
		if err := s.App.Store.SaveSettings(st); err != nil {
			return nil, err
		}
		diag.SetDebug(v.On || s.ForceDebug)
		return map[string]bool{"debug": diag.Debug()}, nil
	})

	// settings
	h("GET /api/settings", func(w http.ResponseWriter, r *http.Request) (any, error) { return s.App.Store.Settings(), nil })
	h("PUT /api/settings", s.saveSettings)
	h("POST /api/settings/test-client", s.testClient)
	h("GET /api/download-clients", func(w http.ResponseWriter, r *http.Request) (any, error) { return dlclient.Clients, nil })
	h("POST /api/settings/test-tmdb", s.testTMDB)
	h("GET /api/subtitles/providers", s.subtitleCatalog)
	h("GET /api/languages", func(w http.ResponseWriter, r *http.Request) (any, error) { return i18n.Languages, nil })
	h("GET /api/i18n/{code}", func(w http.ResponseWriter, r *http.Request) (any, error) {
		return i18n.Catalog(r.PathValue("code")), nil
	})
	h("GET /api/defaults", func(w http.ResponseWriter, r *http.Request) (any, error) {
		// defaults that follow the language chosen at the first launch
		l := i18n.Lookup(r.URL.Query().Get("lang"))
		subs := []string{l.Code}
		if l.Code != "en" {
			subs = append(subs, "en")
		}
		return map[string]any{"language": l.TMDB, "profile": decision.ProfileForLanguage(l.Release),
			"subtitleLanguages": subs, "audioLang": l.Audio}, nil
	})
	h("POST /api/subtitles/test", s.testSubtitleProvider)
	h("GET /api/players", s.players)
	h("POST /api/players/{id}/install", s.installPlayer)
	h("GET /api/qualities", func(w http.ResponseWriter, r *http.Request) (any, error) { return quality.Definitions, nil })
	h("POST /api/fs/pick", func(w http.ResponseWriter, r *http.Request) (any, error) {
		dir := r.URL.Query().Get("type") != "file"
		p, err := pickPath(dir, r.URL.Query().Get("title"))
		if err != nil {
			return nil, err
		}
		return map[string]string{"path": p}, nil
	})
	h("GET /api/fs/check", func(w http.ResponseWriter, r *http.Request) (any, error) {
		p := r.URL.Query().Get("path")
		st, err := os.Stat(p)
		if err != nil {
			return map[string]any{"exists": false}, nil
		}
		writable := false
		if st.IsDir() {
			f, err := os.CreateTemp(p, ".cinearr-test-*")
			if err == nil {
				writable = true
				name := f.Name()
				f.Close()
				os.Remove(name)
			}
		}
		return map[string]any{"exists": true, "dir": st.IsDir(), "writable": writable}, nil
	})

	// discovery / metadata
	h("GET /api/discover", s.discover)
	h("GET /api/lookup", s.lookup)
	h("GET /api/tmdb/{id}", s.withID(func(ctx context.Context, id int, r *http.Request) (any, error) {
		return s.App.TMDB().Movie(ctx, id)
	}))

	// library
	h("GET /api/movies", func(w http.ResponseWriter, r *http.Request) (any, error) { return s.App.Store.Movies(), nil })
	h("POST /api/movies", s.addMovie)
	h("GET /api/movies/{id}", s.withID(func(ctx context.Context, id int, r *http.Request) (any, error) {
		m, ok := s.App.Store.Movie(id)
		if !ok {
			return nil, i18n.New("film not found")
		}
		return m, nil
	}))
	h("DELETE /api/movies/{id}", s.withID(func(ctx context.Context, id int, r *http.Request) (any, error) {
		return nil, s.App.DeleteMovie(ctx, id, r.URL.Query().Get("files") == "1")
	}))
	h("POST /api/movies/{id}/refresh", s.withID(func(ctx context.Context, id int, r *http.Request) (any, error) {
		return nil, s.App.RefreshMovie(ctx, id)
	}))
	h("POST /api/movies/{id}/search", s.withID(func(ctx context.Context, id int, r *http.Request) (any, error) {
		ctx, cancel := context.WithTimeout(ctx, 2*time.Minute)
		defer cancel()
		return s.App.SearchReleases(ctx, id)
	}))
	h("POST /api/movies/{id}/autosearch", s.withID(func(_ context.Context, id int, r *http.Request) (any, error) {
		go func() {
			ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
			defer cancel()
			if err := s.App.AutoSearch(ctx, id); err != nil {
				s.App.Store.UpdateMovie(id, func(m *model.Movie) { m.StatusMessage = err.Error() })
			}
		}()
		return nil, nil
	}))
	h("POST /api/movies/{id}/grab", s.withID(func(ctx context.Context, id int, r *http.Request) (any, error) {
		var rel model.Release
		if err := json.NewDecoder(r.Body).Decode(&rel); err != nil {
			return nil, err
		}
		ctx, cancel := context.WithTimeout(ctx, 90*time.Second)
		defer cancel()
		return nil, s.App.Grab(ctx, id, rel)
	}))
	h("POST /api/movies/{id}/cancel", s.withID(func(ctx context.Context, id int, r *http.Request) (any, error) {
		return nil, s.App.CancelDownload(ctx, id, r.URL.Query().Get("blocklist") == "1")
	}))
	h("POST /api/movies/{id}/play", s.withID(func(ctx context.Context, id int, r *http.Request) (any, error) {
		return nil, s.App.Play(id, r.URL.Query().Get("live") == "1")
	}))
	h("POST /api/movies/{id}/open-folder", s.withID(func(ctx context.Context, id int, r *http.Request) (any, error) {
		m, ok := s.App.Store.Movie(id)
		if !ok || m.File == nil {
			return nil, i18n.New("no file")
		}
		return nil, openFolder(filepath.Dir(m.File.Path))
	}))
	h("GET /api/movies/{id}/subtitles", s.withID(func(ctx context.Context, id int, r *http.Request) (any, error) {
		ctx, cancel := context.WithTimeout(ctx, time.Minute)
		defer cancel()
		cs, errs, err := s.App.SearchSubtitles(ctx, id)
		return s.subtitleResult(cs, errs, err, s.App.MovieSubtitleLinks(id))
	}))
	h("POST /api/movies/{id}/subtitles/import", s.withID(func(ctx context.Context, id int, r *http.Request) (any, error) {
		data, err := readUpload(r)
		if err != nil {
			return nil, err
		}
		return nil, s.App.ImportSubtitle(id, data, r.URL.Query().Get("name"), r.URL.Query().Get("lang"))
	}))
	h("POST /api/movies/{id}/subtitles", s.withID(func(ctx context.Context, id int, r *http.Request) (any, error) {
		var c subtitles.Candidate
		if err := json.NewDecoder(r.Body).Decode(&c); err != nil {
			return nil, err
		}
		ctx, cancel := context.WithTimeout(ctx, time.Minute)
		defer cancel()
		return nil, s.App.DownloadSubtitle(ctx, id, c)
	}))

	s.seriesRoutes(h)

	// indexers
	h("GET /api/indexers/definitions", func(w http.ResponseWriter, r *http.Request) (any, error) {
		list, err := s.App.Indexer.Repo.List(r.Context())
		if list == nil {
			list = []indexer.Meta{}
		}
		resp := map[string]any{"definitions": list, "customDir": s.App.Indexer.Repo.CustomDir()}
		if err != nil {
			resp["warning"] = err.Error()
		}
		return resp, nil
	})
	h("GET /api/indexers/definitions/{id}", func(w http.ResponseWriter, r *http.Request) (any, error) {
		def, err := s.App.Indexer.Repo.Get(r.Context(), r.PathValue("id"))
		if err != nil {
			return nil, err
		}
		login := ""
		captcha := false
		if def.Login != nil {
			login = def.Login.Method
			captcha = def.Login.Captcha != nil
		}
		return map[string]any{"id": def.ID, "name": def.Name, "description": def.Description, "type": def.Type,
			"language": def.Language, "links": def.Links, "settings": def.Settings, "login": login, "captcha": captcha,
			"imdbSearch": def.SupportsImdbMovieSearch()}, nil
	})
	h("POST /api/indexers/test", func(w http.ResponseWriter, r *http.Request) (any, error) {
		var c indexer.Config
		if err := json.NewDecoder(r.Body).Decode(&c); err != nil {
			return nil, err
		}
		if c.ID == "" {
			c.ID = "test"
		}
		ctx, cancel := context.WithTimeout(r.Context(), 90*time.Second)
		defer cancel()
		n, err := s.App.TestIndexer(ctx, c)
		if err != nil {
			return nil, err
		}
		s.App.Indexer.Status.OK(c.ID) // a successful test ends the pause
		return map[string]int{"results": n}, nil
	})
	h("POST /api/proxy/test", func(w http.ResponseWriter, r *http.Request) (any, error) {
		var p proxy.Settings
		if err := json.NewDecoder(r.Body).Decode(&p); err != nil {
			return nil, err
		}
		p, err := proxy.Normalize(p)
		if err != nil {
			return nil, err
		}
		if p.Password == "" && p.Username != "" {
			if cur := s.App.Store.Settings().Proxy; cur.Username == p.Username {
				p.Password = cur.Password
			}
		}
		ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
		defer cancel()
		through, direct, err := proxy.Test(ctx, p)
		if err != nil {
			return nil, err
		}
		return map[string]any{"through": through, "direct": direct, "same": direct.IP != "" && direct.IP == through.IP}, nil
	})
	// indexers and subtitle providers paused after failures in a row
	h("GET /api/sources/status", func(w http.ResponseWriter, r *http.Request) (any, error) {
		return map[string]any{"indexers": s.App.Indexer.Status.All(), "subtitles": subtitles.Pauses.All()}, nil
	})
	h("POST /api/sources/resume", func(w http.ResponseWriter, r *http.Request) (any, error) {
		id := r.URL.Query().Get("id")
		if r.URL.Query().Get("kind") == "subtitles" {
			subtitles.Pauses.OK(id)
		} else {
			s.App.Indexer.Status.OK(id)
		}
		return nil, nil
	})
	h("POST /api/indexers", s.saveIndexer)
	h("DELETE /api/indexers/{id}", func(w http.ResponseWriter, r *http.Request) (any, error) {
		id := r.PathValue("id")
		st := s.App.Store.Settings()
		var keep []indexer.Config
		for _, c := range st.Indexers {
			if c.ID != id {
				keep = append(keep, c)
			}
		}
		st.Indexers = keep
		s.App.Indexer.Forget(id)
		return nil, s.App.Store.SaveSettings(st)
	})

	// activity
	h("GET /api/history", func(w http.ResponseWriter, r *http.Request) (any, error) { return s.App.Store.History(), nil })
	h("GET /api/blocklist", func(w http.ResponseWriter, r *http.Request) (any, error) { return s.App.Store.Blocklist(), nil })
	h("DELETE /api/blocklist/{i}", func(w http.ResponseWriter, r *http.Request) (any, error) {
		i, err := strconv.Atoi(r.PathValue("i"))
		if err != nil {
			return nil, err
		}
		return nil, s.App.Store.RemoveBlocklist(i)
	})

	if s.UI != nil {
		files := http.FileServerFS(s.UI)
		mux.Handle("GET /", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Cache-Control", "no-cache")
			files.ServeHTTP(w, r)
		}))
	}
	return s.guard(mux)
}

// guard rejects requests from other sites (DNS rebinding and CSRF): the
// Host must be local and state-changing calls must carry X-Cinearr.
func (s *Server) guard(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !s.AllowAnyHost {
			host, _, err := net.SplitHostPort(r.Host)
			if err != nil {
				host = r.Host
			}
			if host != "127.0.0.1" && host != "localhost" && host != "::1" && host != "[::1]" {
				http.Error(w, "forbidden host", http.StatusForbidden)
				return
			}
		}
		if strings.HasPrefix(r.URL.Path, "/api/") && r.Method != http.MethodGet && r.Header.Get("X-Cinearr") != "1" {
			http.Error(w, "missing X-Cinearr header", http.StatusForbidden)
			return
		}
		if strings.HasPrefix(r.URL.Path, "/api/") {
			s.lastSeen.Store(time.Now().UnixNano())
		}
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("X-Frame-Options", "DENY")
		w.Header().Set("Content-Security-Policy", "default-src 'self'; img-src 'self' https://image.tmdb.org data:; style-src 'self' 'unsafe-inline'; script-src 'self'; frame-src https://www.youtube-nocookie.com")
		next.ServeHTTP(w, r)
	})
}

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}

func pathID(r *http.Request) (int, error) {
	id, err := strconv.Atoi(r.PathValue("id"))
	if err != nil {
		return 0, i18n.New("invalid identifier")
	}
	return id, nil
}

func (s *Server) withID(fn func(ctx context.Context, id int, r *http.Request) (any, error)) func(http.ResponseWriter, *http.Request) (any, error) {
	return func(w http.ResponseWriter, r *http.Request) (any, error) {
		id, err := pathID(r)
		if err != nil {
			return nil, err
		}
		return fn(r.Context(), id, r)
	}
}

func (s *Server) status(w http.ResponseWriter, r *http.Request) (any, error) {
	st := s.App.Store.Settings()
	last, syncErr := s.App.SyncStatus()
	_, perr := player.Find(st.Player.Kind, st.Player.Path, s.App.Store.Dir())
	_, crashAt := diag.CrashReport()
	var crash any
	if !crashAt.IsZero() {
		crash = crashAt
	}
	return map[string]any{
		"crash": crash, "debug": diag.Debug(),
		"version": Version, "setupDone": st.SetupDone, "dataDir": s.App.Store.Dir(), "os": runtime.GOOS,
		"busy": s.App.Busy(), "busySeries": s.App.BusySeries(), "scanning": s.App.LastScan().Running, "seriesPath": st.SeriesPath != "", "lastSync": last, "syncError": syncErr, "playerFound": perr == nil, "client": dlclient.Name(st.Client.Kind),
		"indexers": len(st.Indexers), "tmdb": st.TMDBKey != "", "subtitleProviders": len(s.App.SubtitleProviders()),
		"openSubtitlesBuiltin": subtitles.BuiltinOpenSubtitlesKey != "", "subtitleProblem": app.SubtitleSetupProblem(st.Subtitles),
	}, nil
}

func (s *Server) saveSettings(w http.ResponseWriter, r *http.Request) (any, error) {
	var v store.Settings
	if err := json.NewDecoder(r.Body).Decode(&v); err != nil {
		return nil, err
	}
	cur := s.App.Store.Settings()
	if v.Indexers == nil {
		v.Indexers = cur.Indexers // the settings page does not edit indexers
	}
	if v.Port == 0 {
		v.Port = cur.Port
	}
	if v.MinFreeSpaceMB < 0 {
		return nil, i18n.New("the free space to keep cannot be negative")
	}
	px, err := proxy.Normalize(v.Proxy)
	if err != nil {
		return nil, err
	}
	v.Proxy = px
	if v.RecycleDays < 0 || v.RecycleDays > 365 {
		return nil, i18n.New("the recycle bin keeps files between 0 and 365 days")
	}
	for i, l := range v.Subtitles.Languages {
		code, err := subtitles.NormalizeLanguage(l)
		if err != nil {
			return nil, err
		}
		v.Subtitles.Languages[i] = code
	}
	exts, err := quality.NormalizeExtensions(v.RejectedExtensions)
	if err != nil {
		return nil, err
	}
	v.RejectedExtensions = exts
	if v.LibraryPath != cur.LibraryPath || v.SeriesPath != cur.SeriesPath || v.Client.SavePath != cur.Client.SavePath ||
		fmt.Sprint(v.PathMappings) != fmt.Sprint(cur.PathMappings) {
		if err := app.CheckFolders(v, s.App.Store.Dir()); err != nil {
			return nil, err
		}
	}
	if v.LibraryPath != "" {
		if err := os.MkdirAll(v.LibraryPath, 0o755); err != nil {
			return nil, i18n.Errorf("library folder not accessible: %w", err)
		}
	}
	if v.SeriesPath != "" {
		if err := os.MkdirAll(v.SeriesPath, 0o755); err != nil {
			return nil, i18n.Errorf("series folder not accessible: %w", err)
		}
	}
	if err := s.App.Store.SaveSettings(v); err != nil {
		return nil, err
	}
	proxy.Set(v.Proxy)
	// a library folder changed: the stored paths follow it (the user moves
	// the files), then the folders are read again
	moved := false
	if v.LibraryPath != cur.LibraryPath {
		s.App.MoveLibrary(cur.LibraryPath, v.LibraryPath, false)
		moved = true
	}
	if v.SeriesPath != cur.SeriesPath {
		s.App.MoveLibrary(cur.SeriesPath, v.SeriesPath, true)
		moved = true
	}
	if moved && v.SetupDone {
		s.App.StartScan()
	}
	diag.SetDebug(v.Debug || s.ForceDebug)
	i18n.SetLanguage(v.UILanguage)
	return s.App.Store.Settings(), nil
}

func (s *Server) testClient(w http.ResponseWriter, r *http.Request) (any, error) {
	var v store.Settings
	if err := json.NewDecoder(r.Body).Decode(&v); err != nil {
		return nil, err
	}
	c := dlclient.New(v.Client)
	ctx, cancel := context.WithTimeout(r.Context(), 20*time.Second)
	defer cancel()
	res, err := c.Test(ctx)
	if err != nil {
		return nil, err
	}
	return map[string]string{"name": c.Name(), "version": res.Version, "warning": res.Warning}, nil
}

// players lists the players with their detection and installation state.
func (s *Server) players(w http.ResponseWriter, r *http.Request) (any, error) {
	dir := s.App.Store.Dir()
	st := s.App.Store.Settings().Player
	type entry struct {
		player.Info
		Path       string          `json:"path"`
		CanInstall bool            `json:"canInstall"`
		Hint       string          `json:"hint,omitempty"`
		Install    player.Progress `json:"install"`
	}
	var list []entry
	for _, p := range player.Players() {
		list = append(list, entry{Info: p, Path: player.Detect(p.ID, dir), CanInstall: player.CanInstall(p.ID),
			Hint: player.InstallHint(p.ID), Install: player.InstallStatus(p.ID)})
	}
	custom := r.URL.Query().Get("path")
	if custom == "" {
		custom = st.Path
	}
	kind := st.Kind
	if q := r.URL.Query().Get("kind"); q != "" {
		kind = q
	}
	res := map[string]any{"players": list, "kind": st.Kind, "customPath": custom}
	if exe, err := player.Find(kind, custom, dir); err == nil {
		res["active"] = map[string]string{"path": exe, "family": player.Family(exe)}
	} else {
		res["error"] = err.Error()
	}
	if custom != "" {
		_, err := os.Stat(custom)
		res["customFound"] = err == nil
	}
	return res, nil
}

// installPlayer downloads a player in the background; the interface polls
// /api/players for the progress.
func (s *Server) installPlayer(w http.ResponseWriter, r *http.Request) (any, error) {
	id := r.PathValue("id")
	if !player.CanInstall(id) {
		if h := player.InstallHint(id); h != "" {
			return nil, i18n.Errorf("automatic installation is not possible on this system: %s", h)
		}
		return nil, i18n.New("automatic installation is not possible on this system")
	}
	if player.InstallStatus(id).Running {
		return player.InstallStatus(id), nil
	}
	dir := s.App.Store.Dir()
	diag.Go("player installation", func() {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Minute)
		defer cancel()
		exe, err := player.Install(ctx, id, dir)
		if err != nil {
			diag.Printf("[player] installation of %s failed: %v", player.Name(id), err)
			return
		}
		diag.Printf("[player] %s installed: %s", player.Name(id), exe)
		// installing a player means choosing it
		st := s.App.Store.Settings()
		st.Player.Kind = id
		if err := s.App.Store.SaveSettings(st); err != nil {
			diag.Printf("[player] settings not saved: %v", err)
		}
	})
	return map[string]any{"running": true}, nil
}

// subtitleResult is the answer of a subtitle search: candidates, provider
// errors, and the search pages of the sites added by the user. Without
// automatic provider, the sites alone are a valid answer.
func (s *Server) subtitleResult(cs []subtitles.Candidate, errs []string, err error, links []subtitles.Link) (any, error) {
	if err != nil {
		if len(links) == 0 {
			return nil, err
		}
		if len(s.App.SubtitleProviders()) > 0 {
			errs = append(errs, err.Error())
		}
	}
	if cs == nil {
		cs = []subtitles.Candidate{}
	}
	if errs == nil {
		errs = []string{}
	}
	return map[string]any{"candidates": cs, "errors": errs, "links": links}, nil
}

// readUpload reads a subtitle file sent by the interface (raw body).
func readUpload(r *http.Request) ([]byte, error) {
	data, err := io.ReadAll(http.MaxBytesReader(nil, r.Body, 20<<20))
	if err != nil {
		return nil, i18n.New("file too large (20 MB at most)")
	}
	if len(data) == 0 {
		return nil, i18n.New("empty file")
	}
	return data, nil
}

// subtitleCatalog lists the subtitle providers that can be added, with
// texts in the interface language.
func (s *Server) subtitleCatalog(w http.ResponseWriter, r *http.Request) (any, error) {
	list := subtitles.Catalog()
	for i := range list {
		p := &list[i]
		p.Name, p.Description = i18n.T(p.Name), i18n.T(p.Description)
		fields := make([]subtitles.Field, len(p.Fields))
		for j, f := range p.Fields {
			f.Label, f.Hint = i18n.T(f.Label), i18n.T(f.Hint)
			fields[j] = f
		}
		p.Fields = fields
	}
	return list, nil
}

// testSubtitleProvider checks the settings of one provider.
func (s *Server) testSubtitleProvider(w http.ResponseWriter, r *http.Request) (any, error) {
	var c subtitles.Config
	if err := json.NewDecoder(r.Body).Decode(&c); err != nil {
		return nil, err
	}
	p, err := subtitles.BuildOne(c)
	if err != nil {
		return nil, err
	}
	t, ok := p.(subtitles.Tester)
	if !ok {
		return map[string]string{"message": i18n.T("no check available for this provider")}, nil
	}
	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()
	msg, err := t.Test(ctx)
	if err != nil {
		return nil, err
	}
	return map[string]string{"message": msg}, nil
}

func (s *Server) testTMDB(w http.ResponseWriter, r *http.Request) (any, error) {
	var v struct {
		Key      string `json:"key"`
		Language string `json:"language"`
	}
	if err := json.NewDecoder(r.Body).Decode(&v); err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
	defer cancel()
	res, err := tmdb.New(v.Key, v.Language).Search(ctx, "Inception", 2010)
	if err != nil {
		return nil, err
	}
	return map[string]int{"results": len(res)}, nil
}

type summaryOut struct {
	tmdb.Summary
	InLibrary bool   `json:"inLibrary"`
	Status    string `json:"status,omitempty"`
	Upcoming  bool   `json:"upcoming,omitempty"` // not released yet
	Kind      string `json:"kind"`               // movie or tv
}

func (s *Server) annotate(list []tmdb.Summary, tv bool) []summaryOut {
	lib := map[int]string{}
	if tv {
		for _, x := range s.App.Store.AllSeries() {
			lib[x.TmdbID] = "series"
		}
	} else {
		for _, m := range s.App.Store.Movies() {
			lib[m.TmdbID] = m.Status
		}
	}
	out := make([]summaryOut, 0, len(list))
	now := time.Now()
	for _, x := range list {
		st, ok := lib[x.ID]
		out = append(out, summaryOut{Summary: x, InLibrary: ok, Status: st, Upcoming: !x.Released(now), Kind: map[bool]string{true: "tv", false: "movie"}[tv]})
	}
	return out
}

func (s *Server) discover(w http.ResponseWriter, r *http.Request) (any, error) {
	c := s.App.TMDB()
	pageNum, _ := strconv.Atoi(r.URL.Query().Get("page"))
	tv := r.URL.Query().Get("kind") == "tv"
	fetch := c.Trending
	switch {
	case tv && r.URL.Query().Get("type") == "popular":
		fetch = c.PopularTV
	case tv:
		fetch = c.TrendingTV
	case r.URL.Query().Get("type") == "popular":
		fetch = c.Popular
	}
	list, more, err := fetch(r.Context(), pageNum)
	if err != nil {
		return nil, err
	}
	// films not yet in cinemas (series not yet aired) cannot be
	// downloaded: leave them out
	now := time.Now()
	released := list[:0]
	for _, x := range list {
		if x.Released(now) {
			released = append(released, x)
		}
	}
	return map[string]any{"results": s.annotate(released, tv), "more": more}, nil
}

func (s *Server) lookup(w http.ResponseWriter, r *http.Request) (any, error) {
	q := strings.TrimSpace(r.URL.Query().Get("q"))
	if q == "" {
		return map[string]any{"results": []summaryOut{}, "more": false}, nil
	}
	c := s.App.TMDB()
	pageNum, _ := strconv.Atoi(r.URL.Query().Get("page"))
	tv := r.URL.Query().Get("kind") == "tv"
	// IMDb id or IMDb URL
	if i := strings.Index(q, "tt"); i >= 0 && pageNum <= 1 {
		id := q[i:]
		end := 2
		for end < len(id) && id[end] >= '0' && id[end] <= '9' {
			end++
		}
		if end >= 9 {
			find := c.FindByImdb
			if tv {
				find = c.FindTVByImdb
			}
			if m, err := find(r.Context(), id[:end]); err == nil {
				return map[string]any{"results": s.annotate([]tmdb.Summary{*m}, tv), "more": false}, nil
			}
		}
	}
	year := 0
	if f := strings.Fields(q); len(f) > 1 {
		if y, err := strconv.Atoi(f[len(f)-1]); err == nil && y > 1880 && y < 2100 {
			year = y
			q = strings.Join(f[:len(f)-1], " ")
		}
	}
	search := c.SearchPage
	if tv {
		search = c.SearchTVPage
	}
	list, more, err := search(r.Context(), q, year, pageNum)
	if err != nil {
		return nil, err
	}
	return map[string]any{"results": s.annotate(list, tv), "more": more}, nil
}

func (s *Server) addMovie(w http.ResponseWriter, r *http.Request) (any, error) {
	var v struct {
		TmdbID int  `json:"tmdbId"`
		Search bool `json:"search"`
	}
	if err := json.NewDecoder(r.Body).Decode(&v); err != nil {
		return nil, err
	}
	return s.App.AddMovie(r.Context(), v.TmdbID, v.Search)
}

func (s *Server) saveIndexer(w http.ResponseWriter, r *http.Request) (any, error) {
	var c indexer.Config
	if err := json.NewDecoder(r.Body).Decode(&c); err != nil {
		return nil, err
	}
	if c.Kind == "" {
		c.Kind = "cardigann"
	}
	if c.Kind == "cardigann" && c.DefinitionID == "" {
		return nil, i18n.New("definition missing")
	}
	if c.Kind == "torznab" && c.BaseURL == "" {
		return nil, i18n.New("Torznab URL missing")
	}
	if c.Priority == 0 {
		c.Priority = 25
	}
	st := s.App.Store.Settings()
	if c.ID == "" {
		c.ID = fmt.Sprintf("%s-%d", strings.ToLower(strings.ReplaceAll(firstNonEmpty(c.DefinitionID, "torznab"), " ", "-")), time.Now().UnixNano()%1e9)
		st.Indexers = append(st.Indexers, c)
	} else {
		found := false
		for i := range st.Indexers {
			if st.Indexers[i].ID == c.ID {
				st.Indexers[i] = c
				found = true
			}
		}
		if !found {
			st.Indexers = append(st.Indexers, c)
		}
	}
	s.App.Indexer.Forget(c.ID)
	return c, s.App.Store.SaveSettings(st)
}

func firstNonEmpty(v ...string) string {
	for _, s := range v {
		if s != "" {
			return s
		}
	}
	return ""
}
