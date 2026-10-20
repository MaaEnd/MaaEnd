import assert from "node:assert/strict";
import test from "node:test";

import {
  buildEditPreviewPlan,
  routePreviewData,
  routePreviewForDisplay,
  routePreviewStatus,
} from "./static/js/edit_preview.js";

test("manual preview start keeps every authored point as a planning target", () => {
  const points = [
    {x: 30, y: 40, zone: "map02_2f"},
    {x: 50, y: 60, zone: "map02_2f"},
  ];

  const plan = buildEditPreviewPlan(points, {
    position: [10, 20],
    positionZone: "map02_2f",
  });

  assert.deepEqual(plan, {
    ok: true,
    position: [10, 20],
    positionZone: "map02_2f",
    startDeckY: null,
    targets: points,
    explicit: true,
  });
  assert.notEqual(plan.targets, points);
});

test("manual preview start supports planning to one authored target", () => {
  const point = {x: 30, y: 40, zone: "map02_2f"};
  const plan = buildEditPreviewPlan([point], {position: [10, 20], positionZone: "map02base"});

  assert.equal(plan.ok, true);
  assert.deepEqual(plan.targets, [point]);
});

test("default preview start preserves the first-point compatibility behavior", () => {
  const points = [
    {x: 10, y: 20, zone: "map02base", target_deck_y: 325.79},
    {x: 30, y: 40, zone: "map02base"},
  ];

  assert.deepEqual(buildEditPreviewPlan(points), {
    ok: true,
    position: [10, 20],
    positionZone: "map02base",
    startDeckY: 325.79,
    targets: [points[1]],
    explicit: false,
  });
});

test("default preview asks for a manual start when only one target exists", () => {
  const plan = buildEditPreviewPlan([{x: 30, y: 40, zone: "map02base"}]);

  assert.equal(plan.ok, false);
  assert.match(plan.error, /设置规划起点/);
});

test("runtime preview responses fill every overlay field", () => {
  const route = routePreviewData({ok: true, points: [[1, 2]]});

  assert.deepEqual(route, {points: [[1, 2]], walk_segments: [], zipline_segments: [], diagnostics: [], zipline: {}});
  assert.deepEqual(routePreviewStatus(route, {expanded_waypoints: 7}, "片段"), [
    "片段：展开为 7 个运行时路点。",
    "#10b981",
  ]);
  assert.deepEqual(routePreviewStatus({...route, zipline: {no_data: true}}, {}, "片段"), [
    "片段：当前区域没有可用的滑索记录，已回退为步行路线。",
    "#f59e0b",
  ]);
});

test("runtime preview display projects the route and the failure gap into the view frame", () => {
  const shift = ([x, y]) => [x + 100, y];
  const route = routePreviewData({
    points: [
      [0, 0],
      [5, 0],
    ],
    walk_segments: [
      [
        [0, 0],
        [5, 0],
      ],
    ],
  });
  const shown = routePreviewForDisplay(route, null, shift);

  assert.deepEqual(shown.previewPoints, [
    [100, 0],
    [105, 0],
  ]);
  assert.deepEqual(shown.walkSegments, [
    [
      [100, 0],
      [105, 0],
    ],
  ]);
  assert.equal(shown.showPlannedPath, true);
  assert.equal(routePreviewForDisplay(route, null, shift, {debugOptions: {planned: false}}).showPlannedPath, false);

  const failed = routePreviewForDisplay(null, {gap_start: [1, 1], gap_goal: null, segment_start: [2, 2]}, shift);
  assert.equal(failed.hasRoute, false);
  assert.deepEqual(failed.failure.gap_start, [101, 1]);
  assert.deepEqual(failed.failure.segment_start, [102, 2]);
  assert.equal(failed.failure.gap_goal, null);
  assert.equal(routePreviewForDisplay(null, null, shift), null);
});
