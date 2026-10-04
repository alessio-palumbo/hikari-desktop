import { useEffect, useRef, useState } from 'react';
import { Check, ChevronDown, Copy, Eye, Pencil, Plus, Trash2, X } from 'lucide-react';
import { deleteUserTheme, getUserThemes, previewTheme, saveUserTheme } from '../backend/api';
import { DeviceKind, type Device } from '../domain/lifx';
import { ThemeEditor, themeSwatchColor } from './ThemeEditor';
import { drawPreviewFrame, usePreviewCanvas } from './usePreviewCanvas';
import { nextThemeVariation, sortedUserThemes, themeColors, themeEditorDraft, themeRequest, themeSelectionAction, themes, type Theme, type SaveUserThemeRequest, type UserTheme, type ThemeDevicePreview, type ThemePreview, type ThemeRequest } from '../domain/themes';
import './ThemeControls.css';

export function ThemeControls({ devices, disabled = false, editing = false, showDeviceNames = true, onApply }: {
  devices: Device[]; disabled?: boolean; editing?: boolean; showDeviceNames?: boolean; onApply: (request: ThemeRequest) => Promise<void>;
}) {
  const [selected, setSelected] = useState('builtin-0');
  const [variation, setVariation] = useState(0);
  const variations = useRef(new Map<string, number>());
  const [userThemes, setUserThemes] = useState<UserTheme[]>([]);
  const [editor, setEditor] = useState<SaveUserThemeRequest>();
  const [storageBusy, setStorageBusy] = useState(true);
  const [storageError, setStorageError] = useState('');
  const [deletePending, setDeletePending] = useState(false);
  const [preview, setPreview] = useState<ThemePreview>();
  const [error, setError] = useState('');
  const [applying, setApplying] = useState(false);
  const [rendering, setRendering] = useState(false);
  const [previewMode, setPreviewMode] = useState(false);
  const [applied, setApplied] = useState(false);
  const generation = useRef(0);
  const mounted = useRef(true);
  const themeList = useRef<HTMLDivElement>(null);
  const [moreThemesBelow, setMoreThemesBelow] = useState(false);
  const entries = [...themes.map((theme, index) => ({ id: `builtin-${index}`, theme })), ...userThemes];
  const selectedEntry = entries.find((entry) => entry.id === selected) ?? entries[0];
  const activeTheme = editor?.theme ?? selectedEntry.theme;
  const request = themeRequest({ ...activeTheme, name: activeTheme.name.trim() || 'Untitled' }, devices, variation);
  const targets = request.serials.join(',');
  const currentTargets = useRef(targets);
  currentTargets.current = targets;

  useEffect(() => {
    const list = themeList.current;
    if (!list) return;
    const update = () => setMoreThemesBelow(list.scrollHeight - list.clientHeight - list.scrollTop > 1);
    update();
    const observer = new ResizeObserver(update);
    observer.observe(list);
    list.addEventListener('scroll', update, { passive: true });
    return () => { observer.disconnect(); list.removeEventListener('scroll', update); };
  }, [userThemes, editor]);

  useEffect(() => {
    mounted.current = true;
    let disposed = false;
    void getUserThemes().then((result) => {
      if (!disposed) setUserThemes(sortedUserThemes(result));
    }).catch((failure) => {
      if (!disposed) setStorageError(String(failure instanceof Error ? failure.message : failure));
    }).finally(() => { if (!disposed) setStorageBusy(false); });
    return () => { disposed = true; mounted.current = false; };
  }, []);

  useEffect(() => {
    const token = ++generation.current;
    setError(''); setApplied(false);
    setRendering(previewMode && !!targets && !editing);
    const timer = previewMode && targets && !editing ? setTimeout(() => {
      void previewTheme(request).then((result) => {
        if (generation.current === token) setPreview(result);
      }).catch((failure) => {
        if (generation.current === token) setError(String(failure instanceof Error ? failure.message : failure));
      }).finally(() => { if (generation.current === token) setRendering(false); });
    }, 200) : undefined;
    if (!previewMode || !targets || editing) setPreview(undefined);
    return () => { generation.current++; clearTimeout(timer); };
  }, [activeTheme, targets, previewMode, editing, variation]);

  const save = async () => {
    if (!editor) return;
    setStorageBusy(true); setStorageError('');
    try {
      const result = await saveUserTheme(editor);
      variations.current.set(result.id, variation);
      setUserThemes((current) => sortedUserThemes([...current.filter((entry) => entry.id !== result.id), result]));
      setSelected(result.id); setEditor(undefined);
    } catch (failure) { setStorageError(String(failure instanceof Error ? failure.message : failure)); }
    finally { setStorageBusy(false); }
  };

  const remove = async () => {
    setStorageBusy(true); setStorageError('');
    try {
      await deleteUserTheme(selectedEntry.id);
      variations.current.delete(selectedEntry.id);
      setUserThemes((current) => current.filter((entry) => entry.id !== selectedEntry.id));
      setSelected('builtin-0'); setVariation(variations.current.get('builtin-0') ?? 0); setDeletePending(false);
    } catch (failure) { setStorageError(String(failure instanceof Error ? failure.message : failure)); }
    finally { setStorageBusy(false); }
  };

  const apply = async (theme: Theme, requestedVariation = variation) => {
    if (applying || disabled || editing || editor || !targets) return;
    setApplying(true); setError(''); setApplied(false);
    try {
      await onApply(themeRequest(theme, devices, requestedVariation));
      if (mounted.current && currentTargets.current === targets) setApplied(true);
    } catch (failure) {
      if (mounted.current) setError(failure instanceof Error ? failure.message : String(failure));
    } finally {
      if (mounted.current) setApplying(false);
    }
  };

  return <section className="theme-controls">
    {editor ? <ThemeEditor draft={editor} saving={storageBusy} error={storageError} onChange={setEditor} onSave={() => void save()} onCancel={() => { setEditor(undefined); setStorageError(''); }} /> : <>
      <div className="theme-browser-actions">
        <button className="icon-button" type="button" title="New theme" aria-label="New theme" disabled={storageBusy || applying} onClick={() => { setEditor(themeEditorDraft()); setStorageError(''); }}><Plus size={14} /></button>
        <button className="icon-button" type="button" title="Duplicate theme" aria-label="Duplicate theme" disabled={storageBusy || applying} onClick={() => { setEditor(themeEditorDraft(selectedEntry.theme)); setStorageError(''); }}><Copy size={13} /></button>
        {selectedEntry.id.startsWith('user-') ? <>
          <button className="icon-button" type="button" title="Edit theme" aria-label="Edit theme" disabled={storageBusy || applying} onClick={() => { setEditor(themeEditorDraft(selectedEntry.theme, selectedEntry.id)); setStorageError(''); }}><Pencil size={13} /></button>
          <button className="icon-button" type="button" title="Delete theme" aria-label="Delete theme" disabled={storageBusy || applying} onClick={() => setDeletePending(true)}><Trash2 size={13} /></button>
        </> : null}
      </div>
      {deletePending ? <div className="theme-delete-confirm"><span>delete {selectedEntry.theme.name}?</span><button className="icon-button" type="button" title="Cancel deletion" aria-label="Cancel deletion" disabled={storageBusy} onClick={() => setDeletePending(false)}><X size={13} /></button><button className="icon-button" type="button" title="Confirm deletion" aria-label="Confirm deletion" disabled={storageBusy} onClick={() => void remove()}><Check size={13} /></button></div> : null}
      <div className="theme-list-wrap">
      <div className="theme-list" ref={themeList} tabIndex={0} role="region" aria-label="Themes">
        {entries.map((entry, index) => <div key={entry.id}>
          {index === 0 || index === themes.length ? <div className="theme-section-label">{index === 0 ? 'built-in' : 'yours'}</div> : null}
          <div className="theme-row" data-active={selectedEntry.id === entry.id}>
          <button className="theme-select" type="button" aria-label={`${previewMode ? 'Preview' : 'Apply'} ${entry.theme.name}`} disabled={applying || storageBusy || disabled || editing || !targets} onClick={() => {
            const next = nextThemeVariation(variations.current.get(entry.id));
            variations.current.set(entry.id, next); setVariation(next);
            setSelected(entry.id); setDeletePending(false);
            if (themeSelectionAction(previewMode) === 'apply') void apply(entry.theme, next);
          }}>
            <span className="theme-swatches" aria-hidden="true">{themeColors(entry.theme).slice(0, 5).map((color, stop) => <i key={stop} style={{ background: themeSwatchColor(color) }} />)}</span>
            <span className="theme-name">{entry.theme.name}</span>
          </button>
          </div>
        </div>)}
      </div>
      <span className="theme-scroll-hint" data-visible={moreThemesBelow} aria-hidden="true"><ChevronDown size={12} /></span>
      </div>
      {storageError ? <div className="inspector-error" role="alert">{storageError}</div> : null}
    </>}
    <div className="theme-actions">
      {previewMode && !editor ? <button className="icon-button theme-preview-apply" type="button" title={applying ? 'Applying theme' : applied ? 'Theme applied' : 'Apply theme'} aria-label={applying ? 'Applying theme' : applied ? 'Theme applied' : 'Apply theme'} data-applied={applied} disabled={applying || rendering || !targets || disabled || editing || applied} onClick={() => void apply(activeTheme)}><Check size={14} /></button> : null}
      {previewMode ? <button type="button" className="icon-button" title="Close preview" aria-label="Close theme preview" disabled={applying} onClick={() => setPreviewMode(false)}><X size={14} /></button> : <button type="button" className="icon-button" title="Preview theme locally" aria-label="Preview theme locally" disabled={applying || !targets || disabled || editing} onClick={() => { if (!editor) variations.current.set(selectedEntry.id, variation); setPreviewMode(true); }}><Eye size={14} /></button>}
    </div>
    {previewMode ? <div className="theme-preview-section" aria-busy={rendering}>
      {preview ? <div className="theme-previews">{preview.devices.map((device) => <ThemeCanvas key={device.serial} device={device} kind={devices.find((target) => target.serial === device.serial)?.kind ?? DeviceKind.Matrix} showName={showDeviceNames} />)}</div> : null}
    </div> : null}
    {editing ? <div className="theme-status" role="status">Exit layout editing to apply a theme.</div> : null}
    {applying ? <div className="theme-status" role="status">applying theme…</div> : null}
    {error ? <div className="inspector-error" role="alert">{error}</div> : null}
  </section>;
}

function ThemeCanvas({ device, kind, showName }: { device: ThemeDevicePreview; kind: DeviceKind; showName: boolean }) {
  const { canvas, layout } = usePreviewCanvas(device.width, device.height, kind);
  useEffect(() => {
    if (!canvas.current || device.width < 1 || device.height < 1) return;
    drawPreviewFrame(canvas.current, device.width, device.cells, device.colors, layout);
  }, [device, layout.width, layout.height, layout.cellWidth, layout.cellHeight, layout.left]);
  return <figure>{showName ? <figcaption>{device.name}</figcaption> : null}<canvas ref={canvas} role="img" aria-label={`${device.name} theme preview`} /></figure>;
}
