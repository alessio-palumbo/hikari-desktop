package main

import (
	"context"
	"errors"
	"testing"

	"hikari-desktop/internal/backend"
)

type diagnosticsTransport struct {
	*recordingTransport
	ctx    context.Context
	serial string
	err    error
}

func (t *diagnosticsTransport) PingDevice(ctx context.Context, serial string) (backend.DevicePingResult, error) {
	t.ctx, t.serial = ctx, serial
	return backend.DevicePingResult{Serial: serial, Samples: 5, Received: 5}, t.err
}

func TestAppPingDevice(t *testing.T) {
	transport := &diagnosticsTransport{recordingTransport: &recordingTransport{}}
	app := NewAppWithTransport(transport)
	result, err := app.PingDevice("serial")
	if err != nil || result.Serial != "serial" || transport.serial != "serial" {
		t.Fatalf("result %#v, error %v", result, err)
	}
	if _, ok := transport.ctx.Deadline(); !ok {
		t.Fatal("request has no timeout")
	}
	transport.err = errors.New("unavailable")
	if _, err := app.PingDevice("serial"); !errors.Is(err, transport.err) {
		t.Fatal(err)
	}
	if _, err := NewAppWithTransport(&recordingTransport{}).PingDevice("serial"); err == nil {
		t.Fatal("unsupported transport accepted")
	}
}
