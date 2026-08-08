import type { DeviceEffectStatus } from '../backend/api';
import { isLightDevice, type Device, type Group } from '../domain/lifx';
import { DevicePreview } from './DevicePreview';
import { PowerDot, RowChevron, Slider } from './primitives';
import './DeviceList.css';

interface DeviceListProps {
  group?: Group;
  groups: Group[];
  devices: Device[];
  selectedSerial?: string;
  groupInspecting: boolean;
  searching: boolean;
  refreshing: boolean;
  deviceStatus: Record<string, { loading?: boolean; error?: string }>;
  deviceEffectStatus: Record<string, DeviceEffectStatus & { loading?: boolean }>;
  onSelect: (serial: string) => void;
  onGroupInspect: () => void;
  onSurfaceClick: () => void;
  onDeviceChange: (device: Device) => void;
  onMasterChange: (on: boolean, brightness?: number) => void;
}

export function DeviceList({
  group,
  groups,
  devices,
  selectedSerial,
  groupInspecting,
  searching,
  refreshing,
  deviceStatus,
  deviceEffectStatus,
  onSelect,
  onGroupInspect,
  onSurfaceClick,
  onDeviceChange,
  onMasterChange,
}: DeviceListProps) {
  const lightDevices = devices.filter(isLightDevice);
  const onCount = lightDevices.filter((device) => device.on).length;
  const avgBrightness = lightDevices.length ? lightDevices.reduce((sum, device) => sum + device.brightness, 0) / lightDevices.length : 0;
  const searchSections = groups
    .map((entry) => ({ group: entry, devices: devices.filter((device) => device.groupId === entry.id) }))
    .filter((section) => section.devices.length > 0);

  return (
    <main
      className="center-panel"
      onClick={(event) => {
        const target = event.target instanceof Element ? event.target : null;
        if (target?.closest('.device-row, .group-controls, .search-section-header, button, input')) return;
        onSurfaceClick();
      }}
    >
      <div className="device-list-shell">
        {!searching ? (
          <header className="group-header">
            <div className="group-title-row">
              <h1>{group?.name.toLowerCase() ?? 'no group'}</h1>
            </div>
            <div className="group-controls">
              <PowerDot disabled={!lightDevices.length} on={onCount > 0} onChange={(next) => onMasterChange(next)} />
              <Slider disabled={!lightDevices.length} value={avgBrightness} onChange={(value) => onMasterChange(value > 0, value)} />
              <button className="group-inspector-button" type="button" aria-label="Group controls" disabled={!group || !devices.length} data-active={groupInspecting} onClick={onGroupInspect}>
                <RowChevron />
              </button>
            </div>
          </header>
        ) : null}

        {searching ? (
          <section className="search-sections">
            {searchSections.map((section) => (
              <div className="search-section" key={section.group.id}>
                <div className="search-section-header">
                  <span>{section.group.name.toLowerCase()}</span>
                </div>
                <div className="device-list">
                  {section.devices.map((device) => (
                    <DeviceRow
                      key={device.serial}
                      device={device}
                      status={deviceStatus[device.serial]}
                      effectStatus={deviceEffectStatus[device.serial]}
                      selected={device.serial === selectedSerial}
                      onSelect={onSelect}
                      onChange={onDeviceChange}
                    />
                  ))}
                </div>
              </div>
            ))}
            {!devices.length ? <div className="empty-list">no devices matched</div> : null}
          </section>
        ) : (
          <section className="device-list">
            {devices.map((device) => (
              <DeviceRow
                key={device.serial}
                device={device}
                status={deviceStatus[device.serial]}
                effectStatus={deviceEffectStatus[device.serial]}
                selected={device.serial === selectedSerial}
                onSelect={onSelect}
                onChange={onDeviceChange}
              />
            ))}
            {!devices.length ? <div className="empty-list">{refreshing ? 'discovering LAN devices' : group ? 'no LAN devices in this group' : 'no LAN devices found'}</div> : null}
          </section>
        )}
      </div>
    </main>
  );
}

function DeviceRow({
  device,
  status,
  effectStatus,
  selected,
  onSelect,
  onChange,
}: {
  device: Device;
  status?: { loading?: boolean; error?: string };
  effectStatus?: DeviceEffectStatus & { loading?: boolean };
  selected: boolean;
  onSelect: (serial: string) => void;
  onChange: (device: Device) => void;
}) {
  const isLight = isLightDevice(device);
  const disabled = status?.loading || !device.online;
  return (
    <div className="device-row" data-selected={selected} data-offline={!device.online ? 'true' : 'false'} onClick={() => onSelect(device.serial)}>
      {isLight ? <PowerDot on={device.on} disabled={disabled} onChange={(next) => onChange({ ...device, on: next })} /> : <span className="switch-row-icon" aria-hidden="true" />}
      <div className="device-name">
        <strong>{device.name}</strong>
        {effectStatus?.running ? <EffectRunning effect={effectStatus.effect ?? 'effect'} /> : <span>{status?.error ? status.error : !device.online ? 'offline' : device.model}</span>}
      </div>
      <div className="device-preview-cell">
        <DevicePreview device={device} />
      </div>
      <div className="row-slider" onClick={(event) => event.stopPropagation()}>
        {isLight ? <Slider disabled={disabled} value={device.brightness} onChange={(value) => onChange({ ...device, brightness: value, on: value > 0 })} /> : null}
      </div>
      <span className="device-brightness mono">{deviceRowStatus(device, isLight, status)}</span>
      <span className="row-chevron">
        <RowChevron />
      </span>
    </div>
  );
}

function deviceRowStatus(device: Device, isLight: boolean, status?: { loading?: boolean; error?: string }): string {
  if (status?.loading) return "...";
  if (!isLight) return "switch";
  return device.on ? Math.round(device.brightness * 100) + "%" : "off";
}

function EffectRunning({ effect }: { effect: string }) {
  return (
    <span className="effect-running">
      <i aria-hidden="true">
        <b />
        <b />
        <b />
      </i>
      {effect}
    </span>
  );
}
