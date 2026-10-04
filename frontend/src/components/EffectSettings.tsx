import { useEffect, useState } from 'react';
import { Check, RotateCcw } from 'lucide-react';
import { getDeviceEffectParameters, type EffectParameter } from '../backend/api';
import { effectSettingsDiffer, formatEffectSpeed, quantizeEffectParameter, speedToUnit, unitToSpeedMs, type DeviceEffectDefinition, type EffectParameters } from '../domain/effects';
import './EffectSettings.css';

export function EffectSettings({ serial, effect, speedMs, appliedSpeedMs, appliedValues, onSpeedChange, values, disabled, onChange, onApply }: {
  serial: string;
  effect: DeviceEffectDefinition;
  speedMs: number;
  appliedSpeedMs: number;
  appliedValues?: EffectParameters;
  onSpeedChange: (speedMs: number) => void;
  values?: EffectParameters;
  disabled: boolean;
  onChange: (values: EffectParameters) => void;
  onApply: (values?: EffectParameters) => void;
}) {
  const [parameters, setParameters] = useState<EffectParameter[]>([]);
  const [error, setError] = useState('');
  const [loaded, setLoaded] = useState(!effect.configurable);
  useEffect(() => {
    if (disabled || !effect.configurable) return;
    let disposed = false;
    setError('');
    void getDeviceEffectParameters(serial, effect.id).then((result) => {
      if (disposed) return;
      setParameters(result);
      setLoaded(true);
      if (!values) onChange(appliedValues ?? Object.fromEntries(result.map((param) => [param.key, param.value])));
    }).catch((failure) => { if (!disposed) setError(String(failure instanceof Error ? failure.message : failure)); });
    return () => { disposed = true; };
  }, [serial, effect.id, disabled]);
  const current = values ?? Object.fromEntries(parameters.map((param) => [param.key, param.value]));
  const dirty = effectSettingsDiffer(speedMs, appliedSpeedMs, current, appliedValues ?? Object.fromEntries(parameters.map((param) => [param.key, param.value])));
  const changedDefaults = effectSettingsDiffer(speedMs, effect.speed.defaultMs, current, Object.fromEntries(parameters.map((param) => [param.key, param.default])));
  return <div className="effect-settings" onClick={(event) => event.stopPropagation()}>
    <label>
      <span>speed<output>{formatEffectSpeed(speedMs)}</output></span>
      <input type="range" aria-label={`${effect.label} speed`} aria-valuetext={formatEffectSpeed(speedMs)} min={0} max={100} value={Math.round(speedToUnit(speedMs, effect.speed) * 100)} disabled={disabled} onChange={(event) => onSpeedChange(unitToSpeedMs(Number(event.target.value) / 100, effect.speed))} />
    </label>
    {error ? <div className="inspector-error" role="status">{error}</div> : parameters.map((param) => <label key={param.key}>
      <span><span className={param.description ? 'effect-setting-help' : undefined} title={param.description}>{param.label}</span><output>{Math.round(current[param.key] * 100)}%</output></span>
      <input type="range" aria-label={`${effect.label} ${param.label}`} aria-valuetext={`${Math.round(current[param.key] * 100)}%`} min={param.min} max={param.max} step="any" value={current[param.key]} disabled={disabled} onChange={(event) => onChange({ ...current, [param.key]: quantizeEffectParameter(Number(event.target.value), param) })} />
    </label>)}
    {loaded ? <div className="effect-settings-actions">
      <button type="button" title="Reset settings to Hikari defaults" aria-label="Reset effect settings" disabled={disabled || !changedDefaults} onClick={() => { onSpeedChange(effect.speed.defaultMs); if (effect.configurable) onChange(Object.fromEntries(parameters.map((param) => [param.key, param.default]))); }}><RotateCcw size={12} /></button>
      <button type="button" className="effect-settings-apply" disabled={disabled || !dirty || !!error} onClick={() => onApply(effect.configurable ? current : undefined)}><Check size={12} />apply</button>
    </div> : !error ? <span className="effect-settings-loading">loading…</span> : null}
  </div>;
}
