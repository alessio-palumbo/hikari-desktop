import type { Device, Matrix } from '../domain/lifx';
import { deviceColor, hsl } from '../domain/lifx';
import './DevicePreview.css';

export function DevicePreview({ device }: { device: Device }) {
  if (device.kind === 'single') {
    return <span className="preview-single" style={{ background: hsl(deviceColor(device)), opacity: device.on ? 0.45 + device.brightness * 0.55 : 0.15 }} />;
  }

  if (device.kind === 'multizone') {
    return (
      <span className="preview-zones" data-on={device.on ? 'true' : 'false'} style={{ opacity: device.on ? 0.35 + device.brightness * 0.65 : undefined }}>
        {device.zones?.map((zone, index) => <i key={index} style={{ background: hsl(zone) }} />)}
      </span>
    );
  }

  return <MatrixPreview chain={device.chain ?? []} on={device.on} brightness={device.brightness} />;
}

function MatrixPreview({ chain, on, brightness }: { chain: Matrix[]; on: boolean; brightness: number }) {
  if (!chain.length) return null;

  const minX = Math.min(...chain.map((matrix) => matrix.x));
  const minY = Math.min(...chain.map((matrix) => matrix.y));
  const maxX = Math.max(...chain.map((matrix) => matrix.x + matrix.w));
  const maxY = Math.max(...chain.map((matrix) => matrix.y + matrix.h));
  const width = maxX - minX;
  const height = maxY - minY;
  const cell = Math.max(2, Math.min(5, Math.floor(88 / width)));
  const gap = cell >= 4 ? 1 : 0;
  const step = cell + gap;

  return (
    <span
      className="preview-matrix"
      data-on={on ? 'true' : 'false'}
      style={{ width: width * step - gap, height: height * step - gap, opacity: on ? 0.35 + brightness * 0.65 : undefined }}
    >
      {chain.map((matrix) =>
        matrix.rows.flatMap((row, rowIndex) =>
          Array.from({ length: row.cols }, (_, columnIndex) => {
            const pixelIndex = matrix.rows.slice(0, rowIndex).reduce((sum, r) => sum + r.cols, 0) + columnIndex;
            const pixel = matrix.pixels[pixelIndex];
            return (
              <i
                key={`${matrix.id}-${pixelIndex}`}
                style={{
                  left: (matrix.x - minX + row.offset + columnIndex) * step,
                  top: (matrix.y - minY + rowIndex) * step,
                  width: cell,
                  height: cell,
                  background: hsl(pixel),
                }}
              />
            );
          }),
        ),
      )}
    </span>
  );
}
