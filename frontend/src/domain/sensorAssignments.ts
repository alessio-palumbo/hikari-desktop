import type { FloorPlanProfilePreferences } from './floorPlanProfiles.js';
import type { SensorNode } from '../backend/api.js';

export interface SensorAssignmentHint {
  kind: 'move' | 'other-profile';
  label: string;
}

export type RoomSensorState = 'none' | 'offline' | 'online' | 'occupied';

interface SensorAssignment {
  profileId: string;
  profileName: string;
  floorId: string;
  floorLabel: string;
  roomId: string;
  roomLabel: string;
}

export function sensorAssignmentHints(
  preferences: FloorPlanProfilePreferences,
  currentProfileId: string,
  currentFloorId: string,
  currentRoomId: string,
): Record<string, SensorAssignmentHint> {
  const assignments = collectSensorAssignments(preferences);
  const hints: Record<string, SensorAssignmentHint> = {};

  for (const [sensorId, entries] of Object.entries(assignments)) {
    const currentProfile = entries.find((entry) => entry.profileId === currentProfileId);
    if (currentProfile && (currentProfile.floorId !== currentFloorId || currentProfile.roomId !== currentRoomId)) {
      hints[sensorId] = {
        kind: 'move',
        label: roomAssignmentLabel(currentProfile),
      };
      continue;
    }
    if (currentProfile) continue;

    const elsewhere = entries.filter((entry) => entry.profileId !== currentProfileId);
    if (elsewhere.length) {
      const first = elsewhere[0];
      hints[sensorId] = {
        kind: 'other-profile',
        label: `${first.profileName} / ${roomAssignmentLabel(first)}${elsewhere.length > 1 ? ` +${elsewhere.length - 1}` : ''}`,
      };
    }
  }

  return hints;
}

export function roomSensorState(sensorIds: string[], sensors: SensorNode[]): RoomSensorState {
  if (!sensorIds.length) return 'none';
  const assigned = sensorIds
    .map((id) => sensors.find((sensor) => sensor.id === id))
    .filter((sensor): sensor is SensorNode => !!sensor);
  if (assigned.some((sensor) => sensor.online && sensor.presenceKnown && sensor.present)) return 'occupied';
  if (assigned.some((sensor) => sensor.online)) return 'online';
  return 'offline';
}

function collectSensorAssignments(preferences: FloorPlanProfilePreferences): Record<string, SensorAssignment[]> {
  const assignments: Record<string, SensorAssignment[]> = {};
  for (const profile of Object.values(preferences.profiles)) {
    for (const floor of profile.layout.floors) {
      for (const room of floor.rooms) {
        for (const sensorId of room.presence?.sensorIds ?? []) {
          (assignments[sensorId] ??= []).push({
            profileId: profile.id,
            profileName: profile.name,
            floorId: floor.id,
            floorLabel: floor.label,
            roomId: room.id,
            roomLabel: room.label,
          });
        }
      }
    }
  }
  return assignments;
}

function roomAssignmentLabel(assignment: SensorAssignment): string {
  return assignment.floorId === 'ground'
    ? assignment.roomLabel
    : `${assignment.floorLabel} / ${assignment.roomLabel}`;
}
