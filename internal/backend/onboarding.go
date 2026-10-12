package backend

import (
	"context"
	"errors"
	"sort"
	"strings"
	"sync"
	"time"

	lifxdevice "github.com/alessio-palumbo/lifxlan-go/pkg/device"
)

const onboardingScanTimeout = 45 * time.Second

var (
	ErrOnboardingUnavailable = errors.New("device setup is unavailable in this build")
	ErrOnboardingBusy        = errors.New("device setup is already busy")
	ErrOnboardingClosed      = errors.New("device setup has been closed")
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
	Networks    []string     `json:"networks"`
}

type SetupLightRequest struct {
	AttemptID   string `json:"attemptId"`
	BulbSSID    string `json:"bulbSsid"`
	SSID        string `json:"ssid"`
	Password    string `json:"password"`
	OpenNetwork bool   `json:"openNetwork"`
}

type SetupProgress struct {
	AttemptID string `json:"attemptId"`
	Phase     string `json:"phase"`
}

type SetupLightResult struct {
	Serial     string `json:"serial"`
	Configured bool   `json:"configured"`
	Verified   bool   `json:"verified"`
	Cancelled  bool   `json:"cancelled"`
	Error      string `json:"error,omitempty"`
	Warning    string `json:"warning,omitempty"`
}

type SetupVerifier interface{ GetDevices() []lifxdevice.Device }

type onboardingProvisioner interface {
	Provision(context.Context, SetupLightRequest, SetupVerifier, func(string)) SetupLightResult
}

type onboardingScanner interface {
	Scan(context.Context) (SetupLightDiscovery, error)
}

// OnboardingService is separate from device discovery. Constructing it does not
// scan, request permission, join Wi-Fi, or create another LIFX controller.
type OnboardingService struct {
	scanner       onboardingScanner
	mu            sync.Mutex
	closed        bool
	cancel        context.CancelFunc
	done          chan struct{}
	candidates    map[string]SetupLight
	provisioning  bool
	scanCancelled bool
	lastScan      SetupLightDiscovery
	lastScanAt    time.Time
}

func NewOnboardingService() *OnboardingService {
	return &OnboardingService{scanner: newPlatformOnboardingScanner()}
}

func (s *OnboardingService) Capabilities() OnboardingCapabilities {
	return OnboardingCapabilities{Available: s.scanner != nil}
}

// Discover reuses a recent completed scan on modal reopen. Explicit refresh
// always scans again; results are not kept across provisioning or shutdown.
func (s *OnboardingService) Discover(ctx context.Context, refresh bool) (SetupLightDiscovery, error) {
	if err := ctx.Err(); err != nil {
		return SetupLightDiscovery{}, err
	}
	s.mu.Lock()
	if !refresh && !s.closed && s.done == nil && !s.lastScanAt.IsZero() && time.Since(s.lastScanAt) < 10*time.Second {
		result := cloneSetupDiscovery(s.lastScan)
		s.mu.Unlock()
		return result, nil
	}
	s.mu.Unlock()
	return s.Scan(ctx)
}

func cloneSetupDiscovery(result SetupLightDiscovery) SetupLightDiscovery {
	result.Lights = append([]SetupLight{}, result.Lights...)
	result.Networks = append([]string{}, result.Networks...)
	return result
}

func (s *OnboardingService) Scan(ctx context.Context) (SetupLightDiscovery, error) {
	empty := SetupLightDiscovery{Lights: []SetupLight{}}
	ctx, cancel := context.WithTimeout(ctx, onboardingScanTimeout)
	defer cancel()
	ctx, finish, err := s.begin(ctx, onboardingScanTimeout, false)
	if err != nil {
		return empty, err
	}
	defer finish()
	s.mu.Lock()
	s.lastScanAt = time.Time{}
	s.mu.Unlock()
	result, err := s.scanner.Scan(ctx)
	s.mu.Lock()
	s.candidates = make(map[string]SetupLight)
	if err == nil && ctx.Err() == nil {
		for _, light := range result.Lights {
			s.candidates[light.SSID] = light
		}
	}
	s.mu.Unlock()
	if ctx.Err() != nil {
		return empty, ctx.Err()
	}
	if err != nil {
		return empty, err
	}
	if result.Lights == nil {
		result.Lights = []SetupLight{}
	}
	seenNetworks := make(map[string]bool)
	var networks []string
	for _, ssid := range result.Networks {
		if ssid != "" && !seenNetworks[ssid] {
			networks = append(networks, ssid)
			seenNetworks[ssid] = true
		}
	}
	sort.Strings(networks)
	result.Networks = networks
	if result.Networks == nil {
		result.Networks = []string{}
	}
	sort.Slice(result.Lights, func(i, j int) bool {
		if result.Lights[i].SSID != result.Lights[j].SSID {
			return result.Lights[i].SSID < result.Lights[j].SSID
		}
		return result.Lights[i].Serial < result.Lights[j].Serial
	})
	s.mu.Lock()
	s.lastScan, s.lastScanAt = cloneSetupDiscovery(result), time.Now()
	s.mu.Unlock()
	return result, nil
}

