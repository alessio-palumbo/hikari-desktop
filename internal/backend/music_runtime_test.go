package backend

import (
	"context"
	"errors"
	"math"
	"sync/atomic"
	"testing"
	"time"

	maestrolive "github.com/alessio-palumbo/lifx-maestro/live"
	maestronative "github.com/alessio-palumbo/lifx-maestro/live/native"
	device "github.com/alessio-palumbo/lifxlan-go/pkg/device"
)

type runtimeTestTransport struct {
	service *MusicService
	frames  chan maestrolive.FrameBatch
}

func (t *runtimeTestTransport) UpdateMusicControls(c MusicControls) error {
	return t.service.UpdateControls(c)
}

func (t *runtimeTestTransport) MusicStatus() MusicStatus            { return t.service.Status() }
func (t *runtimeTestTransport) StopMusic(ctx context.Context) error { return t.service.Stop(ctx) }
func (t *runtimeTestTransport) SetMusicStatusObserver(fn func(MusicStatus)) {
	t.service.SetStatusObserver(fn)
}
func (t *runtimeTestTransport) RunMusic(ctx context.Context, input MusicInput) error {
	config := maestrolive.Config{Source: input.Source, Analyzer: input.Analyzer, Style: input.Style, Dynamics: input.Dynamics, Tracker: input.Tracker, MasterBrightness: input.MasterBrightness, Observer: input.Observer, Sink: maestrolive.SinkFunc(func(ctx context.Context, frames maestrolive.FrameBatch) error {
		if t.frames != nil {
			select {
			case t.frames <- frames:
			case <-ctx.Done():
				return ctx.Err()
			}
		}
		return nil
	})}
	for _, serial := range input.Serials {
		config.Targets = append(config.Targets, maestrolive.Target{ID: serial, Surface: device.Surface{LightType: device.LightTypeSingleZone, Width: 1, Height: 1, Zones: 1}})
	}
	return t.service.Run(ctx, config)
}

func testMusicRuntime(t *testing.T) (*MusicRuntimeService, *musicTestAnalyzer) {
	t.Helper()
	s := NewMusicRuntimeService(NewMockTransport())
	s.transport = &runtimeTestTransport{service: NewMusicService()}
	s.transport.SetMusicStatusObserver(s.observeTransport)
	s.captureAvailable = func() bool { return true }
	s.newSource = func(string) (maestrolive.AudioSource, error) { return musicTestSource{}, nil }
	a := &musicTestAnalyzer{}
	s.newAnalyzer = func(context.Context) (maestrolive.Analyzer, error) { return a, nil }
	s.listInputs = func() ([]MusicInputDevice, error) { return []MusicInputDevice{{ID: "test", Name: "Test input"}}, nil }
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		_ = s.Close(ctx)
	})
	return s, a
}

func musicRuntimeRequest(id string) StartMusicRequest {
	return StartMusicRequest{RunID: id, Serials: []string{"d073d501a2c3"}, Style: "warm", Dynamics: "auto", Sensitivity: "low"}
}

func awaitRuntimeStatus(t *testing.T, events <-chan MusicStatus, state string) MusicStatus {
	t.Helper()
	deadline := time.After(3 * time.Second)
	for {
		select {
		case status := <-events:
			if status.State == state {
				return status
			}
		case <-deadline:
			t.Fatalf("missing Music %s status", state)
			return MusicStatus{}
		}
	}
}

func TestMusicRuntimeStartsLazilyStopsAndClosesAnalyzerOnce(t *testing.T) {
	s, a := testMusicRuntime(t)
	var sources, analyzers, listings atomic.Int32
	s.newSource = func(string) (maestrolive.AudioSource, error) { sources.Add(1); return musicTestSource{}, nil }
	s.newAnalyzer = func(context.Context) (maestrolive.Analyzer, error) {
		analyzers.Add(1)
		return a, nil
	}
	s.listInputs = func() ([]MusicInputDevice, error) { listings.Add(1); return []MusicInputDevice{}, nil }
	if !s.Settings().Available || sources.Load() != 0 || analyzers.Load() != 0 || listings.Load() != 0 {
		t.Fatal("settings initialized audio")
	}
	if _, err := s.Inputs(); err != nil || listings.Load() != 1 {
		t.Fatal("input enumeration did not delegate")
	}
	events := make(chan MusicStatus, 32)
	s.SetStatusObserver(func(status MusicStatus) { events <- status })
	request := musicRuntimeRequest("first")
	if _, err := s.Start(context.Background(), request); err != nil {
		t.Fatal(err)
	}
	request.Serials[0] = "mutated"
	running := awaitRuntimeStatus(t, events, "running")
	if running.RunID != "first" || running.Serials[0] != "d073d501a2c3" {
		t.Fatalf("status=%+v", running)
	}
	if _, err := s.Start(context.Background(), musicRuntimeRequest("other")); !errors.Is(err, ErrMusicBusy) {
		t.Fatalf("busy=%v", err)
	}
	if err := s.Stop(context.Background(), "first"); err != nil {
		t.Fatal(err)
	}
	stopped := s.Status()
	if stopped.State != "stopped" || stopped.Revision <= running.Revision || a.closes.Load() != 1 {
		t.Fatalf("status=%+v closes=%d", stopped, a.closes.Load())
	}
}

