package main

import (
	"context"

	"hikari-desktop/internal/backend"
)

type App struct {
	ctx       context.Context
	transport backend.DeviceTransport
}

func NewApp() *App {
	return NewAppWithTransport(backend.NewLifxTransport())
}

func NewAppWithTransport(transport backend.DeviceTransport) *App {
	if transport == nil {
		transport = backend.NewMockTransport()
	}
	return &App{transport: transport}
}

func (a *App) startup(ctx context.Context) {
	a.ctx = ctx
}

func (a *App) context() context.Context {
	if a.ctx == nil {
		return context.Background()
	}
	return a.ctx
}

func (a *App) GetDeviceSnapshot() backend.DeviceSnapshot {
	snapshot, err := a.transport.Snapshot(a.context())
	if err != nil {
		return backend.DeviceSnapshot{}
	}
	return snapshot
}

func (a *App) SetDeviceState(req backend.SetDeviceStateRequest) backend.Device {
	device, err := a.transport.SetDeviceState(a.context(), req)
	if err != nil {
		return req.Device
	}
	return device
}
