import { useEffect, useRef, useState, type FormEvent } from 'react';
import { Check, ChevronDown, Eye, EyeOff, LoaderCircle, RefreshCw, Wifi, X } from 'lucide-react';
import { cancelLightSetup, scanSetupLights, subscribeToSetupProgress } from '../backend/api';
import { DeviceMetadataEditor } from './DeviceMetadataEditor';
import type { Device, Group, Location } from '../domain/lifx';
import { canSubmitSetup, setupMetadataDefaults, setupPhaseLabel, type SetupLight, type SetupLightRequest, type SetupLightResult } from '../domain/onboarding';
import './OnboardingModal.css';

interface Props {
  devices: Device[];
  locations: Location[];
  groups: Group[];
  saving: boolean;
  onMetadataSave: (request: { serial: string; label: string; locationId: string; groupId: string }) => Promise<void>;
  onSetup: (request: SetupLightRequest, signal: AbortSignal) => Promise<SetupLightResult>;
  onClose: (serial?: string) => void;
}

export function OnboardingModal({ onSetup, onClose, devices, locations, groups, saving, onMetadataSave }: Props) {
  const [lights, setLights] = useState<SetupLight[]>([]);
  const [bulbSsid, setBulbSsid] = useState('');
  const [ssid, setSsid] = useState('');
  const [password, setPassword] = useState('');
  const [showPassword, setShowPassword] = useState(false);
  const [networks, setNetworks] = useState<string[]>([]);
  const [manualNetwork, setManualNetwork] = useState(false);
  const openNetwork = password.length === 0;
  const [scanning, setScanning] = useState(false);
  const [running, setRunning] = useState(false);
  const [cancelling, setCancelling] = useState(false);
  const [confirming, setConfirming] = useState(false);
  const [phase, setPhase] = useState('prepare');
  const [error, setError] = useState('');
  const [result, setResult] = useState<SetupLightResult>();
  const discoveredDevice = devices.find((device) => device.serial === result?.serial);
  const attempt = useRef('');
  const abort = useRef<AbortController | undefined>(undefined);
  const mounted = useRef(true);
  const scanInFlight = useRef(false);
  const initialSSID = useRef(false);
  const root = useRef<HTMLDivElement>(null);

  const scan = async (refresh = false) => {
    if (scanInFlight.current || running) return;
    scanInFlight.current = true; setScanning(true); setError(''); setConfirming(false); setResult(undefined);
    try {
      const discovery = await scanSetupLights(refresh);
      if (!mounted.current) return;
      setLights(discovery.lights); setBulbSsid('');
      const availableNetworks = [...new Set([...discovery.networks, discovery.currentSsid].filter((name) => name && !discovery.lights.some((light) => light.ssid === name)))].sort();
      setNetworks(availableNetworks);
      if (!initialSSID.current) { setSsid(availableNetworks.includes(discovery.currentSsid) ? discovery.currentSsid : ''); setManualNetwork(!availableNetworks.length); initialSSID.current = true; }
    } catch (failure) { if (mounted.current) { setLights([]); setError(message(failure)); } }
    finally { scanInFlight.current = false; if (mounted.current) setScanning(false); }
  };

  useEffect(() => {
    mounted.current = true;
    const unsubscribe = subscribeToSetupProgress((progress) => {
      if (progress.attemptId === attempt.current) setPhase(progress.phase);
    });
    let disposed = false;
    void Promise.resolve().then(() => { if (!disposed) void scan(); });
    return () => { disposed = true; mounted.current = false; unsubscribe?.(); if (scanInFlight.current) void cancelLightSetup(); };
  }, []);

  const close = () => { if (!running && !saving) onClose(result?.verified ? result.serial : undefined); };

  useEffect(() => {
    if (root.current && !root.current.contains(document.activeElement)) {
      root.current.querySelector<HTMLElement>('button:not(:disabled), select:not(:disabled)')?.focus();
    }
  }, [scanning, running]);

  useEffect(() => {
    const keydown = (event: KeyboardEvent) => {
      if (event.key === 'Escape') { event.preventDefault(); event.stopPropagation(); if (!running && !saving) close(); }
      if (event.key !== 'Tab' || !root.current) return;
      const controls = [...root.current.querySelectorAll<HTMLElement>('button:not(:disabled), input:not(:disabled), select:not(:disabled)')];
      const first = controls[0], last = controls[controls.length - 1];
      if (!root.current.contains(document.activeElement)) { event.preventDefault(); first?.focus(); }
      else if (event.shiftKey && document.activeElement === first) { event.preventDefault(); last?.focus(); }
      else if (!event.shiftKey && document.activeElement === last) { event.preventDefault(); first?.focus(); }
    };
    window.addEventListener('keydown', keydown, true);
    return () => window.removeEventListener('keydown', keydown, true);
  }, [running, result, saving]);

  const submit = async (event: FormEvent) => {
    event.preventDefault();
    const request = { attemptId: `setup-${Date.now()}-${Math.random().toString(36).slice(2)}`, bulbSsid, ssid, password, openNetwork };
    if (running || scanning || !canSubmitSetup(request)) return;
    if (!confirming) { setConfirming(true); return; }
    attempt.current = request.attemptId;
    abort.current = new AbortController();
    setRunning(true); setPhase('prepare'); setError(''); setResult(undefined); setPassword(''); setShowPassword(false); setConfirming(false);
    try { const next = await onSetup(request, abort.current.signal); if (mounted.current) setResult(next); }
    catch (failure) { if (mounted.current) setError(message(failure)); }
    finally { if (mounted.current) { setRunning(false); setCancelling(false); } }
  };

  const cancel = async () => {
    setCancelling(true);
    abort.current?.abort();
    try { await cancelLightSetup(attempt.current); }
    catch (failure) { setError(message(failure)); setCancelling(false); }
  };

  return <div className="setup-backdrop" onClick={(event) => { if (event.target === event.currentTarget) close(); }}>
    <div className="setup-modal" ref={root} role="dialog" aria-modal="true" aria-labelledby="setup-title">
      <header><span><Wifi size={16} /><strong id="setup-title">{result?.verified ? 'device details' : 'add device'}</strong></span><button className="icon-button" type="button" aria-label="Close device setup" title="Close" disabled={running || saving} onClick={close}><X size={15} /></button></header>
      {result?.verified ? <div className="setup-details">
        <div className="setup-status" role="status"><span><Check size={13} />Device found on the LAN</span></div>
        {discoveredDevice ? <DeviceMetadataEditor key={discoveredDevice.serial} device={discoveredDevice} initialValues={setupMetadataDefaults(discoveredDevice, bulbSsid, locations, groups)} locations={locations} groups={groups} saving={saving} cancelLabel="skip" onCancel={close} onSave={async (request) => { await onMetadataSave(request); close(); }} /> : <><p className="setup-confirm">Waiting for device details</p><button className="setup-action" type="button" onClick={close}>done</button></>}
      </div> : <form onSubmit={(event) => void submit(event)}>
        <fieldset disabled={running || scanning || !!result?.configured}>
          <label><span>device</span><span className="setup-select"><select autoFocus aria-label="Device in setup mode" value={bulbSsid} onChange={(event) => { setBulbSsid(event.target.value); setConfirming(false); }}><option value="">select device</option>{lights.map((light) => <option key={light.ssid} value={light.ssid}>{light.ssid}</option>)}</select><ChevronDown size={12} /></span></label>
          <label><span>Wi-Fi</span>{manualNetwork || !networks.length ? <span className="setup-network-manual"><input aria-label="Wi-Fi network name" value={ssid} maxLength={32} autoComplete="off" spellCheck={false} placeholder="network name" onChange={(event) => { setSsid(event.target.value); setConfirming(false); }} />{networks.length ? <button className="icon-button" type="button" title="Choose a discovered network" aria-label="Choose a discovered network" onClick={() => setManualNetwork(false)}><ChevronDown size={12} /></button> : null}</span> : <span className="setup-select"><select aria-label="Wi-Fi network" value={networks.includes(ssid) ? `ssid:${ssid}` : ''} onChange={(event) => { const value = event.target.value; if (value === 'manual') { setManualNetwork(true); setSsid(''); } else setSsid(value.slice(5)); setConfirming(false); }}><option value="">select network</option>{networks.map((name) => <option key={name} value={`ssid:${name}`}>{name}</option>)}<option value="manual">other network...</option></select><ChevronDown size={12} /></span>}</label>
          <label><span>password</span><span className="setup-password"><input type={showPassword ? 'text' : 'password'} value={password} maxLength={64} autoComplete="new-password" placeholder="leave empty for an open network" onChange={(event) => { setPassword(event.target.value); setConfirming(false); }} /><button type="button" className="icon-button" title={showPassword ? 'Hide password' : 'Show password'} aria-label={showPassword ? 'Hide password' : 'Show password'} aria-pressed={showPassword} onClick={() => setShowPassword((current) => !current)}>{showPassword ? <EyeOff size={13} /> : <Eye size={13} />}</button></span></label>
        </fieldset>
        {error || result?.error ? <p className="setup-error" role="alert">{error || result?.error}</p> : null}
        {result?.warning ? <p className="setup-error" role="alert">{result.warning}</p> : null}
        {confirming ? <p className="setup-confirm">Set up {bulbSsid} on {ssid}{openNetwork ? ' as an open network (no password)' : ''}?<br />This computer will briefly disconnect from Wi-Fi.</p> : null}
        <footer><div className="setup-status" role="status">{scanning ? <span><LoaderCircle size={13} className="setup-spinner" />finding setup networks</span> : running ? <span><LoaderCircle size={13} className="setup-spinner" />{cancelling ? 'Cancelling and reconnecting Wi-Fi' : setupPhaseLabel(phase)}</span> : result?.verified ? <span><Check size={13} />Device found on the LAN</span> : !lights.length && !error ? 'No devices in setup mode found.' : lights.length && !result ? `${lights.length} ${lights.length === 1 ? 'device' : 'devices'} found` : null}</div><button className="icon-button" type="button" title="Scan again" aria-label="Scan again" disabled={scanning || running} onClick={() => void scan(true)}><RefreshCw size={14} /></button>{running ? <button className="setup-action" type="button" disabled={cancelling} onClick={() => void cancel()}>cancel</button> : result?.verified ? <button className="setup-action" type="button" onClick={close}>done</button> : <button className="setup-action" type="submit" disabled={scanning || !canSubmitSetup({ attemptId: '', bulbSsid, ssid, password, openNetwork }) || !!result?.configured}>{confirming ? 'connect' : 'set up'}</button>}</footer>
      </form>}
    </div>
  </div>;
}

function message(error: unknown): string { return error instanceof Error ? error.message : String(error); }
