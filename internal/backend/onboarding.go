package backend

import (
	"context"
	"errors"
	"sort"
	"sync"
	"time"
)

const onboardingScanTimeout = 45 * time.Second

var (
	ErrOnboardingUnavailable = errors.New("light setup is unavailable in this build")
	ErrOnboardingBusy        = errors.New("light setup is already busy")
	ErrOnboardingClosed      = errors.New("light setup has been closed")
)

type OnboardingCapabilities struct {
	Available bool `json:"available"`
}

type SetupLight struct {
	Serial string  `json:"serial"`
	SSID   string  `json:"ssid"`
	RSSI   float64 `json:"rssi"`
}

type SetupLightDiscovery struct {
	Lights      []SetupLight `json:"lights"`
	CurrentSSID string       `json:"currentSsid"`
}

type onboardingScanner interface {
	Scan(context.Context) (SetupLightDiscovery, error)
}

// OnboardingService is separate from device discovery. Constructing it does not
// scan, request permission, join Wi-Fi, or create another LIFX controller.
type OnboardingService struct {
	scanner onboardingScanner
	mu      sync.Mutex
	closed  bool
	cancel  context.CancelFunc
	done    chan struct{}
}

func NewOnboardingService() *OnboardingService {
	return &OnboardingService{scanner: newPlatformOnboardingScanner()}
}

func (s *OnboardingService) Capabilities() OnboardingCapabilities {
	return OnboardingCapabilities{Available: s.scanner != nil}
}

func (s *OnboardingService) Scan(ctx context.Context) (SetupLightDiscovery, error) {
	empty := SetupLightDiscovery{Lights: []SetupLight{}}
	if err := ctx.Err(); err != nil {
		return empty, err
	}
	if s.scanner == nil {
		return empty, ErrOnboardingUnavailable
	}
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return empty, ErrOnboardingClosed
	}
	if s.done != nil {
		s.mu.Unlock()
		return empty, ErrOnboardingBusy
	}
	ctx, cancel := context.WithTimeout(ctx, onboardingScanTimeout)
	done := make(chan struct{})
	s.cancel, s.done = cancel, done
	s.mu.Unlock()
	defer func() {
		cancel()
		s.mu.Lock()
		s.cancel, s.done = nil, nil
		close(done)
		s.mu.Unlock()
	}()
	result, err := s.scanner.Scan(ctx)
	if ctx.Err() != nil {
		return empty, ctx.Err()
	}
	if err != nil {
		return empty, err
	}
	if result.Lights == nil {
		result.Lights = []SetupLight{}
	}
	sort.Slice(result.Lights, func(i, j int) bool {
		if result.Lights[i].SSID != result.Lights[j].SSID {
			return result.Lights[i].SSID < result.Lights[j].SSID
		}
		return result.Lights[i].Serial < result.Lights[j].Serial
	})
	return result, nil
}

func (s *OnboardingService) CancelScan() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.cancel != nil {
		s.cancel()
	}
}

func (s *OnboardingService) Close(ctx context.Context) error {
	s.mu.Lock()
	s.closed = true
	if s.cancel != nil {
		s.cancel()
	}
	done := s.done
	s.mu.Unlock()
	if done == nil {
		return nil
	}
	select {
	case <-done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}
