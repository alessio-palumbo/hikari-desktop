import { useEffect, useRef, useState } from 'react';
import { ArrowDown, ArrowDownLeft, ArrowDownRight, ArrowLeft, ArrowRight, ArrowUp, ArrowUpLeft, ArrowUpRight, Brush, Droplet, Info, LogOut, Pipette, RotateCcw, Undo2, Wand2, X } from 'lucide-react';
import type { Device, HslColor } from '../domain/lifx';
import { hsl, previewLightness, previewOpacity } from '../domain/lifx';
import { ColorWheel, Slider } from './primitives';
import './Inspector.css';

type PaintMode = 'color' | 'white';
type PaintTool = 'brush' | 'fill' | 'gradient' | 'picker';
type GradientStops = { start?: HslColor; end?: HslColor };
type GradientDirection = 'e' | 'w' | 's' | 'n' | 'se' | 'nw' | 'ne' | 'sw';

interface InspectorProps {
  device?: Device;
  editing: boolean;
  dirty: boolean;
  canUndo: boolean;
  saving: boolean;
  error?: string;
  onClose: () => void;
  onChange: (device: Device) => void;
  onEnterEditMode: () => void;
  onExitEditMode: () => void;
  onApply: () => void;
  onRevert: () => void;
  onUndo: () => void;
}

