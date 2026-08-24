package backend

import (
	"context"
	"errors"
	"fmt"
	"log"
	"math"
	"net"
	"os"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	lifxclient "github.com/alessio-palumbo/lifxlan-go/pkg/client"
	lifxcontroller "github.com/alessio-palumbo/lifxlan-go/pkg/controller"
	lifxdevice "github.com/alessio-palumbo/lifxlan-go/pkg/device"
	lifxeffects "github.com/alessio-palumbo/lifxlan-go/pkg/effects"
	lifxeffectadapters "github.com/alessio-palumbo/lifxlan-go/pkg/effects/adapters"
	"github.com/alessio-palumbo/lifxlan-go/pkg/messages"
	"github.com/alessio-palumbo/lifxlan-go/pkg/protocol"
	"github.com/alessio-palumbo/lifxprotocol-go/gen/protocol/packets"
)

const (
	defaultColorTransitionDuration  = 300 * time.Millisecond
	defaultFirmwareEffectSpeed      = 5 * time.Second
	defaultAppEffectStep            = 180 * time.Millisecond
	minAppEffectStep                = 100 * time.Millisecond
	maxAppEffectStep                = 500 * time.Millisecond
	effectPowerOffSettleDelay       = 250 * time.Millisecond
	defaultAppEffectStopTimeout     = 750 * time.Millisecond
	defaultAppEffectTailSize        = 5
	matrixEffectPaletteMaxColors    = 16
	matrixEffectPaletteHueBuckets   = 16
	matrixEffectPaletteMinLightness = 0.01
)

// lifxController is the subset of lifxlan-go's controller.Controller used by
// the transport. Keeping this tiny interface makes mapping and sends testable
// without starting network discovery in unit tests.
type lifxController interface {
	Close() error
	GetDevices() []lifxdevice.Device
	Send(lifxdevice.Serial, *protocol.Message) error
}

type lifxControllerFactory func(*lifxclient.Config) (lifxController, error)
type broadcastInterfaceLister func() ([]lifxclient.BroadcastInterface, error)

// LifxTransport adapts lifxlan-go behind DeviceTransport.
//
// Snapshot maps controller.GetDevices() into the frontend DTO. SetDeviceState
// sends power and color state for single-zone, multizone, and matrix devices.
// Matrix colors are rotated into UI orientation for previews and rotated back
// to device order when applying edited pixels.
//
// Start creates the controller and begins lifxlan-go discovery. Tests can inject
// a fake controller directly with NewLifxTransportWithController.
type LifxTransport struct {
	controller            lifxController
	controllerFactory     lifxControllerFactory
	interfaceLister       broadcastInterfaceLister
	settingsStore         networkSettingsStore
	mu                    sync.RWMutex
	cache                 map[string]Device
	effects               map[string]runningAppEffect
	firmware              map[string]runningFirmwareEffect
	restores              map[string]effectRestore
	selectedInterfaceName string
	networkWarning        string
}

type runningAppEffect struct {
	effect    DeviceEffect
	speedMS   int
	direction string
	cancel    context.CancelFunc
	done      <-chan struct{}
	previous  Device
}

type runningFirmwareEffect struct {
	effect DeviceEffect
	wasOff bool
}

type effectRestore struct {
	device Device
	until  time.Time
}

func NewLifxTransport() *LifxTransport {
	return newLifxTransport(nil, nil, defaultNetworkSettingsStore())
}

func NewLifxTransportWithController(controller lifxController) *LifxTransport {
	transport := newLifxTransport(nil, injectedControllerInterfaceLister, &memoryNetworkSettingsStore{})
	transport.controller = controller
	return transport
}

func newLifxTransport(factory lifxControllerFactory, lister broadcastInterfaceLister, store networkSettingsStore) *LifxTransport {
	if factory == nil {
		factory = defaultLifxControllerFactory
	}
	if lister == nil {
		lister = lifxclient.BroadcastInterfaces
	}
	if store == nil {
		store = &memoryNetworkSettingsStore{}
	}
	return &LifxTransport{
		controllerFactory: factory,
		interfaceLister:   lister,
		settingsStore:     store,
		cache:             make(map[string]Device),
		effects:           make(map[string]runningAppEffect),
		firmware:          make(map[string]runningFirmwareEffect),
		restores:          make(map[string]effectRestore),
	}
}

func injectedControllerInterfaceLister() ([]lifxclient.BroadcastInterface, error) {
	return []lifxclient.BroadcastInterface{{Name: "test", IP: net.IPv4(127, 0, 0, 1), Broadcast: net.IPv4(127, 255, 255, 255)}}, nil
}

func defaultLifxControllerFactory(cfg *lifxclient.Config) (lifxController, error) {
	options := []lifxcontroller.Option{
		lifxcontroller.WithHFStateRefreshPeriod(2 * time.Second),
		lifxcontroller.WithLFStateRefreshPeriod(time.Minute),
		lifxcontroller.WithPreflightHandshakeTimeout(10 * time.Second),
	}
	if cfg != nil {
		options = append(options, lifxcontroller.WithClientConfig(cfg))
	}
	return lifxcontroller.New(options...)
}

func (t *LifxTransport) Start(ctx context.Context) error {
	t.mu.RLock()
	started := t.controller != nil
	t.mu.RUnlock()
	if started {
		return nil
	}

	selectedName, warning, err := t.loadValidatedInterfaceName()
	if err != nil {
		return err
	}
	ctrl, err := t.createController(selectedName)
	if err != nil {
		return err
	}

	t.mu.Lock()
	t.controller = ctrl
	t.selectedInterfaceName = selectedName
	t.networkWarning = warning
	t.mu.Unlock()
	log.Print("hikari: lifx controller created")
	return nil
}

func (t *LifxTransport) Close(ctx context.Context) error {
	ctrl := t.currentController()
	if ctrl == nil {
		return nil
	}
	if err := t.restoreAllAppEffects(ctx, ctrl); err != nil {
		log.Printf("hikari: restore app effects on close failed: %v", err)
	}
	if err := ctrl.Close(); err != nil {
		return fmt.Errorf("close lifx controller: %w", err)
	}
	t.mu.Lock()
	if t.controller == ctrl {
		t.controller = nil
	}
	t.clearRuntimeStateLocked()
	t.mu.Unlock()
	log.Print("hikari: lifx controller closed")
	return nil
}

func (t *LifxTransport) NetworkSettings(ctx context.Context) (NetworkSettings, error) {
	return t.networkSettings()
}

func (t *LifxTransport) SetNetworkInterface(ctx context.Context, req SetNetworkInterfaceRequest) (NetworkSettings, error) {
	name := strings.TrimSpace(req.InterfaceName)
	settings, err := t.networkSettingsForSelection(name)
	if err != nil {
		return settings, err
	}

	t.mu.RLock()
	currentName := t.selectedInterfaceName
	wasStarted := t.controller != nil
	t.mu.RUnlock()
	if currentName == "" && !wasStarted {
		loadedName, loadErr := t.settingsStore.LoadNetworkInterfaceName()
		if loadErr != nil {
			return settings, loadErr
		}
		currentName = loadedName
	}

	if currentName == name {
		return settings, nil
	}
	if err := t.settingsStore.SaveNetworkInterfaceName(name); err != nil {
		return settings, err
	}
	if !wasStarted {
		t.mu.Lock()
		t.selectedInterfaceName = name
		t.networkWarning = settings.Warning
		t.mu.Unlock()
		return settings, nil
	}
	if err := t.restartController(ctx, name, settings.Warning); err != nil {
		return settings, err
	}
	return settings, nil
}

func (t *LifxTransport) RestartDeviceDiscovery(ctx context.Context) (NetworkSettings, error) {
	selectedName, warning, err := t.loadValidatedInterfaceName()
	if err != nil {
		return NetworkSettings{SelectedInterfaceName: "", Interfaces: []NetworkInterface{}, Warning: userFriendlyNetworkError(err)}, err
	}
	if err := t.restartController(ctx, selectedName, warning); err != nil {
		settings, settingsErr := t.networkSettings()
		if settingsErr != nil {
			return NetworkSettings{SelectedInterfaceName: selectedName, Interfaces: []NetworkInterface{}, Warning: userFriendlyNetworkError(err)}, err
		}
		settings.Warning = userFriendlyNetworkError(err)
		return settings, err
	}
	return t.networkSettings()
}

func (t *LifxTransport) Snapshot(ctx context.Context) (DeviceSnapshot, error) {
	ctrl, err := t.requireController()
	if err != nil {
		return DeviceSnapshot{}, err
	}
	if err := t.ensureNetworkInterfaceAvailable(); err != nil {
		return DeviceSnapshot{}, err
	}
	devices := ctrl.GetDevices()
	log.Printf("hikari: lifx snapshot read %d devices", len(devices))
	snapshot := mapLifxDevices(devices)
	t.reconcileRestoreSnapshot(&snapshot, time.Now())
	t.replaceCache(snapshot.Devices)
	return snapshot, nil
}

func (t *LifxTransport) SetDeviceState(ctx context.Context, req SetDeviceStateRequest) (Device, error) {
	ctrl, err := t.requireController()
	if err != nil {
		return req.Device, err
	}
	serial, err := parseDeviceSerial(req.Device)
	if err != nil {
		return req.Device, err
	}

	current := t.cachedDevice(req.Device.Serial)
	if current == nil {
		snapshot := mapLifxDevices(ctrl.GetDevices())
		t.replaceCache(snapshot.Devices)
		current = t.cachedDevice(req.Device.Serial)
	}

	intent := normalizeDeviceCommandIntent(req.Intent, req.Device)
	restart, wasRunning := t.stopAppEffectForStateChange(req.Device.Serial)
	if err := sendDeviceState(ctx, ctrl, serial, req.Device, req.Preview, intent, current); err != nil {
		log.Printf("hikari: set device state failed for %s: %v", req.Device.Serial, err)
		return req.Device, err
	}
	t.storeCachedDevice(req.Device)
	if wasRunning {
		restartReq := StartDeviceEffectRequest{
			Device:    req.Device,
			Effect:    restart.effect,
			SpeedMS:   restart.speedMS,
			Direction: restart.direction,
		}
		if _, err := t.startAppDeviceEffect(ctx, ctrl, serial, restartReq); err != nil {
			log.Printf("hikari: restart app effect failed for %s: %v", req.Device.Serial, err)
			return req.Device, fmt.Errorf("restart app effect: %w", err)
		}
	}
	return req.Device, nil
}

