import assert from "node:assert/strict";
import test from "node:test";

import {
  applyAssertRectDrag,
  assertRectCursor,
  hitTestAssertRect,
  normalizeAssertRect,
} from "./static/js/assert_rect.js";

const worldToCanvas = (x, y) => [x * 2 + 10, y * 2 + 20];

test("normalizes assert rectangles drawn in any direction", () => {
  assert.deepEqual(normalizeAssertRect([30, 40, 10, 20]), [10, 20, 30, 40]);
  assert.equal(normalizeAssertRect([0, 0, Number.NaN, 10]), null);
});

test("hit tests assert corners, edges, body, and empty canvas in CSS pixels", () => {
  const rect = [10, 20, 30, 40];
  assert.equal(hitTestAssertRect(rect, worldToCanvas, 30, 60), "nw");
  assert.equal(hitTestAssertRect(rect, worldToCanvas, 50, 60), "n");
  assert.equal(hitTestAssertRect(rect, worldToCanvas, 70, 100), "se");
  assert.equal(hitTestAssertRect(rect, worldToCanvas, 50, 80), "move");
  assert.equal(hitTestAssertRect(rect, worldToCanvas, 90, 80), null);
});

test("moves an assert rectangle without changing its size", () => {
  assert.deepEqual(applyAssertRectDrag([10, 20, 30, 40], "move", [15, 25], [20, 32]), [15, 27, 35, 47]);
});

test("resizes assert edges and corners, including crossing the opposite edge", () => {
  assert.deepEqual(applyAssertRectDrag([10, 20, 30, 40], "w", [10, 30], [5, 30]), [5, 20, 30, 40]);
  assert.deepEqual(applyAssertRectDrag([10, 20, 30, 40], "se", [30, 40], [45, 55]), [10, 20, 45, 55]);
  assert.deepEqual(applyAssertRectDrag([10, 20, 30, 40], "e", [30, 30], [5, 30]), [5, 20, 10, 40]);
});

test("maps assert drag kinds to platform resize cursors", () => {
  assert.equal(assertRectCursor("nw"), "nwse-resize");
  assert.equal(assertRectCursor("ne"), "nesw-resize");
  assert.equal(assertRectCursor("n"), "ns-resize");
  assert.equal(assertRectCursor("e"), "ew-resize");
  assert.equal(assertRectCursor("move"), "move");
});
