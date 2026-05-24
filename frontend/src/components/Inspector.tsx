import { RotateCcw, Undo2, X } from 'lucide-react';
import type { Device, HslColor } from '../domain/lifx';
import { hsl } from '../domain/lifx';
import { DevicePreview } from './DevicePreview';
import { Slider } from './primitives';
import './Inspector.css';

interface InspectorProps {
  device?: Device;
  dirty: boolean;
  livePreview: boolean;
  canUndo: boolean;
  saving: boolean;
  onClose: () => void;
  onChange: (device: Device) => void;
  onApply: () => void;
  onRevert: () => void;
  onUndo: () => void;
  onLivePreviewChange: (enabled: boolean) => void;
}

export function Inspector(props: InspectorProps) {
  if (!props.device) {
    return <aside className="right-panel panel-empty">select a light</aside>;
  }

  const device = props.device;

  return (
    <aside className="right-panel inspector">
      <header className="inspector-header">
        <div>
          <h2>{device.name}</h2>
          <p className="mono">{device.serial}</p>
        </div>
        <button className="icon-button close-button" aria-label="Close details" onClick={props.onClose}>
          <X size={15} />
        </button>
      </header>

      <div className="inspector-meta">
        <span>{device.model}</span>
        <span>{device.kind}</span>
      </div>

      <div className="large-preview">
        <DevicePreview device={device} />
      </div>

      <Slider label="brightness" value={device.brightness} onChange={(value) => props.onChange({ ...device, brightness: value, on: value > 0 })} />

      {device.kind === 'single' ? <SingleControls device={device} onChange={props.onChange} /> : null}
      {device.kind === 'multizone' ? <MultizoneDraftEditor device={device} onChange={props.onChange} /> : null}
      {device.kind === 'matrix' ? <MatrixDraftEditor device={device} onChange={props.onChange} /> : null}

      {device.kind !== 'single' ? (
        <footer className="draft-bar">
          <label>
            <input type="checkbox" checked={props.livePreview} onChange={(event) => props.onLivePreviewChange(event.target.checked)} />
            live preview
          </label>
          <div className="draft-actions">
            <button className="icon-button" aria-label="Undo" disabled={!props.canUndo} onClick={props.onUndo}>
              <Undo2 size={15} />
            </button>
            <button className="icon-button" aria-label="Revert" disabled={!props.dirty} onClick={props.onRevert}>
              <RotateCcw size={15} />
            </button>
            <button className="apply-button" disabled={!props.dirty || props.saving} onClick={props.onApply}>
              {props.saving ? 'applying' : 'apply'}
            </button>
          </div>
        </footer>
      ) : null}
    </aside>
  );
}

function SingleControls({ device, onChange }: { device: Device; onChange: (device: Device) => void }) {
  const color = device.color ?? { h: 38, s: 0.5, l: 0.55 };
  return (
    <section className="control-section">
      <h3>color</h3>
      <input
        className="hue-slider"
        type="range"
        min={0}
        max={360}
        value={color.h}
        onChange={(event) => onChange({ ...device, color: { ...color, h: Number(event.target.value) } })}
      />
      <div className="color-readout" style={{ background: hsl(color) }} />
    </section>
  );
}

function MultizoneDraftEditor({ device, onChange }: { device: Device; onChange: (device: Device) => void }) {
  const zones = device.zones ?? [];
  const paint = { h: 205, s: 0.85, l: 0.55 };

  return (
    <section className="control-section">
      <h3>zones</h3>
      <div className="zone-editor">
        {zones.map((zone, index) => (
          <button
            key={index}
            aria-label={`Paint zone ${index + 1}`}
            style={{ background: hsl(zone) }}
            onClick={() => {
              const next = [...zones];
              next[index] = paint;
              onChange({ ...device, zones: next });
            }}
          />
        ))}
      </div>
    </section>
  );
}

function MatrixDraftEditor({ device, onChange }: { device: Device; onChange: (device: Device) => void }) {
  const tiles = device.tiles ?? [];
  const paint: HslColor = { h: 310, s: 0.72, l: 0.55 };

  return (
    <section className="control-section">
      <h3>matrix</h3>
      <div className="matrix-editor">
        {tiles.map((tile, tileIndex) => (
          <div className="tile-editor" key={tile.id}>
            {tile.pixels.map((pixel, pixelIndex) => (
              <button
                key={pixelIndex}
                aria-label={`Paint tile ${tileIndex + 1} pixel ${pixelIndex + 1}`}
                style={{ background: hsl(pixel) }}
                onClick={() => {
                  const nextTiles = tiles.map((entry, entryIndex) => {
                    if (entryIndex !== tileIndex) return entry;
                    const pixels = [...entry.pixels];
                    pixels[pixelIndex] = paint;
                    return { ...entry, pixels };
                  });
                  onChange({ ...device, tiles: nextTiles });
                }}
              />
            ))}
          </div>
        ))}
      </div>
    </section>
  );
}
