#!/bin/sh
# Uninstalls Cinearr, and its data if you wish. Your films, series and
# downloads are never deleted.
set -eu
dir=$(cd "$(dirname "$0")" && pwd)
data="${XDG_CONFIG_HOME:-$HOME/.config}/cinearr"
if [ -d "$dir/data" ]; then data="$dir/data"; fi

echo "Uninstall Cinearr from $dir"
echo
echo "  1) Uninstall Cinearr and keep its data (for a later reinstallation)"
echo "  2) Uninstall Cinearr and delete all its data: settings, list of films and series,"
echo "     history, logs and installed players ($data)"
echo "  3) Cancel"
echo
echo "Your films, series and downloads are never deleted."
printf 'Choice [1/2/3]: '
read -r choice || true
case "$choice" in
1) wipe=no ;;
2) wipe=yes ;;
*) echo "Nothing was changed."; exit 0 ;;
esac

port=$(sed -n 's/.*"port": *\([0-9]*\).*/\1/p' "$data/settings.json" 2>/dev/null | head -n 1)
port=${port:-7979}
if command -v curl >/dev/null 2>&1 && curl -fs -m 2 "http://127.0.0.1:$port/api/ping" >/dev/null 2>&1; then
	curl -fs -m 5 -X POST -H 'X-Cinearr: 1' "http://127.0.0.1:$port/api/quit" >/dev/null 2>&1 || true
	sleep 2
fi

apps="${XDG_DATA_HOME:-$HOME/.local/share}/applications"
desktop=$(xdg-user-dir DESKTOP 2>/dev/null || echo "$HOME/Desktop")
rm -f "$apps/cinearr.desktop" "$desktop/cinearr.desktop"
if [ "$(readlink "$HOME/.local/bin/cinearr" 2>/dev/null || true)" = "$dir/cinearr" ]; then rm -f "$HOME/.local/bin/cinearr"; fi
if [ "$wipe" = yes ]; then rm -rf "$data"; fi
for f in cinearr update.sh uninstall.sh cinearr.png LICENSE README.md; do
	rm -f "$dir/$f"
done
rmdir "$dir" 2>/dev/null || true
command -v update-desktop-database >/dev/null 2>&1 && update-desktop-database "$apps" >/dev/null 2>&1 || true

if [ "$wipe" = yes ]; then
	echo "Cinearr and its data are uninstalled."
else
	echo "Cinearr is uninstalled. Its data is kept in $data."
fi
