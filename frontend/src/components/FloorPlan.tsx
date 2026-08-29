import { useEffect, useRef, useState, type RefObject } from 'react';
import { ChevronDown, Plus, Trash2, X } from 'lucide-react';
import type { Device, Group, Location } from '../domain/lifx';
import { deviceColor, hsl, isLightDevice, previewLightness, previewOpacity } from '../domain/lifx';
import { FLOOR_PLAN_ROOM_TYPES, keepRoomDevicesInsideShape, roomAtPoint, roomCenter, roomInteriorPoint, type FloorPlanDevicePlacement, type FloorPlanFloor, type FloorPlanLocation, type FloorPlanPoint, type FloorPlanRoom, type FloorPlanRoomPatch, type FloorPlanRoomType } from '../domain/floorPlan';
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
  onAddRoom: () => string | undefined;
  onAddFloor: () => void;
  onSelectFloor: (floorId: string) => void;
  onRenameFloor: (floorId: string, label: string) => void;
  onRemoveFloor: (floorId: string) => void;
  onUpdateRoom: (floorId: string, roomId: string, patch: FloorPlanRoomPatch) => void;
  onRemoveRoom: (floorId: string, roomId: string) => void;
  onPlaceDevice: (serial: string, placement: FloorPlanDevicePlacement) => void;
  onRemoveDevice: (serial: string) => void;
  onSelect: (serial: string) => void;
  onDeviceChange: (device: Device) => void;
  onRoomSelect: (floorId: string, roomId: string) => void;
  onRoomPower: (floorId: string, roomId: string, on: boolean) => void;
  onSurfaceClick: () => void;
}

