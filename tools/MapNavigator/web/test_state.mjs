import assert from "node:assert/strict";
import test from "node:test";

import {ActionType, matchTargetDeckHeight, normalizePathPoints} from "./static/js/model.js";
import {AppState, Mode} from "./static/js/state.js";

function makePoint(action = ActionType.NAVMESH, overrides = {}) {
    return {
        x: 100,
        y: 200,
        action,
        actions: [action],
        zone: "Base",
        strict: false,
        ...overrides,
    };
}

test("path editor starts active and hand-authored points default to NAVMESH", () => {
    const state = new AppState();
    assert.equal(state.mode, Mode.EDIT);

    state.editInsertManualNavmeshPoint(123.45, 678.9, "Wuling_Base");
    assert.equal(state.points.length, 1);
    assert.deepEqual(state.points[0].actions, [ActionType.NAVMESH]);
    assert.equal(state.points[0].zone, "Wuling_Base");
    assert.equal(state.points[0].strict, false);
    assert.equal(state.points[0].required, undefined);
});

test("hand-authored tier points carry target_tier while recorded RUN points stay RUN", () => {
    const state = new AppState();
    state.editInsertManualNavmeshPoint(81.77, 108.72, "ValleyIV_L1_171", "ValleyIV_L1_171");
    assert.equal(state.points[0].target_tier, "ValleyIV_L1_171");

    state.setPoints([makePoint(ActionType.RUN)]);
    assert.deepEqual(state.points[0].actions, [ActionType.RUN]);
    assert.equal(state.points[0].target_tier, undefined);
});

test("path normalization preserves finite target decks and keeps distinct decks separate", () => {
    const normalized = normalizePathPoints([
        makePoint(ActionType.NAVMESH, {target_deck_y: "100.5"}),
        makePoint(ActionType.NAVMESH, {target_deck_y: 200.5}),
    ]);

    assert.equal(normalized.length, 2);
    assert.equal(normalized[0].target_deck_y, 100.5);
    assert.equal(normalized[1].target_deck_y, 200.5);
    assert.equal(
        normalizePathPoints([makePoint(ActionType.NAVMESH, {target_deck_y: true})])[0].target_deck_y,
        undefined,
    );
    assert.equal(
        normalizePathPoints([makePoint(ActionType.NAVMESH, {target_deck_y: " "})])[0].target_deck_y,
        undefined,
    );
});

test("target deck matching follows the runtime two-pixel nearest-surface band", () => {
    const decks = [
        {height: 332.92},
        {height: 323.22},
        {height: 315.55},
    ];
    assert.equal(matchTargetDeckHeight(decks, 315.37), 315.55);
    assert.equal(matchTargetDeckHeight(decks, 310), null);
});

test("selected NAVMESH target deck participates in undo, redo, and clear", () => {
    const state = new AppState();
    state.setPoints([makePoint(ActionType.NAVMESH, {target_deck_y: 100.5})]);
    state.select(0);

    assert.deepEqual(state.editSetSelectedTargetDeck(200.5), {
        selectionEmpty: false,
        unsupported: false,
        changed: true,
    });
    assert.equal(state.selectedPoint().target_deck_y, 200.5);
    assert.equal(state.undo(), true);
    assert.equal(state.selectedPoint().target_deck_y, 100.5);
    assert.equal(state.redo(), true);
    assert.equal(state.selectedPoint().target_deck_y, 200.5);

    assert.equal(state.editSetSelectedTargetDeck(null).changed, true);
    assert.equal(state.selectedPoint().target_deck_y, undefined);
});

test("target deck editing rejects ordinary points and multi-selection", () => {
    const state = new AppState();
    state.setPoints([
        makePoint(ActionType.RUN),
        makePoint(ActionType.NAVMESH, {x: 300}),
    ]);

    state.select(0);
    assert.equal(state.editSetSelectedTargetDeck(100).unsupported, true);
    state.setSelection([
        0,
        1,
    ]);
    assert.equal(state.editSetSelectedTargetDeck(100).unsupported, true);
});

test("changing a NAVMESH target or coordinate frame clears its stale target deck", () => {
    const state = new AppState();
    state.setPoints([makePoint(ActionType.NAVMESH, {target_deck_y: 100.5})]);
    state.select(0);

    state.editMoveSelected(101, 201);
    assert.equal(state.selectedPoint().target_deck_y, undefined);

    state.editSetSelectedTargetDeck(200.5);
    state.editApplyActionToSelected("Navmesh", false, false, "Tier");
    assert.equal(state.selectedPoint().target_deck_y, undefined);

    state.editSetSelectedTargetDeck(300.5);
    state.editApplyActionToSelected("Run", false, false, "Tier");
    assert.equal(state.selectedPoint().target_deck_y, undefined);
});
