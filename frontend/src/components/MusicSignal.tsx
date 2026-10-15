import { useEffect, useRef, useState, type CSSProperties } from 'react';
import { getMusicSignal, subscribeToMusicSignal } from '../backend/api';
import { appendMusicSignal, musicSignalLevel, musicSignalEndAt, type MusicSignal, type MusicStatus } from '../domain/music';

const traces = [
  { key: 'low', color: '#c1a16a' },
  { key: 'mid', color: '#7eabb6' },
  { key: 'high', color: '#af839d' },
  { key: 'energy', color: '#b8bec6' },
] as const;

export function MusicSignalView({ status }: { status: MusicStatus }) {
  const [history, setHistory] = useState<MusicSignal[]>([]);
  const canvasRef = useRef<HTMLCanvasElement>(null);
  const latest = history[history.length - 1];
  const running = status.state === 'running';

  useEffect(() => {
    if (!running) {
      setHistory([]);
      return;
    }
    let disposed = false;
    const receive = (point: MusicSignal) => {
      if (!disposed) setHistory((current) => appendMusicSignal(current, point, status.runId));
    };
    const unsubscribe = subscribeToMusicSignal(receive);
    void getMusicSignal().then((point) => { if (point) receive(point); }).catch(() => undefined);
    return () => { disposed = true; unsubscribe?.(); };
  }, [status.runId, running]);

  useEffect(() => {
    const canvas = canvasRef.current;
    if (!canvas) return;
    const receivedAt = performance.now();
    const reduceMotion = window.matchMedia('(prefers-reduced-motion: reduce)').matches;
    let animation = 0;
    let lastDraw = -Infinity;
    const draw = () => {
      if (document.hidden) return;
      const { width, height } = canvas.getBoundingClientRect();
      if (!width || !height) return;
      const ratio = window.devicePixelRatio || 1;
      const pixelWidth = Math.round(width * ratio), pixelHeight = Math.round(height * ratio);
      if (canvas.width !== pixelWidth || canvas.height !== pixelHeight) {
        canvas.width = pixelWidth; canvas.height = pixelHeight;
      }
      const ctx = canvas.getContext('2d');
      if (!ctx) return;
      ctx.setTransform(ratio, 0, 0, ratio, 0, 0);
      ctx.clearRect(0, 0, width, height);
      ctx.strokeStyle = '#ffffff10';
      ctx.lineWidth = 1;
      for (const level of [0, .25, .5, .75, 1]) {
        ctx.strokeStyle = level === 0 || level === 1 ? '#ffffff24' : '#ffffff10';
        const y = 4 + (1 - level) * (height - 8);
        ctx.beginPath(); ctx.moveTo(0, y); ctx.lineTo(width, y); ctx.stroke();
      }
      if (!running || !history.length) return;
      const end = musicSignalEndAt(history[history.length - 1].atMs, performance.now() - receivedAt, running, reduceMotion);
      const xAt = (at: number) => width - 2 - (end - at) / 12000 * (width - 4);
      for (const trace of [...traces].reverse()) {
        ctx.strokeStyle = trace.color;
        ctx.globalAlpha = trace.key === 'energy' ? .35 : .9;
        ctx.lineWidth = trace.key === 'energy' ? 1 : 1.5;
        ctx.beginPath();
        let previous: { x: number; y: number } | undefined;
        for (const point of history) {
          const x = xAt(point.atMs);
          const y = 4 + (1 - musicSignalLevel(point[trace.key])) * (height - 8);
          if (!previous) ctx.moveTo(x, y);
          else {
            const midX = (previous.x + x) / 2;
            ctx.bezierCurveTo(midX, previous.y, midX, y, x, y);
          }
          previous = { x, y };
        }
        ctx.stroke();
      }
      ctx.globalAlpha = 1;
    };
    const animate = (now: number) => {
      if (!running || reduceMotion || document.hidden || now - receivedAt > 250) { animation = 0; return; }
      if (now - lastDraw >= 1000 / 30) { draw(); lastDraw = now; }
      animation = requestAnimationFrame(animate);
    };
    const visibility = () => {
      if (animation) cancelAnimationFrame(animation);
      animation = 0;
      if (!document.hidden) { draw(); animation = requestAnimationFrame(animate); }
    };
    draw();
    lastDraw = performance.now();
    animation = requestAnimationFrame(animate);
    const observer = new ResizeObserver(draw);
    observer.observe(canvas);
    document.addEventListener('visibilitychange', visibility);
    return () => { observer.disconnect(); cancelAnimationFrame(animation); document.removeEventListener('visibilitychange', visibility); };
  }, [history, running]);

  return <section className="music-signal" aria-label="Music signal" data-running={running}>
    <div className="music-signal-heading"><span>signal</span><span className="mono" title="Microphone input RMS level relative to digital full scale">{latest && running ? `${Math.round(latest.inputDb)} dB` : running ? 'listening' : 'stopped'}</span></div>
    <canvas ref={canvasRef} role="img" aria-label="Low, mid, high and energy history over the last 12 seconds" />
    <div className="music-signal-legend">{traces.map((trace) => {
      const value = latest && running ? musicSignalLevel(latest[trace.key]) : 0;
      return <div key={trace.key} className="music-signal-meter" style={{ '--trace-color': trace.color } as CSSProperties}><span>{trace.key}</span><div role="meter" aria-label={trace.key === 'energy' ? 'Music energy' : `${trace.key} band activity`} aria-valuemin={0} aria-valuemax={100} aria-valuenow={Math.round(value * 100)} title={trace.key === 'energy' ? 'Maestro interpreted energy' : 'Normalized band activity from Maestro'}><span style={{ width: `${value * 100}%` }} /></div></div>;
    })}</div>
  </section>;
}
