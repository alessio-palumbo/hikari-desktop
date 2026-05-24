import type { Device, Tile } from '../domain/lifx';
import { deviceColor, hsl } from '../domain/lifx';
import './DevicePreview.css';

export function DevicePreview({ device }: { device: Device }) {
  if (device.kind === 'single') {
    return <span className="preview-single" style={{ background: hsl(deviceColor(device)), opacity: device.on ? 0.45 + device.brightness * 0.55 : 0.15 }} />;
  }

  if (device.kind === 'multizone') {
    return (
      <span className="preview-zones" data-on={device.on ? 'true' : 'false'}>
        {device.zones?.map((zone, index) => <i key={index} style={{ background: hsl(zone, 0.4 + device.brightness * 0.25) }} />)}
      </span>
    );
  }

  return <MatrixPreview tiles={device.tiles ?? []} on={device.on} brightness={device.brightness} />;
}

function MatrixPreview({ tiles, on, brightness }: { tiles: Tile[]; on: boolean; brightness: number }) {
  if (!tiles.length) return null;

  const minX = Math.min(...tiles.map((tile) => tile.x));
  const minY = Math.min(...tiles.map((tile) => tile.y));
  const maxX = Math.max(...tiles.map((tile) => tile.x + tile.w));
  const maxY = Math.max(...tiles.map((tile) => tile.y + tile.h));
  const width = maxX - minX;
  const height = maxY - minY;
  const cell = Math.max(2, Math.min(5, Math.floor(88 / width)));
  const gap = cell >= 4 ? 1 : 0;
  const step = cell + gap;

  return (
    <span className="preview-matrix" data-on={on ? 'true' : 'false'} style={{ width: width * step - gap, height: height * step - gap }}>
      {tiles.map((tile) =>
        tile.rows.flatMap((row, rowIndex) =>
          Array.from({ length: row.cols }, (_, columnIndex) => {
            const pixelIndex = tile.rows.slice(0, rowIndex).reduce((sum, r) => sum + r.cols, 0) + columnIndex;
            const pixel = tile.pixels[pixelIndex];
            return (
              <i
                key={`${tile.id}-${pixelIndex}`}
                style={{
                  left: (tile.x - minX + row.offset + columnIndex) * step,
                  top: (tile.y - minY + rowIndex) * step,
                  width: cell,
                  height: cell,
                  background: hsl(pixel, 0.4 + brightness * 0.25),
                }}
              />
            );
          }),
        ),
      )}
    </span>
  );
}
