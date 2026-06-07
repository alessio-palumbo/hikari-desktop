import { useCallback, useEffect, useMemo, useRef, useState } from 'react';
import { getDeviceSnapshot, setDeviceState } from './backend/api';
import { DeviceList } from './components/DeviceList';
import { GroupInspector } from './components/GroupInspector';
import { Inspector } from './components/Inspector';
import { Sidebar } from './components/Sidebar';
import { activateEditedDevice, commitDraft, createDraft, revertDraft, undoDraft, updateDraft, type DeviceDraft } from './domain/editor';
import type { Device, DeviceSnapshot, HslColor } from './domain/lifx';
import { createPendingState, isPendingConfirmed, isPendingExpired, reconcileSnapshot, type PendingDeviceState } from './domain/reconcile';

const REFRESH_INTERVAL_MS = 5000;
const DISCOVERY_REFRESH_INTERVAL_MS = 1000;
const DISCOVERY_GRACE_MS = 10000;
const INITIAL_DISCOVERY_DELAY_MS = 2000;
const LOCATION_KEY = 'hikari:selectedLocation';
const GROUP_KEY = 'hikari:selectedGroup';

type DeviceStatus = Record<string, { loading?: boolean; error?: string }>;
type PendingDeviceStates = Record<string, PendingDeviceState>;

