package backend

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"sync"
	"sync/atomic"

	maestrolive "github.com/alessio-palumbo/lifx-maestro/live"
	maestropython "github.com/alessio-palumbo/lifx-maestro/live/python"
)

type MusicTransport interface {
	RunMusic(context.Context, MusicInput) error
	StopMusic(context.Context) error
	MusicStatus() MusicStatus
	SetMusicStatusObserver(func(MusicStatus))
}

type StartMusicRequest struct {
	RunID       string   `json:"runId"`
	Serials     []string `json:"serials"`
	InputID     string   `json:"inputId"`
	Style       string   `json:"style"`
	Dynamics    string   `json:"dynamics"`
	Sensitivity string   `json:"sensitivity"`
}

type MusicInputDevice struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	Default bool   `json:"default"`
}
type MusicSettings struct {
	Available bool     `json:"available"`
	Warning   string   `json:"warning,omitempty"`
	Styles    []string `json:"styles"`
}

var musicStyles = []string{"synthwave", "warm", "cinematic", "neon", "minimal", "cyberpunk"}

type musicRuntimeRun struct {
	id     string
	cancel context.CancelFunc
	done   chan struct{}
}

// MusicRuntimeService owns the optional runtime and asynchronous Wails-facing
// lifecycle. It never opens a microphone or spawns a process on construction.
type MusicRuntimeService struct {
	transport        MusicTransport
	newSource        func(string) (maestrolive.AudioSource, error)
	listInputs       func() ([]MusicInputDevice, error)
	newAnalyzer      func(context.Context, maestropython.Config) (maestrolive.Analyzer, error)
	loadConfig       func() (maestropython.Config, error)
	captureAvailable func() bool
	mu               sync.Mutex
	closed           bool
	run              *musicRuntimeRun
	cancelledID      string
	status           MusicStatus
	observer         func(MusicStatus)
}

func NewMusicRuntimeService(transport DeviceTransport) *MusicRuntimeService {
	t, _ := transport.(MusicTransport)
	s := &MusicRuntimeService{transport: t, newSource: newMusicMicrophone, listInputs: listMusicMicrophones, newAnalyzer: maestropython.New, loadConfig: loadMusicRuntimeConfig, captureAvailable: musicCaptureSupported, status: MusicStatus{State: "idle", Serials: []string{}}}
	if t != nil {
		t.SetMusicStatusObserver(s.observeTransport)
	}
	return s
}

func loadMusicRuntimeConfig() (maestropython.Config, error) {
	c := maestropython.Config{BinaryPath: os.Getenv("HIKARI_MAESTRO_ANALYZER"), PythonPath: os.Getenv("HIKARI_MAESTRO_PYTHON"), ScriptPath: os.Getenv("HIKARI_MAESTRO_SCRIPT")}
	paths := []string{c.BinaryPath}
	if c.BinaryPath != "" {
		c.PythonPath, c.ScriptPath = "", ""
	}
	if c.BinaryPath == "" {
		if c.PythonPath == "" || c.ScriptPath == "" {
			return c, errors.New("Music analyzer is not configured")
		}
		paths = []string{c.PythonPath, c.ScriptPath}
	}
	for _, path := range paths {
		if !filepath.IsAbs(path) {
			return c, errors.New("Music runtime paths must be absolute")
		}
		info, err := os.Stat(path)
		if err != nil || info.IsDir() {
			return c, errors.New("Music runtime file is unavailable")
		}
	}
	return c, nil
}

func (s *MusicRuntimeService) Settings() MusicSettings {
	settings := MusicSettings{Styles: slices.Clone(musicStyles)}
	if s.transport == nil {
		settings.Warning = "Music requires the LAN transport"
		return settings
	}
	if !s.captureAvailable() {
		settings.Warning = "Music capture is unavailable in this build"
		return settings
	}
	if _, err := s.loadConfig(); err != nil {
		settings.Warning = err.Error()
		return settings
	}
	settings.Available = true
	return settings
}

func (s *MusicRuntimeService) Inputs() ([]MusicInputDevice, error) {
	if settings := s.Settings(); !settings.Available {
		return []MusicInputDevice{}, errors.New(settings.Warning)
	}
	return s.listInputs()
}

func (s *MusicRuntimeService) SetStatusObserver(observer func(MusicStatus)) {
	s.mu.Lock()
	s.observer = observer
	s.mu.Unlock()
}
func (s *MusicRuntimeService) Status() MusicStatus {
	s.mu.Lock()
	defer s.mu.Unlock()
	status := s.status
	status.Serials = append([]string{}, status.Serials...)
	return status
}
func (s *MusicRuntimeService) observeTransport(status MusicStatus) {
	s.mu.Lock()
	run := s.run
	if run == nil || s.status.State == "stopping" && status.State == "running" {
		s.mu.Unlock()
		return
	}
	status.RunID = run.id
	status.Revision = s.status.Revision + 1
	status.Serials = slices.Clone(status.Serials)
	s.status = status
	observer := s.observer
	s.mu.Unlock()
	if observer != nil {
		observer(cloneMusicRuntimeStatus(status))
	}
}

