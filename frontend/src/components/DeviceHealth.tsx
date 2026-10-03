import { useMemo, useState } from 'react';
import { Activity, ArrowDown, ArrowUp, ChevronRight, LoaderCircle } from 'lucide-react';
import { pingDevice, type DevicePingResult } from '../backend/api';
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
  const [pinging, setPinging] = useState<string>();
  const [pings, setPings] = useState<Record<string, DevicePingResult>>({});
  const [pingErrors, setPingErrors] = useState<Record<string, string>>({});
  async function runPing(serial: string) {
    if (pinging) return;
    setPinging(serial);
    setPingErrors((current) => ({ ...current, [serial]: '' }));
    try {
      const result = await pingDevice(serial);
      setPings((current) => ({ ...current, [serial]: result }));
    } catch (error) {
      setPingErrors((current) => ({ ...current, [serial]: String(error instanceof Error ? error.message : error) }));
    } finally { setPinging(undefined); }
  }
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
            <colgroup><col className="device-health-device-column" /><col /><col /><col /><col className="device-health-ping-column" /></colgroup>
            <thead><tr>
              {columns.map((column) => <th key={column.key} scope="col" aria-sort={sort === column.key ? descending ? 'descending' : 'ascending' : 'none'}>
                <button type="button" onClick={() => {
                  if (sort === column.key) setDescending(!descending);
                  else { setSort(column.key); setDescending(false); }
                }} title={column.key === 'lastSeen' ? 'Time since Hikari last received a LAN response from this device. This is response age, not network latency.' : undefined}>
                  {column.label}<span className="device-health-sort-icon" aria-hidden="true">{sort === column.key ? descending ? <ArrowDown size={11} /> : <ArrowUp size={11} /> : null}</span>
                </button>
              </th>)}
              <th scope="col" title="On-demand LIFX echo round-trip time: five samples, each with a one-second timeout. Does not change device state.">ping</th>
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
                <td><div className="device-health-ping">
                  <span className="device-health-ping-result" title={pingErrors[device.serial] || (pings[device.serial] ? `Measured ${new Date(pings[device.serial].measuredAtMs).toLocaleTimeString()}. Min / max shown below median; ${pings[device.serial].received} of ${pings[device.serial].samples} replies.` : undefined)}>
                    {pingErrors[device.serial] ? <span role="status">failed</span> : pinging === device.serial ? 'checking' : pings[device.serial] ? <>
                      {pings[device.serial].received ? `${pings[device.serial].medianMs.toFixed(1)} ms` : 'no reply'}
                      <small>{pings[device.serial].received ? `${pings[device.serial].minMs.toFixed(1)} / ${pings[device.serial].maxMs.toFixed(1)} ms` : ''}{pings[device.serial].timeouts ? ` · ${pings[device.serial].timeouts} timeouts` : ''}</small>
                    </> : '—'}
                  </span>
                  <button className="device-health-ping-button" type="button" disabled={!device.online || Boolean(pinging)} title="Ping device (five samples)" aria-label={`Ping ${device.name}`} onClick={(event) => { event.stopPropagation(); void runPing(device.serial); }}>
                    {pinging === device.serial ? <LoaderCircle size={14} className="device-health-ping-spinner" /> : <Activity size={14} />}
                  </button>
                </div></td>
              </tr>;
            })}</tbody>
          </table>
          {!devices.length ? <p className="device-health-empty">no devices matched</p> : null}
        </div>
      </div>
    </main>
  );
}
