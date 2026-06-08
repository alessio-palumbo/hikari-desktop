import { DeviceKind, type Device, type HslColor } from './lifx.js';

export type GradientStops = { start?: HslColor; end?: HslColor };
export type GradientDirection = 'e' | 'w' | 's' | 'n' | 'se' | 'nw' | 'ne' | 'sw';

export function initialPaintColor(device: Device): HslColor {
  if (device.kind === DeviceKind.Single && device.color) return device.color;
  if (device.kind === DeviceKind.Multizone && device.zones?.length) return device.zones[Math.floor(device.zones.length / 2)];
  if (device.kind === DeviceKind.Matrix && device.chain?.[0]?.pixels.length) {
    const pixels = device.chain[0].pixels;
    return pixels[Math.floor(pixels.length / 2)];
  }
  return { h: 38, s: 0.5, l: 0.55 };
}

export function kelvinToHsl(kelvin: number): HslColor {
  return { h: 0, s: 0, l: 0.72, kelvin };
}

export function applyDeviceColor(device: Device, color: HslColor): Device {
  const brightness = device.brightness > 0 ? device.brightness : Math.max(color.l, 0.55);
  const nextColor = { ...color, l: brightness };
  if (device.kind === DeviceKind.Single) {
    return { ...device, on: true, brightness, color: nextColor, kelvin: nextColor.kelvin ?? device.kelvin };
  }
  if (device.kind === DeviceKind.Multizone) {
    return {
      ...device,
      on: true,
      brightness,
      color: nextColor,
      kelvin: nextColor.kelvin ?? device.kelvin,
      zones: device.zones?.map(() => nextColor) ?? [],
    };
  }
  return {
    ...device,
    on: true,
    brightness,
    color: nextColor,
    kelvin: nextColor.kelvin ?? device.kelvin,
    chain: device.chain?.map((matrix) => ({ ...matrix, pixels: matrix.pixels.map(() => nextColor) })) ?? [],
  };
}

export function applyDeviceBrightness(device: Device, brightness: number): Device {
  const on = brightness > 0;
  return { ...device, on, brightness };
}

export function paintMultizoneBrush(device: Device, zoneIndex: number, color: HslColor): Device {
  const zones = [...(device.zones ?? [])];
  zones[zoneIndex] = color;
  return { ...device, zones };
}

export function paintMultizoneFill(device: Device, color: HslColor): Device {
  return { ...device, zones: device.zones?.map(() => color) ?? [] };
}

export function paintMultizoneGradient(device: Device, stops: GradientStops, direction: GradientDirection): Device | undefined {
  const gradient = multizoneGradient(device.zones ?? [], stops, direction);
  if (!gradient) return undefined;
  return { ...device, zones: gradient };
}

export function paintMatrixBrush(device: Device, matrixIndex: number, pixelIndex: number, color: HslColor): Device {
  return {
    ...device,
    chain: device.chain?.map((matrix, index) => {
      if (index !== matrixIndex) return matrix;
      const pixels = [...matrix.pixels];
      pixels[pixelIndex] = color;
      return { ...matrix, pixels };
    }) ?? [],
  };
}

export function paintMatrixFill(device: Device, matrixIndex: number, color: HslColor): Device {
  return {
    ...device,
    chain: device.chain?.map((matrix, index) => (index === matrixIndex ? { ...matrix, pixels: matrix.pixels.map(() => color) } : matrix)) ?? [],
  };
}

export function paintMatrixGradient(device: Device, matrixIndex: number, stops: GradientStops, direction: GradientDirection): Device | undefined {
  const sourceMatrix = device.chain?.[matrixIndex];
  if (!sourceMatrix) return undefined;
  const gradient = matrixGradient(sourceMatrix, stops, direction);
  if (!gradient) return undefined;
  return {
    ...device,
    chain: device.chain?.map((matrix, index) => (index === matrixIndex ? { ...matrix, pixels: gradient } : matrix)) ?? [],
  };
}

export function matrixGridCols(matrix: NonNullable<Device['chain']>[number]): number {
  return Math.max(1, ...matrix.rows.map((row) => Math.ceil(row.offset + row.cols)), Math.round(matrix.w));
}

function multizoneGradient(colors: HslColor[], stops: GradientStops, direction: GradientDirection): HslColor[] | undefined {
  if (!stops.start || !stops.end) return undefined;
  return colors.map((_, index) => {
    const t = index / Math.max(1, colors.length - 1);
    return interpolateHsl(stops.start!, stops.end!, direction === 'w' ? 1 - t : t);
  });
}

function matrixGradient(matrix: NonNullable<Device['chain']>[number], stops: GradientStops, direction: GradientDirection): HslColor[] | undefined {
  if (!stops.start || !stops.end) return undefined;
  const pixels = [...matrix.pixels];
  const width = matrixGridCols(matrix);
  const height = Math.max(1, matrix.rows.length);
  for (const [rowIndex, row] of matrix.rows.entries()) {
    const rowStart = matrix.rows.slice(0, rowIndex).reduce((sum, entry) => sum + entry.cols, 0);
    for (let columnIndex = 0; columnIndex < row.cols; columnIndex += 1) {
      const pixelIndex = rowStart + columnIndex;
      const x = row.offset + columnIndex;
      const y = rowIndex;
      pixels[pixelIndex] = interpolateHsl(stops.start, stops.end, gradientAmount(x, y, width, height, direction));
    }
  }
  return pixels;
}

function gradientAmount(x: number, y: number, width: number, height: number, direction: GradientDirection): number {
  const maxX = Math.max(1, width - 1);
  const maxY = Math.max(1, height - 1);
  const nx = x / maxX;
  const ny = y / maxY;
  const vectors: Record<GradientDirection, { x: number; y: number }> = {
    e: { x: 1, y: 0 },
    w: { x: -1, y: 0 },
    s: { x: 0, y: 1 },
    n: { x: 0, y: -1 },
    se: { x: 1, y: 1 },
    nw: { x: -1, y: -1 },
    ne: { x: 1, y: -1 },
    sw: { x: -1, y: 1 },
  };
  const vector = vectors[direction];
  const projection = nx * vector.x + ny * vector.y;
  const corners = [0, vector.x, vector.y, vector.x + vector.y];
  const min = Math.min(...corners);
  const max = Math.max(...corners);
  return (projection - min) / Math.max(1, max - min);
}

function interpolateHsl(from: HslColor, to: HslColor, amount: number): HslColor {
  const t = Math.max(0, Math.min(1, amount));
  return {
    h: from.h + (to.h - from.h) * t,
    s: from.s + (to.s - from.s) * t,
    l: from.l + (to.l - from.l) * t,
  };
}
