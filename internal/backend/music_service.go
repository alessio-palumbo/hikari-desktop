package backend

import (
	"context"
	"errors"
	"sync"
	"time"

	maestrolive "github.com/alessio-palumbo/lifx-maestro/live"
)

var (
	ErrMusicBusy   = errors.New("music is already running")
	ErrMusicClosed = errors.New("music service has been closed")
)

const musicCleanupTimeout = 20 * time.Second

type MusicStatus struct {
	State    string   `json:"state"`
	Serials  []string `json:"serials"`
	Error    string   `json:"error,omitempty"`
	RunID    string   `json:"runId,omitempty"`
	Revision uint64   `json:"revision,omitempty"`
}

type musicRun struct {
	cancel context.CancelFunc
	done   chan struct{}
	err    error
}

// MusicService owns one Live run, not a LIFX controller or audio runtime.
// It is inert until Run is called. The later transport layer supplies a sink
// bound to validated serials and owns capture/power/restore; after Stop returns
// successfully, no more frames can be sent by that run.
type MusicService struct {
	mu       sync.Mutex
	closed   bool
	run      *musicRun
	status   MusicStatus
	observer func(MusicStatus)
}

func NewMusicService() *MusicService {
	return &MusicService{status: MusicStatus{State: "idle", Serials: []string{}}}
}

func (s *MusicService) Status() MusicStatus {
	s.mu.Lock()
	defer s.mu.Unlock()
	status := s.status
	status.Serials = append([]string{}, status.Serials...)
	return status
}

func (s *MusicService) SetStatusObserver(observer func(MusicStatus)) {
	s.mu.Lock()
	s.observer = observer
	s.mu.Unlock()
}

func (s *MusicService) notify() {
	s.mu.Lock()
	observer := s.observer
	status := s.status
	status.Serials = append([]string{}, status.Serials...)
	s.mu.Unlock()
	if observer != nil {
		observer(status)
	}
}

// Run blocks until cancellation, input completion or failure. A validated,
// accepted run takes ownership of Analyzer.Close, called once after all engine
// work has stopped. Rejected configurations leave resources with the caller.
// Source and sink must honor cancellation; sink writes must finish synchronously.
func (s *MusicService) Run(ctx context.Context, config maestrolive.Config) error {
	return s.runWithLifecycle(ctx, config, nil, nil)
}

// Preparation reserves targets before capture. Cleanup runs only after engine
// output has quiesced, using a fresh bounded context so cancellation can restore.
func (s *MusicService) runWithLifecycle(ctx context.Context, config maestrolive.Config, prepare func(context.Context) error, cleanup func(context.Context) error) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	engine, err := maestrolive.New(config)
	if err != nil {
		return err
	}
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return ErrMusicClosed
	}
	if s.run != nil {
		s.mu.Unlock()
		return ErrMusicBusy
	}
	runCtx, cancel := context.WithCancel(ctx)
	run := &musicRun{cancel: cancel, done: make(chan struct{})}
	s.run = run
	serials := make([]string, 0, len(config.Targets))
	for _, target := range config.Targets {
		serials = append(serials, target.ID)
	}
	s.status = MusicStatus{State: "running", Serials: serials}
	s.mu.Unlock()
	s.notify()

	if prepare != nil {
		err = prepare(runCtx)
	}
	if err == nil {
		err = engine.Run(runCtx)
	}
	if runCtx.Err() != nil && errors.Is(err, runCtx.Err()) {
		err = nil
	}
	cancel()
	err = errors.Join(err, config.Analyzer.Close())
	if cleanup != nil {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), musicCleanupTimeout)
		err = errors.Join(err, cleanup(cleanupCtx))
		cleanupCancel()
	}
	s.mu.Lock()
	run.err = err
	s.run = nil
	s.status.State = "stopped"
	if err != nil {
		s.status.State = "error"
		s.status.Error = err.Error()
	}
	close(run.done)
	s.mu.Unlock()
	s.notify()
	return err
}

func (s *MusicService) Stop(ctx context.Context) error {
	s.mu.Lock()
	run := s.run
	if run != nil {
		s.status.State = "stopping"
	}
	s.mu.Unlock()
	if run != nil {
		s.notify()
	}
	return waitForMusicRun(ctx, run)
}

// Close prevents subsequent runs even if waiting for the active one times out.
// A timed-out wait does not mean output has stopped: wait again before restoring.
func (s *MusicService) Close(ctx context.Context) error {
	s.mu.Lock()
	s.closed = true
	run := s.run
	if run != nil {
		s.status.State = "stopping"
	}
	s.mu.Unlock()
	if run != nil {
		s.notify()
	}
	return waitForMusicRun(ctx, run)
}

func waitForMusicRun(ctx context.Context, run *musicRun) error {
	if run == nil {
		return nil
	}
	run.cancel()
	select {
	case <-run.done:
		return run.err
	case <-ctx.Done():
		return ctx.Err()
	}
}
