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

export type FloorPlanRoomPatch = Partial<Pick<FloorPlanRoom, 'label' | 'type' | 'points'>> & {
  devices?: Record<string, FloorPlanDevicePlacement>;
};

export const FLOOR_PLAN_ROOM_TYPES: FloorPlanRoomType[] = [
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
];

const roomTypes = new Set<FloorPlanRoomType>(FLOOR_PLAN_ROOM_TYPES);

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

export function addFloorToLocation(preferences: FloorPlanPreferences, locationId: string, floor: FloorPlanFloor): FloorPlanPreferences {
  const normalized = ensureLocationFloorPlan(preferences, locationId);
  const cleanLocationId = cleanId(locationId);
  const normalizedFloor = normalizeFloor(floor);
  const location = normalized.locations[cleanLocationId];
  if (!location || !normalizedFloor) return normalized;

  return {
    ...normalized,
    locations: {
      ...normalized.locations,
      [cleanLocationId]: {
        activeFloorId: normalizedFloor.id,
        floors: [...location.floors.filter((existing) => existing.id !== normalizedFloor.id), normalizedFloor],
      },
    },
  };
}

export function setActiveFloor(preferences: FloorPlanPreferences, locationId: string, floorId: string): FloorPlanPreferences {
  const normalized = ensureLocationFloorPlan(preferences, locationId);
  const cleanLocationId = cleanId(locationId);
  const cleanFloorId = cleanId(floorId);
  const location = normalized.locations[cleanLocationId];
  if (!location || !location.floors.some((floor) => floor.id === cleanFloorId)) return normalized;

  return {
    ...normalized,
    locations: {
      ...normalized.locations,
      [cleanLocationId]: { ...location, activeFloorId: cleanFloorId },
    },
  };
}

export function updateFloorLabel(preferences: FloorPlanPreferences, locationId: string, floorId: string, label: string): FloorPlanPreferences {
  const cleanLabelValue = cleanLabel(label);
  if (!cleanLabelValue) return normalizeFloorPlanPreferences(preferences);
  return updateFloor(preferences, locationId, floorId, (floor) => ({ ...floor, label: cleanLabelValue }));
}

