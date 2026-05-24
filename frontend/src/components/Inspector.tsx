import { useEffect, useRef, useState } from 'react';
import { Brush, Droplet, Pipette, RotateCcw, Undo2, Wand2, X } from 'lucide-react';
import type { Device, HslColor } from '../domain/lifx';
import { hsl } from '../domain/lifx';
import { ColorWheel, Slider } from './primitives';
import './Inspector.css';

type PaintMode = 'color' | 'white';
type PaintTool = 'brush' | 'fill' | 'gradient' | 'picker';

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
  const [mode, setMode] = useState<PaintMode>('color');
  const [tool, setTool] = useState<PaintTool>('brush');
  const [paintColor, setPaintColor] = useState(() => initialPaintColor(device));
  const [whiteKelvin, setWhiteKelvin] = useState(() => device.kelvin ?? 3500);
  const [whiteColor, setWhiteColor] = useState(() => kelvinToHsl(device.kelvin ?? 3500));

  useEffect(() => {
    setMode('color');
    setTool('brush');
    setPaintColor(initialPaintColor(device));
    setWhiteKelvin(device.kelvin ?? 3500);
    setWhiteColor(kelvinToHsl(device.kelvin ?? 3500));
  }, [device.id]);

  const whiteValue = Math.max(0, Math.min(1, (whiteKelvin - 2500) / 4000));
  const activePaintColor = mode === 'white' ? whiteColor : paintColor;

  const setColor = (color: HslColor) => {
    setPaintColor(color);
    if (device.kind === 'single') props.onChange({ ...device, color });
  };

  const setKelvin = (value: number) => {
    const nextKelvin = Math.round(2500 + value * 4000);
    const color = kelvinToHsl(nextKelvin);
    setWhiteKelvin(nextKelvin);
    setWhiteColor(color);
    if (device.kind === 'single') props.onChange({ ...device, kelvin: nextKelvin, color });
  };

  const pickColor = (color: HslColor) => {
    if (mode === 'white') setWhiteColor(color);
    else setPaintColor(color);
  };

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

      <ModeToggle value={mode} onChange={setMode} />

      {mode === 'color' ? (
        <section className="control-section">
          <div className="color-wheel-wrap">
            <ColorWheel color={paintColor} onChange={setColor} />
          </div>
        </section>
      ) : (
        <WhiteScale value={whiteValue} onChange={setKelvin} />
      )}

      <Slider label="brightness" value={device.brightness} onChange={(value) => props.onChange({ ...device, brightness: value, on: value > 0 })} />

      {device.kind !== 'single' ? <ToolToggle value={tool} onChange={setTool} /> : null}
      {device.kind === 'multizone' ? (
        <MultizoneDraftEditor device={device} paintColor={activePaintColor} tool={tool} onPickColor={pickColor} onChange={props.onChange} />
      ) : null}
      {device.kind === 'matrix' ? (
        <MatrixDraftEditor device={device} paintColor={activePaintColor} tool={tool} onPickColor={pickColor} onChange={props.onChange} />
      ) : null}

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

function ModeToggle({ value, onChange }: { value: PaintMode; onChange: (value: PaintMode) => void }) {
  return (
    <div className="mode-toggle" role="tablist" aria-label="Color mode">
      {(['color', 'white'] as const).map((option) => (
        <button key={option} role="tab" aria-selected={value === option} data-active={value === option} onClick={() => onChange(option)}>
          {option}
        </button>
      ))}
    </div>
  );
}

function ToolToggle({ value, onChange }: { value: PaintTool; onChange: (value: PaintTool) => void }) {
  const tools: Array<{ id: PaintTool; label: string; icon: typeof Brush }> = [
    { id: 'brush', label: 'Brush', icon: Brush },
    { id: 'fill', label: 'Fill', icon: Droplet },
    { id: 'gradient', label: 'Gradient', icon: Wand2 },
    { id: 'picker', label: 'Picker', icon: Pipette },
  ];
  return (
    <div className="tool-toggle" aria-label="Paint tool">
      {tools.map((item) => {
        const Icon = item.icon;
        return (
          <button key={item.id} aria-label={item.label} title={item.label} data-active={value === item.id} onClick={() => onChange(item.id)}>
            <Icon size={14} strokeWidth={1.7} />
          </button>
        );
      })}
    </div>
  );
}

function WhiteScale({ value, onChange }: { value: number; onChange: (value: number) => void }) {
  return (
    <section className="control-section">
      <div className="temperature-label">
        <span>temperature</span>
        <span className="mono">{Math.round(2500 + value * 4000)}K</span>
      </div>
      <input
        className="temperature-scale"
        type="range"
        min={0}
        max={1000}
        value={Math.round(value * 1000)}
        onChange={(event) => onChange(Number(event.target.value) / 1000)}
        aria-label="Temperature"
      />
    </section>
  );
}

