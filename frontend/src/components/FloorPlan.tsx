import { useEffect, useRef, useState } from 'react';
import { ChevronDown, Trash2 } from 'lucide-react';
import type { Device, Group, Location } from '../domain/lifx';
import { deviceColor, hsl, isLightDevice, previewLightness, previewOpacity } from '../domain/lifx';
import { FLOOR_PLAN_ROOM_TYPES, type FloorPlanDevicePlacement, type FloorPlanFloor, type FloorPlanLocation, type FloorPlanPoint, type FloorPlanRoom, type FloorPlanRoomPatch, type FloorPlanRoomType } from '../domain/floorPlan';
import { CenterViewToggle, type CenterView } from './CenterViewToggle';
import './FloorPlan.css';

interface FloorPlanProps {
  location?: Location;
  groups: Group[];
  devices: Device[];
  layout?: FloorPlanLocation;
  selectedSerial?: string;
  selectedGroupId?: string;
  selectedRoomId?: string;
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
  onUpdateRoom: (floorId: string, roomId: string, patch: FloorPlanRoomPatch) => void;
  onRemoveRoom: (floorId: string, roomId: string) => void;
  onPlaceDevice: (serial: string, placement: FloorPlanDevicePlacement) => void;
  onSelect: (serial: string) => void;
  onRoomSelect: (floorId: string, roomId: string) => void;
  onSurfaceClick: () => void;
}

