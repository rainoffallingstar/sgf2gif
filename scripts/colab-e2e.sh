#!/usr/bin/env bash

set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"

if ! command -v go >/dev/null 2>&1 && [[ -x /usr/local/go/bin/go ]]; then
  export PATH="/usr/local/go/bin:$PATH"
fi

ensure_writable_dir() {
  local dir="$1"
  mkdir -p "$dir" >/dev/null 2>&1 || return 1
  local probe="$dir/.sgf2gif_write_probe"
  ( : >"$probe" ) >/dev/null 2>&1 || return 1
  rm -f "$probe" >/dev/null 2>&1 || true
  return 0
}

OUT_DIR="${OUT_DIR:-}"
if [[ -z "$OUT_DIR" ]]; then
  if [[ -d /content ]]; then
    OUT_DIR="/content"
  else
    OUT_DIR="/tmp/sgf2gif-e2e"
  fi
fi

SKIP_REMOTE="${SKIP_REMOTE:-0}"
SKIP_KATAGO="${SKIP_KATAGO:-0}"

FOX_URL_DEFAULT="https://www.foxwq.com/qipu/newlist/id/2026031862241631.html"
FOX_URL="${FOX_URL:-$FOX_URL_DEFAULT}"

E2E_SGF_DEFAULT="$ROOT_DIR/testdata/85130272-301-yrc21-rainoffallingstar1234.sgf"
E2E_SGF="${E2E_SGF:-$E2E_SGF_DEFAULT}"

bin="/tmp/sgf2gif-colab-e2e"

step() {
  printf '\n== %s ==\n' "$1"
}

require_file_nonempty() {
  local path="$1"
  if [[ ! -s "$path" ]]; then
    echo "[fail] expected non-empty file: $path" >&2
    exit 1
  fi
  echo "[ok] $path"
}

mkdir -p "$OUT_DIR"

cur_gomodcache="$(go env GOMODCACHE)"
cur_gocache="$(go env GOCACHE)"
if ! ensure_writable_dir "$cur_gomodcache" || ! ensure_writable_dir "$cur_gocache"; then
  GO_CACHE_ROOT="${GO_CACHE_ROOT:-/tmp/sgf2gif-go-cache}"
  export GOMODCACHE="$GO_CACHE_ROOT/gomodcache"
  export GOCACHE="$GO_CACHE_ROOT/gocache"
  export GOPATH="${GOPATH:-$GO_CACHE_ROOT/gopath}"
  mkdir -p "$GOMODCACHE" "$GOCACHE" "$GOPATH"
fi

step "Go env"
go version
go env GOPATH GOMODCACHE GOCACHE GOMOD GOOS GOARCH | sed 's/^/  /'

step "Unit tests"
(
  cd "$ROOT_DIR"
  go test ./...
)

step "Build sgf2gif"
(
  cd "$ROOT_DIR"
  CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o "$bin" .
)
"$bin" -h >/dev/null 2>&1 || true

step "Local render (no KataGo)"
local_gif="$OUT_DIR/local.gif"
"$bin" "$E2E_SGF" "$local_gif"
require_file_nonempty "$local_gif"

if [[ "$SKIP_REMOTE" != "1" ]]; then
  step "Remote download + render"
  ogs_sgf="$OUT_DIR/ogs-download.sgf"
  fox_sgf="$OUT_DIR/fox-download.sgf"
  ogs_gif="$OUT_DIR/ogs-remote.gif"

  "$bin" --download-sgf ogs:85130272 "$ogs_sgf"
  "$bin" --download-sgf "$FOX_URL" "$fox_sgf"
  "$bin" "https://online-go.com/game/85130272" "$ogs_gif"

  require_file_nonempty "$ogs_sgf"
  require_file_nonempty "$fox_sgf"
  require_file_nonempty "$ogs_gif"

  echo
  echo "OGS SGF head:"
  sed -n '1,12p' "$ogs_sgf" || true
  echo
  echo "Fox SGF head:"
  sed -n '1,12p' "$fox_sgf" || true
else
  step "Remote download + render (skipped)"
  echo "Set SKIP_REMOTE=0 to enable."
fi

if [[ "$SKIP_KATAGO" != "1" ]]; then
  step "KataGo analysis render (mild) + cache rerender"
  katago_gif="$OUT_DIR/katago-mild.gif"
  "$bin" --katago-strength mild "$E2E_SGF" "$katago_gif"
  require_file_nonempty "$katago_gif"

  cache_sgf="${katago_gif%.*}.katago.sgf"
  require_file_nonempty "$cache_sgf"

  rerender_gif="$OUT_DIR/katago-mild-rerender.gif"
  "$bin" "$cache_sgf" "$rerender_gif"
  require_file_nonempty "$rerender_gif"

  cache_only_gif="$OUT_DIR/katago-cache-only.gif"
  "$bin" --katago-cache-only "$cache_sgf" "$cache_only_gif"
  require_file_nonempty "$cache_only_gif"
else
  step "KataGo analysis render (skipped)"
  echo "Set SKIP_KATAGO=0 to enable."
fi

step "Artifacts"
ls -lh "$OUT_DIR" | sed 's/^/  /'

step "Done"
echo "E2E complete. Output dir: $OUT_DIR"
