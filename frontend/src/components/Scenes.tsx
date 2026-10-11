import { useEffect, useState, type FormEvent } from 'react';
import { Check, ChevronDown, Info, LoaderCircle, Pencil, Play, Plus, RefreshCw, Save, Trash2, X } from 'lucide-react';
import { deleteScene, getScenes } from '../backend/api';
import { addSceneMembers, sceneMembers, visibleScenes, type CaptureSceneRequest, type SceneCaptureScope, type SceneRecallResult, type SceneSummary } from '../domain/scenes';
import { effectPreviewColor, type Device } from '../domain/lifx';
import { CenterViewToggle, type CenterView } from './CenterViewToggle';
import './Scenes.css';

interface ScenesProps {
  devices: Device[];
  scopes: SceneCaptureScope[];
  defaultScopeId?: string;
  query: string;
  view: CenterView;
  onViewChange: (view: CenterView) => void;
  onCapture: (request: CaptureSceneRequest) => Promise<SceneSummary>;
  onEdit: (request: CaptureSceneRequest) => Promise<SceneSummary>;
  onRecall: (scene: SceneSummary) => Promise<SceneRecallResult>;
}

export function Scenes(props: ScenesProps) {
  const [scenes, setScenes] = useState<SceneSummary[]>([]);
  const [loading, setLoading] = useState(true);
  const [busy, setBusy] = useState<string>();
  const [error, setError] = useState('');
  const [creating, setCreating] = useState(false);
  const [name, setName] = useState('');
  const [createSerials, setCreateSerials] = useState<string[]>([]);
  const [recalled, setRecalled] = useState<string>();
  const [editing, setEditing] = useState<string>();
  const [editName, setEditName] = useState('');
  const [editSerials, setEditSerials] = useState<string[]>([]);
  const [confirming, setConfirming] = useState<{ id: string; action: 'delete' | 'update' }>();
  const [expanded, setExpanded] = useState<string>();
  const [results, setResults] = useState<Record<string, SceneRecallResult>>({});

  const disabled = loading || !!busy;

  useEffect(() => {
    let disposed = false;
    void getScenes().then((entries) => { if (!disposed) setScenes(entries); }).catch((failure) => { if (!disposed) setError(message(failure)); }).finally(() => { if (!disposed) setLoading(false); });
    return () => { disposed = true; };
  }, []);

  useEffect(() => {
    if (!recalled) return;
    const timer = window.setTimeout(() => setRecalled(undefined), 650);
    return () => window.clearTimeout(timer);
  }, [recalled]);

  const upsert = (entry: SceneSummary) => setScenes((current) => [...current.filter((item) => item.id !== entry.id), entry]);

  const capture = async (event: FormEvent) => {
    event.preventDefault();
    if (disabled || !name.trim() || !createSerials.length) return;
    setBusy('new'); setError('');
    try {
      upsert(await props.onCapture({ name: name.trim(), serials: createSerials }));
      setCreating(false); setName('');
    } catch (failure) { setError(message(failure)); }
    finally { setBusy(undefined); }
  };

  const recall = async (scene: SceneSummary) => {
    if (disabled) return;
    setBusy(scene.id); setError('');
    setResults((current) => { const next = { ...current }; delete next[scene.id]; return next; });
    try { const result = await props.onRecall(scene); setResults((current) => ({ ...current, [scene.id]: result })); if (!result.failures.length) setRecalled(scene.id); }
    catch (failure) { setError(message(failure)); }
    finally { setBusy(undefined); }
  };

  const confirm = async () => {
    if (!confirming || disabled) return;
    setBusy(confirming.id); setError('');
    try {
      if (confirming.action === 'delete') {
        await deleteScene(confirming.id);
        setScenes((current) => current.filter((entry) => entry.id !== confirming.id));
      } else {
        const previous = scenes.find((scene) => scene.id === confirming.id);
        upsert(await props.onCapture({ id: confirming.id, name: previous?.name ?? '', serials: previous?.targets.map((target) => target.serial) ?? [] }));
        setResults((current) => { const next = { ...current }; delete next[confirming.id]; return next; });
      }
      setConfirming(undefined);
    } catch (failure) { setError(message(failure)); }
    finally { setBusy(undefined); }
  };

  const saveEdit = async (event: FormEvent) => {
    event.preventDefault();
    if (!editing || disabled || !editName.trim() || !editSerials.length) return;
    setBusy(editing); setError('');
    try { upsert(await props.onEdit({ id: editing, name: editName.trim(), serials: editSerials })); setEditing(undefined); setResults((current) => { const next = { ...current }; delete next[editing]; return next; }); }
    catch (failure) { setError(message(failure)); }
    finally { setBusy(undefined); }
  };

  const entries = visibleScenes(scenes, props.devices, props.query);
  return <main className="center-panel">
    <div className="scenes-shell">
      <header className="scenes-header">
        <div className="scenes-title-row"><h1>scenes</h1><CenterViewToggle view={props.view} onChange={props.onViewChange} /></div>
        <div className="scenes-actions"><span>{scenes.length} saved</span><button className="icon-button" type="button" title="Capture a new scene" aria-label="Capture a new scene" disabled={disabled || !props.scopes.some((entry) => entry.serials.length)} onClick={() => { setCreating((current) => !current); setCreateSerials((props.scopes.find((entry) => entry.id === props.defaultScopeId) ?? props.scopes[0])?.serials ?? []); setEditing(undefined); setError(''); }}><Plus size={15} /></button></div>
      </header>
      {creating ? <form className="scene-capture-form" onSubmit={capture}>
        <label><span>name</span><input autoFocus value={name} maxLength={128} onChange={(event) => setName(event.target.value)} onKeyDown={(event) => { if (event.key === 'Escape') { event.preventDefault(); event.stopPropagation(); setCreating(false); } }} /></label>
        <SceneMembership scopes={props.scopes} serials={createSerials} devices={props.devices} disabled={disabled} onChange={setCreateSerials} />
        <div className="scene-capture-actions"><button className="icon-button" type="button" title="Cancel capture" aria-label="Cancel capture" disabled={!!busy} onClick={() => setCreating(false)}><X size={14} /></button><button className="icon-button scene-primary" type="submit" title="Save scene" aria-label="Save scene" disabled={disabled || !name.trim() || !createSerials.length}>{busy === 'new' ? <LoaderCircle size={14} className="scene-spinner" /> : <Save size={14} />}</button></div>
      </form> : null}
      {error ? <p className="scene-error" role="alert">{error}</p> : null}
      {loading ? <p className="scene-empty">loading scenes</p> : !entries.length ? <p className="scene-empty">{props.query.trim() ? 'No matching scenes.' : 'No scenes saved.'}</p> : null}
      <div className="scene-list">{entries.map((scene) => {
        const members = sceneMembers(scene, props.devices);
        const available = members.filter((target) => target.available).length;
        const result = results[scene.id];
        return <section className="scene-row" key={scene.id}>
          <div className="scene-row-main">
            {editing === scene.id ? <form className="scene-edit-name" onSubmit={saveEdit}><input autoFocus aria-label="Scene name" value={editName} maxLength={128} onChange={(event) => setEditName(event.target.value)} onKeyDown={(event) => { if (event.key === 'Escape') { event.preventDefault(); event.stopPropagation(); setEditing(undefined); } }} /><button className="icon-button" type="button" aria-label="Cancel edit" title="Cancel edit" onClick={() => setEditing(undefined)}><X size={13} /></button><button className="icon-button scene-primary" type="submit" aria-label="Save scene" title="Save scene" disabled={disabled || !editName.trim() || !editSerials.length}><Save size={13} /></button></form> : <button className="scene-recall" data-recalled={recalled === scene.id} data-unavailable={!available} type="button" title={`Recall ${scene.name}`} disabled={disabled || !available} onClick={() => void recall(scene)}>{busy === scene.id ? <LoaderCircle size={14} className="scene-spinner" /> : <Play size={14} />}<span>{scene.name}<small title={`Captured ${new Date(scene.savedAtMs).toLocaleString()}`}>{available}/{members.length} available</small></span></button>}
            {editing !== scene.id ? <div className="scene-row-actions">
              <button className="icon-button" type="button" title="Scene lights" aria-label={`Lights in ${scene.name}`} aria-expanded={expanded === scene.id} onClick={() => setExpanded((current) => current === scene.id ? undefined : scene.id)}><Info size={13} /></button>
              <button className="icon-button" type="button" title="Save current light states over this scene" aria-label={`Update ${scene.name}`} disabled={disabled || !!editing || available !== members.length} onClick={() => setConfirming({ id: scene.id, action: 'update' })}><RefreshCw size={13} /></button>
              <button className="icon-button" type="button" title="Edit scene" aria-label={`Edit ${scene.name}`} disabled={disabled} onClick={() => { setCreating(false); setExpanded(undefined); setEditing(scene.id); setEditName(scene.name); setEditSerials(scene.targets.map((target) => target.serial)); setConfirming(undefined); }}><Pencil size={13} /></button>
              <button className="icon-button" type="button" title="Delete scene" aria-label={`Delete ${scene.name}`} disabled={disabled} onClick={() => setConfirming({ id: scene.id, action: 'delete' })}><Trash2 size={13} /></button>
            </div> : null}
          </div>
          {editing === scene.id ? <div className="scene-edit-members"><SceneMembership scopes={props.scopes} serials={editSerials} devices={props.devices} savedTargets={scene.targets} disabled={disabled} onChange={setEditSerials} /></div> : null}
          {confirming?.id === scene.id ? <div className="scene-confirm"><span>{confirming.action === 'delete' ? 'delete scene?' : `Save the current state of ${members.length} ${members.length === 1 ? 'light' : 'lights'} over this scene?`}</span><button className="icon-button" type="button" aria-label="Cancel" title="Cancel" disabled={disabled} onClick={() => setConfirming(undefined)}><X size={13} /></button><button className="icon-button scene-primary" type="button" aria-label="Confirm" title="Confirm" disabled={disabled} onClick={() => void confirm()}><Check size={13} /></button></div> : null}
          {expanded === scene.id && editing !== scene.id ? <ul className="scene-members">{members.map((member) => <li key={member.serial}><span className="scene-member-dot" data-available={member.available} /><span title={member.serial}>{member.label}</span><span className="scene-saved-state"><span className="scene-swatches" title="Saved colors (representative, not pixel layout)">{member.colors?.map((color, index) => <i key={index} style={{ background: effectPreviewColor(color) }} />)}</span><small>{member.on === undefined ? '' : !member.on ? 'off' : `${Math.round((member.brightness ?? 0) * 100)}%`}</small>{!member.available ? <small>unavailable</small> : null}</span></li>)}</ul> : null}
          {result?.failures.length ? <div className="scene-result" role="status">{result.failures.map((failure) => <p className="scene-error" key={failure.serial}>{members.find((member) => member.serial === failure.serial)?.label ?? failure.serial}: {failure.error}{failure.stateMayHaveChanged ? ' (state may be partially applied)' : ''}</p>)}</div> : null}
        </section>;
      })}</div>
    </div>
  </main>;
}