export function FloorPlan({
  location,
  groups,
  devices,
  layout,
  selectedSerial,
  selectedGroupId,
  selectedRoomId,
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
  onRoomSelect,
  onSurfaceClick,
}: FloorPlanProps) {
  const canvasRef = useRef<HTMLElement | null>(null);
  const floor = activeFloor(layout);
  const [editedRoomId, setEditedRoomId] = useState<string | undefined>();
  const editedRoom = floor?.rooms.find((room) => room.id === editedRoomId);
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

  useEffect(() => {
    if (!editing || !editedRoomId || floor?.rooms.some((room) => room.id === editedRoomId)) return;
    setEditedRoomId(undefined);
  }, [editing, floor?.id, floor?.rooms, editedRoomId]);

  return (
    <main
      className="center-panel"
      onClick={(event) => {
        const target = event.target instanceof Element ? event.target : null;
        if (target?.closest('.floor-device-node, .floor-unplaced-device, .floor-tools, .floor-room, .floor-room-editor, button, select, input')) return;
        setEditedRoomId(undefined);
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
              <span>{floor?.rooms.length ?? 0} room{floor?.rooms.length === 1 ? '' : 's'}</span>
            </div>
            <div className="floor-tools">
              {editing && layout ? (
                <label className="floor-select-wrap">
                  <select value={floor?.id ?? ''} aria-label="Floor" onChange={(event) => onSelectFloor(event.target.value)}>
                    {layout.floors.map((entry) => (
                      <option key={entry.id} value={entry.id}>{entry.label}</option>
                    ))}
                  </select>
                  <ChevronDown size={12} aria-hidden="true" />
                </label>
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

        {editing && floor ? (
          <RoomEditor
            floorId={floor.id}
            room={editedRoom}
            onUpdateRoom={onUpdateRoom}
            onRemoveRoom={(floorId, roomId) => {
              onRemoveRoom(floorId, roomId);
              setEditedRoomId(undefined);
            }}
          />
        ) : null}

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
              selected={editing ? room.id === editedRoomId : room.id === selectedRoomId}
              canvasPoint={canvasPoint}
              floorDevices={floor.devices}
              onSelectRoom={(roomId) => {
                if (editing) setEditedRoomId(roomId);
                else onRoomSelect(floor.id, roomId);
              }}
              onUpdateRoom={onUpdateRoom}
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

        <section className="floor-unplaced" aria-label="Unassigned devices">
          <div className="floor-section-title">
            <span>unassigned</span>
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
  selected,
  canvasPoint,
  floorDevices,
  onSelectRoom,
  onUpdateRoom,
}: {
  room: FloorPlanRoom;
  floorId: string;
  editing: boolean;
  selected: boolean;
  canvasPoint: (clientX: number, clientY: number) => FloorPlanPoint | undefined;
  floorDevices: Record<string, FloorPlanDevicePlacement>;
  onSelectRoom: (roomId: string) => void;
  onUpdateRoom: (floorId: string, roomId: string, patch: FloorPlanRoomPatch) => void;
}) {
  const dragRef = useRef<{
    offset: FloorPlanPoint;
    room: FloorPlanRoom;
    devices: Record<string, FloorPlanDevicePlacement>;
  } | undefined>(undefined);
  const movedRef = useRef(false);

  return (
    <div
      className="floor-room"
      data-type={room.type ?? 'other'}
      data-editing={editing ? 'true' : 'false'}
      data-selected={selected ? 'true' : 'false'}
      style={{
        clipPath: `polygon(${room.points.map((point) => `${point.x * 100}% ${point.y * 100}%`).join(', ')})`,
      }}
      onPointerDown={(event) => {
        movedRef.current = false;
        if (!editing) return;
        const point = canvasPoint(event.clientX, event.clientY);
        if (!point) return;
        event.preventDefault();
        event.stopPropagation();
        const center = roomCenter(room);
        dragRef.current = {
          offset: { x: point.x - center.x, y: point.y - center.y },
          room,
          devices: assignedRoomDevices(floorDevices, room.id),
        };
        event.currentTarget.setPointerCapture(event.pointerId);
      }}
      onPointerMove={(event) => {
        if (!editing || !event.currentTarget.hasPointerCapture(event.pointerId)) return;
        const point = canvasPoint(event.clientX, event.clientY);
        const drag = dragRef.current;
        if (!point || !drag) return;
        movedRef.current = true;
        const points = moveRoomTo(drag.room, { x: point.x - drag.offset.x, y: point.y - drag.offset.y });
        const delta = roomMoveDelta(drag.room, points);
        onUpdateRoom(floorId, room.id, { points, devices: moveAssignedDevices(drag.devices, delta) });
      }}
      onPointerUp={(event) => {
        dragRef.current = undefined;
        if (event.currentTarget.hasPointerCapture(event.pointerId)) event.currentTarget.releasePointerCapture(event.pointerId);
      }}
      onPointerCancel={(event) => {
        dragRef.current = undefined;
        if (event.currentTarget.hasPointerCapture(event.pointerId)) event.currentTarget.releasePointerCapture(event.pointerId);
      }}
      onClick={(event) => {
        event.stopPropagation();
        if (movedRef.current) {
          movedRef.current = false;
          return;
        }
        onSelectRoom(room.id);
      }}
    >
      <span style={roomLabelPosition(room)}>{room.label}</span>
    </div>
  );
}

function RoomEditor({
  floorId,
  room,
  onUpdateRoom,
  onRemoveRoom,
}: {
  floorId: string;
  room?: FloorPlanRoom;
  onUpdateRoom: (floorId: string, roomId: string, patch: FloorPlanRoomPatch) => void;
  onRemoveRoom: (floorId: string, roomId: string) => void;
}) {
  const [label, setLabel] = useState(room?.label ?? '');

  useEffect(() => setLabel(room?.label ?? ''), [room?.id, room?.label]);

  const commitLabel = () => {
    if (!room) return;
    const next = label.trim();
    setLabel(next || room.label);
    if (next && next !== room.label) onUpdateRoom(floorId, room.id, { label: next });
  };

  return (
    <div className="floor-room-editor" data-empty={!room ? 'true' : 'false'} onClick={(event) => event.stopPropagation()} onKeyDown={(event) => event.stopPropagation()}>
      {room ? (
        <>
          <span>room</span>
          <input
            value={label}
            aria-label={`${room.label} room label`}
            onChange={(event) => setLabel(event.target.value)}
            onBlur={commitLabel}
            onKeyDown={(event) => {
              event.stopPropagation();
              if (event.key !== 'Enter') return;
              event.preventDefault();
              commitLabel();
              event.currentTarget.blur();
            }}
          />
          <label className="floor-select-wrap">
            <select value={room.type ?? 'other'} aria-label={`${room.label} room type`} onChange={(event) => onUpdateRoom(floorId, room.id, { type: event.target.value as FloorPlanRoomType })}>
              {FLOOR_PLAN_ROOM_TYPES.map((type) => (
                <option key={type} value={type}>{roomTypeLabel(type)}</option>
              ))}
            </select>
            <ChevronDown size={12} aria-hidden="true" />
          </label>
          <button type="button" className="floor-icon-button" aria-label={`Delete ${room.label}`} onClick={() => onRemoveRoom(floorId, room.id)}>
            <Trash2 size={13} strokeWidth={1.8} aria-hidden="true" />
          </button>
        </>
      ) : (
        <span>select a room to edit label and type</span>
      )}
    </div>
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
  const center = roomCenter(room);
  return { left: `${center.x * 100}%`, top: `${center.y * 100}%` };
}

function roomCenter(room: FloorPlanRoom): FloorPlanPoint {
  const x = room.points.reduce((sum, point) => sum + point.x, 0) / room.points.length;
  const y = room.points.reduce((sum, point) => sum + point.y, 0) / room.points.length;
  return { x, y };
}

function moveRoomTo(room: FloorPlanRoom, center: FloorPlanPoint): FloorPlanPoint[] {
  const current = roomCenter(room);
  const bounds = roomBounds(room.points);
  let dx = center.x - current.x;
  let dy = center.y - current.y;
  dx = Math.max(-bounds.left, Math.min(1 - bounds.right, dx));
  dy = Math.max(-bounds.top, Math.min(1 - bounds.bottom, dy));
  return room.points.map((point) => ({ x: point.x + dx, y: point.y + dy }));
}

function roomMoveDelta(room: FloorPlanRoom, points: FloorPlanPoint[]): FloorPlanPoint {
  const before = roomCenter(room);
  const after = pointsCenter(points);
  return { x: after.x - before.x, y: after.y - before.y };
}

function assignedRoomDevices(devices: Record<string, FloorPlanDevicePlacement>, roomId: string): Record<string, FloorPlanDevicePlacement> {
  return Object.fromEntries(Object.entries(devices).filter(([, placement]) => placement.roomId === roomId));
}

function moveAssignedDevices(devices: Record<string, FloorPlanDevicePlacement>, delta: FloorPlanPoint): Record<string, FloorPlanDevicePlacement> {
  return Object.fromEntries(
    Object.entries(devices).map(([serial, placement]) => [
      serial,
      { ...placement, x: clampUnit(placement.x + delta.x), y: clampUnit(placement.y + delta.y) },
    ]),
  );
}

function roomBounds(points: FloorPlanPoint[]) {
  return points.reduce(
    (bounds, point) => ({
      left: Math.min(bounds.left, point.x),
      right: Math.max(bounds.right, point.x),
      top: Math.min(bounds.top, point.y),
      bottom: Math.max(bounds.bottom, point.y),
    }),
    { left: 1, right: 0, top: 1, bottom: 0 },
  );
}

function pointsCenter(points: FloorPlanPoint[]): FloorPlanPoint {
  return {
    x: points.reduce((sum, point) => sum + point.x, 0) / points.length,
    y: points.reduce((sum, point) => sum + point.y, 0) / points.length,
  };
}

function clampUnit(value: number): number {
  return Math.max(0, Math.min(1, value));
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
