import { useEffect, useMemo, useState } from 'react';
import { Info, X } from 'lucide-react';
import type { Device, HslColor } from '../domain/lifx';
import { applyDeviceBrightness, applyDeviceColor, initialPaintColor, kelvinToHsl } from '../domain/paint';
import { ColorWheel, Slider } from './primitives';
import { ModeToggle, WhiteScale } from './Inspector';
import './Inspector.css';

type PaintMode = 'color' | 'white';

interface RoomInspectorProps {
  roomName: string;
  devices: Device[];
  onClose: () => void;
  onDeviceChange: (device: Device) => void;
}

export function RoomInspector({ roomName, devices, onClose, onDeviceChange }: RoomInspectorProps) {
  const onlineDevices = devices.filter((device) => device.online);
  const colorDevices = onlineDevices.filter((device) => device.capability?.hasColor ?? true);
  const hasColor = colorDevices.length > 0;
  const kelvinRange = useMemo(() => roomKelvinRange(onlineDevices), [onlineDevices]);
  const firstDevice = onlineDevices[0];
  const [mode, setMode] = useState<PaintMode>(hasColor ? 'color' : 'white');
  const [paintColor, setPaintColor] = useState<HslColor>(() => (firstDevice ? initialPaintColor(firstDevice) : { h: 38, s: 0.5, l: 0.55 }));
  const [whiteKelvin, setWhiteKelvin] = useState(() => clampKelvin(firstDevice?.kelvin ?? 3500, kelvinRange.min, kelvinRange.max));
  const [showInfo, setShowInfo] = useState(false);
  const avgBrightness = onlineDevices.length ? onlineDevices.reduce((sum, device) => sum + device.brightness, 0) / onlineDevices.length : 0;
  const allOff = onlineDevices.length > 0 && onlineDevices.every((device) => !device.on);
  const whiteValue = Math.max(0, Math.min(1, (whiteKelvin - kelvinRange.min) / Math.max(1, kelvinRange.max - kelvinRange.min)));

  useEffect(() => {
    setMode(hasColor ? 'color' : 'white');
  }, [roomName, hasColor]);

  useEffect(() => {
    if (!firstDevice) return;
    setPaintColor(initialPaintColor(firstDevice));
    setWhiteKelvin(clampKelvin(firstDevice.kelvin ?? 3500, kelvinRange.min, kelvinRange.max));
  }, [firstDevice?.serial, roomName, kelvinRange.max, kelvinRange.min]);

  const setRoomBrightness = (brightness: number) => {
    for (const device of onlineDevices) onDeviceChange(applyDeviceBrightness(device, brightness));
  };

  const setRoomColor = (color: HslColor) => {
    const next = { ...color, l: Math.max(avgBrightness, color.l) };
    setPaintColor(next);
    for (const device of colorDevices) onDeviceChange(applyDeviceColor(device, next));
  };

  const setRoomKelvin = (value: number) => {
    const kelvin = Math.round(kelvinRange.min + value * (kelvinRange.max - kelvinRange.min));
    const next = { ...kelvinToHsl(kelvin), l: Math.max(avgBrightness, 0.55) };
    setWhiteKelvin(kelvin);
    for (const device of onlineDevices) onDeviceChange(applyDeviceColor(device, next));
  };

  return (
    <aside className="right-panel inspector">
      <header className="inspector-header">
        <div>
          <h2>{roomName}</h2>
          <p>{onlineDevices.length} light{onlineDevices.length === 1 ? '' : 's'} assigned</p>
        </div>
        <button className="icon-button close-button" aria-label="Close room controls" onClick={onClose}>
          <X size={15} />
        </button>
      </header>

      <div className="inspector-meta">
        <span>room controls</span>
        <button className="info-toggle" type="button" aria-label="Room devices" aria-expanded={showInfo} data-active={showInfo ? 'true' : 'false'} onClick={() => setShowInfo((value) => !value)}>
          <Info size={13} />
        </button>
      </div>

      {showInfo ? <RoomDeviceInfo devices={devices} /> : null}

      <ModeToggle value={mode} hasColor={hasColor} onChange={(value) => {
        if (value !== 'effects') setMode(value);
      }} />

      {mode === 'color' ? (
        <section className="control-section">
          <div className="color-wheel-wrap">
            <ColorWheel color={paintColor} onChange={setRoomColor} />
          </div>
        </section>
      ) : (
        <WhiteScale value={whiteValue} kelvinMin={kelvinRange.min} kelvinMax={kelvinRange.max} onChange={setRoomKelvin} />
      )}

      <Slider label="brightness" disabled={!onlineDevices.length} value={avgBrightness} valueLabel={allOff ? 'off' : undefined} onChange={setRoomBrightness} />
    </aside>
  );
}

function RoomDeviceInfo({ devices }: { devices: Device[] }) {
  return (
    <dl className="device-info room-device-info">
      {devices.map((device) => (
        <div key={device.serial}>
          <dt>{device.name}</dt>
          <dd>{device.model || 'unknown'}</dd>
        </div>
      ))}
    </dl>
  );
}

function roomKelvinRange(devices: Device[]): { min: number; max: number } {
  const mins = devices.map((device) => device.capability?.kelvinMin).filter((value): value is number => typeof value === 'number' && value > 0);
  const maxes = devices.map((device) => device.capability?.kelvinMax).filter((value): value is number => typeof value === 'number' && value > 0);
  const min = mins.length ? Math.max(...mins) : 2500;
  const max = maxes.length ? Math.min(...maxes) : 6500;
  return min <= max ? { min, max } : { min: 2500, max: 6500 };
}

function clampKelvin(kelvin: number, min: number, max: number): number {
  return Math.max(min, Math.min(max, kelvin));
}
