import type { Device, HslColor } from './lifx';

export type DeviceCommandIntent = 'power' | 'brightness' | 'color' | 'zones' | 'matrix';

export function prepareDeviceCommand(next: Device, previous?: Device): Device {
  if (!previous || near(previous.brightness, next.brightness) || next.kind === 'single') return next;
  return applyBrightnessToZones(next, next.brightness);
}

export function commandIntent(next: Device, previous?: Device): DeviceCommandIntent {
  if (previous && next.on !== previous.on && near(next.brightness, previous.brightness) && sameStatePayload(next, previous)) return 'power';
  if (previous && !near(next.brightness, previous.brightness) && sameStatePayload(next, previous)) return 'brightness';
  return 'color';
}

export function draftIntent(device: Device): DeviceCommandIntent {
  if (device.kind === 'multizone') return 'zones';
  if (device.kind === 'matrix') return 'matrix';
  return 'color';
}

function applyBrightnessToZones(device: Device, brightness: number): Device {
  const withBrightness = (color: HslColor): HslColor => ({ ...color, l: brightness });
  if (device.kind === 'multizone') {
    return { ...device, zones: device.zones?.map(withBrightness) ?? [] };
  }
  if (device.kind === 'matrix') {
    return { ...device, chain: device.chain?.map((matrix) => ({ ...matrix, pixels: matrix.pixels.map(withBrightness) })) ?? [] };
  }
  return device;
}

function sameStatePayload(next: Device, previous: Device): boolean {
  return sameValue(next.color, previous.color) && next.kelvin === previous.kelvin && sameValue(next.zones, previous.zones) && sameValue(next.chain, previous.chain);
}

function sameValue(a: unknown, b: unknown): boolean {
  return JSON.stringify(a ?? null) === JSON.stringify(b ?? null);
}

function near(a: number, b: number) {
  return Math.abs(a - b) < 0.01;
}
