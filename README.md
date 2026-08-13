# 光 (ひかり)

hikari is a Wails desktop app for controlling LIFX devices on the local network.

The app is in active development, but it is ready to try with real LAN devices. It currently has a real `lifxlan-go` transport for LAN discovery and direct device control, plus a mock transport for UI development. Scenes, effects, presets, full installer packaging, signing, and notarization are not implemented yet.

## Current Scope

- Local LAN device discovery through `lifxlan-go`.
- Single-zone power, brightness, color, and white temperature control.
- Multizone and matrix global power, brightness, color, and white temperature control.
- Multizone and matrix draft editing with brush, fill, picker, and gradient tools.
- Matrix custom grids and orientation-aware preview/apply behavior.
- Periodic refresh with pending-state reconciliation to avoid stale device updates fighting recent UI changes.
- Optional local text commands through the rule-only `lifx-command-engine` sidecar.

## Download

Prebuilt macOS, Windows, and Linux artifacts are published on the GitHub Releases page.

To try hikari:

1. Open the latest release.
2. Download the artifact for your platform.
3. Extract the archive.
4. Run `hikari`.

### macOS Quarantine

macOS builds are not notarized yet. After extracting the release archive, macOS may block the app. Remove the quarantine attribute before opening it:

```sh
xattr -dr com.apple.quarantine hikari.app
```

Run the command from the folder containing `hikari.app`, or replace `hikari.app` with the full app path.

## Shortcuts

- `Cmd+F` on macOS or `Ctrl+F` on Windows/Linux: focus and select the search field.
- `S`: focus and select the search field when focus is not in a text field or control.
- `Space`: open the text command prompt when focus is not in a text field or control, or when the focused search field is empty.
- `Esc` in search: clear the search text; when search is empty, blur the field.
- `Esc` in the text command prompt: clear the prompt and preview; when empty, close the prompt.
- `Esc` with the right panel open: close the active device or group panel.

## Local Text Commands

Local text commands use the standalone `lifx-command-engine` JSONL sidecar. Release builds bundle the lightweight rule-only sidecar and enable local commands automatically. The sidecar only interprets text into a structured plan; hikari still validates targets, previews the action, asks for confirmation, and sends any LIFX commands itself.

For development, run `./scripts/bundle-command-sidecar.sh` after `wails build`, put `lifx-command-engine` on `PATH`, or set an explicit path with environment variables:

```sh
HIKARI_COMMANDS_ENABLED=1 \
HIKARI_COMMAND_ENGINE_PATH=/path/to/lifx-command-engine \
wails dev
```

Optional FunctionGemma and whisper.cpp runtime/model paths belong in a `lifx-command-engine` config file, then in hikari set `HIKARI_COMMAND_ENGINE_CONFIG` to that file. The base hikari app does not download or bundle model weights or speech runtimes.

## Requirements

- Go 1.25
- Node.js 22 or newer
- npm
- Wails v2

Install Wails:

```sh
go install github.com/wailsapp/wails/v2/cmd/wails@v2.12.0
```

Linux also needs the native Wails/WebKit dependencies for your distribution.

## Run

Install frontend dependencies:

```sh
cd frontend
npm ci
cd ..
```

Run with real LAN discovery:

```sh
wails dev
```

Run with mock devices:

```sh
HIKARI_TRANSPORT=mock wails dev
```

On Windows PowerShell:

```powershell
$env:HIKARI_TRANSPORT="mock"
wails dev
```

## Test

Run Go tests:

```sh
go test ./...
```

Run frontend tests:

```sh
cd frontend
npm run test
```

Build the frontend:

```sh
cd frontend
npm run build
```

## Build

Build the desktop app for the current platform:

```sh
wails build -clean
./scripts/bundle-command-sidecar.sh
```

The sidecar script builds the pinned `lifx-command-engine` module from `go.mod` and places it next to the app binary, or in `hikari.app/Contents/Resources` on macOS.

Release builds are intended to be produced natively on each platform through GitHub Actions.

## Architecture

- `main.go` and `app.go`: Wails entry point and app binding.
- `internal/backend`: device transport interface, LIFX transport, optional command-engine sidecar service, mock transport, DTOs, and backend tests.
- `frontend/src/domain`: typed frontend device models, draft editor state, and refresh reconciliation.
- `frontend/src/components`: React UI components for the shell, device list, previews, and inspector.
- `frontend/src/styles`: global styles and design tokens.

The frontend calls:

- `GetDeviceSnapshot()`
- `SetDeviceState(req)`
- `CommandEngineSettings()`
- `SetCommandEngineSettings(req)`
- `InterpretCommand(req)`

The backend keeps `lifxlan-go` behind the transport boundary so real device behavior can be hardened without coupling the UI directly to LAN implementation details.

## Release Builds

The release workflow builds macOS, Windows, and Linux artifacts from tags matching `v*`.

Current release limitations:

- macOS signing and notarization are not configured.
- Windows signing is not configured.
- Linux packaging is limited to the Wails build output.
