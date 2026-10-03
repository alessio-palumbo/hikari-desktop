package backend

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"time"

	lifxdevice "github.com/alessio-palumbo/lifxlan-go/pkg/device"
)

const pingSampleCount = 5
const pingSampleTimeout = time.Second

type DeviceDiagnostics interface {
	PingDevice(context.Context, string) (DevicePingResult, error)
}

type DevicePingResult struct {
	Serial       string  `json:"serial"`
	Samples      int     `json:"samples"`
	Received     int     `json:"received"`
	Timeouts     int     `json:"timeouts"`
	MinMS        float64 `json:"minMs"`
	MedianMS     float64 `json:"medianMs"`
	MaxMS        float64 `json:"maxMs"`
	MeasuredAtMS int64   `json:"measuredAtMs"`
}

type devicePinger interface {
	Ping(context.Context, lifxdevice.Serial) (time.Duration, error)
}

func (t *LifxTransport) PingDevice(ctx context.Context, rawSerial string) (DevicePingResult, error) {
	if !t.diagnosticsMu.TryLock() {
		return DevicePingResult{}, fmt.Errorf("a device ping is already running")
	}
	defer t.diagnosticsMu.Unlock()
	serial, err := parseDeviceSerial(Device{Serial: rawSerial})
	if err != nil {
		return DevicePingResult{}, err
	}
	ctrl, err := t.requireController()
	if err != nil {
		return DevicePingResult{}, err
	}
	if _, ok := ctrl.GetDevice(serial); !ok {
		return DevicePingResult{}, fmt.Errorf("device is no longer available")
	}
	pinger, ok := ctrl.(devicePinger)
	if !ok {
		return DevicePingResult{}, fmt.Errorf("device ping is unavailable")
	}
	return collectDevicePing(ctx, rawSerial, func(sampleCtx context.Context) (time.Duration, error) {
		return pinger.Ping(sampleCtx, serial)
	})
}

// Deadlines count as unanswered samples; lifecycle cancellation or transport
// errors abort the run rather than disguising them as network timeouts.
func collectDevicePing(ctx context.Context, serial string, ping func(context.Context) (time.Duration, error)) (DevicePingResult, error) {
	result := DevicePingResult{Serial: serial}
	values := make([]float64, 0, pingSampleCount)
	for i := 0; i < pingSampleCount; i++ {
		if err := ctx.Err(); err != nil {
			return DevicePingResult{}, err
		}
		sampleCtx, cancel := context.WithTimeout(ctx, pingSampleTimeout)
		rtt, err := ping(sampleCtx)
		cancel()
		if ctx.Err() != nil {
			return DevicePingResult{}, ctx.Err()
		}
		result.Samples++
		if errors.Is(err, context.DeadlineExceeded) {
			result.Timeouts++
			continue
		}
		if err != nil {
			return DevicePingResult{}, fmt.Errorf("ping device: %w", err)
		}
		values = append(values, float64(rtt)/float64(time.Millisecond))
	}
	result.Received = len(values)
	result.MeasuredAtMS = time.Now().UnixMilli()
	if len(values) > 0 {
		sort.Float64s(values)
		result.MinMS, result.MaxMS = values[0], values[len(values)-1]
		result.MedianMS = values[len(values)/2]
		if len(values)%2 == 0 {
			result.MedianMS = (values[len(values)/2-1] + values[len(values)/2]) / 2
		}
	}
	return result, nil
}
