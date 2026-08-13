import { FormEvent, useEffect, useState } from 'react';
import { Check, MessageSquareText, X } from 'lucide-react';
import type { CommandEngineSettings, CommandPreview, CommandPreviewAction } from '../backend/api';
import './CommandModal.css';

interface CommandModalProps {
  open: boolean;
  settings: CommandEngineSettings;
  loading: boolean;
  interpreting: boolean;
  executing: boolean;
  error?: string;
  preview?: CommandPreview;
  onClose: () => void;
  onSettingsChange: (settings: { enabled: boolean; enginePath?: string; configPath?: string }) => void;
  onInterpret: (text: string) => void;
  onConfirm: () => void;
  onClear: () => void;
}

export function CommandModal(props: CommandModalProps) {
  const [text, setText] = useState('');
  const [enabled, setEnabled] = useState(props.settings.enabled);
  const [enginePath, setEnginePath] = useState(props.settings.enginePath ?? '');
  const [configPath, setConfigPath] = useState(props.settings.configPath ?? '');

  useEffect(() => {
    setEnabled(props.settings.enabled);
    setEnginePath(props.settings.enginePath ?? '');
    setConfigPath(props.settings.configPath ?? '');
  }, [props.settings]);

  useEffect(() => {
    if (props.open) return;
    setText('');
    props.onClear();
  }, [props.open]);

  const canInterpret = enabled && text.trim().length > 0 && !props.interpreting && !props.loading;
  const canConfirm = !!props.preview && !props.preview.empty && !props.executing;

  const submit = (event: FormEvent) => {
    event.preventDefault();
    if (canInterpret) props.onInterpret(text);
  };

  if (!props.open) return null;

  return (
    <div className="command-backdrop" role="presentation" onMouseDown={(event) => {
      if (event.target === event.currentTarget) props.onClose();
    }}>
      <section className="command-modal" role="dialog" aria-modal="true" aria-label="Text command">
        <header className="command-header">
          <div>
            <MessageSquareText size={15} />
            <strong>text command</strong>
          </div>
          <button type="button" aria-label="Close command modal" onClick={props.onClose}>
            <X size={15} />
          </button>
        </header>

        <div className="command-settings-row">
          <label>
            <input type="checkbox" checked={enabled} onChange={(event) => setEnabled(event.target.checked)} />
            <span>enable local commands</span>
          </label>
          <button type="button" disabled={props.loading} onClick={() => props.onSettingsChange({ enabled, enginePath, configPath })}>
            save
          </button>
        </div>
        <div className="command-paths">
          <input value={enginePath} onChange={(event) => setEnginePath(event.target.value)} placeholder="lifx-command-engine path or PATH lookup" spellCheck={false} />
          <input value={configPath} onChange={(event) => setConfigPath(event.target.value)} placeholder="optional config path" spellCheck={false} />
        </div>
        {props.settings.warning ? <p className="command-warning">{props.settings.warning}</p> : null}

        <form className="command-input-row" onSubmit={submit}>
          <input
            value={text}
            disabled={!enabled}
            onChange={(event) => setText(event.target.value)}
            placeholder={enabled ? 'turn desk warm white at 35%' : 'local commands disabled'}
            autoFocus
            spellCheck={false}
          />
          <button type="submit" disabled={!canInterpret}>
            interpret
          </button>
        </form>

        {props.error ? <p className="command-error">{props.error}</p> : null}

        {props.preview ? (
          <div className="command-preview" data-empty={props.preview.empty ? 'true' : 'false'}>
            <div className="command-preview-title">
              <strong>{props.preview.empty ? 'No supported command found' : props.preview.summary}</strong>
              <span>{Math.round(props.preview.confidence * 100)}% · {props.preview.confidenceLevel || 'unknown'}</span>
            </div>

            {props.preview.commands.length ? (
              <div className="command-command-list">
                {props.preview.commands.map((command, index) => (
                  <section className="command-command" key={index}>
                    <div className="command-command-meta">
                      <span>{command.targets.length} target{command.targets.length === 1 ? '' : 's'}</span>
                      <strong>{describeAction(command.action)}</strong>
                    </div>
                    <div className="command-targets">
                      {command.targets.map((target) => (
                        <span key={target.serial} title={target.serial}>{target.label || target.serial}</span>
                      ))}
                    </div>
                  </section>
                ))}
              </div>
            ) : null}

            {props.preview.skippedTargets.length ? (
              <p className="command-skipped">Skipped unsupported target{props.preview.skippedTargets.length === 1 ? '' : 's'}: {props.preview.skippedTargets.map((target) => target.label || target.serial).join(', ')}</p>
            ) : null}

            {props.preview.reasons.length ? (
              <ul>
                {props.preview.reasons.slice(0, 3).map((reason) => (
                  <li key={reason}>{reason}</li>
                ))}
              </ul>
            ) : null}
          </div>
        ) : null}

        <footer className="command-actions">
          <button type="button" onClick={props.onClose}>cancel</button>
          <button type="button" className="command-confirm" disabled={!canConfirm} onClick={props.onConfirm}>
            <Check size={14} />
            confirm
          </button>
        </footer>
      </section>
    </div>
  );
}

function describeAction(action: CommandPreviewAction): string {
  const parts: string[] = [];
  if (typeof action.power === 'boolean') parts.push(action.power ? 'power on' : 'power off');
  if (typeof action.brightness === 'number') parts.push(`brightness ${Math.round(action.brightness)}%`);
  if (typeof action.kelvin === 'number') parts.push(`${action.kelvin}K`);
  if (typeof action.hue === 'number') parts.push(`hue ${Math.round(action.hue)}°`);
  if (typeof action.saturation === 'number') parts.push(`saturation ${Math.round(action.saturation)}%`);
  return parts.length ? parts.join(' · ') : 'no state change';
}
