import { FormEvent, KeyboardEvent, useEffect, useRef, useState } from 'react';
import { Mic, Sparkles, Square, X } from 'lucide-react';
import type { CommandPreview, CommandPreviewAction, CommandTranscript } from '../backend/api';
import './CommandModal.css';

interface CommandModalProps {
  open: boolean;
  interpreting: boolean;
  transcribing: boolean;
  executing: boolean;
  autoExecute: boolean;
  voiceAvailable: boolean;
  error?: string;
  warning?: string;
  transcript?: CommandTranscript;
  preview?: CommandPreview;
  onClose: () => void;
  onInterpret: (text: string) => void;
  onVoice: (audioBase64: string) => void;
  onConfirm: () => void;
  onAutoExecuteChange: (enabled: boolean) => void;
  onClear: () => void;
}

export function CommandModal(props: CommandModalProps) {
  const [text, setText] = useState('');
  const [history, setHistory] = useState<string[]>([]);
  const [historyIndex, setHistoryIndex] = useState<number | undefined>();
  const [recording, setRecording] = useState(false);
  const [recordingError, setRecordingError] = useState<string | undefined>();
  const audioContextRef = useRef<AudioContext | undefined>(undefined);
  const processorRef = useRef<ScriptProcessorNode | undefined>(undefined);
  const sourceRef = useRef<MediaStreamAudioSourceNode | undefined>(undefined);
  const streamRef = useRef<MediaStream | undefined>(undefined);
  const chunksRef = useRef<Float32Array[]>([]);
  const sampleRateRef = useRef(48000);
  const startedAtRef = useRef(0);
  const pressingRef = useRef(false);
  const keyboardRecordingRef = useRef(false);
  const maxTimerRef = useRef<number | undefined>(undefined);

  useEffect(() => {
    if (props.open) return;
    setText('');
    setHistoryIndex(undefined);
    stopRecording(false);
    props.onClear();
  }, [props.open]);

  const canInterpret = text.trim().length > 0 && !props.interpreting && !props.transcribing && !recording;
  const canConfirm = !!props.preview && !props.preview.empty && !props.executing && !props.interpreting && !props.transcribing && !recording;
  const microphoneAvailable = browserMicrophoneAvailable();
  const voiceDisabled = !props.voiceAvailable || !microphoneAvailable || props.transcribing || props.interpreting || props.executing;

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
    setRecordingError(undefined);
    props.onClear();
  };

  const beginRecording = async () => {
    if (voiceDisabled || recording) return;
    pressingRef.current = true;
    setRecording(true);
    setRecordingError(undefined);
    try {
      if (!navigator.mediaDevices?.getUserMedia) {
        throw new Error('microphone recording is not available in this WebView');
      }
      const stream = await navigator.mediaDevices.getUserMedia({ audio: { channelCount: 1, echoCancellation: true, noiseSuppression: true } });
      if (!pressingRef.current) {
        stream.getTracks().forEach((track) => track.stop());
        return;
      }
      const AudioContextCtor = window.AudioContext || window.webkitAudioContext;
      const context = new AudioContextCtor();
      const source = context.createMediaStreamSource(stream);
      const processor = context.createScriptProcessor(4096, 1, 1);
      chunksRef.current = [];
      sampleRateRef.current = context.sampleRate;
      startedAtRef.current = performance.now();
      processor.onaudioprocess = (audioEvent) => {
        chunksRef.current.push(new Float32Array(audioEvent.inputBuffer.getChannelData(0)));
      };
      source.connect(processor);
      processor.connect(context.destination);
      streamRef.current = stream;
      audioContextRef.current = context;
      sourceRef.current = source;
      processorRef.current = processor;
      maxTimerRef.current = window.setTimeout(() => void stopRecording(true), 10000);
    } catch (error) {
      pressingRef.current = false;
      keyboardRecordingRef.current = false;
      cleanupRecording();
      setRecording(false);
      setRecordingError(errorMessage(error));
    }
  };

  const togglePointerRecording = () => {
    if (recording) {
      void stopRecording(true);
      return;
    }
    void beginRecording();
  };

  const stopRecording = async (submit: boolean) => {
    pressingRef.current = false;
    keyboardRecordingRef.current = false;
    if (maxTimerRef.current) window.clearTimeout(maxTimerRef.current);
    const elapsed = performance.now() - startedAtRef.current;
    const chunks = chunksRef.current;
    const sampleRate = sampleRateRef.current;
    cleanupRecording();
    setRecording(false);
    if (!submit || !chunks.length) return;
    if (elapsed < 400) {
      setRecordingError('hold a little longer');
      return;
    }
    try {
      props.onVoice(arrayBufferToBase64(encodeWav(chunks, sampleRate)));
    } catch (error) {
      setRecordingError(errorMessage(error));
    }
  };

  const cleanupRecording = () => {
    if (maxTimerRef.current) window.clearTimeout(maxTimerRef.current);
    maxTimerRef.current = undefined;
    processorRef.current?.disconnect();
    sourceRef.current?.disconnect();
    streamRef.current?.getTracks().forEach((track) => track.stop());
    void audioContextRef.current?.close();
    processorRef.current = undefined;
    sourceRef.current = undefined;
    streamRef.current = undefined;
    audioContextRef.current = undefined;
  };

  const handleTextKeyDown = (event: KeyboardEvent<HTMLInputElement>) => {
    if (event.key === ' ' && text.length === 0) {
      event.preventDefault();
      event.stopPropagation();
      if (voiceDisabled || event.repeat || pressingRef.current) return;
      keyboardRecordingRef.current = true;
      void beginRecording();
      return;
    }

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

  const handleTextKeyUp = (event: KeyboardEvent<HTMLInputElement>) => {
    if (event.key !== ' ' || !keyboardRecordingRef.current) return;
    event.preventDefault();
    event.stopPropagation();
    void stopRecording(true);
  };

  const handleTextBeforeInput = (event: FormEvent<HTMLInputElement>) => {
    const nativeEvent = event.nativeEvent as InputEvent;
    if (text.length === 0 && nativeEvent.data?.trim() === '') event.preventDefault();
  };

  useEffect(() => {
    if (!props.open) return undefined;
    const onKeyDown = (event: globalThis.KeyboardEvent) => {
      if (event.key === 'Escape') {
        event.preventDefault();
        event.stopPropagation();
        if (recording) void stopRecording(false);
        else props.onClose();
        return;
      }
      if (event.key !== ' ' || text.length !== 0 || commandVoiceShortcutBlocksTarget(event.target)) return;
      event.preventDefault();
      event.stopPropagation();
      if (voiceDisabled || event.repeat || pressingRef.current) return;
      keyboardRecordingRef.current = true;
      void beginRecording();
    };
    const onKeyUp = (event: globalThis.KeyboardEvent) => {
      if (event.key !== ' ' || !keyboardRecordingRef.current) return;
      event.preventDefault();
      event.stopPropagation();
      void stopRecording(true);
    };
    window.addEventListener('keydown', onKeyDown);
    window.addEventListener('keyup', onKeyUp);
    return () => {
      window.removeEventListener('keydown', onKeyDown);
      window.removeEventListener('keyup', onKeyUp);
    };
  }, [props.open, text, voiceDisabled, recording, props.onClose]);

  if (!props.open) return null;

  return (
    <div className="command-backdrop" role="presentation" onMouseDown={(event) => {
      if (event.target === event.currentTarget) props.onClose();
    }}>
      <section className="command-modal" role="dialog" aria-modal="true" aria-label="Text command">
        <header className="command-header">
          <div>
            <Sparkles size={15} />
            <strong>quick action</strong>
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
              onBeforeInput={handleTextBeforeInput}
              onKeyDown={handleTextKeyDown}
              onKeyUp={handleTextKeyUp}
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
          <button
            type="button"
            className="command-voice-button"
            data-recording={recording ? 'true' : 'false'}
            disabled={voiceDisabled && !recording}
            aria-label={recording ? 'Stop recording' : 'Start recording'}
            title={voiceButtonTitle(props.voiceAvailable, microphoneAvailable)}
            onClick={togglePointerRecording}
          >
            {recording ? <Square size={12} /> : <Mic size={13} />}
          </button>
          <button type="submit" className="command-submit-button" disabled={!canInterpret && !canConfirm}>
            {props.transcribing ? 'transcribing' : canConfirm ? 'confirm' : 'interpret'}
          </button>
        </form>

        <p className="command-voice-status" data-active={recording || props.transcribing ? 'true' : 'false'}>
          {recording ? 'listening' : props.transcribing ? 'transcribing' : '\u00a0'}
        </p>

        <label className="command-auto-execute" title="Automatically run high-confidence commands that do not require confirmation.">
          <input type="checkbox" checked={props.autoExecute} onChange={(event) => props.onAutoExecuteChange(event.target.checked)} />
          <span>auto-run high confidence</span>
        </label>

        {props.voiceAvailable && !microphoneAvailable ? <p className="command-error">Microphone recording is not available in this WebView.</p> : null}
        {recordingError ? <p className="command-error">{recordingError}</p> : null}
        {props.error ? <p className="command-error">{props.error}</p> : null}

        {props.transcript?.text ? <p className="command-transcript">Heard: "{props.transcript.text}"</p> : null}

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

function commandVoiceShortcutBlocksTarget(target: EventTarget | null): boolean {
  if (!(target instanceof HTMLElement)) return false;
  const tag = target.tagName.toLowerCase();
  return tag === 'input' || tag === 'textarea' || tag === 'select' || tag === 'button' || target.isContentEditable;
}

function browserMicrophoneAvailable(): boolean {
  return typeof navigator !== 'undefined' && typeof navigator.mediaDevices?.getUserMedia === 'function';
}

function voiceButtonTitle(voiceAvailable: boolean, microphoneAvailable: boolean): string {
  if (!voiceAvailable) return 'Voice commands are not configured';
  if (!microphoneAvailable) return 'Microphone recording is not available in this WebView';
  return 'Click to start or stop recording, or hold Space while the command is empty';
}

function encodeWav(chunks: Float32Array[], sourceSampleRate: number): ArrayBuffer {
  const samples = downsample(mergeChunks(chunks), sourceSampleRate, 16000);
  const buffer = new ArrayBuffer(44 + samples.length * 2);
  const view = new DataView(buffer);
  writeString(view, 0, 'RIFF');
  view.setUint32(4, 36 + samples.length * 2, true);
  writeString(view, 8, 'WAVE');
  writeString(view, 12, 'fmt ');
  view.setUint32(16, 16, true);
  view.setUint16(20, 1, true);
  view.setUint16(22, 1, true);
  view.setUint32(24, 16000, true);
  view.setUint32(28, 16000 * 2, true);
  view.setUint16(32, 2, true);
  view.setUint16(34, 16, true);
  writeString(view, 36, 'data');
  view.setUint32(40, samples.length * 2, true);
  let offset = 44;
  for (const sample of samples) {
    const clamped = Math.max(-1, Math.min(1, sample));
    view.setInt16(offset, clamped < 0 ? clamped * 0x8000 : clamped * 0x7fff, true);
    offset += 2;
  }
  return buffer;
}

function mergeChunks(chunks: Float32Array[]): Float32Array {
  const length = chunks.reduce((total, chunk) => total + chunk.length, 0);
  const merged = new Float32Array(length);
  let offset = 0;
  for (const chunk of chunks) {
    merged.set(chunk, offset);
    offset += chunk.length;
  }
  return merged;
}

function downsample(samples: Float32Array, sourceRate: number, targetRate: number): Float32Array {
  if (sourceRate === targetRate) return samples;
  const ratio = sourceRate / targetRate;
  const length = Math.floor(samples.length / ratio);
  const result = new Float32Array(length);
  for (let i = 0; i < length; i += 1) {
    const start = Math.floor(i * ratio);
    const end = Math.min(Math.floor((i + 1) * ratio), samples.length);
    let sum = 0;
    for (let j = start; j < end; j += 1) sum += samples[j];
    result[i] = sum / Math.max(1, end - start);
  }
  return result;
}

function arrayBufferToBase64(buffer: ArrayBuffer): string {
  const bytes = new Uint8Array(buffer);
  let binary = '';
  const chunkSize = 0x8000;
  for (let i = 0; i < bytes.length; i += chunkSize) {
    binary += String.fromCharCode(...bytes.subarray(i, i + chunkSize));
  }
  return window.btoa(binary);
}

function writeString(view: DataView, offset: number, value: string) {
  for (let i = 0; i < value.length; i += 1) view.setUint8(offset + i, value.charCodeAt(i));
}

function errorMessage(error: unknown): string {
  if (error instanceof Error) return error.message;
  if (typeof error === 'string') return error;
  return 'voice command failed';
}

declare global {
  interface Window {
    webkitAudioContext?: typeof AudioContext;
  }
}
