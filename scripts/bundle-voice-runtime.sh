#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$ROOT_DIR"

bundle_macos_dylibs() {
  local source_binary="$1"
  local bundled_binary="$2"
  local destination_dir="$3"
  local dependency
  local dependency_name
  local source_dir
  local copied=1
  local inspect_file

  if ! command -v otool >/dev/null 2>&1 || ! command -v install_name_tool >/dev/null 2>&1; then
    echo "warning: otool/install_name_tool not found; skipping macOS dylib relocation" >&2
    return
  fi

  source_dir="$(cd "$(dirname "$source_binary")" && pwd)"

  while [[ "$copied" -eq 1 ]]; do
    copied=0
    for inspect_file in "$bundled_binary" "$destination_dir"/*.dylib; do
      [[ -e "$inspect_file" ]] || continue
      while IFS= read -r dependency; do
        [[ "$dependency" == *.dylib ]] || continue
        [[ "$dependency" == /usr/lib/* || "$dependency" == /System/* || "$dependency" == @loader_path/* ]] && continue
        dependency_name="$(basename "$dependency")"

        if [[ ! -f "$destination_dir/$dependency_name" ]]; then
          if [[ -f "$dependency" ]]; then
            cp "$dependency" "$destination_dir/$dependency_name"
            copied=1
          elif [[ -f "$source_dir/$dependency_name" ]]; then
            cp "$source_dir/$dependency_name" "$destination_dir/$dependency_name"
            copied=1
          else
            echo "warning: could not find whisper dependency $dependency_name from $dependency" >&2
            continue
          fi
        fi

        chmod +w "$destination_dir/$dependency_name" 2>/dev/null || true
        install_name_tool -id "@loader_path/$dependency_name" "$destination_dir/$dependency_name" 2>/dev/null || true
        install_name_tool -change "$dependency" "@loader_path/$dependency_name" "$inspect_file" 2>/dev/null || true
      done < <(otool -L "$inspect_file" | awk 'NR > 1 { print $1 }')
    done
  done
}

WHISPER_COMMAND="${HIKARI_WHISPER_COMMAND:-${WHISPER_COMMAND:-}}"
WHISPER_MODEL="${HIKARI_WHISPER_MODEL:-${WHISPER_MODEL:-}}"
MODEL_NAME="${HIKARI_WHISPER_MODEL_NAME:-ggml-base.en.bin}"

if [[ -z "$WHISPER_COMMAND" || -z "$WHISPER_MODEL" ]]; then
  echo "error: set HIKARI_WHISPER_COMMAND and HIKARI_WHISPER_MODEL before bundling voice runtime" >&2
  exit 1
fi
if [[ ! -x "$WHISPER_COMMAND" ]]; then
  echo "error: whisper command is not executable: $WHISPER_COMMAND" >&2
  exit 1
fi
if [[ ! -f "$WHISPER_MODEL" ]]; then
  echo "error: whisper model not found: $WHISPER_MODEL" >&2
  exit 1
fi

OUTPUT_NAME="whisper-cli"
GOOS_VALUE="${GOOS:-$(go env GOOS)}"
if [[ "$GOOS_VALUE" == "windows" ]]; then
  OUTPUT_NAME="whisper-cli.exe"
fi

STAGING_DIR="$ROOT_DIR/build/bin"
mkdir -p "$STAGING_DIR/models"

MAC_RESOURCES="$ROOT_DIR/build/bin/hikari.app/Contents/Resources"
if [[ -d "$MAC_RESOURCES" ]]; then
  mkdir -p "$MAC_RESOURCES/models"
  RUNTIME_DIR="$MAC_RESOURCES"
  MODEL_DIR="$MAC_RESOURCES/models"
else
  RUNTIME_DIR="$STAGING_DIR"
  MODEL_DIR="$STAGING_DIR/models"
fi

cp "$WHISPER_COMMAND" "$RUNTIME_DIR/$OUTPUT_NAME"
cp "$WHISPER_MODEL" "$MODEL_DIR/$MODEL_NAME"
chmod +x "$RUNTIME_DIR/$OUTPUT_NAME" 2>/dev/null || true

if [[ "$GOOS_VALUE" == "darwin" ]]; then
  bundle_macos_dylibs "$WHISPER_COMMAND" "$RUNTIME_DIR/$OUTPUT_NAME" "$RUNTIME_DIR"
fi

if [[ -d "$MAC_RESOURCES" ]]; then
  echo "Bundled voice runtime into $MAC_RESOURCES"
else
  echo "Bundled voice runtime into $STAGING_DIR"
fi
