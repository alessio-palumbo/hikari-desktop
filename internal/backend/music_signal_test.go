package backend

import (
	"context"
	"math"
	"testing"
	"time"

	maestrolive "github.com/alessio-palumbo/lifx-maestro/live"
)

func TestMusicSignalCoalescesObservationsWithoutPollingAutomation(t *testing.T) {
	feed := &musicSignalFeed{runID: "run"}
	if feed.snapshot() != nil {
		t.Fatal("signal exists before an observation")
	}
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	ticks := make(chan time.Time)
	points := make(chan MusicSignal, 8)
	done := make(chan struct{})
	go func() { defer close(done); feed.publish(ctx, ticks, func(point MusicSignal) { points <- point }) }()
	read := func() MusicSignal {
		t.Helper()
		select {
		case point := <-points:
			return point
		case <-time.After(time.Second):
			t.Fatal("missing signal publication")
			return MusicSignal{}
		}
	}
	feed.Observe(maestrolive.State{At: 100 * time.Millisecond, InputDB: -30, Low: .3})
	feed.Observe(maestrolive.State{At: 200 * time.Millisecond, InputDB: -20, Low: .6, Mid: .4, High: .2})
	select {
	case <-points:
		t.Fatal("observation published synchronously")
	default:
	}
	ticks <- time.Time{}
	point := read()
	if point.RunID != "run" || point.AtMS != 200 || point.InputDB != -20 || point.Low != .6 {
		t.Fatalf("point=%+v", point)
	}
	copy := feed.snapshot()
	copy.InputDB = -70
	if feed.snapshot().InputDB != -20 {
		t.Fatal("snapshot aliases stored state")
	}
	// An unchanged snapshot must not be republished on every timer tick.
	ticks <- time.Time{}
	feed.Observe(maestrolive.State{At: 300 * time.Millisecond, InputDB: -15})
	ticks <- time.Time{}
	if point := read(); point.AtMS != 300 {
		t.Fatalf("duplicate/stale publication=%+v", point)
	}
	cancel()
	awaitMusic(t, done)
	feed.Observe(maestrolive.State{At: 400 * time.Millisecond})
	select {
	case <-points:
		t.Fatal("publication after shutdown")
	default:
	}
}

func TestMusicSignalSanitizesNonJSONNumbersAndNormalizesBands(t *testing.T) {
	feed := &musicSignalFeed{runID: "safe"}
	feed.Observe(maestrolive.State{InputDB: math.Inf(-1), Low: math.NaN(), Mid: 2, High: -.2, Energy: math.NaN()})
	point := feed.snapshot()
	if point.InputDB != -96 || point.Low != 0 || point.Mid != 1 || point.High != 0 || point.Energy != 0 {
		t.Fatalf("unsafe signal=%+v", point)
	}
	s := NewMusicRuntimeService(NewMockTransport())
	if s.Signal() != nil {
		t.Fatal("passive signal getter initialized capture")
	}
}
