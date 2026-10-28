#!/bin/sh
set -eu
cd "$(dirname "$0")/.."
# Build a production binary. Bundle/sign outside synced Documents folders to avoid FinderInfo attributes.
./scripts/build-icon.sh
wails build -s -skipbindings -nopackage -o 'Away OS'
AWAY_STAGE=$(mktemp -d "${TMPDIR:-/tmp}/away-package.XXXXXX")
AWAY_APP="$AWAY_STAGE/Away OS.app"
mkdir -p "$AWAY_APP/Contents/MacOS" "$AWAY_APP/Contents/Resources"
cp -X 'build/bin/Away OS' "$AWAY_APP/Contents/MacOS/Away OS"
cp -X build/Info.plist "$AWAY_APP/Contents/Info.plist"
cp -X build/iconfile.icns "$AWAY_APP/Contents/Resources/iconfile.icns"
xattr -cr "$AWAY_APP"
# Prefer a single installed Developer ID certificate; never guess between teams.
AWAY_SIGN_IDENTITY=${AWAY_SIGN_IDENTITY:-}
if [ -z "$AWAY_SIGN_IDENTITY" ]; then
  AWAY_IDENTITIES=$(security find-identity -v -p codesigning | awk '/"Developer ID Application:/ {print $2}')
  AWAY_ID_COUNT=$(printf '%s\n' "$AWAY_IDENTITIES" | awk 'NF {n++} END {print n+0}')
  case "$AWAY_ID_COUNT" in
    0) AWAY_SIGN_IDENTITY=- ;;
    1) AWAY_SIGN_IDENTITY=$AWAY_IDENTITIES ;;
    *) printf '%s\n' 'Multiple Developer ID certificates found. Set AWAY_SIGN_IDENTITY explicitly.' >&2; exit 1 ;;
  esac
fi
if [ "$AWAY_SIGN_IDENTITY" = - ]; then
  printf '%s\n' 'Signing: ad-hoc development build (no Developer ID selected).'
  codesign --force --sign - --entitlements build/darwin-entitlements.plist "$AWAY_APP"
else
  printf '%s\n' 'Signing with Developer ID / selected certificate, hardened runtime and secure timestamp.'
  codesign --force --sign "$AWAY_SIGN_IDENTITY" --options runtime --timestamp --entitlements build/darwin-entitlements.plist "$AWAY_APP"
fi
codesign --verify --deep --strict "$AWAY_APP"
ditto --norsrc -c -k --keepParent "$AWAY_APP" 'build/bin/Away OS-macOS-arm64.zip'
printf '\nNative app: %s\nArchive: %s/build/bin/Away OS-macOS-arm64.zip\n' "$AWAY_APP" "$PWD"
