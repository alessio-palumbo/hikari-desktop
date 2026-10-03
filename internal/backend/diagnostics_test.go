package backend

import (
	"context"
	"errors"
	"testing"
	"time"

	lifxdevice "github.com/alessio-palumbo/lifxlan-go/pkg/device"
)

func TestCollectDevicePing(t *testing.T) {
	for _, tc := range []struct {
		name             string
		values           []time.Duration
		timeouts         int
		min, median, max float64
	}{
		{"five replies", []time.Duration{9 * time.Millisecond, time.Millisecond, 3 * time.Millisecond, 7 * time.Millisecond, 5 * time.Millisecond}, 0, 1, 5, 9},
		{"partial replies", []time.Duration{time.Millisecond, 4 * time.Millisecond, 6 * time.Millisecond, 9 * time.Millisecond}, 1, 1, 5, 9},
		{"all timeout", nil, 5, 0, 0, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			calls := 0
			result, err := collectDevicePing(context.Background(), "test", func(ctx context.Context) (time.Duration, error) {
				if _, ok := ctx.Deadline(); !ok {
					t.Fatal("sample has no deadline")
				}
				calls++
				if calls > len(tc.values) {
					return 0, context.DeadlineExceeded
				}
				return tc.values[calls-1], nil
			})
			if err != nil {
				t.Fatal(err)
			}
			if result.Serial != "test" || result.Samples != 5 || result.Received != len(tc.values) || result.Timeouts != tc.timeouts || result.MinMS != tc.min || result.MedianMS != tc.median || result.MaxMS != tc.max || result.MeasuredAtMS == 0 {
				t.Fatalf("unexpected result: %#v", result)
			}
		})
	}
}

func TestCollectDevicePingCancellationAndErrors(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	calls := 0
	_, err := collectDevicePing(ctx, "test", func(context.Context) (time.Duration, error) { calls++; return 0, nil })
	if !errors.Is(err, context.Canceled) || calls != 0 {
		t.Fatalf("canceled run: %v, calls %d", err, calls)
	}
	ctx, cancel = context.WithCancel(context.Background())
	_, err = collectDevicePing(ctx, "test", func(context.Context) (time.Duration, error) { cancel(); return 0, context.Canceled })
	if !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	failure := errors.New("session closed")
	_, err = collectDevicePing(context.Background(), "test", func(context.Context) (time.Duration, error) { return 0, failure })
	if !errors.Is(err, failure) {
		t.Fatal(err)
	}
}

type diagnosticController struct {
	*fakeLifxController
	ping func(context.Context, lifxdevice.Serial) (time.Duration, error)
}

func (f *diagnosticController) Ping(ctx context.Context, serial lifxdevice.Serial) (time.Duration, error) {
	return f.ping(ctx, serial)
}

func TestTransportPingIsReadOnlyAndSingleFlight(t *testing.T) {
	serial, err := lifxdevice.SerialFromHex("d073d501a2c3")
	if err != nil {
		t.Fatal(err)
	}
	started, release := make(chan struct{}), make(chan struct{})
	calls := 0
	ctrl := &diagnosticController{fakeLifxController: &fakeLifxController{devices: []lifxdevice.Device{{Serial: serial}}}}
	ctrl.ping = func(ctx context.Context, got lifxdevice.Serial) (time.Duration, error) {
		if got != serial {
			t.Errorf("serial = %v", got)
		}
		calls++
		if calls == 1 {
			close(started)
			<-release
		}
		return time.Millisecond, nil
	}
	transport := NewLifxTransportWithController(ctrl)
	done := make(chan error, 1)
	go func() { _, err := transport.PingDevice(context.Background(), "d073d501a2c3"); done <- err }()
	<-started
	_, err = transport.PingDevice(context.Background(), "d073d501a2c3")
	close(release)
	if err == nil {
		t.Error("concurrent ping was accepted")
	}
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	if calls != 5 || len(ctrl.sends) != 0 {
		t.Fatalf("calls %d, state sends %d", calls, len(ctrl.sends))
	}
	if _, err := transport.PingDevice(context.Background(), "bad"); err == nil {
		t.Fatal("invalid serial accepted")
	}
	if _, err := transport.PingDevice(context.Background(), "d073d501a2c4"); err == nil {
		t.Fatal("missing device accepted")
	}
}
