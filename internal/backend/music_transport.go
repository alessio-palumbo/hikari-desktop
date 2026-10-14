package backend

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"slices"
	"strings"
	"time"

	maestrolive "github.com/alessio-palumbo/lifx-maestro/live"
	lifxcontroller "github.com/alessio-palumbo/lifxlan-go/pkg/controller"
	lifxdevice "github.com/alessio-palumbo/lifxlan-go/pkg/device"
	lifxeffects "github.com/alessio-palumbo/lifxlan-go/pkg/effects"
	"github.com/alessio-palumbo/lifxlan-go/pkg/messages"
	"github.com/alessio-palumbo/lifxlan-go/pkg/protocol"
)

const maxMusicDevices = 64

// Audio is supplied by the caller. This layer never starts a runtime, discovers
// another controller or expands selectors during a run: serials are explicit.
type MusicInput struct {
	Serials  []string
	Source   maestrolive.AudioSource
	Analyzer maestrolive.Analyzer
	Style    string
	Dynamics string
	Tracker  maestrolive.TrackerConfig
	Observer maestrolive.Observer
}

func (t *LifxTransport) musicService() *MusicService {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.music == nil {
		t.music = NewMusicService()
	}
	return t.music
}

func (t *LifxTransport) MusicStatus() MusicStatus {
	t.mu.RLock()
	service := t.music
	t.mu.RUnlock()
	if service == nil {
		return MusicStatus{State: "idle", Serials: []string{}}
	}
	return service.Status()
}

func (t *LifxTransport) SetMusicStatusObserver(observer func(MusicStatus)) {
	t.musicService().SetStatusObserver(observer)
}

func (t *LifxTransport) MusicOwnsSerial(serial string) bool {
	status := t.MusicStatus()
	return (status.State == "running" || status.State == "stopping") && slices.Contains(status.Serials, serial)
}

func (t *LifxTransport) StopMusic(ctx context.Context) error {
	t.mu.RLock()
	service := t.music
	t.mu.RUnlock()
	if service == nil {
		return nil
	}
	return service.Stop(ctx)
}

func (t *LifxTransport) stopMusicForSerial(ctx context.Context, serial string) error {
	if !t.MusicOwnsSerial(serial) {
		return nil
	}
	return t.StopMusic(ctx)
}

type musicTarget struct {
	serial   lifxdevice.Serial
	device   lifxdevice.Device
	previous Device
	surface  lifxdevice.Surface
	snapshot lifxdevice.StateSnapshot
	renderer lifxeffects.Renderer
	touched  bool
}

