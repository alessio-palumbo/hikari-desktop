import { normalizeFloorPlanLocation, type FloorPlanLocation } from './floorPlan.js';

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
  const current = normalizeFloorPlanProfilePreferences(preferences);
  const profile = current.profiles[profileId];
  if (!profile) return current;
  const ignored = new Set(profile.ignoredSerials);
  const observedSerials = uniqueValues(observation.deviceSerials).filter((serial) => !ignored.has(serial));
  return {
    ...current,
    activeProfileId: profile.id,
    profiles: {
      ...current.profiles,
      [profile.id]: {
        ...profile,
        knownDeviceSerials: uniqueValues([...profile.knownDeviceSerials, ...observedSerials]),
        locationHints: uniqueValues([...profile.locationHints, ...observation.locationIds]),
      },
    },
  };
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
