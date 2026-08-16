import assert from 'node:assert/strict';
import test from 'node:test';

import {
  cardinalHeading,
  headingFromVector,
  headingVector,
  normalizeHeading,
  transformHeading,
} from './static/js/heading.js';
import { RecordingController } from './static/js/ui/recording.js';
import { PositionReadout } from './static/js/ui/position.js';

function fakeElement() {
  const classes = new Set();
  return {
    dataset: {},
    style: {},
    textContent: '',
    attributes: {},
    classList: {
      add: (name) => classes.add(name),
      remove: (name) => classes.delete(name),
      toggle: (name, enabled) => (enabled ? classes.add(name) : classes.delete(name)),
      contains: (name) => classes.has(name),
    },
    addEventListener: () => {},
    setAttribute(name, value) {
      this.attributes[name] = value;
    },
  };
}

test('normalizes headings and assigns eight-way compass labels', () => {
  assert.equal(normalizeHeading(360), 0);
  assert.equal(normalizeHeading(-45), 315);
  assert.equal(normalizeHeading(null), null);
  assert.equal(cardinalHeading(0), '北');
  assert.equal(cardinalHeading(44.9), '东北');
  assert.equal(cardinalHeading(270), '西');
});

test('uses the MapLocator north-up clockwise vector convention', () => {
  const north = headingVector(0);
  const east = headingVector(90);
  assert.ok(north);
  assert.ok(east);
  assert.ok(Math.abs(north[0]) < 1e-12);
  assert.ok(Math.abs(north[1] + 1) < 1e-12);
  assert.ok(Math.abs(east[0] - 1) < 1e-12);
  assert.ok(Math.abs(east[1]) < 1e-12);
  assert.ok(Math.abs(headingFromVector(1, 0) - 90) < 1e-12);
});

test('transforms the heading with the displayed coordinate frame', () => {
  assert.equal(transformHeading(0, (x, y) => [x, y]), 0);
  assert.equal(transformHeading(90, (x, y) => [-x, y]), 270);
  assert.ok(Math.abs(transformHeading(45, (x, y) => [2 * x, y]) - 63.43494882292201) < 1e-9);
});

test('renders a structured position and rotates the compass arrow', () => {
  const root = fakeElement();
  const coordinates = fakeElement();
  const heading = fakeElement();
  const zone = fakeElement();
  const arrow = fakeElement();
  const readout = new PositionReadout({ root, coordinates, heading, zone, arrow });

  assert.equal(readout.update({ x: 724.05, y: 2406.82, zone: 'Wuling_Base', rot: 302.7 }), true);
  assert.equal(coordinates.textContent, '[724.05, 2406.82]');
  assert.equal(heading.textContent, '朝向 西北 302.7°');
  assert.equal(zone.textContent, 'Wuling_Base');
  assert.equal(arrow.style.transform, 'rotate(302.7deg)');
  assert.equal(root.dataset.state, 'active');
  assert.equal(root.classList.contains('heading-unavailable'), false);
});

test('forwards structured WebSocket position messages', () => {
  const received = [];
  const controller = new RecordingController({
    btnStart: fakeElement(),
    btnStop: fakeElement(),
    appEl: fakeElement(),
    connection: {},
    onPosition: (fix) => received.push(fix),
  });

  controller._handleMessage({ type: 'position', x: 1, y: 2, zone: 'Wuling_Base', rot: 90 });
  assert.deepEqual(received, [{ x: 1, y: 2, zone: 'Wuling_Base', rot: 90 }]);
});
