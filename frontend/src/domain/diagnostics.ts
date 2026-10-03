import type { Device } from './lifx.js';

export function deviceUptime(device: Device, nowMs = Date.now()): string {
  if (!device.online || device.estimatedBootedAtMs === undefined || !Number.isFinite(device.estimatedBootedAtMs)) return 'unknown';
  const minutes = Math.floor(Math.max(0, nowMs - device.estimatedBootedAtMs) / 60000);
  if (minutes < 1) return '<1m';
  if (minutes < 60) return `${minutes}m`;
  const hours = Math.floor(minutes / 60);
  if (hours < 24) return `${hours}h ${minutes % 60}m`;
  return `${Math.floor(hours / 24)}d ${hours % 24}h`;
}
