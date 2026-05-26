import type { Device } from './lifx';

export interface DeviceDraft {
  base: Device;
  draft: Device;
  history: Device[];
  future: Device[];
  dirty: boolean;
}

export function createDraft(device: Device): DeviceDraft {
  return {
    base: cloneDevice(device),
    draft: cloneDevice(device),
    history: [],
    future: [],
    dirty: false,
  };
}

export function updateDraft(state: DeviceDraft, next: Device): DeviceDraft {
  return {
    ...state,
    draft: cloneDevice(next),
    history: [...state.history, cloneDevice(state.draft)].slice(-40),
    future: [],
    dirty: true,
  };
}

export function undoDraft(state: DeviceDraft): DeviceDraft {
  const previous = state.history[state.history.length - 1];
  if (!previous) return state;
  return {
    ...state,
    draft: cloneDevice(previous),
    history: state.history.slice(0, -1),
    future: [cloneDevice(state.draft), ...state.future],
    dirty: true,
  };
}

export function revertDraft(state: DeviceDraft): DeviceDraft {
  return {
    ...state,
    draft: cloneDevice(state.base),
    history: [],
    future: [],
    dirty: false,
  };
}

export function commitDraft(state: DeviceDraft, committed: Device): DeviceDraft {
  return {
    ...state,
    base: cloneDevice(committed),
    draft: cloneDevice(committed),
    history: [],
    future: [],
    dirty: false,
  };
}

export function cloneDevice(device: Device): Device {
  return JSON.parse(JSON.stringify(device)) as Device;
}
