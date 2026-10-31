import assert from "node:assert/strict";
import test from "node:test";

import {
  BoxSelection,
  DRAG_ACTIVATION_DISTANCE,
  PanGesture,
  bindTemporaryPan,
  isFormField,
  isZoomInKey,
  isZoomOutKey,
  movedExceeded,
  wheelZoomFactor,
} from "./static/js/map_view.js";
import {PointEditing} from "./static/js/state.js";

function fakeCamera() {
  return {
    moves: [],
    panBy(dx, dy) {
      this.moves.push([dx, dy]);
    },
  };
}

test("a press only becomes a drag once either axis passes the activation distance", () => {
  assert.equal(movedExceeded(10, 10, 10 + DRAG_ACTIVATION_DISTANCE, 10 - DRAG_ACTIVATION_DISTANCE), false);
  assert.equal(movedExceeded(10, 10, 10 + DRAG_ACTIVATION_DISTANCE + 1, 10), true);
  assert.equal(movedExceeded(10, 10, 10, 10 - DRAG_ACTIVATION_DISTANCE - 1), true);
});

test("wheel zoom scales line and page deltas and clamps each event", () => {
  assert.equal(wheelZoomFactor({deltaMode: 0, deltaY: 0}, 600), 1);
  assert.ok(Math.abs(wheelZoomFactor({deltaMode: 0, deltaY: -100}, 600) - Math.exp(0.15)) < 1e-12);
  assert.ok(Math.abs(wheelZoomFactor({deltaMode: 1, deltaY: 3}, 600) - Math.exp(-3 * 16 * 0.0015)) < 1e-12);
  assert.equal(wheelZoomFactor({deltaMode: 2, deltaY: 1}, 600), 0.5);
  assert.equal(wheelZoomFactor({deltaMode: 0, deltaY: -10000}, 600), 2);
});

test("zoom keys accept the main row and the keypad", () => {
  for (const e of [{key: "+"}, {key: "="}, {key: "", code: "NumpadAdd"}]) assert.equal(isZoomInKey(e), true);
  for (const e of [{key: "-"}, {key: "_"}, {key: "", code: "NumpadSubtract"}]) assert.equal(isZoomOutKey(e), true);
  assert.equal(isZoomInKey({key: "1"}), false);
  assert.equal(isZoomOutKey({key: "1"}), false);
});

test("shortcuts stay quiet inside form controls", () => {
  for (const tagName of ["INPUT", "SELECT", "TEXTAREA"]) assert.equal(isFormField({tagName}), true);
  assert.equal(isFormField({tagName: "DIV", isContentEditable: true}), true);
  assert.equal(isFormField({tagName: "CANVAS"}), false);
  assert.equal(isFormField(null), false);
});

test("an armed pan waits for the threshold, then pans by the pointer delta", () => {
  const camera = fakeCamera();
  const canvas = {style: {cursor: ""}};
  const pan = new PanGesture(camera, canvas);

  pan.arm(100, 100);
  pan.move(103, 103);
  assert.equal(pan.active, false);
  assert.equal(pan.armed, true);

  pan.move(110, 100);
  assert.equal(pan.active, true);
  assert.equal(pan.armed, false);
  assert.equal(canvas.style.cursor, "grabbing");
  assert.deepEqual(camera.moves, []);

  pan.move(115, 95);
  assert.deepEqual(camera.moves, [[5, -5]]);
  pan.cancel();
  assert.equal(pan.active, false);
});

test("a started pan follows the pointer from the first move", () => {
  const camera = fakeCamera();
  const pan = new PanGesture(camera, {style: {}});
  pan.start(0, 0);
  pan.move(2, 1);
  pan.move(3, 4);
  assert.deepEqual(camera.moves, [
    [2, 1],
    [1, 3],
  ]);
});

test("a box that never leaves the press point closes as a click", () => {
  const box = new BoxSelection();
  box.start(50, 50);
  box.move(53, 52);
  assert.deepEqual(box.rect, {x0: 50, y0: 50, x1: 53, y1: 52});
  assert.deepEqual(box.finish(53, 52), {click: true, x0: 50, y0: 50, x1: 53, y1: 52});
  assert.equal(box.active, false);
  assert.equal(box.rect, null);
  assert.equal(box.finish(60, 60), null);

  box.start(50, 50);
  assert.equal(box.finish(80, 20).click, false);
});

test("box selection keeps local indices of the points inside the rect", () => {
  const points = [
    {x: 0, y: 0},
    {x: 10, y: 10},
    {x: 20, y: 5},
    {x: 30, y: 30},
  ];
  const identity = (x, y) => [x, y];
  assert.deepEqual(PointEditing.indicesInRect(points, [0, 1, 2, 3], identity, 25, 0, 5, 12), [1, 2]);
  assert.deepEqual(PointEditing.indicesInRect(points, [3, 1], identity, 0, 0, 40, 40), [0, 1]);
});

test("holding Alt swaps to the pan tool and restores the previous tool on release", () => {
  const previousWindow = globalThis.window;
  globalThis.window = new EventTarget();
  try {
    let tool = "select";
    const seen = [];
    const temporary = bindTemporaryPan({
      current: () => tool,
      panTool: () => "pan",
      setTool: (next) => {
        seen.push([next, temporary.savedTool]);
        tool = next;
      },
    });
    const key = (type, {key: name, target = null}) => {
      const event = new Event(type, {cancelable: true});
      event.key = name;
      Object.defineProperty(event, "target", {value: target});
      window.dispatchEvent(event);
      return event;
    };

    assert.equal(key("keydown", {key: "Alt"}).defaultPrevented, true);
    key("keydown", {key: "Alt"});
    assert.equal(tool, "pan");
    key("keyup", {key: "Alt"});
    assert.equal(tool, "select");
    assert.deepEqual(seen, [
      ["pan", "select"],
      ["select", null],
    ]);

    key("keydown", {key: "Alt", target: {tagName: "INPUT"}});
    assert.equal(tool, "select");
    assert.equal(temporary.savedTool, null);
  } finally {
    globalThis.window = previousWindow;
  }
});

test("extra pan keys work like Alt, and only the key that started the pan ends it", () => {
  const previousWindow = globalThis.window;
  globalThis.window = new EventTarget();
  try {
    let tool = "select";
    bindTemporaryPan({
      current: () => tool,
      panTool: () => "pan",
      setTool: (next) => (tool = next),
      keys: ["Alt", " "],
    });
    const key = (type, name, target = null) => {
      const event = new Event(type, {cancelable: true});
      event.key = name;
      Object.defineProperty(event, "target", {value: target});
      window.dispatchEvent(event);
      return event;
    };

    key("keydown", " ");
    assert.equal(tool, "pan");
    assert.equal(key("keydown", " ").defaultPrevented, true);
    key("keydown", "Alt");
    key("keyup", "Alt");
    assert.equal(tool, "pan");
    assert.equal(key("keyup", " ").defaultPrevented, true);
    assert.equal(tool, "select");

    key("keydown", " ", {tagName: "INPUT"});
    assert.equal(tool, "select");
    key("keydown", "x");
    assert.equal(tool, "select");
  } finally {
    globalThis.window = previousWindow;
  }
});
