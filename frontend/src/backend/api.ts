import { DeviceKind, type Device, type DeviceSnapshot, type HslColor, type Matrix } from '../domain/lifx';
import type { DeviceCommandIntent } from '../domain/commands';
import type { DeviceEffect } from '../domain/effects';

interface WailsApp {
  GetDeviceSnapshot?: () => Promise<DeviceSnapshot>;
  NetworkSettings?: () => Promise<NetworkSettings>;
  SetNetworkInterface?: (request: SetNetworkInterfaceRequest) => Promise<NetworkSettings>;
  RestartDeviceDiscovery?: () => Promise<NetworkSettings>;
  SetDeviceState?: (request: SetDeviceStateRequest) => Promise<Device>;
  StartDeviceEffect?: (request: StartDeviceEffectRequest) => Promise<DeviceEffectStatus>;
  StopDeviceEffect?: (request: StopDeviceEffectRequest) => Promise<DeviceEffectStatus>;
  CommandEngineSettings?: () => Promise<CommandEngineSettings>;
  SetCommandEngineSettings?: (request: SetCommandEngineSettingsRequest) => Promise<CommandEngineSettings>;
  InterpretCommand?: (request: InterpretCommandRequest) => Promise<CommandPreview>;
}

export interface NetworkInterfaceOption {
  name: string;
  address: string;
  broadcast: string;
  label: string;
  shortLabel?: string;
}

export interface NetworkSettings {
  selectedInterfaceName: string;
  interfaces: NetworkInterfaceOption[];
  warning?: string;
}

interface SetNetworkInterfaceRequest {
  interfaceName: string;
}

interface SetDeviceStateRequest {
  device: Device;
  preview: boolean;
  intent: DeviceCommandIntent;
}

interface StopDeviceEffectRequest {
  device: Device;
}

export interface StartDeviceEffectOptions {
  effect?: DeviceEffect;
  speedMs?: number;
  direction?: 'forward' | 'reverse';
}

interface StartDeviceEffectRequest extends StartDeviceEffectOptions {
  device: Device;
}

export interface DeviceEffectStatus {
  serial: string;
  running: boolean;
  effect?: string;
  error?: string;
}

export interface CommandEngineSettings {
  enabled: boolean;
  enginePath?: string;
  configPath?: string;
  available: boolean;
  warning?: string;
}

interface SetCommandEngineSettingsRequest {
  enabled: boolean;
  enginePath?: string;
  configPath?: string;
}

interface InterpretCommandRequest {
  text: string;
}

export interface CommandPreview {
  summary: string;
  confidence: number;
  confidenceLevel?: string;
  reasons: string[];
  needsConfirmation: boolean;
  empty: boolean;
  commands: CommandPreviewCommand[];
}

export interface CommandPreviewCommand {
  targets: CommandPreviewTarget[];
  action: CommandPreviewAction;
}

export interface CommandPreviewTarget {
  serial: string;
  label?: string;
  group?: string;
  location?: string;
}

