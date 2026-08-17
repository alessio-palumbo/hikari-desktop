import type { Device, Group, Location } from '../domain/lifx';
import { deviceColor, hsl, isLightDevice, previewLightness, previewOpacity } from '../domain/lifx';
import type { FloorPlanFloor, FloorPlanLocation, FloorPlanRoom } from '../domain/floorPlan';
import { CenterViewToggle, type CenterView } from './CenterViewToggle';
import './FloorPlan.css';

interface FloorPlanProps {
  location?: Location;
  groups: Group[];
  devices: Device[];
  layout?: FloorPlanLocation;
  selectedSerial?: string;
  selectedGroupId?: string;
  searching: boolean;
  query: string;
  view: CenterView;
  onViewChange: (view: CenterView) => void;
  onSelect: (serial: string) => void;
  onSurfaceClick: () => void;
}

export function FloorPlan({
  location,
  groups,
  devices,
  layout,
  selectedSerial,
  selectedGroupId,
  searching,
  query,
  view,
  onViewChange,
  onSelect,
  onSurfaceClick,
}: FloorPlanProps) {
  const floor = activeFloor(layout);
  const placed = new Set(Object.keys(floor?.devices ?? {}));
  const selectedGroupDevices = selectedGroupId ? new Set(devices.filter((device) => device.groupId === selectedGroupId).map((device) => device.serial)) : undefined;
  const matches = searchMatches(devices, groups, query);
  const placedDevices = floor ? devices.filter((device) => placed.has(device.serial)) : [];
  const unplacedDevices = floor ? devices.filter((device) => !placed.has(device.serial)) : devices;

  return (
    <main
      className="center-panel"
      onClick={(event) => {
        const target = event.target instanceof Element ? event.target : null;
        if (target?.closest('.floor-device-node, .floor-unplaced-device, button, select, input')) return;
        onSurfaceClick();
      }}
    >
      <div className="floor-plan-shell">
        <header className="floor-plan-header">
          <div>
            <span>{location?.name.toLowerCase() ?? 'location'}</span>
            <h1>{floor?.label.toLowerCase() ?? 'floor plan'}</h1>
          </div>
          <div className="floor-plan-actions">
            <div className="floor-plan-meta">
              <span>{devices.length} device{devices.length === 1 ? '' : 's'}</span>
              <span>{floor?.rooms.length ?? 0} room{floor?.rooms.length === 1 ? '' : 's'}</span>
            </div>
            <CenterViewToggle view={view} onChange={onViewChange} />
          </div>
        </header>

        <section className="floor-canvas" aria-label={`${floor?.label ?? 'Floor'} floor plan`}>
          {floor?.rooms.map((room) => <RoomShape key={room.id} room={room} />)}

          {placedDevices.map((device) => {
            const placement = floor?.devices[device.serial];
            if (!placement) return null;
            return (
              <DeviceNode
                key={device.serial}
                device={device}
                selected={device.serial === selectedSerial}
                dimmed={shouldDim(device, searching, matches, selectedGroupDevices)}
                x={placement.x}
                y={placement.y}
                onSelect={onSelect}
              />
            );
          })}

          {!floor?.rooms.length && !placedDevices.length ? (
            <div className="floor-empty">
              <strong>no floor layout yet</strong>
              <span>rooms and device placement will appear here</span>
            </div>
          ) : null}
        </section>

        <section className="floor-unplaced" aria-label="Unplaced devices">
          <div className="floor-section-title">
            <span>unplaced</span>
            <b>{unplacedDevices.length}</b>
          </div>
          {unplacedDevices.length ? (
            <div className="floor-unplaced-list">
              {unplacedDevices.map((device) => (
                <button
                  key={device.serial}
                  type="button"
                  className="floor-unplaced-device"
                  data-selected={device.serial === selectedSerial}
                  data-dimmed={shouldDim(device, searching, matches, selectedGroupDevices) ? 'true' : 'false'}
                  onClick={() => onSelect(device.serial)}
                >
                  <DeviceSwatch device={device} />
                  <span>{device.name}</span>
                </button>
              ))}
            </div>
          ) : (
            <p>all devices placed on this floor</p>
          )}
        </section>
      </div>
    </main>
  );
}

function activeFloor(layout?: FloorPlanLocation): FloorPlanFloor | undefined {
  if (!layout?.floors.length) return undefined;
  return layout.floors.find((floor) => floor.id === layout.activeFloorId) ?? layout.floors[0];
}

function RoomShape({ room }: { room: FloorPlanRoom }) {
  return (
    <div
      className="floor-room"
      data-type={room.type ?? 'other'}
      style={{
        clipPath: `polygon(${room.points.map((point) => `${point.x * 100}% ${point.y * 100}%`).join(', ')})`,
      }}
    >
      <span style={roomLabelPosition(room)}>{room.label}</span>
    </div>
  );
}

function DeviceNode({ device, selected, dimmed, x, y, onSelect }: { device: Device; selected: boolean; dimmed: boolean; x: number; y: number; onSelect: (serial: string) => void }) {
  return (
    <button
      type="button"
      className="floor-device-node"
      data-selected={selected}
      data-offline={!device.online ? 'true' : 'false'}
      data-dimmed={dimmed ? 'true' : 'false'}
      style={{ left: `${x * 100}%`, top: `${y * 100}%` }}
      onClick={() => onSelect(device.serial)}
    >
      <DeviceSwatch device={device} />
      <span>{device.name}</span>
    </button>
  );
}

function DeviceSwatch({ device }: { device: Device }) {
  if (!isLightDevice(device)) return <i className="floor-device-swatch" data-switch="true" />;
  const color = deviceColor(device);
  return <i className="floor-device-swatch" style={{ background: hsl(color, previewLightness(color, device.brightness, device.on)), opacity: previewOpacity(device.on) }} />;
}

function roomLabelPosition(room: FloorPlanRoom): { left: string; top: string } {
  const x = room.points.reduce((sum, point) => sum + point.x, 0) / room.points.length;
  const y = room.points.reduce((sum, point) => sum + point.y, 0) / room.points.length;
  return { left: `${x * 100}%`, top: `${y * 100}%` };
}

function searchMatches(devices: Device[], groups: Group[], query: string): Set<string> {
  const q = query.trim().toLowerCase();
  if (!q) return new Set(devices.map((device) => device.serial));
  return new Set(
    devices
      .filter((device) => {
        const group = groups.find((entry) => entry.id === device.groupId);
        return [device.name, device.serial, device.model, group?.name ?? ''].some((value) => value.toLowerCase().includes(q));
      })
      .map((device) => device.serial),
  );
}

function shouldDim(device: Device, searching: boolean, matches: Set<string>, selectedGroupDevices?: Set<string>): boolean {
  if (searching) return !matches.has(device.serial);
  return !!selectedGroupDevices && !selectedGroupDevices.has(device.serial);
}
