#!/usr/bin/env bash

set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
KATAGO_BIN="${KATAGO_BIN:-$ROOT_DIR/katago/bin/katago}"
KATAGO_MODEL="${KATAGO_MODEL:-$ROOT_DIR/katago/models/default_model.bin.gz}"
KATAGO_GTP_CONFIG="${KATAGO_GTP_CONFIG:-$ROOT_DIR/katago/configs/gtp_example.cfg}"
KATAGO_ANALYSIS_CONFIG="${KATAGO_ANALYSIS_CONFIG:-$ROOT_DIR/katago/configs/analysis_example.cfg}"

check_file() {
  local path="$1"
  local label="$2"
  if [[ -e "$path" ]]; then
    printf '[ok] %s: %s\n' "$label" "$path"
  else
    printf '[missing] %s: %s\n' "$label" "$path" >&2
    return 1
  fi
}

check_file "$KATAGO_BIN" "katago binary"
check_file "$KATAGO_MODEL" "model"
check_file "$KATAGO_GTP_CONFIG" "gtp config"
check_file "$KATAGO_ANALYSIS_CONFIG" "analysis config"

"$KATAGO_BIN" version
