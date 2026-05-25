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
  kelvin?: number;
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
  groupId: string;
  serial: string;
  name: string;
  model: string;
  kind: DeviceKind;
  online: boolean;
  on: boolean;
  brightness: number;
  capability: DeviceCapability;
  color?: HslColor;
  kelvin?: number;
  zones?: HslColor[];
  chain?: Matrix[];
}

export interface DeviceCapability {
  hasColor: boolean;
  kelvinMin: number;
  kelvinMax: number;
}

export interface DeviceSnapshot {
  locations: Location[];
  groups: Group[];
  devices: Device[];
}

export function hsl(color: HslColor, lightness?: number): string {
  if (color.kelvin && color.s === 0) return kelvinCss(color.kelvin, lightness ?? color.l);
  return `hsl(${color.h} ${Math.round(color.s * 100)}% ${Math.round((lightness ?? color.l) * 100)}%)`;
}

export function kelvinCss(kelvin: number, lightness = 0.72): string {
  const t = Math.max(0, Math.min(1, (kelvin - 1500) / 7500));
  const hue = 28 + (210 - 28) * t;
  const saturation = 0.72 - 0.38 * t;
  return `hsl(${hue} ${Math.round(saturation * 100)}% ${Math.round(lightness * 100)}%)`;
}

export function previewLightness(color: HslColor, brightness: number, on = true): number {
  const baseLightness = previewBaseLightness(color);
  if (!on) return Math.max(0.2, baseLightness * 0.45);
  const scaled = 0.32 + Math.sqrt(Math.max(0, Math.min(1, brightness))) * 0.74;
  return Math.max(0.28, Math.min(0.84, baseLightness * scaled));
}

export function previewOpacity(on = true): number {
  if (!on) return 0.3;
  return 1;
}

function previewBaseLightness(color: HslColor): number {
  if (color.kelvin && color.s === 0) return 0.72;
  if (color.s < 0.05) return 0.68;
  return 0.58;
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
