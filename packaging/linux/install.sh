#!/bin/sh
# Installs Cinearr for the current user: the program in a folder of your
# choice (default ~/.local/share/cinearr), an entry in the applications
# menu, a "cinearr" command and, if you wish, a desktop shortcut.
# Usage: ./install.sh [folder]
set -eu
here=$(cd "$(dirname "$0")" && pwd)
apps="${XDG_DATA_HOME:-$HOME/.local/share}/applications"
entry="$apps/cinearr.desktop"

# already installed: point to the updater and the uninstaller
if [ -f "$entry" ]; then
	old=$(sed -n 's/^Path=//p' "$entry")
	if [ -n "$old" ] && [ -x "$old/cinearr" ]; then
		echo "Cinearr $("$old/cinearr" -version 2>/dev/null || echo) is already installed in:"
		echo "  $old"
		echo
		echo "To update it:                       $old/update.sh"
		echo "To uninstall it (then reinstall):   $old/uninstall.sh"
		exit 1
	fi
fi

dir="${1:-}"
if [ -z "$dir" ]; then
	default="${XDG_DATA_HOME:-$HOME/.local/share}/cinearr"
	printf 'Installation folder [%s]: ' "$default"
	read -r dir || true
	dir="${dir:-$default}"
fi
case "$dir" in "~"*) dir="$HOME${dir#\~}" ;; esac
mkdir -p "$dir"
dir=$(cd "$dir" && pwd)

for f in cinearr update.sh uninstall.sh cinearr.png LICENSE README.md; do
	if [ -e "$here/$f" ]; then cp "$here/$f" "$dir/"; fi
done
chmod +x "$dir/cinearr" "$dir/update.sh" "$dir/uninstall.sh"

mkdir -p "$apps"
cat > "$entry" <<DESKTOP
[Desktop Entry]
Type=Application
Name=Cinearr
Comment=Find, download and watch films and series
Exec="$dir/cinearr"
Path=$dir
Icon=$dir/cinearr.png
Terminal=false
Categories=AudioVideo;Video;Network;
DESKTOP
command -v update-desktop-database >/dev/null 2>&1 && update-desktop-database "$apps" >/dev/null 2>&1 || true

mkdir -p "$HOME/.local/bin"
ln -sf "$dir/cinearr" "$HOME/.local/bin/cinearr"

desktop=$(xdg-user-dir DESKTOP 2>/dev/null || echo "$HOME/Desktop")
if [ -d "$desktop" ]; then
	printf 'Add a shortcut on the desktop? [y/N] '
	read -r answer || true
	case "$answer" in
	[yYoO]*)
		cp "$entry" "$desktop/cinearr.desktop"
		chmod +x "$desktop/cinearr.desktop"
		command -v gio >/dev/null 2>&1 && gio set "$desktop/cinearr.desktop" metadata::trusted true 2>/dev/null || true
		;;
	esac
fi

echo
echo "Cinearr is installed in $dir"
echo "Start it from the applications menu, or with the command: cinearr"
echo "Update: $dir/update.sh    Uninstall: $dir/uninstall.sh"
