import { useEffect, useState } from 'react';
import { RotateCcw } from 'lucide-react';
import { previewDeviceEffect, type DeviceEffectPreview } from '../backend/api';
import type { DeviceEffect, EffectParameters } from '../domain/effects';
import { type Device } from '../domain/lifx';
import { drawPreviewFrame, usePreviewCanvas } from './usePreviewCanvas';
import { playEffectPreviewFrames } from '../domain/effectPreviewPlayback';
import './EffectPreview.css';

export function EffectPreview({ device, effect, speedMs, params }: { device: Device; effect: DeviceEffect; speedMs: number; params?: EffectParameters }) {
  const [preview, setPreview] = useState<DeviceEffectPreview>();
  const [error, setError] = useState('');
  const [replay, setReplay] = useState(0);
  const [finished, setFinished] = useState(false);
  const [rendering, setRendering] = useState(true);
  const { canvas, layout } = usePreviewCanvas(preview?.width, preview?.height, device.kind, !!preview && !error);

  useEffect(() => {
    let disposed = false;
    setError('');
    setFinished(false);
    setRendering(true);
    const request = setTimeout(() => {
      void previewDeviceEffect(device, effect, speedMs, params).then((result) => {
        if (!disposed) { setPreview(result); setRendering(false); }
      }).catch((failure) => {
        if (!disposed) { setError(String(failure instanceof Error ? failure.message : failure)); setRendering(false); }
      });
    }, 200);
    return () => { disposed = true; clearTimeout(request); };
  }, [device.serial, effect, speedMs, params]);

  useEffect(() => {
    if (rendering || error || !preview || !canvas.current) return;
    setFinished(false);
    return playEffectPreviewFrames(preview.frames.length, preview.stepMs, (frame) => {
      if (canvas.current) drawPreviewFrame(canvas.current, preview.width, preview.cells, preview.frames[frame] ?? [], layout);
    }, () => setFinished(true));
  }, [preview, replay, rendering, error, layout.width, layout.height, layout.cellWidth, layout.cellHeight, layout.left]);

  const replayLabel = finished ? 'Preview finished - replay' : 'Replay local preview';

  return <div className="effect-preview">
    <div className="effect-preview-header"><span>preview</span><button type="button" aria-label={replayLabel} title={replayLabel} data-finished={finished} disabled={!preview || rendering || !!error} onClick={(event) => { event.stopPropagation(); setFinished(false); setReplay((value) => value + 1); }}><RotateCcw size={12} /></button></div>
    {error ? <div className="inspector-error" role="status">{error}</div> : preview ? <canvas ref={canvas} aria-label={`${effect} local preview`} role="img" /> : <div className="effect-preview-loading" role="status">rendering…</div>}
  </div>;
}
