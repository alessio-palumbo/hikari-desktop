import { useEffect, useRef } from 'react';
import { ChevronDown, Search, X } from 'lucide-react';
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
  refreshing: boolean;
  refreshError?: string;
  onQueryChange: (query: string) => void;
  onLocationChange: (id: string) => void;
  onGroupChange: (id: string) => void;
  onLocationPower: (locationId: string, on: boolean) => void;
  onGroupPower: (groupId: string, on: boolean) => void;
}

export function Sidebar(props: SidebarProps) {
  const searchRef = useRef<HTMLInputElement>(null);
  const groupsInLocation = props.groups.filter((group) => group.locationId === props.selectedLocationId);
  const selectedLocation = props.locations.find((location) => location.id === props.selectedLocationId);
  const selectedLocationGroupIds = new Set(groupsInLocation.map((group) => group.id));
  const locationDevices = props.devices.filter((device) => selectedLocationGroupIds.has(device.groupId));
  const locationOn = locationDevices.some((device) => device.on);
  const statusText = props.refreshError
    ? props.refreshError
    : props.devices.length
      ? `${props.devices.length} LAN lights${props.refreshing ? ' · refreshing' : ''}`
      : props.refreshing
        ? 'discovering LAN devices'
        : 'no LAN devices found';

  useEffect(() => {
    searchRef.current?.focus();
  }, []);

  useEffect(() => {
    const onKeyDown = (event: KeyboardEvent) => {
      if (event.key.toLowerCase() !== 'f' || (!event.metaKey && !event.ctrlKey) || event.altKey) return;
      event.preventDefault();
      event.stopPropagation();
      searchRef.current?.focus();
      searchRef.current?.select();
    };
    window.addEventListener('keydown', onKeyDown);
    return () => window.removeEventListener('keydown', onKeyDown);
  }, []);

  return (
    <aside className="left-panel sidebar">
      <div className="sidebar-search">
        <Search size={13} />
        <input
          ref={searchRef}
          value={props.query}
          autoComplete="off"
          autoCorrect="off"
          spellCheck={false}
          onChange={(event) => props.onQueryChange(event.target.value)}
          onKeyDown={(event) => {
            if (event.key !== 'Escape') return;
            event.preventDefault();
            event.stopPropagation();
            if (props.query) props.onQueryChange('');
            else event.currentTarget.blur();
          }}
          placeholder="Search..."
        />
        {props.query ? (
          <button type="button" aria-label="Clear search" onClick={() => props.onQueryChange('')}>
            <X size={12} />
          </button>
        ) : null}
      </div>

      <div className="location-control">
        <PowerDot disabled={!props.selectedLocationId || !locationDevices.length} on={locationOn} size={7} onChange={(next) => props.onLocationPower(props.selectedLocationId, next)} />
        <div className="location-select-wrap">
          <select className="location-select" value={props.selectedLocationId} onChange={(event) => props.onLocationChange(event.target.value)} aria-label="Location">
            {props.locations.map((location) => (
              <option key={location.id} value={location.id}>
                {location.name}
              </option>
            ))}
          </select>
          <ChevronDown size={13} aria-hidden="true" />
        </div>
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

      <div className="lan-status" data-error={props.refreshError ? 'true' : 'false'}>
        <span />
        <span>{statusText}</span>
      </div>
    </aside>
  );
}
