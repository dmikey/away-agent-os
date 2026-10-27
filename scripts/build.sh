#!/bin/sh
set -eu
cd "$(dirname "$0")/.."
python3 scripts/fetch-guest.py
CGO_ENABLED=0 GOOS=linux GOARCH=arm64 go build -trimpath -ldflags='-s -w' -o internal/macvm/assets/guest-init ./cmd/guest
python3 scripts/build-guest.py
(cd frontend && pnpm install --frozen-lockfile && pnpm run check && pnpm run build)
node scripts/check-transport.mjs
node scripts/check-window-intents.mjs
go test ./...
./scripts/package-macos.sh