function MultizoneDraftEditor({
  device,
  paintColor,
  tool,
  onPickColor,
  onChange,
}: {
  device: Device;
  paintColor: HslColor;
  tool: PaintTool;
  onPickColor: (color: HslColor) => void;
  onChange: (device: Device) => void;
}) {
  const zones = device.zones ?? [];
  const dragPaintedRef = useRef<Set<number>>(new Set());
  const applyTool = (index: number) => {
    const next = [...zones];
    if (tool === 'picker') {
      onPickColor(zones[index]);
      return;
    }
    if (tool === 'fill') {
      onChange({ ...device, zones: zones.map(() => paintColor) });
      return;
    }
    if (tool === 'gradient') {
      onChange({ ...device, zones: applyLinearGradient(zones, index, paintColor) });
      return;
    }
    next[index] = paintColor;
    onChange({ ...device, zones: next });
  };
  const applyBrush = (index: number) => {
    if (dragPaintedRef.current.has(index)) return;
    dragPaintedRef.current.add(index);
    const next = [...zones];
    next[index] = paintColor;
    onChange({ ...device, zones: next });
  };
  const zoneFromPointer = (event: React.PointerEvent<HTMLDivElement>) => {
    const target = document.elementFromPoint(event.clientX, event.clientY);
    const button = target instanceof HTMLElement ? target.closest<HTMLButtonElement>('[data-zone-index]') : null;
    return button ? Number(button.dataset.zoneIndex) : null;
  };

  return (
    <section className="control-section">
      <h3>zones</h3>
      <div
        className="zone-editor"
        onPointerDown={(event) => {
          const index = zoneFromPointer(event);
          if (index == null) return;
          event.currentTarget.setPointerCapture(event.pointerId);
          if (tool !== 'brush') {
            applyTool(index);
            return;
          }
          dragPaintedRef.current = new Set();
          applyBrush(index);
        }}
        onPointerMove={(event) => {
          if (tool !== 'brush' || !event.currentTarget.hasPointerCapture(event.pointerId)) return;
          const index = zoneFromPointer(event);
          if (index != null) applyBrush(index);
        }}
        onPointerUp={(event) => {
          if (event.currentTarget.hasPointerCapture(event.pointerId)) event.currentTarget.releasePointerCapture(event.pointerId);
          dragPaintedRef.current = new Set();
        }}
        onPointerCancel={(event) => {
          if (event.currentTarget.hasPointerCapture(event.pointerId)) event.currentTarget.releasePointerCapture(event.pointerId);
          dragPaintedRef.current = new Set();
        }}
      >
        {zones.map((zone, index) => (
          <button
            key={index}
            data-zone-index={index}
            aria-label={`Paint zone ${index + 1}`}
            style={{ background: hsl(zone) }}
          />
        ))}
      </div>
    </section>
  );
}

