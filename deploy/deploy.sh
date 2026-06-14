#!/usr/bin/env sh
# Refresh kungal-link-live-checker under PM2 (no Docker needed).
#
# Config + secrets live in /opt/link-checker/.env (chmod 600); the PM2 ecosystem
# reads them. Do NOT add `--update-env` to the reload — it re-reads env from the
# shell (which lacks LLC_API_KEYS) and would drop the key, leaving the server
# fail-closed (every /v1 request 401).
#
# Getting the new binary in place, pick one:
#   - Go installed on host : this script builds /opt/link-checker/llc from src.
#   - No Go (e.g. the egress box): cross-compile elsewhere and scp it first —
#       CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -trimpath -ldflags="-s -w" \
#         -o llc ./cmd/server && scp llc <host>:/opt/link-checker/llc
#     then run this script.
set -eu
ROOT=/opt/link-checker

if command -v go >/dev/null 2>&1 && [ -d "$ROOT/src/.git" ]; then
  cd "$ROOT/src" && git pull --ff-only
  CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o "$ROOT/llc" ./cmd/server
  echo "built $ROOT/llc from source"
else
  echo "Go/src not present — using the already-placed (scp'd) $ROOT/llc."
fi

if pm2 describe link-checker >/dev/null 2>&1; then
  pm2 reload link-checker          # graceful reload of the new binary; keeps .env-sourced env
  echo "reloaded link-checker"
else
  cd "$ROOT" && pm2 start ecosystem.config.js && pm2 save
  echo "started link-checker (pm2 save persisted it)"
fi
