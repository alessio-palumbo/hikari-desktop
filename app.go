package main

import (
	"context"
	"log"
	"os"
	"strings"

	"hikari-desktop/internal/backend"
)

type App struct {
	ctx           context.Context
	transport     backend.DeviceTransport
	commandEngine *backend.CommandEngineService
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
	return &App{transport: transport, commandEngine: backend.NewCommandEngineService()}
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
	if a.commandEngine != nil {
		if err := a.commandEngine.Close(ctx); err != nil {
			log.Printf("hikari: command engine shutdown failed: %v", err)
		}
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

func (a *App) NetworkSettings() (backend.NetworkSettings, error) {
	return a.transport.NetworkSettings(a.context())
}

func (a *App) SetNetworkInterface(req backend.SetNetworkInterfaceRequest) (backend.NetworkSettings, error) {
	return a.transport.SetNetworkInterface(a.context(), req)
}

func (a *App) RestartDeviceDiscovery() (backend.NetworkSettings, error) {
	return a.transport.RestartDeviceDiscovery(a.context())
}

func (a *App) SetDeviceState(req backend.SetDeviceStateRequest) (backend.Device, error) {
	return a.transport.SetDeviceState(a.context(), req)
}

func (a *App) StartDeviceEffect(req backend.StartDeviceEffectRequest) (backend.DeviceEffectStatus, error) {
	return a.transport.StartDeviceEffect(a.context(), req)
}

func (a *App) StopDeviceEffect(req backend.StopDeviceEffectRequest) (backend.DeviceEffectStatus, error) {
	return a.transport.StopDeviceEffect(a.context(), req)
}

func (a *App) CommandEngineSettings() (backend.CommandEngineSettings, error) {
	return a.commandEngine.Settings(a.context())
}

func (a *App) SetCommandEngineSettings(req backend.SetCommandEngineSettingsRequest) (backend.CommandEngineSettings, error) {
	return a.commandEngine.SetSettings(a.context(), req)
}

func (a *App) InterpretCommand(req backend.InterpretCommandRequest) (backend.CommandPreview, error) {
	snapshot, err := a.transport.Snapshot(a.context())
	if err != nil {
		return backend.CommandPreview{}, err
	}
	return a.commandEngine.Interpret(a.context(), req.Text, snapshot)
}

func (a *App) TranscribeCommand(req backend.TranscribeCommandRequest) (backend.SpeechCommandPreview, error) {
	snapshot, err := a.transport.Snapshot(a.context())
	if err != nil {
		return backend.SpeechCommandPreview{}, err
	}
	return a.commandEngine.TranscribeAndInterpret(a.context(), req, snapshot)
}