func (t *LifxTransport) StartDeviceEffect(ctx context.Context, req StartDeviceEffectRequest) (DeviceEffectStatus, error) {
	ctrl, err := t.requireController()
	if err != nil {
		return DeviceEffectStatus{Serial: req.Device.Serial, Running: false, Effect: string(req.Effect), Error: err.Error()}, err
	}
	serial, err := parseDeviceSerial(req.Device)
	if err != nil {
		return DeviceEffectStatus{Serial: req.Device.Serial, Running: false, Effect: string(req.Effect), Error: err.Error()}, err
	}
	req.Device = t.effectRequestDevice(req.Device)
	if isAppEffect(req.Effect) {
		return t.startAppDeviceEffect(ctx, ctrl, serial, req)
	}
	msg, effect, err := startDeviceEffectMessage(req)
	if err != nil {
		return DeviceEffectStatus{Serial: req.Device.Serial, Running: false, Effect: string(req.Effect), Error: err.Error()}, err
	}
	if err := ctx.Err(); err != nil {
		return DeviceEffectStatus{Serial: req.Device.Serial, Running: false, Effect: string(effect), Error: err.Error()}, err
	}
	appPrevious := t.stopAppEffect(req.Device.Serial)
	if appPrevious != nil {
		if err := restoreDeviceState(ctx, ctrl, serial, *appPrevious); err != nil {
			return DeviceEffectStatus{Serial: req.Device.Serial, Running: false, Effect: string(effect), Error: err.Error()}, fmt.Errorf("restore app effect state: %w", err)
		}
		t.storeCachedDevice(*appPrevious)
		t.storeRestoreDevice(*appPrevious, time.Now().Add(3*time.Second))
	}
	wasOff := t.captureFirmwareEffectWasOff(req.Device.Serial, appPrevious, req.Device)
	logLifxSend(serial, req.Device, "effect-start", msg)
	if err := ctrl.Send(serial, msg); err != nil {
		return DeviceEffectStatus{Serial: req.Device.Serial, Running: false, Effect: string(effect), Error: err.Error()}, fmt.Errorf("start device effect: %w", err)
	}
	if wasOff {
		powerMsg := messages.SetPowerOn()
		logLifxSend(serial, req.Device, "effect-power-on", powerMsg)
		if err := ctrl.Send(serial, powerMsg); err != nil {
			return DeviceEffectStatus{Serial: req.Device.Serial, Running: false, Effect: string(effect), Error: err.Error()}, fmt.Errorf("set power on for effect: %w", err)
		}
		active := req.Device
		active.On = true
		t.storeCachedDevice(active)
	}
	t.storeFirmwareEffect(req.Device.Serial, runningFirmwareEffect{effect: effect, wasOff: wasOff})
	return DeviceEffectStatus{Serial: req.Device.Serial, Running: true, Effect: string(effect)}, nil
}

func (t *LifxTransport) StopDeviceEffect(ctx context.Context, req StopDeviceEffectRequest) (DeviceEffectStatus, error) {
	ctrl, err := t.requireController()
	if err != nil {
		return DeviceEffectStatus{Serial: req.Device.Serial, Running: true, Error: err.Error()}, err
	}
	serial, err := parseDeviceSerial(req.Device)
	if err != nil {
		return DeviceEffectStatus{Serial: req.Device.Serial, Running: true, Error: err.Error()}, err
	}
	previous := t.stopAppEffect(req.Device.Serial)
	if previous != nil {
		if err := restoreDeviceState(ctx, ctrl, serial, *previous); err != nil {
			return DeviceEffectStatus{Serial: req.Device.Serial, Running: true, Error: err.Error()}, fmt.Errorf("restore app effect state: %w", err)
		}
		t.storeCachedDevice(*previous)
		t.storeRestoreDevice(*previous, time.Now().Add(3*time.Second))
		return DeviceEffectStatus{Serial: req.Device.Serial, Running: false}, nil
	}
	firmwareWasOff := t.stopFirmwareEffect(req.Device.Serial)
	if firmwareWasOff != nil && *firmwareWasOff {
		if err := sendEffectPowerOff(ctx, ctrl, serial, req.Device); err != nil {
			return DeviceEffectStatus{Serial: req.Device.Serial, Running: true, Error: err.Error()}, fmt.Errorf("restore firmware effect power: %w", err)
		}
		off := req.Device
		off.On = false
		t.storeCachedDevice(off)
		t.storeRestoreDevice(off, time.Now().Add(3*time.Second))
	}
	for _, msg := range stopDeviceEffectMessages(req.Device) {
		if err := ctx.Err(); err != nil {
			return DeviceEffectStatus{Serial: req.Device.Serial, Running: true, Error: err.Error()}, err
		}
		logLifxSend(serial, req.Device, "effect-off", msg)
		if err := ctrl.Send(serial, msg); err != nil {
			return DeviceEffectStatus{Serial: req.Device.Serial, Running: true, Error: err.Error()}, fmt.Errorf("stop device effect: %w", err)
		}
	}
	return DeviceEffectStatus{Serial: req.Device.Serial, Running: false}, nil
}

func startDeviceEffectMessage(req StartDeviceEffectRequest) (*protocol.Message, DeviceEffect, error) {
	speed := effectSpeed(req.SpeedMS)
	switch req.Device.Kind {
	case DeviceKindMultizone:
		if req.Effect != "" && req.Effect != DeviceEffectMove {
			return nil, req.Effect, fmt.Errorf("effect %q is not supported for multizone devices", req.Effect)
		}
		return messages.SetMultizoneMoveEffect(speed, !strings.EqualFold(req.Direction, "reverse")), DeviceEffectMove, nil
	case DeviceKindMatrix:
		effect := req.Effect
		if effect == "" {
			effect = DeviceEffectFlame
		}
		if !matrixEffectSupported(req.Device, effect) {
			return nil, effect, fmt.Errorf("effect %q requires matrix firmware 4.8 or newer", effect)
		}
		switch effect {
		case DeviceEffectFlame:
			return messages.SetMatrixFlameEffect(speed), DeviceEffectFlame, nil
		case DeviceEffectMorph:
			return messages.SetMatrixMorphEffect(speed, matrixEffectPalette(req.Device)...), DeviceEffectMorph, nil
		case DeviceEffectClouds:
			return messages.SetMatrixCloudsEffect(speed, nil), DeviceEffectClouds, nil
		default:
			return nil, req.Effect, fmt.Errorf("effect %q is not supported for matrix devices", req.Effect)
		}
	default:
		return nil, req.Effect, fmt.Errorf("effects are not supported for %s devices", req.Device.Kind)
	}
}

func matrixEffectSupported(device Device, effect DeviceEffect) bool {
	switch effect {
	case DeviceEffectClouds:
		return firmwareAtLeast(device.Firmware, 4, 8)
	default:
		return true
	}
}

func (t *LifxTransport) startAppDeviceEffect(ctx context.Context, ctrl lifxController, serial lifxdevice.Serial, req StartDeviceEffectRequest) (DeviceEffectStatus, error) {
	if !appEffectSupportedForDevice(req.Effect, req.Device.Kind) {
		err := fmt.Errorf("effect %q is not supported for %s devices", req.Effect, req.Device.Kind)
		return DeviceEffectStatus{Serial: req.Device.Serial, Running: false, Effect: string(req.Effect), Error: err.Error()}, err
	}
	lifxDevice, ok := lifxDeviceBySerial(ctrl.GetDevices(), serial)
	if !ok {
		err := fmt.Errorf("device %s was not found in lifx snapshot", req.Device.Serial)
		return DeviceEffectStatus{Serial: req.Device.Serial, Running: false, Effect: string(req.Effect), Error: err.Error()}, err
	}
	previous := t.stopAppEffect(req.Device.Serial)
	captured := captureAppEffectState(req.Device, previous, t.cachedDevice(req.Device.Serial))
	effect, err := newAppEffect(req, lifxDevice, captured)
	if err != nil {
		return DeviceEffectStatus{Serial: req.Device.Serial, Running: false, Effect: string(req.Effect), Error: err.Error()}, err
	}
	if err := ctx.Err(); err != nil {
		return DeviceEffectStatus{Serial: req.Device.Serial, Running: false, Effect: string(req.Effect), Error: err.Error()}, err
	}
	runCtx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	renderer := lifxeffectadapters.NewRendererForDevice(lifxDevice, func(msg *protocol.Message) error {
		logLifxSend(serial, req.Device, "app-effect-frame", msg)
		return ctrl.Send(serial, msg)
	})
	step := appEffectStep(req, lifxDevice)
	if !captured.On {
		frame, ok := effect.Next(step)
		if !ok {
			return DeviceEffectStatus{Serial: req.Device.Serial, Running: false, Effect: string(req.Effect)}, fmt.Errorf("effect %q produced no frame", req.Effect)
		}
		if err := renderer.RenderFrame(ctx, frame); err != nil {
			return DeviceEffectStatus{Serial: req.Device.Serial, Running: false, Effect: string(req.Effect), Error: err.Error()}, fmt.Errorf("prime effect frame: %w", err)
		}
		msg := messages.SetPowerOn()
		logLifxSend(serial, req.Device, "effect-power-on", msg)
		if err := ctrl.Send(serial, msg); err != nil {
			return DeviceEffectStatus{Serial: req.Device.Serial, Running: false, Effect: string(req.Effect), Error: err.Error()}, fmt.Errorf("set power on for effect: %w", err)
		}
		effect.Reset()
	}
	active := captured
	active.On = true
	t.storeCachedDevice(active)
	t.storeAppEffect(req.Device.Serial, runningAppEffect{effect: req.Effect, speedMS: req.SpeedMS, direction: req.Direction, cancel: cancel, done: done, previous: captured})
	go t.runAppDeviceEffect(runCtx, req.Device.Serial, req.Effect, lifxeffects.NewRunner(effect, renderer, step), done)
	return DeviceEffectStatus{Serial: req.Device.Serial, Running: true, Effect: string(req.Effect)}, nil
}

func (t *LifxTransport) runAppDeviceEffect(ctx context.Context, serial string, effect DeviceEffect, runner *lifxeffects.Runner, done chan<- struct{}) {
	defer close(done)
	if err := runner.Run(ctx); err != nil && ctx.Err() == nil {
		log.Printf("hikari: app effect %q failed for %s: %v", effect, serial, err)
	}
	t.clearAppEffect(serial, effect)
}

