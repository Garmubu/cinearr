# AGENTS.md — guide for AI coding assistants

This file describes Cinearr for AI assistants (Codex, Claude Code, Gemini CLI, Copilot, Cursor…). `CLAUDE.md`, `GEMINI.md` and `.github/copilot-instructions.md` point here. Human contributors: see [CONTRIBUTING.md](CONTRIBUTING.md).

## The project in one paragraph

Cinearr is a single desktop executable that does the job of Radarr (films), Sonarr (series), Prowlarr (indexers) and Bazarr (subtitles), drives the user's own torrent client (qBittorrent, Transmission, Deluge, µTorrent) and opens their video player (VLC, MPC-HC, mpv). It is written in **Go with the standard library only**; the interface is a **vanilla JavaScript single-page app** embedded in the binary and shown in a Chromium-based browser in application mode. Much of the logic is **ported** from the *arr projects and Bazarr (see `CREDITS.md`).

## Commands

```bash
go build ./...                 # build everything
go run ./cmd/cinearr -debug    # run the app (opens a window; -browser / -no-window)
make test                      # go vet (linux, windows, darwin) + go test ./...
make lint                      # golangci-lint v2, configured in .golangci.yml (if installed)
make e2e                       # end-to-end: fake TMDB/tracker/torrent clients + Playwright
make release                   # cross-compile the 6 targets into dist/
gofmt -l .                     # must print nothing
```

Before declaring a change done: `gofmt -l .` is empty, `make test` passes and, for anything touching searches, downloads, import or the interface, `make e2e` passes (needs Python 3 and `pip install playwright && playwright install chromium`). Unit tests never use the network: they rely on `httptest` servers.

## Hard rules