export function removeFloorFromLocation(preferences: FloorPlanPreferences, locationId: string, floorId: string): FloorPlanPreferences {
  const normalized = ensureLocationFloorPlan(preferences, locationId);
  const cleanLocationId = cleanId(locationId);
  const cleanFloorId = cleanId(floorId);
  const location = normalized.locations[cleanLocationId];
  if (!location || location.floors.length <= 1) return normalized;

  const removedIndex = location.floors.findIndex((floor) => floor.id === cleanFloorId);
  const floors = location.floors.filter((floor) => floor.id !== cleanFloorId);
  if (floors.length === location.floors.length) return normalized;
  const fallbackFloor = floors[Math.max(0, removedIndex - 1)] ?? floors[0];

  return {
    ...normalized,
    locations: {
      ...normalized.locations,
      [cleanLocationId]: {
        activeFloorId: floors.some((floor) => floor.id === location.activeFloorId) ? location.activeFloorId : fallbackFloor.id,
        floors,
      },
    },
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
          if (floor.id !== cleanFloorId) {
            if (!floor.devices[cleanSerial]) return floor;
            const devices = { ...floor.devices };
            delete devices[cleanSerial];
            return { ...floor, devices };
          }
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

export function removeDeviceFromFloorPlan(
  preferences: FloorPlanPreferences,
  locationId: string,
  serial: string,
): FloorPlanPreferences {
  const normalized = ensureLocationFloorPlan(preferences, locationId);
  const cleanLocationId = cleanId(locationId);
  const cleanSerial = cleanId(serial);
  const location = normalized.locations[cleanLocationId];
  if (!location || !cleanSerial) return normalized;

  return {
    ...normalized,
    locations: {
      ...normalized.locations,
      [cleanLocationId]: {
        ...location,
        floors: location.floors.map((floor) => {
          if (!floor.devices[cleanSerial]) return floor;
          const devices = { ...floor.devices };
          delete devices[cleanSerial];
          return { ...floor, devices };
        }),
      },
    },
  };
}

export function addRoomToFloor(
  preferences: FloorPlanPreferences,
  locationId: string,
  floorId: string,
  room: FloorPlanRoom,
): FloorPlanPreferences {
  const normalized = ensureLocationFloorPlan(preferences, locationId);
  const cleanLocationId = cleanId(locationId);
  const cleanFloorId = cleanId(floorId);
  const location = normalized.locations[cleanLocationId];
  const normalizedRoom = normalizeRoom(room);
  if (!location || !normalizedRoom) return normalized;

  return {
    ...normalized,
    locations: {
      ...normalized.locations,
      [cleanLocationId]: {
        ...location,
        activeFloorId: cleanFloorId || location.activeFloorId,
        floors: location.floors.map((floor) => {
          if (floor.id !== cleanFloorId) return floor;
          return { ...floor, rooms: [...floor.rooms.filter((existing) => existing.id !== normalizedRoom.id), normalizedRoom] };
        }),
      },
    },
  };
}

export function updateRoomInFloor(
  preferences: FloorPlanPreferences,
  locationId: string,
  floorId: string,
  roomId: string,
  patch: FloorPlanRoomPatch,
): FloorPlanPreferences {
  const cleanRoomId = cleanId(roomId);
  if (!cleanRoomId) return normalizeFloorPlanPreferences(preferences);

  return updateFloor(preferences, locationId, floorId, (floor) => ({
    ...floor,
    ...updateFloorRoomGeometry(floor, cleanRoomId, patch),
  }));
}

function updateFloorRoomGeometry(
  floor: FloorPlanFloor,
  roomId: string,
  patch: FloorPlanRoomPatch,
): Pick<FloorPlanFloor, 'rooms' | 'devices'> {
  let delta: FloorPlanPoint | undefined;
  const rooms = floor.rooms.map((room) => {
    if (room.id !== roomId) return room;
    const label = patch.label === undefined ? room.label : cleanLabel(patch.label) || room.label;
    const type = patch.type === undefined ? room.type : roomTypes.has(patch.type) ? patch.type : undefined;
    const points = patch.points === undefined ? room.points : patch.points.filter(isPointLike).map(normalizePoint);
    const nextPoints = points.length >= 3 ? points : room.points;
    if (patch.points !== undefined) {
      const before = pointsCenter(room.points);
      const after = pointsCenter(nextPoints);
      delta = { x: after.x - before.x, y: after.y - before.y };
    }
    return { ...room, label, points: nextPoints, ...(type ? { type } : { type: undefined }) };
  });

  if (patch.devices) {
    const devices = { ...floor.devices };
    for (const [serial, placement] of Object.entries(patch.devices)) {
      if (!floor.devices[serial] || floor.devices[serial].roomId !== roomId) continue;
      devices[serial] = normalizePlacement(placement);
    }
    return { rooms, devices };
  }

  if (!delta || (delta.x === 0 && delta.y === 0)) return { rooms, devices: floor.devices };
  const moveDelta = delta;

  const devices = Object.fromEntries(
    Object.entries(floor.devices).map(([serial, placement]) => {
      if (placement.roomId !== roomId) return [serial, placement];
      return [serial, normalizePlacement({ ...placement, x: placement.x + moveDelta.x, y: placement.y + moveDelta.y })];
    }),
  );
  return { rooms, devices };
}

export function removeRoomFromFloor(preferences: FloorPlanPreferences, locationId: string, floorId: string, roomId: string): FloorPlanPreferences {
  const cleanRoomId = cleanId(roomId);
  if (!cleanRoomId) return normalizeFloorPlanPreferences(preferences);

  return updateFloor(preferences, locationId, floorId, (floor) => {
    const devices = { ...floor.devices };
    for (const [serial, placement] of Object.entries(floor.devices)) {
      if (placement.roomId === cleanRoomId) delete devices[serial];
    }
    return {
      ...floor,
      rooms: floor.rooms.filter((room) => room.id !== cleanRoomId),
      devices,
    };
  });
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

export function roomAtPoint(rooms: FloorPlanRoom[], point: FloorPlanPoint): FloorPlanRoom | undefined {
  for (let index = rooms.length - 1; index >= 0; index -= 1) {
    const room = rooms[index];
    if (pointInRoom(point, room)) return room;
  }
  return undefined;
}

export function pointInRoom(point: FloorPlanPoint, room: FloorPlanRoom): boolean {
  return pointInPolygon(point, room.points);
}

export function roomCenter(room: FloorPlanRoom): FloorPlanPoint {
  return pointsCenter(room.points);
}

export function roomInteriorPoint(room: FloorPlanRoom, ideal: FloorPlanPoint): FloorPlanPoint {
  if (pointInRoom(ideal, room)) return ideal;
  const bounds = roomBounds(room.points);
  const width = bounds.right - bounds.left;
  const height = bounds.bottom - bounds.top;
  if (width <= 0 || height <= 0) return roomCenter(room);

  const insetX = Math.min(width / 2, Math.max(0.01, width * 0.08));
  const insetY = Math.min(height / 2, Math.max(0.01, height * 0.08));
  const candidates: FloorPlanPoint[] = [];
  const steps = 8;
  for (let yIndex = 0; yIndex <= steps; yIndex += 1) {
    for (let xIndex = 0; xIndex <= steps; xIndex += 1) {
      candidates.push({
        x: bounds.left + insetX + (Math.max(0, width - insetX * 2) * xIndex) / steps,
        y: bounds.top + insetY + (Math.max(0, height - insetY * 2) * yIndex) / steps,
      });
    }
  }
  return candidates
    .filter((candidate) => pointInRoom(candidate, room))
    .sort((a, b) => squaredDistance(a, ideal) - squaredDistance(b, ideal))[0] ?? roomCenter(room);
}

export function keepRoomDevicesInsideShape(room: FloorPlanRoom, devices: Record<string, FloorPlanDevicePlacement>): Record<string, FloorPlanDevicePlacement> {
  return Object.fromEntries(
    Object.entries(devices).map(([serial, placement]) => {
      const point = { x: placement.x, y: placement.y };
      if (pointInRoom(point, room)) return [serial, placement];
      const next = roomInteriorPoint(room, point);
      return [serial, { ...placement, x: next.x, y: next.y, roomId: room.id }];
    }),
  );
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

function pointsCenter(points: FloorPlanPoint[]): FloorPlanPoint {
  return {
    x: points.reduce((sum, point) => sum + point.x, 0) / points.length,
    y: points.reduce((sum, point) => sum + point.y, 0) / points.length,
  };
}

function roomBounds(points: FloorPlanPoint[]) {
  return points.reduce(
    (bounds, point) => ({
      left: Math.min(bounds.left, point.x),
      right: Math.max(bounds.right, point.x),
      top: Math.min(bounds.top, point.y),
      bottom: Math.max(bounds.bottom, point.y),
    }),
    { left: 1, right: 0, top: 1, bottom: 0 },
  );
}

function squaredDistance(a: FloorPlanPoint, b: FloorPlanPoint): number {
  return (a.x - b.x) ** 2 + (a.y - b.y) ** 2;
}

function pointInPolygon(point: FloorPlanPoint, polygon: FloorPlanPoint[]): boolean {
  let inside = false;
  for (let current = 0, previous = polygon.length - 1; current < polygon.length; previous = current, current += 1) {
    const currentPoint = polygon[current];
    const previousPoint = polygon[previous];
    const crosses = currentPoint.y > point.y !== previousPoint.y > point.y;
    if (!crosses) continue;
    const x = ((previousPoint.x - currentPoint.x) * (point.y - currentPoint.y)) / (previousPoint.y - currentPoint.y) + currentPoint.x;
    if (point.x < x) inside = !inside;
  }
  return inside;
}

function updateFloor(
  preferences: FloorPlanPreferences,
  locationId: string,
  floorId: string,
  updater: (floor: FloorPlanFloor) => FloorPlanFloor,
): FloorPlanPreferences {
  const normalized = ensureLocationFloorPlan(preferences, locationId);
  const cleanLocationId = cleanId(locationId);
  const cleanFloorId = cleanId(floorId);
  const location = normalized.locations[cleanLocationId];
  if (!location) return normalized;

  const floors = location.floors.map((floor) => (floor.id === cleanFloorId ? normalizeFloor(updater(floor)) ?? floor : floor));
  const activeFloorId = floors.some((floor) => floor.id === cleanFloorId) ? cleanFloorId : location.activeFloorId;

  return {
    ...normalized,
    locations: {
      ...normalized.locations,
      [cleanLocationId]: {
        ...location,
        activeFloorId,
        floors,
      },
    },
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
