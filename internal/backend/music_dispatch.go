package backend

import (
	"context"
	"errors"
	"sync"

	maestrolive "github.com/alessio-palumbo/lifx-maestro/live"
	lifxdevice "github.com/alessio-palumbo/lifxlan-go/pkg/device"
	lifxeffects "github.com/alessio-palumbo/lifxlan-go/pkg/effects"
	lifxadapters "github.com/alessio-palumbo/lifxlan-go/pkg/effects/adapters"
)

// Music preserves Maestro's frame transitions, unlike the immediate matrix
// updates used by Hikari's continuously rendered app effects.
type musicMatrixRenderer struct {
	surface lifxdevice.Surface
	send    lifxadapters.SendFunc
}

func (r musicMatrixRenderer) RenderFrame(ctx context.Context, frame lifxeffects.Frame) error {
	return lifxadapters.NewMatrixRenderer(r.send,
		lifxadapters.WithMatrixSurface(r.surface),
		lifxadapters.WithMatrixTransitionDuration(frame.Duration),
	).RenderFrame(ctx, frame)
}

func newMusicRenderer(d lifxdevice.Device, send lifxadapters.SendFunc) lifxeffects.Renderer {
	if d.LightType == lifxdevice.LightTypeMatrix {
		return musicMatrixRenderer{surface: lifxdevice.SurfaceFromDevice(d), send: send}
	}
	return lifxadapters.NewRendererForDevice(d, send)
}

// Match Maestro Live's dispatch: serialize each target, allow different targets
// to progress independently, and replace queued stale frames rather than replay
// a backlog. Close is a mandatory barrier before restoring captured state.
type musicFrameDispatcher struct {
	ctx    context.Context
	cancel context.CancelFunc
	fail   context.CancelFunc
	render func(context.Context, maestrolive.TargetFrame) error
	queues map[string]chan maestrolive.TargetFrame
	mu     sync.Mutex
	closed bool
	err    error
	wg     sync.WaitGroup
}

func newMusicFrameDispatcher(ctx context.Context, ids []string, render func(context.Context, maestrolive.TargetFrame) error, fail context.CancelFunc) *musicFrameDispatcher {
	ctx, cancel := context.WithCancel(ctx)
	d := &musicFrameDispatcher{ctx: ctx, cancel: cancel, fail: fail, render: render, queues: make(map[string]chan maestrolive.TargetFrame)}
	for _, id := range ids {
		queue := make(chan maestrolive.TargetFrame, 1)
		d.queues[id] = queue
		d.wg.Add(1)
		go d.runTarget(queue)
	}
	return d
}

func (d *musicFrameDispatcher) Write(ctx context.Context, batch maestrolive.FrameBatch) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.err != nil {
		return d.err
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if d.closed || d.ctx.Err() != nil {
		return context.Canceled
	}
	for _, output := range batch {
		if d.queues[output.Target] == nil {
			return errors.New("music output contains an unselected target")
		}
	}
	for _, output := range batch {
		// The producer retains its buffers; a queued frame must own its colours.
		output.Frame.Colors = append([]lifxeffects.Color(nil), output.Frame.Colors...)
		queue := d.queues[output.Target]
		select {
		case queue <- output:
		default:
			select {
			case <-queue:
			default:
			}
			queue <- output
		}
	}
	return nil
}

func (d *musicFrameDispatcher) runTarget(queue <-chan maestrolive.TargetFrame) {
	defer d.wg.Done()
	for {
		select {
		case <-d.ctx.Done():
			return
		case output := <-queue:
			if d.ctx.Err() != nil {
				return
			}
			if err := d.render(d.ctx, output); err != nil {
				if d.ctx.Err() == nil || !errors.Is(err, d.ctx.Err()) {
					d.mu.Lock()
					if d.err == nil {
						d.err = err
					}
					d.mu.Unlock()
					d.cancel()
					d.fail()
				}
				return
			}
		}
	}
}

func (d *musicFrameDispatcher) Close() error {
	d.mu.Lock()
	d.closed = true
	d.cancel()
	d.mu.Unlock()
	d.wg.Wait()
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.err
}
