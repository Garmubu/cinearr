#!/usr/bin/env bash
# Reports which application keys will be built into Cinearr, without
# showing them: OPENSUBTITLES_API_KEY and TMDB_API_KEY, from the secrets of
# the repository (Settings › Secrets and variables › Actions). A missing key
# is a warning: users then enter their own.
set -euo pipefail
summary=${GITHUB_STEP_SUMMARY:-/dev/null}
echo "### Built-in keys" >> "$summary"
for name in OPENSUBTITLES_API_KEY TMDB_API_KEY; do
	if [ -n "${!name:-}" ]; then
		echo "$name: set"
		echo "- $name: set" >> "$summary"
	else
		echo "::warning::$name is not set: this build has no built-in key for it, users will have to enter their own."
		echo "- $name: **not set**" >> "$summary"
	fi
done
