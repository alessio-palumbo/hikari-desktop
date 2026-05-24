import { Search } from 'lucide-react';
import type { Device, Group, Location } from '../domain/lifx';
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
  onLocationPower: (locationId: string, on: boolean) => void;
  onGroupPower: (groupId: string, on: boolean) => void;
}

export function Sidebar(props: SidebarProps) {
  const groupsInLocation = props.groups.filter((group) => group.locationId === props.selectedLocationId);
  const selectedLocation = props.locations.find((location) => location.id === props.selectedLocationId);
  const selectedLocationGroupIds = new Set(groupsInLocation.map((group) => group.id));
  const locationDevices = props.devices.filter((device) => selectedLocationGroupIds.has(device.groupId));
  const locationOn = locationDevices.some((device) => device.on);

  return (
    <aside className="left-panel sidebar">
      <label className="sidebar-search">
        <Search size={13} />
        <input value={props.query} onChange={(event) => props.onQueryChange(event.target.value)} placeholder="Search..." />
      </label>

      <div className="location-control">
        <PowerDot on={locationOn} size={7} onChange={(next) => props.onLocationPower(props.selectedLocationId, next)} />
        <select className="location-select" value={props.selectedLocationId} onChange={(event) => props.onLocationChange(event.target.value)}>
          {props.locations.map((location) => (
            <option key={location.id} value={location.id}>
              {location.name}
            </option>
          ))}
        </select>
      </div>

      <nav className="group-list" aria-label={`${selectedLocation?.name ?? 'Location'} groups`}>
        {groupsInLocation.map((group) => {
          const groupDevices = props.devices.filter((device) => device.groupId === group.id);
          const on = groupDevices.some((device) => device.on);
          return (
            <button
              key={group.id}
              className="group-item"
              data-active={group.id === props.selectedGroupId}
              onClick={() => props.onGroupChange(group.id)}
            >
              <PowerDot on={on} size={5} onChange={(next) => props.onGroupPower(group.id, next)} />
              <span>{group.name.toLowerCase()}</span>
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
