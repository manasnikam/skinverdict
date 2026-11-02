#!/usr/bin/env bash
# Build the static Linux binary + UI on your Mac and push it to the VPS.
#   VPS=user@host [ARCH=amd64|arm64] bash deploy/deploy.sh
set -euo pipefail
: "${VPS:?set VPS=user@host}"
ARCH="${ARCH:-amd64}"
cd "$(dirname "$0")/.."
(cd web && npm install --silent && npm run build --silent)
CGO_ENABLED=0 GOOS=linux GOARCH="$ARCH" go build -ldflags="-s -w" -o bin/skinverdict-linux ./cmd/server
scp -q bin/skinverdict-linux "$VPS:/tmp/skinverdict.new"
scp -q data/catalog.json "$VPS:/tmp/catalog.json"
ssh "$VPS" 'sudo install -o skinverdict -g skinverdict -m 755 /tmp/skinverdict.new /opt/skinverdict/skinverdict \
  && sudo install -o skinverdict -g skinverdict -m 644 /tmp/catalog.json /opt/skinverdict/catalog.json \
  && sudo systemctl restart skinverdict && sleep 1 && sudo systemctl --no-pager --lines=5 status skinverdict'
