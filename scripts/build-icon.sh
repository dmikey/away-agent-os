#!/bin/sh
set -eu
cd "$(dirname "$0")/.."
# Keep Finder, Dock and Retina variants derived from the same 1024px artwork.
AWAY_ICON_STAGE=$(mktemp -d "${TMPDIR:-/tmp}/away-icon.XXXXXX")
trap 'rm -rf "$AWAY_ICON_STAGE"' EXIT
mkdir "$AWAY_ICON_STAGE/away.iconset"
for size in 16 32 128 256 512; do
  sips -z "$size" "$size" build/appicon.png --out "$AWAY_ICON_STAGE/away.iconset/icon_${size}x${size}.png" >/dev/null
  twice=$((size * 2))
  sips -z "$twice" "$twice" build/appicon.png --out "$AWAY_ICON_STAGE/away.iconset/icon_${size}x${size}@2x.png" >/dev/null
done
iconutil -c icns "$AWAY_ICON_STAGE/away.iconset" -o build/iconfile.icns
