# Hikari Desktop

Hikari Desktop is a Wails desktop app for controlling LIFX devices on the local network.

The app is in active development. It currently has a real `lifxlan-go` transport for LAN discovery and direct device control, plus a mock transport for UI development. Scenes, effects, presets, final packaging, signing, and notarization are not implemented yet.

## Current Scope

- Local LAN device discovery through `lifxlan-go`.
- Single-zone power, brightness, color, and white temperature control.
- Multizone and matrix global power, brightness, color, and white temperature control.
- Multizone and matrix draft editing with brush, fill, picker, and gradient tools.
- Matrix custom grids and orientation-aware preview/apply behavior.
- Periodic refresh with pending-state reconciliation to avoid stale device updates fighting recent UI changes.

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
```

Release builds are intended to be produced natively on each platform through GitHub Actions.

## Architecture

- `main.go` and `app.go`: Wails entry point and app binding.
- `internal/backend`: device transport interface, LIFX transport, mock transport, DTOs, and backend tests.
- `frontend/src/domain`: typed frontend device models, draft editor state, and refresh reconciliation.
- `frontend/src/components`: React UI components for the shell, device list, previews, and inspector.
- `frontend/src/styles`: global styles and design tokens.

The frontend calls:

- `GetDeviceSnapshot()`
- `SetDeviceState(req)`

The backend keeps `lifxlan-go` behind the transport boundary so real device behavior can be hardened without coupling the UI directly to LAN implementation details.

## Release

The release workflow builds macOS, Windows, and Linux artifacts from tags matching `v*`.

Current release limitations:

- macOS signing and notarization are not configured.
- Windows signing is not configured.
- Linux packaging is limited to the Wails build output.

