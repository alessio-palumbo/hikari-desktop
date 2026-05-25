package backend

import "context"

type MockTransport struct {
	snapshot DeviceSnapshot
}

func NewMockTransport() *MockTransport {
	return &MockTransport{snapshot: MockDeviceSnapshot()}
}

func (t *MockTransport) Start(ctx context.Context) error {
	return nil
}

func (t *MockTransport) Snapshot(ctx context.Context) (DeviceSnapshot, error) {
	return t.snapshot, nil
}

func (t *MockTransport) SetDeviceState(ctx context.Context, req SetDeviceStateRequest) (Device, error) {
	// Keep mock transport intentionally simple: the frontend remains the source
	// of truth for optimistic state until the real lifxlan-go transport exists.
	return req.Device, nil
}
