export const FLOOR_PLAN_STORAGE_KEY = 'hikari:floorPlan';
export const FLOOR_PLAN_VERSION = 1;
export const DEFAULT_FLOOR_ID = 'floor-ground';

export type FloorPlanRoomType =
  | 'bedroom'
  | 'living'
  | 'kitchen'
  | 'bathroom'
  | 'office'
  | 'hallway'
  | 'garage'
  | 'outdoor'
  | 'utility'
  | 'other';

export interface FloorPlanPreferences {
  version: 1;
  locations: Record<string, FloorPlanLocation>;
}

export interface FloorPlanLocation {
  activeFloorId?: string;
  floors: FloorPlanFloor[];
}

export interface FloorPlanFloor {
  id: string;
  label: string;
  rooms: FloorPlanRoom[];
  devices: Record<string, FloorPlanDevicePlacement>;
}

export interface FloorPlanRoom {
  id: string;
  label: string;
  type?: FloorPlanRoomType;
  points: FloorPlanPoint[];
}

export interface FloorPlanDevicePlacement {
  x: number;
  y: number;
  roomId?: string;
}

export interface FloorPlanPoint {
  x: number;
  y: number;
}

export interface FloorPlanStorage {
  getItem(key: string): string | null;
  setItem(key: string, value: string): void;
}

const roomTypes = new Set<FloorPlanRoomType>([
  'bedroom',
  'living',
  'kitchen',
  'bathroom',
  'office',
  'hallway',
  'garage',
  'outdoor',
  'utility',
  'other',
]);

export function emptyFloorPlanPreferences(): FloorPlanPreferences {
  return { version: FLOOR_PLAN_VERSION, locations: {} };
}

export function loadFloorPlanPreferences(storage: FloorPlanStorage, key = FLOOR_PLAN_STORAGE_KEY): FloorPlanPreferences {
  try {
    return parseFloorPlanPreferences(storage.getItem(key));
  } catch (error) {
    console.warn('Unable to read floor plan preferences', error);
    return emptyFloorPlanPreferences();
  }
}

export function saveFloorPlanPreferences(storage: FloorPlanStorage, preferences: FloorPlanPreferences, key = FLOOR_PLAN_STORAGE_KEY): void {
  try {
    storage.setItem(key, JSON.stringify(normalizeFloorPlanPreferences(preferences)));
  } catch (error) {
    console.warn('Unable to save floor plan preferences', error);
  }
}

export function parseFloorPlanPreferences(value: string | null): FloorPlanPreferences {
  if (!value) return emptyFloorPlanPreferences();
  return normalizeFloorPlanPreferences(JSON.parse(value) as Partial<FloorPlanPreferences>);
}

export function ensureLocationFloorPlan(preferences: FloorPlanPreferences, locationId: string, floorLabel = 'Ground'): FloorPlanPreferences {
  const id = cleanId(locationId);
  if (!id) return normalizeFloorPlanPreferences(preferences);

  const current = normalizeFloorPlanPreferences(preferences);
  const existing = current.locations[id];
  if (existing?.floors.length) return current;

  const floor = createFloorPlanFloor(DEFAULT_FLOOR_ID, floorLabel);
  return {
    ...current,
    locations: {
      ...current.locations,
      [id]: {
        activeFloorId: floor.id,
        floors: [floor],
      },
    },
  };
}

export function createFloorPlanFloor(id: string, label: string): FloorPlanFloor {
  return {
    id: cleanId(id) || DEFAULT_FLOOR_ID,
    label: cleanLabel(label) || 'Floor',
    rooms: [],
    devices: {},
  };
}

export function createRectangleRoom(
  id: string,
  label: string,
  type: FloorPlanRoomType | undefined,
  origin: FloorPlanPoint,
  size: FloorPlanPoint,
): FloorPlanRoom {
  const x1 = clampUnit(origin.x);
  const y1 = clampUnit(origin.y);
  const x2 = clampUnit(origin.x + size.x);
  const y2 = clampUnit(origin.y + size.y);
  const left = Math.min(x1, x2);
  const right = Math.max(x1, x2);
  const top = Math.min(y1, y2);
  const bottom = Math.max(y1, y2);

  return {
    id: cleanId(id) || 'room',
    label: cleanLabel(label) || 'Room',
    ...(type && roomTypes.has(type) ? { type } : {}),
    points: [
      { x: left, y: top },
      { x: right, y: top },
      { x: right, y: bottom },
      { x: left, y: bottom },
    ],
  };
}

