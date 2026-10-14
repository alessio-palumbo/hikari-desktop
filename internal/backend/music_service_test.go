package backend

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	maestrolive "github.com/alessio-palumbo/lifx-maestro/live"
	lifxdevice "github.com/alessio-palumbo/lifxlan-go/pkg/device"
)

type musicTestSource struct {
	finite bool
	exited chan struct{}
}

func (musicTestSource) Name() string { return "test audio" }
func (s musicTestSource) Run(ctx context.Context, output chan<- maestrolive.PCMChunk) error {
	if s.exited != nil {
		defer close(s.exited)
	}
	select {
	case output <- maestrolive.PCMChunk{SampleRate: 100, Samples: make([]float32, 50)}:
	case <-ctx.Done():
		return ctx.Err()
	}
	if s.finite {
		return nil
	}
	<-ctx.Done()
	return ctx.Err()
}

type musicTestAnalyzer struct {
	closes     atomic.Int32
	closeErr   error
	analyzeErr error
}

func (a *musicTestAnalyzer) Analyze(ctx context.Context, window maestrolive.PCMWindow) (maestrolive.Features, error) {
	if err := ctx.Err(); err != nil {
		return maestrolive.Features{}, err
	}
	return maestrolive.Features{At: window.End, RMSDB: -12, LowDB: -14, MidDB: -18, HighDB: -20, Onset: true, OnsetStrength: 0.8}, a.analyzeErr
}
func (a *musicTestAnalyzer) Close() error { a.closes.Add(1); return a.closeErr }

func musicTestConfig(analyzer *musicTestAnalyzer, sink maestrolive.Sink) maestrolive.Config {
	return maestrolive.Config{Source: musicTestSource{}, Analyzer: analyzer, Sink: sink,
		Targets: []maestrolive.Target{{ID: "d073d501a2c3", Surface: lifxdevice.Surface{LightType: lifxdevice.LightTypeSingleZone, Width: 1, Height: 1, Zones: 1}}}}
}

func awaitMusic(t *testing.T, channel <-chan struct{}) {
	t.Helper()
	select {
	case <-channel:
	case <-time.After(3 * time.Second):
		t.Fatal("timed out waiting for music")
	}
}

