import { useEffect, useRef, useState, type ReactNode, type Ref } from 'react';
import { AudioLines, ChevronDown, ListX, LoaderCircle, Play, RefreshCw, Settings2, Square, X } from 'lucide-react';
import { getMusicSettings, getMusicInputs } from '../backend/api';
import type { Device } from '../domain/lifx';
import type { SceneCaptureScope } from '../domain/scenes';
import { musicActive, type MusicStatus, type MusicSettings, type MusicInputDevice, type MusicSelection, type StartMusicRequest } from '../domain/music';
import { CenterViewToggle, type CenterView } from './CenterViewToggle';
import { MusicSignalView } from './MusicSignal';
import './Music.css';

interface MusicProps {
  devices: Device[];
  scopes: SceneCaptureScope[];
  query: string;
  status: MusicStatus;
  selection: MusicSelection;
  onSelectionChange: (selection: MusicSelection) => void;
  onControlsChange: (selection: MusicSelection) => Promise<void>;
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
  const [configurationOpen, setConfigurationOpen] = useState(false);
  const configurationRef = useRef<HTMLDivElement>(null);
  const configurationButtonRef = useRef<HTMLButtonElement>(null);
  const addLightsRef = useRef<HTMLSelectElement>(null);
  const selectionHintTimerRef = useRef<number | undefined>(undefined);
  const [selectionHintActive, setSelectionHintActive] = useState(false);
  const { serials, inputId, style, dynamics, sensitivity, masterBrightness } = props.selection;
  const setSerials = (next: string[] | ((current: string[]) => string[])) => {
    window.clearTimeout(selectionHintTimerRef.current);
    setSelectionHintActive(false);
    props.onSelectionChange({ ...props.selection, serials: typeof next === 'function' ? next(serials) : next });
  };
  const setInputId = (inputId: string) => props.onSelectionChange({ ...props.selection, inputId });
  const changeControls = (patch: Partial<MusicSelection>) => {
    setError('');
    void props.onControlsChange({ ...props.selection, ...patch }).catch((failure) => setError(message(failure)));
  };
  const setStyle = (style: string) => changeControls({ style });
  const setDynamics = (dynamics: string) => changeControls({ dynamics });
  const setSensitivity = (sensitivity: string) => props.onSelectionChange({ ...props.selection, sensitivity });
  const active = musicActive(props.status);
  const displayedSerials = active ? props.status.serials : serials;

  useEffect(() => () => window.clearTimeout(selectionHintTimerRef.current), []);

  useEffect(() => {
    let disposed = false;
    void getMusicSettings().then(async (next) => {
      if (disposed) return;
      setSettings(next);
      if (next.available) { const devices = await getMusicInputs(); if (!disposed) setInputs(devices); }
    }).catch((failure) => { if (!disposed) setError(message(failure)); }).finally(() => { if (!disposed) setLoading(false); });
    return () => { disposed = true; };
  }, []);

  useEffect(() => {
    if (!configurationOpen) return;
    const panel = configurationRef.current?.querySelector<HTMLElement>('[role="dialog"]');
    (panel?.querySelector<HTMLElement>('select:not(:disabled)') ?? panel)?.focus();
    const outside = (event: PointerEvent) => {
      if (!configurationRef.current?.contains(event.target as Node)) setConfigurationOpen(false);
    };
    const escape = (event: KeyboardEvent) => {
      if (event.key !== 'Escape') return;
      event.preventDefault();
      event.stopPropagation();
      setConfigurationOpen(false);
      configurationButtonRef.current?.focus();
    };
    document.addEventListener('pointerdown', outside);
    document.addEventListener('keydown', escape, true);
    return () => { document.removeEventListener('pointerdown', outside); document.removeEventListener('keydown', escape, true); };
  }, [configurationOpen]);

  const refreshInputs = async () => {
    setLoading(true); setError('');
    try { const next = await getMusicSettings(); setSettings(next); if (next.available) setInputs(await getMusicInputs()); }
    catch (failure) { setError(message(failure)); }
    finally { setLoading(false); }
  };
  const toggle = async () => {
    setError('');
    if (!active && !serials.length) return;
    try {
      if (active) await props.onStop();
      else await props.onStart({ ...props.selection, runId: crypto.randomUUID() });
    } catch (failure) { setError(message(failure)); }
  };
  const disabled = active || loading;
  const controlsDisabled = loading || props.status.state === 'starting' || props.status.state === 'stopping';
  const noTargets = !active && !serials.length;
  const playHint = active ? 'Stop Music and restore lights' : noTargets ? 'Add lights to start Music' : 'Start Music';
  const guideSelection = () => {
    if (disabled) return;
    addLightsRef.current?.focus();
    window.clearTimeout(selectionHintTimerRef.current);
    setSelectionHintActive(true);
    selectionHintTimerRef.current = window.setTimeout(() => setSelectionHintActive(false), 1400);
  };
  const q = props.query.trim().toLowerCase();
  const scopes = props.scopes.filter((scope) => !q || scope.name.toLowerCase().includes(q));
  const stateLabel = { idle: 'ready', starting: 'starting', running: 'listening', stopping: 'restoring', stopped: 'stopped', error: 'stopped' }[props.status.state];

