// Cinearr: indexers, film search, downloads through the user's torrent client, subtitles
// and playback in a single executable.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"cinearr/internal/api"
	"cinearr/internal/app"
	"cinearr/internal/desktop"
	"cinearr/internal/diag"
	"cinearr/internal/i18n"
	"cinearr/internal/store"
	"cinearr/web"
)

func main() {
	dataDir := flag.String("data", "", "data folder (default: user folder, or ./data next to the executable)")
	listen := flag.String("listen", "", "listen address (default 127.0.0.1:<port>)")
	noWindow := flag.Bool("no-window", false, "do not open a window (server mode)")
	browser := flag.Bool("browser", false, "open in the default browser instead of a dedicated window")
	debugMode := flag.Bool("debug", false, "detailed log: every request to the sites, TMDB and the torrent client (secrets hidden)")
	flag.Parse()

	dir := *dataDir
	if dir == "" {
		dir = store.DataDir()
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		fatal(err)
	}
	logger, closeLog := diag.Setup(dir, *debugMode)
	defer closeLog()
	defer func() {
		if r := recover(); r != nil {
			diag.ReportPanic("main", r)
			desktop.Alert(i18n.T("Cinearr stopped"), i18n.Tf("Unexpected error: %v\n\nThe details are in %s", r, filepath.Join(dir, diag.CrashFile)))
			os.Exit(2)
		}
	}()
	logger.Printf("==== Cinearr %s — %s/%s — data: %s", api.Version, runtime.GOOS, runtime.GOARCH, dir)
	if report, when := diag.CrashReport(); report != "" {
		logger.Printf("a crash was recorded on %s (see %s)", when.Format("2006-01-02 15:04"), diag.CrashFile)
	}

	st, err := store.Open(dir)
	if err != nil {
		fatal(err)
	}
	settings := st.Settings()
	i18n.SetLanguage(settings.UILanguage)
	if settings.Debug && !*debugMode {
		diag.SetDebug(true)
	}
	if b := desktop.FindBrowser(); b != "" {
		logger.Printf("window: %s", b)
	} else {
		logger.Printf("window: no Chromium browser found, the default browser will be used")
	}
	addr := *listen
	if addr == "" {
		port := settings.Port
		if port == 0 {
			port = 7979
		}
		addr = fmt.Sprintf("127.0.0.1:%d", port)
	}
	url := "http://" + strings.Replace(addr, "0.0.0.0", "127.0.0.1", 1) + "/"

	// single instance: if Cinearr already runs, just show it
	if alreadyRunning(url) {
		logger.Printf("Cinearr is already running, opening the window")
		openUI(url, dir, *browser, logger)
		return
	}

	ln, err := net.Listen("tcp", addr)
	if err != nil {
		// port taken by something else: fall back to a random local port
		ln, err = net.Listen("tcp", "127.0.0.1:0")
		if err != nil {
			fatal(err)
		}
		url = "http://" + ln.Addr().String() + "/"
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	a := app.New(st, logger)
	diag.Go("download monitoring", func() { a.Run(ctx) })

	apiServer := &api.Server{App: a, UI: web.Files(), Quit: cancel, ForceDebug: *debugMode,
		AllowAnyHost: *listen != "" && !strings.HasPrefix(*listen, "127.0.0.1")}
	srv := &http.Server{Handler: apiServer.Handler(), ReadHeaderTimeout: 10 * time.Second, ErrorLog: logger}
	go func() {
		if err := srv.Serve(ln); err != nil && !errors.Is(err, http.ErrServerClosed) {
			logger.Printf("server: %v", err)
			cancel()
		}
	}()
	logger.Printf("Cinearr %s started on %s (data: %s)", api.Version, url, dir)

	if !*noWindow {
		if w := openUI(url, dir, *browser, logger); w != nil {
			go func() {
				started := time.Now()
				w.Wait()
				if time.Since(started) > 5*time.Second {
					logger.Printf("window closed, stopping")
					cancel()
					return
				}
				// the browser handed the window to an already running
				// process: follow the interface heartbeat instead
				watchHeartbeat(ctx, apiServer, cancel, logger)
			}()
		} else {
			go watchHeartbeat(ctx, apiServer, cancel, logger)
		}
	}

	sig := make(chan os.Signal, 1)
	signal.Notify(sig, os.Interrupt)
	select {
	case <-sig:
	case <-ctx.Done():
	}
	shutdown, done := context.WithTimeout(context.Background(), 5*time.Second)
	defer done()
	_ = srv.Shutdown(shutdown)
}

// watchHeartbeat stops the application once the interface has not called
// the API for a while (window or browser tab closed).
func watchHeartbeat(ctx context.Context, s *api.Server, cancel context.CancelFunc, logger *log.Logger) {
	t := time.NewTicker(10 * time.Second)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			if last := s.LastSeen(); !last.IsZero() && time.Since(last) > 90*time.Second {
				logger.Printf("interface closed, stopping")
				cancel()
				return
			}
		}
	}
}

func alreadyRunning(url string) bool {
	c := &http.Client{Timeout: 2 * time.Second}
	resp, err := c.Get(url + "api/ping")
	if err != nil {
		return false
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
	return strings.Contains(string(b), `"cinearr"`)
}

// openUI opens the dedicated window, or the default browser as a fallback.
// It returns the window when one was created.
func openUI(url, dir string, useBrowser bool, logger *log.Logger) *desktop.Window {
	if !useBrowser {
		w, err := desktop.OpenWindow(url, filepath.Join(dir, "window"))
		if err == nil {
			return w
		}
		logger.Printf("dedicated window unavailable (%v), opening the browser", err)
	}
	if err := desktop.OpenBrowser(url); err != nil {
		logger.Printf("open %s in your browser", url)
	}
	return nil
}

func fatal(err error) {
	diag.Printf("fatal error: %v", err)
	fmt.Fprintln(os.Stderr, "Cinearr :", err)
	desktop.Alert(i18n.T("Cinearr cannot start"), err.Error())
	os.Exit(1)
}