func TestMusicRuntimeCancellationBeforeAndDuringStartup(t *testing.T) {
	s, _ := testMusicRuntime(t)
	if err := s.Stop(context.Background(), "early"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Start(context.Background(), musicRuntimeRequest("early")); !errors.Is(err, context.Canceled) {
		t.Fatalf("early cancellation=%v", err)
	}
	entered := make(chan struct{})
	s.newAnalyzer = func(ctx context.Context) (maestrolive.Analyzer, error) {
		close(entered)
		<-ctx.Done()
		return nil, ctx.Err()
	}
	if _, err := s.Start(context.Background(), musicRuntimeRequest("pending")); err != nil {
		t.Fatal(err)
	}
	awaitMusic(t, entered)
	if err := s.Stop(context.Background(), "pending"); err != nil {
		t.Fatal(err)
	}
	if s.Status().State != "stopped" {
		t.Fatalf("startup left state=%+v", s.Status())
	}
}

func TestMusicRuntimeStartupFailureIsObservableAndRecoverable(t *testing.T) {
	s, a := testMusicRuntime(t)
	failure := errors.New("analyzer unavailable")
	s.newAnalyzer = func(context.Context) (maestrolive.Analyzer, error) { return nil, failure }
	events := make(chan MusicStatus, 32)
	s.SetStatusObserver(func(status MusicStatus) { events <- status })
	if _, err := s.Start(context.Background(), musicRuntimeRequest("failed")); err != nil {
		t.Fatal(err)
	}
	status := awaitRuntimeStatus(t, events, "error")
	if status.Error != failure.Error() {
		t.Fatalf("status=%+v", status)
	}
	s.newAnalyzer = func(context.Context) (maestrolive.Analyzer, error) { return a, nil }
	if _, err := s.Start(context.Background(), musicRuntimeRequest("retry")); err != nil {
		t.Fatal(err)
	}
	awaitRuntimeStatus(t, events, "running")
	if err := s.Close(context.Background()); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Start(context.Background(), musicRuntimeRequest("closed")); !errors.Is(err, ErrMusicClosed) {
		t.Fatalf("closed=%v", err)
	}
}

type musicControlledSource struct{ chunks <-chan maestrolive.PCMChunk }

func (musicControlledSource) Name() string { return "controlled audio" }
func (s musicControlledSource) Run(ctx context.Context, output chan<- maestrolive.PCMChunk) error {
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case chunk := <-s.chunks:
			select {
			case output <- chunk:
			case <-ctx.Done():
				return ctx.Err()
			}
		}
	}
}

