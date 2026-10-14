package backend

import (
	"context"
	"errors"
	"reflect"
	"sync"
	"testing"
	"time"

	maestrolive "github.com/alessio-palumbo/lifx-maestro/live"
	lifxdevice "github.com/alessio-palumbo/lifxlan-go/pkg/device"
	lifxeffects "github.com/alessio-palumbo/lifxlan-go/pkg/effects"
	"github.com/alessio-palumbo/lifxlan-go/pkg/protocol"
	"github.com/alessio-palumbo/lifxprotocol-go/gen/protocol/packets"
)

func dispatchTestFrame(target string, hue float64) maestrolive.TargetFrame {
	return maestrolive.TargetFrame{Target: target, Frame: lifxeffects.NewFrame(1, 1, 250*time.Millisecond, lifxeffects.Color{Hue: hue, Brightness: 50, Kelvin: 3500})}
}

func TestMusicDispatchTargetsProgressIndependentlyAndReplaceStaleFrames(t *testing.T) {
	entered, release := make(chan struct{}), make(chan struct{})
	var once sync.Once
	releaseSlow := func() { once.Do(func() { close(release) }) }
	fast, latest := make(chan struct{}), make(chan struct{})
	var mu sync.Mutex
	var slowHues []float64
	d := newMusicFrameDispatcher(context.Background(), []string{"slow", "fast"}, func(ctx context.Context, output maestrolive.TargetFrame) error {
		if output.Target == "fast" {
			close(fast)
			return nil
		}
		hue := output.Frame.Colors[0].Hue
		mu.Lock()
		slowHues = append(slowHues, hue)
		mu.Unlock()
		if hue == 1 {
			close(entered)
			<-release
		} else {
			close(latest)
		}
		return nil
	}, func() { t.Error("unexpected dispatch failure") })
	t.Cleanup(func() { releaseSlow(); _ = d.Close() })
	if err := d.Write(context.Background(), maestrolive.FrameBatch{dispatchTestFrame("slow", 1), dispatchTestFrame("fast", 1)}); err != nil {
		t.Fatal(err)
	}
	awaitMusic(t, entered)
	awaitMusic(t, fast)
	if err := d.Write(context.Background(), maestrolive.FrameBatch{dispatchTestFrame("slow", 2)}); err != nil {
		t.Fatal(err)
	}
	frame := dispatchTestFrame("slow", 3)
	if err := d.Write(context.Background(), maestrolive.FrameBatch{frame}); err != nil {
		t.Fatal(err)
	}
	frame.Frame.Colors[0].Hue = 99
	releaseSlow()
	awaitMusic(t, latest)
	if err := d.Close(); err != nil {
		t.Fatal(err)
	}
	mu.Lock()
	defer mu.Unlock()
	if !reflect.DeepEqual(slowHues, []float64{1, 3}) {
		t.Fatalf("stale or mutated frames dispatched: %v", slowHues)
	}
}

func TestMusicDispatchCloseWaitsForInFlightWriteAndDropsPending(t *testing.T) {
	entered, cancelled, release := make(chan struct{}), make(chan struct{}), make(chan struct{})
	var once sync.Once
	releaseWrite := func() { once.Do(func() { close(release) }) }
	d := newMusicFrameDispatcher(context.Background(), []string{"lamp"}, func(ctx context.Context, output maestrolive.TargetFrame) error {
		if output.Frame.Colors[0].Hue != 1 {
			t.Error("pending frame dispatched after cancellation")
			return nil
		}
		close(entered)
		<-ctx.Done()
		close(cancelled)
		<-release
		return ctx.Err()
	}, func() { t.Error("cancellation reported as failure") })
	t.Cleanup(func() { releaseWrite(); _ = d.Close() })
	if err := d.Write(context.Background(), maestrolive.FrameBatch{dispatchTestFrame("lamp", 1)}); err != nil {
		t.Fatal(err)
	}
	awaitMusic(t, entered)
	if err := d.Write(context.Background(), maestrolive.FrameBatch{dispatchTestFrame("lamp", 2)}); err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { done <- d.Close() }()
	awaitMusic(t, cancelled)
	select {
	case <-done:
		t.Fatal("restoration barrier returned while a frame write was in flight")
	default:
	}
	releaseWrite()
	if err := awaitMusicRun(t, done); err != nil {
		t.Fatal(err)
	}
	if err := d.Write(context.Background(), maestrolive.FrameBatch{dispatchTestFrame("lamp", 3)}); !errors.Is(err, context.Canceled) {
		t.Fatalf("write after close: %v", err)
	}
}

func TestMusicDispatchFailureCancelsRunAndSurvivesCleanup(t *testing.T) {
	failure := errors.New("send failed")
	failed := make(chan struct{})
	d := newMusicFrameDispatcher(context.Background(), []string{"lamp"}, func(context.Context, maestrolive.TargetFrame) error {
		return failure
	}, func() { close(failed) })
	t.Cleanup(func() { _ = d.Close() })
	if err := d.Write(context.Background(), maestrolive.FrameBatch{dispatchTestFrame("lamp", 1)}); err != nil {
		t.Fatal(err)
	}
	awaitMusic(t, failed)
	if err := d.Close(); !errors.Is(err, failure) {
		t.Fatalf("lost dispatch error: %v", err)
	}
}

func TestMusicDispatchValidatesEntireBatchBeforeEnqueue(t *testing.T) {
	d := newMusicFrameDispatcher(context.Background(), []string{"lamp"}, func(context.Context, maestrolive.TargetFrame) error {
		t.Error("partially dispatched invalid batch")
		return nil
	}, func() {})
	t.Cleanup(func() { _ = d.Close() })
	if err := d.Write(context.Background(), maestrolive.FrameBatch{dispatchTestFrame("lamp", 1), dispatchTestFrame("other", 1)}); err == nil {
		t.Fatal("unselected target accepted")
	}
}

func TestMusicMatrixRendererPreservesFrameTransitionsAcrossTileChain(t *testing.T) {
	d := previewTestDevice(55, 8, 8, 2)
	d.MatrixProperties.ChainOrientations = []lifxdevice.Orientation{lifxdevice.OrientationRight, lifxdevice.OrientationLeft}
	var tiles []*packets.TileSet64
	renderer := newMusicRenderer(d, func(msg *protocol.Message) error {
		if tile, ok := msg.Payload.(*packets.TileSet64); ok {
			tiles = append(tiles, tile)
		}
		return nil
	})
	surface := lifxdevice.SurfaceFromDevice(d)
	for _, duration := range []time.Duration{250 * time.Millisecond, 600 * time.Millisecond} {
		tiles = nil
		frame := lifxeffects.NewFrame(surface.Width, surface.Height, duration, lifxeffects.Color{Hue: 120, Saturation: 80, Brightness: 50, Kelvin: 3500})
		if err := renderer.RenderFrame(context.Background(), frame); err != nil {
			t.Fatal(err)
		}
		if len(tiles) != 2 {
			t.Fatalf("tile sends=%d", len(tiles))
		}
		for i, tile := range tiles {
			if tile.Duration != uint32(duration.Milliseconds()) || int(tile.TileIndex) != i || tile.Length != 1 {
				t.Fatalf("transition/chain mapping lost: %+v", tile)
			}
		}
	}
}
