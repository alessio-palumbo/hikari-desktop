import { useCallback, useEffect, useMemo, useRef, useState } from 'react';
import { getDeviceSnapshot, setDeviceState } from './backend/api';
import { DeviceList } from './components/DeviceList';
import { Inspector } from './components/Inspector';
import { Sidebar } from './components/Sidebar';
import { commitDraft, createDraft, revertDraft, undoDraft, updateDraft, type DeviceDraft } from './domain/editor';
import type { Device, DeviceSnapshot, HslColor } from './domain/lifx';
import { createPendingState, isPendingConfirmed, isPendingExpired, reconcileSnapshot, type PendingDeviceState } from './domain/reconcile';

const REFRESH_INTERVAL_MS = 5000;
const STARTUP_MIN_MS = 1800;
const LOCATION_KEY = 'hikari:selectedLocation';
const GROUP_KEY = 'hikari:selectedGroup';

type DeviceStatus = Record<string, { loading?: boolean; error?: string }>;
type PendingDeviceStates = Record<string, PendingDeviceState>;

export function App() {
  const [snapshot, setSnapshot] = useState<DeviceSnapshot>({ locations: [], groups: [], devices: [] });
  const [locationId, setLocationId] = useState(() => loadPreference(LOCATION_KEY));
  const [groupId, setGroupId] = useState(() => loadPreference(GROUP_KEY));
  const [selectedSerial, setSelectedSerial] = useState<string | undefined>();
  const [query, setQuery] = useState('');
  const [draft, setDraft] = useState<DeviceDraft | undefined>();
  const [saving, setSaving] = useState(false);
  const [refreshing, setRefreshing] = useState(false);
  const [startupReady, setStartupReady] = useState(false);
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
    void Promise.allSettled([refreshSnapshot(), delay(STARTUP_MIN_MS)]).finally(() => {
      if (!cancelled) setStartupReady(true);
    });
    return () => {
      cancelled = true;
    };
  }, [refreshSnapshot]);

  useEffect(() => {
    if (!startupReady) return undefined;
    const timer = window.setInterval(() => void refreshSnapshot(), REFRESH_INTERVAL_MS);
    return () => window.clearInterval(timer);
  }, [refreshSnapshot, startupReady]);

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
    const command = prepareDeviceCommand(next, previous);
    replaceDevice(command);
    recordPendingState(command, previous);
    setDeviceLoading(command.serial, true);
    try {
      const committed = await setDeviceState(command, true);
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
    setDraft((prev) => (prev ? updateDraft(prev, next) : createDraft(next)));
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
      const committed = await setDeviceState(draft.draft, false);
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
    return <StartupLoading />;
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
        onLocationChange={setLocationId}
        onGroupChange={(id) => {
          setGroupId(id);
          setSelectedSerial(undefined);
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
        searching={query.trim().length > 0}
        refreshing={refreshing}
        deviceStatus={deviceStatus}
        onSelect={setSelectedSerial}
        onDeviceChange={updateListDevice}
        onMasterChange={(on, brightness) =>
          void Promise.all(
            snapshot.devices
              .filter((device) => device.groupId === groupId)
              .map((device) => updateListDevice({ ...device, on, brightness: brightness ?? device.brightness })),
          )
        }
      />

      <Inspector
        device={inspectorDevice}
        editing={!!draft}
        dirty={draft?.dirty ?? false}
        canUndo={(draft?.history.length ?? 0) > 0}
        saving={saving}
        error={inspectorDevice ? deviceStatus[inspectorDevice.serial]?.error : undefined}
        onClose={() => setSelectedSerial(undefined)}
        onChange={updateInspectorDevice}
        onEnterEditMode={enterEditMode}
        onExitEditMode={() => setDraft(undefined)}
        onApply={applyDraft}
        onRevert={() => setDraft((prev) => (prev ? revertDraft(prev) : prev))}
        onUndo={() => setDraft((prev) => (prev ? undoDraft(prev) : prev))}
      />
    </div>
  );
}

function StartupLoading() {
  return (
    <div className="startup-loading">
      <div className="startup-mark" aria-hidden="true">
        <span />
      </div>
      <div className="startup-copy">
        <strong>Hikari</strong>
        <span>
          discovering LAN devices
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
