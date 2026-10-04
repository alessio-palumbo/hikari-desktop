import assert from 'node:assert/strict';
import test from 'node:test';
import { previewCanvasLayout } from '../dist-test/domain/previewLayout.js';
import { DeviceKind } from '../dist-test/domain/lifx.js';

test('ordinary matrix previews use the same capped pixel size regardless of zone count', () => {
  const layouts = [[16, 8], [3, 9], [5, 11]].map(([width, height]) => previewCanvasLayout(width, height, DeviceKind.Matrix, 280));
  assert.deepEqual(layouts.map((layout) => layout.cellWidth), [12, 12, 12]);
  assert.ok(layouts.every((layout) => layout.cellWidth === layout.cellHeight));
  assert.ok(layouts.every((layout) => layout.left > 0));
});

test('strips have the same height with 24, 32, or many zones', () => {
  for (const zones of [24, 32, 128, 4096]) {
    const layout = previewCanvasLayout(zones, 1, DeviceKind.Multizone, 280);
    assert.equal(layout.height, 18);
    assert.equal(layout.cellHeight, 18);
    assert.equal(layout.cellWidth * zones, 280);
    assert.equal(layout.gap, 0);
  }
});

test('small matrices are not enlarged to fill the inspector', () => {
  assert.equal(previewCanvasLayout(2, 2, DeviceKind.Matrix, 280).cellWidth, 12);
  assert.equal(previewCanvasLayout(2, 2, DeviceKind.Matrix, 400).cellWidth, 12);
});

test('large or narrow matrix previews shrink uniformly to fit', () => {
  for (const [columns, rows, width] of [[32, 32, 280], [16, 8, 120], [4096, 1, 280], [1, 4096, 280]]) {
    const layout = previewCanvasLayout(columns, rows, DeviceKind.Matrix, width);
    assert.equal(layout.cellWidth, layout.cellHeight);
    assert.ok(layout.left >= -0.001);
    assert.ok(layout.height <= 160);
    assert.ok(columns * layout.cellWidth + (columns - 1) * layout.gap <= width + 0.001);
  }
});

test('single-zone preview remains compact', () => {
  const layout = previewCanvasLayout(1, 1, DeviceKind.Single, 280);
  assert.equal(layout.cellWidth, 18);
  assert.equal(layout.height, 18);
});