// RunMusic blocks through capture, Live playback and restoration. Accepted runs
// own Analyzer.Close. Manual writes to any target stop the complete session
// before taking a device lock, while unrelated devices remain controllable.
func (t *LifxTransport) RunMusic(ctx context.Context, input MusicInput) error {
	ctx, cancelRun := context.WithCancel(ctx)
	defer cancelRun()
	ctrl, err := t.requireController()
	if err != nil {
		return err
	}
	if len(input.Serials) == 0 || len(input.Serials) > maxMusicDevices {
		return errors.New("music requires 1..64 lights")
	}
	seen := make(map[lifxdevice.Serial]bool)
	targets := make([]*musicTarget, 0, len(input.Serials))
	for _, value := range input.Serials {
		serial, err := parseDeviceSerial(Device{Serial: value})
		if err != nil || serial.IsNil() {
			return fmt.Errorf("invalid music serial %q", value)
		}
		if seen[serial] {
			continue
		}
		seen[serial] = true
		d, ok := ctrl.GetDevice(serial)
		if !ok || (d.Type != lifxdevice.DeviceTypeLight && d.Type != lifxdevice.DeviceTypeHybrid) {
			return fmt.Errorf("music target %s is not an available light", serial)
		}
		targets = append(targets, &musicTarget{serial: serial, device: d, surface: lifxdevice.SurfaceFromDevice(d)})
	}
	slices.SortFunc(targets, func(a, b *musicTarget) int { return strings.Compare(a.serial.String(), b.serial.String()) })
	var releases []func()
	var dispatcher *musicFrameDispatcher
	bySerial := make(map[string]*musicTarget)
	var targetIDs []string
	for _, target := range targets {
		id := target.serial.String()
		bySerial[id] = target
		targetIDs = append(targetIDs, id)
	}
	render := func(ctx context.Context, output maestrolive.TargetFrame) error {
		if err := ctx.Err(); err != nil {
			return err
		}
		target := bySerial[output.Target]
		first := !target.touched
		target.touched = true // Failed sends may still have partially changed a chain.
		if err := target.renderer.RenderFrame(ctx, constrainMusicFrame(output.Frame, target.previous.Capability)); err != nil {
			return err
		}
		if first && !devicePoweredOn(target.previous) {
			if err := ctx.Err(); err != nil {
				return err
			}
			current, found := ctrl.GetDevice(target.serial)
			if !found || deviceEndpointChanged(target.device, current) || t.validateEndpointRequest(target.previous) != nil {
				return lifxcontroller.ErrEndpointChanged
			}
			msg := messages.SetPowerOn()
			logLifxSend(target.serial, target.previous, "music-power-on", msg)
			return ctrl.Send(target.serial, msg)
		}
		return nil
	}
	prepare := func(ctx context.Context) error {
		for _, target := range targets {
			unlock, err := t.lockDeviceCommand(ctx, target.serial)
			if err != nil {
				return err
			}
			releases = append(releases, unlock)
			if t.currentController() != ctrl {
				return lifxcontroller.ErrEndpointChanged
			}
			if err := t.refreshEndpointState(ctx, ctrl, target.serial); err != nil {
				return err
			}
			d, ok := ctrl.GetDevice(target.serial)
			if !ok || deviceEndpointChanged(target.device, d) || !reflect.DeepEqual(target.surface, lifxdevice.SurfaceFromDevice(d)) {
				return fmt.Errorf("music target %s changed; start again with fresh state: %w", target.serial, lifxcontroller.ErrEndpointChanged)
			}
			t.mu.RLock()
			_, appRunning := t.effects[target.serial.String()]
			firmware, tracked := t.firmware[target.serial.String()]
			t.mu.RUnlock()
			observed := mapLifxFirmwareEffect(d)
			if appRunning || tracked && !firmware.stopping || observed != nil && observed.Running {
				return fmt.Errorf("stop the effect on %s before starting music", target.serial)
			}
			mapped := mapLifxDevice(d, mapLifxGroupID(d, mapLifxLocationID(d)))
			if cached := t.cachedDevice(target.serial.String()); cached != nil {
				mapped = *cached
			}
			t.mu.RLock()
			mapped.EndpointRevision = t.endpointRevision[target.serial.String()]
			t.mu.RUnlock()
			if !mapped.Online {
				return fmt.Errorf("music target %s is offline", target.serial)
			}
			target.previous = mapped
			target.snapshot, err = captureAppEffectSnapshot(ctx, ctrl, target.serial, mapped)
			if err != nil {
				return fmt.Errorf("capture music state for %s: %w", target.serial, err)
			}
			target.renderer = newMusicRenderer(d, func(msg *protocol.Message) error {
				if err := ctx.Err(); err != nil {
					return err
				}
				if err := t.validateEndpointRequest(target.previous); err != nil {
					return err
				}
				current, found := ctrl.GetDevice(target.serial)
				if t.currentController() != ctrl || !found || deviceEndpointChanged(target.device, current) {
					return lifxcontroller.ErrEndpointChanged
				}
				logLifxSend(target.serial, target.previous, "music-frame", msg)
				return ctrl.Send(target.serial, msg)
			})
		}
		dispatcher = newMusicFrameDispatcher(ctx, targetIDs, render, cancelRun)
		return nil
	}
	cleanup := func(ctx context.Context) error {
		defer func() {
			for i := len(releases) - 1; i >= 0; i-- {
				releases[i]()
			}
		}()
		var result error
		if dispatcher != nil {
			result = dispatcher.Close()
		}
		for _, target := range targets {
			if !target.touched {
				continue
			}
			current, found := ctrl.GetDevice(target.serial)
			if t.currentController() != ctrl || !found || deviceEndpointChanged(target.device, current) || t.validateEndpointRequest(target.previous) != nil {
				result = errors.Join(result, lifxcontroller.ErrEndpointChanged)
				continue
			}
			if err := restoreAppEffectState(ctx, ctrl, runningAppEffect{previous: target.previous, snapshot: target.snapshot}); err != nil {
				result = errors.Join(result, fmt.Errorf("restore music target %s: %w", target.serial, err))
				continue
			}
			t.storeCachedDevice(target.previous)
			t.storeRestoreDevice(target.previous, time.Now().Add(3*time.Second))
		}
		return result
	}
	config := maestrolive.Config{Source: input.Source, Analyzer: input.Analyzer, Style: input.Style, Dynamics: input.Dynamics, Tracker: input.Tracker, Observer: input.Observer}
	for _, target := range targets {
		id := target.serial.String()
		config.Targets = append(config.Targets, maestrolive.Target{ID: id, Surface: target.surface})
	}
	config.Sink = maestrolive.SinkFunc(func(ctx context.Context, batch maestrolive.FrameBatch) error {
		return dispatcher.Write(ctx, batch)
	})
	return t.musicService().runWithLifecycle(ctx, config, prepare, cleanup)
}

func constrainMusicFrame(frame lifxeffects.Frame, capability DeviceCapability) lifxeffects.Frame {
	frame.Colors = append([]lifxeffects.Color(nil), frame.Colors...)
	for i := range frame.Colors {
		color := &frame.Colors[i]
		if !capability.HasColor {
			color.Saturation = 0
		}
		color.Kelvin = uint16(clampKelvin(int(color.Kelvin), deviceKelvinMin(capability), deviceKelvinMax(capability)))
	}
	return frame
}

// A manual request can contain animated colours from the last UI snapshot.
// Keep restored fields that the command does not intend to change.
func rebaseMusicDeviceChange(request, restored Device, intent DeviceCommandIntent) Device {
	if intent != DeviceCommandPower && intent != DeviceCommandBrightness && intent != DeviceCommandColor {
		return request
	}
	next := restored
	next.On = request.On
	if intent == DeviceCommandPower {
		return next
	}
	if intent == DeviceCommandBrightness {
		next.Brightness = request.Brightness
	}
	changeColor := func(color HSLColor) HSLColor {
		if intent == DeviceCommandBrightness {
			color.L = request.Brightness
		}
		if intent == DeviceCommandColor && request.Color != nil {
			color.H, color.S, color.Kelvin = request.Color.H, request.Color.S, request.Color.Kelvin
		}
		return color
	}
	if next.Color != nil {
		color := changeColor(*next.Color)
		next.Color = &color
	}
	if intent == DeviceCommandColor {
		next.Kelvin = request.Kelvin
		if request.Color == nil {
			next.Color = nil
		}
	}
	next.Zones = slices.Clone(restored.Zones)
	for i := range next.Zones {
		next.Zones[i] = changeColor(next.Zones[i])
	}
	next.Chain = slices.Clone(restored.Chain)
	for i := range next.Chain {
		next.Chain[i].Pixels = slices.Clone(restored.Chain[i].Pixels)
		for j := range next.Chain[i].Pixels {
			next.Chain[i].Pixels[j] = changeColor(next.Chain[i].Pixels[j])
		}
	}
	return next
}
