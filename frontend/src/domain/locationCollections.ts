import type { Device, Group, Location } from './lifx.js';

export interface LocationCollection {
  key: string;
  name: string;
  locationIds: string[];
}

export function collectLocations(locations: Location[]): LocationCollection[] {
  const collections = new Map<string, { names: string[]; ids: string[] }>();
  for (const location of locations) {
    const normalizedName = normalizeLocationName(location.name);
    const existing = collections.get(normalizedName) ?? { names: [], ids: [] };
    existing.names.push(location.name.trim() || 'Unknown');
    existing.ids.push(location.id);
    collections.set(normalizedName, existing);
  }

  return [...collections.entries()]
    .map(([normalizedName, collection]) => ({
      key: `location:${encodeURIComponent(normalizedName)}`,
      name: [...collection.names].sort(compareText)[0],
      locationIds: [...new Set(collection.ids)].sort(compareText),
    }))
    .sort((left, right) => compareText(left.name, right.name) || compareText(left.key, right.key));
}

export function locationCollectionForID(collections: LocationCollection[], locationId: string): LocationCollection | undefined {
  return collections.find((collection) => collection.locationIds.includes(locationId));
}

export function locationCollectionByKey(collections: LocationCollection[], key: string): LocationCollection | undefined {
  return collections.find((collection) => collection.key === key);
}

export function groupsInLocationCollection(collection: LocationCollection | undefined, groups: Group[]): Group[] {
  const locationIds = new Set(collection?.locationIds ?? []);
  return groups.filter((group) => locationIds.has(group.locationId));
}

export function devicesInLocationCollection(collection: LocationCollection | undefined, groups: Group[], devices: Device[]): Device[] {
  const groupIds = new Set(groupsInLocationCollection(collection, groups).map((group) => group.id));
  return devices.filter((device) => groupIds.has(device.groupId));
}

function normalizeLocationName(name: string): string {
  return (name.trim().replace(/\s+/g, ' ') || 'Unknown').toLowerCase();
}

function compareText(left: string, right: string): number {
  return left.localeCompare(right, undefined, { sensitivity: 'base' });
}