func newAppEffect(req StartDeviceEffectRequest, lifxDevice lifxdevice.Device, previous Device) (lifxeffects.Effect, error) {
	caps := appEffectCapabilities(lifxDevice)
	switch req.Effect {
	case DeviceEffectSnake:
		color := appEffectPrimaryColor(previous)
		return lifxeffects.NewSnake(lifxeffects.SnakeConfig{
			Capabilities: caps,
			Size:         appEffectSnakeSize(lifxDevice),
			Color:        color,
		}), nil
	case DeviceEffectWorm:
		return lifxeffects.NewWorm(lifxeffects.WormConfig{
			Capabilities: caps,
			Size:         appEffectSnakeSize(lifxDevice),
			Color:        appEffectPrimaryColor(previous),
		}), nil
	case DeviceEffectFrames:
		return lifxeffects.NewConcentricFrames(lifxeffects.ConcentricFramesConfig{
			Capabilities: caps,
			Direction:    lifxeffects.DirectionInOut,
			Colors:       appEffectPalette(previous),
		}), nil
	case DeviceEffectWaterfall:
		return lifxeffects.NewWaterfall(lifxeffects.WaterfallConfig{
			Capabilities: caps,
			Colors:       appEffectPalette(previous),
		}), nil
	case DeviceEffectRockets:
		return lifxeffects.NewRockets(lifxeffects.RocketsConfig{
			Capabilities: caps,
			Colors:       appEffectPalette(previous),
		}), nil
	case DeviceEffectWave:
		return lifxeffects.NewWave(lifxeffects.WaveConfig{
			Capabilities: caps,
			Palette:      appEffectFlowPalette(previous),
			Waves:        2,
		}), nil
	case DeviceEffectRing:
		return lifxeffects.NewRing(lifxeffects.RingConfig{
			Capabilities: caps,
			Palette:      appEffectFlowPalette(previous),
			Period:       appEffectPeriod(req.SpeedMS, 2*time.Second),
		}), nil
	case DeviceEffectFlow:
		return lifxeffects.NewFlow(lifxeffects.FlowConfig{
			Capabilities:   caps,
			Palette:        appEffectFlowPalette(previous),
			Axis:           lifxeffects.FlowAxisDiagonal,
			BrightnessMode: lifxeffects.FlowBrightnessConstant,
			Sampling:       lifxeffects.FlowSamplingInterpolate,
			Period:         appEffectPeriod(req.SpeedMS, 4*time.Second),
		}), nil
	case DeviceEffectComet:
		return lifxeffects.NewComet(lifxeffects.CometConfig{
			Capabilities:               caps,
			Palette:                    appEffectCometPalette(previous),
			Axis:                       lifxeffects.FlowAxisHorizontal,
			TailSize:                   5,
			BackgroundBrightnessFactor: 1.0,
			PeakBrightnessFactor:       1.5,
			TailCurve:                  3.0,
			TailSaturationFactor:       0.25,
			Period:                     appEffectPeriod(req.SpeedMS, 4*time.Second),
		}), nil
	case DeviceEffectSparkle:
		return lifxeffects.NewSparkle(lifxeffects.SparkleConfig{
			Capabilities:         caps,
			Palette:              appEffectSparklePalette(previous),
			Density:              0.18,
			Decay:                1.2,
			BackgroundFloor:      0.55,
			PeakBrightnessFactor: 1.5,
			Period:               appEffectPeriod(req.SpeedMS, 2*time.Second),
			Seed:                 1,
		}), nil
	case DeviceEffectScanner:
		return lifxeffects.NewScanner(lifxeffects.ScannerConfig{
			Capabilities:               caps,
			Palette:                    appEffectScannerPalette(previous),
			Axis:                       lifxeffects.FlowAxisHorizontal,
			BackgroundBrightnessFactor: 0.8,
			PeakBrightnessFactor:       1.55,
			Period:                     appEffectPeriod(req.SpeedMS, appEffectScannerPeriod(req.Device.Kind)),
		}), nil
	default:
		return nil, fmt.Errorf("effect %q is not supported as an app effect", req.Effect)
	}
}

func appEffectPeriod(speedMS int, fallback time.Duration) time.Duration {
	if speedMS <= 0 {
		return fallback
	}
	return time.Duration(speedMS) * time.Millisecond
}

func appEffectScannerPeriod(kind DeviceKind) time.Duration {
	if kind == DeviceKindMultizone {
		return 4 * time.Second
	}
	return 2 * time.Second
}

func appEffectCapabilities(device lifxdevice.Device) lifxeffects.Capabilities {
	caps := lifxeffects.CapabilitiesFromDevice(device)
	surface := lifxdevice.SurfaceFromDevice(device)
	caps.LightType = surface.LightType
	caps.Width = max(surface.Width, 1)
	caps.Height = max(surface.Height, 1)
	caps.Zones = max(surface.Zones, 1)
	if surface.Matrix != nil {
		caps.ChainLength = len(surface.Matrix.Chains)
		caps.ChainOrientations = make([]lifxdevice.Orientation, 0, len(surface.Matrix.Chains))
		for _, chain := range surface.Matrix.Chains {
			caps.ChainOrientations = append(caps.ChainOrientations, chain.Orientation)
		}
	}
	return caps
}

func appEffectSnakeSize(device lifxdevice.Device) int {
	return min(defaultAppEffectTailSize, max(appEffectCapabilities(device).Width, 1))
}

func appEffectStep(req StartDeviceEffectRequest, device lifxdevice.Device) time.Duration {
	if req.SpeedMS <= 0 {
		return defaultAppEffectStep
	}
	switch req.Effect {
	case DeviceEffectSnake, DeviceEffectWorm:
		caps := appEffectCapabilities(device)
		snakeSize := min(defaultAppEffectTailSize, max(caps.Width, 1))
		steps := max(caps.Width, 1)*max(caps.Height, 1) + snakeSize
		return clampDuration(time.Duration(req.SpeedMS)*time.Millisecond/time.Duration(max(steps, 1)), minAppEffectStep, maxAppEffectStep)
	case DeviceEffectRockets:
		caps := appEffectCapabilities(device)
		steps := max(caps.Width, 1) * max(caps.Height, 1)
		return clampDuration(time.Duration(req.SpeedMS)*time.Millisecond/time.Duration(max(steps, 1)), minAppEffectStep, maxAppEffectStep)
	case DeviceEffectWaterfall, DeviceEffectWave:
		caps := appEffectCapabilities(device)
		return clampDuration(time.Duration(req.SpeedMS)*time.Millisecond/time.Duration(max(caps.Height, 1)), minAppEffectStep, maxAppEffectStep)
	case DeviceEffectFrames:
		caps := appEffectCapabilities(device)
		steps := min((max(caps.Width, 1)-1)/2, (max(caps.Height, 1)-1)/2) + 1
		return clampDuration(time.Duration(req.SpeedMS)*time.Millisecond/time.Duration(max(steps*2, 1)), minAppEffectStep, maxAppEffectStep)
	default:
		return defaultAppEffectStep
	}
}

func clampDuration(value, minValue, maxValue time.Duration) time.Duration {
	if value < minValue {
		return minValue
	}
	if value > maxValue {
		return maxValue
	}
	return value
}

func appEffectPrimaryColor(device Device) lifxeffects.Color {
	if device.Color != nil {
		return hslColorToEffectColor(*device.Color, device.Brightness, device.Kelvin, device.Capability)
	}
	return hslColorToEffectColor(HSLColor{H: 220, S: 0.7, L: device.Brightness, Kelvin: device.Kelvin}, device.Brightness, device.Kelvin, device.Capability)
}

func appEffectPalette(device Device) []lifxeffects.Color {
	colors := representativePaletteColors(nonDarkPaletteColors(visibleAppEffectColors(device)), 6)
	if len(colors) == 0 {
		if device.Color != nil {
			colors = []HSLColor{*device.Color}
		} else {
			colors = []HSLColor{
				{H: 28, S: 0.75, L: 0.65},
				{H: 200, S: 0.7, L: 0.62},
				{H: 285, S: 0.62, L: 0.6},
			}
		}
	}
	if !paletteHasVariation(colors) {
		colors = derivedAppEffectPalette(colors[0])
	}
	palette := make([]lifxeffects.Color, 0, len(colors))
	for _, color := range colors {
		palette = append(palette, hslColorToEffectColor(color, device.Brightness, device.Kelvin, device.Capability))
	}
	return palette
}

func paletteHasVariation(colors []HSLColor) bool {
	if len(colors) < 2 {
		return false
	}
	first := colors[0]
	for _, color := range colors[1:] {
		if math.Abs(first.S-color.S) > 0.08 || math.Abs(first.L-color.L) > 0.08 || hueDistance(first.H, color.H) > 12 {
			return true
		}
		if first.Kelvin > 0 && color.Kelvin > 0 && absInt(first.Kelvin-color.Kelvin) > 250 {
			return true
		}
	}
	return false
}

func derivedAppEffectPalette(color HSLColor) []HSLColor {
	if color.Kelvin > 0 && color.S <= 0.005 {
		return derivedKelvinPalette(color)
	}
	saturation := math.Max(color.S, 0.72)
	lightness := color.L
	if lightness <= 0 {
		lightness = 0.55
	}
	return []HSLColor{
		{H: wrapHue(color.H - 55), S: clamp(saturation*0.95, 0, 1), L: lightness},
		{H: wrapHue(color.H + 65), S: clamp(saturation, 0, 1), L: lightness},
		{H: wrapHue(color.H + 140), S: clamp(saturation*0.9, 0, 1), L: lightness},
	}
}

func derivedKelvinPalette(color HSLColor) []HSLColor {
	kelvin := color.Kelvin
	if kelvin <= 0 {
		kelvin = 3500
	}
	lightness := color.L
	if lightness <= 0 {
		lightness = 0.55
	}
	return []HSLColor{
		{S: 0, L: lightness, Kelvin: clampInt(kelvin-700, 1500, 9000)},
		{S: 0, L: lightness, Kelvin: clampInt(kelvin, 1500, 9000)},
		{S: 0, L: lightness, Kelvin: clampInt(kelvin+700, 1500, 9000)},
	}
}

func wrapHue(hue float64) float64 {
	wrapped := math.Mod(hue, 360)
	if wrapped < 0 {
		wrapped += 360
	}
	return wrapped
}

func hueDistance(a, b float64) float64 {
	distance := math.Abs(wrapHue(a) - wrapHue(b))
	if distance > 180 {
		return 360 - distance
	}
	return distance
}

func visibleAppEffectColors(device Device) []HSLColor {
	if len(device.Zones) > 0 {
		return append([]HSLColor(nil), device.Zones...)
	}
	return visibleMatrixPixels(device.Chain)
}

func appEffectMotionPalette(device Device) lifxeffects.Palette {
	return appEffectFlowPalette(device)
}

func appEffectFlowPalette(device Device) lifxeffects.Palette {
	colors := appEffectPalette(device)
	if len(colors) == 0 {
		return lifxeffects.Palette{}
	}
	midpoint := max(1, len(colors)/2)
	palette := lifxeffects.Palette{
		Name:        "hikari-device",
		Base:        append([]lifxeffects.Color(nil), colors[:midpoint]...),
		Backgrounds: []lifxeffects.Color{appEffectBackgroundColor(device, colors[0])},
	}
	if midpoint < len(colors) {
		palette.Accents = append([]lifxeffects.Color(nil), colors[midpoint:]...)
	}
	return palette
}

