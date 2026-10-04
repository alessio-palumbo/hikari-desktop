package backend

import (
	"context"
	"errors"
	"testing"

	lifxcontroller "github.com/alessio-palumbo/lifxlan-go/pkg/controller"
	lifxdevice "github.com/alessio-palumbo/lifxlan-go/pkg/device"
	"github.com/alessio-palumbo/lifxlan-go/pkg/protocol"
	"github.com/alessio-palumbo/lifxprotocol-go/gen/protocol/packets"
)

type failingEffectFrameController struct {
	*fakeLifxController
	failure error
}

func (c *failingEffectFrameController) Send(serial lifxdevice.Serial, msg *protocol.Message) error {
	if _, ok := msg.Payload.(*packets.TileSet64); ok {
		return c.failure
	}
	return c.fakeLifxController.Send(serial, msg)
}

func TestFailedInitialFrameDoesNotReportAnEffectAsRunning(t *testing.T) {
	_, base, current := parameterTestTransport(t)
	current.On = true
	failure := errors.New("initial frame could not be sent")
	ctrl := &failingEffectFrameController{fakeLifxController: base, failure: failure}
	transport := newTestLifxTransport(t, ctrl)
	transport.storeCachedDevice(current)
	status, err := transport.StartDeviceEffect(context.Background(), StartDeviceEffectRequest{Device: current, Effect: DeviceEffectScanner})
	if !errors.Is(err, failure) || status.Running || status.Error == "" || len(transport.effects) != 0 {
		t.Fatalf("failed startup = %#v, %v", status, err)
	}
}

func TestFirmwareEffectStopsBeforeHikariFramesStart(t *testing.T) {
	_, base, current := parameterTestTransport(t)
	current.On = true
	current.FirmwareEffect = &FirmwareEffectState{Running: true}
	transport := newTestLifxTransport(t, base)
	transport.storeCachedDevice(current)
	if _, err := transport.StartDeviceEffect(context.Background(), StartDeviceEffectRequest{Device: current, Effect: DeviceEffectScanner}); err != nil {
		t.Fatal(err)
	}
	sends := base.sentMessages()
	if len(sends) < 2 {
		t.Fatal("handoff did not stop firmware and render a frame")
	}
	if _, ok := sends[0].msg.Payload.(*packets.TileSetEffect); !ok {
		t.Fatal("first handoff packet was not firmware effect-off")
	}
	if _, ok := sends[1].msg.Payload.(*packets.TileSet64); !ok {
		t.Fatal("effect start returned before rendering its first frame")
	}
	if _, err := transport.StopDeviceEffect(context.Background(), StopDeviceEffectRequest{Device: current}); err != nil {
		t.Fatal(err)
	}
}

func TestOldRunnerCleanupCannotRemoveReplacementWithSameEffect(t *testing.T) {
	transport := NewLifxTransportWithController(&fakeLifxController{})
	oldDone, newDone := make(chan struct{}), make(chan struct{})
	transport.storeAppEffect("d073d501a2c3", runningAppEffect{effect: DeviceEffectScanner, done: newDone})
	transport.clearAppEffect("d073d501a2c3", DeviceEffectScanner, oldDone)
	if transport.effects["d073d501a2c3"].done != newDone {
		t.Fatal("old runner removed the replacement")
	}
	transport.clearAppEffect("d073d501a2c3", DeviceEffectScanner, newDone)
	if len(transport.effects) != 0 {
		t.Fatal("current runner cleanup retained itself")
	}
}

func TestDeviceCommandLockIsCancellableAndDoesNotBlockOtherDevices(t *testing.T) {
	transport := NewLifxTransportWithController(&fakeLifxController{})
	first, _ := lifxdevice.SerialFromHex("d073d501a2c3")
	second, _ := lifxdevice.SerialFromHex("d073d501a2c4")
	unlock, err := transport.lockDeviceCommand(context.Background(), first)
	if err != nil {
		t.Fatal(err)
	}
	otherUnlock, err := transport.lockDeviceCommand(context.Background(), second)
	if err != nil {
		t.Fatal(err)
	}
	otherUnlock()
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		release, err := transport.lockDeviceCommand(ctx, first)
		if release != nil {
			release()
		}
		done <- err
	}()
	cancel()
	if err := <-done; !errors.Is(err, context.Canceled) {
		t.Fatalf("waiting command = %v", err)
	}
	unlock()
	release, err := transport.lockDeviceCommand(context.Background(), first)
	if err != nil {
		t.Fatal(err)
	}
	release()
}

func TestStopCannotOvertakeAnInFlightEffectStart(t *testing.T) {
	_, base, current := parameterTestTransport(t)
	entered, resume := make(chan struct{}), make(chan struct{})
	ctrl := &snapshotBoundaryController{fakeLifxController: base}
	ctrl.capture = func(ctx context.Context, serials []lifxdevice.Serial, opts lifxcontroller.SnapshotOptions) (lifxdevice.StateSnapshot, error) {
		close(entered)
		<-resume
		return base.CaptureStateSnapshot(ctx, serials, opts)
	}
	transport := newTestLifxTransport(t, ctrl)
	transport.storeCachedDevice(current)
	started := make(chan error, 1)
	go func() {
		_, err := transport.StartDeviceEffect(context.Background(), StartDeviceEffectRequest{Device: current, Effect: DeviceEffectScanner})
		started <- err
	}()
	<-entered
	ctx, cancel := context.WithCancel(context.Background())
	stopped := make(chan error, 1)
	go func() {
		_, err := transport.StopDeviceEffect(ctx, StopDeviceEffectRequest{Device: current})
		stopped <- err
	}()
	cancel()
	if err := <-stopped; !errors.Is(err, context.Canceled) {
		t.Fatalf("stop overtook start: %v", err)
	}
	if len(base.sentMessages()) != 0 {
		t.Fatal("waiting stop sent an effect-off packet")
	}
	close(resume)
	if err := <-started; err != nil {
		t.Fatal(err)
	}
	if _, err := transport.StopDeviceEffect(context.Background(), StopDeviceEffectRequest{Device: current}); err != nil {
		t.Fatal(err)
	}
	if len(base.restoredStateSnapshots()) != 1 {
		t.Fatal("completed effect did not restore")
	}
}
