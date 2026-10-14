import type { Device } from './lifx.js';

export interface MusicStatus { state: 'idle' | 'starting' | 'running' | 'stopping' | 'stopped' | 'error'; serials: string[]; error?: string; runId?: string; revision?: number }
export interface MusicSettings { available: boolean; warning?: string; styles: string[] }
export interface MusicInputDevice { id: string; name: string; default: boolean }
export interface StartMusicRequest { runId: string; serials: string[]; inputId: string; style: string; dynamics: string; sensitivity: string }
export interface MusicSelection { serials: string[]; inputId: string; style: string; dynamics: string; sensitivity: string }

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
