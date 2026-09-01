import { useEffect, useRef } from 'react';
import { ChevronDown, MessageSquareText, Network, RefreshCw, Search, X } from 'lucide-react';
import type { NetworkSettings } from '../backend/api';
import type { Device, Group } from '../domain/lifx';
import type { LocationCollection } from '../domain/locationCollections';
import { devicesInLocationCollection, groupsInLocationCollection } from '../domain/locationCollections.js';
import { PowerDot } from './primitives';
import './Sidebar.css';

interface SidebarProps {
  locations: LocationCollection[];
  groups: Group[];
  devices: Device[];
  selectedLocationKey: string;
  selectedGroupId: string;
  query: string;
  refreshing: boolean;
  refreshError?: string;
  networkSettings: NetworkSettings;
  networkChanging: boolean;
  onQueryChange: (query: string) => void;
  onLocationChange: (key: string) => void;
  onGroupChange: (id: string) => void;
  onLocationPower: (locationKey: string, on: boolean) => void;
  onNetworkInterfaceChange: (name: string) => void;
  onRefreshDiscovery: () => void;
  onOpenCommands: () => void;
  commandsOpen?: boolean;
  onGroupPower: (groupId: string, on: boolean) => void;
}

export function Sidebar(props: SidebarProps) {
  const searchRef = useRef<HTMLInputElement>(null);
  const selectedLocation = props.locations.find((location) => location.key === props.selectedLocationKey);
  const groupsInLocation = groupsInLocationCollection(selectedLocation, props.groups);
  const locationDevices = devicesInLocationCollection(selectedLocation, props.groups, props.devices);
  const locationOn = locationDevices.some((device) => device.on);
  const statusText = props.refreshError
    ? props.refreshError
    : props.devices.length
      ? `${props.devices.length} LAN devices${props.refreshing ? ' · refreshing' : ''}`
      : props.refreshing
        ? 'discovering LAN devices'
        : 'no LAN devices found';

  useEffect(() => {
    searchRef.current?.focus();
  }, []);

  useEffect(() => {
    const onKeyDown = (event: KeyboardEvent) => {
      if (props.commandsOpen) return;
      const key = event.key.toLowerCase();
      const searchShortcut = key === 's' && !event.metaKey && !event.ctrlKey && !event.altKey && !event.shiftKey && !isEditableShortcutTarget(event.target);
      const findShortcut = key === 'f' && (event.metaKey || event.ctrlKey) && !event.altKey;
      if (!searchShortcut && !findShortcut) return;
      event.preventDefault();
      event.stopPropagation();
      searchRef.current?.focus();
      searchRef.current?.select();
    };
    window.addEventListener('keydown', onKeyDown);
    return () => window.removeEventListener('keydown', onKeyDown);
  }, [props.commandsOpen]);

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
          data-shortcut-target="search"
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
        <button className="command-open-button" type="button" aria-label="Text command" title="Text command" onClick={props.onOpenCommands}>
          <MessageSquareText size={12} />
        </button>
      </div>

      <div className="location-control">
        <PowerDot disabled={!props.selectedLocationKey || !locationDevices.length} on={locationOn} size={7} onChange={(next) => props.onLocationPower(props.selectedLocationKey, next)} />
        <div className="location-select-wrap">
          <select className="location-select" value={props.selectedLocationKey} onChange={(event) => props.onLocationChange(event.target.value)} aria-label="Location">
            {props.locations.map((location) => (
              <option key={location.key} value={location.key}>
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

      <div className="lan-footer">
        <div className="lan-status" data-error={props.refreshError ? 'true' : 'false'}>
          <span />
          <span>{statusText}</span>
        </div>
        <NetworkInterfaceControl settings={props.networkSettings} changing={props.networkChanging} onChange={props.onNetworkInterfaceChange} onRefresh={props.onRefreshDiscovery} compact />
      </div>
    </aside>
  );
}

function isEditableShortcutTarget(target: EventTarget | null): boolean {
  if (!(target instanceof HTMLElement)) return false;
  const tag = target.tagName.toLowerCase();
  return tag === 'input' || tag === 'textarea' || tag === 'select' || tag === 'button' || target.isContentEditable;
}

interface NetworkInterfaceControlProps {
  settings: NetworkSettings;
  changing: boolean;
  onChange: (name: string) => void;
  onRefresh?: () => void;
  compact?: boolean;
}

function selectedNetworkLabel(settings: NetworkSettings): string {
  if (!settings.selectedInterfaceName) return 'Automatic';
  const selected = settings.interfaces.find((entry) => entry.name === settings.selectedInterfaceName);
  return selected?.shortLabel ?? selected?.label ?? settings.selectedInterfaceName + ' - unavailable';
}

function selectedNetworkUnavailable(settings: NetworkSettings): boolean {
  return !!settings.selectedInterfaceName && !settings.interfaces.some((entry) => entry.name === settings.selectedInterfaceName);
}

export function NetworkInterfaceControl(props: NetworkInterfaceControlProps) {
  const selectedLabel = selectedNetworkLabel(props.settings);
  const unavailable = selectedNetworkUnavailable(props.settings);
  return (
    <div className="network-settings" data-compact={props.compact ? 'true' : 'false'} data-unavailable={unavailable ? 'true' : 'false'}>
      <div className="network-select-wrap">
        <Network className="network-select-icon" size={13} aria-hidden="true" />
        <span className="network-select-value" aria-hidden="true">{selectedLabel}</span>
        <select
          id="network-interface"
          className="network-select"
          aria-label="Network interface"
          title={`Network interface: ${selectedLabel}`}
          value={props.settings.selectedInterfaceName}
          disabled={props.changing}
          onChange={(event) => props.onChange(event.target.value)}
        >
          {unavailable ? <option value={props.settings.selectedInterfaceName}>{props.settings.selectedInterfaceName} - unavailable</option> : null}
          <option value="">Automatic</option>
          {props.settings.interfaces.map((entry) => (
            <option key={entry.name} value={entry.name}>
              {entry.label}
            </option>
          ))}
        </select>
        <ChevronDown size={13} aria-hidden="true" />
      </div>
      {props.onRefresh ? (
        <button className="network-refresh" type="button" aria-label="Refresh discovery" title="Refresh discovery" disabled={props.changing} onClick={props.onRefresh}>
          <RefreshCw size={13} />
        </button>
      ) : null}
      {props.settings.warning ? <small>{props.settings.warning}</small> : null}
    </div>
  );
}
