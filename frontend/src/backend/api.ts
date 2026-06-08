import { DeviceKind, type Device, type DeviceSnapshot, type HslColor, type Matrix } from '../domain/lifx';
import type { DeviceCommandIntent } from '../domain/commands';

interface WailsApp {
  GetDeviceSnapshot?: () => Promise<DeviceSnapshot>;
  SetDeviceState?: (request: SetDeviceStateRequest) => Promise<Device>;
}

interface SetDeviceStateRequest {
  device: Device;
  preview: boolean;
  intent: DeviceCommandIntent;
}

declare global {
  interface Window {
    go?: {
      main?: {
        App?: WailsApp;
      };
    };
  }
}

export async function getDeviceSnapshot(): Promise<DeviceSnapshot> {
  const app = window.go?.main?.App;
  if (app?.GetDeviceSnapshot) return normalizeSnapshot(await app.GetDeviceSnapshot());
  return mockSnapshot();
}

export async function setDeviceState(device: Device, preview = false, intent: DeviceCommandIntent = 'color'): Promise<Device> {
  const app = window.go?.main?.App;
  if (app?.SetDeviceState) return app.SetDeviceState({ device, preview, intent });
  await new Promise((resolve) => window.setTimeout(resolve, preview ? 60 : 180));
  return device;
}

function mockSnapshot(): DeviceSnapshot {
  return {
    locations: [
      { id: 'home', name: 'Home' },
      { id: 'studio', name: 'Studio' },
    ],
    groups: [
      { id: 'living', locationId: 'home', name: 'Living Room' },
      { id: 'kitchen', locationId: 'home', name: 'Kitchen' },
      { id: 'desk', locationId: 'studio', name: 'Desk' },
    ],
    devices: [
      single('living', 'Ceiling', 'A19 color', 'd0:73:d5:01:a2:c3', 0.62, { h: 38, s: 0.35, l: 0.65 }, 3200),
      single('living', 'Sofa Lamp', 'BR30 color', 'd0:73:d5:01:a2:d8', 0.48, { h: 18, s: 0.85, l: 0.55 }, 2700),
      {
        groupId: 'living',
        serial: 'd0:73:d5:01:a2:e1',
        name: 'TV Backlight',
        model: 'Z 32',
        kind: DeviceKind.Multizone,
        online: true,
        on: true,
        brightness: 0.78,
        capability: colorCapability(),
        zones: makeZones(32, 290, 70),
      },
      {
        groupId: 'living',
        serial: 'd0:73:d5:01:a2:e4',
        name: 'Wall Tiles',
        model: 'Tile 5',
        kind: DeviceKind.Matrix,
        online: true,
        on: true,
        brightness: 0.55,
        capability: colorCapability(),
        chain: makeMatrixChain(5, 170, 290),
      },
      single('kitchen', 'Pendant', 'A19 color', 'd0:73:d5:02:b1:01', 0.9, { h: 38, s: 0.2, l: 0.85 }, 4500),
      {
        groupId: 'kitchen',
        serial: 'd0:73:d5:02:b1:10',
        name: 'Under-counter',
        model: 'Z 24',
        kind: DeviceKind.Multizone,
        online: true,
        on: false,
        brightness: 0.55,
        capability: colorCapability(),
        zones: makeZones(24, 30, 60),
      },
      {
        groupId: 'desk',
        serial: 'd0:73:d5:10:f5:01',
        name: 'Desk Strip',
        model: 'Z 32',
        kind: DeviceKind.Multizone,
        online: true,
        on: true,
        brightness: 0.85,
        capability: colorCapability(),
        zones: makeZones(32, 200, 260),
      },
    ],
  };
}

function normalizeSnapshot(snapshot: DeviceSnapshot | null | undefined): DeviceSnapshot {
  return {
    locations: Array.isArray(snapshot?.locations) ? snapshot.locations : [],
    groups: Array.isArray(snapshot?.groups) ? snapshot.groups : [],
    devices: Array.isArray(snapshot?.devices) ? snapshot.devices : [],
  };
}

function single(groupId: string, name: string, model: string, serial: string, brightness: number, color: HslColor, kelvin: number): Device {
  return { groupId, serial, name, model, kind: DeviceKind.Single, online: true, on: brightness > 0, brightness, capability: colorCapability(), color, kelvin };
}

function colorCapability() {
  return { hasColor: true, kelvinMin: 1500, kelvinMax: 9000 };
}

function makeZones(count: number, start: number, end: number): HslColor[] {
  return Array.from({ length: count }, (_, index) => {
    const t = index / Math.max(1, count - 1);
    return { h: start + (end - start) * t, s: 0.85, l: 0.55 };
  });
}

function makeMatrixChain(count: number, start: number, end: number): Matrix[] {
  const positions = [
    [0, 0],
    [8, 0],
    [16, 0],
    [4, 8],
    [12, 8],
  ];
  return Array.from({ length: count }, (_, matrixIndex) => {
    const [x, y] = positions[matrixIndex] ?? [matrixIndex * 8, 0];
    const rows = Array.from({ length: 8 }, () => ({ cols: 8, offset: 0 }));
    const pixels = Array.from({ length: 64 }, (_, pixelIndex) => {
      const t = (matrixIndex * 64 + pixelIndex) / Math.max(1, count * 64 - 1);
      return { h: start + (end - start) * t, s: 0.75, l: 0.5 };
    });
    return { id: matrixIndex, x, y, w: 8, h: 8, rows, pixels };
  });
}
