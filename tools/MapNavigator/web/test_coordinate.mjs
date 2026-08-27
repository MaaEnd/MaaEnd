import assert from 'node:assert/strict';
import test from 'node:test';

import {parsePastedCoordinatePair} from './static/js/ui/coordinate.js';

test('parses a formatted coordinate array containing non-breaking spaces', () => {
  const pasted = '[\n\u00a0        724.98,\n\u00a0        1596.8\n    ]';
  assert.deepEqual(parsePastedCoordinatePair(pasted), [724.98, 1596.8]);
});

test('parses negative and exponent coordinate values', () => {
  assert.deepEqual(parsePastedCoordinatePair('[-12.5, 1.6e3]'), [-12.5, 1600]);
});

test('rejects content that is not exactly one numeric coordinate pair', () => {
  assert.equal(parsePastedCoordinatePair('724.98'), null);
  assert.equal(parsePastedCoordinatePair('[724.98]'), null);
  assert.equal(parsePastedCoordinatePair('[724.98, 1596.8, 1]'), null);
  assert.equal(parsePastedCoordinatePair('["724.98", 1596.8]'), null);
});

test('rejects malformed or empty pasted content', () => {
  assert.equal(parsePastedCoordinatePair('[724.98,]'), null);
  assert.equal(parsePastedCoordinatePair('  '), null);
  assert.equal(parsePastedCoordinatePair(null), null);
});
