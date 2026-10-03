import { useMemo, useState } from 'react';
import { ArrowDown, ArrowUp, ChevronRight } from 'lucide-react';
import { deviceSignal, deviceUptime, healthDevices, lastResponseAge, signalQuality, type HealthSort } from '../domain/diagnostics';
import type { DeviceSnapshot } from '../domain/lifx';
import { CenterViewToggle, type CenterView } from './CenterViewToggle';
import './DeviceHealth.css';

interface DeviceHealthProps {
  snapshot: DeviceSnapshot;
  query: string;
  selectedSerial?: string;
  view: CenterView;
  onViewChange: (view: CenterView) => void;
  onSelect: (serial: string) => void;
  onSurfaceClick: () => void;
}

const columns: { key: HealthSort; label: string }[] = [
  { key: 'name', label: 'device' },
  { key: 'signal', label: 'signal' },
  { key: 'lastSeen', label: 'last response' },
  { key: 'uptime', label: 'uptime' },
];

export function DeviceHealth(props: DeviceHealthProps) {
  const [sort, setSort] = useState<HealthSort>('name');
  const [descending, setDescending] = useState(false);
  const devices = useMemo(() => healthDevices(props.snapshot, props.query, sort, descending), [props.snapshot, props.query, sort, descending]);
  const groups = new Map(props.snapshot.groups.map((group) => [group.id, group]));
  const locations = new Map(props.snapshot.locations.map((location) => [location.id, location.name]));
  const online = props.snapshot.devices.filter((device) => device.online).length;
  const nowMs = Date.now();
  return (
    <main className="center-panel" onClick={(event) => {
      if (!(event.target instanceof Element) || !event.target.closest('button, tr[data-device]')) props.onSurfaceClick();
    }}>
      <div className="device-health-shell">
        <header className="device-health-header">
          <div className="device-health-title-row">
            <h1>stats</h1>
            <CenterViewToggle view={props.view} onChange={props.onViewChange} />
          </div>
          <p title={`${online} online, ${props.snapshot.devices.length - online} offline`}>{props.snapshot.devices.length} devices</p>
        </header>
        <div className="device-health-scroll">
          <table className="device-health-table">
            <colgroup><col className="device-health-device-column" /><col /><col /><col /></colgroup>
            <thead><tr>
              {columns.map((column) => <th key={column.key} scope="col" aria-sort={sort === column.key ? descending ? 'descending' : 'ascending' : 'none'}>
                <button type="button" onClick={() => {
                  if (sort === column.key) setDescending(!descending);
                  else { setSort(column.key); setDescending(false); }
                }} title={column.key === 'lastSeen' ? 'Time since Hikari last received a LAN response from this device. This is response age, not network latency.' : undefined}>
                  {column.label}<span className="device-health-sort-icon" aria-hidden="true">{sort === column.key ? descending ? <ArrowDown size={11} /> : <ArrowUp size={11} /> : null}</span>
                </button>
              </th>)}
            </tr></thead>
            <tbody>{devices.map((device) => {
              const group = groups.get(device.groupId);
              return <tr key={device.serial} data-device data-selected={props.selectedSerial === device.serial} onClick={() => props.onSelect(device.serial)}>
                <td><button className="device-health-name" type="button" onClick={(event) => { event.stopPropagation(); props.onSelect(device.serial); }}>
                  <span><strong>{device.name}{!device.online ? <span className="device-health-offline">offline</span> : null}</strong><small>{[locations.get(group?.locationId ?? ''), group?.name].filter(Boolean).join(' / ') || device.model}</small></span>
                  <ChevronRight size={13} />
                </button></td>
                <td className="device-health-signal" data-quality={signalQuality(device)} title={device.online ? device.rssiText : undefined}>{deviceSignal(device)}</td>
                <td title={device.lastSeenAtMs ? `Last LAN response: ${new Date(device.lastSeenAtMs).toLocaleString()}` : 'No LAN response timestamp available'}>{lastResponseAge(device, nowMs)}</td>
                <td>{deviceUptime(device, nowMs)}</td>
              </tr>;
            })}</tbody>
          </table>
          {!devices.length ? <p className="device-health-empty">no devices matched</p> : null}
        </div>
      </div>
    </main>
  );
}