export interface CommandPreviewAction {
  power?: boolean;
  hue?: number;
  saturation?: number;
  brightness?: number;
  kelvin?: number;
  durationMs?: number;
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

export async function getNetworkSettings(): Promise<NetworkSettings> {
  const app = window.go?.main?.App;
  if (app?.NetworkSettings) return normalizeNetworkSettings(await app.NetworkSettings());
  return { selectedInterfaceName: '', interfaces: [] };
}

export async function restartDeviceDiscovery(): Promise<NetworkSettings> {
  const app = window.go?.main?.App;
  if (app?.RestartDeviceDiscovery) return normalizeNetworkSettings(await app.RestartDeviceDiscovery());
  return getNetworkSettings();
}

export async function setNetworkInterface(interfaceName: string): Promise<NetworkSettings> {
  const app = window.go?.main?.App;
  if (app?.SetNetworkInterface) return normalizeNetworkSettings(await app.SetNetworkInterface({ interfaceName }));
  return { selectedInterfaceName: interfaceName, interfaces: [] };
}

export async function setDeviceState(device: Device, preview = false, intent: DeviceCommandIntent = 'color'): Promise<Device> {
  const app = window.go?.main?.App;
  if (app?.SetDeviceState) return app.SetDeviceState({ device, preview, intent });
  await new Promise((resolve) => window.setTimeout(resolve, preview ? 60 : 180));
  return device;
}

export async function startDeviceEffect(device: Device, options: StartDeviceEffectOptions = {}): Promise<DeviceEffectStatus> {
  const app = window.go?.main?.App;
  if (app?.StartDeviceEffect) return app.StartDeviceEffect({ device, ...options });
  await new Promise((resolve) => window.setTimeout(resolve, 80));
  if (device.kind === DeviceKind.Single) return { serial: device.serial, running: false, error: 'effects are not supported for single zone devices' };
  return { serial: device.serial, running: true, effect: options.effect ?? (device.kind === DeviceKind.Multizone ? 'move' : 'flame') };
}

export async function stopDeviceEffect(device: Device): Promise<DeviceEffectStatus> {
  const app = window.go?.main?.App;
  if (app?.StopDeviceEffect) return app.StopDeviceEffect({ device });
  await new Promise((resolve) => window.setTimeout(resolve, 80));
  return { serial: device.serial, running: false };
}

export async function getCommandEngineSettings(): Promise<CommandEngineSettings> {
  const app = window.go?.main?.App;
  if (app?.CommandEngineSettings) return normalizeCommandEngineSettings(await app.CommandEngineSettings());
  return { enabled: false, available: false };
}

export async function setCommandEngineSettings(settings: SetCommandEngineSettingsRequest): Promise<CommandEngineSettings> {
  const app = window.go?.main?.App;
  if (app?.SetCommandEngineSettings) return normalizeCommandEngineSettings(await app.SetCommandEngineSettings(settings));
  return { ...settings, available: false };
}

export async function interpretCommand(text: string): Promise<CommandPreview> {
  const app = window.go?.main?.App;
  if (app?.InterpretCommand) return normalizeCommandPreview(await app.InterpretCommand({ text }));
  await new Promise((resolve) => window.setTimeout(resolve, 120));
  return { summary: 'No supported command found', confidence: 0, confidenceLevel: 'low', reasons: ['command engine unavailable'], needsConfirmation: false, empty: true, commands: [] };
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
        serial: 'd0:73:d5:02:b1:20',
        name: 'Wall Switch',
        model: 'LIFX Switch',
        kind: DeviceKind.Switch,
        online: true,
        on: false,
        brightness: 0,
        capability: { hasColor: false, kelvinMin: 0, kelvinMax: 0 },
        relays: [
          { index: 0, on: false },
          { index: 1, on: true },
        ],
        buttonConfig: {
          known: true,
          hapticDurationMs: 180,
          backlightOnColor: { h: 185, s: 0.8, l: 0.65, kelvin: 3500 },
          backlightOffColor: { h: 38, s: 0.15, l: 0.2, kelvin: 2700 },
        },
      },
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

function normalizeNetworkSettings(settings: NetworkSettings | null | undefined): NetworkSettings {
  return {
    selectedInterfaceName: settings?.selectedInterfaceName ?? '',
    interfaces: Array.isArray(settings?.interfaces) ? settings.interfaces : [],
    warning: settings?.warning,
  };
}

function normalizeCommandEngineSettings(settings: CommandEngineSettings | null | undefined): CommandEngineSettings {
  return {
    enabled: Boolean(settings?.enabled),
    enginePath: settings?.enginePath ?? '',
    configPath: settings?.configPath ?? '',
    available: Boolean(settings?.available),
    warning: settings?.warning,
  };
}

function normalizeCommandPreview(preview: CommandPreview | null | undefined): CommandPreview {
  return {
    summary: preview?.summary ?? '',
    confidence: Number(preview?.confidence ?? 0),
    confidenceLevel: preview?.confidenceLevel ?? '',
    reasons: Array.isArray(preview?.reasons) ? preview.reasons : [],
    needsConfirmation: Boolean(preview?.needsConfirmation),
    empty: Boolean(preview?.empty),
    commands: Array.isArray(preview?.commands) ? preview.commands : [],
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
