import type { DeviceSnapshot } from './lifx';

export interface ReconcileOptions {
  draftSerials?: Set<string>;
}

export function reconcileSnapshot(current: DeviceSnapshot, incoming: DeviceSnapshot, options: ReconcileOptions = {}): DeviceSnapshot {
  const draftSerials = options.draftSerials ?? new Set<string>();
  const incomingBySerial = new Map(incoming.devices.map((device) => [device.serial, device]));
  const currentBySerial = new Map(current.devices.map((device) => [device.serial, device]));
  const devices = incoming.devices.map((device) => {
    const currentDevice = currentBySerial.get(device.serial);
    if (currentDevice && draftSerials.has(device.serial) && currentDevice.kind !== 'single') {
      return { ...currentDevice, online: device.online };
    }
    return device;
  });

  for (const device of current.devices) {
    if (!incomingBySerial.has(device.serial)) {
      devices.push({ ...device, online: false });
    }
  }

  return {
    locations: incoming.locations.length ? incoming.locations : current.locations,
    groups: incoming.groups.length ? incoming.groups : current.groups,
    devices,
  };
}
