import { DEFAULT_FLOOR_ID, FLOOR_PLAN_STORAGE_KEY, FLOOR_PLAN_VERSION, createFloorPlanFloor, normalizeFloorPlanLocation, normalizeFloorPlanPreferences, type FloorPlanLocation, type FloorPlanPreferences, type FloorPlanStorage } from './floorPlan.js';
import type { DeviceSnapshot } from './lifx.js';

export const FLOOR_PLAN_PROFILE_VERSION = 2;

export interface FloorPlanProfilePreferences {
  version: 2;
  activeProfileId?: string;
  profiles: Record<string, FloorPlanProfile>;
}

export interface FloorPlanProfile {
  id: string;
  name: string;
  knownDeviceSerials: string[];
  locationHints: string[];
  ignoredSerials: string[];
  layout: FloorPlanLocation;
}

export interface FloorPlanObservation {
  deviceSerials: string[];
  locationIds: string[];
}

export interface FloorPlanProfileCandidate {
  profileId: string;
  serialMatches: number;
  locationMatches: number;
}

export type FloorPlanProfileResolution =
  | { kind: 'matched'; profileId: string; candidates: FloorPlanProfileCandidate[] }
  | { kind: 'ambiguous'; candidates: FloorPlanProfileCandidate[] }
  | { kind: 'new'; candidates: [] };

export function emptyFloorPlanProfilePreferences(): FloorPlanProfilePreferences {
  return { version: FLOOR_PLAN_PROFILE_VERSION, profiles: {} };
}

export function loadFloorPlanProfilePreferences(storage: FloorPlanStorage, key = FLOOR_PLAN_STORAGE_KEY): FloorPlanProfilePreferences {
  try {
    return parseFloorPlanProfilePreferences(storage.getItem(key));
  } catch (error) {
    console.warn('Unable to read floor plan profiles', error);
    return emptyFloorPlanProfilePreferences();
  }
}

export function saveFloorPlanProfilePreferences(
  storage: FloorPlanStorage,
  preferences: FloorPlanProfilePreferences,
  key = FLOOR_PLAN_STORAGE_KEY,
): void {
  try {
    storage.setItem(key, JSON.stringify(normalizeFloorPlanProfilePreferences(preferences)));
  } catch (error) {
    console.warn('Unable to save floor plan profiles', error);
  }
}

export function parseFloorPlanProfilePreferences(value: string | null): FloorPlanProfilePreferences {
  if (!value) return emptyFloorPlanProfilePreferences();
  const parsed: unknown = JSON.parse(value);
  if (isRecord(parsed) && parsed.version === FLOOR_PLAN_VERSION) {
    return migrateLocationFloorPlans(normalizeFloorPlanPreferences(parsed));
  }
  return normalizeFloorPlanProfilePreferences(parsed);
}

export function createDefaultFloorPlanLocation(): FloorPlanLocation {
  const floor = createFloorPlanFloor(DEFAULT_FLOOR_ID, 'Ground');
  return { activeFloorId: floor.id, floors: [floor] };
}

export function createFloorPlanProfile(
  id: string,
  name: string,
  layout: FloorPlanLocation,
  observation: FloorPlanObservation = { deviceSerials: [], locationIds: [] },
): FloorPlanProfile | undefined {
  const cleanProfileId = cleanValue(id);
  const normalizedLayout = normalizeFloorPlanLocation(layout);
  if (!cleanProfileId || !normalizedLayout) return undefined;
  return {
    id: cleanProfileId,
    name: cleanValue(name) || 'Floor plan',
    knownDeviceSerials: uniqueValues(observation.deviceSerials),
    locationHints: uniqueValues(observation.locationIds),
    ignoredSerials: [],
    layout: normalizedLayout,
  };
}

export function normalizeFloorPlanProfilePreferences(value: unknown): FloorPlanProfilePreferences {
  if (!isRecord(value) || value.version !== FLOOR_PLAN_PROFILE_VERSION || !isRecord(value.profiles)) {
    return emptyFloorPlanProfilePreferences();
  }

  const profiles: Record<string, FloorPlanProfile> = {};
  for (const [profileId, valueProfile] of Object.entries(value.profiles)) {
    if (!isRecord(valueProfile)) continue;
    const id = cleanValue(valueProfile.id) || cleanValue(profileId);
    const layout = normalizeFloorPlanLocation(valueProfile.layout);
    if (!id || !layout) continue;
    profiles[id] = {
      id,
      name: cleanValue(valueProfile.name) || 'Floor plan',
      knownDeviceSerials: uniqueValues(valueProfile.knownDeviceSerials),
      locationHints: uniqueValues(valueProfile.locationHints),
      ignoredSerials: uniqueValues(valueProfile.ignoredSerials),
      layout,
    };
  }

  const activeProfileId = cleanValue(value.activeProfileId);
  return {
    version: FLOOR_PLAN_PROFILE_VERSION,
    ...(activeProfileId && profiles[activeProfileId] ? { activeProfileId } : {}),
    profiles,
  };
}

export function resolveFloorPlanProfile(
  preferences: FloorPlanProfilePreferences,
  observation: FloorPlanObservation,
): FloorPlanProfileResolution {
  const current = normalizeFloorPlanProfilePreferences(preferences);
  const serials = new Set(uniqueValues(observation.deviceSerials));
  const locationIds = new Set(uniqueValues(observation.locationIds));
  const candidates = Object.values(current.profiles)
    .map((profile) => candidateForProfile(profile, serials, locationIds))
    .filter((candidate) => candidate.serialMatches > 0 || candidate.locationMatches > 0)
    .sort(compareCandidates);

  if (!candidates.length) return { kind: 'new', candidates: [] };
  if (candidates.length > 1 && sameEvidence(candidates[0], candidates[1])) {
    return { kind: 'ambiguous', candidates };
  }
  return { kind: 'matched', profileId: candidates[0].profileId, candidates };
}

