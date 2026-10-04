import { useEffect, useRef, useState } from 'react';
import { Check, Eye } from 'lucide-react';
import { previewTheme } from '../backend/api';
import { DeviceKind, effectPreviewColor, type Device } from '../domain/lifx';
import { drawPreviewFrame, usePreviewCanvas } from './usePreviewCanvas';
import { themeRequest, themes, type ThemeDevicePreview, type ThemePreview, type ThemeRequest } from '../domain/themes';
import './ThemeControls.css';

export function ThemeControls({ devices, disabled = false, editing = false, onApply }: {
  devices: Device[]; disabled?: boolean; editing?: boolean; onApply: (request: ThemeRequest) => Promise<void>;
}) {
  const [selected, setSelected] = useState(0);
  const [preview, setPreview] = useState<ThemePreview>();
  const [error, setError] = useState('');
  const [busy, setBusy] = useState<'preview' | 'apply'>();
  const [applied, setApplied] = useState(false);
  const generation = useRef(0);
  const previewOpened = useRef(false);
  const request = themeRequest(themes[selected], devices);
  const targets = request.serials.join(',');

  useEffect(() => {
    generation.current++;
    setError(''); setBusy(undefined); setApplied(false);
    if (previewOpened.current && targets) void run('preview');
    else setPreview(undefined);
    return () => { generation.current++; };
  }, [selected, targets]);

  const run = async (action: 'preview' | 'apply') => {
    if (action === 'preview') previewOpened.current = true;
    const token = ++generation.current;
    setBusy(action); setError(''); setApplied(false);
    try {
      if (action === 'preview') {
        const result = await previewTheme(request);
        if (generation.current === token) setPreview(result);
      } else {
        await onApply(request);
        if (generation.current === token) setApplied(true);
      }
    } catch (failure) {
      if (generation.current === token) setError(failure instanceof Error ? failure.message : String(failure));
    } finally {
      if (generation.current === token) setBusy(undefined);
    }
  };

  return <section className="theme-controls">
    <div className="theme-list" role="radiogroup" aria-label="Theme">
      {themes.map((theme, index) => <button type="button" role="radio" aria-checked={selected === index} key={theme.name} disabled={busy === 'apply'} data-active={selected === index} onClick={() => setSelected(index)}>
        <span className="theme-swatches" aria-hidden="true">{theme.palette.Base.map((color, stop) => <i key={stop} style={{ background: effectPreviewColor({ h: color.Hue, s: color.Saturation / 100, l: 1, kelvin: color.Kelvin }) }} />)}</span>
        <span>{theme.name}</span>
      </button>)}
    </div>
    <div className="theme-actions">
      <button type="button" className="icon-button" title="Preview theme locally" aria-label="Preview theme locally" disabled={!!busy || !targets || disabled || editing} onClick={() => void run('preview')}><Eye size={14} /></button>
      <button type="button" className="apply-button" title="Apply theme, preserving each light's brightness" disabled={!!busy || !targets || disabled || editing || applied} onClick={() => void run('apply')}>{busy === 'apply' ? 'applying' : applied ? <><Check size={12} /> applied</> : 'apply'}</button>
    </div>
    {editing ? <div className="theme-status" role="status">Exit layout editing to apply a theme.</div> : null}
    {busy === 'preview' ? <div className="theme-status" role="status">rendering preview…</div> : null}
    {error ? <div className="inspector-error" role="alert">{error}</div> : null}
    {preview ? <div className="theme-previews" aria-busy={busy === 'preview'}>{preview.devices.map((device) => <ThemeCanvas key={device.serial} device={device} kind={devices.find((target) => target.serial === device.serial)?.kind ?? DeviceKind.Matrix} />)}</div> : null}
  </section>;
}

function ThemeCanvas({ device, kind }: { device: ThemeDevicePreview; kind: DeviceKind }) {
  const { canvas, layout } = usePreviewCanvas(device.width, device.height, kind);
  useEffect(() => {
    if (!canvas.current || device.width < 1 || device.height < 1) return;
    drawPreviewFrame(canvas.current, device.width, device.cells, device.colors, layout);
  }, [device, layout.width, layout.height, layout.cellWidth, layout.cellHeight, layout.left]);
  return <figure><figcaption>{device.name}</figcaption><canvas ref={canvas} role="img" aria-label={`${device.name} theme preview`} /></figure>;
}