func appEffectSparklePalette(device Device) lifxeffects.Palette {
	palette := appEffectFlowPalette(device)
	palette.Name = "hikari-sparkle"
	background := appEffectBackgroundColor(device, appEffectPrimaryColor(device))
	palette.Backgrounds = []lifxeffects.Color{scaledEffectColor(background, 0.85, 1.0)}

	if len(palette.Accents) == 0 {
		palette.Accents = []lifxeffects.Color{
			derivedEffectAccent(background, 145, 1.25),
			derivedEffectAccent(background, 215, 1.35),
		}
		return palette
	}
	for index := range palette.Accents {
		palette.Accents[index] = vividEffectAccent(palette.Accents[index], background, 1.25)
	}
	return palette
}

func appEffectScannerPalette(device Device) lifxeffects.Palette {
	background := appEffectBackgroundColor(device, appEffectPrimaryColor(device))
	background = scaledEffectColor(background, 0.85, 1.0)

	accent, ok := contrastingAppEffectColor(device, background)
	if ok {
		accent = vividEffectAccent(accent, background, 1.45)
	} else {
		accent = derivedEffectAccent(background, 180, 1.45)
	}

	return lifxeffects.Palette{
		Name:        "hikari-scanner",
		Base:        []lifxeffects.Color{background},
		Accents:     []lifxeffects.Color{accent},
		Backgrounds: []lifxeffects.Color{background},
	}
}

func appEffectCometPalette(device Device) lifxeffects.Palette {
	background := appEffectBackgroundColor(device, appEffectPrimaryColor(device))
	base := appEffectCometBaseColor(background)
	accent := appEffectCometAccentColor(device, background)
	return lifxeffects.Palette{
		Name:        "hikari-comet",
		Base:        []lifxeffects.Color{base},
		Accents:     []lifxeffects.Color{accent},
		Backgrounds: []lifxeffects.Color{background},
	}
}

func appEffectBackgroundColor(device Device, fallback lifxeffects.Color) lifxeffects.Color {
	colors := representativePaletteColors(nonDarkPaletteColors(visibleAppEffectColors(device)), 1)
	if len(colors) > 0 {
		return hslColorToEffectColor(colors[0], device.Brightness, device.Kelvin, device.Capability)
	}
	if device.Color != nil {
		return hslColorToEffectColor(*device.Color, device.Brightness, device.Kelvin, device.Capability)
	}
	return fallback
}

func scaledEffectColor(color lifxeffects.Color, brightnessFactor float64, saturationFactor float64) lifxeffects.Color {
	color.Brightness = clamp(color.Brightness*brightnessFactor, 0, 100)
	color.Saturation = clamp(color.Saturation*saturationFactor, 0, 100)
	return color
}

func vividEffectAccent(color lifxeffects.Color, background lifxeffects.Color, brightnessFactor float64) lifxeffects.Color {
	if hueDistance(color.Hue, background.Hue) < 45 {
		color.Hue = wrapHue(background.Hue + 180)
	}
	color.Saturation = math.Max(color.Saturation, 85)
	color.Brightness = clamp(math.Max(color.Brightness*brightnessFactor, math.Max(background.Brightness+25, 60)), 0, 100)
	return color
}

func derivedEffectAccent(background lifxeffects.Color, hueOffset float64, brightnessFactor float64) lifxeffects.Color {
	accent := background
	accent.Hue = wrapHue(background.Hue + hueOffset)
	accent.Saturation = math.Max(background.Saturation, 85)
	accent.Brightness = clamp(math.Max(background.Brightness*brightnessFactor, math.Max(background.Brightness+25, 60)), 0, 100)
	accent.Kelvin = 3500
	return accent
}

func appEffectCometBaseColor(background lifxeffects.Color) lifxeffects.Color {
	base := background
	base.Hue = wrapHue(base.Hue + 180)
	base.Saturation = math.Max(base.Saturation*0.75, 60)
	base.Brightness = clamp(math.Max(base.Brightness*0.35, 5), 0, 100)
	base.Kelvin = 5000
	return base
}

func appEffectCometAccentColor(device Device, background lifxeffects.Color) lifxeffects.Color {
	accent := warmCometHeadColor(background)
	if existing, ok := warmExistingAppEffectColor(device, background); ok {
		accent = existing
	}
	accent.Saturation = math.Max(accent.Saturation, 80)
	accent.Brightness = clamp(math.Max(accent.Brightness, math.Max(background.Brightness+30, 65)), 0, 100)
	accent.Kelvin = 2800
	return accent
}

func warmCometHeadColor(background lifxeffects.Color) lifxeffects.Color {
	accent := background
	switch {
	case hueDistance(background.Hue, 35) <= 70:
		accent.Hue = wrapHue(background.Hue + 24)
	case hueDistance(background.Hue, 0) <= 40:
		accent.Hue = 35
	case hueDistance(background.Hue, 55) <= 35:
		accent.Hue = 28
	default:
		accent.Hue = 32
	}
	return accent
}

func warmExistingAppEffectColor(device Device, background lifxeffects.Color) (lifxeffects.Color, bool) {
	colors := representativePaletteColors(nonDarkPaletteColors(visibleAppEffectColors(device)), 6)
	var best HSLColor
	bestScore := 0.0
	for _, color := range colors {
		candidate := hslColorToEffectColor(color, device.Brightness, device.Kelvin, device.Capability)
		warmScore := 90 - hueDistance(candidate.Hue, 32)
		contrastScore := hueDistance(background.Hue, candidate.Hue) * 0.15
		brightnessScore := candidate.Brightness * 0.15
		score := warmScore + contrastScore + brightnessScore
		if score > bestScore && warmScore > 20 {
			best = color
			bestScore = score
		}
	}
	if bestScore <= 0 {
		return lifxeffects.Color{}, false
	}
	return hslColorToEffectColor(best, device.Brightness, device.Kelvin, device.Capability), true
}

func contrastingAppEffectColor(device Device, background lifxeffects.Color) (lifxeffects.Color, bool) {
	colors := representativePaletteColors(nonDarkPaletteColors(visibleAppEffectColors(device)), 6)
	var best HSLColor
	bestScore := 0.0
	for _, color := range colors {
		candidate := hslColorToEffectColor(color, device.Brightness, device.Kelvin, device.Capability)
		score := hueDistance(background.Hue, candidate.Hue) + math.Abs(background.Saturation-candidate.Saturation)*0.5 + math.Abs(background.Brightness-candidate.Brightness)*0.25
		if score > bestScore {
			best = color
			bestScore = score
		}
	}
	if bestScore <= 35 {
		return lifxeffects.Color{}, false
	}
	return hslColorToEffectColor(best, device.Brightness, device.Kelvin, device.Capability), true
}

func hslColorToEffectColor(color HSLColor, brightness float64, kelvin int, capability DeviceCapability) lifxeffects.Color {
	if brightness <= 0 {
		brightness = color.L
	}
	if kelvin <= 0 {
		kelvin = color.Kelvin
	}
	color.L = brightness
	hsbk := hslColorToHSBK(color, brightness, kelvin, capability)
	return lifxdevice.NewColor(hsbk)
}

func captureAppEffectState(requested Device, previous *Device, cached *Device) Device {
	if previous != nil {
		return *previous
	}
	if cached != nil {
		return *cached
	}
	return requested
}

func (t *LifxTransport) effectRequestDevice(requested Device) Device {
	if cached := t.cachedDevice(requested.Serial); cached != nil {
		return *cached
	}
	return requested
}

