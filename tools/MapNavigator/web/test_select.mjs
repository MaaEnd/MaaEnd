import assert from "node:assert/strict";
import test from "node:test";

import {nextWheelSelectIndex} from "./static/js/ui/select.js";

test("steps a select in the vertical wheel direction", () => {
    assert.equal(nextWheelSelectIndex(1, 4, 120), 2);
    assert.equal(nextWheelSelectIndex(2, 4, -120), 1);
});

test("keeps a select within its option bounds", () => {
    assert.equal(nextWheelSelectIndex(0, 4, -120), 0);
    assert.equal(nextWheelSelectIndex(3, 4, 120), 3);
});

test("ignores wheel events without a usable vertical delta", () => {
    assert.equal(nextWheelSelectIndex(1, 4, 0), 1);
    assert.equal(nextWheelSelectIndex(1, 4, Number.NaN), 1);
    assert.equal(nextWheelSelectIndex(-1, 0, 120), -1);
});
