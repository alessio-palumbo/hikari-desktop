package backend

import (
	"time"

	lifxdevice "github.com/alessio-palumbo/lifxlan-go/pkg/device"
	lifxeffects "github.com/alessio-palumbo/lifxlan-go/pkg/effects"
)

func deviceWithEffectSnapshot(d lifxdevice.Device, state lifxdevice.DeviceStateSnapshot) lifxdevice.Device {
	d.Color = state.Color
	d.PoweredOn = state.PoweredOn
	d.MultizoneProperties.Zones = lifxdevice.CloneHSBKs(state.Zones)
	d.MatrixProperties.ChainZones = lifxdevice.CloneMatrixChains(state.MatrixChains)
	d.MatrixProperties.Width = state.MatrixWidth
	return d
}

func newPatternBreathe(req StartDeviceEffectRequest, d lifxdevice.Device) (lifxeffects.Effect, error) {
	frame, err := lifxeffects.FrameFromDeviceState(d, defaultAppEffectStep)
	if err != nil {
		return nil, err
	}
	return lifxeffects.NewPatternBreathe(lifxeffects.PatternBreatheConfig{
		InitialFrame:  frame,
		Period:        appEffectPeriod(req.SpeedMS, 4*time.Second),
		MinMultiplier: 0.1,
		MaxMultiplier: 1,
	})
}
