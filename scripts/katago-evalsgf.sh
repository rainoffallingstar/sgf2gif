#!/usr/bin/env bash

set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
KATAGO_BIN="${KATAGO_BIN:-$ROOT_DIR/katago/bin/katago}"
KATAGO_MODEL="${KATAGO_MODEL:-$ROOT_DIR/katago/models/default_model.bin.gz}"
KATAGO_GTP_CONFIG="${KATAGO_GTP_CONFIG:-$ROOT_DIR/katago/configs/gtp_example.cfg}"
KATAGO_VISITS="${KATAGO_VISITS:-200}"
KATAGO_THREADS="${KATAGO_THREADS:-4}"

if [[ $# -lt 2 ]]; then
  echo "usage: $0 <sgf-file> <move-num> [extra katago evalsgf args...]" >&2
  exit 1
fi

SGF_FILE="$1"
MOVE_NUM="$2"
shift 2

exec "$KATAGO_BIN" evalsgf \
  -config "$KATAGO_GTP_CONFIG" \
  -model "$KATAGO_MODEL" \
  -m "$MOVE_NUM" \
  -v "$KATAGO_VISITS" \
  -t "$KATAGO_THREADS" \
  -print-json \
  "$@" \
  "$SGF_FILE"
