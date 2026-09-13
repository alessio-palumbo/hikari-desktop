package backend

import "context"

// DeviceTransport is the backend boundary between Wails methods and the device
// implementation. Start begins background transport work, such as lifxlan-go
// discovery and observed-state subscriptions, before snapshots are requested.
type DeviceTransport interface {
	Start(ctx context.Context) error
	Close(ctx context.Context) error
	Snapshot(ctx context.Context) (DeviceSnapshot, error)
	NetworkSettings(ctx context.Context) (NetworkSettings, error)
	SetNetworkInterface(ctx context.Context, req SetNetworkInterfaceRequest) (NetworkSettings, error)
	RestartDeviceDiscovery(ctx context.Context) (NetworkSettings, error)
	SetDeviceState(ctx context.Context, req SetDeviceStateRequest) (Device, error)
	SetDeviceMetadata(ctx context.Context, req SetDeviceMetadataRequest) (Device, error)
	StartDeviceEffect(ctx context.Context, req StartDeviceEffectRequest) (DeviceEffectStatus, error)
	StopDeviceEffect(ctx context.Context, req StopDeviceEffectRequest) (DeviceEffectStatus, error)
}
