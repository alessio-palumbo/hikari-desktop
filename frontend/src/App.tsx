import { useCallback, useEffect, useMemo, useRef, useState } from 'react';
import type { ReactNode } from 'react';
import { getCommandEngineSettings, getDeviceSnapshot, getNetworkSettings, interpretCommand, restartDeviceDiscovery, setCommandEngineSettings, setDeviceState, setNetworkInterface, startDeviceEffect, stopDeviceEffect, type CommandEngineSettings, type CommandPreview, type DeviceEffectStatus, type NetworkSettings } from './backend/api';
import { CommandModal } from './components/CommandModal';
import { DeviceList } from './components/DeviceList';
import { GroupInspector } from './components/GroupInspector';
import { Inspector } from './components/Inspector';
import { NetworkInterfaceControl, Sidebar } from './components/Sidebar';
import { commandIntent, draftIntent, prepareDeviceCommand } from './domain/commands';
import { activateEditedDevice, commitDraft, createDraft, revertDraft, undoDraft, updateDraft, type DeviceDraft } from './domain/editor';
import type { DeviceEffect } from './domain/effects';
import { DeviceKind, isLightDevice, type Device, type DeviceSnapshot } from './domain/lifx';
import { applyTextCommandAction, executableTextCommandTargets } from './domain/textCommands';
import { createPendingState, isPendingConfirmed, isPendingExpired, reconcileSnapshot, type PendingDeviceState } from './domain/reconcile';

const REFRESH_INTERVAL_MS = 5000;
const DISCOVERY_REFRESH_INTERVAL_MS = 1000;
const DISCOVERY_GRACE_MS = 10000;
const INITIAL_DISCOVERY_DELAY_MS = 2000;
const LOCATION_KEY = 'hikari:selectedLocation';
const GROUP_KEY = 'hikari:selectedGroup';

type DeviceStatus = Record<string, { loading?: boolean; error?: string }>;
type DeviceEffectStates = Record<string, DeviceEffectStatus & { loading?: boolean }>;
type PendingDeviceStates = Record<string, PendingDeviceState>;

