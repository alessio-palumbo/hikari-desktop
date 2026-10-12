import { useState, type FormEvent } from 'react';
import { ChevronDown } from 'lucide-react';
import type { Device, Group, Location } from '../domain/lifx';

export function DeviceMetadataEditor({
  device,
  locations,
  groups,
  saving,
  onCancel,
  onSave,
  cancelLabel = 'cancel',
  initialValues,
}: {
  device: Device;
  locations: Location[];
  groups: Group[];
  saving: boolean;
  onCancel: () => void;
  cancelLabel?: string;
  initialValues?: { label: string; locationId: string; groupId: string };
  onSave: (request: { serial: string; label: string; locationId: string; groupId: string }) => Promise<void>;
}) {
  const currentGroup = groups.find((group) => group.id === device.groupId);
  const [label, setLabel] = useState(initialValues?.label ?? device.name);
  const [locationSelection, setLocationId] = useState(initialValues?.locationId ?? currentGroup?.locationId ?? locations[0]?.id ?? '');
  const locationId = locations.some((location) => location.id === locationSelection) ? locationSelection : currentGroup?.locationId ?? locations[0]?.id ?? '';
  const locationGroups = groups.filter((group) => group.locationId === locationId);
  const [groupSelection, setGroupId] = useState(initialValues?.groupId ?? (locationGroups.some((group) => group.id === device.groupId) ? device.groupId : locationGroups[0]?.id ?? ''));
  const groupId = locationGroups.some((group) => group.id === groupSelection) ? groupSelection : locationGroups.find((group) => group.id === device.groupId)?.id ?? locationGroups[0]?.id ?? '';
  const [error, setError] = useState<string>();
  const normalizedLabel = label.trim();
  const unchanged = normalizedLabel === device.name && groupId === device.groupId;

  const submit = async (event: FormEvent) => {
    event.preventDefault();
    if (!normalizedLabel || !locationId || !groupId || saving) return;
    setError(undefined);
    try {
      await onSave({ serial: device.serial, label: normalizedLabel, locationId, groupId });
    } catch (saveError) {
      setError(saveError instanceof Error ? saveError.message : String(saveError));
    }
  };

  return (
    <form className="device-metadata-editor" onSubmit={submit}>
      <label>
        <span>label</span>
        <input autoFocus maxLength={32} value={label} onChange={(event) => setLabel(event.target.value)} />
      </label>
      <label>
        <span>location</span>
        <span className="device-metadata-select">
          <select aria-label="Device location" value={locationId} onChange={(event) => {
            const nextLocationId = event.target.value;
            const nextGroups = groups.filter((group) => group.locationId === nextLocationId);
            setLocationId(nextLocationId);
            setGroupId(nextGroups[0]?.id ?? '');
          }}>
            {locations.map((location) => <option key={location.id} value={location.id}>{location.name}</option>)}
          </select>
          <ChevronDown size={12} aria-hidden="true" />
        </span>
      </label>
      <label>
        <span>group</span>
        <span className="device-metadata-select">
          <select aria-label="Device group" value={groupId} onChange={(event) => setGroupId(event.target.value)}>
            {locationGroups.map((group) => <option key={group.id} value={group.id}>{group.name}</option>)}
          </select>
          <ChevronDown size={12} aria-hidden="true" />
        </span>
      </label>
      {error ? <p className="device-metadata-error">{error}</p> : null}
      <div className="device-metadata-actions">
        <button type="button" onClick={onCancel} disabled={saving}>{cancelLabel}</button>
        <button type="submit" disabled={saving || unchanged || !normalizedLabel || !groupId}>{saving ? 'saving' : 'save'}</button>
      </div>
    </form>
  );
}