1. **No new dependencies.** `go.mod` has no `require`; keep it so. No npm, no bundler, no framework for the interface.
2. **CGO_ENABLED=0** must keep working on Windows, macOS and Linux (amd64 and arm64). OS-specific code goes in files with build constraints (`*_windows.go`, `*_other.go`, `disk_unix.go`).
3. **Safety is a feature.** Never execute downloaded files. Keep the content checks before sending a release (`torrent.Inspect` in `app/release.go`), on magnet metadata (`app/monitor.go`) and at import (`app/importer.go`). Do not weaken the API guard (localhost only, `X-Cinearr: 1` header on non-GET requests) or the content security policy in `internal/api/api.go`.
4. **Secrets.** Settings hold passwords, API keys and cookies. Never log them on purpose: the log writer cleans every line with `diag.RedactText` (Sonarr's rules: keys, passwords, tokens, cookies, passkeys, e-mails, user names in home paths), and URLs go through `diag.RedactURL`; add a rule in `internal/diag/redact.go` (with a test) for any new kind of secret. The OpenSubtitles application key is injected at build time from the `OPENSUBTITLES_API_KEY` environment variable and must never be committed.
5. **Every user-facing string is translated** (see Translations). English is the source language.
6. **Ported code stays faithful.** Regular expressions from Radarr, Sonarr and Prowlarr use .NET syntax and run on `internal/rx`: do not rewrite them for Go's `regexp`. Name the original class or file in a comment, and add new borrowings to `CREDITS.md`.
7. **The user's files are precious.** Nothing deletes library files unless the user ticked “also delete the files”, and even then they go through the recycle bin (`app/recycle.go`). The reset (`Store.Reset`) keeps the library and never touches files.
8. **Documentation.** The README is a short presentation for people discovering the project: keep it short, with no comparison to other software. Every user-visible change goes into the user guide `docs/guide.md`. Screenshots only show the demonstration data (`tools/e2e/screenshots.py`).

## Architecture

```
cmd/cinearr/            entry point: flags, data folder, single instance, window, shutdown
web/                    embedded interface (embed.go) — static/ = index.html, app.js, app.css
internal/
  api/                  HTTP JSON API under /api, used only by the interface
    api.go              routes, guard, settings, discovery, films, indexers, activity
    series.go           series routes, reset
    diagnostic.go       diagnostic zip (secrets redacted)
  app/                  orchestration (the "service" layer)
    app.go              App struct, cached clients, busy flags, history, background loop
    movies.go           films: add, refresh, search, automatic search, grab, cancel, delete
    series.go           series: add (seasons from TMDB), refresh, searches, season packs, grab, cancel
    release.go          fetch .torrent / magnet, safety check, send to the torrent client
    monitor.go          polling of the torrent client for films and series, cleanup after seeding
    importer.go         checks and import into the library (hard link, copy or move)
    scan.go             reading of the films and series folders (at launch and on demand), folder change, rebuild of a damaged library
    folders.go          checks of the films and series folders (system folders, nesting, download folder)
    recycle.go          recycle bin of the library folders
    media.go            subtitles (search, save, automatic) and playback
  decision/             decision engine: decision.go (films, Radarr), episodes.go (series, Sonarr)
  parser/               release name parser: parser.go, quality.go, language.go, releasegroup.go (Radarr), episode.go (series)
  quality/              quality catalogue and definitions, file extension risks
  indexer/              manager (parallel searches), repository (Prowlarr definitions), torznab.go
  indexer/cardigann/    Cardigann engine (port of Prowlarr): definitions, templates, filters, login, search
  dlclient/             torrent clients behind one interface: qbittorrent, transmission, deluge, utorrent
  qbit/                 qBittorrent Web API client (used by dlclient)
  tmdb/                 TMDB client: tmdb.go (films), tv.go (series, seasons)
  subtitles/            Bazarr scores (subtitles.go), provider catalog (catalog.go), providers
  player/               detection, launch and automatic installation of VLC / MPC-HC / mpv
  store/                persistence as JSON files in the data folder
  model/                shared types: Release, Movie, Series, Season, Episode, Download…
  i18n/                 translations: i18n.go + locales/{fr,es,it,de}.json
  rx/                   .NET-compatible regular expression engine
  dom/, yaml/           HTML/CSS-selector and YAML parsers for Cardigann definitions
  torrent/              bencode, info-hash, magnets, safety inspection of file lists
  diag/                 log, detailed mode, crash capture, secret redaction (redact.go)
  proxy/                HTTP/SOCKS5 proxy for the indexers (and optionally TMDB, subtitles); local addresses direct
  backoff/              pause of the indexers and subtitle providers that keep failing (Radarr's escalation)
  desktop/              application window, native dialogs, folder picker
tools/e2e/              end-to-end harness (fakes) and Python drivers
```

### Data flow

`Discover (TMDB) → library (store) → search (indexer manager → Cardigann/Torznab) → decision (parser + profile) → release.send (safety check, torrent client) → monitor (poll every 5 s) → importer (checks, library) → media (subtitles, player)`.

### State and persistence

- `store.Store` keeps everything in memory and writes JSON files atomically (`settings.json`, `library.json`, `series.json`, `history.json`, `blocklist.json`).
- Getters return **copies**. Change data only through `Store.UpdateMovie(id, fn)` / `Store.UpdateSeries(id, fn)`, which run `fn` under the lock and save only when something changed. Never keep a pointer into the store.
- Old settings are migrated in `Settings.migrate()` (called on load and save); add migrations there when a field changes.
- Film status: `wanted → downloading → importing → available` (or `failed`). Episode status: `""` (nothing asked) then the same values; a series download (`model.SeriesDownload`) lists the episodes it covers.

### Interface (`web/static/app.js`)

- One file, no build step. Views are functions `viewX(v, arg)` selected by the hash route (`#/discover`, `#/library/series`, `#/movie/27205`, `#/series/1920`, `#/settings/subtitles`…).
- **Strict CSP**: no inline handlers or scripts. Clicks are delegated: elements carry `data-act="name"` and the handler lives in the `actions` object. Checkboxes keep their native behaviour.
- Escape every dynamic value with `esc()`; translate with `t('English text', {params})` (or `te()` when the result goes into HTML and contains user data).
- `refreshStatus()` polls `/api/status`, `/api/movies` and `/api/series` every 3 s and calls `state.refresh` to redraw the current view.

### Translations

- English strings are written directly in the code: Go uses `i18n.T`, `i18n.Tf` (printf-style), `i18n.New`, `i18n.Errorf`; history messages are format strings translated by `a.history` / `a.seriesHistory`; the interface uses `t()`, `te()`, `plural()`.
- Each new string must be added to the four catalogs `internal/i18n/locales/{fr,es,it,de}.json` (key = the English text). Missing keys fall back to English. `go test ./internal/i18n` checks that every translation keeps the placeholders (`%s`, `%d`, `{name}`).
- Product names, quality names (Bluray-1080p) and release titles are not translated.

## Recipes

- **Add a torrent client**: implement `dlclient.Client` in `internal/dlclient/<name>.go`, add it to `Clients` and to `New` in `dlclient.go`, with tests on an `httptest` server. The interface lists clients from `/api/download-clients`.
- **Add a subtitle provider**: implement `subtitles.Provider` (and `Tester`; `SupportsEpisodes()` if it has series), register it in `Catalog()` in `catalog.go` with its fields and its `Films` / `Series` flags, add a test with a fake server, credit the source in `CREDITS.md`. Users can also add sites themselves without code: `custom-api` (any OpenSubtitles-compatible API, reusing the `OpenSubtitles` client with another address) and `custom-link` (a search page opened in the browser, then *Add a subtitle file*).
- **Add a player**: detection and arguments in `internal/player/player.go` (family, arguments for title, subtitles, languages, resume), optional installer in `install.go`.
- **Add an interface language**: add it to `i18n.Languages`, create `locales/<code>.json` from `fr.json`, add it to `UI_LANGUAGES` and `LOCALES` in `app.js`.
- **Add a setting**: field in `store.Settings` (with a default in `Defaults()` and a migration if needed), form field in the right tab of `drawSettingsTab()` using `field()` / `check()` / `select()` with a `data-bind` path, translations.
- **Change the decision rules**: `internal/decision`; keep the rejection messages translated and add a case to the tests.

## Testing notes

- `internal/parser/testdata/radarr_cases.json` holds 683 cases extracted from Radarr's NUnit fixtures: they must all pass.
- `tools/e2e/main.go` starts the real application on `127.0.0.1:7980` with fake TMDB (films Inception…, series Twin Peaks), a fake Cardigann tracker (including a malicious release) and fake qBittorrent and Transmission that “download” sparse files. `tools/e2e/fakebin/vlc` is a fake player that records its arguments.
- `tools/e2e/series_api.py` tests the series flow through the API; `tools/e2e/drive_ui.py` drives the whole interface (in French) and fails on any console error; `tools/e2e/screenshots.py` regenerates `docs/screenshots/` from the demonstration data (`E2E_DEMO=1`: open films, public-domain classics, a Creative Commons series; generated posters).

## Style

- `gofmt`, comments and identifiers in English, short doc comments on exported identifiers and on non-obvious functions.
- Errors: lowercase messages (product names excepted), wrapped with `%w` when they carry a cause.
- JSON fields in camelCase. API handlers return `(any, error)`; the error message is shown to the user, so it must be translated and understandable.
- Prefer small shared helpers over copies: films and series share the monitoring, import, subtitle and playback helpers in `internal/app`.
- Keep the binary small and the interface fast: no heavy assets, lazy loading of posters, no polling faster than the existing loops.
