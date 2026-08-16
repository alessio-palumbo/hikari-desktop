#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$ROOT_DIR"

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
if [[ "${GOOS:-$(go env GOOS)}" == "windows" ]]; then
  OUTPUT_NAME="whisper-cli.exe"
fi

STAGING_DIR="$ROOT_DIR/build/bin"
mkdir -p "$STAGING_DIR/models"
cp "$WHISPER_COMMAND" "$STAGING_DIR/$OUTPUT_NAME"
cp "$WHISPER_MODEL" "$STAGING_DIR/models/$MODEL_NAME"
chmod +x "$STAGING_DIR/$OUTPUT_NAME" 2>/dev/null || true

MAC_RESOURCES="$ROOT_DIR/build/bin/hikari.app/Contents/Resources"
if [[ -d "$MAC_RESOURCES" ]]; then
  mkdir -p "$MAC_RESOURCES/models"
  cp "$WHISPER_COMMAND" "$MAC_RESOURCES/$OUTPUT_NAME"
  cp "$WHISPER_MODEL" "$MAC_RESOURCES/models/$MODEL_NAME"
  chmod +x "$MAC_RESOURCES/$OUTPUT_NAME"
  echo "Bundled voice runtime into $MAC_RESOURCES"
else
  echo "Bundled voice runtime into $STAGING_DIR"
fi