export function App() {
  const [snapshot, setSnapshot] = useState<DeviceSnapshot>({ locations: [], groups: [], devices: [] });
  const [startupStartedAt] = useState(() => Date.now());
  const [locationId, setLocationId] = useState(() => loadPreference(LOCATION_KEY));
  const [groupId, setGroupId] = useState(() => loadPreference(GROUP_KEY));
  const [selectedSerial, setSelectedSerial] = useState<string | undefined>();
  const [selectedGroupInspectorId, setSelectedGroupInspectorId] = useState<string | undefined>();
  const [query, setQuery] = useState('');
  const [draft, setDraft] = useState<DeviceDraft | undefined>();
  const [saving, setSaving] = useState(false);
  const [refreshing, setRefreshing] = useState(false);
  const [startupReady, setStartupReady] = useState(false);
  const [now, setNow] = useState(() => Date.now());
  const [refreshError, setRefreshError] = useState<string | undefined>();
  const [deviceStatus, setDeviceStatus] = useState<DeviceStatus>({});
  const [pendingState, setPendingState] = useState<PendingDeviceStates>({});
  const draftRef = useRef<DeviceDraft | undefined>(undefined);
  const pendingStateRef = useRef<PendingDeviceStates>({});

  useEffect(() => {
    draftRef.current = draft;
  }, [draft]);

  useEffect(() => {
    pendingStateRef.current = pendingState;
  }, [pendingState]);

  const refreshSnapshot = useCallback(async () => {
    setRefreshing(true);
    try {
      const next = await getDeviceSnapshot();
      const currentDraft = draftRef.current;
      const draftSerials = currentDraft?.dirty ? new Set([currentDraft.draft.serial]) : undefined;
      const pending = pendingStateRef.current;
      setSnapshot((prev) => reconcileSnapshot(prev, next, { draftSerials, pending }));
      clearSettledPending(next, pending);
      setRefreshError(undefined);
    } catch (error) {
      setRefreshError(errorMessage(error));
    } finally {
      setRefreshing(false);
    }
  }, []);

  useEffect(() => {
    let cancelled = false;
    void delay(INITIAL_DISCOVERY_DELAY_MS)
      .then(() => {
        if (cancelled) return undefined;
        return refreshSnapshot();
      })
      .finally(() => {
        if (!cancelled) setStartupReady(true);
      });
    return () => {
      cancelled = true;
    };
  }, [refreshSnapshot]);

  useEffect(() => {
    if (!startupReady) return undefined;
    const interval = snapshot.devices.length ? REFRESH_INTERVAL_MS : DISCOVERY_REFRESH_INTERVAL_MS;
    const timer = window.setInterval(() => void refreshSnapshot(), interval);
    return () => window.clearInterval(timer);
  }, [refreshSnapshot, snapshot.devices.length, startupReady]);

  useEffect(() => {
    if (!startupReady || snapshot.devices.length) return undefined;
    const timer = window.setInterval(() => setNow(Date.now()), 250);
    return () => window.clearInterval(timer);
  }, [snapshot.devices.length, startupReady]);

  useEffect(() => {
    if (snapshot.locations.length && !snapshot.locations.some((location) => location.id === locationId)) {
      setLocationId(snapshot.locations[0].id);
      return;
    }
    const groups = snapshot.groups.filter((group) => group.locationId === locationId);
    if (groups.length && !groups.some((group) => group.id === groupId)) {
      setGroupId(groups[0].id);
      setSelectedSerial(undefined);
    }
  }, [groupId, locationId, snapshot.groups, snapshot.locations]);

  useEffect(() => savePreference(LOCATION_KEY, locationId), [locationId]);
  useEffect(() => savePreference(GROUP_KEY, groupId), [groupId]);

  const selectedDevice = snapshot.devices.find((device) => device.serial === selectedSerial);
  const inspectorGroup = snapshot.groups.find((group) => group.id === selectedGroupInspectorId);
  const inspectorGroupDevices = inspectorGroup ? snapshot.devices.filter((device) => device.groupId === inspectorGroup.id) : [];

  useEffect(() => {
    if (!selectedDevice) {
      setDraft(undefined);
      return;
    }
    setDraft((prev) => {
      if (prev?.draft.serial === selectedDevice.serial) return prev.dirty ? prev : createDraft(selectedDevice);
      return undefined;
    });
  }, [selectedDevice]);

  useEffect(() => {
    if (!selectedSerial && !selectedGroupInspectorId) return undefined;
    const onKeyDown = (event: KeyboardEvent) => {
      if (event.key !== 'Escape') return;
      event.preventDefault();
      event.stopPropagation();
      setSelectedSerial(undefined);
      setSelectedGroupInspectorId(undefined);
    };
    window.addEventListener('keydown', onKeyDown);
    return () => window.removeEventListener('keydown', onKeyDown);
  }, [selectedGroupInspectorId, selectedSerial]);

  const selectDevice = (serial: string) => {
    setSelectedGroupInspectorId(undefined);
    setSelectedSerial((current) => (current === serial ? undefined : serial));
  };

  const openGroupInspector = () => {
    if (!currentGroup) return;
    setSelectedSerial(undefined);
    setSelectedGroupInspectorId((current) => (current === currentGroup.id ? undefined : currentGroup.id));
  };

  const closeInspector = () => {
    setSelectedSerial(undefined);
    setSelectedGroupInspectorId(undefined);
  };

  const visibleDevices = useMemo(() => {
    const q = query.trim().toLowerCase();
    if (q) {
      return snapshot.devices.filter((device) => {
        const group = snapshot.groups.find((entry) => entry.id === device.groupId);
        return [device.name, device.serial, device.model, group?.name ?? ''].some((value) => value.toLowerCase().includes(q));
      });
    }
    return snapshot.devices.filter((device) => device.groupId === groupId);
  }, [groupId, query, snapshot.devices, snapshot.groups]);

  const currentGroup = snapshot.groups.find((group) => group.id === groupId);
  const inspectorDevice = draft?.draft ?? selectedDevice;

  const replaceDevice = (next: Device) => {
    setSnapshot((prev) => ({ ...prev, devices: prev.devices.map((device) => (device.serial === next.serial ? next : device)) }));
  };

  const recordPendingState = (next: Device, previous?: Device) => {
    const pending = createPendingState(next, previous);
    if (!pending) return;
    setPendingState((prev) => ({ ...prev, [next.serial]: pending }));
  };

  const clearPendingState = (serial: string) => {
    setPendingState((prev) => {
      if (!prev[serial]) return prev;
      const next = { ...prev };
      delete next[serial];
      return next;
    });
  };

  const clearSettledPending = (next: DeviceSnapshot, pending: PendingDeviceStates) => {
    const now = Date.now();
    const bySerial = new Map(next.devices.map((device) => [device.serial, device]));
    setPendingState((prev) => {
      let changed = false;
      const updated = { ...prev };
      for (const item of Object.values(pending)) {
        const device = bySerial.get(item.serial);
        if ((device && isPendingConfirmed(device, item)) || isPendingExpired(item, now)) {
          delete updated[item.serial];
          changed = true;
        }
      }
      return changed ? updated : prev;
    });
  };

  const setDeviceLoading = (serial: string, loading: boolean, error?: string) => {
    setDeviceStatus((prev) => ({ ...prev, [serial]: { loading, error } }));
  };

  const updateListDevice = async (next: Device) => {
    const previous = snapshot.devices.find((device) => device.serial === next.serial);
    const brightnessOnly = isBrightnessOnlyChange(next, previous);
    const command = prepareDeviceCommand(next, previous);
    const powerChanged = didPowerChange(command, previous);
    const powerOnly = isPowerOnlyChange(command, previous);
    replaceDevice(command);
    recordPendingState(command, previous);
    setDeviceLoading(command.serial, true);
    try {
      const committed = await setDeviceState(command, true, powerChanged, powerOnly, brightnessOnly);
      replaceDevice(committed);
      setDeviceLoading(command.serial, false);
    } catch (error) {
      clearPendingState(command.serial);
      if (previous) replaceDevice(previous);
      setDeviceLoading(command.serial, false, errorMessage(error));
    }
  };

  const updateInspectorDevice = async (next: Device) => {
    if (next.kind === 'single' || !draft) {
      await updateListDevice(next);
      return;
    }
    setDraft((prev) => (prev ? updateDraft(prev, activateEditedDevice(next)) : createDraft(activateEditedDevice(next))));
  };

  const enterEditMode = () => {
    if (!selectedDevice || selectedDevice.kind === 'single') return;
    setDraft((prev) => (prev?.draft.serial === selectedDevice.serial ? prev : createDraft(selectedDevice)));
  };

  const applyDraft = async () => {
    if (!draft) return;
    setSaving(true);
    setDeviceLoading(draft.draft.serial, true);
    try {
      const committed = await setDeviceState(draft.draft, false, didPowerChange(draft.draft, draft.base));
      recordPendingState(committed, draft.base);
      replaceDevice(committed);
      setDraft(commitDraft(draft, committed));
      setDeviceLoading(draft.draft.serial, false);
    } catch (error) {
      clearPendingState(draft.draft.serial);
      setDeviceLoading(draft.draft.serial, false, errorMessage(error));
    } finally {
      setSaving(false);
    }
  };

  if (!startupReady) {
    return <DiscoveryStatus title="ひかり" message="discovering LAN devices" />;
  }

  const showingInitialDiscovery = !snapshot.devices.length && now - startupStartedAt < DISCOVERY_GRACE_MS;

  if (showingInitialDiscovery) {
    return <DiscoveryStatus title="ひかり" message="discovering LAN devices" />;
  }

  if (!snapshot.devices.length) {
    return <DiscoveryStatus title="No devices found." message="Discovering" />;
  }

  return (
    <div className="app-shell">
      <Sidebar
        locations={snapshot.locations}
        groups={snapshot.groups}
        devices={snapshot.devices}
        selectedLocationId={locationId}
        selectedGroupId={groupId}
        query={query}
        refreshing={refreshing}
        refreshError={refreshError}
        onQueryChange={setQuery}
        onLocationChange={(id) => {
          setLocationId(id);
          setSelectedSerial(undefined);
          setSelectedGroupInspectorId(undefined);
        }}
        onGroupChange={(id) => {
          setGroupId(id);
          setSelectedSerial(undefined);
          setSelectedGroupInspectorId(undefined);
          setQuery('');
        }}
        onLocationPower={(id, on) =>
          void Promise.all(
            snapshot.devices
              .filter((device) => {
                const group = snapshot.groups.find((entry) => entry.id === device.groupId);
                return group?.locationId === id;
              })
              .map((device) => updateListDevice({ ...device, on })),
          )
        }
        onGroupPower={(id, on) =>
          void Promise.all(snapshot.devices.filter((device) => device.groupId === id).map((device) => updateListDevice({ ...device, on })))
        }
      />

      <DeviceList
        group={currentGroup}
        groups={snapshot.groups}
        devices={visibleDevices}
        selectedSerial={selectedSerial}
        groupInspecting={selectedGroupInspectorId === currentGroup?.id}
        searching={query.trim().length > 0}
        refreshing={refreshing}
        deviceStatus={deviceStatus}
        onSelect={selectDevice}
        onGroupInspect={openGroupInspector}
        onSurfaceClick={closeInspector}
        onDeviceChange={updateListDevice}
        onMasterChange={(on, brightness) =>
          void Promise.all(
            snapshot.devices
              .filter((device) => device.groupId === groupId)
              .map((device) => updateListDevice({ ...device, on, brightness: brightness ?? device.brightness })),
          )
        }
      />

      {inspectorDevice ? (
        <Inspector
          device={inspectorDevice}
          editing={!!draft}
          dirty={draft?.dirty ?? false}
          canUndo={(draft?.history.length ?? 0) > 0}
          saving={saving}
          error={deviceStatus[inspectorDevice.serial]?.error}
          onClose={() => setSelectedSerial(undefined)}
          onChange={updateInspectorDevice}
          onEnterEditMode={enterEditMode}
          onExitEditMode={() => setDraft(undefined)}
          onApply={applyDraft}
          onRevert={() => setDraft((prev) => (prev ? revertDraft(prev) : prev))}
          onUndo={() => setDraft((prev) => (prev ? undoDraft(prev) : prev))}
        />
      ) : inspectorGroup ? (
        <GroupInspector
          group={inspectorGroup}
          devices={inspectorGroupDevices}
          onClose={() => setSelectedGroupInspectorId(undefined)}
          onDeviceChange={updateListDevice}
        />
      ) : null}
    </div>
  );
}

