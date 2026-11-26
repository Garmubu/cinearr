// Package diag centralises logging and diagnostics: log file with rotation,
// crash capture, panic recovery for background tasks, a verbose "debug"
// mode logging every outgoing HTTP request (secrets redacted), and the
// redaction helpers used by the diagnostic export.
package diag

import (
	"bytes"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"runtime/debug"
	"strings"
	"sync/atomic"
	"time"
)

var (
	logger  = log.New(os.Stderr, "", log.LstdFlags)
	verbose atomic.Bool
	started = time.Now()
	dataDir string
)

// LogFile, PrevLogFile and CrashFile are the file names in the data folder.
const (
	LogFile     = "cinearr.log"
	PrevLogFile = "cinearr.log.1"
	CrashFile   = "crash.log"
)

// Setup opens the log file (rotating it above 5 MB), routes fatal crashes
// to crash.log and installs the logging HTTP transport.
func Setup(dir string, debugMode bool) (*log.Logger, func()) {
	dataDir = dir
	path := filepath.Join(dir, LogFile)
	if st, err := os.Stat(path); err == nil && st.Size() > 5<<20 {
		_ = os.Rename(path, filepath.Join(dir, PrevLogFile))
	}
	var out io.Writer = os.Stderr
	closeFn := func() {}
	if f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644); err == nil {
		// the file first: with the Windows GUI build stderr is not writable
		out = io.MultiWriter(f, os.Stderr)
		closeFn = func() { f.Close() }
	}
	// every line is cleaned of secrets and personal data before being
	// written, as Sonarr does: the log is meant to be shared in reports
	logger = log.New(redactWriter{out}, "", log.LstdFlags|log.Lmicroseconds)
	verbose.Store(debugMode)

	// unrecovered panics and fatal runtime errors (Go 1.23+)
	if f, err := os.OpenFile(filepath.Join(dir, CrashFile), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644); err == nil {
		_ = debug.SetCrashOutput(f, debug.CrashOptions{}) // keeps its own copy of the handle
		f.Close()
	}

	http.DefaultTransport = &Transport{Base: http.DefaultTransport}
	return logger, closeFn
}

// Logger returns the application logger.
func Logger() *log.Logger { return logger }

// SetDebug toggles verbose logging at runtime.
func SetDebug(on bool) {
	if verbose.Swap(on) != on {
		logger.Printf("[diag] detailed log %s", map[bool]string{true: "on", false: "off"}[on])
	}
}

// Debug reports whether verbose logging is on.
func Debug() bool { return verbose.Load() }

// Printf logs a line.
func Printf(format string, a ...any) { logger.Printf(format, a...) }

// Debugf logs a line in debug mode only.
func Debugf(format string, a ...any) {
	if verbose.Load() {
		logger.Printf("[debug] "+format, a...)
	}
}

// Uptime since start.
func Uptime() time.Duration { return time.Since(started) }

// Recover logs a recovered panic with its stack; use as `defer diag.Recover("task")`.
func Recover(task string) {
	if r := recover(); r != nil {
		ReportPanic(task, r)
	}
}

// ReportPanic writes a panic and its stack to the log and to crash.log.
func ReportPanic(task string, r any) {
	stack := debug.Stack()
	msg := fmt.Sprintf("PANIC in %s: %v\n%s", task, r, stack)
	logger.Print(msg)
	if dataDir != "" {
		if f, err := os.OpenFile(filepath.Join(dataDir, CrashFile), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644); err == nil {
			fmt.Fprintf(f, "=== %s (recovered) ===\n%s\n", time.Now().Format(time.RFC3339), msg)
			f.Close()
		}
	}
}

// Go runs fn in a goroutine that cannot bring the application down.
func Go(task string, fn func()) {
	go func() {
		defer Recover(task)
		fn()
	}()
}

