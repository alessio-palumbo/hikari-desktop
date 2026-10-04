package backend

import (
	"context"
	"errors"
	"reflect"
	"testing"
	"time"

	lifxcontroller "github.com/alessio-palumbo/lifxlan-go/pkg/controller"
	lifxdevice "github.com/alessio-palumbo/lifxlan-go/pkg/device"
	"github.com/alessio-palumbo/lifxprotocol-go/gen/protocol/packets"
)

type snapshotBoundaryController struct {
	*fakeLifxController
	capture func(context.Context, []lifxdevice.Serial, lifxcontroller.SnapshotOptions) (lifxdevice.StateSnapshot, error)
}

type restoreOptionsController struct {
	*fakeLifxController
	options lifxcontroller.RestoreOptions
	gap     time.Duration
}

func (c *restoreOptionsController) RestoreStateSnapshot(ctx context.Context, snapshot lifxdevice.StateSnapshot, opts lifxcontroller.RestoreOptions) error {
	c.options = opts
	if sends := c.sentMessages(); len(sends) > 0 {
		c.gap = time.Since(sends[len(sends)-1].at)
	}
	return c.fakeLifxController.RestoreStateSnapshot(ctx, snapshot, opts)
}

func TestAppEffectRestoreUsesImmediateTransitionOnlyForOffState(t *testing.T) {
	for _, tc := range []struct {
		name     string
		on       bool
		duration time.Duration
	}{
		{"off", false, 0},
		{"on", true, defaultColorTransitionDuration},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, base, current := parameterTestTransport(t)
			device := base.devices[0].Clone()
			device.PoweredOn = tc.on
			snapshot := lifxdevice.NewStateSnapshot([]lifxdevice.Device{device})
			ctrl := &restoreOptionsController{fakeLifxController: base}
			if err := restoreAppEffectState(context.Background(), ctrl, runningAppEffect{previous: current, snapshot: snapshot}); err != nil {
				t.Fatal(err)
			}
			if ctrl.options.Duration != tc.duration {
				t.Fatalf("restore duration = %v, want %v", ctrl.options.Duration, tc.duration)
			}
			if !tc.on {
				sends := base.sentMessages()
				if len(sends) != 1 {
					t.Fatalf("power-off sends = %d, want one before restore", len(sends))
				}
				power, ok := sends[0].msg.Payload.(*packets.DeviceSetPower)
				if !ok || power.Level != 0 || ctrl.gap < effectPowerOffSettleDelay {
					t.Fatal("restore did not wait for power-off before restoring colours")
				}
			} else if len(base.sentMessages()) != 0 {
				t.Fatal("on-state restore unexpectedly powered off")
			}
			if !reflect.DeepEqual(base.restoredStateSnapshots()[0], snapshot) {
				t.Fatal("transition selection changed the restored colours or power")
			}
		})
	}
}

func (c *snapshotBoundaryController) CaptureStateSnapshot(ctx context.Context, serials []lifxdevice.Serial, opts lifxcontroller.SnapshotOptions) (lifxdevice.StateSnapshot, error) {
	return c.capture(ctx, serials, opts)
}

func TestAppEffectCaptureFailureDoesNotChangeDevice(t *testing.T) {
	for _, on := range []bool{false, true} {
		t.Run(map[bool]string{false: "off", true: "on"}[on], func(t *testing.T) {
			_, base, current := parameterTestTransport(t)
			current.On = on
			failure := errors.New("timed out capturing restorable state: matrix coverage")
			ctrl := &snapshotBoundaryController{fakeLifxController: base, capture: func(context.Context, []lifxdevice.Serial, lifxcontroller.SnapshotOptions) (lifxdevice.StateSnapshot, error) {
				return lifxdevice.StateSnapshot{}, failure
			}}
			transport := newTestLifxTransport(t, ctrl)
			transport.storeCachedDevice(current)
			status, err := transport.StartDeviceEffect(context.Background(), StartDeviceEffectRequest{Device: current, Effect: DeviceEffectScanner})
			if !errors.Is(err, failure) || status.Running || status.Error == "" {
				t.Fatalf("capture failure not surfaced: %#v %v", status, err)
			}
			if len(base.sentMessages()) != 0 || len(base.restoredStateSnapshots()) != 0 || len(transport.effects) != 0 {
				t.Fatal("failed capture sent commands or started an effect")
			}
			if !reflect.DeepEqual(*transport.cachedDevice(current.Serial), current) {
				t.Fatal("failed capture changed cached device state")
			}
		})
	}
}

func TestAppEffectCaptureCancellationDoesNotPrimeOrPowerOn(t *testing.T) {
	_, base, current := parameterTestTransport(t)
	current.On = false
	entered := make(chan struct{})
	ctrl := &snapshotBoundaryController{fakeLifxController: base, capture: func(ctx context.Context, _ []lifxdevice.Serial, _ lifxcontroller.SnapshotOptions) (lifxdevice.StateSnapshot, error) {
		close(entered)
		<-ctx.Done()
		return lifxdevice.StateSnapshot{}, ctx.Err()
	}}
	transport := newTestLifxTransport(t, ctrl)
	transport.storeCachedDevice(current)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() {
		_, err := transport.StartDeviceEffect(ctx, StartDeviceEffectRequest{Device: current, Effect: DeviceEffectScanner})
		done <- err
	}()
	<-entered
	cancel()
	if err := <-done; !errors.Is(err, context.Canceled) {
		t.Fatalf("capture cancellation = %v", err)
	}
	if len(base.sentMessages()) != 0 || len(transport.effects) != 0 {
		t.Fatal("cancelled capture primed a frame or powered on")
	}
}

func TestAppEffectSettingsRestartReusesObservedCapture(t *testing.T) {
	_, base, current := parameterTestTransport(t)
	captures := 0
	ctrl := &snapshotBoundaryController{fakeLifxController: base, capture: func(ctx context.Context, serials []lifxdevice.Serial, opts lifxcontroller.SnapshotOptions) (lifxdevice.StateSnapshot, error) {
		captures++
		if opts.RequireFresh || len(serials) != 1 || serials[0].String() != current.Serial {
			t.Fatal("capture changed freshness or device selection")
		}
		return base.CaptureStateSnapshot(ctx, serials, opts)
	}}
	transport := newTestLifxTransport(t, ctrl)
	transport.storeCachedDevice(current)
	for _, speed := range []int{4000, 6000} {
		if _, err := transport.StartDeviceEffect(context.Background(), StartDeviceEffectRequest{Device: current, Effect: DeviceEffectScanner, SpeedMS: speed}); err != nil {
			t.Fatal(err)
		}
	}
	if captures != 1 {
		t.Fatalf("capture count = %d, want one before first start", captures)
	}
	if _, err := transport.StopDeviceEffect(context.Background(), StopDeviceEffectRequest{Device: current}); err != nil {
		t.Fatal(err)
	}
	if len(base.restoredStateSnapshots()) != 1 {
		t.Fatal("stop did not restore the original capture")
	}
}
