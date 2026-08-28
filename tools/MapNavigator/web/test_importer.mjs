import assert from 'node:assert/strict';
import test from 'node:test';

import {
  collectAstarImportBasePoints,
  completeAstarImportWithStart,
  filterProjectNodes,
  normalizeAssertTarget,
  readClipboardText,
} from './static/js/ui/importer.js';

const routes = [
  {
    resource_path: 'assets/resource/pipeline/AutoCollect/AutoCollectRoute7.json',
    node_name: 'AutoCollectRoute7GotoFindLine4',
    kind: 'path',
    zone_ids: ['Wuling_Base'],
  },
  {
    resource_path: 'assets/resource/pipeline/EnvironmentMonitoring/Wuling.json',
    node_name: 'EnvironmentMonitoringGotoTarget',
    kind: 'path',
    zone_ids: ['Wuling_Base'],
  },
  {
    resource_path: 'assets/resource/pipeline/AutoCollect/AutoCollectRoute7.json',
    node_name: 'AutoCollectRoute7AssertLocation',
    kind: 'assert',
    zone_id: 'Wuling_Base',
  },
];

test('project route filter matches resource paths case-insensitively', () => {
  assert.deepEqual(filterProjectNodes(routes, 'AUTOCOLLECT', 'path'), [routes[0]]);
});

test('project route filter matches Pipeline node names', () => {
  assert.deepEqual(filterProjectNodes(routes, 'gototarget', 'path'), [routes[1]]);
});

test('blank project route filter keeps the backend order', () => {
  assert.deepEqual(filterProjectNodes(routes, '  ', 'path'), routes.slice(0, 2));
});

test('project node filter separates assertions and searches by zone', () => {
  assert.deepEqual(filterProjectNodes(routes, 'wuling_base', 'assert'), [routes[2]]);
});

test('A* import resolves target tiers and keeps only the first navmesh geometry', () => {
  const zoneIds = { Base: 1, Tier: 2, Other: 3 };
  const result = collectAstarImportBasePoints(
    [
      { x: 10, y: 20, zone: 'Base', target_tier: 'Tier', target_deck_y: 111.5 },
      { x: 30, y: 40, zone: 'Base', target_deck_y: '222.5' },
      { x: 50, y: 60, zone: 'Other', target_deck_y: 333.5 },
      { x: 70, y: 80, zone: 'Missing' },
    ],
    (zone) => zoneIds[zone] ?? Number.NaN,
    (zoneId) => (zoneId === 2 ? 1 : zoneId),
    (zoneId, x, y) => (zoneId === 2 ? [x + 100, y + 200] : [x, y]),
  );

  assert.deepEqual(result, {
    firstZoneId: 2,
    basePoints: [
      [110, 220],
      [30, 40],
    ],
    decks: [111.5, 222.5],
    skipped: 2,
  });
});

test('A* import prepends a manual start to every pending target', () => {
  assert.deepEqual(
    completeAstarImportWithStart(
      [5, 6],
      [
        [10, 20],
        [30, 40],
      ],
      [111.5, 222.5],
      (x, y) => [x + 100, y + 200],
    ),
    {
      points: [
        [5, 6],
        [110, 220],
        [130, 240],
      ],
      decks: [null, 111.5, 222.5],
    },
  );
  assert.equal(completeAstarImportWithStart([5, 6], [], [], (x, y) => [x, y]), null);
});

test('A* import keeps missing and invalid target decks aligned as null', () => {
  const result = collectAstarImportBasePoints(
    [
      { x: 10, y: 20, zone: 'Base' },
      { x: 30, y: 40, zone: 'Base', target_deck_y: 'invalid' },
    ],
    () => 1,
    (zoneId) => zoneId,
    (_zoneId, x, y) => [x, y],
  );

  assert.deepEqual(result.decks, [null, null]);
});

test('clipboard reader returns the current JSON text unchanged', async () => {
  const text = '{"path":[[1,2]]}';
  assert.equal(await readClipboardText({ readText: async () => text }), text);
});

test('clipboard reader rejects empty content', async () => {
  await assert.rejects(readClipboardText({ readText: async () => ' \r\n ' }), /没有可导入的 JSON 内容/);
});

test('clipboard reader reports an unavailable Clipboard API', async () => {
  await assert.rejects(readClipboardText(undefined), /不支持读取剪贴板/);
});

test('clipboard reader preserves read failures for the UI to explain', async () => {
  const denied = Object.assign(new Error('permission denied'), { name: 'NotAllowedError' });
  await assert.rejects(readClipboardText({ readText: async () => Promise.reject(denied) }), denied);
});

test('assert import normalizes four finite numeric target values', () => {
  assert.deepEqual(normalizeAssertTarget(['1', 2, '30.5', 40]), [1, 2, 30.5, 40]);
  assert.deepEqual(normalizeAssertTarget([1, 2, 30, 40, 50]), [1, 2, 30, 40]);
});

test('assert import rejects incomplete, non-finite, and empty rectangles', () => {
  assert.equal(normalizeAssertTarget([1, 2, 30]), null);
  assert.equal(normalizeAssertTarget([1, 2, 'invalid', 40]), null);
  assert.equal(normalizeAssertTarget([1, 2, 30, Number.POSITIVE_INFINITY]), null);
  assert.equal(normalizeAssertTarget([null, 2, 30, 40]), null);
  assert.equal(normalizeAssertTarget([1, 2, 0, 40]), null);
});
