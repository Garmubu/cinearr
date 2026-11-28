#!/bin/sh
# Runs the end-to-end tests against the local fakes (no network needed):
#   1. series_api.py  - the series flow through the API
#   2. drive_ui.py    - the whole interface with Playwright (setup wizard,
#                       films, series, settings, reset)
# Needs Python 3 and Playwright: pip install playwright && playwright install chromium
set -e
cd "$(dirname "$0")/../.."
# built like an official release: with a built-in TMDB key (the fake TMDB
# accepts it), so the wizard has no TMDB step
go build -ldflags "-X cinearr/internal/tmdb.BuiltinKey=e2e-built-in" -o bin/e2e ./tools/e2e
export PATH="$PWD/tools/e2e/fakebin:$PATH"
export CINEARR_PLAYER_LOG=/tmp/cinearr-player-args.txt

start() {
	bin/e2e > /tmp/e2e.log 2>&1 &
	pid=$!
	i=0
	until grep -q '^E2E ' /tmp/e2e.log 2>/dev/null; do
		i=$((i + 1))
		[ $i -gt 100 ] && { cat /tmp/e2e.log; kill $pid; exit 1; }
		sleep 0.1
	done
}
stop() { kill $pid 2>/dev/null; wait $pid 2>/dev/null || true; }
trap stop EXIT

start
python3 tools/e2e/series_api.py
stop
start
python3 tools/e2e/drive_ui.py
