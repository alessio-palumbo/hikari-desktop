package main

import (
	"context"
	"testing"

	"hikari-desktop/internal/backend"
)

type previewTransport struct {
	*recordingTransport
	request backend.StartDeviceEffectRequest
	ctx     context.Context
}

func (t *previewTransport) PreviewDeviceEffect(ctx context.Context, req backend.StartDeviceEffectRequest) (backend.DeviceEffectPreview, error) {
	t.ctx, t.request = ctx, req
	return backend.DeviceEffectPreview{Width: 8, Height: 8}, nil
}

func TestAppPreviewDeviceEffect(t *testing.T) {
	transport := &previewTransport{recordingTransport: &recordingTransport{}}
	req := backend.StartDeviceEffectRequest{Device: backend.Device{Serial: "test"}, Effect: backend.DeviceEffectSnake, SpeedMS: 4000}
	result, err := NewAppWithTransport(transport).PreviewDeviceEffect(req)
	if err != nil || result.Width != 8 || transport.request.Device.Serial != req.Device.Serial || transport.request.Effect != req.Effect || transport.request.SpeedMS != req.SpeedMS {
		t.Fatalf("result %#v, error %v", result, err)
	}
	if _, ok := transport.ctx.Deadline(); !ok {
		t.Fatal("preview has no deadline")
	}
	if _, err := NewAppWithTransport(&recordingTransport{}).PreviewDeviceEffect(req); err == nil {
		t.Fatal("unsupported transport accepted")
	}
}
