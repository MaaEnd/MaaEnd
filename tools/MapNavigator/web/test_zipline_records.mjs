import assert from 'node:assert/strict';
import test from 'node:test';

import {
  measureZiplinePair,
  nextZiplineMeasurementSelection,
  projectZiplineRecords,
} from './static/js/zipline_records.js';

const frames = {
  types: [
    {name: '滑索架', template_id: 'zipline', max_span: 80},
    {name: '长距滑索架', template_id: 'long-zipline', max_span: 110},
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

test('measures the runtime world span and reports its components', () => {
  const result = measureZiplinePair(
    {point: [0, 0], world: [0, 0, 0], templateId: 'zipline', levelId: 'level-a'},
    {point: [6, 8], world: [30, 40, 0], templateId: 'zipline', levelId: 'level-a'},
    frames,
  );

  assert.equal(result.worldDistance, 50);
  assert.equal(result.horizontalDistance, 30);
  assert.equal(result.heightDelta, 40);
  assert.equal(result.baseDistance, 10);
  assert.deepEqual([result.deltaX, result.deltaY, result.deltaZ], [30, 40, 0]);
  assert.equal(result.maxSpan, 80);
  assert.equal(result.geometryConnected, true);
});

test('rejects geometric links that differ in type, level, or span', () => {
  const tower = {point: [0, 0], world: [0, 0, 0], templateId: 'zipline', levelId: 'level-a'};
  assert.equal(
    measureZiplinePair(tower, {...tower, world: [81, 0, 0]}, frames).geometryConnected,
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
  assert.equal(result.geometryConnected, null);
});

test('cycles an A/B measurement selection across clicks', () => {
  assert.deepEqual(nextZiplineMeasurementSelection([], 'a'), ['a']);
  assert.deepEqual(nextZiplineMeasurementSelection(['a'], 'a'), []);
  assert.deepEqual(nextZiplineMeasurementSelection(['a'], 'b'), ['a', 'b']);
  assert.deepEqual(nextZiplineMeasurementSelection(['a', 'b'], 'c'), ['c']);
});
