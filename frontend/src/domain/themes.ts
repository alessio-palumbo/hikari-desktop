import { isLightDevice, type Device, type HslColor } from './lifx.js';

export interface ThemeColor { Hue: number; Saturation: number; Brightness: number; Kelvin: number }
export interface Theme {
  name: string;
  palette: { Base: ThemeColor[] };
  layout: 'gradient' | 'steps' | 'solid';
  axis: 'horizontal' | 'vertical';
}
export interface ThemeRequest { theme: Theme; serials: string[]; brightness: 'preserve' }
export interface ThemeDevicePreview {
  serial: string; name: string; on: boolean; width: number; height: number;
  cells: boolean[]; colors: HslColor[];
}
export interface ThemePreview { devices: ThemeDevicePreview[] }
export interface ThemeApplyResult {
  devices: Device[];
  failures: { serial: string; error: string; stateMayHaveChanged: boolean }[];
}

const color = (Hue: number, Saturation = 85, Kelvin = 3500): ThemeColor => ({ Hue, Saturation, Brightness: 100, Kelvin });
export const themes: Theme[] = [
  { name: 'Sunset', palette: { Base: [color(18), color(340), color(275)] }, layout: 'gradient', axis: 'horizontal' },
  { name: 'Ocean', palette: { Base: [color(175), color(210), color(245)] }, layout: 'gradient', axis: 'horizontal' },
  { name: 'Aurora', palette: { Base: [color(150), color(195), color(285)] }, layout: 'gradient', axis: 'horizontal' },
  { name: 'Warm whites', palette: { Base: [color(0, 0, 2000), color(0, 0, 2700), color(0, 0, 3500)] }, layout: 'gradient', axis: 'horizontal' },
];

export function themeRequest(theme: Theme, devices: Device[]): ThemeRequest {
  return { theme, serials: [...new Set(devices.filter((device) => device.online && isLightDevice(device)).map((device) => device.serial))].sort(), brightness: 'preserve' };
}
