import { FormEvent, KeyboardEvent, useEffect, useState } from 'react';
import { Check, MessageSquareText, X } from 'lucide-react';
import type { CommandPreview, CommandPreviewAction } from '../backend/api';
import './CommandModal.css';

interface CommandModalProps {
  open: boolean;
  interpreting: boolean;
  executing: boolean;
  error?: string;
  warning?: string;
  preview?: CommandPreview;
  onClose: () => void;
  onInterpret: (text: string) => void;
  onConfirm: () => void;
  onClear: () => void;
}

export function CommandModal(props: CommandModalProps) {
  const [text, setText] = useState('');
  const [history, setHistory] = useState<string[]>([]);
  const [historyIndex, setHistoryIndex] = useState<number | undefined>();

  useEffect(() => {
    if (props.open) return;
    setText('');
    setHistoryIndex(undefined);
    props.onClear();
  }, [props.open]);

  const canInterpret = text.trim().length > 0 && !props.interpreting;
  const canConfirm = !!props.preview && !props.preview.empty && !props.executing && !props.interpreting;

  const submit = (event: FormEvent) => {
    event.preventDefault();
    if (canConfirm) {
      props.onConfirm();
      return;
    }
    if (!canInterpret) return;
    const prompt = text.trim();
    setHistory((current) => [prompt, ...current.filter((entry) => entry !== prompt)].slice(0, 20));
    setHistoryIndex(undefined);
    props.onInterpret(prompt);
  };

  const updateText = (next: string) => {
    setText(next);
    setHistoryIndex(undefined);
    props.onClear();
  };

  const clearText = () => {
    setText('');
    setHistoryIndex(undefined);
    props.onClear();
  };

  const handleTextKeyDown = (event: KeyboardEvent<HTMLInputElement>) => {
    if (event.key === 'Escape') {
      event.preventDefault();
      event.stopPropagation();
      if (text || props.preview || props.error) clearText();
      else props.onClose();
      return;
    }

    if (event.key === 'ArrowUp') {
      if (!history.length) return;
      event.preventDefault();
      const nextIndex = typeof historyIndex === 'number' ? Math.min(historyIndex + 1, history.length - 1) : 0;
      setHistoryIndex(nextIndex);
      setText(history[nextIndex]);
      props.onClear();
      return;
    }

    if (event.key === 'ArrowDown') {
      if (!history.length || typeof historyIndex !== 'number') return;
      event.preventDefault();
      const nextIndex = historyIndex - 1;
      if (nextIndex < 0) {
        setHistoryIndex(undefined);
        setText('');
      } else {
        setHistoryIndex(nextIndex);
        setText(history[nextIndex]);
      }
      props.onClear();
    }
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

        {props.warning ? <p className="command-warning">{props.warning}</p> : null}

        <form className="command-input-row" onSubmit={submit}>
          <div className="command-text-wrap">
            <input
              value={text}
              onChange={(event) => updateText(event.target.value)}
              onKeyDown={handleTextKeyDown}
              placeholder="turn desk warm white at 35%"
              autoComplete="off"
              autoFocus
              spellCheck={false}
            />
            {text ? (
              <button type="button" aria-label="Clear command" onClick={clearText}>
                <X size={12} />
              </button>
            ) : null}
          </div>
          <button type="submit" disabled={!canInterpret && !canConfirm}>
            {canConfirm ? 'confirm' : 'interpret'}
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
