import type { SensorNode } from '../backend/api.js';
import type { FloorPlanPresenceConfig } from './floorPlan.js';

export type RoomOccupancyPhase = 'unknown' | 'occupied' | 'pending-clear' | 'unoccupied';
export type RoomOccupancyCommand = 'on' | 'off' | 'dim' | 'restore';

export interface RoomOccupancyState {
  phase: RoomOccupancyPhase;
  lightingEnabled: boolean;
  pendingUntil?: number;
  clearActionApplied?: 'off' | 'dim';
}

export interface RoomOccupancyTransition {
  state: RoomOccupancyState;
  command?: RoomOccupancyCommand;
}

export const initialRoomOccupancyState = (): RoomOccupancyState => ({
  phase: 'unknown',
  lightingEnabled: false,
});

// reconcileRoomOccupancy interprets complete sensor observations. Missing or
// offline sensors are uncertainty, never a confident absence.
export function reconcileRoomOccupancy(
  previous: RoomOccupancyState,
  config: FloorPlanPresenceConfig | undefined,
  nodes: SensorNode[],
  now: number,
): RoomOccupancyTransition {
  const sensorIds = config?.sensorIds ?? [];
  if (!sensorIds.length) {
    return {
      state: { phase: 'unknown', lightingEnabled: false },
      ...(previous.clearActionApplied === 'dim' ? { command: 'restore' as const } : {}),
    };
  }

  const byId = new Map(nodes.map((node) => [node.id, node]));
  // Undiscovered assignments remain unknown; discovered non-presence sensors
  // do not participate in room occupancy semantics.
  const assigned = sensorIds
    .map((id) => byId.get(id))
    .filter((node) => !node || node.capabilities.includes('presence'));
  const lightingEnabled = Boolean(config?.lightingEnabled && assigned.length);
  if (!assigned.length) {
    return {
      state: { phase: 'unknown', lightingEnabled: false },
      ...(previous.clearActionApplied === 'dim' ? { command: 'restore' as const } : {}),
    };
  }
  const anyPresent = assigned.some((node) => node?.online && node.presenceKnown && node.present);
  const allKnownAbsent = assigned.every((node) => node?.online && node.presenceKnown && !node.present);

  if (anyPresent) {
    const wasAlreadyOn = previous.lightingEnabled && (previous.phase === 'occupied' || previous.phase === 'pending-clear');
    const shouldTurnOn = lightingEnabled && !wasAlreadyOn;
    const command = previous.clearActionApplied === 'dim' ? 'restore' : shouldTurnOn ? 'on' : undefined;
    return {
      state: { phase: 'occupied', lightingEnabled },
      ...(command ? { command } : {}),
    };
  }

  if (!allKnownAbsent) {
    return {
      state: {
        phase: 'unknown',
        lightingEnabled,
        ...(previous.clearActionApplied ? { clearActionApplied: previous.clearActionApplied } : {}),
      },
    };
  }

  if (!lightingEnabled) {
    return {
      state: { phase: 'unoccupied', lightingEnabled: false },
      ...(previous.clearActionApplied === 'dim' ? { command: 'restore' as const } : {}),
    };
  }

  if (previous.clearActionApplied) {
    if (previous.clearActionApplied === 'dim' && config?.clearAction !== 'dim') {
      const delaySeconds = Math.max(1, config?.offDelaySeconds ?? 30);
      return {
        state: { phase: 'pending-clear', lightingEnabled: true, pendingUntil: now + delaySeconds * 1000 },
        command: 'restore',
      };
    }
    return {
      state: { phase: 'unoccupied', lightingEnabled: true, clearActionApplied: previous.clearActionApplied },
    };
  }

  if (previous.phase === 'pending-clear' && previous.pendingUntil !== undefined) {
    if (now >= previous.pendingUntil) {
      const clearAction = config?.clearAction === 'dim' ? 'dim' : 'off';
      return {
        state: { phase: 'unoccupied', lightingEnabled: true, clearActionApplied: clearAction },
        command: clearAction,
      };
    }
    return { state: { ...previous, lightingEnabled: true } };
  }

  if (previous.phase === 'unoccupied' && previous.lightingEnabled) {
    return { state: { phase: 'unoccupied', lightingEnabled: true } };
  }

  const delaySeconds = Math.max(1, config?.offDelaySeconds ?? 30);
  return {
    state: {
      phase: 'pending-clear',
      lightingEnabled: true,
      pendingUntil: now + delaySeconds * 1000,
    },
  };
}
