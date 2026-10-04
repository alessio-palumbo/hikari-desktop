import { useState } from 'react';
import { ArrowLeft, ArrowRight, Plus, Save, Trash2, X } from 'lucide-react';
import { effectPreviewColor } from '../domain/lifx';
import { moveThemeColor, themeColors, type SaveUserThemeRequest, type ThemeColor } from '../domain/themes';
import { ColorWheel } from './primitives';

export function ThemeEditor({ draft, saving, error, onChange, onSave, onCancel }: {
  draft: SaveUserThemeRequest;
  saving: boolean;
  error: string;
  onChange: (draft: SaveUserThemeRequest) => void;
  onSave: () => void;
  onCancel: () => void;
}) {
  const [selected, setSelected] = useState(0);
  const [mode, setMode] = useState<'color' | 'white'>(() => themeColors(draft.theme)[0].Saturation === 0 ? 'white' : 'color');
  const colors = themeColors(draft.theme);
  const stop = colors[selected];
  const changeColors = (Base: ThemeColor[]) => onChange({ ...draft, theme: { ...draft.theme, palette: { Base } } });
  const update = (color: ThemeColor) => changeColors(colors.map((current, index) => index === selected ? color : current));
  const move = (direction: number) => {
    const destination = selected + direction;
    onChange({ ...draft, theme: moveThemeColor(draft.theme, selected, destination) });
    setSelected(destination);
  };
  return <div className="theme-editor" onKeyDown={(event) => {
    if (event.key === 'Escape') {
      event.preventDefault(); event.stopPropagation();
      if (!saving) onCancel();
    }
  }}>
    <header className="theme-editor-header">
      <input aria-label="Theme name" maxLength={128} value={draft.theme.name} disabled={saving} onChange={(event) => onChange({ ...draft, theme: { ...draft.theme, name: event.target.value } })} onKeyDown={(event) => {
        if (event.key === 'Enter' && !saving && draft.theme.name.trim()) { event.preventDefault(); event.stopPropagation(); onSave(); }
      }} autoFocus />
      <button type="button" className="icon-button" aria-label="Cancel theme editing" title="Cancel theme editing" disabled={saving} onClick={onCancel}><X size={14} /></button>
      <button type="button" className="icon-button" aria-label="Save theme" title="Save theme" disabled={saving || !draft.theme.name.trim()} onClick={onSave}><Save size={14} /></button>
    </header>
    <fieldset disabled={saving}>
      <div className="theme-palette" role="group" aria-label="Palette colors">
        {colors.map((color, index) => <button type="button" key={index} className="theme-color" data-active={index === selected} aria-label={`Color ${index + 1}`} aria-pressed={index === selected} onClick={() => { setSelected(index); setMode(color.Saturation === 0 ? 'white' : 'color'); }}>
          <i style={{ background: themeSwatchColor(color) }} />
        </button>)}
        <button type="button" className="icon-button" title="Add color" aria-label="Add palette color" disabled={colors.length >= 64} onClick={() => { changeColors([...colors, { ...stop }]); setSelected(colors.length); }}><Plus size={13} /></button>
      </div>
      <div className="theme-color-actions">
        <span>color {selected + 1}</span>
        <button type="button" className="icon-button" aria-label="Move color left" title="Move color left" disabled={selected === 0} onClick={() => move(-1)}><ArrowLeft size={13} /></button>
        <button type="button" className="icon-button" aria-label="Move color right" title="Move color right" disabled={selected === colors.length - 1} onClick={() => move(1)}><ArrowRight size={13} /></button>
        <button type="button" className="icon-button" aria-label="Remove palette color" title="Remove palette color" disabled={colors.length <= 1} onClick={() => { changeColors(colors.filter((_, index) => index !== selected)); setSelected(Math.min(selected, colors.length - 2)); }}><Trash2 size={13} /></button>
      </div>
      <div className="mode-toggle" role="tablist" aria-label="Palette color mode">
        {(['color', 'white'] as const).map((value) => <button key={value} type="button" role="tab" aria-selected={value === mode} data-active={value === mode} onClick={() => setMode(value)}>{value}</button>)}
      </div>
      {mode === 'color' ? <div className="color-wheel-wrap"><ColorWheel color={{ h: stop.Hue, s: stop.Saturation / 100, l: 1 }} onChange={(color) => update({ ...stop, Hue: color.h, Saturation: color.s * 100 })} /></div> : <section className="control-section">
        <div className="temperature-label"><span>temperature</span><span className="mono">{stop.Kelvin}K</span></div>
        <input className="temperature-scale" type="range" aria-label="Palette temperature" min={1500} max={9000} step={50} value={stop.Kelvin} onChange={(event) => update({ ...stop, Saturation: 0, Kelvin: Number(event.target.value) })} />
      </section>}
    </fieldset>
    {error ? <div className="inspector-error" role="alert">{error}</div> : null}
  </div>;
}

export function themeSwatchColor(color: ThemeColor): string {
  return effectPreviewColor({ h: color.Hue, s: color.Saturation / 100, l: 1, kelvin: color.Kelvin });
}
