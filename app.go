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

func (a *App) GetDeviceSnapshot() (backend.DeviceSnapshot, error) {
	return a.transport.Snapshot(a.context())
}

func (a *App) SetDeviceState(req backend.SetDeviceStateRequest) (backend.Device, error) {
	return a.transport.SetDeviceState(a.context(), req)
}
