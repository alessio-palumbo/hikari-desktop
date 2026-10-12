package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"os"
	"strings"
	"sync"
	"time"

	"hikari-desktop/internal/backend"

	wailsruntime "github.com/wailsapp/wails/v2/pkg/runtime"
)

const (
	deviceSnapshotEvent = "hikari:devices:snapshot"
	sensorSnapshotEvent = "hikari:sensors:snapshot"
	setupProgressEvent  = "hikari:setup:progress"
)

type App struct {
	ctx            context.Context
	transport      backend.DeviceTransport
	sensors        sensorProvider
	commandEngine  *backend.CommandEngineService
	floorPlans     backend.FloorPlanStore
	effectSettings backend.EffectSettingsStore
	themes         backend.ThemeStore
	scenes         *backend.SceneService
	onboarding     onboardingProvider
	controlGate    sync.RWMutex
	settingUp      bool
	closing        bool
	setupMu        sync.Mutex
	setupAttempt   string
	setupCancel    context.CancelFunc
	cancelledSetup string
	emitEvent      func(context.Context, string, ...interface{})
}

type onboardingProvider interface {
	Capabilities() backend.OnboardingCapabilities
	Discover(context.Context, bool) (backend.SetupLightDiscovery, error)
	CancelScan()
	Close(context.Context) error
	Provision(context.Context, backend.SetupLightRequest, backend.SetupVerifier, func(backend.SetupProgress)) (backend.SetupLightResult, error)
}

type sensorProvider interface {
	Start(context.Context) error
	Close(context.Context) error
	Snapshot(context.Context) (backend.SensorSnapshot, error)
}

type sensorSnapshotObserver interface {
	SetSnapshotObserver(func(backend.SensorSnapshot))
}

type deviceSnapshotObserver interface {
	SetSnapshotObserver(func(backend.DeviceSnapshot))
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
		transport:      transport,
		sensors:        sensors,
		commandEngine:  backend.NewCommandEngineService(),
		floorPlans:     backend.NewFloorPlanStore(),
		effectSettings: backend.NewEffectSettingsStore(),
		themes:         backend.NewThemeStore(),
		scenes:         backend.NewSceneService(transport, backend.NewSceneStore()),
		onboarding:     backend.NewOnboardingService(),
		emitEvent:      wailsruntime.EventsEmit,
	}
}