func TestMusicServiceStopQuiescesFramesBeforeClosingAnalyzer(t *testing.T) {
	s := NewMusicService()
	if s.Status().State != "idle" {
		t.Fatal("service started eagerly")
	}
	entered, release, exited, sourceExited := make(chan struct{}), make(chan struct{}), make(chan struct{}), make(chan struct{})
	a := &musicTestAnalyzer{}
	config := musicTestConfig(a, maestrolive.SinkFunc(func(ctx context.Context, frames maestrolive.FrameBatch) error {
		defer close(exited)
		if len(frames) != 1 || frames[0].Target != "d073d501a2c3" {
			t.Error("unexpected frame target")
		}
		close(entered)
		<-ctx.Done()
		<-release
		if a.closes.Load() != 0 {
			t.Error("analyzer closed before output stopped")
		}
		return ctx.Err()
	}))
	config.Source = musicTestSource{exited: sourceExited}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	defer func() {
		select {
		case <-release:
		default:
			close(release)
		}
	}()
	done := make(chan error, 1)
	go func() { done <- s.Run(ctx, config) }()
	awaitMusic(t, entered)
	status := s.Status()
	status.Serials[0] = "mutated"
	if s.Status().Serials[0] != "d073d501a2c3" {
		t.Fatal("status leaked mutable serials")
	}
	other := &musicTestAnalyzer{}
	if err := s.Run(ctx, musicTestConfig(other, config.Sink)); !errors.Is(err, ErrMusicBusy) || other.closes.Load() != 0 {
		t.Fatalf("busy=%v closes=%d", err, other.closes.Load())
	}
	stopCtx, stopCancel := context.WithCancel(context.Background())
	stopCancel()
	if err := s.Stop(stopCtx); !errors.Is(err, context.Canceled) {
		t.Fatalf("bounded stop=%v", err)
	}
	if s.Status().State != "stopping" {
		t.Fatal("still-active output reported stopped")
	}
	close(release)
	if err := s.Stop(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	awaitMusic(t, exited)
	awaitMusic(t, sourceExited)
	if a.closes.Load() != 1 || s.Status().State != "stopped" {
		t.Fatalf("closes=%d status=%+v", a.closes.Load(), s.Status())
	}
}

func TestMusicServiceFailuresAndRestart(t *testing.T) {
	s := NewMusicService()
	failure := errors.New("injected sink failure")
	a := &musicTestAnalyzer{}
	err := s.Run(context.Background(), musicTestConfig(a, maestrolive.SinkFunc(func(context.Context, maestrolive.FrameBatch) error { return failure })))
	if !errors.Is(err, failure) || s.Status().State != "error" || a.closes.Load() != 1 {
		t.Fatalf("failure=%v status=%+v", err, s.Status())
	}
	for range 2 {
		a := &musicTestAnalyzer{}
		config := musicTestConfig(a, maestrolive.SinkFunc(func(context.Context, maestrolive.FrameBatch) error { return nil }))
		config.Source = musicTestSource{finite: true}
		if err := s.Run(context.Background(), config); err != nil {
			t.Fatal(err)
		}
		if a.closes.Load() != 1 || s.Status().State != "stopped" || s.Status().Error != "" {
			t.Fatalf("restart status=%+v closes=%d", s.Status(), a.closes.Load())
		}
	}
}

func TestMusicServiceRejectsInvalidAndClosedRunsWithoutTakingResources(t *testing.T) {
	s := NewMusicService()
	a := &musicTestAnalyzer{}
	config := musicTestConfig(a, maestrolive.SinkFunc(func(context.Context, maestrolive.FrameBatch) error { return nil }))
	config.Targets = nil
	if err := s.Run(context.Background(), config); err == nil || a.closes.Load() != 0 || s.Status().State != "idle" {
		t.Fatalf("invalid=%v status=%+v", err, s.Status())
	}
	if err := s.Close(context.Background()); err != nil {
		t.Fatal(err)
	}
	config = musicTestConfig(a, config.Sink)
	if err := s.Run(context.Background(), config); !errors.Is(err, ErrMusicClosed) || a.closes.Load() != 0 {
		t.Fatalf("closed=%v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := NewMusicService().Run(ctx, config); !errors.Is(err, context.Canceled) || a.closes.Load() != 0 {
		t.Fatalf("cancelled start=%v", err)
	}
}

func TestMusicServiceReportsAnalyzerCleanupFailure(t *testing.T) {
	want := errors.New("analyzer cleanup failed")
	a := &musicTestAnalyzer{closeErr: want}
	config := musicTestConfig(a, maestrolive.SinkFunc(func(context.Context, maestrolive.FrameBatch) error { return nil }))
	config.Source = musicTestSource{finite: true}
	s := NewMusicService()
	if err := s.Run(context.Background(), config); !errors.Is(err, want) || s.Status().State != "error" {
		t.Fatalf("cleanup=%v status=%+v", err, s.Status())
	}
}

func TestMusicServiceCloseStopsActiveRun(t *testing.T) {
	s := NewMusicService()
	entered := make(chan struct{})
	a := &musicTestAnalyzer{}
	config := musicTestConfig(a, maestrolive.SinkFunc(func(ctx context.Context, _ maestrolive.FrameBatch) error {
		close(entered)
		<-ctx.Done()
		return ctx.Err()
	}))
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- s.Run(ctx, config) }()
	awaitMusic(t, entered)
	if err := s.Close(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	if a.closes.Load() != 1 {
		t.Fatal("active analyzer was not closed exactly once")
	}
	if err := s.Run(context.Background(), config); !errors.Is(err, ErrMusicClosed) {
		t.Fatalf("closed service restarted: %v", err)
	}
}

func TestMusicServiceAnalysisFailureStopsCapture(t *testing.T) {
	want := errors.New("analysis failed")
	a := &musicTestAnalyzer{analyzeErr: want}
	config := musicTestConfig(a, maestrolive.SinkFunc(func(context.Context, maestrolive.FrameBatch) error {
		t.Error("output after failed analysis")
		return nil
	}))
	exited := make(chan struct{})
	config.Source = musicTestSource{exited: exited}
	if err := NewMusicService().Run(context.Background(), config); !errors.Is(err, want) {
		t.Fatalf("analysis=%v", err)
	}
	awaitMusic(t, exited)
	if a.closes.Load() != 1 {
		t.Fatal("analysis failure leaked analyzer")
	}
}
