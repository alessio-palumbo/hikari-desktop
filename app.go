package main

import (
	"context"
	"log"
	"os"
	"strings"

	"hikari-desktop/internal/backend"
)

type App struct {
	ctx       context.Context
	transport backend.DeviceTransport
}

func NewApp() *App {
	if strings.EqualFold(os.Getenv("HIKARI_TRANSPORT"), "mock") {
		log.Print("hikari: using mock device transport")
		return NewAppWithTransport(backend.NewMockTransport())
	}
	log.Print("hikari: using lifx LAN device transport")
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
	if err := a.transport.Start(ctx); err != nil {
		log.Printf("hikari: transport startup failed: %v", err)
	}
}

func (a *App) shutdown(ctx context.Context) {
	if err := a.transport.Close(ctx); err != nil {
		log.Printf("hikari: transport shutdown failed: %v", err)
	}
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

func (a *App) StopDeviceEffect(req backend.StopDeviceEffectRequest) (backend.DeviceEffectStatus, error) {
	return a.transport.StopDeviceEffect(a.context(), req)
}
