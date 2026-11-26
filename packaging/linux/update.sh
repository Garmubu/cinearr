#!/bin/sh
# Updates an installed Cinearr to the latest published version: downloads
# it, checks it against checksums.txt and replaces the program. Settings
# and library are kept.
set -eu
REPO="@REPO@"
dir=$(cd "$(dirname "$0")" && pwd)
current=$("$dir/cinearr" -version 2>/dev/null || echo 0.0.0)
api="${CINEARR_API:-https://api.github.com}"

fetch() { # url [output]
	if command -v curl >/dev/null 2>&1; then
		if [ $# -eq 2 ]; then curl -fsSL -o "$2" "$1"; else curl -fsSL "$1"; fi
	else
		if [ $# -eq 2 ]; then wget -qO "$2" "$1"; else wget -qO- "$1"; fi
	fi
}

if [ -z "$REPO" ] || [ "$REPO" = "@""REPO@" ]; then
	echo "This copy does not know where its updates are published (local build)." >&2
	exit 1
fi
json=$(fetch "$api/repos/$REPO/releases/latest") || { echo "GitHub cannot be reached." >&2; exit 1; }
latest=$(printf '%s' "$json" | sed -n 's/.*"tag_name": *"v\{0,1\}\([^"]*\)".*/\1/p' | head -n 1)
if [ -z "$latest" ]; then
	echo "No published version found." >&2
	exit 1
fi
newest=$(printf '%s\n%s\n' "$current" "$latest" | sort -V | tail -n 1)
if [ "$latest" = "$current" ] || [ "$newest" = "$current" ]; then
	echo "Cinearr $current is up to date."
	exit 0
fi

case "$(uname -m)" in
x86_64 | amd64) arch=x64 ;;
aarch64 | arm64) arch=arm64 ;;
*) echo "Unsupported processor: $(uname -m)" >&2; exit 1 ;;
esac
name="Cinearr-$latest-linux-$arch.tar.gz"
url=$(printf '%s' "$json" | tr ',' '\n' | sed -n "s|.*\"browser_download_url\": *\"\([^\"]*/$name\)\".*|\1|p" | head -n 1)
sums=$(printf '%s' "$json" | tr ',' '\n' | sed -n 's|.*"browser_download_url": *"\([^"]*/checksums.txt\)".*|\1|p' | head -n 1)
if [ -z "$url" ] || [ -z "$sums" ]; then
	echo "The files of version $latest are not available yet: try again in a few minutes." >&2
	exit 1
fi

printf 'Cinearr %s is available (installed: %s). Update now? [Y/n] ' "$latest" "$current"
read -r answer || true
case "$answer" in [nN]*) exit 0 ;; esac

tmp=$(mktemp -d)
trap 'rm -rf "$tmp"' EXIT
fetch "$url" "$tmp/$name"
fetch "$sums" "$tmp/checksums.txt"
(cd "$tmp" && grep " $name\$" checksums.txt | sha256sum -c --quiet -) || {
	echo "Wrong checksum: the download is corrupted or modified, nothing was changed." >&2
	exit 1
}
tar -xzf "$tmp/$name" -C "$tmp"
src=$(dirname "$(find "$tmp" -name cinearr -type f | head -n 1)")

# a running Cinearr is asked to stop
data="${XDG_CONFIG_HOME:-$HOME/.config}/cinearr"
if [ -d "$dir/data" ]; then data="$dir/data"; fi
port=$(sed -n 's/.*"port": *\([0-9]*\).*/\1/p' "$data/settings.json" 2>/dev/null | head -n 1)
port=${port:-7979}
if curl -fs -m 2 "http://127.0.0.1:$port/api/ping" >/dev/null 2>&1; then
	curl -fs -m 5 -X POST -H 'X-Cinearr: 1' "http://127.0.0.1:$port/api/quit" >/dev/null 2>&1 || true
	sleep 2
fi

for f in cinearr update.sh uninstall.sh cinearr.png LICENSE README.md; do
	if [ -e "$src/$f" ]; then cp "$src/$f" "$dir/$f.new" && mv -f "$dir/$f.new" "$dir/$f"; fi
done
chmod +x "$dir/cinearr" "$dir/update.sh" "$dir/uninstall.sh"
echo "Cinearr is updated to $latest."
