package backend

import (
	"context"
	"math"
	"sync"
	"time"

	maestrolive "github.com/alessio-palumbo/lifx-maestro/live"
)

const musicSignalInterval = 100 * time.Millisecond

// MusicSignal is observational only. Bands are Maestro's normalized activity
// levels, not raw spectrum bins or output brightness.
type MusicSignal struct {
	RunID   string  `json:"runId"`
	AtMS    int64   `json:"atMs"`
	InputDB float64 `json:"inputDb"`
	Low     float64 `json:"low"`
	Mid     float64 `json:"mid"`
	High    float64 `json:"high"`
	Energy  float64 `json:"energy"`
}

type musicSignalFeed struct {
	mu     sync.Mutex
	runID  string
	latest *MusicSignal
}

func (f *musicSignalFeed) Observe(state maestrolive.State) {
	point := MusicSignal{RunID: f.runID, AtMS: state.At.Milliseconds(), InputDB: signalNumber(state.InputDB, -96, 0), Low: signalNumber(state.Low, 0, 1), Mid: signalNumber(state.Mid, 0, 1), High: signalNumber(state.High, 0, 1), Energy: signalNumber(state.Energy, 0, 1)}
	f.mu.Lock()
	f.latest = &point
	f.mu.Unlock()
}

func signalNumber(value, minimum, maximum float64) float64 {
	if math.IsNaN(value) || math.IsInf(value, 0) {
		return minimum
	}
	return min(maximum, max(minimum, value))
}

func (f *musicSignalFeed) snapshot() *MusicSignal {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.latest == nil {
		return nil
	}
	point := *f.latest
	return &point
}

// The analyzer only stores its latest observation. Event publication runs on a
// separate, bounded-rate loop so webview work cannot delay audio processing.
func (f *musicSignalFeed) publish(ctx context.Context, ticks <-chan time.Time, emit func(MusicSignal)) {
	var previous int64 = -1
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticks:
			if ctx.Err() != nil {
				return
			}
			point := f.snapshot()
			if point != nil && point.AtMS != previous {
				previous = point.AtMS
				emit(*point)
			}
		}
	}
}

func (s *MusicRuntimeService) Signal() *MusicSignal {
	s.mu.Lock()
	feed := s.signalFeed
	s.mu.Unlock()
	if feed == nil {
		return nil
	}
	return feed.snapshot()
}

func (s *MusicRuntimeService) SetSignalObserver(observer func(MusicSignal)) {
	s.mu.Lock()
	s.signalObserver = observer
	s.mu.Unlock()
}

func (s *MusicRuntimeService) emitSignal(point MusicSignal) {
	s.mu.Lock()
	observer := s.signalObserver
	s.mu.Unlock()
	if observer != nil {
		observer(point)
	}
}
