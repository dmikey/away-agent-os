#!/bin/sh
set -eu
cd "$(dirname "$0")/.."
# Build a production binary. Bundle/sign outside synced Documents folders to avoid FinderInfo attributes.
wails build -s -skipbindings -nopackage -o 'Away OS'
AWAY_STAGE=$(mktemp -d "${TMPDIR:-/tmp}/away-package.XXXXXX")
AWAY_APP="$AWAY_STAGE/Away OS.app"
mkdir -p "$AWAY_APP/Contents/MacOS" "$AWAY_APP/Contents/Resources"
cp -X 'build/bin/Away OS' "$AWAY_APP/Contents/MacOS/Away OS"
cp -X build/Info.plist "$AWAY_APP/Contents/Info.plist"
cp -X build/iconfile.icns "$AWAY_APP/Contents/Resources/iconfile.icns"
xattr -cr "$AWAY_APP"
codesign --force --deep --sign - --entitlements build/darwin-entitlements.plist "$AWAY_APP"
codesign --verify --deep --strict "$AWAY_APP"
ditto --norsrc -c -k --keepParent "$AWAY_APP" 'build/bin/Away OS-macOS-arm64.zip'
printf '\nNative app: %s\nArchive: %s/build/bin/Away OS-macOS-arm64.zip\n' "$AWAY_APP" "$PWD"
