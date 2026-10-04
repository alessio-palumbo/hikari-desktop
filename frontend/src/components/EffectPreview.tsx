import { useEffect, useRef, useState } from 'react';
import { RotateCcw } from 'lucide-react';
import { previewDeviceEffect, type DeviceEffectPreview } from '../backend/api';
import type { DeviceEffect, EffectParameters } from '../domain/effects';
import { effectPreviewColor, type Device } from '../domain/lifx';
import { playEffectPreviewFrames } from '../domain/effectPreviewPlayback';
import './EffectPreview.css';

export function EffectPreview({ device, effect, speedMs, params }: { device: Device; effect: DeviceEffect; speedMs: number; params?: EffectParameters }) {
  const [preview, setPreview] = useState<DeviceEffectPreview>();
  const [error, setError] = useState('');
  const [replay, setReplay] = useState(0);
  const [finished, setFinished] = useState(false);
  const [rendering, setRendering] = useState(true);
  const canvas = useRef<HTMLCanvasElement>(null);

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
    const context = canvas.current.getContext('2d');
    if (!context) return;
    setFinished(false);
    const width = 560;
    const height = Math.max(40, Math.min(280, width * preview.height / preview.width));
    canvas.current.width = width;
    canvas.current.height = height;
    const cellSize = Math.min(width / preview.width, height / preview.height);
    const left = (width - cellSize * preview.width) / 2;
    const top = (height - cellSize * preview.height) / 2;
    return playEffectPreviewFrames(preview.frames.length, preview.stepMs, (frame) => {
      context.clearRect(0, 0, width, height);
      preview.frames[frame]?.forEach((color, index) => {
        if (!preview.cells[index]) return;
        context.fillStyle = effectPreviewColor(color);
        context.fillRect(left + (index % preview.width) * cellSize + 1, top + Math.floor(index / preview.width) * cellSize + 1, Math.max(1, cellSize - 2), Math.max(1, cellSize - 2));
      });
    }, () => setFinished(true));
  }, [preview, replay, rendering, error]);

  const replayLabel = finished ? 'Preview finished - replay' : 'Replay local preview';

  return <div className="effect-preview">
    <div className="effect-preview-header"><span>preview</span><button type="button" aria-label={replayLabel} title={replayLabel} data-finished={finished} disabled={!preview || rendering || !!error} onClick={(event) => { event.stopPropagation(); setFinished(false); setReplay((value) => value + 1); }}><RotateCcw size={12} /></button></div>
    {error ? <div className="inspector-error" role="status">{error}</div> : preview ? <canvas ref={canvas} aria-label={`${effect} local preview`} role="img" /> : <div className="effect-preview-loading" role="status">rendering…</div>}
  </div>;
}
