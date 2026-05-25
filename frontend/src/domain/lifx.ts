export type DeviceKind = 'single' | 'multizone' | 'matrix';

export interface Location {
  id: string;
  name: string;
}

export interface Group {
  id: string;
  locationId: string;
  name: string;
}

export interface HslColor {
  h: number;
  s: number;
  l: number;
}

export interface MatrixRow {
  cols: number;
  offset: number;
}

export interface Matrix {
  id: number;
  x: number;
  y: number;
  w: number;
  h: number;
  rows: MatrixRow[];
  pixels: HslColor[];
}

export interface Device {
  id: string;
  groupId: string;
  serial: string;
  name: string;
  model: string;
  kind: DeviceKind;
  online: boolean;
  on: boolean;
  brightness: number;
  color?: HslColor;
  kelvin?: number;
  zones?: HslColor[];
  chain?: Matrix[];
}

export interface DeviceSnapshot {
  locations: Location[];
  groups: Group[];
  devices: Device[];
}

export function hsl(color: HslColor, lightness?: number): string {
  return `hsl(${color.h} ${Math.round(color.s * 100)}% ${Math.round((lightness ?? color.l) * 100)}%)`;
}

export function deviceColor(device: Device): HslColor {
  if (device.kind === 'single' && device.color) return device.color;
  if (device.kind === 'multizone' && device.zones?.length) return device.zones[Math.floor(device.zones.length / 2)];
  if (device.kind === 'matrix' && device.chain?.length) {
    const pixels = device.chain.flatMap((matrix) => matrix.pixels);
    const sum = pixels.reduce(
      (acc, color) => ({ h: acc.h + color.h, s: acc.s + color.s, l: acc.l + color.l }),
      { h: 0, s: 0, l: 0 },
    );
    return { h: sum.h / pixels.length, s: sum.s / pixels.length, l: sum.l / pixels.length };
  }
  return { h: 38, s: 0.1, l: 0.7 };
}

export function deviceTypeLabel(device: Device): string {
  if (device.kind === 'single') return 'single zone';
  if (device.kind === 'multizone') return `multizone · ${device.zones?.length ?? 0} zones`;
  const pixels = device.chain?.reduce((sum, matrix) => sum + matrix.pixels.length, 0) ?? 0;
  return `matrix chain · ${device.chain?.length ?? 0} matrices · ${pixels}px`;
}