func TestMusicLiveControlsPreserveSessionAndUseMaestroBrightnessScaling(t *testing.T) {
	s, analyzer := testMusicRuntime(t)
	frames := make(chan maestrolive.FrameBatch, 32)
	s.transport.(*runtimeTestTransport).frames = frames
	chunks := make(chan maestrolive.PCMChunk, 1)
	s.newSource = func(string) (maestrolive.AudioSource, error) { return musicControlledSource{chunks}, nil }
	events := make(chan MusicStatus, 32)
	s.SetStatusObserver(func(status MusicStatus) { events <- status })
	req := musicRuntimeRequest("controls")
	req.Dynamics = "balanced"
	half := .5
	req.MasterBrightness = &half
	if _, err := s.Start(context.Background(), req); err != nil {
		t.Fatal(err)
	}
	awaitRuntimeStatus(t, events, "running")
	transport := s.transport.(*runtimeTestTransport)
	transport.service.mu.Lock()
	engine := transport.service.run.engine
	transport.service.mu.Unlock()
	readBrightness := func() float64 {
		t.Helper()
		select {
		case batch := <-frames:
			return batch[0].Frame.Colors[0].Brightness
		case <-time.After(3 * time.Second):
			t.Fatal("missing Music frame")
			return 0
		}
	}
	chunks <- maestrolive.PCMChunk{SampleRate: 100, Samples: make([]float32, 50)}
	initial := readBrightness()
	controls := MusicControls{RunID: req.RunID, Style: req.Style, Dynamics: req.Dynamics, MasterBrightness: .25}
	if err := s.UpdateControls(controls); err != nil {
		t.Fatal(err)
	}
	chunks <- maestrolive.PCMChunk{SampleRate: 100, Samples: make([]float32, 50)}
	dimmed := readBrightness()
	// Tracker energy evolves between windows, so compare the output ceiling
	// rather than assuming the generator produced identical brightness twice.
	if dimmed <= 0 || dimmed > 25 || dimmed >= initial {
		t.Fatalf("brightness: initial=%v dimmed=%v", initial, dimmed)
	}
	controls.Style, controls.Dynamics = "neon", "energetic"
	if err := s.UpdateControls(controls); err != nil {
		t.Fatal(err)
	}
	transport.service.mu.Lock()
	sameEngine := transport.service.run.engine == engine
	transport.service.mu.Unlock()
	if !sameEngine || analyzer.closes.Load() != 0 {
		t.Fatal("live controls restarted the session")
	}
	controls.RunID = "stale-session"
	if err := s.UpdateControls(controls); err == nil {
		t.Fatal("stale session controls accepted")
	}
	controls.RunID = req.RunID
	for _, bad := range []MusicControls{
		{Style: "missing", Dynamics: "auto", MasterBrightness: 1},
		{Style: "warm", Dynamics: "missing", MasterBrightness: 1},
		{Style: "warm", Dynamics: "auto", MasterBrightness: math.NaN()},
		{Style: "warm", Dynamics: "auto", MasterBrightness: 1.1},
		{Style: "warm", Dynamics: "auto", MasterBrightness: -.1},
	} {
		bad.RunID = req.RunID
		if err := s.UpdateControls(bad); err == nil {
			t.Fatalf("invalid controls accepted: %+v", bad)
		}
	}
	if err := s.Stop(context.Background(), req.RunID); err != nil {
		t.Fatal(err)
	}
	if err := s.UpdateControls(controls); err == nil {
		t.Fatal("controls accepted after stop")
	}
}

func TestMusicRuntimeNativeNeedsNoConfiguration(t *testing.T) {
	s, analyzer := testMusicRuntime(t)
	var natives atomic.Int32
	s.newAnalyzer = func(context.Context) (maestrolive.Analyzer, error) { natives.Add(1); return analyzer, nil }
	if settings := s.Settings(); !settings.Available || settings.Warning != "" {
		t.Fatalf("settings=%+v", settings)
	}
	if natives.Load() != 0 {
		t.Fatal("settings initialized analyzer")
	}
	events := make(chan MusicStatus, 32)
	s.SetStatusObserver(func(status MusicStatus) { events <- status })
	req := musicRuntimeRequest("native")
	if _, err := s.Start(context.Background(), req); err != nil {
		t.Fatal(err)
	}
	awaitRuntimeStatus(t, events, "running")
	if err := s.Stop(context.Background(), req.RunID); err != nil {
		t.Fatal(err)
	}
	if natives.Load() != 1 || analyzer.closes.Load() != 1 {
		t.Fatal("native analyzer lifecycle mismatch")
	}
}

func TestMusicNativeAnalyzerProcessesPCM(t *testing.T) {
	analyzer := maestronative.New()
	t.Cleanup(func() { _ = analyzer.Close() })
	const rate = 22050
	samples := make([]float32, rate/2)
	for i := range samples {
		samples[i] = float32(.3 * math.Sin(2*math.Pi*440*float64(i)/rate))
	}
	features, err := analyzer.Analyze(context.Background(), maestrolive.PCMWindow{SampleRate: rate, Samples: samples, End: 500 * time.Millisecond})
	if err != nil || math.IsNaN(features.RMSDB) || math.IsInf(features.RMSDB, 0) || features.RMSDB < -30 || features.RMSDB >= 0 {
		t.Fatalf("features=%+v err=%v", features, err)
	}
}