function MatrixDraftEditor({
  device,
  paintColor,
  tool,
  onPickColor,
  onChange,
}: {
  device: Device;
  paintColor: HslColor;
  tool: PaintTool;
  onPickColor: (color: HslColor) => void;
  onChange: (device: Device) => void;
}) {
  const tiles = device.tiles ?? [];
  const dragPaintedRef = useRef<Set<string>>(new Set());
  const applyTool = (tileIndex: number, pixelIndex: number) => {
    const sourceTile = tiles[tileIndex];
    const sourcePixel = sourceTile.pixels[pixelIndex];
    if (tool === 'picker') {
      onPickColor(sourcePixel);
      return;
    }
    if (tool === 'fill') {
      onChange({ ...device, tiles: tiles.map((tile) => ({ ...tile, pixels: tile.pixels.map(() => paintColor) })) });
      return;
    }
    if (tool === 'gradient') {
      const allPixels = tiles.reduce((sum, tile) => sum + tile.pixels.length, 0);
      let cursor = 0;
      const targetOffset = tiles.slice(0, tileIndex).reduce((sum, tile) => sum + tile.pixels.length, 0) + pixelIndex;
      onChange({
        ...device,
        tiles: tiles.map((tile) => {
          const pixels = tile.pixels.map((pixel) => {
            const color = interpolateHsl(pixel, paintColor, 1 - Math.abs(cursor - targetOffset) / Math.max(1, allPixels - 1));
            cursor += 1;
            return color;
          });
          return { ...tile, pixels };
        }),
      });
      return;
    }
    onChange({
      ...device,
      tiles: tiles.map((tile, index) => {
        if (index !== tileIndex) return tile;
        const pixels = [...tile.pixels];
        pixels[pixelIndex] = paintColor;
        return { ...tile, pixels };
      }),
    });
  };
  const applyBrush = (tileIndex: number, pixelIndex: number) => {
    const key = `${tileIndex}:${pixelIndex}`;
    if (dragPaintedRef.current.has(key)) return;
    dragPaintedRef.current.add(key);
    onChange({
      ...device,
      tiles: tiles.map((tile, index) => {
        if (index !== tileIndex) return tile;
        const pixels = [...tile.pixels];
        pixels[pixelIndex] = paintColor;
        return { ...tile, pixels };
      }),
    });
  };
  const pixelFromPointer = (event: React.PointerEvent<HTMLDivElement>) => {
    const target = document.elementFromPoint(event.clientX, event.clientY);
    const button = target instanceof HTMLElement ? target.closest<HTMLButtonElement>('[data-tile-index][data-pixel-index]') : null;
    if (!button) return null;
    return {
      tileIndex: Number(button.dataset.tileIndex),
      pixelIndex: Number(button.dataset.pixelIndex),
    };
  };

  return (
    <section className="control-section">
      <h3>matrix</h3>
      <div
        className="matrix-editor"
        onPointerDown={(event) => {
          const hit = pixelFromPointer(event);
          if (!hit) return;
          event.currentTarget.setPointerCapture(event.pointerId);
          if (tool !== 'brush') {
            applyTool(hit.tileIndex, hit.pixelIndex);
            return;
          }
          dragPaintedRef.current = new Set();
          applyBrush(hit.tileIndex, hit.pixelIndex);
        }}
        onPointerMove={(event) => {
          if (tool !== 'brush' || !event.currentTarget.hasPointerCapture(event.pointerId)) return;
          const hit = pixelFromPointer(event);
          if (hit) applyBrush(hit.tileIndex, hit.pixelIndex);
        }}
        onPointerUp={(event) => {
          if (event.currentTarget.hasPointerCapture(event.pointerId)) event.currentTarget.releasePointerCapture(event.pointerId);
          dragPaintedRef.current = new Set();
        }}
        onPointerCancel={(event) => {
          if (event.currentTarget.hasPointerCapture(event.pointerId)) event.currentTarget.releasePointerCapture(event.pointerId);
          dragPaintedRef.current = new Set();
        }}
      >
        {tiles.map((tile, tileIndex) => (
          <div className="tile-editor" key={tile.id}>
            {tile.pixels.map((pixel, pixelIndex) => (
              <button
                key={pixelIndex}
                data-tile-index={tileIndex}
                data-pixel-index={pixelIndex}
                aria-label={`Paint tile ${tileIndex + 1} pixel ${pixelIndex + 1}`}
                style={{ background: hsl(pixel) }}
              />
            ))}
          </div>
        ))}
      </div>
    </section>
  );
}

function initialPaintColor(device: Device): HslColor {
  if (device.kind === 'single' && device.color) return device.color;
  if (device.kind === 'multizone' && device.zones?.length) return device.zones[Math.floor(device.zones.length / 2)];
  if (device.kind === 'matrix' && device.tiles?.[0]?.pixels.length) {
    const pixels = device.tiles[0].pixels;
    return pixels[Math.floor(pixels.length / 2)];
  }
  return { h: 38, s: 0.5, l: 0.55 };
}

function kelvinToHsl(kelvin: number): HslColor {
  const t = Math.max(0, Math.min(1, (kelvin - 2500) / 4000));
  if (t < 0.5) {
    const warm = t * 2;
    return { h: 28 + (38 - 28) * warm, s: 0.8 - (0.8 - 0.25) * warm, l: 0.7 + (0.92 - 0.7) * warm };
  }
  const cool = (t - 0.5) * 2;
  return { h: 38 + (210 - 38) * cool, s: 0.25 + (0.35 - 0.25) * cool, l: 0.92 };
}

function applyLinearGradient(colors: HslColor[], anchorIndex: number, paintColor: HslColor): HslColor[] {
  return colors.map((color, index) => interpolateHsl(color, paintColor, 1 - Math.abs(index - anchorIndex) / Math.max(1, colors.length - 1)));
}

function interpolateHsl(from: HslColor, to: HslColor, amount: number): HslColor {
  const t = Math.max(0, Math.min(1, amount));
  return {
    h: from.h + (to.h - from.h) * t,
    s: from.s + (to.s - from.s) * t,
    l: from.l + (to.l - from.l) * t,
  };
}
