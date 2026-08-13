#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$ROOT_DIR"

SIDECAR_PACKAGE="github.com/alessio-palumbo/lifx-command-engine/cmd/lifx-command-engine"
OUTPUT_NAME="lifx-command-engine"
if [[ "${GOOS:-$(go env GOOS)}" == "windows" ]]; then
  OUTPUT_NAME="lifx-command-engine.exe"
fi

STAGING_DIR="$ROOT_DIR/build/bin"
mkdir -p "$STAGING_DIR"

echo "Building $SIDECAR_PACKAGE"
go build -o "$STAGING_DIR/$OUTPUT_NAME" "$SIDECAR_PACKAGE"

MAC_RESOURCES="$ROOT_DIR/build/bin/hikari.app/Contents/Resources"
if [[ -d "$MAC_RESOURCES" ]]; then
  mkdir -p "$MAC_RESOURCES"
  cp "$STAGING_DIR/$OUTPUT_NAME" "$MAC_RESOURCES/$OUTPUT_NAME"
  chmod +x "$MAC_RESOURCES/$OUTPUT_NAME"
  echo "Bundled command sidecar into $MAC_RESOURCES/$OUTPUT_NAME"
else
  chmod +x "$STAGING_DIR/$OUTPUT_NAME" 2>/dev/null || true
  echo "Bundled command sidecar into $STAGING_DIR/$OUTPUT_NAME"
fi
