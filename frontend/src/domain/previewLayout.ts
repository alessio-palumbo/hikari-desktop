import { DeviceKind } from './lifx.js';

export function previewCanvasLayout(columns: number, rows: number, kind: DeviceKind, availableWidth: number) {
  const width = Math.max(1, Math.floor(availableWidth));
  if (kind === DeviceKind.Multizone) {
    return { width, height: 18, cellWidth: width / columns, cellHeight: 18, gap: 0, left: 0, top: 0 };
  }
  // Match the list's capped-cell approach, at a more readable inspector scale.
  const gap = kind === DeviceKind.Single ? 0 : Math.min(1, width / (columns * 4), 160 / (rows * 4));
  const cell = Math.min(kind === DeviceKind.Single ? 18 : 12, (width - gap * (columns - 1)) / columns, (160 - gap * (rows - 1)) / rows);
  const contentWidth = columns * cell + (columns - 1) * gap;
  const height = Math.ceil(rows * cell + (rows - 1) * gap);
  return { width, height, cellWidth: cell, cellHeight: cell, gap, left: (width - contentWidth) / 2, top: 0 };
}
