import assert from 'node:assert/strict';
import test from 'node:test';

import {
  measureZiplinePair,
  nextZiplineMeasurementSelection,
  projectZiplineRecords,
} from './static/js/zipline_records.js';

const frames = {
  types: [
    {name: '滑索架', template_id: 'zipline', max_span: 80, footprint: [3, 3]},
    {name: '长距滑索架', template_id: 'long-zipline', max_span: 110, footprint: [5, 7]},
  ],
  frames: [
    {
      zone_name: 'map02base',
      map_id: 'map02',
      plane: [0.75, 0, 1536, 0, -0.75, 1344],
      height_scale: 2,
      height_offset: 1,
      template_ids: ['zipline'],
    },
  ],
};

test('projects only matching map and template marks into base coordinates', () => {
  const records = {
    maps: [
      {
        map_id: 'map02',
        marks: [
          {level_id: 'map02_lv002', template_id: 'zipline', x: -1511, y: 322, z: -541},
          {level_id: 'map02_lv002', template_id: 'power', x: 1, y: 2, z: 3},
        ],
      },
      {map_id: 'map01', marks: [{template_id: 'zipline', x: 0, y: 0, z: 0}]},
    ],
  };

  assert.deepEqual(projectZiplineRecords(records, frames, 'map02base'), [
    {
      measureKey: 'record:0',
      point: [402.75, 1749.75],
      height: 645,
      world: [-1511, 322, -541],
      mapId: 'map02',
      levelId: 'map02_lv002',
      templateId: 'zipline',
    },
  ]);
  assert.deepEqual(projectZiplineRecords(records, frames, 'map01base'), []);
});

test('measures the possible center span range and reports its components', () => {
  const result = measureZiplinePair(
    {point: [0, 0], world: [0, 0, 0], templateId: 'zipline', levelId: 'level-a'},
    {point: [6, 8], world: [30, 40, 0], templateId: 'zipline', levelId: 'level-a'},
    frames,
  );

  assert.equal(result.worldDistance, 50);
  assert.equal(result.horizontalDistance, 30);
  assert.equal(result.heightDelta, 40);
  assert.equal(result.minimumWorldDistance, Math.hypot(28, 40));
  assert.equal(result.maximumWorldDistance, Math.hypot(32, 40, 2));
  assert.equal(result.minimumHorizontalDistance, 28);
  assert.equal(result.maximumHorizontalDistance, Math.hypot(32, 2));
  assert.deepEqual([result.uncertaintyX, result.uncertaintyZ], [2, 2]);
  assert.equal(result.baseDistance, 10);
  assert.deepEqual([result.deltaX, result.deltaY, result.deltaZ], [30, 40, 0]);
  assert.equal(result.maxSpan, 80);
  assert.equal(result.geometryConnected, true);
});

test('uses the minimum possible center span for the runtime geometry verdict', () => {
  const result = measureZiplinePair(
    {point: [0, 0], world: [0, 0, 0], templateId: 'zipline', levelId: 'level-a'},
    {point: [81, 0], world: [81, 0, 0], templateId: 'zipline', levelId: 'level-a'},
    frames,
  );

  assert.equal(result.worldDistance, 81);
  assert.equal(result.minimumWorldDistance, 79);
  assert.equal(result.maximumWorldDistance, Math.hypot(83, 2));
  assert.equal(result.geometryConnected, true);
  assert.match(result.geometryReason, /79\.00 m 不超过 80\.00 m/);
});

test('combines both footprints when measuring towers of different types', () => {
  const result = measureZiplinePair(
    {point: [0, 0], world: [0, 0, 0], templateId: 'zipline', levelId: 'level-a'},
    {point: [20, 30], world: [20, 0, 30], templateId: 'long-zipline', levelId: 'level-a'},
    frames,
  );

  assert.deepEqual([result.uncertaintyX, result.uncertaintyZ], [3, 4]);
  assert.equal(result.minimumWorldDistance, Math.hypot(17, 26));
  assert.equal(result.maximumWorldDistance, Math.hypot(23, 34));
  assert.equal(result.minimumHorizontalDistance, Math.hypot(17, 26));
  assert.equal(result.maximumHorizontalDistance, Math.hypot(23, 34));
  assert.equal(result.maxSpan, null);
  assert.equal(result.geometryConnected, false);
  assert.equal(result.geometryReason, '滑索架类型不同');
});

test('rejects geometric links that differ in type, level, or span', () => {
  const tower = {point: [0, 0], world: [0, 0, 0], templateId: 'zipline', levelId: 'level-a'};
  assert.equal(
    measureZiplinePair(tower, {...tower, world: [83, 0, 0]}, frames).geometryConnected,
    false,
  );
  assert.equal(
    measureZiplinePair(tower, {...tower, world: [1, 0, 0], templateId: 'long-zipline'}, frames)
      .geometryConnected,
    false,
  );
  assert.equal(
    measureZiplinePair(tower, {...tower, world: [1, 0, 0], levelId: 'level-b'}, frames).geometryConnected,
    false,
  );
});

test('keeps base distance when world coordinates are unavailable', () => {
  const result = measureZiplinePair({point: [0, 0]}, {point: [3, 4]}, frames);
  assert.equal(result.baseDistance, 5);
  assert.equal(result.worldDistance, null);
  assert.equal(result.minimumWorldDistance, null);
  assert.equal(result.maximumWorldDistance, null);
  assert.equal(result.geometryConnected, null);
});

test('cycles an A/B measurement selection across clicks', () => {
  assert.deepEqual(nextZiplineMeasurementSelection([], 'a'), ['a']);
  assert.deepEqual(nextZiplineMeasurementSelection(['a'], 'a'), []);
  assert.deepEqual(nextZiplineMeasurementSelection(['a'], 'b'), ['a', 'b']);
  assert.deepEqual(nextZiplineMeasurementSelection(['a', 'b'], 'c'), ['c']);
});