function DiscoveryStatus(props: { title: string; message: string }) {
  return (
    <div className="discovery-status">
      <div className="discovery-copy">
        <strong>{props.title}</strong>
        <span>
          {props.message}
          <i aria-hidden="true" />
        </span>
      </div>
    </div>
  );
}

function loadPreference(key: string): string {
  try {
    return window.localStorage.getItem(key) ?? '';
  } catch (error) {
    console.warn(`Unable to read preference ${key}`, error);
    return '';
  }
}

function savePreference(key: string, value: string) {
  try {
    if (value) window.localStorage.setItem(key, value);
  } catch (error) {
    console.warn(`Unable to save preference ${key}`, error);
  }
}

function prepareDeviceCommand(next: Device, previous?: Device): Device {
  if (!previous || near(previous.brightness, next.brightness) || next.kind === 'single') return next;
  return applyBrightnessToZones(next, next.brightness);
}

function didPowerChange(next: Device, previous?: Device): boolean {
  return !previous || next.on !== previous.on;
}

function isPowerOnlyChange(next: Device, previous?: Device): boolean {
  if (!previous || next.on === previous.on) return false;
  if (!near(next.brightness, previous.brightness)) return false;
  if (!sameColor(next.color, previous.color)) return false;
  if (!sameColors(next.zones, previous.zones)) return false;
  return sameMatrixChain(next.chain, previous.chain);
}

