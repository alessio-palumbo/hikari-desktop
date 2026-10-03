import type { Device, DeviceSnapshot } from './lifx.js';

export type HealthSort = 'name' | 'signal' | 'lastSeen' | 'uptime';

export function healthDevices(snapshot: DeviceSnapshot, query: string, sort: HealthSort, descending: boolean): Device[] {
  const groups = new Map(snapshot.groups.map((group) => [group.id, group]));
  const locations = new Map(snapshot.locations.map((location) => [location.id, location.name]));
  const needle = query.trim().toLowerCase();
  return snapshot.devices.filter((device) => {
    const group = groups.get(device.groupId);
    return [device.name, device.serial, device.model, device.ipAddress, group?.name, locations.get(group?.locationId ?? '')]
      .some((value) => value?.toLowerCase().includes(needle));
  }).sort((a, b) => {
    let comparison = 0;
    switch (sort) {
      case 'name': comparison = a.name.localeCompare(b.name); break;
      // RSSI and SNR use different scales; rank by the library's signal band.
      case 'signal': comparison = signalRank(a) - signalRank(b); break;
      case 'lastSeen': comparison = (a.lastSeenAtMs ?? Infinity) - (b.lastSeenAtMs ?? Infinity); break;
      case 'uptime': comparison = (a.estimatedBootedAtMs ?? -Infinity) - (b.estimatedBootedAtMs ?? -Infinity); break;
    }
    return (descending ? -comparison : comparison) || a.name.localeCompare(b.name) || a.serial.localeCompare(b.serial);
  });
}

function signalRank(device: Device): number {
  if (!device.online || device.rssi === undefined || device.rssi === 0) return -1;
  return ['Very Poor', 'Poor', 'Fair', 'Good', 'Excellent'].indexOf(device.rssiText ?? '');
}

export function signalQuality(device: Device): 'good' | 'fair' | 'poor' | 'unknown' {
  const rank = signalRank(device);
  if (rank < 0) return 'unknown';
  if (rank >= 3) return 'good';
  return rank === 2 ? 'fair' : 'poor';
}

export function deviceSignal(device: Device): string {
  if (!device.online || device.rssi === undefined || device.rssi === 0) return 'unknown';
  const unit = device.rssi < 0 ? 'dBm' : 'SNR';
  return `${device.rssi} ${unit}`;
}

export function lastResponseAge(device: Device, nowMs = Date.now()): string {
  if (device.lastSeenAtMs === undefined || !Number.isFinite(device.lastSeenAtMs)) return 'unknown';
  const seconds = Math.floor(Math.max(0, nowMs - device.lastSeenAtMs) / 1000);
  if (seconds < 1) return '<1s';
  if (seconds < 60) return `${seconds}s`;
  if (seconds < 3600) return `${Math.floor(seconds / 60)}m`;
  if (seconds < 86400) return `${Math.floor(seconds / 3600)}h`;
  return `${Math.floor(seconds / 86400)}d`;
}

export function deviceUptime(device: Device, nowMs = Date.now()): string {
  if (!device.online || device.estimatedBootedAtMs === undefined || !Number.isFinite(device.estimatedBootedAtMs)) return 'unknown';
  const minutes = Math.floor(Math.max(0, nowMs - device.estimatedBootedAtMs) / 60000);
  if (minutes < 1) return '<1m';
  if (minutes < 60) return `${minutes}m`;
  const hours = Math.floor(minutes / 60);
  if (hours < 24) return `${hours}h ${minutes % 60}m`;
  return `${Math.floor(hours / 24)}d ${hours % 24}h`;
}
