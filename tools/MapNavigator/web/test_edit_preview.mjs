import assert from "node:assert/strict";
import test from "node:test";

import {buildEditPreviewPlan} from "./static/js/edit_preview.js";

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
    {x: 10, y: 20, zone: "map02base"},
    {x: 30, y: 40, zone: "map02base"},
  ];

  assert.deepEqual(buildEditPreviewPlan(points), {
    ok: true,
    position: [10, 20],
    positionZone: "map02base",
    targets: [points[1]],
    explicit: false,
  });
});

test("default preview asks for a manual start when only one target exists", () => {
  const plan = buildEditPreviewPlan([{x: 30, y: 40, zone: "map02base"}]);

  assert.equal(plan.ok, false);
  assert.match(plan.error, /设置规划起点/);
});
