import { DeviceKind, isLightDevice, type Device } from './lifx.js';

export type InspectorMode = 'color' | 'white' | 'effects';

export function compatibleInspectorMode(mode: InspectorMode, device: Device): InspectorMode {
  // Switches have no light tabs, so visiting one should not change the light-tab preference.
  if (!isLightDevice(device)) return mode;
  const hasColor = device.capability?.hasColor ?? true;
  if (mode === 'white' || (mode === 'color' && hasColor)) return mode;
  if (mode === 'effects' && device.kind !== DeviceKind.Single) return mode;
  return hasColor ? 'color' : 'white';
}
