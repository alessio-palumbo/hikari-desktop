package backend

import "context"

// DeviceTransport is the backend boundary between Wails methods and the device
// implementation. The app uses MockTransport for now; LifxTransport will later
// adapt lifxlan-go without changing the frontend API.
type DeviceTransport interface {
	Snapshot(ctx context.Context) (DeviceSnapshot, error)
	SetDeviceState(ctx context.Context, req SetDeviceStateRequest) (Device, error)
}
