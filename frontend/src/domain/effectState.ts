import type { Device } from './lifx.js';
import { deviceEffectSource } from './effects.js';

export interface EffectDisplayStatus {
  serial: string;
  running: boolean;
  effect?: string;
  speedMs?: number;
  ownedByHikari?: boolean;
  error?: string;
  loading?: boolean;
  pendingUntil?: number;
}

// Firmware observations are authoritative once a command acknowledgement is
// settled. During Hikari's short acknowledgement window, retain the optimistic
// status so stale pre-command reports cannot flicker start or stop controls.
export function displayedEffectStatus(
  device: Device,
  optimistic?: EffectDisplayStatus,
  now = Date.now(),
): EffectDisplayStatus | undefined {
  const observed = device.firmwareEffect;
  if (!observed) return optimistic;
  const observationMatches = optimistic?.running === observed.running
    && (!observed.running || !optimistic.effect || optimistic.effect === observed.effect);
  if (observationMatches && !observed.hikariPending) {
    if (!observed.running) return { serial: device.serial, running: false };
    return {
      serial: device.serial,
      running: true,
      effect: observed.effect,
      speedMs: observed.speedMs,
      ownedByHikari: observed.ownedByHikari,
    };
  }
  if (optimistic?.loading || observed.hikariPending || (optimistic?.pendingUntil ?? 0) > now) return optimistic;
  if (
    observed.running
    && observed.ownedByHikari
    && optimistic?.running === false
    && optimistic.effect
    && deviceEffectSource(optimistic.effect) === 'firmware'
  ) {
    return optimistic;
  }
  if (observed.running) {
    return {
      serial: device.serial,
      running: true,
      effect: observed.effect,
      speedMs: observed.speedMs,
      ownedByHikari: observed.ownedByHikari,
    };
  }
  if (optimistic?.effect && deviceEffectSource(optimistic.effect) === 'firmware') {
    return { serial: device.serial, running: false };
  }
  return optimistic;
}
