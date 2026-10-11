import { isLightDevice, type Device, type DeviceSnapshot, type HslColor } from './lifx.js';

export interface SceneTarget { serial: string; label: string; on?: boolean; brightness?: number; colors?: HslColor[] }
export interface SceneSummary { id: string; name: string; savedAtMs: number; targets: SceneTarget[] }
export interface CaptureSceneRequest { id?: string; name: string; serials: string[] }
export interface SceneRecallResult {
  devices: Device[];
  failures: { serial: string; error: string; stateMayHaveChanged: boolean }[];
}
export interface SceneCaptureScope { id: string; name: string; kind: 'all' | 'group' | 'room' | 'device'; serials: string[] }

export function addSceneMembers(current: string[], added: string[]): string[] {
  return [...new Set([...current, ...added])];
}

export function sceneCaptureScopes(snapshot: DeviceSnapshot, rooms: SceneCaptureScope[] = []): SceneCaptureScope[] {
  const lights = snapshot.devices.filter((device) => device.online && isLightDevice(device));
  const serials = (devices: Device[]) => [...new Set(devices.map((device) => device.serial))].sort();
  const available = new Set(serials(lights));
  const scopes: SceneCaptureScope[] = [{ id: 'all', name: 'LAN lights', kind: 'all', serials: serials(lights) }];
  const groups = snapshot.groups.map((group) => ({
    id: `group:${group.id}`, name: `${snapshot.locations.find((location) => location.id === group.locationId)?.name ?? ''} / ${group.name}`.replace(/^ \/ /, ''),
    kind: 'group' as const, serials: serials(lights.filter((device) => device.groupId === group.id)),
  })).filter((group) => group.serials.length > 0).sort((a, b) => a.name.localeCompare(b.name) || a.id.localeCompare(b.id));
  scopes.push(...groups);
  scopes.push(...rooms.map((room) => ({ ...room, serials: [...new Set(room.serials)].filter((serial) => available.has(serial)).sort() })).filter((room) => room.serials.length > 0));
  scopes.push(...lights.map((device) => ({ id: `device:${device.serial}`, name: device.name, kind: 'device' as const, serials: [device.serial] })).sort((a, b) => a.name.localeCompare(b.name) || a.id.localeCompare(b.id)));
  return scopes;
}

export function sceneMembers(scene: SceneSummary, devices: Device[]): (SceneTarget & { available: boolean })[] {
  const current = new Map(devices.map((device) => [device.serial, device]));
  return scene.targets.map((target) => {
    const device = current.get(target.serial);
    return { ...target, label: device?.name ?? target.label, available: !!device?.online && isLightDevice(device) };
  });
}

export function visibleScenes(scenes: SceneSummary[], devices: Device[], query: string): SceneSummary[] {
  const q = query.trim().toLowerCase();
  return scenes.filter((scene) => !q || [scene.name, ...sceneMembers(scene, devices).flatMap((target) => [target.label, target.serial])].some((value) => value.toLowerCase().includes(q)))
    .sort((a, b) => a.name.toLowerCase().localeCompare(b.name.toLowerCase()) || a.id.localeCompare(b.id));
}
