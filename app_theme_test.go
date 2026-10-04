package main

import (
	"context"
	"errors"
	"reflect"
	"testing"
	"time"

	"hikari-desktop/internal/backend"
)

type themePreviewTransport struct {
	*recordingTransport
	req      backend.ThemeRequest
	preview  backend.ThemePreview
	failure  error
	deadline time.Time
	applied  backend.ThemeApplyResult
}

func (t *themePreviewTransport) ApplyTheme(ctx context.Context, req backend.ThemeRequest) (backend.ThemeApplyResult, error) {
	t.req = req
	t.deadline, _ = ctx.Deadline()
	if err := ctx.Err(); err != nil {
		return backend.ThemeApplyResult{}, err
	}
	return t.applied, t.failure
}

func TestAppThemeApplyForwardsPartialResultsAndCancellation(t *testing.T) {
	transport := &themePreviewTransport{recordingTransport: &recordingTransport{}, applied: backend.ThemeApplyResult{
		Devices: []backend.Device{{Serial: "d073d501a2c3"}}, Failures: []backend.ThemeApplyFailure{{Serial: "d073d501a2c4", Error: "send failed", StateMayHaveChanged: true}},
	}}
	app := NewAppWithTransport(transport)
	req := backend.ThemeRequest{Serials: []string{"d073d501a2c3", "d073d501a2c4"}}
	result, err := app.ApplyTheme(req)
	if err != nil || !reflect.DeepEqual(result, transport.applied) || !reflect.DeepEqual(transport.req, req) {
		t.Fatalf("partial apply forwarding: %#v %v", result, err)
	}
	if remaining := time.Until(transport.deadline); remaining <= 0 || remaining > 10*time.Second {
		t.Fatal("theme apply is not bounded")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	app.ctx = ctx
	if _, err := app.ApplyTheme(req); !errors.Is(err, context.Canceled) {
		t.Fatal("apply shutdown cancellation lost")
	}
	if _, err := NewAppWithTransport(backend.NewMockTransport()).ApplyTheme(req); err == nil {
		t.Fatal("unsupported transport accepted theme application")
	}
}

func (t *themePreviewTransport) PreviewTheme(ctx context.Context, req backend.ThemeRequest) (backend.ThemePreview, error) {
	t.req = req
	t.deadline, _ = ctx.Deadline()
	if err := ctx.Err(); err != nil {
		return backend.ThemePreview{}, err
	}
	return t.preview, t.failure
}

func TestAppThemePreviewUsesOptionalTransportWithTimeout(t *testing.T) {
	transport := &themePreviewTransport{recordingTransport: &recordingTransport{}, preview: backend.ThemePreview{Devices: []backend.ThemeDevicePreview{{Serial: "d073d501a2c3"}}}}
	app := NewAppWithTransport(transport)
	req := backend.ThemeRequest{Serials: []string{"d073d501a2c3"}}
	got, err := app.PreviewTheme(req)
	if err != nil || !reflect.DeepEqual(got, transport.preview) || !reflect.DeepEqual(transport.req, req) {
		t.Fatalf("preview forwarding: %#v %v", got, err)
	}
	if remaining := time.Until(transport.deadline); remaining <= 0 || remaining > 5*time.Second {
		t.Fatal("preview request lacks bounded timeout")
	}
	transport.failure = errors.New("capture failed")
	if _, err := app.PreviewTheme(req); !errors.Is(err, transport.failure) {
		t.Fatal("preview error lost")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	app.ctx = ctx
	if _, err := app.PreviewTheme(req); !errors.Is(err, context.Canceled) {
		t.Fatal("shutdown cancellation lost")
	}
}

func TestAppThemePreviewUnavailableWithoutProvider(t *testing.T) {
	app := NewAppWithTransport(backend.NewMockTransport())
	if _, err := app.PreviewTheme(backend.ThemeRequest{}); err == nil {
		t.Fatal("unsupported theme preview silently accepted")
	}
}
