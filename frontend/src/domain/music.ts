import type { Device } from './lifx.js';

export interface MusicStatus { state: 'idle' | 'starting' | 'running' | 'stopping' | 'stopped' | 'error'; serials: string[]; error?: string; runId?: string; revision?: number }
export interface MusicSettings { available: boolean; warning?: string; styles: string[] }
export interface MusicInputDevice { id: string; name: string; default: boolean }
export interface MusicSignal { runId: string; atMs: number; inputDb: number; low: number; mid: number; high: number; energy: number }
export interface MusicControls { runId: string; style: string; dynamics: string; masterBrightness: number }
export interface MusicSelection { serials: string[]; inputId: string; style: string; dynamics: string; sensitivity: string; masterBrightness: number }
export interface StartMusicRequest extends MusicSelection { runId: string }

export function musicActive(status: MusicStatus): boolean { return ['starting', 'running', 'stopping'].includes(status.state); }
export function musicOwnedSerials(status: MusicStatus): Set<string> { return new Set(musicActive(status) ? status.serials : []); }
export function musicAutomationDevices(devices: Device[], status: MusicStatus): Device[] {
  const owned = musicOwnedSerials(status);
  return devices.filter((device) => !owned.has(device.serial));
}
export function newerMusicStatus(current: MusicStatus, incoming: MusicStatus): MusicStatus {
  if ((incoming.revision ?? 0) < (current.revision ?? 0)) return current;
  // An initial snapshot must not overwrite a local pending start.
  if ((incoming.revision ?? 0) === (current.revision ?? 0) && current.runId && current.runId !== incoming.runId) return current;
  return { ...incoming, serials: incoming.serials ?? [] };
}

export function appendMusicSignal(history: MusicSignal[], point: MusicSignal, runId?: string): MusicSignal[] {
  if (!runId || point.runId !== runId || !Number.isFinite(point.atMs) || !Number.isFinite(point.inputDb)) return history;
  if (history.length && point.atMs <= history[history.length - 1].atMs) return history;
  return [...history.filter((sample) => sample.atMs >= point.atMs - 12000), point].slice(-120);
}

export function musicSignalLevel(value: number): number {
  return Number.isFinite(value) ? Math.max(0, Math.min(1, value)) : 0;
}

// Reveal the newest segment over one analysis hop rather than jumping its end
// point on each update. This buffer affects visualisation only.
export function musicSignalEndAt(latestAt: number, elapsedMs: number, running: boolean, reduceMotion: boolean): number {
  if (!running || reduceMotion) return latestAt;
  return latestAt - 100 + Math.max(0, Math.min(100, elapsedMs));
}
