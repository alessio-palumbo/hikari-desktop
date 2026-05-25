import { useCallback, useEffect, useMemo, useRef, useState } from 'react';
import { getDeviceSnapshot, setDeviceState } from './backend/api';
import { DeviceList } from './components/DeviceList';
import { Inspector } from './components/Inspector';
import { Sidebar } from './components/Sidebar';
import { commitDraft, createDraft, revertDraft, undoDraft, updateDraft, type DeviceDraft } from './domain/editor';
import type { Device, DeviceSnapshot } from './domain/lifx';
import { createPendingState, isPendingConfirmed, isPendingExpired, reconcileSnapshot, type PendingDeviceState } from './domain/reconcile';

const REFRESH_INTERVAL_MS = 5000;
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

  useEffect(() => void refreshSnapshot(), [refreshSnapshot]);

  useEffect(() => {
    const timer = window.setInterval(() => void refreshSnapshot(), REFRESH_INTERVAL_MS);
    return () => window.clearInterval(timer);
  }, [refreshSnapshot]);

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
    if (selectedDevice.kind === 'single') {
      setDraft(undefined);
      return;
    }
    setDraft((prev) => {
      if (prev?.draft.serial === selectedDevice.serial && prev.dirty) return prev;
      return createDraft(selectedDevice);
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
    replaceDevice(next);
    recordPendingState(next, previous);
    setDeviceLoading(next.serial, true);
    try {
      const committed = await setDeviceState(next, true);
      replaceDevice(committed);
      setDeviceLoading(next.serial, false);
    } catch (error) {
      clearPendingState(next.serial);
      if (previous) replaceDevice(previous);
      setDeviceLoading(next.serial, false, errorMessage(error));
    }
  };

  const updateInspectorDevice = async (next: Device) => {
    if (next.kind === 'single') {
      await updateListDevice(next);
      return;
    }
    setDraft((prev) => (prev ? updateDraft(prev, next) : createDraft(next)));
    if (draft?.livePreview) {
      const previous = snapshot.devices.find((device) => device.serial === next.serial);
      recordPendingState(next, previous);
      setDeviceLoading(next.serial, true);
      try {
        await setDeviceState(next, true);
        setDeviceLoading(next.serial, false);
      } catch (error) {
        clearPendingState(next.serial);
        setDeviceLoading(next.serial, false, errorMessage(error));
      }
    }
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
        onRefresh={refreshSnapshot}
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
        dirty={draft?.dirty ?? false}
        livePreview={draft?.livePreview ?? false}
        canUndo={(draft?.history.length ?? 0) > 0}
        saving={saving}
        error={inspectorDevice ? deviceStatus[inspectorDevice.serial]?.error : undefined}
        onClose={() => setSelectedSerial(undefined)}
        onChange={updateInspectorDevice}
        onApply={applyDraft}
        onRevert={() => setDraft((prev) => (prev ? revertDraft(prev) : prev))}
        onUndo={() => setDraft((prev) => (prev ? undoDraft(prev) : prev))}
        onLivePreviewChange={(enabled) => setDraft((prev) => (prev ? { ...prev, livePreview: enabled } : prev))}
      />
    </div>
  );
}

function loadPreference(key: string): string {
  return window.localStorage.getItem(key) ?? '';
}

function savePreference(key: string, value: string) {
  if (value) window.localStorage.setItem(key, value);
}

function errorMessage(error: unknown): string {
  if (error instanceof Error) return error.message;
  if (typeof error === 'string') return error;
  return 'device command failed';
}
