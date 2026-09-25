import assert from "node:assert/strict";
import test from "node:test";

import {LiveHeightTracker, LiveTrail} from "./static/js/live_height_tracker.js";

/**
 * Ground [0,100]² at height 0, a ramp [100,120] rising 0→10 that shares its edge with the
 * ground, a floating bridge [40,60] at height 10 over the ground, and a floating plate
 * [112,120] at height 6 under the top of the ramp.
 */
function makeMesh() {
  const vertices = [];
  const indices = [];
  const vertex = (u, v, height) => {
    vertices.push(u, v, height);
    return vertices.length / 3 - 1;
  };
  const quad = (a, b, c, d) => indices.push(a, b, c, a, c, d);

  const g0 = vertex(0, 0, 0);
  const g1 = vertex(100, 0, 0);
  const g2 = vertex(100, 100, 0);
  const g3 = vertex(0, 100, 0);
  quad(g0, g1, g2, g3);
  quad(g1, vertex(120, 0, 10), vertex(120, 100, 10), g2);
  quad(vertex(40, 0, 10), vertex(60, 0, 10), vertex(60, 100, 10), vertex(40, 100, 10));
  quad(vertex(112, 0, 6), vertex(120, 0, 6), vertex(120, 100, 6), vertex(112, 100, 6));

  return {
    vertices: new Float32Array(vertices),
    indices: new Uint32Array(indices),
    vertexCount: vertices.length / 3,
    triangleCount: indices.length / 3,
  };
}

const tracker = new LiveHeightTracker(makeMesh());
const heights = (faces) => faces.map((face) => face.height);

test("facesAt lists every stacked face, highest first", () => {
  assert.deepEqual(heights(tracker.facesAt(50, 50)), [10, 0]);
  assert.deepEqual(heights(tracker.facesAt(20, 50)), [0]);
  assert.equal(tracker.facesAt(110, 50).length, 1);
  assert.ok(Math.abs(tracker.facesAt(110, 50)[0].height - 5) < 1e-6);
});

test("facesAt snaps to a face edge within the tolerance and rejects points beyond it", () => {
  assert.deepEqual(heights(tracker.facesAt(-1, 50)), [0]);
  assert.deepEqual(tracker.facesAt(-3, 50), []);
  assert.deepEqual(tracker.facesAt(NaN, 50), []);
});

test("resolve prefers the face connected to the previous one over the closer height", () => {
  const onRamp = tracker.resolve(null, 110, 50).entry;
  assert.ok(Math.abs(onRamp.height - 5) < 1e-6);
  // Plate at 6 is closer to 5 than the ramp's 7.5, but only the ramp shares edges with it.
  const next = tracker.resolve(onRamp, 115, 50).entry;
  assert.ok(Math.abs(next.height - 7.5) < 1e-6);
  assert.equal(next.onMesh, true);
});

test("resolve keeps the ground when walking under the bridge", () => {
  const ground = tracker.resolve(null, 20, 50).entry;
  const under = tracker.resolve(ground, 45, 50).entry;
  assert.equal(under.height, 0);
});

test("resolve holds the previous height off-mesh and asks for an ambiguous start", () => {
  const ground = tracker.resolve(null, 20, 50).entry;
  const off = tracker.resolve(ground, -10, 50).entry;
  assert.equal(off.height, 0);
  assert.equal(off.onMesh, false);
  assert.equal(off.tri, ground.tri);
  const start = tracker.resolve(null, 50, 50);
  assert.equal(start.entry, null);
  assert.deepEqual(heights(start.choices), [10, 0]);
  assert.deepEqual(tracker.resolve(null, -10, 50), {entry: null, choices: null});
});

test("LiveTrail resolves incrementally and skips an off-mesh start", () => {
  const trail = new LiveTrail(tracker);
  assert.equal(trail.update([[-10, 50]]), false);
  assert.equal(trail.entries.length, 0);
  assert.equal(
    trail.update([
      [-10, 50],
      [20, 50],
      [45, 50],
    ]),
    false,
  );
  assert.deepEqual(
    trail.entries.map((entry) => entry.height),
    [0, 0],
  );
  assert.equal(trail.consumed, 3);
  assert.equal(trail.heightFor(46, 50), 0);
});

test("LiveTrail waits for a start choice, then lifts the buffered points onto that floor", () => {
  const trail = new LiveTrail(tracker);
  assert.equal(trail.update([[50, 50]]), true);
  assert.equal(trail.pending, true);
  assert.deepEqual(heights(trail.pendingChoices), [10, 0]);
  assert.equal(trail.heightFor(50, 50), null);
  const points = [
    [50, 50],
    [55, 50],
    [65, 50],
  ];
  assert.equal(trail.update(points), false);
  assert.equal(trail.entries.length, 0);
  trail.chooseStart(10, points);
  assert.equal(trail.pending, false);
  assert.deepEqual(
    trail.entries.map((entry) => entry.height),
    [10, 10, 0],
  );
  // A trail that restarts elsewhere forgets the choice and asks again.
  assert.equal(trail.update([[50, 60]]), true);
  assert.equal(trail.pending, true);
});

test("LiveTrail restarts when the prefix is rewritten", () => {
  const trail = new LiveTrail(tracker);
  trail.update([
    [20, 50],
    [30, 50],
  ]);
  assert.equal(trail.entries.length, 2);
  trail.update([[25, 50]]);
  assert.equal(trail.entries.length, 1);
  assert.equal(trail.entries[0].u, 25);
  trail.update([]);
  assert.equal(trail.entries.length, 0);
});
