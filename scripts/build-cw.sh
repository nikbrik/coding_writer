#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
BIN="${CW_BIN:-$ROOT_DIR/.codingwriter/bin/cw}"
VERSION="${CW_VERSION:-$(tr -d '[:space:]' < "$ROOT_DIR/VERSION")}"
COMMIT="${CW_COMMIT:-$(git -C "$ROOT_DIR" rev-parse --short=12 HEAD 2>/dev/null || printf 'unknown')}"
BUILD_DATE="${CW_BUILD_DATE:-$(date -u +%Y-%m-%dT%H:%M:%SZ)}"
GOCACHE_DIR="${GOCACHE:-/private/tmp/coding_writer_gocache}"

mkdir -p "$(dirname "$BIN")" "$GOCACHE_DIR"
cd "$ROOT_DIR"

GOCACHE="$GOCACHE_DIR" go build \
  -ldflags "-X github.com/nikbrik/coding_writer/internal/cli.Version=$VERSION -X github.com/nikbrik/coding_writer/internal/cli.Commit=$COMMIT -X github.com/nikbrik/coding_writer/internal/cli.BuildDate=$BUILD_DATE" \
  -o "$BIN" ./cmd/cw

"$BIN" --version
