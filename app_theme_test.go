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
	req      backend.ThemePreviewRequest
	preview  backend.ThemePreview
	failure  error
	deadline time.Time
}

func (t *themePreviewTransport) PreviewTheme(ctx context.Context, req backend.ThemePreviewRequest) (backend.ThemePreview, error) {
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
	req := backend.ThemePreviewRequest{Serials: []string{"d073d501a2c3"}}
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
	if _, err := app.PreviewTheme(backend.ThemePreviewRequest{}); err == nil {
		t.Fatal("unsupported theme preview silently accepted")
	}
}
