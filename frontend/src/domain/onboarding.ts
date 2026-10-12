import type { Device, Group, Location } from './lifx.js';

export interface SetupLight { serial: string; ssid: string; rssi: number }
export interface SetupLightDiscovery { lights: SetupLight[]; currentSsid: string; networks: string[] }
export interface SetupLightRequest { attemptId: string; bulbSsid: string; ssid: string; password: string; openNetwork: boolean }
export interface SetupProgress { attemptId: string; phase: string }
export interface SetupLightResult { serial: string; configured: boolean; verified: boolean; cancelled: boolean; error?: string; warning?: string }

export function setupPhaseLabel(phase: string): string {
  return ({ prepare: 'Preparing setup', join: 'Joining the device', wait: 'Waiting for the device', dial: 'Connecting to the device', configure: 'Wi-Fi details sent', rejoin: 'Reconnecting Wi-Fi', verify: 'Waiting for LAN discovery' } as Record<string, string>)[phase] ?? 'Setting up device';
}

export function canSubmitSetup(request: SetupLightRequest): boolean {
  const length = (value: string) => new TextEncoder().encode(value).length;
  return !!request.bulbSsid && !!request.ssid.trim() && length(request.ssid) <= 32 && request.ssid !== request.bulbSsid
    && (request.openNetwork ? !request.password : length(request.password) >= 8 && length(request.password) <= 64);
}
// Defaults are a draft only: nothing is assigned until the user presses Save.
export function setupMetadataDefaults(device: Device, setupSSID: string, locations: Location[], groups: Group[]): { label: string; locationId: string; groupId: string } {
  const meaningful = (name: string) => !!name.trim() && name.trim().toLowerCase() !== 'unknown';
  const currentGroup = groups.find((group) => group.id === device.groupId);
  const currentLocation = locations.find((location) => location.id === currentGroup?.locationId);
  const location = currentLocation && meaningful(currentLocation.name) ? currentLocation : locations.find((entry) => meaningful(entry.name)) ?? locations[0];
  const availableGroups = groups.filter((group) => group.locationId === location?.id);
  const group = currentGroup && currentGroup.locationId === location?.id && meaningful(currentGroup.name) ? currentGroup : availableGroups.find((entry) => meaningful(entry.name)) ?? availableGroups[0];
  const name = device.name.trim();
  return { label: meaningful(name) && name.toLowerCase() !== device.serial.toLowerCase() ? device.name : setupSSID || device.name, locationId: location?.id ?? '', groupId: group?.id ?? '' };
}