func (s *MusicRuntimeService) Start(ctx context.Context, req StartMusicRequest) (MusicStatus, error) {
	req.Serials = slices.Clone(req.Serials)
	if err := ctx.Err(); err != nil {
		return s.Status(), err
	}
	if req.RunID == "" || len(req.RunID) > 64 || len(req.Serials) == 0 || len(req.Serials) > maxMusicDevices {
		return s.Status(), errors.New("invalid Music request")
	}
	if req.Style == "" {
		req.Style = "synthwave"
	}
	if !slices.Contains(musicStyles, req.Style) {
		return s.Status(), errors.New("unsupported Music style")
	}
	if req.Dynamics == "" {
		req.Dynamics = "auto"
	}
	if !slices.Contains([]string{"auto", "calm", "balanced", "energetic"}, req.Dynamics) {
		return s.Status(), errors.New("unsupported Music dynamics")
	}
	tracker, err := maestrolive.TrackerConfigForSensitivity(req.Sensitivity)
	if err != nil {
		return s.Status(), err
	}
	if settings := s.Settings(); !settings.Available {
		return s.Status(), errors.New(settings.Warning)
	}
	config, err := s.loadConfig()
	if err != nil {
		return s.Status(), err
	}
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return s.Status(), ErrMusicClosed
	}
	if s.run != nil {
		s.mu.Unlock()
		return s.Status(), ErrMusicBusy
	}
	if s.cancelledID == req.RunID {
		s.mu.Unlock()
		return s.Status(), context.Canceled
	}
	runCtx, cancel := context.WithCancel(ctx)
	run := &musicRuntimeRun{id: req.RunID, cancel: cancel, done: make(chan struct{})}
	s.run = run
	status := MusicStatus{State: "starting", RunID: run.id, Serials: slices.Clone(req.Serials), Revision: s.status.Revision + 1}
	s.status = status
	observer := s.observer
	s.mu.Unlock()
	if observer != nil {
		observer(cloneMusicRuntimeStatus(status))
	}
	go func() {
		defer cancel()
		source, err := s.newSource(req.InputID)
		if err == nil {
			var analyzer maestrolive.Analyzer
			analyzer, err = s.newAnalyzer(runCtx, config)
			if err == nil {
				owned := &musicRuntimeAnalyzer{Analyzer: analyzer, ctx: runCtx}
				err = s.transport.RunMusic(runCtx, MusicInput{Serials: req.Serials, Source: source, Analyzer: owned, Style: req.Style, Dynamics: req.Dynamics, Tracker: tracker})
				err = errors.Join(err, owned.Close())
			}
		}
		if runCtx.Err() != nil && errors.Is(err, runCtx.Err()) {
			err = nil
		}
		status := MusicStatus{State: "stopped", RunID: run.id, Serials: slices.Clone(req.Serials)}
		if err != nil {
			status.State = "error"
			status.Error = err.Error()
		}
		// Keep the operation registered through final notification and cleanup.
		s.mu.Lock()
		status.Revision = s.status.Revision + 1
		s.status = status
		s.run = nil
		close(run.done)
		observer := s.observer
		s.mu.Unlock()
		if observer != nil {
			observer(cloneMusicRuntimeStatus(status))
		}
	}()
	return s.Status(), nil
}

func (s *MusicRuntimeService) Stop(ctx context.Context, id string) error {
	s.mu.Lock()
	run := s.run
	if id != "" && len(id) <= 64 {
		s.cancelledID = id
	}
	if run == nil || id != "" && run.id != id {
		s.mu.Unlock()
		return nil
	}
	status := s.status
	status.State = "stopping"
	status.Revision++
	s.status = status
	observer := s.observer
	run.cancel()
	s.mu.Unlock()
	if observer != nil {
		observer(cloneMusicRuntimeStatus(status))
	}
	select {
	case <-run.done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func cloneMusicRuntimeStatus(status MusicStatus) MusicStatus {
	status.Serials = slices.Clone(status.Serials)
	return status
}

func (s *MusicRuntimeService) Close(ctx context.Context) error {
	s.mu.Lock()
	s.closed = true
	s.mu.Unlock()
	return s.Stop(ctx, "")
}

type musicRuntimeAnalyzer struct {
	maestrolive.Analyzer
	ctx       context.Context
	cancelled atomic.Bool
	once      sync.Once
	err       error
}

func (a *musicRuntimeAnalyzer) Analyze(ctx context.Context, window maestrolive.PCMWindow) (maestrolive.Features, error) {
	features, err := a.Analyzer.Analyze(ctx, window)
	if ctx.Err() != nil {
		a.cancelled.Store(true)
	}
	return features, err
}
func (a *musicRuntimeAnalyzer) Close() error {
	a.once.Do(func() {
		a.err = a.Analyzer.Close()
		var exit *exec.ExitError
		if (a.ctx.Err() != nil || a.cancelled.Load()) && errors.As(a.err, &exit) && exit.ExitCode() < 0 {
			a.err = nil
		}
	})
	return a.err
}
