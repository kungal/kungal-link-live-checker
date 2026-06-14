#!/usr/bin/env sh
# Build-on-host deploy for kungal-link-live-checker (no Docker; PM2-managed).
# Zero deps + single binary => building on the box is trivial and fast.
#
# Layout:
#   /opt/link-checker/src   <- git clone of this repo
#   /opt/link-checker/llc   <- built binary (PM2 runs this)
#   /opt/link-checker/logs  <- PM2 logs
#
# Usage on the server:  sh /opt/link-checker/src/deploy/deploy.sh
set -eu

ROOT=/opt/link-checker
SRC="$ROOT/src"

mkdir -p "$ROOT/logs"
cd "$SRC"
git pull --ff-only
CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o "$ROOT/llc" ./cmd/server

if pm2 describe link-checker >/dev/null 2>&1; then
  pm2 reload link-checker --update-env
  echo "reloaded link-checker"
else
  echo "Binary built at $ROOT/llc. First start (set your key):"
  echo "  LLC_API_KEYS=\$(openssl rand -hex 16) pm2 start $SRC/deploy/ecosystem.config.js && pm2 save"
fi
