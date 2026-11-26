# Contributing to Cinearr

Thanks for your interest! Bug reports, translations, new torrent clients, subtitle providers or players, and fixes are all welcome.

If you work with an AI coding assistant, it will find the project guide in [AGENTS.md](AGENTS.md) (also loaded through `CLAUDE.md`, `GEMINI.md` and `.github/copilot-instructions.md`). That file is the reference for the architecture and the rules below; this one is the short version for humans.

## Reporting a problem

1. Reproduce it with **Detailed log** enabled (*Settings › Data and diagnostics*).
2. Click **Export a diagnostic**: the zip contains the log, the version, the system, the settings, the library and the history, with passwords, keys, cookies and passkeys hidden.
3. Open an issue describing what you did, what you expected and what happened, and attach the zip.

Never post your settings file or a log exported by other means: they may contain credentials.

## Development setup

You only need **Go 1.23 or newer**. The project has no external dependency.

```bash
git clone https://github.com/<you>/cinearr && cd cinearr
go run ./cmd/cinearr -debug -data ./data    # a separate data folder for development
```

Useful options: `-browser` (open in your browser), `-no-window`, `-listen 127.0.0.1:7979`.

| Command | What it does |
|---|---|
| `make test` | `go vet` for Linux, Windows and macOS, then `go test ./...` |
| `make lint` | golangci-lint v2 with the configuration in `.golangci.yml` (optional) |
| `make e2e` | End-to-end tests (see below) |
| `make release` | Builds every release file into `dist/` (`tools/release/package.sh`) |
| `gofmt -l .` | Must print nothing |

### End-to-end tests

`tools/e2e` starts the real application on `http://127.0.0.1:7980` against local fakes: TMDB (films and the series *Twin Peaks*), a Cardigann tracker that also serves a malicious release, qBittorrent and Transmission that “download” sparse files, and a fake VLC that records its arguments. Nothing goes to the network.

```bash
pip install playwright && playwright install chromium
make e2e
```

It runs `series_api.py` (series flow through the API) then `drive_ui.py` (setup wizard, films, series, settings, reset, in French), and fails on any error, including JavaScript console errors. Screenshots land in `e2e-shots/`.

You can also start the harness alone and use the interface by hand:

```bash
PATH="$PWD/tools/e2e/fakebin:$PATH" go run ./tools/e2e
```

`tools/e2e/screenshots.py` regenerates the screenshots of the README with demonstration data only (open films of the Blender Foundation, public-domain classics, a Creative Commons series; the posters are generated):

```bash
E2E_DEMO=1 PATH="$PWD/tools/e2e/fakebin:$PATH" go run ./tools/e2e > /tmp/e2e.log &
python3 tools/e2e/screenshots.py
```

The detailed documentation for users is [docs/guide.md](docs/guide.md); keep the README short and update the guide with every user-visible change.

## Ground rules

- **Standard library only**, `CGO_ENABLED=0`, and the six targets must keep building. Put OS-specific code in files with build constraints.
- **Interface**: plain JavaScript, HTML and CSS in `web/static/`, no framework or build step. The content security policy forbids inline handlers: use `data-act` attributes and the `actions` object.
- **Safety**: never execute a downloaded file; keep the content checks; never log secrets.
- **Faithful ports**: logic taken from Radarr, Sonarr, Prowlarr or Bazarr keeps its behaviour and its regular expressions (they run on `internal/rx`, a .NET-compatible engine). Mention the source in a comment and in [CREDITS.md](CREDITS.md).
- **Tests without network**: use `httptest` servers for every remote service.
- **Small, focused pull requests**, with tests for new behaviour and a description of what you checked.

## Translations

The interface and the messages are written in English in the code and translated through one catalog per language in `internal/i18n/locales/` (`fr.json`, `es.json`, `it.json`, `de.json`). The key is the English text, the value its translation:

```json
"{n} episodes selected": "{n} épisodes sélectionnés"
```

- Keep the placeholders exactly (`{name}`, `%s`, `%d`, `%%`): `go test ./internal/i18n` checks them.
- A missing key falls back to English, so partial catalogs are fine.
- To add a language: copy `fr.json`, translate the values, then add the language to `i18n.Languages` (`internal/i18n/i18n.go`) and to `UI_LANGUAGES` and `LOCALES` in `web/static/app.js`.

## Where things live

| You want to… | Look at |
|---|---|
| Change how releases are accepted or ranked | `internal/decision` (films: `decision.go`, series: `episodes.go`) |
| Recognise a new release naming scheme | `internal/parser` (`parser.go`, `episode.go`) with test cases |
| Support a new torrent client | `internal/dlclient` (implement `Client`, register it in `Clients` and `New`) |
| Add a subtitle provider | `internal/subtitles` (`catalog.go` + a provider) |
| Support another player | `internal/player` |
| Change searches, downloads or imports | `internal/app` (`movies.go`, `series.go`, `release.go`, `monitor.go`, `importer.go`) |
| Change the interface | `web/static/app.js`, `app.css` |
| Add an API route | `internal/api` (`api.go`, `series.go`) |

## Releases

To publish a version, push a branch named `release/MAJOR.MINOR.PATCH`:

```bash
git push origin main:release/0.2.0
```

The release workflow (`.github/workflows/release.yml`):

1. runs the tests on Windows, macOS and Linux;
2. builds every file with `tools/release/package.sh` (portable Windows archives, Linux archives with `install.sh`, `update.sh` and `uninstall.sh`, `Cinearr.app`, and the files of the Windows installer), with the icons and version information made by `go-winres` from `packaging/windows/winres-*.json`;
3. builds `Cinearr-Setup-X.Y.Z.exe` with Inno Setup (`packaging/windows/cinearr.iss`) on Windows, and the `.dmg` disk images (ad hoc signed) on macOS;
4. creates the tag `v0.2.0` and publishes the GitHub release with every file and `checksums.txt`.

The Windows updater (`update.exe`, `cmd/cinearr-update`) and the in-app update check look for the installer and `checksums.txt` in the latest release: keep the file names of `package.sh` and of the installer. When `CHANGELOG.md` has an entry `## [0.2.0]`, it becomes the text of the release. The workflow stops if the branch name is not a version or if the version already exists. The repository secrets `OPENSUBTITLES_API_KEY` and `TMDB_API_KEY`, when set, are built into the executables so that users need no key of their own; they must never appear in the repository. The *Built-in keys* step of the workflows (and the `keys` job of the CI) says which ones are set, without showing them. To build with keys locally: `OPENSUBTITLES_API_KEY=… TMDB_API_KEY=… make build`.

## License

By contributing, you agree that your contributions are licensed under the GNU General Public License v3.0, like the rest of the project.
