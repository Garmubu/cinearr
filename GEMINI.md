# GEMINI.md

Context for Gemini CLI. The project guide shared by every AI assistant is in AGENTS.md and is imported below; keep project knowledge there, not here.

@./AGENTS.md

## Gemini-specific notes

- Gemini CLI can also load AGENTS.md directly: set `"context": { "fileName": ["AGENTS.md", "GEMINI.md"] }` in `.gemini/settings.json` if you prefer.
- Run `gofmt -l .` and `make test` after Go changes, and `make e2e` after changes to searches, downloads, import or the interface.
- When you add a user-facing string, add it to the four catalogs in `internal/i18n/locales/` in the same change.
- Do not add dependencies, telemetry or network calls in tests. Never commit `dist/`, `bin/`, data folders or secrets.