func restoreDeviceState(ctx context.Context, ctrl lifxController, serial lifxdevice.Serial, device Device) error {
	if !device.On {
		if err := sendEffectPowerOff(ctx, ctrl, serial, device); err != nil {
			return fmt.Errorf("restore power off: %w", err)
		}
		for index, msg := range deviceStateMessages(device, false) {
			if msg == nil {
				continue
			}
			if err := ctx.Err(); err != nil {
				return err
			}
			logLifxSend(serial, device, fmt.Sprintf("effect-restore-state-%d", index+1), msg)
			if err := ctrl.Send(serial, msg); err != nil {
				return fmt.Errorf("restore %s state: %w", device.Kind, err)
			}
		}
		return nil
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	intent := normalizeDeviceCommandIntent("", device)
	return sendDeviceState(ctx, ctrl, serial, device, false, intent, &device)
}

func sendEffectPowerOff(ctx context.Context, ctrl lifxController, serial lifxdevice.Serial, device Device) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	msg := messages.SetPowerOff()
	logLifxSend(serial, device, "effect-restore-power-off", msg)
	if err := ctrl.Send(serial, msg); err != nil {
		return err
	}
	timer := time.NewTimer(effectPowerOffSettleDelay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

func lifxDeviceBySerial(devices []lifxdevice.Device, serial lifxdevice.Serial) (lifxdevice.Device, bool) {
	for _, device := range devices {
		if device.Serial == serial {
			return device, true
		}
	}
	return lifxdevice.Device{}, false
}

func isAppEffect(effect DeviceEffect) bool {
	switch effect {
	case DeviceEffectSnake, DeviceEffectWorm, DeviceEffectFrames, DeviceEffectWaterfall, DeviceEffectRockets, DeviceEffectWave, DeviceEffectRing, DeviceEffectFlow, DeviceEffectComet, DeviceEffectSparkle, DeviceEffectScanner:
		return true
	default:
		return false
	}
}

func appEffectSupportedForDevice(effect DeviceEffect, kind DeviceKind) bool {
	if !isAppEffect(effect) {
		return false
	}
	switch kind {
	case DeviceKindMatrix:
		return effect != DeviceEffectComet
	case DeviceKindMultizone:
		return effect == DeviceEffectFlow || effect == DeviceEffectComet || effect == DeviceEffectSparkle || effect == DeviceEffectScanner
	default:
		return false
	}
}

func matrixEffectPalette(device Device) []packets.LightHsbk {
	visibleColors := visibleMatrixPixels(device.Chain)
	colors := nonDarkPaletteColors(visibleColors)
	if len(colors) == 0 && len(visibleColors) == 0 && device.Color != nil {
		colors = []HSLColor{*device.Color}
	}
	if len(colors) == 0 {
		colors = []HSLColor{
			{H: 28, S: 0.75, L: 0.7},
			{H: 200, S: 0.7, L: 0.65},
			{H: 280, S: 0.65, L: 0.62},
		}
	}
	targetBrightness := matrixEffectPaletteBrightness()
	colors = normalizePaletteBrightness(representativePaletteColors(colors, matrixEffectPaletteMaxColors), targetBrightness)
	logMatrixEffectPalette(device, visibleColors, colors, targetBrightness)
	return hslColorsToHSBK(colors, device.Brightness, device.Kelvin, device.Capability)
}

func visibleMatrixPixels(chain []Matrix) []HSLColor {
	var colors []HSLColor
	for _, matrix := range chain {
		if len(matrix.Rows) == 0 {
			colors = append(colors, matrix.Pixels...)
			continue
		}
		index := 0
		for _, row := range matrix.Rows {
			for col := 0; col < row.Cols; col++ {
				if index >= len(matrix.Pixels) {
					break
				}
				if !hiddenMatrixColumn(row.HiddenCols, col) {
					colors = append(colors, matrix.Pixels[index])
				}
				index++
			}
		}
	}
	return colors
}

func hiddenMatrixColumn(hidden []int, col int) bool {
	for _, hiddenCol := range hidden {
		if hiddenCol == col {
			return true
		}
	}
	return false
}

func nonDarkPaletteColors(colors []HSLColor) []HSLColor {
	filtered := make([]HSLColor, 0, len(colors))
	for _, color := range colors {
		if color.L > matrixEffectPaletteMinLightness {
			filtered = append(filtered, color)
		}
	}
	return filtered
}

func representativePaletteColors(colors []HSLColor, limit int) []HSLColor {
	if limit <= 0 {
		return nil
	}
	if len(colors) <= limit {
		return append([]HSLColor(nil), colors...)
	}
	buckets := make([]paletteBucket, matrixEffectPaletteHueBuckets)
	for _, color := range colors {
		index := paletteHueBucket(color)
		buckets[index].add(color)
	}
	palette := make([]HSLColor, 0, min(limit, len(buckets)))
	for _, bucket := range buckets {
		if bucket.count > 0 {
			palette = append(palette, bucket.average())
		}
	}
	if len(palette) > limit {
		palette = samplePaletteColors(palette, limit)
	}
	return palette
}

func paletteHueBucket(color HSLColor) int {
	hue := math.Mod(color.H, 360)
	if hue < 0 {
		hue += 360
	}
	index := int(math.Floor(hue / 360 * matrixEffectPaletteHueBuckets))
	if index >= matrixEffectPaletteHueBuckets {
		return matrixEffectPaletteHueBuckets - 1
	}
	return index
}

type paletteBucket struct {
	count       int
	hueX        float64
	hueY        float64
	saturation  float64
	lightness   float64
	kelvinSum   int
	kelvinCount int
}

func (b *paletteBucket) add(color HSLColor) {
	radians := color.H * math.Pi / 180
	weight := math.Max(color.S, 0.01)
	b.count++
	b.hueX += math.Cos(radians) * weight
	b.hueY += math.Sin(radians) * weight
	b.saturation += color.S
	b.lightness += color.L
	if color.Kelvin > 0 {
		b.kelvinSum += color.Kelvin
		b.kelvinCount++
	}
}

func (b paletteBucket) average() HSLColor {
	count := float64(b.count)
	color := HSLColor{
		H: averageHue(b.hueX, b.hueY),
		S: b.saturation / count,
		L: b.lightness / count,
	}
	if b.kelvinCount > 0 {
		color.Kelvin = int(math.Round(float64(b.kelvinSum) / float64(b.kelvinCount)))
		if color.S <= 0.005 {
			color.H = 0
			color.S = 0
		}
	}
	return color
}

func matrixEffectPaletteBrightness() float64 {
	// Matrix Morph firmware appears to treat palette brightness as effect intensity;
	// values below 100% can look much darker than the device brightness suggests.
	return 1
}

func normalizePaletteBrightness(colors []HSLColor, brightness float64) []HSLColor {
	normalized := make([]HSLColor, len(colors))
	for i, color := range colors {
		color.L = brightness
		normalized[i] = color
	}
	return normalized
}

func samplePaletteColors(colors []HSLColor, limit int) []HSLColor {
	if len(colors) <= limit {
		return append([]HSLColor(nil), colors...)
	}
	sampled := make([]HSLColor, 0, limit)
	for i := 0; i < limit; i++ {
		index := int(math.Round(float64(i) * float64(len(colors)-1) / float64(limit-1)))
		sampled = append(sampled, colors[index])
	}
	return sampled
}

func logMatrixEffectPalette(device Device, visibleColors []HSLColor, palette []HSLColor, brightness float64) {
	if !lifxDebugEnabled() {
		return
	}
	hues := make([]string, len(palette))
	for i, color := range palette {
		hues[i] = fmt.Sprintf("%.0f", color.H)
	}
	log.Printf(
		"hikari: morph palette name=%q group=%s visible=%d palette=%d brightness=%.2f hues=[%s]",
		device.Name,
		device.GroupID,
		len(visibleColors),
		len(palette),
		brightness,
		strings.Join(hues, ","),
	)
}

func firmwareAtLeast(version string, major, minor int) bool {
	fields := strings.FieldsFunc(version, func(r rune) bool {
		return r < '0' || r > '9'
	})
	if len(fields) < 2 {
		return false
	}
	gotMajor, err := strconv.Atoi(fields[0])
	if err != nil {
		return false
	}
	gotMinor, err := strconv.Atoi(fields[1])
	if err != nil {
		return false
	}
	if gotMajor != major {
		return gotMajor > major
	}
	return gotMinor >= minor
}

func effectSpeed(speedMS int) time.Duration {
	if speedMS <= 0 {
		return defaultFirmwareEffectSpeed
	}
	return time.Duration(speedMS) * time.Millisecond
}

func userFriendlyNetworkError(err error) string {
	if err == nil {
		return ""
	}
	message := strings.ToLower(err.Error())
	if strings.Contains(message, "network interface") && strings.Contains(message, "not available") {
		return "Selected network interface unavailable. Choose another interface or Automatic."
	}
	if strings.Contains(message, "no suitable broadcast interface") || strings.Contains(message, "broadcast interface") {
		return "No network interfaces found."
	}
	if strings.Contains(message, "transport has not been started") || strings.Contains(message, "network connection") || strings.Contains(message, "connection lost") {
		return "Connection lost. Refresh discovery to reconnect."
	}
	return err.Error()
}

func stopDeviceEffectMessages(device Device) []*protocol.Message {

	switch device.Kind {
	case DeviceKindMultizone:
		return []*protocol.Message{messages.SetMultizoneEffectOff()}
	case DeviceKindMatrix:
		return []*protocol.Message{messages.SetMatrixEffectOff()}
	default:
		return nil
	}
}

func (t *LifxTransport) createController(interfaceName string) (lifxController, error) {
	var cfg *lifxclient.Config
	if interfaceName != "" {
		cfg = &lifxclient.Config{BroadcastInterfaceName: interfaceName}
	}
	ctrl, err := t.controllerFactory(cfg)
	if err != nil {
		return nil, fmt.Errorf("create lifx controller: %w", err)
	}
	return ctrl, nil
}

func (t *LifxTransport) restartController(ctx context.Context, interfaceName string, warning string) error {
	old := t.currentController()
	if old != nil {
		if err := t.restoreAllAppEffects(ctx, old); err != nil {
			log.Printf("hikari: restore app effects before network switch failed: %v", err)
		}
		t.mu.Lock()
		if t.controller == old {
			t.controller = nil
		}
		t.clearRuntimeStateLocked()
		t.mu.Unlock()
		if err := old.Close(); err != nil {
			return fmt.Errorf("close lifx controller before network switch: %w", err)
		}
	}

	ctrl, err := t.createController(interfaceName)
	if err != nil {
		return err
	}

	t.mu.Lock()
	t.controller = ctrl
	t.selectedInterfaceName = interfaceName
	t.networkWarning = warning
	t.mu.Unlock()
	log.Printf("hikari: lifx controller restarted with network interface %q", interfaceName)
	return nil
}

func (t *LifxTransport) currentController() lifxController {
	t.mu.RLock()
	defer t.mu.RUnlock()
	return t.controller
}

func (t *LifxTransport) clearRuntimeStateLocked() {
	t.cache = make(map[string]Device)
	t.effects = make(map[string]runningAppEffect)
	t.firmware = make(map[string]runningFirmwareEffect)
	t.restores = make(map[string]effectRestore)
}

func (t *LifxTransport) loadValidatedInterfaceName() (string, string, error) {
	name, err := t.settingsStore.LoadNetworkInterfaceName()
	if err != nil {
		return "", "", err
	}
	name = strings.TrimSpace(name)
	if name == "" {
		return "", "", nil
	}
	settings, err := t.networkSettingsForSelection(name)
	if err == nil {
		return settings.SelectedInterfaceName, settings.Warning, nil
	}
	return name, fmt.Sprintf("Selected network interface %q is unavailable.", name), nil
}

func (t *LifxTransport) networkSettings() (NetworkSettings, error) {
	interfaces, err := t.listNetworkInterfaces()
	if err != nil {
		return NetworkSettings{SelectedInterfaceName: t.configuredInterfaceName(), Interfaces: []NetworkInterface{}, Warning: err.Error()}, nil
	}
	return NetworkSettings{SelectedInterfaceName: t.configuredInterfaceName(), Interfaces: interfaces, Warning: t.currentNetworkWarning()}, nil
}

func (t *LifxTransport) networkSettingsForSelection(name string) (NetworkSettings, error) {
	interfaces, err := t.listNetworkInterfaces()
	if err != nil {
		return NetworkSettings{SelectedInterfaceName: "", Interfaces: []NetworkInterface{}, Warning: err.Error()}, err
	}
	if name == "" {
		return NetworkSettings{SelectedInterfaceName: "", Interfaces: interfaces}, nil
	}
	for _, entry := range interfaces {
		if entry.Name == name {
			return NetworkSettings{SelectedInterfaceName: name, Interfaces: interfaces}, nil
		}
	}
	settings := NetworkSettings{SelectedInterfaceName: "", Interfaces: interfaces, Warning: fmt.Sprintf("Network interface %q is no longer available; using Automatic.", name)}
	return settings, fmt.Errorf("network interface %q is not available", name)
}

func (t *LifxTransport) ensureNetworkInterfaceAvailable() error {
	interfaces, err := t.interfaceLister()
	if err != nil {
		return fmt.Errorf("check network interfaces: %w", err)
	}
	if len(interfaces) == 0 {
		return fmt.Errorf("no suitable broadcast interface found")
	}
	selected := t.currentInterfaceName()
	if selected == "" {
		return nil
	}
	for _, entry := range interfaces {
		if entry.Name == selected {
			return nil
		}
	}
	return fmt.Errorf("network interface %q is not available", selected)
}

func shortNetworkInterfaceLabel(entry lifxclient.BroadcastInterface) string {
	broadcast := entry.Broadcast.String()
	lastDot := strings.LastIndexByte(broadcast, '.')
	if lastDot >= 0 && lastDot+1 < len(broadcast) {
		broadcast = broadcast[lastDot+1:]
	}
	return fmt.Sprintf("%s - %s/%s", entry.Name, entry.IP.String(), broadcast)
}

func (t *LifxTransport) listNetworkInterfaces() ([]NetworkInterface, error) {
	interfaces, err := t.interfaceLister()
	if err != nil {
		return nil, fmt.Errorf("list network interfaces: %w", err)
	}
	mapped := make([]NetworkInterface, 0, len(interfaces))
	for _, entry := range interfaces {
		mapped = append(mapped, NetworkInterface{
			Name:       entry.Name,
			Address:    entry.IP.String(),
			Broadcast:  entry.Broadcast.String(),
			Label:      fmt.Sprintf("%s - %s -> %s", entry.Name, entry.IP.String(), entry.Broadcast.String()),
			ShortLabel: shortNetworkInterfaceLabel(entry),
		})
	}
	sort.SliceStable(mapped, func(i, j int) bool {
		if mapped[i].Name == mapped[j].Name {
			return mapped[i].Address < mapped[j].Address
		}
		return mapped[i].Name < mapped[j].Name
	})
	return mapped, nil
}

func (t *LifxTransport) configuredInterfaceName() string {
	name := t.currentInterfaceName()
	if name != "" {
		return name
	}
	name, err := t.settingsStore.LoadNetworkInterfaceName()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(name)
}

func (t *LifxTransport) currentInterfaceName() string {
	t.mu.RLock()
	defer t.mu.RUnlock()
	return t.selectedInterfaceName
}

func (t *LifxTransport) currentNetworkWarning() string {
	t.mu.RLock()
	defer t.mu.RUnlock()
	return t.networkWarning
}

func (t *LifxTransport) requireController() (lifxController, error) {
	ctrl := t.currentController()
	if ctrl != nil {
		return ctrl, nil
	}
	return nil, fmt.Errorf("lifx transport has not been started")
}

func (t *LifxTransport) replaceCache(devices []Device) {
	t.mu.Lock()
	defer t.mu.Unlock()
	next := make(map[string]Device, len(devices))
	for _, device := range devices {
		if t.restoreActiveLocked(device.Serial, time.Now()) {
			if restore, ok := t.restores[device.Serial]; ok {
				next[device.Serial] = restore.device
				continue
			}
		}
		if t.effectRunningLocked(device.Serial) {
			if cached, ok := t.cache[device.Serial]; ok {
				next[device.Serial] = cached
				continue
			}
		}
		next[device.Serial] = device
	}
	t.cache = next
}

func (t *LifxTransport) storeCachedDevice(device Device) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.cache == nil {
		t.cache = make(map[string]Device)
	}
	t.cache[device.Serial] = device
}

func (t *LifxTransport) cachedDevice(serial string) *Device {
	t.mu.RLock()
	defer t.mu.RUnlock()
	device, ok := t.cache[serial]
	if !ok {
		return nil
	}
	return &device
}

func (t *LifxTransport) storeAppEffect(serial string, effect runningAppEffect) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.effects == nil {
		t.effects = make(map[string]runningAppEffect)
	}
	t.effects[serial] = effect
}

