#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/../../.." && pwd)"
SOURCE_FILE="${SOURCE_FILE:-$ROOT_DIR/scripts/icon/appicon.png}"
APP_ICON_OUTPUT_FILE="${APP_ICON_OUTPUT_FILE:-$ROOT_DIR/build/appicon.png}"
FRONTEND_ICON_OUTPUT_FILE="${FRONTEND_ICON_OUTPUT_FILE:-$ROOT_DIR/frontend/src/assets/app-mark.png}"
ICON_SIZE="${ICON_SIZE:-1024}"
RENDER_SCRIPT="$ROOT_DIR/scripts/icon/process/render-svg-icon.swift"

if [[ ! -f "$SOURCE_FILE" ]]; then
  printf 'Missing icon source file: %s\n' "$SOURCE_FILE" >&2
  exit 1
fi

mkdir -p "$(dirname "$APP_ICON_OUTPUT_FILE")" "$(dirname "$FRONTEND_ICON_OUTPUT_FILE")"

sync_frontend_icon() {
  if [[ "$APP_ICON_OUTPUT_FILE" != "$FRONTEND_ICON_OUTPUT_FILE" ]]; then
    cp "$APP_ICON_OUTPUT_FILE" "$FRONTEND_ICON_OUTPUT_FILE"
  fi
}

case "${SOURCE_FILE##*.}" in
  png|PNG)
    if [[ "$SOURCE_FILE" != "$APP_ICON_OUTPUT_FILE" ]]; then
      cp "$SOURCE_FILE" "$APP_ICON_OUTPUT_FILE"
    fi
    sync_frontend_icon
    printf 'Copied %s\n' "$APP_ICON_OUTPUT_FILE"
    exit 0
    ;;
esac

if ! command -v swift >/dev/null 2>&1; then
  printf 'Missing required command: swift\n' >&2
  exit 1
fi

if [[ ! -f "$RENDER_SCRIPT" ]]; then
  printf 'Missing icon render script: %s\n' "$RENDER_SCRIPT" >&2
  exit 1
fi

export CLANG_MODULE_CACHE_PATH="${CLANG_MODULE_CACHE_PATH:-${TMPDIR:-/tmp}/swift-module-cache}"

swift "$RENDER_SCRIPT" "$SOURCE_FILE" "$APP_ICON_OUTPUT_FILE" "$ICON_SIZE"
sync_frontend_icon

printf 'Rendered %s\n' "$APP_ICON_OUTPUT_FILE"
