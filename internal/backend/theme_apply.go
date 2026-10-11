package backend

import (
	"context"
	"fmt"
	"time"

	lifxdevice "github.com/alessio-palumbo/lifxlan-go/pkg/device"
	lifxeffects "github.com/alessio-palumbo/lifxlan-go/pkg/effects"
	"github.com/alessio-palumbo/lifxlan-go/pkg/messages"
	"github.com/alessio-palumbo/lifxlan-go/pkg/protocol"
	lifxthemes "github.com/alessio-palumbo/lifxlan-go/pkg/themes"
)

type ThemeApplier interface {
	ApplyTheme(context.Context, ThemeRequest) (ThemeApplyResult, error)
}

// Successful sends are not device acknowledgements. Individual failures are
// returned normally so Wails preserves successful targets in a partial apply.
type ThemeApplyResult struct {
	Devices  []Device            `json:"devices"`
	Failures []ThemeApplyFailure `json:"failures"`
}

type ThemeApplyFailure struct {
	Serial              string `json:"serial"`
	Error               string `json:"error"`
	StateMayHaveChanged bool   `json:"stateMayHaveChanged"`
}

type compiledThemeTarget struct {
	serial   lifxdevice.Serial
	before   Device
	after    Device
	messages []*protocol.Message
}

func (t *LifxTransport) ApplyTheme(ctx context.Context, req ThemeRequest) (ThemeApplyResult, error) {
	prepared, err := t.prepareTheme(ctx, req)
	if err != nil {
		return ThemeApplyResult{}, err
	}
	defer prepared.release()
	// Validate packetization for every device before stopping an effect or sending.
	targets := make([]compiledThemeTarget, 0, len(prepared.plan))
	for i, application := range prepared.plan {
		d := prepared.devices[i] // Both are in validated serial order.
		before := mapLifxDevice(d, mapLifxGroupID(d, mapLifxLocationID(d)))
		if cached := t.cachedDevice(d.Serial.String()); cached != nil {
			before = *cached
		}
		target, err := compileThemeTarget(ctx, d, application, before)
		if err != nil {
			return ThemeApplyResult{}, fmt.Errorf("compile theme target %s: %w", d.Serial, err)
		}
		targets = append(targets, target)
	}
	result := ThemeApplyResult{Devices: []Device{}, Failures: []ThemeApplyFailure{}}
	for _, target := range targets {
		changed, err := t.sendThemeTarget(ctx, prepared.controller, target)
		if err != nil {
			if changed {
				// An older restore guard must not hide an uncertain partial write.
				t.mu.Lock()
				delete(t.restores, target.serial.String())
				delete(t.cache, target.serial.String())
				t.mu.Unlock()
			}
			result.Failures = append(result.Failures, ThemeApplyFailure{Serial: target.serial.String(), Error: err.Error(), StateMayHaveChanged: changed})
			continue
		}
		t.storeCachedDevice(target.after)
		// Reuse the short restore guard against pre-theme LAN observations. The
		// frontend will receive the applied state before those observations settle.
		t.storeRestoreDevice(target.after, time.Now().Add(3*time.Second))
		result.Devices = append(result.Devices, target.after)
	}
	return result, nil
}

func compileThemeTarget(ctx context.Context, d lifxdevice.Device, application lifxthemes.PartApplication, before Device) (compiledThemeTarget, error) {
	surface := lifxdevice.SurfaceFromDevice(d)
	state := lifxeffects.NewPhysicalColorState(surface)
	target := compiledThemeTarget{serial: d.Serial, before: before}
	for _, msg := range application.Messages {
		if err := ctx.Err(); err != nil {
			return compiledThemeTarget{}, err
		}
		if err := applyPreviewPacket(&state, surface, msg); err != nil {
			return compiledThemeTarget{}, err
		}
		target.messages = append(target.messages, msg)
	}
	if len(target.messages) == 0 {
		return compiledThemeTarget{}, fmt.Errorf("theme produced no colour commands")
	}
	d.PoweredOn = true
	d.MultizoneProperties.Zones = state.Zones
	d.MatrixProperties.ChainZones = state.MatrixChains
	if d.LightType == lifxdevice.LightTypeSingleZone {
		d.Color = lifxdevice.NewColor(state.Zones[0])
	}
	mapped := mapLifxDevice(d, before.GroupID)
	// Preserve UI metadata, replacing only the state actually being applied.
	after := before
	after.On, after.Color, after.Brightness, after.Kelvin = true, mapped.Color, mapped.Brightness, mapped.Kelvin
	if application.Uplight != nil {
		after.Brightness = max(after.Brightness, application.Uplight.Brightness/100)
		after.On = after.Brightness > 0
	}
	after.Zones, after.Chain = mapped.Zones, mapped.Chain
	after.FirmwareEffect = &FirmwareEffectState{Running: false}
	target.after = after
	return target, nil
}

func (t *LifxTransport) sendThemeTarget(ctx context.Context, ctrl lifxController, target compiledThemeTarget) (bool, error) {
	if err := ctx.Err(); err != nil {
		return false, err
	}
	changed := t.stopAppEffect(target.serial.String()) != nil
	// This shared handoff also handles externally started firmware effects and
	// retries pending matrix starts before allowing static colour writes.
	if target.before.FirmwareEffect != nil && target.before.FirmwareEffect.Running {
		changed = true
	}
	t.mu.RLock()
	firmware, tracked := t.firmware[target.serial.String()]
	t.mu.RUnlock()
	if tracked && !firmware.stopping {
		changed = true
	}
	if err := t.stopFirmwareEffectBeforeApp(ctx, ctrl, target.serial, target.before); err != nil {
		return true, err
	}
	for _, msg := range target.messages {
		if err := ctx.Err(); err != nil {
			return changed, err
		}
		changed = true // A failed UDP send may still have reached the device.
		logLifxSend(target.serial, target.after, "theme-colours", msg)
		if err := ctrl.Send(target.serial, msg); err != nil {
			return changed, fmt.Errorf("apply theme colours: %w", err)
		}
	}
	if !devicePoweredOn(target.before) {
		if err := ctx.Err(); err != nil {
			return changed, err
		}
		msg := messages.SetPowerOn()
		logLifxSend(target.serial, target.after, "theme-power-on", msg)
		if err := ctrl.Send(target.serial, msg); err != nil {
			return changed, fmt.Errorf("power on for theme: %w", err)
		}
	}
	return changed, nil
}
