import assert from "node:assert/strict";
import test from "node:test";

import {advanceQuickRouteTest, buildQuickRouteTestRequest} from "./static/js/quick_route_test.js";

function endpoint(position, overrides = {}) {
    return {
        x: position[0],
        y: position[1],
        position,
        positionZone: "map02base",
        targetTier: "",
        geometryZoneId: 2,
        segmentIndex: 0,
        ...overrides,
    };
}

test("first, second, and third clicks create S, G, then a fresh S", () => {
    const first = advanceQuickRouteTest(
        null,
        endpoint([
            10,
            20,
        ]),
    );
    assert.equal(first.ok, true);
    assert.equal(first.shouldPlan, false);
    assert.deepEqual(first.state.goal, null);

    const second = advanceQuickRouteTest(
        first.state,
        endpoint([
            30,
            40,
        ]),
    );
    assert.equal(second.ok, true);
    assert.equal(second.shouldPlan, true);
    assert.deepEqual(
        second.state.start.position,
        [
            10,
            20,
        ],
    );
    assert.deepEqual(
        second.state.goal.position,
        [
            30,
            40,
        ],
    );

    const third = advanceQuickRouteTest(
        second.state,
        endpoint([
            50,
            60,
        ]),
    );
    assert.equal(third.ok, true);
    assert.equal(third.shouldPlan, false);
    assert.deepEqual(
        third.state.start.position,
        [
            50,
            60,
        ],
    );
    assert.equal(third.state.goal, null);
});

test("rejects an endpoint on another navmesh geometry", () => {
    const first = advanceQuickRouteTest(
        null,
        endpoint([
            10,
            20,
        ]),
    );
    const second = advanceQuickRouteTest(
        first.state,
        endpoint(
            [
                30,
                40,
            ],
            {geometryZoneId: 3},
        ),
    );

    assert.equal(second.ok, false);
    assert.match(second.error, /同一张 navmesh 底图/);
});

test("base goal omits target_tier while preserving the runtime start frame", () => {
    const state = {
        start: endpoint([
            10,
            20,
        ]),
        goal: endpoint([
            30,
            40,
        ]),
    };

    assert.deepEqual(buildQuickRouteTestRequest(state), {
        ok: true,
        request: {
            position: [
                10,
                20,
            ],
            position_zone: "map02base",
            custom_action_param: {
                path: [
                    {
                        action: "NAVMESH",
                        target: [
                            30,
                            40,
                        ],
                    },
                ],
            },
        },
    });
});

test("tier goal declares target_tier and zipline planning", () => {
    const state = {
        start: endpoint(
            [
                10,
                20,
            ],
            {positionZone: "map02_2f"},
        ),
        goal: endpoint(
            [
                30,
                40,
            ],
            {positionZone: "map02_2f", targetTier: "map02_2f"},
        ),
    };

    assert.deepEqual(buildQuickRouteTestRequest(state, {zip: true}), {
        ok: true,
        request: {
            position: [
                10,
                20,
            ],
            position_zone: "map02_2f",
            custom_action_param: {
                path: [
                    {
                        action: "NAVMESH",
                        target: [
                            30,
                            40,
                        ],
                        target_tier: "map02_2f",
                    },
                ],
                zip: true,
            },
        },
    });
});