func (t *LifxTransport) storeFirmwareEffect(serial string, effect runningFirmwareEffect) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.firmware == nil {
		t.firmware = make(map[string]runningFirmwareEffect)
	}
	t.firmware[serial] = effect
}

func (t *LifxTransport) captureFirmwareEffectWasOff(serial string, restored *Device, requested Device) bool {
	t.mu.RLock()
	running, ok := t.firmware[serial]
	t.mu.RUnlock()
	if ok {
		return running.wasOff
	}
	if restored != nil {
		return !restored.On
	}
	if cached := t.cachedDevice(serial); cached != nil {
		return !cached.On
	}
	return !requested.On
}

func (t *LifxTransport) stopFirmwareEffect(serial string) *bool {
	t.mu.Lock()
	effect, ok := t.firmware[serial]
	if ok {
		delete(t.firmware, serial)
	}
	t.mu.Unlock()
	if !ok {
		return nil
	}
	wasOff := effect.wasOff
	return &wasOff
}

func (t *LifxTransport) stopAppEffect(serial string) *Device {
	t.mu.Lock()
	effect, ok := t.effects[serial]
	if ok {
		delete(t.effects, serial)
	}
	t.mu.Unlock()
	if !ok {
		return nil
	}
	effect.cancel()
	waitForAppEffectStop(effect)
	previous := effect.previous
	return &previous
}

func (t *LifxTransport) stopAppEffectForStateChange(serial string) (runningAppEffect, bool) {
	t.mu.Lock()
	effect, ok := t.effects[serial]
	if ok {
		delete(t.effects, serial)
	}
	t.mu.Unlock()
	if !ok {
		return runningAppEffect{}, false
	}
	effect.cancel()
	waitForAppEffectStop(effect)
	return effect, true
}

func (t *LifxTransport) stopAllAppEffects() {
	t.mu.Lock()
	effects := t.effects
	t.effects = make(map[string]runningAppEffect)
	t.mu.Unlock()
	for _, effect := range effects {
		effect.cancel()
		waitForAppEffectStop(effect)
	}
}

func (t *LifxTransport) restoreAllAppEffects(ctx context.Context, ctrl lifxController) error {
	t.mu.Lock()
	effects := t.effects
	t.effects = make(map[string]runningAppEffect)
	t.mu.Unlock()

	var restoreErr error
	for _, effect := range effects {
		effect.cancel()
		waitForAppEffectStop(effect)
		serial, err := parseDeviceSerial(effect.previous)
		if err != nil {
			restoreErr = errors.Join(restoreErr, err)
			continue
		}
		if err := restoreDeviceState(ctx, ctrl, serial, effect.previous); err != nil {
			restoreErr = errors.Join(restoreErr, err)
			continue
		}
		t.storeCachedDevice(effect.previous)
	}
	return restoreErr
}

func (t *LifxTransport) clearAppEffect(serial string, effectID DeviceEffect) {
	t.mu.Lock()
	defer t.mu.Unlock()
	effect, ok := t.effects[serial]
	if ok && effect.effect == effectID {
		delete(t.effects, serial)
	}
}

func (t *LifxTransport) reconcileRestoreSnapshot(snapshot *DeviceSnapshot, now time.Time) {
	t.mu.Lock()
	defer t.mu.Unlock()
	for index, device := range snapshot.Devices {
		restore, ok := t.restores[device.Serial]
		if !ok {
			continue
		}
		if now.After(restore.until) {
			delete(t.restores, device.Serial)
			continue
		}
		snapshot.Devices[index] = restore.device
	}
}

func (t *LifxTransport) storeRestoreDevice(device Device, until time.Time) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.restores == nil {
		t.restores = make(map[string]effectRestore)
	}
	t.restores[device.Serial] = effectRestore{device: device, until: until}
}

func (t *LifxTransport) effectRunningLocked(serial string) bool {
	_, ok := t.effects[serial]
	return ok
}

func (t *LifxTransport) restoreActiveLocked(serial string, now time.Time) bool {
	restore, ok := t.restores[serial]
	if !ok {
		return false
	}
	if now.After(restore.until) {
		delete(t.restores, serial)
		return false
	}
	return true
}

func waitForAppEffectStop(effect runningAppEffect) {
	if effect.done == nil {
		return
	}
	timer := time.NewTimer(defaultAppEffectStopTimeout)
	defer timer.Stop()
	select {
	case <-effect.done:
	case <-timer.C:
		log.Printf("hikari: timed out waiting for app effect %q to stop", effect.effect)
	}
}

func mapLifxDevices(devices []lifxdevice.Device) DeviceSnapshot {
	snapshot := emptyDeviceSnapshot()
	locationIDs := make(map[string]bool)
	groupIDs := make(map[string]bool)

	for _, d := range devices {
		if !isSupportedDevice(d) {
			log.Printf("hikari: skipping unsupported lifx device %s type %q", d.Serial.String(), d.Type.String())
			continue
		}
		locationID := idOrUnknown(d.Location, "unknown-location")
		groupID := idOrUnknown(d.Group, "unknown-group")
		if !locationIDs[locationID] {
			snapshot.Locations = append(snapshot.Locations, Location{ID: locationID, Name: nameOrUnknown(d.Location, "Unknown")})
			locationIDs[locationID] = true
		}
		if !groupIDs[groupID] {
			snapshot.Groups = append(snapshot.Groups, Group{ID: groupID, LocationID: locationID, Name: nameOrUnknown(d.Group, "Unknown")})
			groupIDs[groupID] = true
		}
		snapshot.Devices = append(snapshot.Devices, mapLifxDevice(d, groupID))
	}

	return sortDeviceSnapshot(snapshot)
}

func isSupportedDevice(d lifxdevice.Device) bool {
	switch d.Type.String() {
	case "", "light", "hybrid", "switch":
		return true
	default:
		return false
	}
}

func mapLifxDevice(d lifxdevice.Device, groupID string) Device {
	capability := mapLifxCapability(d.ColorProperties)
	color := mapLifxColor(d.Color, capability)
	kelvin := int(d.Color.Kelvin)
	if kelvin <= 0 && isFixedKelvin(capability) {
		kelvin = capability.KelvinMin
	}
	device := Device{
		GroupID:    groupID,
		Serial:     d.Serial.String(),
		Name:       nameOrUnknown(d.Label, d.Serial.String()),
		Model:      nameOrUnknown(d.RegistryName, "LIFX"),
		Kind:       mapDeviceKind(d),
		IPAddress:  deviceIPAddress(d),
		ProductID:  d.ProductID,
		Firmware:   d.FirmwareVersion,
		RSSI:       int(d.WifiRSSI),
		RSSIText:   d.WifiRSSI.String(),
		Online:     true,
		On:         d.PoweredOn,
		Brightness: color.L,
		Capability: capability,
		Color:      &color,
		Kelvin:     kelvin,
		Relays:     mapLifxRelays(d.Relays),
	}

	if device.Kind == DeviceKindSwitch {
		device.On = relaysPoweredOn(device.Relays)
		device.Brightness = 0
		device.Color = nil
		device.Kelvin = 0
		device.Capability = DeviceCapability{}
		device.ButtonConfig = mapLifxButtonConfig(d, DeviceCapability{HasColor: true, KelvinMin: 1500, KelvinMax: 9000})
		return device
	}

	switch device.Kind {
	case DeviceKindMultizone:
		device.ZoneCount = len(d.MultizoneProperties.Zones)
		device.Zones = mapLifxColors(d.MultizoneProperties.Zones, capability)
		applyColorSummary(&device, device.Zones)
	case DeviceKindMatrix:
		device.PixelCount = d.MatrixProperties.NZones
		device.ChainLen = d.MatrixProperties.ChainLength
		if device.ChainLen == 0 {
			device.ChainLen = len(d.MatrixProperties.ChainZones)
		}
		device.Chain = mapLifxMatrixChain(d, capability)
		applyColorSummary(&device, visibleMatrixPixels(device.Chain))
		logMatrixSnapshot(device)
	}

	return device
}