function isBrightnessOnlyChange(next: Device, previous?: Device): boolean {
  if (!previous || near(next.brightness, previous.brightness)) return false;
  if (!sameColor(next.color, previous.color)) return false;
  if (!sameColors(next.zones, previous.zones)) return false;
  return sameMatrixChain(next.chain, previous.chain);
}

function sameColor(a?: HslColor, b?: HslColor): boolean {
  if (!a || !b) return a === b;
  return near(a.h, b.h) && near(a.s, b.s) && near(a.l, b.l) && (a.kelvin ?? 0) === (b.kelvin ?? 0);
}

function sameColors(a?: HslColor[], b?: HslColor[]): boolean {
  if (!a || !b) return a === b;
  if (a.length !== b.length) return false;
  return a.every((color, index) => sameColor(color, b[index]));
}

function sameMatrixChain(a?: Device['chain'], b?: Device['chain']): boolean {
  if (!a || !b) return a === b;
  if (a.length !== b.length) return false;
  return a.every((matrix, index) => {
    const other = b[index];
    return (
      matrix.id === other.id &&
      near(matrix.x, other.x) &&
      near(matrix.y, other.y) &&
      near(matrix.w, other.w) &&
      near(matrix.h, other.h) &&
      (matrix.sendWidth ?? 0) === (other.sendWidth ?? 0) &&
      (matrix.orientation ?? 0) === (other.orientation ?? 0) &&
      sameColors(matrix.pixels, other.pixels)
    );
  });
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

function near(a: number, b: number) {
  return Math.abs(a - b) < 0.01;
}

function errorMessage(error: unknown): string {
  if (error instanceof Error) return error.message;
  if (typeof error === 'string') return error;
  return 'device command failed';
}

function delay(ms: number) {
  return new Promise((resolve) => window.setTimeout(resolve, ms));
}
