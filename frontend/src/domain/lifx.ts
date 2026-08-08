export const DeviceKind = {
  Single: 'single',
  Multizone: 'multizone',
  Matrix: 'matrix',
  Switch: 'switch',
} as const;

export type DeviceKind = (typeof DeviceKind)[keyof typeof DeviceKind];

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
  hiddenCols?: number[];
}

export interface Matrix {
  id: number;
  x: number;
  y: number;
  w: number;
  h: number;
  sendWidth?: number;
  orientation?: number;
  rows: MatrixRow[];
  pixels: HslColor[];
}

export interface Relay {
  index: number;
  on: boolean;
}

export interface ButtonConfig {
  known: boolean;
  hapticDurationMs: number;
  backlightOnColor: HslColor;
  backlightOffColor: HslColor;
}

export interface Device {
  groupId: string;
  serial: string;
  name: string;
  model: string;
  kind: DeviceKind;
  ipAddress?: string;
  productId?: number;
  firmware?: string;
  rssi?: number;
  rssiText?: string;
  zoneCount?: number;
  pixelCount?: number;
  chainLength?: number;
  online: boolean;
  on: boolean;
  brightness: number;
  capability: DeviceCapability;
  color?: HslColor;
  kelvin?: number;
  zones?: HslColor[];
  chain?: Matrix[];
  relays?: Relay[];
  buttonConfig?: ButtonConfig;
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
  const [red, green, blue] = kelvinRgb(kelvin);
  const displayLightness = Math.max(0, Math.min(1, lightness));
  const scale = 0.42 + displayLightness * 0.82;
  return `rgb(${clampRgb(red * scale)} ${clampRgb(green * scale)} ${clampRgb(blue * scale)})`;
}

function kelvinRgb(kelvin: number): [number, number, number] {
  const temperature = Math.max(10, Math.min(400, kelvin / 100));
  let red: number;
  let green: number;
  let blue: number;

  if (temperature <= 66) {
    red = 255;
    green = 99.4708025861 * Math.log(temperature) - 161.1195681661;
    blue = temperature <= 19 ? 0 : 138.5177312231 * Math.log(temperature - 10) - 305.0447927307;
  } else {
    red = 329.698727446 * Math.pow(temperature - 60, -0.1332047592);
    green = 288.1221695283 * Math.pow(temperature - 60, -0.0755148492);
    blue = 255;
  }

  // LIFX white rendering should read as warm/cool white, not saturated orange/blue.
  return [softenWhite(red), softenWhite(green), softenWhite(blue)];
}

function softenWhite(channel: number): number {
  return clampRgb(channel * 0.62 + 255 * 0.38);
}

function clampRgb(value: number): number {
  return Math.round(Math.max(0, Math.min(255, value)));
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

export function isLightDevice(device: Device): boolean {
  return device.kind !== DeviceKind.Switch;
}

export function deviceColor(device: Device): HslColor {
  if (device.kind === DeviceKind.Single && device.color) return device.color;
  if (device.kind === DeviceKind.Multizone && device.zones?.length) return device.zones[Math.floor(device.zones.length / 2)];
  if (device.kind === DeviceKind.Matrix && device.chain?.length) {
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
  if (device.kind === DeviceKind.Single) return "single zone";
  if (device.kind === DeviceKind.Multizone) return "multizone · " + (device.zones?.length ?? 0) + " zones";
  if (device.kind === DeviceKind.Switch) return "switch";
  const pixels = device.chain?.reduce((sum, matrix) => sum + matrix.pixels.length, 0) ?? 0;
  return "matrix chain · " + (device.chain?.length ?? 0) + " matrices · " + pixels + "px";
}
