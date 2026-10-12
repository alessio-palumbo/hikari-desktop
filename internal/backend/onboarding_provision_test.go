package backend

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	lifxdevice "github.com/alessio-palumbo/lifxlan-go/pkg/device"
)

type fakeSetupAdapter struct {
	fakeOnboardingScanner
	provision func(context.Context, SetupLightRequest, SetupVerifier, func(string)) SetupLightResult
}

func (f fakeSetupAdapter) Provision(ctx context.Context, req SetupLightRequest, verifier SetupVerifier, progress func(string)) SetupLightResult {
	return f.provision(ctx, req, verifier, progress)
}

type emptySetupVerifier struct{}

func (emptySetupVerifier) GetDevices() []lifxdevice.Device { return nil }

func testSetupRequest() SetupLightRequest {
	return SetupLightRequest{AttemptID: "attempt-one", BulbSSID: "LIFX A19 aabbcc", SSID: "Home", Password: "private-password"}
}

func TestSetupRequiresSelectedDiscoveredLightAndKeepsSecretsOutOfProgress(t *testing.T) {
	calls := 0
	s := &OnboardingService{scanner: fakeSetupAdapter{
		fakeOnboardingScanner: fakeOnboardingScanner{scan: func(context.Context) (SetupLightDiscovery, error) {
			return SetupLightDiscovery{Lights: []SetupLight{{Serial: "d073d5aabbcc", SSID: "LIFX A19 aabbcc"}, {Serial: "d073d5ddeeff", SSID: "LIFX A19 ddeeff"}}}, nil
		}},
		provision: func(ctx context.Context, req SetupLightRequest, _ SetupVerifier, progress func(string)) SetupLightResult {
			calls++
			if req.BulbSSID != testSetupRequest().BulbSSID {
				t.Fatal("wrong light provisioned")
			}
			if _, ok := ctx.Deadline(); !ok {
				t.Fatal("provisioning has no deadline")
			}
			progress("join")
			return SetupLightResult{Configured: true, Verified: true}
		},
	}}
	if _, err := s.Provision(context.Background(), testSetupRequest(), emptySetupVerifier{}, nil); err == nil {
		t.Fatal("undiscovered light provisioned")
	}
	if _, err := s.Scan(context.Background()); err != nil {
		t.Fatal(err)
	}
	var progress SetupProgress
	result, err := s.Provision(context.Background(), testSetupRequest(), emptySetupVerifier{}, func(p SetupProgress) { progress = p })
	if err != nil || calls != 1 || result.Serial != "d073d5aabbcc" || progress.AttemptID != "attempt-one" {
		t.Fatalf("provision=%#v %#v %v", result, progress, err)
	}
	data, _ := json.Marshal([]any{result, progress})
	if strings.Contains(string(data), testSetupRequest().Password) || strings.Contains(string(data), "password") {
		t.Fatal("credentials leaked in progress/result")
	}
	if _, err := s.Provision(context.Background(), testSetupRequest(), emptySetupVerifier{}, nil); err == nil {
		t.Fatal("configured candidate reused without rescan")
	}
}

func TestSetupInvalidCredentialsNeverInvokeAdapter(t *testing.T) {
	s := &OnboardingService{scanner: fakeSetupAdapter{provision: func(context.Context, SetupLightRequest, SetupVerifier, func(string)) SetupLightResult {
		t.Fatal("invalid request invoked adapter")
		return SetupLightResult{}
	}}}
	for _, alter := range []func(*SetupLightRequest){
		func(r *SetupLightRequest) { r.AttemptID = "" }, func(r *SetupLightRequest) { r.SSID = "" },
		func(r *SetupLightRequest) { r.SSID = strings.Repeat("x", 33) }, func(r *SetupLightRequest) { r.SSID = r.BulbSSID },
		func(r *SetupLightRequest) { r.Password = "short" }, func(r *SetupLightRequest) { r.OpenNetwork = true },
	} {
		req := testSetupRequest()
		alter(&req)
		if _, err := s.Provision(context.Background(), req, emptySetupVerifier{}, nil); err == nil {
			t.Fatal("invalid request accepted")
		}
	}
}

func TestSetupCancellationWaitsForRecoveryAndSerializesScans(t *testing.T) {
	started, recoveryDone := make(chan struct{}), make(chan struct{})
	s := &OnboardingService{candidates: map[string]SetupLight{testSetupRequest().BulbSSID: {Serial: "d073d5aabbcc"}}, scanner: fakeSetupAdapter{provision: func(ctx context.Context, _ SetupLightRequest, _ SetupVerifier, progress func(string)) SetupLightResult {
		close(started)
		<-ctx.Done()
		progress("rejoin")
		<-recoveryDone
		return SetupLightResult{Cancelled: true}
	}}}
	done := make(chan SetupLightResult, 1)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() {
		result, _ := s.Provision(ctx, testSetupRequest(), emptySetupVerifier{}, nil)
		done <- result
	}()
	<-started
	cancel()
	if _, err := s.Scan(context.Background()); !errors.Is(err, ErrOnboardingBusy) {
		t.Fatalf("scan during recovery=%v", err)
	}
	select {
	case <-done:
		t.Fatal("setup returned before recovery")
	default:
	}
	close(recoveryDone)
	if result := <-done; !result.Cancelled {
		t.Fatal("cancellation not reported")
	}
}
