#!/usr/bin/env bash
# Builds the files of a release in dist/.
#
#   tools/release/package.sh VERSION [owner/repo]
#
# Produces:
#   Cinearr-V-windows-x64-portable.zip, Cinearr-V-windows-arm64-portable.zip
#   Cinearr-V-linux-x64.tar.gz, Cinearr-V-linux-arm64.tar.gz (with install.sh)
#   Cinearr-V-macos-apple-silicon.zip, Cinearr-V-macos-intel.zip (Cinearr.app)
#   windows-installer/  cinearr.exe, update.exe, uninstall.exe and LICENSE,
#                       for packaging/windows/cinearr.iss (Inno Setup)
#   macos-arm64.app.tar, macos-amd64.app.tar: Cinearr.app with its file
#                       modes, signed and put in a .dmg by the release workflow
#
# The OpenSubtitles.com application key is read from OPENSUBTITLES_API_KEY.
# Windows icons and version information need go-winres
# (go install github.com/tc-hib/go-winres@latest); without it the
# executables keep the default icon.
set -euo pipefail
version=${1:?usage: package.sh VERSION [owner/repo]}
repo=${2:-}
root=$(cd "$(dirname "$0")/../.." && pwd)
cd "$root"
out=dist
rm -rf "$out"
mkdir -p "$out"

ldflags="-s -w -X cinearr/internal/api.Version=$version -X cinearr/internal/update.Version=$version"
ldflags+=" -X cinearr/internal/update.Repo=$repo -X cinearr/internal/subtitles.BuiltinOpenSubtitlesKey=${OPENSUBTITLES_API_KEY:-}"
docs=(README.md LICENSE CREDITS.md CHANGELOG.md)

build() { # goos goarch output package [extra ldflags]
	GOOS=$1 GOARCH=$2 CGO_ENABLED=0 go build -trimpath -ldflags "$ldflags ${5:-}" -o "$3" "$4"
}

# Windows resources: icon, version information, manifest
numeric=$(printf '%s' "$version" | sed 's/[^0-9.].*//')
if command -v go-winres >/dev/null 2>&1; then
	for pair in cinearr:cinearr update:cinearr-update uninstall:cinearr-uninstall; do
		go-winres make --in "packaging/windows/winres-${pair%%:*}.json" --out "cmd/${pair##*:}/rsrc" \
			--arch amd64,arm64 --product-version "$numeric.0" --file-version "$numeric.0"
	done
else
	echo "warning: go-winres not found, the Windows executables keep the default icon" >&2
fi

# Windows: portable archives, and the files of the installer (x64)
mkdir -p "$out/windows-installer"
for arch in amd64:x64 arm64:arm64; do
	goarch=${arch%%:*} label=${arch##*:}
	dir="$out/Cinearr-$version-windows-$label"
	mkdir -p "$dir/docs"
	build windows "$goarch" "$dir/cinearr.exe" ./cmd/cinearr "-H=windowsgui"
	cp "${docs[@]}" "$dir/" && cp docs/guide.md "$dir/docs/"
	(cd "$out" && zip -qr "Cinearr-$version-windows-$label-portable.zip" "$(basename "$dir")")
	if [ "$goarch" = amd64 ]; then
		cp "$dir/cinearr.exe" LICENSE "$out/windows-installer/"
		build windows amd64 "$out/windows-installer/update.exe" ./cmd/cinearr-update "-H=windowsgui"
		build windows amd64 "$out/windows-installer/uninstall.exe" ./cmd/cinearr-uninstall "-H=windowsgui"
	fi
	rm -rf "$dir"
done

# Linux: archive with the install, update and uninstall scripts
for arch in amd64:x64 arm64:arm64; do
	goarch=${arch%%:*} label=${arch##*:}
	name="Cinearr-$version-linux-$label"
	dir="$out/$name"
	mkdir -p "$dir/docs"
	build linux "$goarch" "$dir/cinearr" ./cmd/cinearr
	cp packaging/linux/install.sh packaging/linux/uninstall.sh "$dir/"
	sed "s|@REPO@|$repo|" packaging/linux/update.sh > "$dir/update.sh"
	chmod +x "$dir"/*.sh "$dir/cinearr"
	cp packaging/icons/icon-256.png "$dir/cinearr.png"
	cp "${docs[@]}" "$dir/" && cp docs/guide.md "$dir/docs/"
	tar -czf "$out/$name.tar.gz" -C "$out" "$name"
	rm -rf "$dir"
done

# macOS: Cinearr.app
for arch in arm64:apple-silicon amd64:intel; do
	goarch=${arch%%:*} label=${arch##*:}
	stage="$out/macos-$goarch"
	app="$stage/Cinearr.app/Contents"
	mkdir -p "$app/MacOS" "$app/Resources"
	build darwin "$goarch" "$app/MacOS/cinearr" ./cmd/cinearr
	sed "s|@VERSION@|$version|g" packaging/macos/Info.plist > "$app/Info.plist"
	cp packaging/icons/cinearr.icns "$app/Resources/cinearr.icns"
	tar -cf "$out/macos-$goarch.app.tar" -C "$stage" Cinearr.app
	(cd "$stage" && zip -qry "../Cinearr-$version-macos-$label.zip" Cinearr.app)
	rm -rf "$stage"
done

(cd "$out" && sha256sum Cinearr-* > checksums.txt)
ls -la "$out"