export function App() {
  const [snapshot, setSnapshot] = useState<DeviceSnapshot>({ locations: [], groups: [], devices: [] });
  const [discoveryStartedAt, setDiscoveryStartedAt] = useState(() => Date.now());
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
  const [networkSettings, setNetworkSettings] = useState<NetworkSettings>({ selectedInterfaceName: '', interfaces: [] });
  const [networkChanging, setNetworkChanging] = useState(false);
  const [commandSettings, setCommandSettings] = useState<CommandEngineSettings>({ enabled: false, available: false });
  const [commandOpen, setCommandOpen] = useState(false);
  const [commandInterpreting, setCommandInterpreting] = useState(false);
  const [commandExecuting, setCommandExecuting] = useState(false);
  const [commandPreview, setCommandPreview] = useState<CommandPreview | undefined>();
  const [commandError, setCommandError] = useState<string | undefined>();
  const [deviceStatus, setDeviceStatus] = useState<DeviceStatus>({});
  const [deviceEffectStatus, setDeviceEffectStatus] = useState<DeviceEffectStates>({});
  const [pendingState, setPendingState] = useState<PendingDeviceStates>({});
  const draftRef = useRef<DeviceDraft | undefined>(undefined);
  const pendingStateRef = useRef<PendingDeviceStates>({});
  const networkRecoveryRef = useRef(false);

  useEffect(() => {
    draftRef.current = draft;
  }, [draft]);

  useEffect(() => {
    pendingStateRef.current = pendingState;
  }, [pendingState]);

  useEffect(() => {
    let cancelled = false;
    void getNetworkSettings()
      .then((settings) => {
        if (!cancelled) setNetworkSettings(settings);
      })
      .catch((error) => {
        if (!cancelled) setNetworkSettings((prev) => ({ ...prev, warning: errorMessage(error) }));
      });
    void getCommandEngineSettings()
      .then((settings) => {
        if (!cancelled) setCommandSettings(settings);
      })
      .catch((error) => {
        if (!cancelled) setCommandSettings((prev) => ({ ...prev, warning: errorMessage(error) }));
      });
    return () => {
      cancelled = true;
    };
  }, []);

  const refreshSnapshot = useCallback(async () => {
    setRefreshing(true);
    try {
      const next = await readSnapshotWithRecovery(networkRecoveryRef);
      const currentDraft = draftRef.current;
      const draftSerials = currentDraft?.dirty ? new Set([currentDraft.draft.serial]) : undefined;
      const pending = pendingStateRef.current;
      setSnapshot((prev) => reconcileSnapshot(prev, next, { draftSerials, pending }));
      clearSettledPending(next, pending);
      setRefreshError(undefined);
      void getNetworkSettings()
        .then((settings) => setNetworkSettings({ ...settings, warning: undefined }))
        .catch(() => setNetworkSettings((prev) => (prev.warning ? { ...prev, warning: undefined } : prev)));
    } catch (error) {
      handleSnapshotRefreshError(error);
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

  const openCommandModal = useCallback(() => {
    setCommandOpen(true);
    if (commandSettings.enabled) return;
    setCommandSettings((prev) => ({ ...prev, enabled: true, warning: undefined }));
    void setCommandEngineSettings({
      enabled: true,
      enginePath: commandSettings.enginePath,
      configPath: commandSettings.configPath,
    })
      .then(setCommandSettings)
      .catch((error) => setCommandSettings((prev) => ({ ...prev, warning: errorMessage(error) })));
  }, [commandSettings]);

  useEffect(() => {
    const onKeyDown = (event: KeyboardEvent) => {
      if (event.key !== ' ' || event.metaKey || event.ctrlKey || event.altKey || event.shiftKey) return;
      if (commandOpen || isEditableShortcutTarget(event.target, { allowEmptySearch: true })) return;
      event.preventDefault();
      event.stopPropagation();
      openCommandModal();
    };
    window.addEventListener('keydown', onKeyDown);
    return () => window.removeEventListener('keydown', onKeyDown);
  }, [commandOpen, openCommandModal]);

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

  const setDeviceEffectLoading = (serial: string, loading: boolean, error?: string) => {
    setDeviceEffectStatus((prev) => ({ ...prev, [serial]: { ...(prev[serial] ?? { serial, running: false }), loading, error } }));
  };

  const handleSnapshotRefreshError = (error: unknown) => {
    const message = networkErrorMessage(error);
    setSelectedSerial(undefined);
    setSelectedGroupInspectorId(undefined);
    setDraft(undefined);
    setDeviceStatus({});
    setDeviceEffectStatus({});
    setPendingState({});
    setSnapshot({ locations: [], groups: [], devices: [] });
    setNetworkSettings((prev) => ({ ...prev, interfaces: [], warning: undefined }));
    setRefreshError(message);
    setDiscoveryStartedAt(Date.now());
    setNow(Date.now());
    void getNetworkSettings()
      .then((settings) => setNetworkSettings({ ...settings, warning: undefined }))
      .catch(() => undefined);
  };

  const handleRecoverableNetworkError = (error: unknown): boolean => {
    if (!isRecoverableNetworkError(error)) return false;
    const message = networkErrorMessage(error);
    setSelectedSerial(undefined);
    setSelectedGroupInspectorId(undefined);
    setDraft(undefined);
    setDeviceStatus({});
    setDeviceEffectStatus({});
    setPendingState({});
    setSnapshot({ locations: [], groups: [], devices: [] });
    setNetworkSettings((prev) => ({ ...prev, interfaces: [], warning: undefined }));
    setRefreshError(message);
    setDiscoveryStartedAt(Date.now());
    setNow(Date.now());
    void getNetworkSettings()
      .then((settings) => setNetworkSettings({ ...settings, warning: undefined }))
      .catch(() => undefined);
    return true;
  };

  const updateListDevice = async (next: Device) => {
    const previous = snapshot.devices.find((device) => device.serial === next.serial);
    const intent = commandIntent(next, previous);
    const command = prepareDeviceCommand(next, previous);
    replaceDevice(command);
    recordPendingState(command, previous);
    setDeviceLoading(command.serial, true);
    try {
      const committed = await setDeviceState(command, true, intent);
      replaceDevice(committed);
      setDeviceLoading(command.serial, false);
    } catch (error) {
      clearPendingState(command.serial);
      if (handleRecoverableNetworkError(error)) return;
      if (previous) replaceDevice(previous);
      setDeviceLoading(command.serial, false, errorMessage(error));
    }
  };

  const updateInspectorDevice = async (next: Device) => {
    if (!isLightDevice(next) || next.kind === DeviceKind.Single || !draft) {
      await updateListDevice(next);
      return;
    }
    setDraft((prev) => (prev ? updateDraft(prev, activateEditedDevice(next)) : createDraft(activateEditedDevice(next))));
  };

  const enterEditMode = () => {
    if (!selectedDevice || !isLightDevice(selectedDevice) || selectedDevice.kind === DeviceKind.Single) return;
    setDraft((prev) => (prev?.draft.serial === selectedDevice.serial ? prev : createDraft(selectedDevice)));
  };

  const applyDraft = async () => {
    if (!draft) return;
    setSaving(true);
    setDeviceLoading(draft.draft.serial, true);
    try {
      const committed = await setDeviceState(draft.draft, false, draftIntent(draft.draft));
      recordPendingState(committed, draft.base);
      replaceDevice(committed);
      setDraft(commitDraft(draft, committed));
      setDeviceLoading(draft.draft.serial, false);
    } catch (error) {
      clearPendingState(draft.draft.serial);
      if (handleRecoverableNetworkError(error)) return;
      setDeviceLoading(draft.draft.serial, false, errorMessage(error));
    } finally {
      setSaving(false);
    }
  };

  const startInspectorEffect = async (device: Device, effect: DeviceEffect, speedMs: number) => {
    setDeviceEffectLoading(device.serial, true);
    try {
      const status = await startDeviceEffect(device, { effect, speedMs });
      setDeviceEffectStatus((prev) => ({ ...prev, [device.serial]: { ...status, loading: false } }));
    } catch (error) {
      if (handleRecoverableNetworkError(error)) return;
      setDeviceEffectLoading(device.serial, false, errorMessage(error));
    }
  };

  const stopInspectorEffect = async (device: Device) => {
    setDeviceEffectLoading(device.serial, true);
    try {
      const status = await stopDeviceEffect(device);
      setDeviceEffectStatus((prev) => ({ ...prev, [device.serial]: { ...status, loading: false } }));
    } catch (error) {
      if (handleRecoverableNetworkError(error)) return;
      setDeviceEffectLoading(device.serial, false, errorMessage(error));
    }
  };

  const resetDiscoveryState = () => {
    setSelectedSerial(undefined);
    setSelectedGroupInspectorId(undefined);
    setDraft(undefined);
    setDeviceStatus({});
    setDeviceEffectStatus({});
    setPendingState({});
    setSnapshot({ locations: [], groups: [], devices: [] });
    setDiscoveryStartedAt(Date.now());
    setLocationId('');
    setGroupId('');
    setNow(Date.now());
  };

  const changeNetworkInterface = async (interfaceName: string) => {
    setNetworkChanging(true);
    setRefreshError(undefined);
    resetDiscoveryState();
    try {
      const settings = await setNetworkInterface(interfaceName);
      setNetworkSettings({ ...settings, warning: undefined });
      await refreshSnapshot();
    } catch (error) {
      const message = networkErrorMessage(error);
      setNetworkSettings((prev) => ({ ...prev, warning: isRecoverableNetworkError(error) ? undefined : message }));
      setRefreshError(message);
    } finally {
      setNetworkChanging(false);
    }
  };

  const refreshDiscovery = async () => {
    setNetworkChanging(true);
    setRefreshError(undefined);
    resetDiscoveryState();
    try {
      const settings = await restartDeviceDiscovery();
      setNetworkSettings({ ...settings, warning: undefined });
      await delay(INITIAL_DISCOVERY_DELAY_MS);
      await refreshSnapshot();
    } catch (error) {
      const message = networkErrorMessage(error);
      setNetworkSettings((prev) => ({ ...prev, warning: isRecoverableNetworkError(error) ? undefined : message }));
      setRefreshError(message);
    } finally {
      setNetworkChanging(false);
    }
  };

  const interpretTextCommand = async (text: string) => {
    setCommandInterpreting(true);
    setCommandError(undefined);
    try {
      const preview = await interpretCommand(text);
      setCommandPreview(preview);
    } catch (error) {
      setCommandPreview(undefined);
      setCommandError(errorMessage(error));
      void getCommandEngineSettings().then(setCommandSettings).catch(() => undefined);
    } finally {
      setCommandInterpreting(false);
    }
  };

  const confirmTextCommand = async () => {
    if (!commandPreview) return;
    setCommandExecuting(true);
    setCommandError(undefined);
    try {
      for (const command of commandPreview.commands) {
        const devices = executableTextCommandTargets([command], snapshot.devices);
        for (const device of devices) {
          await updateListDevice(applyTextCommandAction(device, command.action));
        }
      }
      setCommandOpen(false);
      setCommandPreview(undefined);
    } catch (error) {
      setCommandError(errorMessage(error));
    } finally {
      setCommandExecuting(false);
    }
  };

  if (!startupReady) {
    return <DiscoveryStatus title="ひかり" message="discovering LAN devices" />;
  }

  const showingInitialDiscovery = !snapshot.devices.length && now - discoveryStartedAt < DISCOVERY_GRACE_MS;

  if (showingInitialDiscovery) {
    return (
      <DiscoveryStatus title="ひかり" message={refreshError ?? 'discovering LAN devices'}>
        {refreshError ? <DiscoveryActions networkSettings={networkSettings} networkChanging={networkChanging} onNetworkChange={changeNetworkInterface} onRefresh={refreshDiscovery} /> : null}
      </DiscoveryStatus>
    );
  }

  if (!snapshot.devices.length) {
    return (
      <DiscoveryStatus title={refreshError ? 'ひかり' : 'No devices found.'} message={refreshError ?? 'Discovering'}>
        <DiscoveryActions networkSettings={networkSettings} networkChanging={networkChanging} onNetworkChange={changeNetworkInterface} onRefresh={refreshDiscovery} />
      </DiscoveryStatus>
    );
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
        networkSettings={networkSettings}
        networkChanging={networkChanging}
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
              .filter(isLightDevice)
              .filter((device) => {
                const group = snapshot.groups.find((entry) => entry.id === device.groupId);
                return group?.locationId === id;
              })
              .map((device) => updateListDevice({ ...device, on })),
          )
        }
        onNetworkInterfaceChange={(name) => void changeNetworkInterface(name)}
        onRefreshDiscovery={() => void refreshDiscovery()}
        onOpenCommands={openCommandModal}
        onGroupPower={(id, on) =>
          void Promise.all(snapshot.devices.filter(isLightDevice).filter((device) => device.groupId === id).map((device) => updateListDevice({ ...device, on })))
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
        deviceEffectStatus={deviceEffectStatus}
        onSelect={selectDevice}
        onGroupInspect={openGroupInspector}
        onSurfaceClick={closeInspector}
        onDeviceChange={updateListDevice}
        onMasterChange={(on, brightness) =>
          void Promise.all(
            snapshot.devices
              .filter(isLightDevice)
              .filter((device) => device.groupId === groupId)
              .map((device) => updateListDevice({ ...device, on, brightness: brightness ?? device.brightness })),
          )
        }
      />

      <CommandModal
        open={commandOpen}
        interpreting={commandInterpreting}
        executing={commandExecuting}
        error={commandError}
        warning={commandSettings.warning}
        preview={commandPreview}
        onClose={() => setCommandOpen(false)}
        onInterpret={(text) => void interpretTextCommand(text)}
        onConfirm={() => void confirmTextCommand()}
        onClear={() => {
          setCommandPreview(undefined);
          setCommandError(undefined);
        }}
      />

      {inspectorDevice ? (
        <Inspector
          device={inspectorDevice}
          editing={!!draft}
          dirty={draft?.dirty ?? false}
          canUndo={(draft?.history.length ?? 0) > 0}
          saving={saving}
          error={deviceStatus[inspectorDevice.serial]?.error}
          effectStatus={deviceEffectStatus[inspectorDevice.serial]}
          onClose={() => setSelectedSerial(undefined)}
          onChange={updateInspectorDevice}
          onStartEffect={(effect, speedMs) => void startInspectorEffect(inspectorDevice, effect, speedMs)}
          onStopEffect={() => void stopInspectorEffect(inspectorDevice)}
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

function isEditableShortcutTarget(target: EventTarget | null, options: { allowEmptySearch?: boolean } = {}): boolean {
  if (!(target instanceof HTMLElement)) return false;
  if (options.allowEmptySearch && target instanceof HTMLInputElement && target.dataset.shortcutTarget === 'search' && target.value.length === 0) return false;
  const tag = target.tagName.toLowerCase();
  return tag === 'input' || tag === 'textarea' || tag === 'select' || tag === 'button' || target.isContentEditable;
}

async function readSnapshotWithRecovery(recoveryRef: { current: boolean }): Promise<DeviceSnapshot> {
  try {
    return await getDeviceSnapshot();
  } catch (error) {
    if (!isRecoverableNetworkError(error) || recoveryRef.current) throw error;
    recoveryRef.current = true;
    try {
      await restartDeviceDiscovery();
      await delay(INITIAL_DISCOVERY_DELAY_MS);
      return await getDeviceSnapshot();
    } finally {
      recoveryRef.current = false;
    }
  }
}

function DiscoveryActions(props: { networkSettings: NetworkSettings; networkChanging: boolean; onNetworkChange: (name: string) => void; onRefresh: () => void }) {
  return (
    <div className="discovery-settings">
      <NetworkInterfaceControl settings={props.networkSettings} changing={props.networkChanging} onChange={(name) => void props.onNetworkChange(name)} onRefresh={props.onRefresh} />
    </div>
  );
}

function DiscoveryStatus(props: { title: string; message: string; children?: ReactNode }) {
  return (
    <div className="discovery-status">
      <div className="discovery-copy">
        <strong>{props.title}</strong>
        <span>
          {props.message}
          <i aria-hidden="true" />
        </span>
      </div>
      {props.children}
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

function errorMessage(error: unknown): string {
  if (error instanceof Error) return error.message;
  if (typeof error === 'string') return error;
  return 'device command failed';
}

function isRecoverableNetworkError(error: unknown): boolean {
  const lower = errorMessage(error).toLowerCase();
  return (
    lower.includes('lifx transport has not been started') ||
    lower.includes('no suitable broadcast interface') ||
    lower.includes('network interface') ||
    lower.includes('network interfaces') ||
    lower.includes('network connection') ||
    lower.includes('connection lost') ||
    lower.includes('failed to send message to device') ||
    lower.includes('send message to device')
  );
}

function networkErrorMessage(error: unknown): string {
  const message = errorMessage(error);
  const lower = message.toLowerCase();
  if ((lower.includes('network interface') || lower.includes('network interfaces')) && (lower.includes('not available') || lower.includes('unavailable'))) {
    return 'Selected network interface unavailable. Choose another interface or Automatic.';
  }
  if (lower.includes('no suitable broadcast interface') || lower.includes('broadcast interface') || lower.includes('network interfaces')) {
    return 'No network interfaces found.';
  }
  if (
    lower.includes('lifx transport has not been started') ||
    lower.includes('network connection') ||
    lower.includes('connection lost') ||
    lower.includes('failed to send message to device') ||
    lower.includes('send message to device')
  ) {
    return 'Connection lost. Refresh discovery to reconnect.';
  }
  return message;
}

function delay(ms: number) {
  return new Promise((resolve) => window.setTimeout(resolve, ms));
}
