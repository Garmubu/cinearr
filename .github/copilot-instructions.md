# Copilot instructions

Read [AGENTS.md](../AGENTS.md) at the root of the repository: it is the guide for every AI assistant (architecture, commands, rules, recipes). The essentials:

- Go with the **standard library only** (no new modules), `CGO_ENABLED=0`, Windows/macOS/Linux. Interface: vanilla JavaScript in `web/static/app.js`, no framework, no build step, strict CSP (no inline handlers: use `data-act` and the `actions` object).
- Logic ported from Radarr, Sonarr, Prowlarr and Bazarr stays faithful; their .NET regular expressions run on `internal/rx`.
- Every user-facing string is written in English and added to `internal/i18n/locales/{fr,es,it,de}.json`.
- Safety: never execute downloaded files, keep the torrent content checks, never log secrets (use `diag.RedactText`).
- Store data is changed only through `Store.UpdateMovie` / `Store.UpdateSeries`.
- Library files are never deleted directly: they go through the recycle bin (`app/recycle.go`).
- Keep the README short; user-visible changes go into `docs/guide.md`.
- Check with `gofmt -l .`, `make test` and, for searches, downloads, import or the interface, `make e2e`.
