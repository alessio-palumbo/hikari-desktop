import { useRef } from 'react';
import type { Device, Group, Location } from '../domain/lifx';
import { deviceColor, hsl, isLightDevice, previewLightness, previewOpacity } from '../domain/lifx';
import { FLOOR_PLAN_ROOM_TYPES, type FloorPlanDevicePlacement, type FloorPlanFloor, type FloorPlanLocation, type FloorPlanPoint, type FloorPlanRoom, type FloorPlanRoomType } from '../domain/floorPlan';
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
  editing: boolean;
  onViewChange: (view: CenterView) => void;
  onEditingChange: (editing: boolean) => void;
  onAddRoom: () => void;
  onAddFloor: () => void;
  onSelectFloor: (floorId: string) => void;
  onRenameFloor: (floorId: string, label: string) => void;
  onRemoveFloor: (floorId: string) => void;
  onUpdateRoom: (floorId: string, roomId: string, patch: { label?: string; type?: FloorPlanRoomType }) => void;
  onRemoveRoom: (floorId: string, roomId: string) => void;
  onPlaceDevice: (serial: string, placement: FloorPlanDevicePlacement) => void;
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
  editing,
  onViewChange,
  onEditingChange,
  onAddRoom,
  onAddFloor,
  onSelectFloor,
  onRenameFloor,
  onRemoveFloor,
  onUpdateRoom,
  onRemoveRoom,
  onPlaceDevice,
  onSelect,
  onSurfaceClick,
}: FloorPlanProps) {
  const canvasRef = useRef<HTMLElement | null>(null);
  const floor = activeFloor(layout);
  const placed = new Set(Object.keys(floor?.devices ?? {}));
  const selectedGroupDevices = selectedGroupId ? new Set(devices.filter((device) => device.groupId === selectedGroupId).map((device) => device.serial)) : undefined;
  const matches = searchMatches(devices, groups, query);
  const placedDevices = floor ? devices.filter((device) => placed.has(device.serial)) : [];
  const unplacedDevices = floor ? devices.filter((device) => !placed.has(device.serial)) : devices;
  const placeDevice = (serial: string, point: FloorPlanPoint) => {
    const roomId = floor ? roomAtPoint(floor.rooms, point)?.id : undefined;
    onPlaceDevice(serial, { ...point, ...(roomId ? { roomId } : {}) });
  };
  const canvasPoint = (clientX: number, clientY: number): FloorPlanPoint | undefined => {
    const rect = canvasRef.current?.getBoundingClientRect();
    if (!rect || rect.width <= 0 || rect.height <= 0) return undefined;
    return {
      x: Math.max(0, Math.min(1, (clientX - rect.left) / rect.width)),
      y: Math.max(0, Math.min(1, (clientY - rect.top) / rect.height)),
    };
  };

  return (
    <main
      className="center-panel"
      onClick={(event) => {
        const target = event.target instanceof Element ? event.target : null;
        if (target?.closest('.floor-device-node, .floor-unplaced-device, .floor-tools, button, select, input')) return;
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
            <div className="floor-tools">
              {editing && layout ? (
                <select value={floor?.id ?? ''} aria-label="Floor" onChange={(event) => onSelectFloor(event.target.value)}>
                  {layout.floors.map((entry) => (
                    <option key={entry.id} value={entry.id}>{entry.label}</option>
                  ))}
                </select>
              ) : null}
              {editing && floor ? <input value={floor.label} aria-label="Floor label" onChange={(event) => onRenameFloor(floor.id, event.target.value)} /> : null}
              {editing ? <button type="button" onClick={onAddFloor}>add floor</button> : null}
              {editing ? <button type="button" onClick={onAddRoom}>add room</button> : null}
              {editing && floor && (layout?.floors.length ?? 0) > 1 ? <button type="button" onClick={() => onRemoveFloor(floor.id)}>delete floor</button> : null}
              <button type="button" data-active={editing ? 'true' : 'false'} onClick={() => onEditingChange(!editing)}>
                edit
              </button>
            </div>
            <CenterViewToggle view={view} onChange={onViewChange} />
          </div>
        </header>

        <section
          ref={canvasRef}
          className="floor-canvas"
          data-editing={editing ? 'true' : 'false'}
          aria-label={`${floor?.label ?? 'Floor'} floor plan`}
          onDragOver={(event) => {
            if (!editing) return;
            event.preventDefault();
          }}
          onDrop={(event) => {
            if (!editing) return;
            const serial = event.dataTransfer.getData('application/x-hikari-device');
            const point = canvasPoint(event.clientX, event.clientY);
            if (!serial || !point) return;
            event.preventDefault();
            placeDevice(serial, point);
          }}
        >
          {floor?.rooms.map((room) => (
            <RoomShape
              key={room.id}
              room={room}
              floorId={floor.id}
              editing={editing}
              onUpdateRoom={onUpdateRoom}
              onRemoveRoom={onRemoveRoom}
            />
          ))}

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
                editing={editing}
                canvasPoint={canvasPoint}
                onMove={placeDevice}
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
                  draggable={editing}
                  onDragStart={(event) => {
                    if (!editing) return;
                    event.dataTransfer.setData('application/x-hikari-device', device.serial);
                    event.dataTransfer.effectAllowed = 'move';
                  }}
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

function RoomShape({
  room,
  floorId,
  editing,
  onUpdateRoom,
  onRemoveRoom,
}: {
  room: FloorPlanRoom;
  floorId: string;
  editing: boolean;
  onUpdateRoom: (floorId: string, roomId: string, patch: { label?: string; type?: FloorPlanRoomType }) => void;
  onRemoveRoom: (floorId: string, roomId: string) => void;
}) {
  return (
    <>
      <div
        className="floor-room"
        data-type={room.type ?? 'other'}
        data-editing={editing ? 'true' : 'false'}
        style={{
          clipPath: `polygon(${room.points.map((point) => `${point.x * 100}% ${point.y * 100}%`).join(', ')})`,
        }}
      >
        <span style={roomLabelPosition(room)}>{room.label}</span>
      </div>
      {editing ? (
        <div className="floor-room-editor" style={roomLabelPosition(room)}>
          <input value={room.label} aria-label={`${room.label} room label`} onChange={(event) => onUpdateRoom(floorId, room.id, { label: event.target.value })} />
          <select value={room.type ?? 'other'} aria-label={`${room.label} room type`} onChange={(event) => onUpdateRoom(floorId, room.id, { type: event.target.value as FloorPlanRoomType })}>
            {FLOOR_PLAN_ROOM_TYPES.map((type) => (
              <option key={type} value={type}>{roomTypeLabel(type)}</option>
            ))}
          </select>
          <button type="button" aria-label={`Delete ${room.label}`} onClick={() => onRemoveRoom(floorId, room.id)}>delete</button>
        </div>
      ) : null}
    </>
  );
}

function DeviceNode({
  device,
  selected,
  dimmed,
  x,
  y,
  editing,
  canvasPoint,
  onMove,
  onSelect,
}: {
  device: Device;
  selected: boolean;
  dimmed: boolean;
  x: number;
  y: number;
  editing: boolean;
  canvasPoint: (clientX: number, clientY: number) => FloorPlanPoint | undefined;
  onMove: (serial: string, point: FloorPlanPoint) => void;
  onSelect: (serial: string) => void;
}) {
  const movedRef = useRef(false);
  return (
    <button
      type="button"
      className="floor-device-node"
      data-selected={selected}
      data-offline={!device.online ? 'true' : 'false'}
      data-dimmed={dimmed ? 'true' : 'false'}
      data-editing={editing ? 'true' : 'false'}
      style={{ left: `${x * 100}%`, top: `${y * 100}%` }}
      onPointerDown={(event) => {
        movedRef.current = false;
        if (!editing) return;
        event.preventDefault();
        event.stopPropagation();
        event.currentTarget.setPointerCapture(event.pointerId);
      }}
      onPointerMove={(event) => {
        if (!editing || !event.currentTarget.hasPointerCapture(event.pointerId)) return;
        const point = canvasPoint(event.clientX, event.clientY);
        if (!point) return;
        movedRef.current = true;
        onMove(device.serial, point);
      }}
      onPointerUp={(event) => {
        if (event.currentTarget.hasPointerCapture(event.pointerId)) event.currentTarget.releasePointerCapture(event.pointerId);
      }}
      onPointerCancel={(event) => {
        if (event.currentTarget.hasPointerCapture(event.pointerId)) event.currentTarget.releasePointerCapture(event.pointerId);
      }}
      onClick={() => {
        if (movedRef.current) {
          movedRef.current = false;
          return;
        }
        onSelect(device.serial);
      }}
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

function roomTypeLabel(type: FloorPlanRoomType): string {
  return type.replace('-', ' ');
}

function roomAtPoint(rooms: FloorPlanRoom[], point: FloorPlanPoint): FloorPlanRoom | undefined {
  return rooms.find((room) => pointInPolygon(point, room.points));
}

function pointInPolygon(point: FloorPlanPoint, polygon: FloorPlanPoint[]): boolean {
  let inside = false;
  for (let current = 0, previous = polygon.length - 1; current < polygon.length; previous = current, current += 1) {
    const currentPoint = polygon[current];
    const previousPoint = polygon[previous];
    const crosses = currentPoint.y > point.y !== previousPoint.y > point.y;
    if (!crosses) continue;
    const x = ((previousPoint.x - currentPoint.x) * (point.y - currentPoint.y)) / (previousPoint.y - currentPoint.y) + currentPoint.x;
    if (point.x < x) inside = !inside;
  }
  return inside;
}
