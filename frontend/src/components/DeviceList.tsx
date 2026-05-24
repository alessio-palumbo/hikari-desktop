import type { Device, Group } from '../domain/lifx';
import { deviceTypeLabel } from '../domain/lifx';
import { DevicePreview } from './DevicePreview';
import { PowerDot, RowChevron, Slider } from './primitives';
import './DeviceList.css';

interface DeviceListProps {
  group?: Group;
  devices: Device[];
  selectedId?: string;
  searching: boolean;
  onSelect: (id: string) => void;
  onDeviceChange: (device: Device) => void;
  onMasterChange: (on: boolean, brightness?: number) => void;
}

export function DeviceList({ group, devices, selectedId, searching, onSelect, onDeviceChange, onMasterChange }: DeviceListProps) {
  const onCount = devices.filter((device) => device.on).length;
  const avgBrightness = devices.length ? devices.reduce((sum, device) => sum + device.brightness, 0) / devices.length : 0;

  return (
    <main className="center-panel">
      <div className="device-list-shell">
        <header className="group-header">
          <h1>{searching ? 'search results' : group?.name.toLowerCase() ?? 'no group'}</h1>
          <div className="group-controls">
            <PowerDot on={onCount > 0} onChange={(next) => onMasterChange(next)} />
            <Slider value={avgBrightness} onChange={(value) => onMasterChange(value > 0, value)} />
          </div>
        </header>

        <section className="device-list">
          {devices.map((device) => (
            <DeviceRow
              key={device.id}
              device={device}
              selected={device.id === selectedId}
              onSelect={onSelect}
              onChange={onDeviceChange}
            />
          ))}
          {!devices.length ? <div className="empty-list">no lights matched</div> : null}
        </section>
      </div>
    </main>
  );
}

function DeviceRow({ device, selected, onSelect, onChange }: { device: Device; selected: boolean; onSelect: (id: string) => void; onChange: (device: Device) => void }) {
  return (
    <div className="device-row" data-selected={selected} onClick={() => onSelect(device.id)}>
      <PowerDot on={device.on} onChange={(next) => onChange({ ...device, on: next })} />
      <div className="device-name">
        <strong>{device.name}</strong>
        <span>
          {deviceTypeLabel(device)} · {device.model}
        </span>
      </div>
      <div className="device-preview-cell">
        <DevicePreview device={device} />
      </div>
      <div className="row-slider" onClick={(event) => event.stopPropagation()}>
        <Slider value={device.brightness} onChange={(value) => onChange({ ...device, brightness: value, on: value > 0 })} />
      </div>
      <span className="device-brightness mono">{device.on ? `${Math.round(device.brightness * 100)}%` : 'off'}</span>
      <span className="row-chevron">
        <RowChevron />
      </span>
    </div>
  );
}