export function Inspector(props: InspectorProps) {
  if (!props.device) {
    return null;
  }

  const device = props.device;
  const [mode, setMode] = useState<PaintMode>('color');
  const [tool, setTool] = useState<PaintTool | null>(null);
  const [paintColor, setPaintColor] = useState(() => initialPaintColor(device));
  const [whiteKelvin, setWhiteKelvin] = useState(() => clampKelvin(device.kelvin ?? 3500, device));
  const [whiteColor, setWhiteColor] = useState(() => kelvinToHsl(clampKelvin(device.kelvin ?? 3500, device)));
  const [gradientStops, setGradientStops] = useState<GradientStops>({});
  const [gradientDirection, setGradientDirection] = useState<GradientDirection>('e');
  const [showInfo, setShowInfo] = useState(false);
  const hasColor = device.capability?.hasColor ?? true;

  useEffect(() => {
    setMode((device.capability?.hasColor ?? true) ? 'color' : 'white');
    setTool(null);
    setPaintColor(initialPaintColor(device));
    const kelvin = clampKelvin(device.kelvin ?? 3500, device);
    setWhiteKelvin(kelvin);
    setWhiteColor(kelvinToHsl(kelvin));
    setGradientStops({});
    setGradientDirection('e');
    setShowInfo(false);
  }, [device.serial]);

  useEffect(() => {
    if (!props.editing) {
      setTool(null);
      return;
    }
    setPaintColor((current) => ({ ...current, l: device.brightness }));
    setWhiteColor((current) => ({ ...current, l: device.brightness }));
  }, [device.brightness, props.editing]);

  const kelvinMin = device.capability?.kelvinMin ?? 2500;
  const kelvinMax = device.capability?.kelvinMax ?? 6500;
  const whiteValue = Math.max(0, Math.min(1, (whiteKelvin - kelvinMin) / Math.max(1, kelvinMax - kelvinMin)));
  const activePaintColor = mode === 'white' ? whiteColor : paintColor;
  const brightnessValue = props.editing ? activePaintColor.l : device.brightness;

  const setColor = (color: HslColor) => {
    const next = { ...color, l: brightnessValue };
    setPaintColor(next);
    recordGradientColor(next);
    if (!props.editing && hasColor) props.onChange(applyDeviceColor(device, next));
  };

  const setKelvin = (value: number) => {
    const nextKelvin = Math.round(kelvinMin + value * (kelvinMax - kelvinMin));
    const color = { ...kelvinToHsl(nextKelvin), l: brightnessValue };
    setWhiteKelvin(nextKelvin);
    setWhiteColor(color);
    recordGradientColor(color);
    if (!props.editing) props.onChange(applyDeviceColor(device, color));
  };

  const setBrightness = (value: number) => {
    if (!props.editing) {
      props.onChange(applyDeviceBrightness(device, value));
      return;
    }
    if (mode === 'white') setWhiteColor((current) => ({ ...current, l: value }));
    else setPaintColor((current) => ({ ...current, l: value }));
  };

  const pickColor = (color: HslColor) => {
    if (color.kelvin && color.s === 0) {
      setMode('white');
      setWhiteKelvin(clampKelvin(color.kelvin, device));
      setWhiteColor(color);
      return;
    }
    setMode('color');
    setPaintColor(color);
  };

  const chooseTool = (next: PaintTool) => {
    if (!props.editing) props.onEnterEditMode();
    if (next === 'gradient' && tool !== 'gradient') setGradientStops({});
    setTool(next);
  };

  const recordGradientColor = (color: HslColor) => {
    if (!props.editing || tool !== 'gradient') return;
    setGradientStops((current) => {
      if (!current.start) return { start: color };
      return { start: current.end ?? current.start, end: color };
    });
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
        <button className="info-toggle" type="button" aria-label="Device info" aria-expanded={showInfo} data-active={showInfo ? 'true' : 'false'} onClick={() => setShowInfo((value) => !value)}>
          <Info size={13} />
        </button>
      </div>

      {showInfo ? <DeviceInfo device={device} /> : null}

      <ModeToggle value={mode} hasColor={hasColor} onChange={setMode} />

      {mode === 'color' ? (
        <section className="control-section">
          <div className="color-wheel-wrap">
            <ColorWheel color={paintColor} onChange={setColor} />
          </div>
        </section>
      ) : (
        <WhiteScale value={whiteValue} kelvinMin={kelvinMin} kelvinMax={kelvinMax} onChange={setKelvin} />
      )}

      <Slider label={props.editing ? 'paint brightness' : 'brightness'} value={brightnessValue} onChange={setBrightness} />
      {props.error ? <div className="inspector-error">{props.error}</div> : null}

      {device.kind !== 'single' ? (
        <section className="edit-tools-section" data-editing={props.editing ? 'true' : 'false'}>
          <div className="edit-tools-header">
            <span>{props.editing ? 'editing layout' : 'layout tools'}</span>
            <button type="button" className="exit-edit-button" disabled={!props.editing} aria-hidden={!props.editing} tabIndex={props.editing ? 0 : -1} onClick={props.onExitEditMode}>
              <LogOut size={13} />
              <span>exit</span>
            </button>
          </div>
          <ToolToggle value={tool} onChange={chooseTool} />
          {props.editing && tool === 'gradient' ? (
            <GradientControls deviceKind={device.kind} stops={gradientStops} direction={gradientDirection} onDirectionChange={setGradientDirection} />
          ) : null}
          {props.editing && device.kind === 'multizone' ? (
            <MultizoneDraftEditor
              device={device}
              paintColor={activePaintColor}
              gradientStops={gradientStops}
              gradientDirection={gradientDirection}
              tool={tool}
              onPickColor={pickColor}
              onChange={props.onChange}
            />
          ) : null}
          {props.editing && device.kind === 'matrix' ? (
            <MatrixDraftEditor
              device={device}
              paintColor={activePaintColor}
              gradientStops={gradientStops}
              gradientDirection={gradientDirection}
              tool={tool}
              onPickColor={pickColor}
              onChange={props.onChange}
            />
          ) : null}
          {props.editing ? (
            <footer className="draft-bar">
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
        </section>
      ) : null}
    </aside>
  );
}

function DeviceInfo({ device }: { device: Device }) {
  const rows = [
    ['type', deviceKindLabel(device)],
    ...deviceShapeRows(device),
    ['ip', device.ipAddress || 'unknown'],
    ['product id', device.productId ? String(device.productId) : 'unknown'],
    ['firmware', device.firmware || 'unknown'],
    ['rssi', formatRSSI(device)],
  ];
  return (
    <dl className="device-info">
      {rows.map(([label, value]) => (
        <div key={label}>
          <dt>{label}</dt>
          <dd>{value}</dd>
        </div>
      ))}
    </dl>
  );
}

function deviceShapeRows(device: Device): string[][] {
  if (device.kind === 'multizone') {
    return [['zones', String(device.zoneCount ?? device.zones?.length ?? 'unknown')]];
  }
  if (device.kind === 'matrix') {
    const pixelCount = device.pixelCount ?? device.chain?.[0]?.pixels.length;
    const chainLength = device.chainLength ?? device.chain?.length;
    const rows = [['pixels', pixelCount === undefined ? 'unknown' : String(pixelCount)]];
    if (chainLength !== undefined && chainLength > 1) rows.push(['chain length', String(chainLength)]);
    return rows;
  }
  return [];
}

function deviceKindLabel(device: Device): string {
  if (device.kind === 'single') return 'single zone';
  return device.kind;
}

function formatRSSI(device: Device): string {
  if (device.rssi === undefined && !device.rssiText) return 'unknown';
  if (device.rssi === undefined) return device.rssiText ?? 'unknown';
  return device.rssiText ? `${device.rssi} (${device.rssiText})` : String(device.rssi);
}

function GradientControls({
  deviceKind,
  stops,
  direction,
  onDirectionChange,
}: {
  deviceKind: Device['kind'];
  stops: GradientStops;
  direction: GradientDirection;
  onDirectionChange: (direction: GradientDirection) => void;
}) {
  const directions: Array<{ id: GradientDirection; label: string; icon: typeof ArrowRight }> =
    deviceKind === 'multizone'
      ? [
          { id: 'e', label: 'Forward', icon: ArrowRight },
          { id: 'w', label: 'Reverse', icon: ArrowLeft },
        ]
      : [
          { id: 'e', label: 'Right', icon: ArrowRight },
          { id: 'se', label: 'Down right', icon: ArrowDownRight },
          { id: 's', label: 'Down', icon: ArrowDown },
          { id: 'sw', label: 'Down left', icon: ArrowDownLeft },
          { id: 'w', label: 'Left', icon: ArrowLeft },
          { id: 'nw', label: 'Up left', icon: ArrowUpLeft },
          { id: 'n', label: 'Up', icon: ArrowUp },
          { id: 'ne', label: 'Up right', icon: ArrowUpRight },
        ];
  return (
    <div className="gradient-controls">
      <div className="gradient-stops" aria-label="Gradient colors">
        <span className="gradient-stop" data-empty={!stops.start ? 'true' : 'false'} style={{ background: stops.start ? hsl(stops.start) : undefined }} />
        <span className="gradient-arrow" />
        <span className="gradient-stop" data-empty={!stops.end ? 'true' : 'false'} style={{ background: stops.end ? hsl(stops.end) : undefined }} />
      </div>
      <div className="gradient-directions" data-kind={deviceKind} aria-label="Gradient direction">
        {directions.map((item) => {
          const Icon = item.icon;
          return (
            <button key={item.id} type="button" aria-label={item.label} title={item.label} data-active={direction === item.id} onClick={() => onDirectionChange(item.id)}>
              <Icon size={10} strokeWidth={1.8} />
            </button>
          );
        })}
      </div>
    </div>
  );
}

export function ModeToggle({ value, hasColor, onChange }: { value: PaintMode; hasColor: boolean; onChange: (value: PaintMode) => void }) {
  const options: PaintMode[] = hasColor ? ['color', 'white'] : ['white'];
  return (
    <div className="mode-toggle" role="tablist" aria-label="Color mode">
      {options.map((option) => (
        <button key={option} role="tab" aria-selected={value === option} data-active={value === option} onClick={() => onChange(option)}>
          {option}
        </button>
      ))}
    </div>
  );
}

function ToolToggle({ value, onChange }: { value: PaintTool | null; onChange: (value: PaintTool) => void }) {
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

export function WhiteScale({ value, kelvinMin, kelvinMax, onChange }: { value: number; kelvinMin: number; kelvinMax: number; onChange: (value: number) => void }) {
  return (
    <section className="control-section">
      <div className="temperature-label">
        <span>temperature</span>
        <span className="mono">{Math.round(kelvinMin + value * (kelvinMax - kelvinMin))}K</span>
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
  gradientStops,
  gradientDirection,
  tool,
  onPickColor,
  onChange,
}: {
  device: Device;
  paintColor: HslColor;
  gradientStops: GradientStops;
  gradientDirection: GradientDirection;
  tool: PaintTool | null;
  onPickColor: (color: HslColor) => void;
  onChange: (device: Device) => void;
}) {
  const zones = device.zones ?? [];
  const dragPaintedRef = useRef<Set<number>>(new Set());
  const applyTool = (index?: number) => {
    if (!tool) return;
    const next = [...zones];
    if (tool === 'picker') {
      if (index == null) return;
      onPickColor(zones[index]);
      return;
    }
    if (tool === 'fill') {
      onChange({ ...device, zones: zones.map(() => paintColor) });
      return;
    }
    if (tool === 'gradient') {
      const gradient = applyMultizoneGradient(zones, gradientStops, gradientDirection);
      if (gradient) onChange({ ...device, zones: gradient });
      return;
    }
    if (index == null) return;
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
        data-editing="true"
        onPointerDown={(event) => {
          if (!tool) return;
          const index = zoneFromPointer(event);
          if (index == null && tool !== 'fill' && tool !== 'gradient') return;
          event.currentTarget.setPointerCapture(event.pointerId);
          if (tool !== 'brush') {
            applyTool(index ?? undefined);
            return;
          }
          if (index == null) return;
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
            style={{ background: hsl(zone, previewLightness(zone, zone.l || device.brightness, device.on)), opacity: previewOpacity(device.on) }}
          />
        ))}
      </div>
    </section>
  );
}

function MatrixDraftEditor({
  device,
  paintColor,
  gradientStops,
  gradientDirection,
  tool,
  onPickColor,
  onChange,
}: {
  device: Device;
  paintColor: HslColor;
  gradientStops: GradientStops;
  gradientDirection: GradientDirection;
  tool: PaintTool | null;
  onPickColor: (color: HslColor) => void;
  onChange: (device: Device) => void;
}) {
  const chain = device.chain ?? [];
  const dragPaintedRef = useRef<Set<string>>(new Set());
  const applyTool = (matrixIndex: number, pixelIndex?: number) => {
    if (!tool) return;
    const sourceMatrix = chain[matrixIndex];
    if (tool === 'picker') {
      if (pixelIndex == null) return;
      const sourcePixel = sourceMatrix.pixels[pixelIndex];
      onPickColor(sourcePixel);
      return;
    }
    if (tool === 'fill') {
      onChange({
        ...device,
        chain: chain.map((matrix, index) => (index === matrixIndex ? { ...matrix, pixels: matrix.pixels.map(() => paintColor) } : matrix)),
      });
      return;
    }
    if (tool === 'gradient') {
      const gradient = applyMatrixGradient(sourceMatrix, gradientStops, gradientDirection);
      if (!gradient) return;
      onChange({
        ...device,
        chain: chain.map((matrix, index) => (index === matrixIndex ? { ...matrix, pixels: gradient } : matrix)),
      });
      return;
    }
    if (pixelIndex == null) return;
    onChange({
      ...device,
      chain: chain.map((matrix, index) => {
        if (index !== matrixIndex) return matrix;
        const pixels = [...matrix.pixels];
        pixels[pixelIndex] = paintColor;
        return { ...matrix, pixels };
      }),
    });
  };
  const applyBrush = (matrixIndex: number, pixelIndex: number) => {
    const key = `${matrixIndex}:${pixelIndex}`;
    if (dragPaintedRef.current.has(key)) return;
    dragPaintedRef.current.add(key);
    onChange({
      ...device,
      chain: chain.map((matrix, index) => {
        if (index !== matrixIndex) return matrix;
        const pixels = [...matrix.pixels];
        pixels[pixelIndex] = paintColor;
        return { ...matrix, pixels };
      }),
    });
  };
  const pixelFromPointer = (event: React.PointerEvent<HTMLDivElement>) => {
    const target = document.elementFromPoint(event.clientX, event.clientY);
    const button = target instanceof HTMLElement ? target.closest<HTMLButtonElement>('[data-matrix-index][data-pixel-index]') : null;
    if (!button) return null;
    return {
      matrixIndex: Number(button.dataset.matrixIndex),
      pixelIndex: Number(button.dataset.pixelIndex),
    };
  };
  const matrixIndexFromPointer = (event: React.PointerEvent<HTMLDivElement>) => {
    const target = document.elementFromPoint(event.clientX, event.clientY);
    const entry = target instanceof HTMLElement ? target.closest<HTMLElement>('[data-matrix-entry-index]') : null;
    if (entry) return Number(entry.dataset.matrixEntryIndex);
    return nearestMatrixIndex(event);
  };
  const nearestMatrixIndex = (event: React.PointerEvent<HTMLDivElement>) => {
    const entries = Array.from(event.currentTarget.querySelectorAll<HTMLElement>('[data-matrix-entry-index]'));
    if (!entries.length) return null;
    let nearest = Number(entries[0].dataset.matrixEntryIndex);
    let nearestDistance = Number.POSITIVE_INFINITY;
    for (const entry of entries) {
      const rect = entry.getBoundingClientRect();
      const clampedX = Math.max(rect.left, Math.min(event.clientX, rect.right));
      const clampedY = Math.max(rect.top, Math.min(event.clientY, rect.bottom));
      const distance = (event.clientX - clampedX) ** 2 + (event.clientY - clampedY) ** 2;
      if (distance < nearestDistance) {
        nearestDistance = distance;
        nearest = Number(entry.dataset.matrixEntryIndex);
      }
    }
    return nearest;
  };

  return (
    <section className="control-section">
      <h3>matrix</h3>
      <div
        className="matrix-editor"
        data-editing="true"
        onPointerDown={(event) => {
          if (!tool) return;
          const hit = pixelFromPointer(event);
          const surfaceMatrixIndex = hit?.matrixIndex ?? (tool === 'fill' || tool === 'gradient' ? matrixIndexFromPointer(event) : null);
          if (surfaceMatrixIndex == null) return;
          event.currentTarget.setPointerCapture(event.pointerId);
          if (tool !== 'brush') {
            applyTool(surfaceMatrixIndex, hit?.pixelIndex);
            return;
          }
          if (!hit) return;
          dragPaintedRef.current = new Set();
          applyBrush(hit.matrixIndex, hit.pixelIndex);
        }}
        onPointerMove={(event) => {
          if (tool !== 'brush' || !event.currentTarget.hasPointerCapture(event.pointerId)) return;
          const hit = pixelFromPointer(event);
          if (hit) applyBrush(hit.matrixIndex, hit.pixelIndex);
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
        {chain.map((matrix, matrixIndex) => (
          <div
            className="matrix-chain-entry"
            key={matrix.id}
            data-matrix-entry-index={matrixIndex}
            style={{ gridTemplateColumns: `repeat(${matrixGridCols(matrix)}, 8px)`, gridTemplateRows: `repeat(${matrix.rows.length}, 8px)` }}
          >
            {matrix.rows.flatMap((row, rowIndex) =>
              Array.from({ length: row.cols }, (_, columnIndex) => {
                const pixelIndex = matrix.rows.slice(0, rowIndex).reduce((sum, entry) => sum + entry.cols, 0) + columnIndex;
                if (row.hiddenCols?.includes(columnIndex)) return null;
                const pixel = matrix.pixels[pixelIndex];
                return (
                  <button
                    key={pixelIndex}
                    data-matrix-index={matrixIndex}
                    data-pixel-index={pixelIndex}
                    aria-label={`Paint matrix ${matrixIndex + 1} pixel ${pixelIndex + 1}`}
                    style={{
                      gridColumn: Math.round(row.offset + columnIndex) + 1,
                      gridRow: rowIndex + 1,
                      background: hsl(pixel, previewLightness(pixel, pixel.l || device.brightness, device.on)),
                      opacity: previewOpacity(device.on),
                    }}
                  />
                );
              }),
            )}
          </div>
        ))}
      </div>
    </section>
  );
}

export function initialPaintColor(device: Device): HslColor {
  if (device.kind === 'single' && device.color) return device.color;
  if (device.kind === 'multizone' && device.zones?.length) return device.zones[Math.floor(device.zones.length / 2)];
  if (device.kind === 'matrix' && device.chain?.[0]?.pixels.length) {
    const pixels = device.chain[0].pixels;
    return pixels[Math.floor(pixels.length / 2)];
  }
  return { h: 38, s: 0.5, l: 0.55 };
}

function matrixGridCols(matrix: NonNullable<Device['chain']>[number]): number {
  return Math.max(1, ...matrix.rows.map((row) => Math.ceil(row.offset + row.cols)), Math.round(matrix.w));
}

export function kelvinToHsl(kelvin: number): HslColor {
  return { h: 0, s: 0, l: 0.72, kelvin };
}

function clampKelvin(kelvin: number, device: Device): number {
  const min = device.capability?.kelvinMin ?? 2500;
  const max = device.capability?.kelvinMax ?? 6500;
  return Math.max(min, Math.min(max, kelvin));
}

export function applyDeviceColor(device: Device, color: HslColor): Device {
  const brightness = device.brightness > 0 ? device.brightness : Math.max(color.l, 0.55);
  const nextColor = { ...color, l: brightness };
  if (device.kind === 'single') {
    return { ...device, on: true, brightness, color: nextColor, kelvin: nextColor.kelvin ?? device.kelvin };
  }
  if (device.kind === 'multizone') {
    return {
      ...device,
      on: true,
      brightness,
      color: nextColor,
      kelvin: nextColor.kelvin ?? device.kelvin,
      zones: device.zones?.map(() => nextColor) ?? [],
    };
  }
  return {
    ...device,
    on: true,
    brightness,
    color: nextColor,
    kelvin: nextColor.kelvin ?? device.kelvin,
    chain: device.chain?.map((matrix) => ({ ...matrix, pixels: matrix.pixels.map(() => nextColor) })) ?? [],
  };
}

export function applyDeviceBrightness(device: Device, brightness: number): Device {
  const on = brightness > 0;
  const withBrightness = (color: HslColor): HslColor => ({ ...color, l: brightness });
  if (device.kind === 'single') {
    return { ...device, on, brightness, color: device.color ? withBrightness(device.color) : device.color };
  }
  if (device.kind === 'multizone') {
    return { ...device, on, brightness, zones: device.zones?.map(withBrightness) ?? [] };
  }
  return {
    ...device,
    on,
    brightness,
    chain: device.chain?.map((matrix) => ({ ...matrix, pixels: matrix.pixels.map(withBrightness) })) ?? [],
  };
}

function applyMultizoneGradient(colors: HslColor[], stops: GradientStops, direction: GradientDirection): HslColor[] | undefined {
  if (!stops.start || !stops.end) return undefined;
  return colors.map((_, index) => {
    const t = index / Math.max(1, colors.length - 1);
    return interpolateHsl(stops.start!, stops.end!, direction === 'w' ? 1 - t : t);
  });
}

function applyMatrixGradient(matrix: NonNullable<Device['chain']>[number], stops: GradientStops, direction: GradientDirection): HslColor[] | undefined {
  if (!stops.start || !stops.end) return undefined;
  const pixels = [...matrix.pixels];
  const width = matrixGridCols(matrix);
  const height = Math.max(1, matrix.rows.length);
  for (const [rowIndex, row] of matrix.rows.entries()) {
    const rowStart = matrix.rows.slice(0, rowIndex).reduce((sum, entry) => sum + entry.cols, 0);
    for (let columnIndex = 0; columnIndex < row.cols; columnIndex += 1) {
      const pixelIndex = rowStart + columnIndex;
      const x = row.offset + columnIndex;
      const y = rowIndex;
      pixels[pixelIndex] = interpolateHsl(stops.start, stops.end, gradientAmount(x, y, width, height, direction));
    }
  }
  return pixels;
}

function gradientAmount(x: number, y: number, width: number, height: number, direction: GradientDirection): number {
  const maxX = Math.max(1, width - 1);
  const maxY = Math.max(1, height - 1);
  const nx = x / maxX;
  const ny = y / maxY;
  const vectors: Record<GradientDirection, { x: number; y: number }> = {
    e: { x: 1, y: 0 },
    w: { x: -1, y: 0 },
    s: { x: 0, y: 1 },
    n: { x: 0, y: -1 },
    se: { x: 1, y: 1 },
    nw: { x: -1, y: -1 },
    ne: { x: 1, y: -1 },
    sw: { x: -1, y: 1 },
  };
  const vector = vectors[direction];
  const projection = nx * vector.x + ny * vector.y;
  const corners = [
    0,
    vector.x,
    vector.y,
    vector.x + vector.y,
  ];
  const min = Math.min(...corners);
  const max = Math.max(...corners);
  return (projection - min) / Math.max(1, max - min);
}

function interpolateHsl(from: HslColor, to: HslColor, amount: number): HslColor {
  const t = Math.max(0, Math.min(1, amount));
  return {
    h: from.h + (to.h - from.h) * t,
    s: from.s + (to.s - from.s) * t,
    l: from.l + (to.l - from.l) * t,
  };
}