func (s *OnboardingService) Provision(ctx context.Context, req SetupLightRequest, verifier SetupVerifier, progress func(SetupProgress)) (SetupLightResult, error) {
	if strings.TrimSpace(req.AttemptID) == "" || len(req.AttemptID) > 64 {
		return SetupLightResult{}, errors.New("invalid setup attempt")
	}
	if strings.TrimSpace(req.SSID) == "" || len(req.SSID) > 32 {
		return SetupLightResult{}, errors.New("Wi-Fi name must contain 1..32 bytes")
	}
	if req.SSID == req.BulbSSID {
		return SetupLightResult{}, errors.New("choose your Wi-Fi network, not the device's setup network")
	}
	if req.OpenNetwork && req.Password != "" {
		return SetupLightResult{}, errors.New("an open network must not have a password")
	}
	if !req.OpenNetwork && (len(req.Password) < 8 || len(req.Password) > 64) {
		return SetupLightResult{}, errors.New("enter an 8..64 byte WPA2 password")
	}
	if verifier == nil {
		return SetupLightResult{}, ErrOnboardingUnavailable
	}
	runner, ok := s.scanner.(onboardingProvisioner)
	if !ok {
		return SetupLightResult{}, ErrOnboardingUnavailable
	}
	ctx, finish, err := s.begin(ctx, 4*time.Minute, true)
	if err != nil {
		return SetupLightResult{}, err
	}
	defer finish()
	s.mu.Lock()
	light, found := s.candidates[req.BulbSSID]
	s.lastScanAt = time.Time{}
	s.mu.Unlock()
	if !found {
		return SetupLightResult{}, errors.New("scan and select a device in setup mode first")
	}
	result := runner.Provision(ctx, req, verifier, func(phase string) {
		if progress != nil {
			progress(SetupProgress{AttemptID: req.AttemptID, Phase: phase})
		}
	})
	result.Serial = light.Serial
	if result.Configured {
		s.mu.Lock()
		delete(s.candidates, req.BulbSSID)
		s.mu.Unlock()
	}
	return result, nil
}

func (s *OnboardingService) begin(ctx context.Context, timeout time.Duration, provisioning bool) (context.Context, func(), error) {
	if err := ctx.Err(); err != nil {
		return nil, nil, err
	}
	if s.scanner == nil {
		return nil, nil, ErrOnboardingUnavailable
	}
	s.mu.Lock()
	// A closed modal cancels its scan, but native Wi-Fi work must finish before
	// a reopened modal starts another one. Never wait on an active setup operation.
	for !provisioning && s.done != nil && s.scanCancelled && !s.closed {
		done := s.done
		s.mu.Unlock()
		select {
		case <-done:
		case <-ctx.Done():
			return nil, nil, ctx.Err()
		}
		s.mu.Lock()
	}
	if err := ctx.Err(); err != nil {
		s.mu.Unlock()
		return nil, nil, err
	}
	if s.closed {
		s.mu.Unlock()
		return nil, nil, ErrOnboardingClosed
	}
	if s.done != nil {
		s.mu.Unlock()
		return nil, nil, ErrOnboardingBusy
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	done := make(chan struct{})
	s.cancel, s.done = cancel, done
	s.provisioning = provisioning
	s.scanCancelled = false
	s.mu.Unlock()
	return ctx, func() {
		cancel()
		s.mu.Lock()
		s.cancel, s.done = nil, nil
		s.provisioning = false
		s.scanCancelled = false
		close(done)
		s.mu.Unlock()
	}, nil
}

func (s *OnboardingService) CancelScan() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.cancel != nil && !s.provisioning {
		s.scanCancelled = true
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
