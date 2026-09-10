import type { SensorNode } from '../backend/api.js';

export function presenceReading(sensor: SensorNode): string {
  if (!sensor.online || !sensor.presenceKnown) return 'unknown';
  if (!sensor.present) return 'clear';
  if (!sensor.capabilities.includes('target_count') || !sensor.targetCount?.known) return 'occupied';

  const count = Math.max(0, sensor.targetCount.value);
  if (count === 0) return 'occupied';
  const saturated = typeof sensor.targetCount.max === 'number' && sensor.targetCount.max > 0 && count >= sensor.targetCount.max;
  return `occupied (${count}${saturated ? '+' : ''})`;
}

export function sensorSignalReading(sensor: SensorNode): string {
  return sensor.online && typeof sensor.rssiDbm === 'number'
    ? `${Math.round(sensor.rssiDbm)} dBm`
    : 'unknown';
}
