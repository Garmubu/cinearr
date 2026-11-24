# CLAUDE.md

Instructions for Claude Code. The project guide shared by every AI assistant is in AGENTS.md and is imported below; keep project knowledge there, not here.

@AGENTS.md

## Claude-specific notes

- Start a session by reading the part of the architecture you are about to change; the layout and the "Recipes" section of AGENTS.md say where things live.
- Run `gofmt -l .` and `make test` after Go changes, and `make e2e` after changes to searches, downloads, import or the interface. Report what you ran and its result.
- When you add a user-facing string, add it to the four catalogs in `internal/i18n/locales/` in the same change.
- Do not add dependencies, telemetry or network calls in tests. Never commit `dist/`, `bin/`, data folders or secrets.
