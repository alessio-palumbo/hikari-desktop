import { useEffect, useMemo, useState } from 'react';
import { ChevronDown, Info, Minus, Plus, Radar, Settings, Trash2, X } from 'lucide-react';
import type { SensorNode } from '../backend/api';
import { defaultFloorPlanPresenceConfig, type FloorPlanPresenceConfig } from '../domain/floorPlan';
import type { Device, HslColor } from '../domain/lifx';
import { applyDeviceBrightness, applyDeviceColor, initialPaintColor, kelvinToHsl } from '../domain/paint';
import { presenceReading } from '../domain/sensors';
import { ColorWheel, Slider } from './primitives';
import { ModeToggle, WhiteScale } from './Inspector';
import './Inspector.css';

type PaintMode = 'color' | 'white';

interface RoomInspectorProps {
  roomName: string;
  devices: Device[];
  sensors: SensorNode[];
  presence?: FloorPlanPresenceConfig;
  onClose: () => void;
  onDeviceChange: (device: Device) => void;
  onPresenceChange: (presence: FloorPlanPresenceConfig) => void;
}

export function RoomInspector({ roomName, devices, sensors, presence, onClose, onDeviceChange, onPresenceChange }: RoomInspectorProps) {
  const onlineDevices = devices.filter((device) => device.online);
  const colorDevices = onlineDevices.filter((device) => device.capability?.hasColor ?? true);
  const hasColor = colorDevices.length > 0;
  const kelvinRange = useMemo(() => roomKelvinRange(onlineDevices), [onlineDevices]);
  const firstDevice = onlineDevices[0];
  const [mode, setMode] = useState<PaintMode>(hasColor ? 'color' : 'white');
  const [paintColor, setPaintColor] = useState<HslColor>(() => (firstDevice ? initialPaintColor(firstDevice) : { h: 38, s: 0.5, l: 0.55 }));
  const [whiteKelvin, setWhiteKelvin] = useState(() => clampKelvin(firstDevice?.kelvin ?? 3500, kelvinRange.min, kelvinRange.max));
  const [showInfo, setShowInfo] = useState(false);
  const presenceConfig = presence ?? defaultFloorPlanPresenceConfig();
  const showSensors = sensors.length > 0 || presenceConfig.sensorIds.length > 0;
  const hasPresenceSensor = presenceConfig.sensorIds.some((sensorId) => {
    const sensor = sensors.find((candidate) => candidate.id === sensorId);
    return !sensor || sensor.capabilities.includes('presence');
  });
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

      {showSensors ? (
        <SensorsSection sensors={sensors} config={presenceConfig} hasPresenceSensor={hasPresenceSensor} onChange={onPresenceChange} />
      ) : null}

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

function SensorsSection(props: {
  sensors: SensorNode[];
  config: FloorPlanPresenceConfig;
  hasPresenceSensor: boolean;
  onChange: (config: FloorPlanPresenceConfig) => void;
}) {
  const { sensors, config, hasPresenceSensor, onChange } = props;
  const [showPresenceSettings, setShowPresenceSettings] = useState(false);
  const [infoSensorId, setInfoSensorId] = useState<string>();
  const byId = new Map(sensors.map((sensor) => [sensor.id, sensor]));
  const available = sensors.filter((sensor) => !config.sensorIds.includes(sensor.id));

  useEffect(() => {
    if (!hasPresenceSensor) setShowPresenceSettings(false);
  }, [hasPresenceSensor]);

  return (
    <section className="sensor-controls">
      <header>
        <span><Radar size={13} aria-hidden="true" /> sensors</span>
        {hasPresenceSensor ? (
          <button
            className="sensor-settings-button"
            type="button"
            aria-label="Sensor settings"
            aria-expanded={showPresenceSettings}
            data-active={showPresenceSettings ? 'true' : 'false'}
            onClick={() => setShowPresenceSettings((value) => !value)}
          >
            <Settings size={12} aria-hidden="true" />
          </button>
        ) : null}
      </header>

      {showPresenceSettings ? <PresenceLightingControls config={config} onChange={onChange} /> : null}

      <div className="sensor-list">
        {config.sensorIds.map((sensorId) => {
          const sensor = byId.get(sensorId);
          return (
            <div className="sensor-item" key={sensorId}>
              <div className="sensor-item-header">
                <span
                  className="sensor-status-dot"
                  data-online={sensor?.online ? 'true' : 'false'}
                  role="img"
                  aria-label={sensor?.online ? 'Online' : 'Offline'}
                />
                <span className="sensor-identity">
                  <strong>{sensor?.name ?? sensorId}</strong>
                </span>
                <button
                  className="sensor-info-button"
                  type="button"
                  aria-label={`Information about ${sensor?.name ?? sensorId}`}
                  aria-expanded={infoSensorId === sensorId}
                  data-active={infoSensorId === sensorId ? 'true' : 'false'}
                  onClick={() => setInfoSensorId((current) => current === sensorId ? undefined : sensorId)}
                >
                  <Info size={11} aria-hidden="true" />
                </button>
                <button
                  className="sensor-remove-button"
                  type="button"
                  aria-label={`Remove ${sensor?.name ?? sensorId}`}
                  onClick={() => onChange({ ...config, sensorIds: config.sensorIds.filter((id) => id !== sensorId) })}
                >
                  <Trash2 size={12} aria-hidden="true" />
                </button>
              </div>
              {infoSensorId === sensorId ? <SensorInfo sensorId={sensorId} /> : null}
              {sensor ? <SensorReadings sensor={sensor} /> : null}
            </div>
          );
        })}
      </div>

      {available.length ? (
        <label className="sensor-select-row">
          <span>assign sensor</span>
          <span className="sensor-select">
            <select
              aria-label="Assign sensor"
              value=""
              onChange={(event) => {
                if (!event.target.value) return;
                onChange({ ...config, sensorIds: [...config.sensorIds, event.target.value].sort() });
              }}
            >
              <option value="">select...</option>
              {available.map((sensor) => <option key={sensor.id} value={sensor.id}>{sensor.name}</option>)}
            </select>
            <ChevronDown size={12} aria-hidden="true" />
          </span>
        </label>
      ) : null}
    </section>
  );
}

function SensorReadings({ sensor }: { sensor: SensorNode }) {
  const readings: Array<{ label: string; value: string; state?: 'occupied' | 'clear' | 'unknown' }> = [];
  if (sensor.capabilities.includes('presence')) {
    const state = !sensor.online || !sensor.presenceKnown ? 'unknown' : sensor.present ? 'occupied' : 'clear';
    readings.push({
      label: 'presence',
      value: presenceReading(sensor),
      state,
    });
  }
  if (!readings.length) return null;

  return (
    <dl className="sensor-readings">
      {readings.map((reading) => (
        <div key={reading.label}>
          <dt>{reading.label}</dt>
          <dd data-state={reading.state}>{reading.value}</dd>
        </div>
      ))}
    </dl>
  );
}

function SensorInfo({ sensorId }: { sensorId: string }) {
  return (
    <dl className="sensor-info">
      <div>
        <dt>sensor ID</dt>
        <dd>{sensorId}</dd>
      </div>
      <div>
        <dt>protocol</dt>
        <dd>Sensaa</dd>
      </div>
    </dl>
  );
}

function PresenceLightingControls(props: {
  config: FloorPlanPresenceConfig;
  onChange: (config: FloorPlanPresenceConfig) => void;
}) {
  const { config, onChange } = props;

  return (
    <div className="presence-lighting-controls">
      <label className="presence-toggle">
        <span>presence lighting</span>
        <input
          type="checkbox"
          checked={config.lightingEnabled}
          onChange={(event) => onChange({ ...config, lightingEnabled: event.target.checked })}
        />
        <i aria-hidden="true" />
      </label>
      <div className="presence-setting-row">
        <span>clear action</span>
        <div className="presence-clear-actions" role="radiogroup" aria-label="Presence lighting clear action">
          <label>
            <input type="radio" name="presence-clear-action" value="off" checked={config.clearAction === 'off'} onChange={() => onChange({ ...config, clearAction: 'off' })} />
            <span>turn off</span>
          </label>
          <label>
            <input type="radio" name="presence-clear-action" value="dim" checked={config.clearAction === 'dim'} onChange={() => onChange({ ...config, clearAction: 'dim' })} />
            <span>dim</span>
          </label>
        </div>
      </div>
      <CompactNumberSetting
        label={config.clearAction === 'dim' ? 'dim after' : 'off delay'}
        ariaLabel="Presence lighting clear delay in seconds"
        value={config.offDelaySeconds}
        min={1}
        max={3600}
        step={5}
        suffix="sec"
        onChange={(offDelaySeconds) => onChange({ ...config, offDelaySeconds: clampOffDelay(offDelaySeconds) })}
      />
      {config.clearAction === 'dim' ? (
        <CompactNumberSetting
          label="brightness"
          ariaLabel="Presence lighting dim brightness percentage"
          value={Math.round(config.dimBrightness * 100)}
          min={1}
          max={100}
          step={5}
          suffix="%"
          onChange={(brightness) => onChange({ ...config, dimBrightness: clampDimBrightness(brightness / 100) })}
        />
      ) : null}
    </div>
  );
}

function CompactNumberSetting(props: {
  label: string;
  ariaLabel: string;
  value: number;
  min: number;
  max: number;
  step: number;
  suffix: string;
  onChange: (value: number) => void;
}) {
  const update = (value: number) => props.onChange(Math.max(props.min, Math.min(props.max, Math.round(value))));
  return (
    <div className="presence-setting-row">
      <span>{props.label}</span>
      <span className="presence-number-value">
        <span className="presence-number-control">
          <button type="button" aria-label={`Decrease ${props.label}`} onClick={() => update(props.value - props.step)}>
            <Minus size={10} aria-hidden="true" />
          </button>
          <input
            aria-label={props.ariaLabel}
            type="number"
            min={props.min}
            max={props.max}
            value={props.value}
            onChange={(event) => update(Number(event.target.value) || props.min)}
          />
          <button type="button" aria-label={`Increase ${props.label}`} onClick={() => update(props.value + props.step)}>
            <Plus size={10} aria-hidden="true" />
          </button>
        </span>
        {props.suffix}
      </span>
    </div>
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

function clampOffDelay(seconds: number): number {
  return Math.max(1, Math.min(3600, Math.round(seconds)));
}

function clampDimBrightness(brightness: number): number {
  return Math.max(0.01, Math.min(1, brightness));
}