interface SceneMembershipProps {
  scopes: SceneCaptureScope[];
  serials: string[];
  devices: Device[];
  savedTargets?: SceneSummary['targets'];
  disabled: boolean;
  onChange: (serials: string[]) => void;
}

function SceneMembership({ scopes, serials, devices, savedTargets = [], disabled, onChange }: SceneMembershipProps) {
  return <div className="scene-membership">
    <span className="scene-select-wrap"><select aria-label="Add scene lights" value="" disabled={disabled} onChange={(event) => {
      const scope = scopes.find((entry) => entry.id === event.target.value);
      if (scope) onChange(addSceneMembers(serials, scope.serials));
    }}>
      <option value="">add lights</option>
      {(['all', 'group', 'room', 'device'] as const).map((kind) => <optgroup key={kind} label={{ all: 'LAN', group: 'Groups', room: 'Rooms', device: 'Devices' }[kind]}>
        {scopes.filter((entry) => entry.kind === kind).map((entry) => <option key={entry.id} value={entry.id} disabled={!entry.serials.some((serial) => !serials.includes(serial))}>{entry.name} ({entry.serials.length})</option>)}
      </optgroup>)}
    </select><ChevronDown size={12} aria-hidden="true" /></span>
    <ul className="scene-members">{serials.map((serial) => {
      const device = devices.find((entry) => entry.serial === serial);
      const label = device?.name ?? savedTargets.find((entry) => entry.serial === serial)?.label ?? serial;
      return <li key={serial}><span title={serial}>{label}</span><button className="icon-button" type="button" title="Remove from scene" aria-label={`Remove ${label} from scene`} disabled={disabled} onClick={() => onChange(serials.filter((value) => value !== serial))}><X size={12} /></button></li>;
    })}</ul>
  </div>;
}

function message(error: unknown): string { return error instanceof Error ? error.message : String(error); }
