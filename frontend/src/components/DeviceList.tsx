import type { Device, Group } from '../domain/lifx';
import { deviceTypeLabel } from '../domain/lifx';
import { DevicePreview } from './DevicePreview';
import { PowerDot, RowChevron, Slider } from './primitives';
import './DeviceList.css';

interface DeviceListProps {
  group?: Group;
  groups: Group[];
  devices: Device[];
  selectedSerial?: string;
  searching: boolean;
  refreshing: boolean;
  deviceStatus: Record<string, { loading?: boolean; error?: string }>;
  onRefresh: () => void;
  onSelect: (serial: string) => void;
  onDeviceChange: (device: Device) => void;
  onMasterChange: (on: boolean, brightness?: number) => void;
}

export function DeviceList({ group, groups, devices, selectedSerial, searching, refreshing, deviceStatus, onRefresh, onSelect, onDeviceChange, onMasterChange }: DeviceListProps) {
  const onCount = devices.filter((device) => device.on).length;
  const avgBrightness = devices.length ? devices.reduce((sum, device) => sum + device.brightness, 0) / devices.length : 0;
  const searchSections = groups
    .map((entry) => ({ group: entry, devices: devices.filter((device) => device.groupId === entry.id) }))
    .filter((section) => section.devices.length > 0);

  return (
    <main className="center-panel">
      <div className="device-list-shell">
        {!searching ? (
          <header className="group-header">
            <div className="group-title-row">
              <h1>{group?.name.toLowerCase() ?? 'no group'}</h1>
              <button className="refresh-button" disabled={refreshing} onClick={onRefresh}>
                {refreshing ? 'refreshing' : 'refresh'}
              </button>
            </div>
            <div className="group-controls">
              <PowerDot on={onCount > 0} onChange={(next) => onMasterChange(next)} />
              <Slider value={avgBrightness} onChange={(value) => onMasterChange(value > 0, value)} />
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
                      selected={device.serial === selectedSerial}
                      onSelect={onSelect}
                      onChange={onDeviceChange}
                    />
                  ))}
                </div>
              </div>
            ))}
            {!devices.length ? <div className="empty-list">no lights matched</div> : null}
          </section>
        ) : (
          <section className="device-list">
            {devices.map((device) => (
              <DeviceRow
                key={device.serial}
                device={device}
                status={deviceStatus[device.serial]}
                selected={device.serial === selectedSerial}
                onSelect={onSelect}
                onChange={onDeviceChange}
              />
            ))}
            {!devices.length ? <div className="empty-list">{refreshing ? 'discovering LAN devices' : group ? 'no LAN lights in this group' : 'no LAN devices found'}</div> : null}
          </section>
        )}
      </div>
    </main>
  );
}

function DeviceRow({
  device,
  status,
  selected,
  onSelect,
  onChange,
}: {
  device: Device;
  status?: { loading?: boolean; error?: string };
  selected: boolean;
  onSelect: (serial: string) => void;
  onChange: (device: Device) => void;
}) {
  const disabled = status?.loading || !device.online;
  return (
    <div className="device-row" data-selected={selected} data-offline={!device.online ? 'true' : 'false'} onClick={() => onSelect(device.serial)}>
      <PowerDot on={device.on} disabled={disabled} onChange={(next) => onChange({ ...device, on: next })} />
      <div className="device-name">
        <strong>{device.name}</strong>
        <span>
          {status?.error ? status.error : !device.online ? 'offline' : `${deviceTypeLabel(device)} · ${device.model}`}
        </span>
      </div>
      <div className="device-preview-cell">
        <DevicePreview device={device} />
      </div>
      <div className="row-slider" onClick={(event) => event.stopPropagation()}>
        <Slider disabled={disabled} value={device.brightness} onChange={(value) => onChange({ ...device, brightness: value, on: value > 0 })} />
      </div>
      <span className="device-brightness mono">{status?.loading ? '...' : device.on ? `${Math.round(device.brightness * 100)}%` : 'off'}</span>
      <span className="row-chevron">
        <RowChevron />
      </span>
    </div>
  );
}