// CrashReport returns the content of crash.log, if any.
func CrashReport() (string, time.Time) {
	if dataDir == "" {
		return "", time.Time{}
	}
	p := filepath.Join(dataDir, CrashFile)
	st, err := os.Stat(p)
	if err != nil || st.Size() == 0 {
		return "", time.Time{}
	}
	b, _ := os.ReadFile(p)
	return string(b), st.ModTime()
}

// AckCrash archives crash.log so the warning is not shown again.
func AckCrash() error {
	p := filepath.Join(dataDir, CrashFile)
	b, err := os.ReadFile(p)
	if err != nil || len(b) == 0 {
		return nil
	}
	arch := filepath.Join(dataDir, "crash-"+time.Now().Format("20060102-150405")+".log")
	if err := os.WriteFile(arch, b, 0o644); err != nil {
		return err
	}
	return os.Truncate(p, 0)
}

// SystemInfo describes the environment for bug reports.
func SystemInfo() string {
	var b strings.Builder
	fmt.Fprintf(&b, "System: %s/%s\n", runtime.GOOS, runtime.GOARCH)
	fmt.Fprintf(&b, "Go: %s\n", runtime.Version())
	fmt.Fprintf(&b, "Processors: %d\n", runtime.NumCPU())
	fmt.Fprintf(&b, "Running for: %s\n", Uptime().Round(time.Second))
	var m runtime.MemStats
	runtime.ReadMemStats(&m)
	fmt.Fprintf(&b, "Memory: %d MB, goroutines: %d\n", m.Alloc>>20, runtime.NumGoroutine())
	if bi, ok := debug.ReadBuildInfo(); ok {
		for _, s := range bi.Settings {
			if s.Key == "vcs.revision" || s.Key == "vcs.time" {
				fmt.Fprintf(&b, "%s: %s\n", s.Key, s.Value)
			}
		}
	}
	return b.String()
}

// ---------------------------------------------------------------------------
// Redaction

// ---------------------------------------------------------------------------
// HTTP transport

// Transport logs outgoing requests: errors and failed responses always,
// every request in debug mode (with the beginning of error bodies).
type Transport struct {
	Base http.RoundTripper
}

// RoundTrip implements http.RoundTripper.
func (t *Transport) RoundTrip(req *http.Request) (*http.Response, error) {
	start := time.Now()
	resp, err := t.Base.RoundTrip(req)
	dur := time.Since(start).Round(time.Millisecond)
	target := RedactURL(req.URL.String())
	if err != nil {
		logger.Printf("[http] %s %s: %v (%s)", req.Method, target, RedactText(err.Error()), dur)
		return resp, err
	}
	if resp.StatusCode >= 400 {
		snippet := ""
		if verbose.Load() {
			snippet = peekBody(resp)
		}
		logger.Printf("[http] %s %s -> %d (%s)%s", req.Method, target, resp.StatusCode, dur, snippet)
	} else if verbose.Load() && !quietPath(req.URL.Path) {
		logger.Printf("[debug] [http] %s %s -> %d %s (%s)", req.Method, target, resp.StatusCode, resp.Header.Get("Content-Type"), dur)
	}
	return resp, nil
}

// quietPath skips the periodic torrent client polls in debug mode
// (errors are always logged).
func quietPath(p string) bool {
	for _, s := range []string{"/api/v2/torrents/info", "/api/v2/app/version", "/api/v2/torrents/files", "/transmission/rpc", "/json", "/gui/"} {
		if strings.HasSuffix(p, s) {
			return true
		}
	}
	return false
}

// peekBody reads the start of the body for the log and restores it.
func peekBody(resp *http.Response) string {
	if resp.Body == nil {
		return ""
	}
	head := make([]byte, 600)
	n, _ := io.ReadFull(resp.Body, head)
	head = head[:n]
	resp.Body = struct {
		io.Reader
		io.Closer
	}{io.MultiReader(bytes.NewReader(head), resp.Body), resp.Body}
	text := strings.Join(strings.Fields(string(head)), " ")
	if text == "" {
		return ""
	}
	return ": " + RedactText(text)
}