func logMatrixSnapshot(device Device) {
	if !lifxDebugEnabled() {
		return
	}
	visible := visibleMatrixPixels(device.Chain)
	active := nonDarkPaletteColors(visible)
	color := "<nil>"
	if device.Color != nil {
		color = fmt.Sprintf("h=%.0f s=%.2f l=%.2f k=%d", device.Color.H, device.Color.S, device.Color.L, device.Color.Kelvin)
	}
	log.Printf(
		"hikari: matrix snapshot name=%q group=%s model=%q product=%d firmware=%q chains=%d pixels=%d visible=%d active=%d brightness=%.2f color=%s",
		device.Name,
		device.GroupID,
		device.Model,
		device.ProductID,
		device.Firmware,
		len(device.Chain),
		device.PixelCount,
		len(visible),
		len(active),
		device.Brightness,
		color,
	)
}

func applyColorSummary(device *Device, colors []HSLColor) {
	if len(colors) == 0 {
		return
	}
	summary := averageHSLColor(colors)
	device.Brightness = summary.L
	device.Color = &summary
	if summary.Kelvin > 0 {
		device.Kelvin = summary.Kelvin
	}
}

func matrixPixels(chain []Matrix) []HSLColor {
	var colors []HSLColor
	for _, matrix := range chain {
		colors = append(colors, matrix.Pixels...)
	}
	return colors
}

func deviceIPAddress(d lifxdevice.Device) string {
	if d.Address == nil || d.Address.IP == nil {
		return ""
	}
	return d.Address.IP.String()
}

func mapLifxMatrixChain(d lifxdevice.Device, capability DeviceCapability) []Matrix {
	surface := lifxdevice.SurfaceFromDevice(d)
	if surface.Matrix == nil {
		return nil
	}

	chain := make([]Matrix, 0, len(d.MatrixProperties.ChainZones))
	for i, zones := range d.MatrixProperties.ChainZones {
		if i >= len(surface.Matrix.Chains) {
			break
		}
		surfaceChain := surface.Matrix.Chains[i]
		sendWidth := surfaceChain.SendWidth
		sendHeight := matrixSendHeight(sendWidth, len(zones), surfaceChain.Bounds.Height)
		chain = append(chain, Matrix{
			ID:          surfaceChain.Index,
			X:           float64(surfaceChain.Bounds.X),
			Y:           float64(surfaceChain.Bounds.Y),
			W:           float64(surfaceChain.Bounds.Width),
			H:           float64(surfaceChain.Bounds.Height),
			SendWidth:   sendWidth,
			Orientation: int(surfaceChain.Orientation),
			Rows:        mapSurfaceRows(surfaceChain.Rows),
			Pixels:      mapLifxColors(adjustUIGridForOrientation(sendWidth, sendHeight, surfaceChain.Orientation, zones), capability),
		})
	}
	return chain
}

func mapSurfaceRows(rows []lifxdevice.MatrixRow) []MatrixRow {
	mapped := make([]MatrixRow, len(rows))
	for i, row := range rows {
		mapped[i] = MatrixRow{
			Cols:       row.Cols,
			Offset:     float64(row.Offset),
			HiddenCols: append([]int(nil), row.HiddenCols...),
		}
	}
	return mapped
}

func matrixSendHeight(sendWidth int, pixels int, fallback int) int {
	if sendWidth > 0 && pixels > 0 {
		return max((pixels+sendWidth-1)/sendWidth, 1)
	}
	return max(fallback, 1)
}

func adjustUIGridForOrientation(width, height int, orientation lifxdevice.Orientation, colors []packets.LightHsbk) []packets.LightHsbk {
	switch orientation {
	case lifxdevice.OrientationRight:
		return lifxdevice.RotateMatrix(lifxdevice.RotateMatrix90(width, height), colors)
	case lifxdevice.OrientationUpsideDown:
		return lifxdevice.RotateMatrix(lifxdevice.RotateMatrix180(width, height), colors)
	case lifxdevice.OrientationLeft:
		return lifxdevice.RotateMatrix(lifxdevice.RotateMatrix270(width, height), colors)
	default:
		return colors
	}
}

func mapLifxRelays(relays []lifxdevice.Relay) []Relay {
	mapped := make([]Relay, len(relays))
	for i, relay := range relays {
		mapped[i] = Relay{Index: relay.Index, On: relay.PoweredOn}
	}
	return mapped
}

func mapLifxButtonConfig(d lifxdevice.Device, capability DeviceCapability) *ButtonConfig {
	if !d.ButtonConfigKnown {
		return nil
	}
	return &ButtonConfig{
		Known:             true,
		HapticDurationMS:  int(d.ButtonConfig.HapticDurationMs),
		BacklightOnColor:  mapLifxColor(d.ButtonConfig.BacklightOnColor, capability),
		BacklightOffColor: mapLifxColor(d.ButtonConfig.BacklightOffColor, capability),
	}
}

func relaysPoweredOn(relays []Relay) bool {
	for _, relay := range relays {
		if relay.On {
			return true
		}
	}
	return false
}

func mapLifxColors(colors []packets.LightHsbk, capability DeviceCapability) []HSLColor {
	mapped := make([]HSLColor, len(colors))
	for i, color := range colors {
		mapped[i] = mapLifxHSBK(color, capability)
	}
	return mapped
}

func mapLifxColor(color lifxdevice.Color, capability DeviceCapability) HSLColor {
	mapped := HSLColor{
		H: color.Hue,
		S: color.Saturation / 100,
		L: color.Brightness / 100,
	}
	kelvin := int(color.Kelvin)
	if kelvin <= 0 && isFixedKelvin(capability) {
		kelvin = capability.KelvinMin
	}
	if kelvin > 0 && (!deviceHasColor(capability) || isFixedKelvin(capability) || color.Saturation <= 0.5) {
		mapped.H = 0
		mapped.S = 0
		mapped.Kelvin = kelvin
	}
	return mapped
}

func mapLifxHSBK(color packets.LightHsbk, capability DeviceCapability) HSLColor {
	c := lifxdevice.NewColor(color)
	return mapLifxColor(c, capability)
}

func averageHSLColor(colors []HSLColor) HSLColor {
	var hueX, hueY, saturation, lightness float64
	var kelvinSum, kelvinCount int
	for _, color := range colors {
		radians := color.H * math.Pi / 180
		weight := math.Max(color.S, 0.01)
		hueX += math.Cos(radians) * weight
		hueY += math.Sin(radians) * weight
		saturation += color.S
		lightness += color.L
		if color.Kelvin > 0 {
			kelvinSum += color.Kelvin
			kelvinCount++
		}
	}

	count := float64(len(colors))
	average := HSLColor{
		H: averageHue(hueX, hueY),
		S: saturation / count,
		L: lightness / count,
	}
	if kelvinCount > 0 {
		average.Kelvin = int(math.Round(float64(kelvinSum) / float64(kelvinCount)))
		if average.S <= 0.005 {
			average.H = 0
			average.S = 0
		}
	}
	return average
}

func averageHue(x, y float64) float64 {
	if math.Abs(x) < 1e-9 && math.Abs(y) < 1e-9 {
		return 0
	}
	hue := math.Atan2(y, x) * 180 / math.Pi
	if hue < 0 {
		hue += 360
	}
	return hue
}

func mapLifxCapability(props lifxdevice.ColorProperties) DeviceCapability {
	minKelvin := props.TemperatureRange.Min
	maxKelvin := props.TemperatureRange.Max
	if minKelvin <= 0 {
		minKelvin = 1500
	}
	if maxKelvin <= 0 {
		maxKelvin = 9000
	}
	return DeviceCapability{
		HasColor:  props.HasColor,
		KelvinMin: minKelvin,
		KelvinMax: maxKelvin,
	}
}

func parseDeviceSerial(device Device) (lifxdevice.Serial, error) {
	serial := device.Serial
	serial = strings.ReplaceAll(serial, ":", "")
	return lifxdevice.SerialFromHex(serial)
}

func sendDeviceState(ctx context.Context, ctrl lifxController, serial lifxdevice.Serial, device Device, direct bool, intent DeviceCommandIntent, current *Device) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	logLifxRequest(serial, device, direct, intent, current)

	if device.Kind == DeviceKindSwitch {
		return sendSwitchDeviceState(ctx, ctrl, serial, device, intent)
	}

	if intent == DeviceCommandPower {
		if device.On {
			msg := messages.SetPowerOn()
			logLifxSend(serial, device, "power-on", msg)
			if err := ctrl.Send(serial, msg); err != nil {
				return fmt.Errorf("set power on: %w", err)
			}
			return nil
		}
		msg := messages.SetPowerOff()
		logLifxSend(serial, device, "power-off", msg)
		if err := ctrl.Send(serial, msg); err != nil {
			return fmt.Errorf("set power off: %w", err)
		}
		return nil
	}

	if !device.On {
		if current != nil && current.On {
			msg := messages.SetPowerOff()
			logLifxSend(serial, device, "power-off", msg)
			if err := ctrl.Send(serial, msg); err != nil {
				return fmt.Errorf("set power off: %w", err)
			}
		}
		return nil
	}

	needsPowerOn := current != nil && !current.On
	if intent == DeviceCommandBrightness {
		msg := brightnessOnlyMessage(device)
		logLifxSend(serial, device, "brightness-only", msg)
		if err := ctrl.Send(serial, msg); err != nil {
			return fmt.Errorf("set brightness: %w", err)
		}
		if needsPowerOn {
			powerMsg := messages.SetPowerOn()
			logLifxSend(serial, device, "power-on", powerMsg)
			if err := ctrl.Send(serial, powerMsg); err != nil {
				return fmt.Errorf("set power on: %w", err)
			}
		}
		return nil
	}

	for index, msg := range deviceStateMessages(device, direct) {
		if msg == nil {
			continue
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		logLifxSend(serial, device, fmt.Sprintf("state-%d", index+1), msg)
		if err := ctrl.Send(serial, msg); err != nil {
			return fmt.Errorf("set %s state: %w", device.Kind, err)
		}
	}
	if needsPowerOn {
		powerMsg := messages.SetPowerOn()
		logLifxSend(serial, device, "power-on", powerMsg)
		if err := ctrl.Send(serial, powerMsg); err != nil {
			return fmt.Errorf("set power on: %w", err)
		}
	}

	return nil
}