export function observeFloorPlanProfile(
  preferences: FloorPlanProfilePreferences,
  profileId: string,
  observation: FloorPlanObservation,
): FloorPlanProfilePreferences {
  const current = preferences;
  const profile = current.profiles[profileId];
  if (!profile) return current;
  const ignored = new Set(profile.ignoredSerials);
  const observedSerials = uniqueValues(observation.deviceSerials).filter((serial) => !ignored.has(serial));
  const knownDeviceSerials = uniqueValues([...profile.knownDeviceSerials, ...observedSerials]);
  const locationHints = uniqueValues([...profile.locationHints, ...observation.locationIds]);
  if (
    current.activeProfileId === profile.id
    && sameValues(knownDeviceSerials, profile.knownDeviceSerials)
    && sameValues(locationHints, profile.locationHints)
  ) {
    return current;
  }
  return {
    ...current,
    activeProfileId: profile.id,
    profiles: {
      ...current.profiles,
      [profile.id]: {
        ...profile,
        knownDeviceSerials,
        locationHints,
      },
    },
  };
}

export function updateFloorPlanProfileLayout(
  preferences: FloorPlanProfilePreferences,
  profileId: string,
  updater: (layout: FloorPlanLocation) => FloorPlanLocation,
): FloorPlanProfilePreferences {
  const profile = preferences.profiles[profileId];
  if (!profile) return preferences;
  const layout = normalizeFloorPlanLocation(updater(profile.layout));
  if (!layout) return preferences;
  return {
    ...preferences,
    profiles: {
      ...preferences.profiles,
      [profileId]: { ...profile, layout },
    },
  };
}

export function renameFloorPlanProfile(
  preferences: FloorPlanProfilePreferences,
  profileId: string,
  name: string,
): FloorPlanProfilePreferences {
  const profile = preferences.profiles[profileId];
  const cleanName = cleanValue(name);
  if (!profile || !cleanName || profile.name === cleanName) return preferences;
  return {
    ...preferences,
    profiles: {
      ...preferences.profiles,
      [profileId]: { ...profile, name: cleanName },
    },
  };
}

export function floorPlanObservation(snapshot: DeviceSnapshot): FloorPlanObservation {
  const onlineDevices = snapshot.devices.filter((device) => device.online);
  const groupIds = new Set(onlineDevices.map((device) => device.groupId));
  const locationIds = new Set(snapshot.groups.filter((group) => groupIds.has(group.id)).map((group) => group.locationId));
  return {
    deviceSerials: uniqueValues(onlineDevices.map((device) => device.serial)),
    locationIds: uniqueValues([...locationIds]),
  };
}

function migrateLocationFloorPlans(preferences: FloorPlanPreferences): FloorPlanProfilePreferences {
  const profiles: Record<string, FloorPlanProfile> = {};
  for (const [locationId, layout] of Object.entries(preferences.locations)) {
    if (!isMeaningfulLayout(layout)) continue;
    const id = `profile:${encodeURIComponent(locationId)}`;
    const locationHint = locationId.startsWith('lifx-location:') ? [locationId] : [];
    const profile = createFloorPlanProfile(id, legacyProfileName(locationId), layout, {
      deviceSerials: layout.floors.flatMap((floor) => Object.keys(floor.devices)),
      locationIds: locationHint,
    });
    if (profile) profiles[id] = profile;
  }
  const profileIds = Object.keys(profiles);
  return {
    version: FLOOR_PLAN_PROFILE_VERSION,
    ...(profileIds.length === 1 ? { activeProfileId: profileIds[0] } : {}),
    profiles,
  };
}

function isMeaningfulLayout(layout: FloorPlanLocation): boolean {
  if (layout.floors.length !== 1) return true;
  const floor = layout.floors[0];
  return floor.id !== DEFAULT_FLOOR_ID
    || floor.label !== 'Ground'
    || floor.rooms.length > 0
    || Object.keys(floor.devices).length > 0;
}

function legacyProfileName(locationId: string): string {
  return locationId.startsWith('lifx-location:') ? 'Floor plan' : cleanValue(locationId) || 'Floor plan';
}

function candidateForProfile(profile: FloorPlanProfile, serials: Set<string>, locationIds: Set<string>): FloorPlanProfileCandidate {
  const ignored = new Set(profile.ignoredSerials);
  const serialMatches = profile.knownDeviceSerials.filter((serial) => !ignored.has(serial) && serials.has(serial)).length;
  const locationMatches = profile.locationHints.filter((locationId) => locationIds.has(locationId)).length;
  return {
    profileId: profile.id,
    serialMatches,
    locationMatches,
  };
}

function compareCandidates(left: FloorPlanProfileCandidate, right: FloorPlanProfileCandidate): number {
  return right.serialMatches - left.serialMatches
    || right.locationMatches - left.locationMatches
    || left.profileId.localeCompare(right.profileId);
}

function sameEvidence(left: FloorPlanProfileCandidate, right: FloorPlanProfileCandidate): boolean {
  return left.serialMatches === right.serialMatches && left.locationMatches === right.locationMatches;
}

function sameValues(left: string[], right: string[]): boolean {
  return left.length === right.length && left.every((value, index) => value === right[index]);
}

function uniqueValues(value: unknown): string[] {
  if (!Array.isArray(value)) return [];
  return [...new Set(value.map(cleanValue).filter(Boolean))].sort((left, right) => left.localeCompare(right));
}

function cleanValue(value: unknown): string {
  return typeof value === 'string' ? value.trim() : '';
}

function isRecord(value: unknown): value is Record<string, unknown> {
  return typeof value === 'object' && value !== null;
}
