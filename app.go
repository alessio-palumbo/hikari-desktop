package main

import (
	"context"
	"log"
	"os"
	"strings"

	"hikari-desktop/internal/backend"

	wailsruntime "github.com/wailsapp/wails/v2/pkg/runtime"
)

const sensorSnapshotEvent = "hikari:sensors:snapshot"

type App struct {
	ctx           context.Context
	transport     backend.DeviceTransport
	sensors       sensorProvider
	commandEngine *backend.CommandEngineService
	floorPlans    backend.FloorPlanStore
	emitEvent     func(context.Context, string, ...interface{})
}

type sensorProvider interface {
	Start(context.Context) error
	Close(context.Context) error
	Snapshot(context.Context) (backend.SensorSnapshot, error)
}

type sensorSnapshotObserver interface {
	SetSnapshotObserver(func(backend.SensorSnapshot))
}

func NewApp() *App {
	var transport backend.DeviceTransport
	if strings.EqualFold(os.Getenv("HIKARI_TRANSPORT"), "mock") {
		log.Print("hikari: using mock device transport")
		transport = backend.NewMockTransport()
	} else {
		log.Print("hikari: using lifx LAN device transport")
		transport = backend.NewLifxTransport()
	}
	return newAppWithServices(transport, backend.NewSensorService())
}

func NewAppWithTransport(transport backend.DeviceTransport) *App {
	return newAppWithServices(transport, nil)
}

func newAppWithServices(transport backend.DeviceTransport, sensors sensorProvider) *App {
	if transport == nil {
		transport = backend.NewMockTransport()
	}
	return &App{
		transport:     transport,
		sensors:       sensors,
		commandEngine: backend.NewCommandEngineService(),
		floorPlans:    backend.NewFloorPlanStore(),
		emitEvent:     wailsruntime.EventsEmit,
	}
}

func (a *App) startup(ctx context.Context) {
	a.ctx = ctx
	if err := a.transport.Start(ctx); err != nil {
		log.Printf("hikari: transport startup failed: %v", err)
	}
	if a.sensors != nil {
		if observable, ok := a.sensors.(sensorSnapshotObserver); ok {
			observable.SetSnapshotObserver(func(snapshot backend.SensorSnapshot) {
				a.emitEvent(a.context(), sensorSnapshotEvent, snapshot)
			})
		}
		if err := a.sensors.Start(ctx); err != nil {
			log.Printf("hikari: sensor discovery startup failed: %v", err)
		}
	}
}

func (a *App) shutdown(ctx context.Context) {
	if err := a.transport.Close(ctx); err != nil {
		log.Printf("hikari: transport shutdown failed: %v", err)
	}
	if a.sensors != nil {
		if observable, ok := a.sensors.(sensorSnapshotObserver); ok {
			observable.SetSnapshotObserver(nil)
		}
		if err := a.sensors.Close(ctx); err != nil {
			log.Printf("hikari: sensor discovery shutdown failed: %v", err)
		}
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

func (a *App) GetSensorSnapshot() (backend.SensorSnapshot, error) {
	if a.sensors == nil {
		return backend.SensorSnapshot{Nodes: []backend.SensorNode{}}, nil
	}
	return a.sensors.Snapshot(a.context())
}

func (a *App) GetFloorPlanPreferences() (backend.FloorPlanPreferencesDocument, error) {
	return a.floorPlans.Load()
}

func (a *App) SaveFloorPlanPreferences(req backend.SaveFloorPlanPreferencesRequest) error {
	return a.floorPlans.Save(req.Data)
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

func (a *App) TranscribeCommandAudio(req backend.TranscribeCommandAudioRequest) (backend.SpeechCommandPreview, error) {
	snapshot, err := a.transport.Snapshot(a.context())
	if err != nil {
		return backend.SpeechCommandPreview{}, err
	}
	return a.commandEngine.TranscribeAudioAndInterpret(a.context(), req, snapshot)
}
