package backend

import (
	"context"
	"errors"
	"math"
	"slices"
	"sync"

	maestrolive "github.com/alessio-palumbo/lifx-maestro/live"
	maestronative "github.com/alessio-palumbo/lifx-maestro/live/native"
)

type MusicTransport interface {
	RunMusic(context.Context, MusicInput) error
	StopMusic(context.Context) error
	MusicStatus() MusicStatus
	SetMusicStatusObserver(func(MusicStatus))
	UpdateMusicControls(MusicControls) error
}

type StartMusicRequest struct {
	RunID            string   `json:"runId"`
	Serials          []string `json:"serials"`
	InputID          string   `json:"inputId"`
	Style            string   `json:"style"`
	Dynamics         string   `json:"dynamics"`
	Sensitivity      string   `json:"sensitivity"`
	MasterBrightness *float64 `json:"masterBrightness"`
}

type MusicControls struct {
	RunID            string  `json:"runId"`
	Style            string  `json:"style"`
	Dynamics         string  `json:"dynamics"`
	MasterBrightness float64 `json:"masterBrightness"`
}

func validateMusicControls(c MusicControls) error {
	if !slices.Contains(musicStyles, c.Style) {
		return errors.New("unsupported Music style")
	}
	if !slices.Contains([]string{"auto", "calm", "balanced", "energetic"}, c.Dynamics) {
		return errors.New("unsupported Music dynamics")
	}
	if math.IsNaN(c.MasterBrightness) || math.IsInf(c.MasterBrightness, 0) || c.MasterBrightness < 0 || c.MasterBrightness > 1 {
		return errors.New("Music master brightness must be between 0 and 100%")
	}
	return nil
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

// MusicRuntimeService owns native analysis and the asynchronous Wails-facing
// lifecycle. Construction never initializes audio capture or analysis.
type MusicRuntimeService struct {
	transport        MusicTransport
	newSource        func(string) (maestrolive.AudioSource, error)
	listInputs       func() ([]MusicInputDevice, error)
	newAnalyzer      func(context.Context) (maestrolive.Analyzer, error)
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
	s := &MusicRuntimeService{transport: t, newSource: newMusicMicrophone, listInputs: listMusicMicrophones, newAnalyzer: func(context.Context) (maestrolive.Analyzer, error) { return maestronative.New(), nil }, captureAvailable: musicCaptureSupported, status: MusicStatus{State: "idle", Serials: []string{}}}
	if t != nil {
		t.SetMusicStatusObserver(s.observeTransport)
	}
	return s
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
	if req.MasterBrightness != nil {
		brightness := *req.MasterBrightness
		req.MasterBrightness = &brightness
		if err := validateMusicControls(MusicControls{Style: req.Style, Dynamics: req.Dynamics, MasterBrightness: brightness}); err != nil {
			return s.Status(), err
		}
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
			analyzer, err = s.newAnalyzer(runCtx)
			if err == nil {
				owned := &musicRuntimeAnalyzer{Analyzer: analyzer}
				err = s.transport.RunMusic(runCtx, MusicInput{Serials: req.Serials, Source: source, Analyzer: owned, Style: req.Style, Dynamics: req.Dynamics, Tracker: tracker, MasterBrightness: req.MasterBrightness})
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

func (s *MusicRuntimeService) UpdateControls(controls MusicControls) error {
	if err := validateMusicControls(controls); err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.run == nil || s.run.id != controls.RunID || s.status.State != "running" {
		return errors.New("Music session is no longer running")
	}
	return s.transport.UpdateMusicControls(controls)
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
	once sync.Once
	err  error
}

func (a *musicRuntimeAnalyzer) Close() error {
	a.once.Do(func() {
		a.err = a.Analyzer.Close()
	})
	return a.err
}
