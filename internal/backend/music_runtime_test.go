package backend

import (
	"context"
	"errors"
	"math"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	maestrolive "github.com/alessio-palumbo/lifx-maestro/live"
	maestropython "github.com/alessio-palumbo/lifx-maestro/live/python"
	device "github.com/alessio-palumbo/lifxlan-go/pkg/device"
)

type runtimeTestTransport struct{ service *MusicService }

func (t *runtimeTestTransport) MusicStatus() MusicStatus            { return t.service.Status() }
func (t *runtimeTestTransport) StopMusic(ctx context.Context) error { return t.service.Stop(ctx) }
func (t *runtimeTestTransport) SetMusicStatusObserver(fn func(MusicStatus)) {
	t.service.SetStatusObserver(fn)
}
func (t *runtimeTestTransport) RunMusic(ctx context.Context, input MusicInput) error {
	config := maestrolive.Config{Source: input.Source, Analyzer: input.Analyzer, Style: input.Style, Dynamics: input.Dynamics, Tracker: input.Tracker, Sink: maestrolive.SinkFunc(func(context.Context, maestrolive.FrameBatch) error { return nil })}
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
	s.loadConfig = func() (maestropython.Config, error) { return maestropython.Config{BinaryPath: "/test/analyzer"}, nil }
	s.newSource = func(string) (maestrolive.AudioSource, error) { return musicTestSource{}, nil }
	a := &musicTestAnalyzer{}
	s.newAnalyzer = func(context.Context, maestropython.Config) (maestrolive.Analyzer, error) { return a, nil }
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
	s.newAnalyzer = func(context.Context, maestropython.Config) (maestrolive.Analyzer, error) {
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
	s.newAnalyzer = func(ctx context.Context, _ maestropython.Config) (maestrolive.Analyzer, error) {
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
	s.newAnalyzer = func(context.Context, maestropython.Config) (maestrolive.Analyzer, error) { return nil, failure }
	events := make(chan MusicStatus, 32)
	s.SetStatusObserver(func(status MusicStatus) { events <- status })
	if _, err := s.Start(context.Background(), musicRuntimeRequest("failed")); err != nil {
		t.Fatal(err)
	}
	status := awaitRuntimeStatus(t, events, "error")
	if status.Error != failure.Error() {
		t.Fatalf("status=%+v", status)
	}
	s.newAnalyzer = func(context.Context, maestropython.Config) (maestrolive.Analyzer, error) { return a, nil }
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

func TestMusicRuntimeConfigNeverDownloadsOrGuessesPaths(t *testing.T) {
	for _, name := range []string{"HIKARI_MAESTRO_ANALYZER", "HIKARI_MAESTRO_PYTHON", "HIKARI_MAESTRO_SCRIPT"} {
		t.Setenv(name, "")
	}
	if _, err := loadMusicRuntimeConfig(); err == nil {
		t.Fatal("missing configuration accepted")
	}
	t.Setenv("HIKARI_MAESTRO_ANALYZER", "relative/analyzer")
	if _, err := loadMusicRuntimeConfig(); err == nil {
		t.Fatal("relative runtime accepted")
	}
	path := filepath.Join(t.TempDir(), "analyzer")
	if err := os.WriteFile(path, []byte("test"), 0600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("HIKARI_MAESTRO_ANALYZER", path)
	t.Setenv("HIKARI_MAESTRO_PYTHON", "ignored-relative-path")
	config, err := loadMusicRuntimeConfig()
	if err != nil || config.BinaryPath != path || config.PythonPath != "" {
		t.Fatalf("config=%+v err=%v", config, err)
	}
}

func TestMusicRuntimeRealAnalyzer(t *testing.T) {
	if os.Getenv("HIKARI_TEST_MUSIC_RUNTIME") != "1" {
		t.Skip("set HIKARI_TEST_MUSIC_RUNTIME=1 with an explicitly configured analyzer")
	}
	config, err := loadMusicRuntimeConfig()
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	analyzer, err := maestropython.New(ctx, config)
	if err != nil {
		t.Fatal(err)
	}
	owned := &musicRuntimeAnalyzer{Analyzer: analyzer, ctx: ctx}
	t.Cleanup(func() {
		cancel()
		if err := owned.Close(); err != nil {
			t.Errorf("close analyzer: %v", err)
		}
	})
	const sampleRate = 22050
	samples := make([]float32, sampleRate/2)
	for i := range samples {
		samples[i] = float32(0.3 * math.Sin(2*math.Pi*440*float64(i)/sampleRate))
	}
	features, err := owned.Analyze(ctx, maestrolive.PCMWindow{Samples: samples, SampleRate: sampleRate, End: 500 * time.Millisecond})
	if err != nil {
		t.Fatal(err)
	}
	if math.IsNaN(features.RMSDB) || math.IsInf(features.RMSDB, 0) || features.RMSDB >= 0 || features.RMSDB < -30 {
		t.Fatalf("unexpected synthetic-audio RMS: %v", features.RMSDB)
	}
}