export function placeDeviceOnFloor(
  preferences: FloorPlanPreferences,
  locationId: string,
  floorId: string,
  serial: string,
  placement: FloorPlanDevicePlacement,
): FloorPlanPreferences {
  const normalized = ensureLocationFloorPlan(preferences, locationId);
  const cleanLocationId = cleanId(locationId);
  const cleanFloorId = cleanId(floorId);
  const cleanSerial = cleanId(serial);
  const location = normalized.locations[cleanLocationId];
  if (!location || !cleanSerial) return normalized;

  return {
    ...normalized,
    locations: {
      ...normalized.locations,
      [cleanLocationId]: {
        ...location,
        activeFloorId: cleanFloorId || location.activeFloorId,
        floors: location.floors.map((floor) => {
          if (floor.id !== cleanFloorId) return floor;
          return {
            ...floor,
            devices: {
              ...floor.devices,
              [cleanSerial]: normalizePlacement(placement),
            },
          };
        }),
      },
    },
  };
}

export function normalizeFloorPlanPreferences(value: Partial<FloorPlanPreferences> | unknown): FloorPlanPreferences {
  if (!isRecord(value)) return emptyFloorPlanPreferences();
  if (value.version !== FLOOR_PLAN_VERSION || !isRecord(value.locations)) return emptyFloorPlanPreferences();

  const locations: Record<string, FloorPlanLocation> = {};
  for (const [locationId, location] of Object.entries(value.locations)) {
    const cleanLocationId = cleanId(locationId);
    if (!cleanLocationId || !isRecord(location) || !Array.isArray(location.floors)) continue;
    const floors = location.floors.map(normalizeFloor).filter((floor): floor is FloorPlanFloor => !!floor);
    if (!floors.length) continue;
    const activeFloorId = cleanId(location.activeFloorId);
    locations[cleanLocationId] = {
      activeFloorId: floors.some((floor) => floor.id === activeFloorId) ? activeFloorId : floors[0].id,
      floors,
    };
  }

  return { version: FLOOR_PLAN_VERSION, locations };
}

export function normalizePoint(point: FloorPlanPoint): FloorPlanPoint {
  return { x: clampUnit(point.x), y: clampUnit(point.y) };
}

function normalizeFloor(value: unknown): FloorPlanFloor | undefined {
  if (!isRecord(value)) return undefined;
  const id = cleanId(value.id);
  if (!id) return undefined;

  const rooms = Array.isArray(value.rooms) ? value.rooms.map(normalizeRoom).filter((room): room is FloorPlanRoom => !!room) : [];
  const devices: Record<string, FloorPlanDevicePlacement> = {};
  if (isRecord(value.devices)) {
    for (const [serial, placement] of Object.entries(value.devices)) {
      const cleanSerial = cleanId(serial);
      if (!cleanSerial || !isRecord(placement)) continue;
      devices[cleanSerial] = normalizePlacement(placement);
    }
  }

  return {
    id,
    label: cleanLabel(value.label) || 'Floor',
    rooms,
    devices,
  };
}

function normalizeRoom(value: unknown): FloorPlanRoom | undefined {
  if (!isRecord(value) || !Array.isArray(value.points)) return undefined;
  const id = cleanId(value.id);
  if (!id) return undefined;
  const points = value.points.filter(isPointLike).map(normalizePoint);
  if (points.length < 3) return undefined;
  const type = roomTypes.has(value.type as FloorPlanRoomType) ? (value.type as FloorPlanRoomType) : undefined;
  return {
    id,
    label: cleanLabel(value.label) || 'Room',
    type,
    points,
  };
}

function normalizePlacement(value: FloorPlanDevicePlacement | Record<string, unknown>): FloorPlanDevicePlacement {
  const roomId = cleanId(value.roomId);
  return {
    ...normalizePoint({ x: numberOrZero(value.x), y: numberOrZero(value.y) }),
    ...(roomId ? { roomId } : {}),
  };
}

function isPointLike(value: unknown): value is FloorPlanPoint {
  return isRecord(value) && typeof value.x === 'number' && typeof value.y === 'number';
}

function isRecord(value: unknown): value is Record<string, unknown> {
  return typeof value === 'object' && value !== null;
}

function cleanId(value: unknown): string {
  return typeof value === 'string' ? value.trim() : '';
}

function cleanLabel(value: unknown): string {
  return typeof value === 'string' ? value.trim() : '';
}

function numberOrZero(value: unknown): number {
  return typeof value === 'number' && Number.isFinite(value) ? value : 0;
}

function clampUnit(value: number): number {
  if (!Number.isFinite(value)) return 0;
  return Math.max(0, Math.min(1, value));
}
