import { useEffect, useMemo, useState } from 'react';
import { applyDevice, getDeviceSnapshot } from './backend/api';
import { DeviceList } from './components/DeviceList';
import { Inspector } from './components/Inspector';
import { Sidebar } from './components/Sidebar';
import { commitDraft, createDraft, revertDraft, undoDraft, updateDraft, type DeviceDraft } from './domain/editor';
import type { Device, DeviceSnapshot } from './domain/lifx';

export function App() {
  const [snapshot, setSnapshot] = useState<DeviceSnapshot>({ locations: [], groups: [], devices: [] });
  const [locationId, setLocationId] = useState('');
  const [groupId, setGroupId] = useState('');
  const [selectedId, setSelectedId] = useState<string | undefined>();
  const [query, setQuery] = useState('');
  const [draft, setDraft] = useState<DeviceDraft | undefined>();
  const [saving, setSaving] = useState(false);

  useEffect(() => {
    getDeviceSnapshot().then((next) => {
      setSnapshot(next);
      setLocationId(next.locations[0]?.id ?? '');
      setGroupId(next.groups[0]?.id ?? '');
    });
  }, []);

  useEffect(() => {
    const groups = snapshot.groups.filter((group) => group.locationId === locationId);
    if (groups.length && !groups.some((group) => group.id === groupId)) {
      setGroupId(groups[0].id);
      setSelectedId(undefined);
    }
  }, [groupId, locationId, snapshot.groups]);

  const selectedDevice = snapshot.devices.find((device) => device.id === selectedId);

  useEffect(() => {
    if (!selectedDevice) {
      setDraft(undefined);
      return;
    }
    setDraft(selectedDevice.kind === 'single' ? undefined : createDraft(selectedDevice));
  }, [selectedDevice?.id]);

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
    setSnapshot((prev) => ({ ...prev, devices: prev.devices.map((device) => (device.id === next.id ? next : device)) }));
  };

  const updateListDevice = async (next: Device) => {
    replaceDevice(next);
    await applyDevice(next, true);
  };

  const updateInspectorDevice = async (next: Device) => {
    if (next.kind === 'single') {
      replaceDevice(next);
      await applyDevice(next, true);
      return;
    }
    setDraft((prev) => (prev ? updateDraft(prev, next) : createDraft(next)));
    if (draft?.livePreview) await applyDevice(next, true);
  };

  const applyDraft = async () => {
    if (!draft) return;
    setSaving(true);
    const committed = await applyDevice(draft.draft, false);
    replaceDevice(committed);
    setDraft(commitDraft(draft, committed));
    setSaving(false);
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
        onQueryChange={setQuery}
        onLocationChange={setLocationId}
        onGroupChange={(id) => {
          setGroupId(id);
          setSelectedId(undefined);
          setQuery('');
        }}
        onGroupPower={(id, on) => setSnapshot((prev) => ({ ...prev, devices: prev.devices.map((device) => (device.groupId === id ? { ...device, on } : device)) }))}
      />

      <DeviceList
        group={currentGroup}
        devices={visibleDevices}
        selectedId={selectedId}
        searching={query.trim().length > 0}
        onSelect={setSelectedId}
        onDeviceChange={updateListDevice}
        onMasterChange={(on, brightness) =>
          setSnapshot((prev) => ({
            ...prev,
            devices: prev.devices.map((device) => (device.groupId === groupId ? { ...device, on, brightness: brightness ?? device.brightness } : device)),
          }))
        }
      />

      <Inspector
        device={inspectorDevice}
        dirty={draft?.dirty ?? false}
        livePreview={draft?.livePreview ?? false}
        canUndo={(draft?.history.length ?? 0) > 0}
        saving={saving}
        onClose={() => setSelectedId(undefined)}
        onChange={updateInspectorDevice}
        onApply={applyDraft}
        onRevert={() => setDraft((prev) => (prev ? revertDraft(prev) : prev))}
        onUndo={() => setDraft((prev) => (prev ? undoDraft(prev) : prev))}
        onLivePreviewChange={(enabled) => setDraft((prev) => (prev ? { ...prev, livePreview: enabled } : prev))}
      />
    </div>
  );
}