func (a *App) startup(ctx context.Context) {
	a.ctx = ctx
	if observable, ok := a.transport.(deviceSnapshotObserver); ok {
		observable.SetSnapshotObserver(func(snapshot backend.DeviceSnapshot) {
			a.emitEvent(a.context(), deviceSnapshotEvent, snapshot)
		})
	}
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
	a.controlGate.Lock()
	a.closing = true
	a.controlGate.Unlock()
	if a.onboarding != nil {
		if err := a.onboarding.Close(ctx); err != nil {
			log.Printf("hikari: light setup shutdown failed: %v", err)
		}
	}
	if observable, ok := a.transport.(deviceSnapshotObserver); ok {
		observable.SetSnapshotObserver(nil)
	}
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

func (a *App) OnboardingCapabilities() backend.OnboardingCapabilities {
	_, supported := a.transport.(backend.OnboardingNetwork)
	return backend.OnboardingCapabilities{Available: supported && a.onboarding.Capabilities().Available}
}

func (a *App) ScanSetupLights(refresh bool) (backend.SetupLightDiscovery, error) {
	if !a.OnboardingCapabilities().Available {
		return backend.SetupLightDiscovery{}, backend.ErrOnboardingUnavailable
	}
	return a.onboarding.Discover(a.context(), refresh)
}

func (a *App) CancelLightSetup(attemptID string) {
	if attemptID == "" {
		a.onboarding.CancelScan()
		return
	}
	a.setupMu.Lock()
	defer a.setupMu.Unlock()
	if a.setupAttempt == attemptID && a.setupCancel != nil {
		a.setupCancel()
	} else if len(attemptID) <= 64 {
		a.cancelledSetup = attemptID
	}
}

func (a *App) SetupLight(req backend.SetupLightRequest) (backend.SetupLightResult, error) {
	if !a.OnboardingCapabilities().Available {
		return backend.SetupLightResult{}, backend.ErrOnboardingUnavailable
	}
	network, ok := a.transport.(backend.OnboardingNetwork)
	if !ok {
		return backend.SetupLightResult{}, backend.ErrOnboardingUnavailable
	}
	a.setupMu.Lock()
	if a.setupCancel != nil {
		a.setupMu.Unlock()
		return backend.SetupLightResult{}, backend.ErrOnboardingBusy
	}
	if strings.TrimSpace(req.AttemptID) == "" || len(req.AttemptID) > 64 {
		a.setupMu.Unlock()
		return backend.SetupLightResult{}, fmt.Errorf("invalid setup attempt")
	}
	if a.cancelledSetup == req.AttemptID {
		a.cancelledSetup = ""
		a.setupMu.Unlock()
		return backend.SetupLightResult{Cancelled: true, Error: "Setup cancelled."}, nil
	}
	ctx, cancelSetup := context.WithCancel(a.context())
	a.setupAttempt, a.setupCancel = req.AttemptID, cancelSetup
	a.setupMu.Unlock()
	defer func() { cancelSetup(); a.setupMu.Lock(); a.setupAttempt = ""; a.setupCancel = nil; a.setupMu.Unlock() }()
	a.emitEvent(a.context(), setupProgressEvent, backend.SetupProgress{AttemptID: req.AttemptID, Phase: "prepare"})
	a.controlGate.Lock()
	if ctx.Err() != nil {
		a.controlGate.Unlock()
		return backend.SetupLightResult{Cancelled: true, Error: "Setup cancelled."}, nil
	}
	if a.closing {
		a.controlGate.Unlock()
		return backend.SetupLightResult{}, backend.ErrOnboardingClosed
	}
	if a.settingUp {
		a.controlGate.Unlock()
		return backend.SetupLightResult{}, backend.ErrOnboardingBusy
	}
	if err := network.CheckSetupReady(); err != nil {
		a.controlGate.Unlock()
		return backend.SetupLightResult{}, err
	}
	a.settingUp = true
	a.controlGate.Unlock()
	defer func() { a.controlGate.Lock(); a.settingUp = false; a.controlGate.Unlock() }()
	result, err := a.onboarding.Provision(ctx, req, network.SetupVerifier(ctx), func(progress backend.SetupProgress) {
		a.emitEvent(ctx, setupProgressEvent, progress)
	})
	if errors.Is(err, context.Canceled) {
		result.Cancelled = true
		result.Error = "Setup cancelled."
		err = nil
	}
	// Cancellation may have interrupted verification, but the library has still
	// attempted host-network recovery. Refresh even after a failed setup, never
	// while the host is still deliberately on the light's setup network.
	a.controlGate.RLock()
	if err == nil && result.Serial != "" && a.context().Err() == nil && !a.closing {
		rctx, cancel := context.WithTimeout(a.context(), 5*time.Second)
		defer cancel()
		if refreshErr := network.RefreshAfterSetup(rctx); refreshErr != nil {
			result.Warning = "Discovery could not resume. Check your network and refresh discovery."
		}
	}
	a.controlGate.RUnlock()
	return result, err
}

func (a *App) beginControl() (func(), error) {
	a.controlGate.RLock()
	if a.closing {
		a.controlGate.RUnlock()
		return nil, backend.ErrOnboardingClosed
	}
	if a.settingUp {
		a.controlGate.RUnlock()
		return nil, backend.ErrOnboardingBusy
	}
	return a.controlGate.RUnlock, nil
}

func (a *App) context() context.Context {
	if a.ctx == nil {
		return context.Background()
	}
	return a.ctx
}

func (a *App) GetDeviceSnapshot() (backend.DeviceSnapshot, error) {
	unlock, err := a.beginControl()
	if err != nil {
		return backend.DeviceSnapshot{}, err
	}
	defer unlock()
	return a.transport.Snapshot(a.context())
}

func (a *App) PingDevice(serial string) (backend.DevicePingResult, error) {
	unlock, err := a.beginControl()
	if err != nil {
		return backend.DevicePingResult{}, err
	}
	defer unlock()
	diagnostics, ok := a.transport.(backend.DeviceDiagnostics)
	if !ok {
		return backend.DevicePingResult{}, fmt.Errorf("device ping is unavailable")
	}
	ctx, cancel := context.WithTimeout(a.context(), 7*time.Second)
	defer cancel()
	return diagnostics.PingDevice(ctx, serial)
}

func (a *App) PreviewDeviceEffect(req backend.StartDeviceEffectRequest) (backend.DeviceEffectPreview, error) {
	previewer, ok := a.transport.(backend.DeviceEffectPreviewer)
	if !ok {
		return backend.DeviceEffectPreview{}, fmt.Errorf("local effect preview is unavailable")
	}
	ctx, cancel := context.WithTimeout(a.context(), 3*time.Second)
	defer cancel()
	return previewer.PreviewDeviceEffect(ctx, req)
}

func (a *App) PreviewTheme(req backend.ThemeRequest) (backend.ThemePreview, error) {
	previewer, ok := a.transport.(backend.ThemePreviewer)
	if !ok {
		return backend.ThemePreview{}, fmt.Errorf("local theme preview is unavailable")
	}
	ctx, cancel := context.WithTimeout(a.context(), 5*time.Second)
	defer cancel()
	return previewer.PreviewTheme(ctx, req)
}

func (a *App) ApplyTheme(req backend.ThemeRequest) (backend.ThemeApplyResult, error) {
	unlock, err := a.beginControl()
	if err != nil {
		return backend.ThemeApplyResult{}, err
	}
	defer unlock()
	applier, ok := a.transport.(backend.ThemeApplier)
	if !ok {
		return backend.ThemeApplyResult{}, fmt.Errorf("theme application is unavailable")
	}
	ctx, cancel := context.WithTimeout(a.context(), 10*time.Second)
	defer cancel()
	return applier.ApplyTheme(ctx, req)
}

func (a *App) GetUserThemes() ([]backend.UserTheme, error) {
	return a.themes.Load()
}

func (a *App) SaveUserTheme(req backend.SaveUserThemeRequest) (backend.UserTheme, error) {
	return a.themes.Save(req)
}

func (a *App) DeleteUserTheme(id string) error {
	return a.themes.Delete(id)
}

func (a *App) GetScenes() ([]backend.SceneSummary, error) { return a.scenes.List() }

func (a *App) CaptureScene(req backend.CaptureSceneRequest) (backend.SceneSummary, error) {
	unlock, err := a.beginControl()
	if err != nil {
		return backend.SceneSummary{}, err
	}
	defer unlock()
	ctx, cancel := context.WithTimeout(a.context(), 5*time.Second)
	defer cancel()
	return a.scenes.Capture(ctx, req)
}

func (a *App) RenameScene(req backend.RenameSceneRequest) (backend.SceneSummary, error) {
	return a.scenes.Rename(req)
}

func (a *App) EditScene(req backend.EditSceneRequest) (backend.SceneSummary, error) {
	unlock, err := a.beginControl()
	if err != nil {
		return backend.SceneSummary{}, err
	}
	defer unlock()
	ctx, cancel := context.WithTimeout(a.context(), 5*time.Second)
	defer cancel()
	return a.scenes.Edit(ctx, req)
}

func (a *App) DeleteScene(id string) error { return a.scenes.Delete(id) }

func (a *App) RecallScene(id string) (backend.SceneRecallResult, error) {
	unlock, err := a.beginControl()
	if err != nil {
		return backend.SceneRecallResult{}, err
	}
	defer unlock()
	ctx, cancel := context.WithTimeout(a.context(), 10*time.Second)
	defer cancel()
	return a.scenes.Recall(ctx, id)
}

func (a *App) DeviceEffectParameters(serial string, effect backend.DeviceEffect) ([]backend.EffectParameter, error) {
	if source, ok := a.transport.(backend.DeviceEffectParameterSource); ok {
		return source.EffectParameters(serial, effect)
	}
	return backend.EffectParameterDefinitions(effect)
}

func (a *App) GetDeviceEffectPreferences(serial string) (map[backend.DeviceEffect]backend.EffectPreference, error) {
	preferences, err := a.effectSettings.Load(serial)
	if err != nil {
		return nil, err
	}
	for effect, preference := range preferences {
		if len(preference.Params) == 0 {
			continue
		}
		parameters, err := a.DeviceEffectParameters(serial, effect)
		if err != nil {
			delete(preferences, effect)
			continue
		}
		preferences[effect] = backend.NormalizeEffectPreference(preference, parameters)
	}
	return preferences, nil
}

func (a *App) SaveDeviceEffectPreference(req backend.SaveDeviceEffectPreferenceRequest) error {
	return a.effectSettings.Save(req)
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
	unlock, err := a.beginControl()
	if err != nil {
		return backend.NetworkSettings{}, err
	}
	defer unlock()
	return a.transport.SetNetworkInterface(a.context(), req)
}

func (a *App) RestartDeviceDiscovery() (backend.NetworkSettings, error) {
	unlock, err := a.beginControl()
	if err != nil {
		return backend.NetworkSettings{}, err
	}
	defer unlock()
	return a.transport.RestartDeviceDiscovery(a.context())
}

func (a *App) SetDeviceState(req backend.SetDeviceStateRequest) (backend.Device, error) {
	unlock, err := a.beginControl()
	if err != nil {
		return backend.Device{}, err
	}
	defer unlock()
	return a.transport.SetDeviceState(a.context(), req)
}

func (a *App) SetLightPartState(req backend.SetLightPartStateRequest) error {
	unlock, err := a.beginControl()
	if err != nil {
		return err
	}
	defer unlock()
	transport, ok := a.transport.(interface {
		SetLightPartState(context.Context, backend.SetLightPartStateRequest) error
	})
	if !ok {
		return fmt.Errorf("light part controls are unavailable")
	}
	ctx, cancel := context.WithTimeout(a.context(), 5*time.Second)
	defer cancel()
	return transport.SetLightPartState(ctx, req)
}

func (a *App) SetDeviceMetadata(req backend.SetDeviceMetadataRequest) (backend.Device, error) {
	unlock, err := a.beginControl()
	if err != nil {
		return backend.Device{}, err
	}
	defer unlock()
	return a.transport.SetDeviceMetadata(a.context(), req)
}

func (a *App) StartDeviceEffect(req backend.StartDeviceEffectRequest) (backend.DeviceEffectStatus, error) {
	unlock, err := a.beginControl()
	if err != nil {
		return backend.DeviceEffectStatus{}, err
	}
	defer unlock()
	return a.transport.StartDeviceEffect(a.context(), req)
}

func (a *App) StopDeviceEffect(req backend.StopDeviceEffectRequest) (backend.DeviceEffectStatus, error) {
	unlock, err := a.beginControl()
	if err != nil {
		return backend.DeviceEffectStatus{}, err
	}
	defer unlock()
	return a.transport.StopDeviceEffect(a.context(), req)
}

func (a *App) CommandEngineSettings() (backend.CommandEngineSettings, error) {
	return a.commandEngine.Settings(a.context())
}

func (a *App) SetCommandEngineSettings(req backend.SetCommandEngineSettingsRequest) (backend.CommandEngineSettings, error) {
	return a.commandEngine.SetSettings(a.context(), req)
}

func (a *App) InterpretCommand(req backend.InterpretCommandRequest) (backend.CommandPreview, error) {
	unlock, err := a.beginControl()
	if err != nil {
		return backend.CommandPreview{}, err
	}
	defer unlock()
	snapshot, err := a.transport.Snapshot(a.context())
	if err != nil {
		return backend.CommandPreview{}, err
	}
	return a.commandEngine.Interpret(a.context(), req.Text, snapshot)
}

func (a *App) TranscribeCommand(req backend.TranscribeCommandRequest) (backend.SpeechCommandPreview, error) {
	unlock, err := a.beginControl()
	if err != nil {
		return backend.SpeechCommandPreview{}, err
	}
	defer unlock()
	snapshot, err := a.transport.Snapshot(a.context())
	if err != nil {
		return backend.SpeechCommandPreview{}, err
	}
	return a.commandEngine.TranscribeAndInterpret(a.context(), req, snapshot)
}

func (a *App) TranscribeCommandAudio(req backend.TranscribeCommandAudioRequest) (backend.SpeechCommandPreview, error) {
	unlock, err := a.beginControl()
	if err != nil {
		return backend.SpeechCommandPreview{}, err
	}
	defer unlock()
	snapshot, err := a.transport.Snapshot(a.context())
	if err != nil {
		return backend.SpeechCommandPreview{}, err
	}
	return a.commandEngine.TranscribeAudioAndInterpret(a.context(), req, snapshot)
}
