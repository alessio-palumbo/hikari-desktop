import type { SensorNode } from '../backend/api.js';
import type { FloorPlanPresenceConfig } from './floorPlan.js';

export type RoomOccupancyPhase = 'unknown' | 'occupied' | 'pending-off' | 'unoccupied';
export type RoomOccupancyCommand = 'on' | 'off';

export interface RoomOccupancyState {
  phase: RoomOccupancyPhase;
  lightingEnabled: boolean;
  pendingUntil?: number;
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
    return { state: { phase: 'unknown', lightingEnabled: false } };
  }

  const byId = new Map(nodes.map((node) => [node.id, node]));
  // Undiscovered assignments remain unknown; discovered non-presence sensors
  // do not participate in room occupancy semantics.
  const assigned = sensorIds
    .map((id) => byId.get(id))
    .filter((node) => !node || node.capabilities.includes('presence'));
  const lightingEnabled = Boolean(config?.lightingEnabled && assigned.length);
  if (!assigned.length) {
    return { state: { phase: 'unknown', lightingEnabled: false } };
  }
  const anyPresent = assigned.some((node) => node?.online && node.presenceKnown && node.present);
  const allKnownAbsent = assigned.every((node) => node?.online && node.presenceKnown && !node.present);

  if (anyPresent) {
    const wasAlreadyOn = previous.lightingEnabled && (previous.phase === 'occupied' || previous.phase === 'pending-off');
    const shouldTurnOn = lightingEnabled && !wasAlreadyOn;
    return {
      state: { phase: 'occupied', lightingEnabled },
      ...(shouldTurnOn ? { command: 'on' as const } : {}),
    };
  }

  if (!allKnownAbsent) {
    return { state: { phase: 'unknown', lightingEnabled } };
  }

  if (!lightingEnabled) {
    return { state: { phase: 'unoccupied', lightingEnabled: false } };
  }

  if (previous.phase === 'pending-off' && previous.pendingUntil !== undefined) {
    if (now >= previous.pendingUntil) {
      return { state: { phase: 'unoccupied', lightingEnabled: true }, command: 'off' };
    }
    return { state: { ...previous, lightingEnabled: true } };
  }

  if (previous.phase === 'unoccupied' && previous.lightingEnabled) {
    return { state: { phase: 'unoccupied', lightingEnabled: true } };
  }

  const delaySeconds = Math.max(1, config?.offDelaySeconds ?? 30);
  return {
    state: {
      phase: 'pending-off',
      lightingEnabled: true,
      pendingUntil: now + delaySeconds * 1000,
    },
  };
}
