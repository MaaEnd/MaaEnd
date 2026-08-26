import assert from 'node:assert/strict';
import test from 'node:test';

import {projectZiplineRecords} from './static/js/zipline_records.js';

const frames = {
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
