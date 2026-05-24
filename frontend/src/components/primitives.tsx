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
