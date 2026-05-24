import { useCallback, useRef, useState } from 'react';
import { ChevronRight } from 'lucide-react';
import type { HslColor } from '../domain/lifx';
import { hsl } from '../domain/lifx';
import './primitives.css';

interface SliderProps {
  value: number;
  onChange: (value: number) => void;
  label?: string;
}

export function Slider({ value, onChange, label }: SliderProps) {
  const percent = Math.round(value * 100);
  return (
    <label className="slider">
      {label ? (
        <span className="slider-label">
          <span>{label}</span>
          <span className="mono">{percent}%</span>
        </span>
      ) : null}
      <input
        aria-label={label ?? 'Brightness'}
        type="range"
        min={0}
        max={100}
        value={percent}
        onChange={(event) => onChange(Number(event.target.value) / 100)}
      />
    </label>
  );
}

interface PowerDotProps {
  on: boolean;
  onChange: (on: boolean) => void;
  size?: number;
}

export function PowerDot({ on, onChange, size = 9 }: PowerDotProps) {
  return (
    <button className="power-dot-button" aria-label={on ? 'Turn off' : 'Turn on'} onClick={() => onChange(!on)}>
      <span className="power-dot" data-on={on ? 'true' : 'false'} style={{ width: size, height: size }} />
    </button>
  );
}

export function StatusDot({ on, color }: { on: boolean; color?: HslColor }) {
  return <span className="status-dot" data-on={on ? 'true' : 'false'} style={{ background: on && color ? hsl(color) : undefined }} />;
}

export function RowChevron() {
  return <ChevronRight size={14} strokeWidth={1.6} />;
}

interface ColorWheelProps {
  color: HslColor;
  onChange: (color: HslColor) => void;
  size?: number;
}

export function ColorWheel({ color, onChange, size = 200 }: ColorWheelProps) {
  const ref = useRef<HTMLDivElement>(null);
  const draggingRef = useRef(false);
  const [dragging, setDragging] = useState(false);
  const radius = size / 2 - 14;
  const radians = ((color.h - 90) * Math.PI) / 180;
  const x = size / 2 + Math.cos(radians) * radius * color.s;
  const y = size / 2 + Math.sin(radians) * radius * color.s;

  const pick = useCallback(
    (clientX: number, clientY: number) => {
      const rect = ref.current?.getBoundingClientRect();
      if (!rect) return;
      const centerX = rect.left + rect.width / 2;
      const centerY = rect.top + rect.height / 2;
      const dx = clientX - centerX;
      const dy = clientY - centerY;
      const hue = (Math.atan2(dy, dx) * 180) / Math.PI + 450;
      const saturation = Math.min(1, Math.sqrt(dx * dx + dy * dy) / radius);
      onChange({ h: hue % 360, s: saturation, l: 0.55 });
    },
    [onChange, radius],
  );

  return (
    <div
      ref={ref}
      className="color-wheel"
      data-dragging={dragging ? 'true' : 'false'}
      style={{ width: size, height: size }}
      onPointerDown={(event) => {
        draggingRef.current = true;
        setDragging(true);
        event.currentTarget.setPointerCapture(event.pointerId);
        pick(event.clientX, event.clientY);
      }}
      onPointerMove={(event) => {
        if (draggingRef.current) pick(event.clientX, event.clientY);
      }}
      onPointerUp={(event) => {
        draggingRef.current = false;
        setDragging(false);
        event.currentTarget.releasePointerCapture(event.pointerId);
      }}
      onPointerCancel={() => {
        draggingRef.current = false;
        setDragging(false);
      }}
      role="slider"
      aria-label="Color"
      aria-valuetext={`${Math.round(color.h)} degrees, ${Math.round(color.s * 100)} percent saturation`}
    >
      <span className="color-wheel-thumb" style={{ left: x, top: y, background: hsl(color, 0.55) }} />
      <span className="color-wheel-readout mono" style={{ left: x, top: y < size * 0.35 ? y + 18 : y - 18 }}>
        {Math.round(color.h)}° · {Math.round(color.s * 100)}%
      </span>
    </div>
  );
}
