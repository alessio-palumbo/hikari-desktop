import { Search } from 'lucide-react';
import type { Device, Group, Location } from '../domain/lifx';
import { deviceColor } from '../domain/lifx';
import { PowerDot } from './primitives';
import './Sidebar.css';

interface SidebarProps {
  locations: Location[];
  groups: Group[];
  devices: Device[];
  selectedLocationId: string;
  selectedGroupId: string;
  query: string;
  onQueryChange: (query: string) => void;
  onLocationChange: (id: string) => void;
  onGroupChange: (id: string) => void;
  onGroupPower: (groupId: string, on: boolean) => void;
}

export function Sidebar(props: SidebarProps) {
  const groupsInLocation = props.groups.filter((group) => group.locationId === props.selectedLocationId);
  const selectedLocation = props.locations.find((location) => location.id === props.selectedLocationId);

  return (
    <aside className="left-panel sidebar">
      <label className="sidebar-search">
        <Search size={13} />
        <input value={props.query} onChange={(event) => props.onQueryChange(event.target.value)} placeholder="Search..." />
      </label>

      <select className="location-select" value={props.selectedLocationId} onChange={(event) => props.onLocationChange(event.target.value)}>
        {props.locations.map((location) => (
          <option key={location.id} value={location.id}>
            {location.name}
          </option>
        ))}
      </select>

      <nav className="group-list" aria-label={`${selectedLocation?.name ?? 'Location'} groups`}>
        {groupsInLocation.map((group) => {
          const groupDevices = props.devices.filter((device) => device.groupId === group.id);
          const on = groupDevices.some((device) => device.on);
          const tint = groupDevices.find((device) => device.on);
          return (
            <button
              key={group.id}
              className="group-item"
              data-active={group.id === props.selectedGroupId}
              onClick={() => props.onGroupChange(group.id)}
            >
              <PowerDot on={on} size={5} onChange={(next) => props.onGroupPower(group.id, next)} />
              <span>{group.name.toLowerCase()}</span>
              {tint ? <i style={{ background: `hsl(${deviceColor(tint).h} 70% 58%)` }} /> : null}
            </button>
          );
        })}
      </nav>

      <div className="lan-status">
        <span />
        <span>{props.devices.length} mock lights · fake LAN</span>
      </div>
    </aside>
  );
}