  return <main className="center-panel"><div className="music-shell">
    <header className="music-header"><div className="music-title-row"><h1>music</h1><CenterViewToggle view={props.view} onChange={props.onViewChange} /></div>
      <div className="music-transport"><span className="music-state" data-active={active}><AudioLines size={14} />{stateLabel}</span><span className="music-count">{displayedSerials.length} {displayedSerials.length === 1 ? 'light' : 'lights'}</span>
        <div className="music-configuration-anchor" ref={configurationRef} onBlur={(event) => { if (event.relatedTarget && !event.currentTarget.contains(event.relatedTarget as Node)) setConfigurationOpen(false); }}>
          <button ref={configurationButtonRef} className="icon-button" title="Music configuration" aria-label="Music configuration" aria-expanded={configurationOpen} aria-controls="music-configuration" aria-haspopup="dialog" onClick={() => setConfigurationOpen((open) => !open)}><Settings2 size={15} /></button>
          {configurationOpen && <div id="music-configuration" role="dialog" aria-label="Microphone settings" tabIndex={-1} className="music-settings music-configuration">
            <label><span>input</span><div className="music-input-row"><MusicSelect label="Music input" value={inputId} disabled={disabled} onChange={setInputId}><option value="">default microphone</option>{inputs.map((input) => <option key={input.id} value={input.id}>{input.name}{input.default ? ' (default)' : ''}</option>)}</MusicSelect><button className="icon-button" title="Refresh audio inputs" aria-label="Refresh audio inputs" disabled={disabled} onClick={() => void refreshInputs()}><RefreshCw size={13} /></button></div></label>
            <label><span>sensitivity</span><MusicSelect label="Music sensitivity" value={sensitivity} disabled={disabled} onChange={setSensitivity}>{['low', 'normal', 'high'].map((name) => <option key={name} value={name}>{name}</option>)}</MusicSelect></label>
          </div>}
        </div>
        <span className="music-play-trigger" title={playHint}>
        <button className="icon-button music-play" data-needs-targets={noTargets} aria-label={noTargets ? playHint : active ? 'Stop Music' : 'Start Music'} disabled={props.status.state === 'stopping' || !active && (loading || !settings.available)} onClick={() => noTargets ? guideSelection() : void toggle()} onKeyDown={(event) => {
          if (noTargets && (event.key === 'Enter' || event.key === ' ')) { event.preventDefault(); event.stopPropagation(); guideSelection(); }
        }}>
          {props.status.state === 'starting' || props.status.state === 'stopping' ? <LoaderCircle size={16} className="music-spinner" /> : active ? <Square size={14} /> : <Play size={16} />}
        </button>
        </span>
      </div>
    </header>
    <div className="music-control-area"><div className="music-settings">
      <label><span>style</span><MusicSelect label="Music style" value={style} disabled={controlsDisabled} onChange={setStyle}>{(settings.styles.length ? settings.styles : ['synthwave']).map((name) => <option key={name} value={name}>{name}</option>)}</MusicSelect></label>
      <label><span>dynamics</span><MusicSelect label="Music dynamics" value={dynamics} disabled={controlsDisabled} onChange={setDynamics}>{['auto', 'calm', 'balanced', 'energetic'].map((name) => <option key={name} value={name}>{name}</option>)}</MusicSelect></label>
      <label><span>brightness</span><div className="music-brightness"><input type="range" aria-label="Music master brightness" min="1" max="100" step="1" value={Math.round(masterBrightness * 100)} disabled={controlsDisabled} onChange={(event) => changeControls({ masterBrightness: Number(event.target.value) / 100 })} /><output>{Math.round(masterBrightness * 100)}%</output></div></label>
    </div><MusicSignalView key={props.status.runId ?? 'idle'} status={props.status} /></div>
    <section className="music-targets"><div className="music-targets-heading"><h2>lights</h2><div className="music-target-actions" data-selection-hint={selectionHintActive}><MusicSelect selectRef={addLightsRef} label="Add Music lights" value="" disabled={disabled} onChange={(id) => { const scope = props.scopes.find((entry) => entry.id === id); if (scope) setSerials((current) => [...new Set([...current, ...scope.serials])]); }}>
      <option value="">add lights</option>{(['group', 'room', 'device', 'all'] as const).map((kind) => <optgroup key={kind} label={{ group: 'Groups', room: 'Rooms', device: 'Devices', all: 'LAN' }[kind]}>{scopes.filter((scope) => scope.kind === kind).map((scope) => <option key={scope.id} value={scope.id} disabled={scope.serials.every((serial) => displayedSerials.includes(serial))}>{scope.name}</option>)}</optgroup>)}
    </MusicSelect><button className="icon-button" aria-label="Clear selected lights" title="Clear selected lights" disabled={disabled || !serials.length} onClick={() => setSerials([])}><ListX size={15} /></button></div></div>
      <ul>{displayedSerials.map((serial) => { const device = props.devices.find((entry) => entry.serial === serial); return <li key={serial}><span className="music-light-dot" data-on={!!device?.on} /><span>{device?.name ?? serial}<small>{device?.model ?? 'unavailable'}</small></span><button className="icon-button" disabled={active} title="Remove light" aria-label={`Remove ${device?.name ?? serial} from Music`} onClick={() => setSerials((current) => current.filter((entry) => entry !== serial))}><X size={13} /></button></li>; })}</ul>
    </section>
    {error || props.status.error || settings.warning ? <p className="music-error" role="alert">{error || props.status.error || settings.warning}</p> : null}
    <span className="music-credit">powered by Maestro</span>
  </div></main>;
}

function MusicSelect({ label, value, disabled, onChange, children, selectRef }: { label: string; value: string; disabled: boolean; onChange: (value: string) => void; children: ReactNode; selectRef?: Ref<HTMLSelectElement> }) {
  return <span className="music-select-wrap"><select ref={selectRef} aria-label={label} value={value} disabled={disabled} onChange={(event) => onChange(event.target.value)}>{children}</select><ChevronDown size={12} aria-hidden="true" /></span>;
}
function message(error: unknown): string { return error instanceof Error ? error.message : String(error); }
