package backend

import "context"

// DeviceTransport is the backend boundary between Wails methods and the device
// implementation. Start begins any background transport work, such as lifxlan-go
// discovery, before the frontend starts polling snapshots.
type DeviceTransport interface {
	Start(ctx context.Context) error
	Close(ctx context.Context) error
	Snapshot(ctx context.Context) (DeviceSnapshot, error)
	SetDeviceState(ctx context.Context, req SetDeviceStateRequest) (Device, error)
}