type RoomPowerState = 'empty' | 'off' | 'mixed' | 'on';
type RoomResizeHandle = 'nw' | 'ne' | 'se' | 'sw';
type RoomEditMode = 'resize' | 'shape';

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
  onRemoveDevice,
  onSelect,
  onDeviceChange,
  onRoomSelect,
  onRoomPower,
  onSurfaceClick,
}: FloorPlanProps) {
  const canvasRef = useRef<HTMLElement | null>(null);
  const floor = activeFloor(layout);
  const [editedRoomId, setEditedRoomId] = useState<string | undefined>();
  const [roomEditMode, setRoomEditMode] = useState<RoomEditMode>('resize');
  const [dropTargetRoomId, setDropTargetRoomId] = useState<string | undefined>();
  const editedRoom = floor?.rooms.find((room) => room.id === editedRoomId);
  const roomLabelRef = useRef<HTMLInputElement | null>(null);
  const placed = new Set(Object.keys(floor?.devices ?? {}));
  const placedAnywhere = new Set(layout?.floors.flatMap((entry) => Object.keys(entry.devices)) ?? []);
  const selectedGroupDevices = selectedGroupId ? new Set(devices.filter((device) => device.groupId === selectedGroupId).map((device) => device.serial)) : undefined;
  const matches = searchMatches(devices, groups, query);
  const placedDevices = floor ? devices.filter((device) => placed.has(device.serial)) : [];
  const unplacedDevices = layout ? devices.filter((device) => !placedAnywhere.has(device.serial)) : devices;
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

  useEffect(() => setRoomEditMode('resize'), [editing, editedRoomId]);

  useEffect(() => {
    if (editing) return;
    setDropTargetRoomId(undefined);
  }, [editing]);

  useEffect(() => {
    if (!editing || !editedRoom) return;
    roomLabelRef.current?.focus();
    roomLabelRef.current?.select();
  }, [editing, editedRoom?.id]);

  return (
    <main
      className="center-panel"
      onClick={(event) => {
        const target = event.target instanceof Element ? event.target : null;
        if (target?.closest('.floor-device-node, .floor-unplaced-device, .floor-tools, .floor-room, .floor-room-editor, button, select, input')) return;
        if (editing) return;
        setEditedRoomId(undefined);
        onSurfaceClick();
      }}
    >
      <div className="floor-plan-shell">
        <header className="floor-plan-header">
          <div>
            <span>{location?.name.toLowerCase() ?? 'location'}</span>
            <h1>{floor?.label ?? 'floor plan'}</h1>
          </div>
          <div className="floor-plan-actions">
            {!editing ? <div className="floor-plan-meta">
              <span>{floor?.rooms.length ?? 0} room{floor?.rooms.length === 1 ? '' : 's'}</span>
            </div> : null}
            <div className="floor-tools">
              {layout && !editing && layout.floors.length > 1 ? (
                <label className="floor-select-wrap">
                  <select value={floor?.id ?? ''} aria-label="Floor" onChange={(event) => onSelectFloor(event.target.value)}>
                    {layout.floors.map((entry) => (
                      <option key={entry.id} value={entry.id}>{entry.label}</option>
                    ))}
                  </select>
                  <ChevronDown size={12} aria-hidden="true" />
                </label>
              ) : null}
              {editing && floor ? (
                <span className="floor-tool-cluster">
                  <input value={floor.label} aria-label="Floor label" onChange={(event) => onRenameFloor(floor.id, event.target.value)} />
                </span>
              ) : null}
              {editing ? (
                <button
                  type="button"
                  className="floor-room-add-button"
                  onClick={() => {
                    const roomId = onAddRoom();
                    if (roomId) setEditedRoomId(roomId);
                  }}
                >
                  <Plus size={12} aria-hidden="true" /> add room
                </button>
              ) : null}
              {editing && layout ? (
                <span className="floor-tool-cluster">
                  <label className="floor-select-wrap">
                    <select value={floor?.id ?? ''} aria-label="Floor" onChange={(event) => onSelectFloor(event.target.value)}>
                      {layout.floors.map((entry) => (
                        <option key={entry.id} value={entry.id}>{entry.label}</option>
                      ))}
                    </select>
                    <ChevronDown size={12} aria-hidden="true" />
                  </label>
                  {floor && layout.floors.length > 1 ? (
                    <button type="button" className="floor-icon-button" aria-label="Delete selected floor" onClick={() => onRemoveFloor(floor.id)}>
                      <Trash2 size={13} strokeWidth={1.8} aria-hidden="true" />
                    </button>
                  ) : null}
                  <button type="button" className="floor-icon-button" aria-label="Add floor" onClick={onAddFloor}>
                    <Plus size={13} strokeWidth={1.9} aria-hidden="true" />
                  </button>
                </span>
              ) : null}
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
            shapeEditing={roomEditMode === 'shape'}
            labelRef={roomLabelRef}
            onShapeEditingChange={(shapeEditing) => setRoomEditMode(shapeEditing ? 'shape' : 'resize')}
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
          data-drop-active={dropTargetRoomId !== undefined ? 'true' : 'false'}
          aria-label={`${floor?.label ?? 'Floor'} floor plan`}
          onDragOver={(event) => {
            if (!editing) return;
            event.preventDefault();
            const point = canvasPoint(event.clientX, event.clientY);
            setDropTargetRoomId(point && floor ? roomAtPoint(floor.rooms, point)?.id : undefined);
          }}
          onDragLeave={(event) => {
            if (event.currentTarget.contains(event.relatedTarget instanceof Node ? event.relatedTarget : null)) return;
            setDropTargetRoomId(undefined);
          }}
          onDrop={(event) => {
            if (!editing) return;
            const serial = event.dataTransfer.getData('application/x-hikari-device');
            const point = canvasPoint(event.clientX, event.clientY);
            setDropTargetRoomId(undefined);
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
              shapeEditing={roomEditMode === 'shape'}
              dropTarget={dropTargetRoomId === room.id}
              powerState={roomPowerState(room.id, floor, devices)}
              canvasPoint={canvasPoint}
              floorDevices={floor.devices}
              onSelectRoom={(roomId) => {
                if (editing) setEditedRoomId(roomId);
                else onRoomSelect(floor.id, roomId);
              }}
              onRoomPower={(roomId, on) => onRoomPower(floor.id, roomId, on)}
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
                x={placement.x}
                y={placement.y}
                editing={editing}
                canvasPoint={canvasPoint}
                onMove={placeDevice}
                onRemove={onRemoveDevice}
                onSelect={onSelect}
                onPowerToggle={(device) => onDeviceChange({ ...device, on: !device.on })}
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
          <DeviceSourceList
            title="unassigned"
            empty=""
            devices={unplacedDevices}
            editing={editing}
            selectedSerial={selectedSerial}
            searching={searching}
            matches={matches}
            selectedGroupDevices={selectedGroupDevices}
            onSelect={onSelect}
            onDragEnd={() => setDropTargetRoomId(undefined)}
          />
        </section>
      </div>
    </main>
  );
}

function activeFloor(layout?: FloorPlanLocation): FloorPlanFloor | undefined {
  if (!layout?.floors.length) return undefined;
  return layout.floors.find((floor) => floor.id === layout.activeFloorId) ?? layout.floors[0];
}

function DeviceSourceList({
  title,
  empty,
  devices,
  editing,
  selectedSerial,
  searching,
  matches,
  selectedGroupDevices,
  onSelect,
  onDragEnd,
}: {
  title: string;
  empty: string;
  devices: Device[];
  editing: boolean;
  selectedSerial?: string;
  searching: boolean;
  matches: Set<string>;
  selectedGroupDevices?: Set<string>;
  onSelect: (serial: string) => void;
  onDragEnd: () => void;
}) {
  return (
    <div className="floor-device-source">
      <div className="floor-section-title">
        <span>{title}</span>
        <b>{devices.length}</b>
      </div>
      {devices.length ? (
        <div className="floor-unplaced-list">
          {devices.map((device) => (
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
              onDragEnd={onDragEnd}
              onClick={() => onSelect(device.serial)}
            >
              <DeviceSwatch device={device} />
              <span>{device.name}</span>
            </button>
          ))}
        </div>
      ) : empty ? (
        <p>{empty}</p>
      ) : null}
    </div>
  );
}

function RoomShape({
  room,
  floorId,
  editing,
  selected,
  shapeEditing,
  dropTarget,
  powerState,
  canvasPoint,
  floorDevices,
  onSelectRoom,
  onRoomPower,
  onUpdateRoom,
}: {
  room: FloorPlanRoom;
  floorId: string;
  editing: boolean;
  selected: boolean;
  shapeEditing: boolean;
  dropTarget: boolean;
  powerState: RoomPowerState;
  canvasPoint: (clientX: number, clientY: number) => FloorPlanPoint | undefined;
  floorDevices: Record<string, FloorPlanDevicePlacement>;
  onSelectRoom: (roomId: string) => void;
  onRoomPower: (roomId: string, on: boolean) => void;
  onUpdateRoom: (floorId: string, roomId: string, patch: FloorPlanRoomPatch) => void;
}) {
  const dragRef = useRef<{
    offset: FloorPlanPoint;
    room: FloorPlanRoom;
    devices: Record<string, FloorPlanDevicePlacement>;
  } | undefined>(undefined);
  const resizeRef = useRef<{
    handle: RoomResizeHandle;
    room: FloorPlanRoom;
    devices: Record<string, FloorPlanDevicePlacement>;
  } | undefined>(undefined);
  const shapeRef = useRef<{
    index: number;
    room: FloorPlanRoom;
    devices: Record<string, FloorPlanDevicePlacement>;
  } | undefined>(undefined);
  const movedRef = useRef(false);

  return (
    <>
      <div
        className="floor-room"
        data-type={room.type ?? 'other'}
        data-editing={editing ? 'true' : 'false'}
        data-selected={selected ? 'true' : 'false'}
        data-drop-target={dropTarget ? 'true' : 'false'}
        data-power={powerState}
        style={{
          clipPath: `polygon(${room.points.map((point) => `${point.x * 100}% ${point.y * 100}%`).join(', ')})`,
        }}
        onPointerDown={(event) => {
          movedRef.current = false;
          if (!editing || shapeEditing) return;
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
          if (!editing || shapeEditing || !event.currentTarget.hasPointerCapture(event.pointerId)) return;
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
          resizeRef.current = undefined;
          shapeRef.current = undefined;
          if (event.currentTarget.hasPointerCapture(event.pointerId)) event.currentTarget.releasePointerCapture(event.pointerId);
        }}
        onPointerCancel={(event) => {
          dragRef.current = undefined;
          resizeRef.current = undefined;
          shapeRef.current = undefined;
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
      {editing && selected && shapeEditing ? (
        <>
          {room.points.map((point, index) => (
            <button
              key={`${room.id}-edge-${index}`}
              type="button"
              className="floor-room-add-point-handle"
              aria-label={`Add ${room.label} shape point`}
              style={roomPointPosition(midpoint(point, room.points[(index + 1) % room.points.length]))}
              onClick={(event) => {
                event.preventDefault();
                event.stopPropagation();
                const points = insertRoomPoint(room, index, midpoint(point, room.points[(index + 1) % room.points.length]));
                onUpdateRoom(floorId, room.id, { points, devices: keepRoomDevicesInsideShape({ ...room, points }, assignedRoomDevices(floorDevices, room.id)) });
              }}
            >
              <Plus size={9} strokeWidth={2.1} aria-hidden="true" />
            </button>
          ))}
          {room.points.map((point, index) => (
            <button
              key={`${room.id}-${index}`}
              type="button"
              className="floor-room-shape-handle"
              aria-label={`Move ${room.label} point ${index + 1}`}
              style={roomPointPosition(point)}
              onPointerDown={(event) => {
                event.preventDefault();
                event.stopPropagation();
                movedRef.current = true;
                shapeRef.current = { index, room, devices: assignedRoomDevices(floorDevices, room.id) };
                event.currentTarget.setPointerCapture(event.pointerId);
              }}
              onPointerMove={(event) => {
                if (!event.currentTarget.hasPointerCapture(event.pointerId)) return;
                const point = canvasPoint(event.clientX, event.clientY);
                const shape = shapeRef.current;
                if (!point || !shape) return;
                const points = moveRoomPoint(shape.room, shape.index, point);
                onUpdateRoom(floorId, room.id, { points, devices: keepRoomDevicesInsideShape({ ...shape.room, points }, shape.devices) });
              }}
              onPointerUp={(event) => {
                shapeRef.current = undefined;
                if (event.currentTarget.hasPointerCapture(event.pointerId)) event.currentTarget.releasePointerCapture(event.pointerId);
              }}
              onPointerCancel={(event) => {
                shapeRef.current = undefined;
                if (event.currentTarget.hasPointerCapture(event.pointerId)) event.currentTarget.releasePointerCapture(event.pointerId);
              }}
              onDoubleClick={(event) => {
                if (room.points.length <= 3) return;
                event.preventDefault();
                event.stopPropagation();
                const points = removeRoomPoint(room, index);
                onUpdateRoom(floorId, room.id, { points, devices: keepRoomDevicesInsideShape({ ...room, points }, assignedRoomDevices(floorDevices, room.id)) });
              }}
              onKeyDown={(event) => {
                if (room.points.length <= 3 || (event.key !== 'Backspace' && event.key !== 'Delete')) return;
                event.preventDefault();
                event.stopPropagation();
                const points = removeRoomPoint(room, index);
                onUpdateRoom(floorId, room.id, { points, devices: keepRoomDevicesInsideShape({ ...room, points }, assignedRoomDevices(floorDevices, room.id)) });
              }}
            />
          ))}
        </>
      ) : null}
      {editing && selected && !shapeEditing && isRectangleRoom(room) ? (
        <>
          {(['nw', 'ne', 'se', 'sw'] as RoomResizeHandle[]).map((handle) => (
            <button
              key={handle}
              type="button"
              className="floor-room-resize-handle"
              data-handle={handle}
              aria-label={`Resize ${room.label}`}
              style={roomResizeHandlePosition(room, handle)}
              onPointerDown={(event) => {
                const point = canvasPoint(event.clientX, event.clientY);
                if (!point) return;
                event.preventDefault();
                event.stopPropagation();
                movedRef.current = true;
                resizeRef.current = { handle, room, devices: assignedRoomDevices(floorDevices, room.id) };
                event.currentTarget.setPointerCapture(event.pointerId);
              }}
              onPointerMove={(event) => {
                if (!event.currentTarget.hasPointerCapture(event.pointerId)) return;
                const point = canvasPoint(event.clientX, event.clientY);
                const resize = resizeRef.current;
                if (!point || !resize) return;
                const points = resizeRectangleRoom(resize.room, resize.handle, point);
                onUpdateRoom(floorId, room.id, { points, devices: keepRoomDevicesInsideShape({ ...resize.room, points }, resize.devices) });
              }}
              onPointerUp={(event) => {
                resizeRef.current = undefined;
                if (event.currentTarget.hasPointerCapture(event.pointerId)) event.currentTarget.releasePointerCapture(event.pointerId);
              }}
              onPointerCancel={(event) => {
                resizeRef.current = undefined;
                if (event.currentTarget.hasPointerCapture(event.pointerId)) event.currentTarget.releasePointerCapture(event.pointerId);
              }}
            />
          ))}
        </>
      ) : null}
      {!editing && powerState !== 'empty' ? (
        <button
          type="button"
          className="floor-room-power"
          aria-label={powerState === 'off' ? `Turn ${room.label} on` : `Turn ${room.label} off`}
          style={roomPowerPosition(room)}
          onClick={(event) => {
            event.stopPropagation();
            onRoomPower(room.id, powerState === 'off');
          }}
        >
          <span className="power-dot" data-on={powerState !== 'off' ? 'true' : 'false'} />
        </button>
      ) : null}
    </>
  );
}

function RoomEditor({
  floorId,
  room,
  shapeEditing,
  labelRef,
  onShapeEditingChange,
  onUpdateRoom,
  onRemoveRoom,
}: {
  floorId: string;
  room?: FloorPlanRoom;
  shapeEditing: boolean;
  labelRef: RefObject<HTMLInputElement | null>;
  onShapeEditingChange: (shapeEditing: boolean) => void;
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
            ref={labelRef}
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
          <button type="button" className="floor-shape-button" data-active={shapeEditing ? 'true' : 'false'} aria-pressed={shapeEditing} onClick={() => onShapeEditingChange(!shapeEditing)}>
            shape
          </button>
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
  x,
  y,
  editing,
  canvasPoint,
  onMove,
  onRemove,
  onSelect,
  onPowerToggle,
}: {
  device: Device;
  selected: boolean;
  x: number;
  y: number;
  editing: boolean;
  canvasPoint: (clientX: number, clientY: number) => FloorPlanPoint | undefined;
  onMove: (serial: string, point: FloorPlanPoint) => void;
  onRemove: (serial: string) => void;
  onSelect: (serial: string) => void;
  onPowerToggle: (device: Device) => void;
}) {
  const movedRef = useRef(false);
  return (
    <div
      role="button"
      tabIndex={0}
      className="floor-device-node"
      data-selected={selected}
      data-on={device.on && isLightDevice(device) ? 'true' : 'false'}
      data-offline={!device.online ? 'true' : 'false'}
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
        if (editing) return;
        if (movedRef.current) {
          movedRef.current = false;
          return;
        }
        onSelect(device.serial);
      }}
      onKeyDown={(event) => {
        if (editing) return;
        if (event.key !== 'Enter' && event.key !== ' ') return;
        event.preventDefault();
        onSelect(device.serial);
      }}
    >
      <button
        type="button"
        className="floor-device-swatch-button"
        aria-label={device.on ? `Turn ${device.name} off` : `Turn ${device.name} on`}
        disabled={!isLightDevice(device) || editing || !device.online}
        onPointerDown={(event) => {
          event.stopPropagation();
        }}
        onClick={(event) => {
          event.preventDefault();
          event.stopPropagation();
          onPowerToggle(device);
        }}
      >
        <DeviceSwatch device={device} />
      </button>
      <span>{device.name}</span>
      {editing ? (
        <i
          role="button"
          tabIndex={0}
          className="floor-device-remove"
          aria-label={`Unassign ${device.name}`}
          onPointerDown={(event) => {
            event.preventDefault();
            event.stopPropagation();
          }}
          onClick={(event) => {
            event.preventDefault();
            event.stopPropagation();
            onRemove(device.serial);
          }}
          onKeyDown={(event) => {
            if (event.key !== 'Enter' && event.key !== ' ') return;
            event.preventDefault();
            event.stopPropagation();
            onRemove(device.serial);
          }}
        >
          <X size={11} strokeWidth={2} aria-hidden="true" />
        </i>
      ) : null}
    </div>
  );
}

function DeviceSwatch({ device }: { device: Device }) {
  if (!isLightDevice(device)) return <i className="floor-device-swatch" data-switch="true" />;
  const color = deviceColor(device);
  return <i className="floor-device-swatch" data-on={device.on ? 'true' : 'false'} style={{ background: hsl(color, previewLightness(color, device.brightness, device.on)), opacity: previewOpacity(device.on) }} />;
}

function roomLabelPosition(room: FloorPlanRoom): { left: string; top: string } {
  return roomAnchorStyle(roomInteriorPoint(room, roomCenter(room)));
}

function roomPowerPosition(room: FloorPlanRoom): { left: string; top: string } {
  const bounds = roomBounds(room.points);
  const ideal = {
    x: Math.max(bounds.left, Math.min(bounds.right, bounds.right - Math.max(0.025, (bounds.right - bounds.left) * 0.12))),
    y: Math.max(bounds.top, Math.min(bounds.bottom, bounds.top + Math.max(0.025, (bounds.bottom - bounds.top) * 0.12))),
  };
  return roomAnchorStyle(roomInteriorPoint(room, ideal));
}

function roomResizeHandlePosition(room: FloorPlanRoom, handle: RoomResizeHandle): { left: string; top: string } {
  const bounds = roomBounds(room.points);
  const x = handle.includes('w') ? bounds.left : bounds.right;
  const y = handle.includes('n') ? bounds.top : bounds.bottom;
  return { left: `${x * 100}%`, top: `${y * 100}%` };
}

function roomPointPosition(point: FloorPlanPoint): { left: string; top: string } {
  return { left: `${point.x * 100}%`, top: `${point.y * 100}%` };
}

function roomAnchorStyle(point: FloorPlanPoint): { left: string; top: string } {
  return { left: `${point.x * 100}%`, top: `${point.y * 100}%` };
}

function roomPowerState(roomId: string, floor: FloorPlanFloor, devices: Device[]): RoomPowerState {
  const lights = devices.filter(isLightDevice).filter((device) => floor.devices[device.serial]?.roomId === roomId);
  const online = lights.filter((device) => device.online);
  if (!online.length) return 'empty';
  const onCount = online.filter((device) => device.on).length;
  if (onCount === 0) return 'off';
  return onCount === online.length ? 'on' : 'mixed';
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

function resizeRectangleRoom(room: FloorPlanRoom, handle: RoomResizeHandle, point: FloorPlanPoint): FloorPlanPoint[] {
  const bounds = roomBounds(room.points);
  const minSize = 0.08;
  let left = bounds.left;
  let right = bounds.right;
  let top = bounds.top;
  let bottom = bounds.bottom;

  if (handle.includes('w')) left = Math.min(clampUnit(point.x), right - minSize);
  if (handle.includes('e')) right = Math.max(clampUnit(point.x), left + minSize);
  if (handle.includes('n')) top = Math.min(clampUnit(point.y), bottom - minSize);
  if (handle.includes('s')) bottom = Math.max(clampUnit(point.y), top + minSize);

  left = clampUnit(left);
  right = clampUnit(right);
  top = clampUnit(top);
  bottom = clampUnit(bottom);

  if (right - left < minSize) {
    if (handle.includes('w')) left = Math.max(0, right - minSize);
    else right = Math.min(1, left + minSize);
  }
  if (bottom - top < minSize) {
    if (handle.includes('n')) top = Math.max(0, bottom - minSize);
    else bottom = Math.min(1, top + minSize);
  }

  return [
    { x: left, y: top },
    { x: right, y: top },
    { x: right, y: bottom },
    { x: left, y: bottom },
  ];
}

function moveRoomPoint(room: FloorPlanRoom, index: number, point: FloorPlanPoint): FloorPlanPoint[] {
  return room.points.map((entry, entryIndex) => (entryIndex === index ? { x: clampUnit(point.x), y: clampUnit(point.y) } : entry));
}

function insertRoomPoint(room: FloorPlanRoom, afterIndex: number, point: FloorPlanPoint): FloorPlanPoint[] {
  const next = [...room.points];
  next.splice(afterIndex + 1, 0, { x: clampUnit(point.x), y: clampUnit(point.y) });
  return next;
}

function removeRoomPoint(room: FloorPlanRoom, index: number): FloorPlanPoint[] {
  if (room.points.length <= 3) return room.points;
  return room.points.filter((_, entryIndex) => entryIndex !== index);
}

function midpoint(a: FloorPlanPoint, b: FloorPlanPoint): FloorPlanPoint {
  return { x: (a.x + b.x) / 2, y: (a.y + b.y) / 2 };
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

function isRectangleRoom(room: FloorPlanRoom): boolean {
  if (room.points.length !== 4) return false;
  const bounds = roomBounds(room.points);
  return room.points.every((point) => (point.x === bounds.left || point.x === bounds.right) && (point.y === bounds.top || point.y === bounds.bottom));
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