func normalizeDeviceCommandIntent(intent DeviceCommandIntent, device Device) DeviceCommandIntent {
	switch intent {
	case DeviceCommandPower, DeviceCommandBrightness, DeviceCommandColor, DeviceCommandZones, DeviceCommandMatrix, DeviceCommandRelayPower, DeviceCommandButton:
		return intent
	}
	switch device.Kind {
	case DeviceKindMultizone:
		return DeviceCommandZones
	case DeviceKindMatrix:
		return DeviceCommandMatrix
	default:
		return DeviceCommandColor
	}
}

func sendSwitchDeviceState(ctx context.Context, ctrl lifxController, serial lifxdevice.Serial, device Device, intent DeviceCommandIntent) error {
	msgs := switchDeviceMessagesForIntent(device, intent)
	for index, msg := range msgs {
		if msg == nil {
			continue
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		logLifxSend(serial, device, fmt.Sprintf("switch-%d", index+1), msg)
		if err := ctrl.Send(serial, msg); err != nil {
			return fmt.Errorf("set switch state: %w", err)
		}
	}
	return nil
}

func switchDeviceMessagesForIntent(device Device, intent DeviceCommandIntent) []*protocol.Message {
	switch intent {
	case DeviceCommandRelayPower:
		return relayPowerMessages(device.Relays)
	case DeviceCommandButton:
		if device.ButtonConfig == nil {
			return nil
		}
		return []*protocol.Message{messages.SetButtonConfig(uint16(clampInt(device.ButtonConfig.HapticDurationMS, 0, 500)), hslToLifxColor(device.ButtonConfig.BacklightOnColor), hslToLifxColor(device.ButtonConfig.BacklightOffColor))}
	default:
		return switchDeviceMessages(device)
	}
}

func switchDeviceMessages(device Device) []*protocol.Message {
	msgs := relayPowerMessages(device.Relays)
	if device.ButtonConfig != nil {
		msgs = append(msgs, messages.SetButtonConfig(uint16(clampInt(device.ButtonConfig.HapticDurationMS, 0, 500)), hslToLifxColor(device.ButtonConfig.BacklightOnColor), hslToLifxColor(device.ButtonConfig.BacklightOffColor)))
	}
	return msgs
}

func relayPowerMessages(relays []Relay) []*protocol.Message {
	msgs := make([]*protocol.Message, 0, len(relays))
	for _, relay := range relays {
		msgs = append(msgs, messages.SetRelayPower(relay.Index, relay.On))
	}
	return msgs
}

func hslToLifxColor(color HSLColor) lifxdevice.Color {
	return lifxdevice.Color{
		Hue:        color.H,
		Saturation: clamp(color.S, 0, 1) * 100,
		Brightness: clamp(color.L, 0, 1) * 100,
		Kelvin:     uint16(clampInt(color.Kelvin, 1500, 9000)),
	}
}

func clampInt(value int, minValue int, maxValue int) int {
	if value < minValue {
		return minValue
	}
	if value > maxValue {
		return maxValue
	}
	return value
}

func absInt(value int) int {
	if value < 0 {
		return -value
	}
	return value
}

func deviceStateMessages(device Device, direct bool) []*protocol.Message {
	if device.Kind == DeviceKindSwitch {
		return switchDeviceMessages(device)
	}

	switch device.Kind {
	case DeviceKindSingle:
		return []*protocol.Message{singleZoneColorMessage(device)}
	case DeviceKindMultizone:
		if direct {
			return []*protocol.Message{singleZoneColorMessage(device)}
		}
		return messages.SetMultizoneExtendedColors(0, hslColorsToHSBK(device.Zones, device.Brightness, device.Kelvin, device.Capability), defaultColorTransitionDuration)
	case DeviceKindMatrix:
		if direct {
			return []*protocol.Message{singleZoneColorMessage(device)}
		}
		msgs := make([]*protocol.Message, 0)
		for _, matrix := range device.Chain {
			width := matrixWidth(matrix)
			if width <= 0 {
				continue
			}
			msgs = append(msgs, messages.SetMatrixColorsFromSlice(
				matrix.ID,
				len(device.Chain),
				width,
				rotateMatrixForOrientation(matrix, hslColorsToHSBK(matrix.Pixels, device.Brightness, device.Kelvin, device.Capability)),
				defaultColorTransitionDuration,
			)...)
		}
		return msgs
	default:
		return nil
	}
}

func rotateMatrixForOrientation(matrix Matrix, colors []packets.LightHsbk) []packets.LightHsbk {
	width := matrixWidth(matrix)
	height := matrixHeight(matrix)
	if width <= 0 || height <= 0 {
		return colors
	}
	return lifxdevice.ReorientMatrix(width, height, lifxdevice.Orientation(matrix.Orientation), colors)
}

func matrixWidth(matrix Matrix) int {
	if matrix.SendWidth > 0 {
		return matrix.SendWidth
	}
	for _, row := range matrix.Rows {
		if row.Cols > 0 {
			return row.Cols
		}
	}
	return int(matrix.W)
}

func matrixHeight(matrix Matrix) int {
	if matrix.SendWidth > 0 && matrix.SendWidth <= len(matrix.Pixels) {
		return len(matrix.Pixels) / matrix.SendWidth
	}
	if len(matrix.Rows) > 0 {
		return len(matrix.Rows)
	}
	return int(matrix.H)
}

func hslColorsToHSBK(colors []HSLColor, brightness float64, kelvin int, capability DeviceCapability) []packets.LightHsbk {
	mapped := make([]packets.LightHsbk, len(colors))
	for i, color := range colors {
		mapped[i] = hslColorToHSBK(color, brightness, kelvin, capability)
	}
	return mapped
}

func hslColorToHSBK(color HSLColor, brightness float64, kelvin int, capability DeviceCapability) packets.LightHsbk {
	brightness = color.L
	if kelvin <= 0 {
		kelvin = 3500
	}
	if color.Kelvin > 0 {
		kelvin = color.Kelvin
		color.S = 0
	} else if !deviceHasColor(capability) {
		color.H = 0
		color.S = 0
	}
	kelvin = clampKelvin(kelvin, deviceKelvinMin(capability), deviceKelvinMax(capability))
	return lifxdevice.Color{
		Hue:        clamp(color.H, 0, 360),
		Saturation: clamp(color.S, 0, 1) * 100,
		Brightness: clamp(brightness, 0, 1) * 100,
		Kelvin:     uint16(clamp(float64(kelvin), 1500, 9000)),
	}.ToDeviceColor()
}

func singleZoneColorMessage(device Device) *protocol.Message {
	brightness := clamp(device.Brightness, 0, 1) * 100
	var hue, saturation *float64
	if device.Color != nil && deviceHasColor(device.Capability) {
		s := clamp(device.Color.S, 0, 1) * 100
		saturation = &s
		if device.Color.Kelvin == 0 && device.Color.S > 0 {
			h := clamp(device.Color.H, 0, 360)
			hue = &h
		}
	}
	var kelvin *uint16
	if device.Kelvin > 0 || device.Color != nil && device.Color.Kelvin > 0 {
		value := device.Kelvin
		if device.Color != nil && device.Color.Kelvin > 0 {
			value = device.Color.Kelvin
		}
		k := uint16(clampKelvin(value, deviceKelvinMin(device.Capability), deviceKelvinMax(device.Capability)))
		kelvin = &k
	}
	return messages.SetColor(hue, saturation, &brightness, kelvin, defaultColorTransitionDuration, 0)
}

func brightnessOnlyMessage(device Device) *protocol.Message {
	brightness := clamp(device.Brightness, 0, 1) * 100
	return messages.SetColor(nil, nil, &brightness, nil, defaultColorTransitionDuration, 0)
}

func logLifxRequest(serial lifxdevice.Serial, device Device, direct bool, intent DeviceCommandIntent, current *Device) {
	if !lifxDebugEnabled() {
		return
	}
	currentPower := "unknown"
	if current != nil {
		currentPower = fmt.Sprintf("%v", current.On)
	}
	log.Printf(
		"hikari: lifx request serial=%s name=%q kind=%s intent=%s on=%v currentOn=%s brightness=%.2f direct=%v",
		serial.String(),
		device.Name,
		device.Kind,
		intent,
		device.On,
		currentPower,
		device.Brightness,
		direct,
	)
}

func logLifxSend(serial lifxdevice.Serial, device Device, action string, msg *protocol.Message) {
	if !lifxDebugEnabled() {
		return
	}
	payload := "<nil>"
	if msg != nil && msg.Payload != nil {
		payload = fmt.Sprintf("%T", msg.Payload)
	}
	log.Printf("hikari: lifx send serial=%s kind=%s action=%s payload=%s", serial.String(), device.Kind, action, payload)
}

func lifxDebugEnabled() bool {
	level := strings.ToLower(os.Getenv("HIKARI_LOG_LEVEL"))
	return level == "debug" || level == "trace"
}

func deviceKelvinMin(capability DeviceCapability) int {
	if capability.KelvinMin > 0 {
		return capability.KelvinMin
	}
	return 1500
}

func deviceHasColor(capability DeviceCapability) bool {
	if capability.HasColor {
		return true
	}
	return capability.KelvinMin == 0 && capability.KelvinMax == 0
}

func isFixedKelvin(capability DeviceCapability) bool {
	return capability.KelvinMin > 0 && capability.KelvinMin == capability.KelvinMax
}

func deviceKelvinMax(capability DeviceCapability) int {
	if capability.KelvinMax > 0 {
		return capability.KelvinMax
	}
	return 9000
}

func clampKelvin(kelvin, minKelvin, maxKelvin int) int {
	if maxKelvin < minKelvin {
		maxKelvin = minKelvin
	}
	if kelvin < minKelvin {
		return minKelvin
	}
	if kelvin > maxKelvin {
		return maxKelvin
	}
	return kelvin
}

func clamp(value, minValue, maxValue float64) float64 {
	if value < minValue {
		return minValue
	}
	if value > maxValue {
		return maxValue
	}
	return value
}

func mapDeviceKind(d lifxdevice.Device) DeviceKind {
	if d.Type == lifxdevice.DeviceTypeSwitch {
		return DeviceKindSwitch
	}
	switch d.LightType.String() {
	case "multi_zone":
		return DeviceKindMultizone
	case "matrix":
		return DeviceKindMatrix
	default:
		return DeviceKindSingle
	}
}

func idOrUnknown(value, fallback string) string {
	if value == "" {
		return fallback
	}
	return value
}

func nameOrUnknown(value, fallback string) string {
	if value == "" {
		return fallback
	}
	return value
}
