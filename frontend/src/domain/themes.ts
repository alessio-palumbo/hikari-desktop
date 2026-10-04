import { isLightDevice, type Device, type HslColor } from './lifx.js';

export interface ThemeColor { Hue: number; Saturation: number; Brightness: number; Kelvin: number }
export interface Theme {
  name: string;
  palette: { Name?: string; Base: ThemeColor[]; Accents?: ThemeColor[]; Backgrounds?: ThemeColor[] };
  layout?: 'gradient' | 'steps' | 'solid';
  axis?: 'horizontal' | 'vertical';
}
export interface UserTheme { id: string; theme: Theme }
export interface SaveUserThemeRequest { id?: string; theme: Theme }
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

export function themeColors(theme: Theme): ThemeColor[] {
  return [...(theme.palette.Base ?? []), ...(theme.palette.Accents ?? []), ...(theme.palette.Backgrounds ?? [])];
}

export function themeEditorDraft(theme?: Theme, id?: string): SaveUserThemeRequest {
  const source = theme ?? { name: 'New theme', palette: { Base: [color(18), color(210)] } };
  return {
    ...(id ? { id } : {}),
    theme: {
      name: theme && !id ? `${source.name.slice(0, 123)} copy` : source.name,
      palette: { Base: themeColors(source).map((stop) => ({ ...stop })) },
      layout: source.layout ?? 'gradient',
      axis: source.axis ?? 'horizontal',
    },
  };
}

export function moveThemeColor(theme: Theme, index: number, destination: number): Theme {
  const colors = themeColors(theme);
  if (index < 0 || index >= colors.length || destination < 0 || destination >= colors.length) return theme;
  const [stop] = colors.splice(index, 1);
  colors.splice(destination, 0, stop);
  return { ...theme, palette: { Base: colors } };
}

export function sortedUserThemes(themes: UserTheme[]): UserTheme[] {
  return [...themes].sort((a, b) => {
    const left = a.theme.name.toLowerCase();
    const right = b.theme.name.toLowerCase();
    return left < right ? -1 : left > right ? 1 : a.id < b.id ? -1 : a.id > b.id ? 1 : 0;
  });
}

export function themeSelectionAction(previewOpen: boolean): 'preview' | 'apply' {
  return previewOpen ? 'preview' : 'apply';
}
