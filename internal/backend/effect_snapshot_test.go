package backend

import (
	"context"
	"errors"
	"reflect"
	"testing"

	lifxcontroller "github.com/alessio-palumbo/lifxlan-go/pkg/controller"
	lifxdevice "github.com/alessio-palumbo/lifxlan-go/pkg/device"
)

type snapshotBoundaryController struct {
	*fakeLifxController
	capture func(context.Context, []lifxdevice.Serial, lifxcontroller.SnapshotOptions) (lifxdevice.StateSnapshot, error)
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
