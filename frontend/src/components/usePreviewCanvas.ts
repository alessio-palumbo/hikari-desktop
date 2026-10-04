import { useEffect, useRef, useState } from 'react';
import { DeviceKind, effectPreviewColor, type HslColor } from '../domain/lifx';
import { previewCanvasLayout } from '../domain/previewLayout';

export function usePreviewCanvas(columns: number | undefined, rows: number | undefined, kind: DeviceKind, visible = true) {
  const canvas = useRef<HTMLCanvasElement>(null);
  const [width, setWidth] = useState(280);
  useEffect(() => {
    const element = canvas.current;
    if (!element) return;
    const observer = new ResizeObserver(([entry]) => setWidth(Math.max(1, entry.contentRect.width)));
    observer.observe(element);
    return () => observer.disconnect();
  }, [columns, rows, kind, visible]);
  const layout = previewCanvasLayout(columns ?? 1, rows ?? 1, kind, width);
  return { canvas, layout };
}

export function drawPreviewFrame(canvas: HTMLCanvasElement, columns: number, cells: boolean[], colors: HslColor[], layout: ReturnType<typeof previewCanvasLayout>) {
  const context = canvas.getContext('2d');
  if (!context) return;
  const ratio = window.devicePixelRatio || 1;
  const width = Math.round(layout.width * ratio);
  const height = Math.round(layout.height * ratio);
  if (canvas.width !== width) canvas.width = width;
  if (canvas.height !== height) canvas.height = height;
  canvas.style.height = `${layout.height}px`;
  context.setTransform(ratio, 0, 0, ratio, 0, 0);
  context.clearRect(0, 0, layout.width, layout.height);
  colors.forEach((color, index) => {
    if (!cells[index]) return;
    context.fillStyle = effectPreviewColor(color);
    context.fillRect(layout.left + index % columns * (layout.cellWidth + layout.gap), layout.top + Math.floor(index / columns) * (layout.cellHeight + layout.gap), layout.cellWidth, layout.cellHeight);
  });
}
