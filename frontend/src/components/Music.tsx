import { useEffect, useRef, useState, type ReactNode } from 'react';
import { AudioLines, ChevronDown, LoaderCircle, Play, RefreshCw, Square, X } from 'lucide-react';
import { getMusicSettings, getMusicInputs } from '../backend/api';
import type { Device } from '../domain/lifx';
import type { SceneCaptureScope } from '../domain/scenes';
import { musicActive, type MusicStatus, type MusicSettings, type MusicInputDevice, type MusicSelection, type StartMusicRequest } from '../domain/music';
import { CenterViewToggle, type CenterView } from './CenterViewToggle';
import './Music.css';

interface MusicProps {
  devices: Device[];
  scopes: SceneCaptureScope[];
  defaultScopeId?: string;
  query: string;
  status: MusicStatus;
  selection: MusicSelection;
  onSelectionChange: (selection: MusicSelection) => void;
  view: CenterView;
  onViewChange: (view: CenterView) => void;
  onStart: (request: StartMusicRequest) => Promise<void>;
  onStop: () => Promise<void>;
}

export function Music(props: MusicProps) {
  const [settings, setSettings] = useState<MusicSettings>({ available: false, styles: [] });
  const [inputs, setInputs] = useState<MusicInputDevice[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState('');
  const { serials, inputId, style, dynamics, sensitivity } = props.selection;
  const setSerials = (next: string[] | ((current: string[]) => string[])) => props.onSelectionChange({ ...props.selection, serials: typeof next === 'function' ? next(serials) : next });
  const setInputId = (inputId: string) => props.onSelectionChange({ ...props.selection, inputId });
  const setStyle = (style: string) => props.onSelectionChange({ ...props.selection, style });
  const setDynamics = (dynamics: string) => props.onSelectionChange({ ...props.selection, dynamics });
  const setSensitivity = (sensitivity: string) => props.onSelectionChange({ ...props.selection, sensitivity });
  const initialized = useRef(false);
  const active = musicActive(props.status);
  const displayedSerials = active ? props.status.serials : serials;

  useEffect(() => {
    if (initialized.current || serials.length) return;
    const scope = props.scopes.find((entry) => entry.id === props.defaultScopeId);
    if (active || scope) { setSerials(active ? props.status.serials : scope!.serials); initialized.current = true; }
  }, [props.scopes, props.defaultScopeId, props.status.serials, active, serials.length]);

  useEffect(() => {
    let disposed = false;
    void getMusicSettings().then(async (next) => {
      if (disposed) return;
      setSettings(next);
      if (next.available) { const devices = await getMusicInputs(); if (!disposed) setInputs(devices); }
    }).catch((failure) => { if (!disposed) setError(message(failure)); }).finally(() => { if (!disposed) setLoading(false); });
    return () => { disposed = true; };
  }, []);

  const refreshInputs = async () => {
    setLoading(true); setError('');
    try { const next = await getMusicSettings(); setSettings(next); if (next.available) setInputs(await getMusicInputs()); }
    catch (failure) { setError(message(failure)); }
    finally { setLoading(false); }
  };
  const toggle = async () => {
    setError('');
    try {
      if (active) await props.onStop();
      else await props.onStart({ runId: crypto.randomUUID(), serials, inputId, style, dynamics, sensitivity });
    } catch (failure) { setError(message(failure)); }
  };
  const disabled = active || loading;
  const q = props.query.trim().toLowerCase();
  const scopes = props.scopes.filter((scope) => !q || scope.name.toLowerCase().includes(q));
  const stateLabel = { idle: 'ready', starting: 'starting', running: 'listening', stopping: 'restoring', stopped: 'stopped', error: 'stopped' }[props.status.state];

  return <main className="center-panel"><div className="music-shell">
    <header className="music-header"><div className="music-title-row"><h1>music</h1><CenterViewToggle view={props.view} onChange={props.onViewChange} /></div>
      <div className="music-transport"><span className="music-state" data-active={active}><AudioLines size={14} />{stateLabel}</span><span className="music-count">{displayedSerials.length} {displayedSerials.length === 1 ? 'light' : 'lights'}</span>
        <button className="icon-button music-play" title={active ? 'Stop Music and restore lights' : 'Start Music'} aria-label={active ? 'Stop Music' : 'Start Music'} disabled={props.status.state === 'stopping' || !active && (loading || !settings.available || !serials.length)} onClick={() => void toggle()}>
          {props.status.state === 'starting' || props.status.state === 'stopping' ? <LoaderCircle size={16} className="music-spinner" /> : active ? <Square size={14} /> : <Play size={16} />}
        </button>
      </div>
    </header>
    <div className="music-settings">
      <label><span>input</span><div className="music-input-row"><MusicSelect label="Music input" value={inputId} disabled={disabled} onChange={setInputId}><option value="">default microphone</option>{inputs.map((input) => <option key={input.id} value={input.id}>{input.name}{input.default ? ' (default)' : ''}</option>)}</MusicSelect><button className="icon-button" title="Refresh audio inputs" aria-label="Refresh audio inputs" disabled={active || loading} onClick={() => void refreshInputs()}><RefreshCw size={13} /></button></div></label>
      <label><span>style</span><MusicSelect label="Music style" value={style} disabled={disabled} onChange={setStyle}>{(settings.styles.length ? settings.styles : ['synthwave']).map((name) => <option key={name} value={name}>{name}</option>)}</MusicSelect></label>
      <label><span>dynamics</span><MusicSelect label="Music dynamics" value={dynamics} disabled={disabled} onChange={setDynamics}>{['auto', 'calm', 'balanced', 'energetic'].map((name) => <option key={name} value={name}>{name}</option>)}</MusicSelect></label>
      <label><span>sensitivity</span><MusicSelect label="Music sensitivity" value={sensitivity} disabled={disabled} onChange={setSensitivity}>{['low', 'normal', 'high'].map((name) => <option key={name} value={name}>{name}</option>)}</MusicSelect></label>
    </div>
    <section className="music-targets"><div className="music-targets-heading"><h2>lights</h2><MusicSelect label="Add Music lights" value="" disabled={disabled} onChange={(id) => { const scope = props.scopes.find((entry) => entry.id === id); if (scope) setSerials((current) => [...new Set([...current, ...scope.serials])]); }}>
      <option value="">add lights</option>{(['group', 'room', 'device', 'all'] as const).map((kind) => <optgroup key={kind} label={{ group: 'Groups', room: 'Rooms', device: 'Devices', all: 'LAN' }[kind]}>{scopes.filter((scope) => scope.kind === kind).map((scope) => <option key={scope.id} value={scope.id} disabled={scope.serials.every((serial) => displayedSerials.includes(serial))}>{scope.name}</option>)}</optgroup>)}
    </MusicSelect></div>
      <ul>{displayedSerials.map((serial) => { const device = props.devices.find((entry) => entry.serial === serial); return <li key={serial}><span className="music-light-dot" data-on={!!device?.on} /><span>{device?.name ?? serial}<small>{device?.model ?? 'unavailable'}</small></span><button className="icon-button" disabled={active} title="Remove light" aria-label={`Remove ${device?.name ?? serial} from Music`} onClick={() => setSerials((current) => current.filter((entry) => entry !== serial))}><X size={13} /></button></li>; })}</ul>
    </section>
    {error || props.status.error || settings.warning ? <p className="music-error" role="alert">{error || props.status.error || settings.warning}</p> : null}
    <span className="music-credit">powered by Maestro</span>
  </div></main>;
}

function MusicSelect({ label, value, disabled, onChange, children }: { label: string; value: string; disabled: boolean; onChange: (value: string) => void; children: ReactNode }) {
  return <span className="music-select-wrap"><select aria-label={label} value={value} disabled={disabled} onChange={(event) => onChange(event.target.value)}>{children}</select><ChevronDown size={12} aria-hidden="true" /></span>;
}
function message(error: unknown): string { return error instanceof Error ? error.message : String(error); }
