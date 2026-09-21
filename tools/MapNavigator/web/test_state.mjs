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
  assert.equal(normalizePathPoints([makePoint(ActionType.NAVMESH, {target_deck_y: true})])[0].target_deck_y, undefined);
  assert.equal(normalizePathPoints([makePoint(ActionType.NAVMESH, {target_deck_y: " "})])[0].target_deck_y, undefined);
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

test("FIND fields survive normalization and keep distinct specs apart", () => {
  const normalized = normalizePathPoints([
    makePoint(ActionType.FIND, {find_target: "GiftOperatorName", find_stop: "GiftOperatorApproachStop"}),
    makePoint(ActionType.FIND, {find_text: "佩丽卡", find_stop: "GiftOperatorApproachStop"}),
  ]);

  assert.equal(normalized.length, 2);
  assert.equal(normalized[0].find_target, "GiftOperatorName");
  assert.equal(normalized[0].find_stop, "GiftOperatorApproachStop");
  assert.deepEqual(normalized[1].find_text, ["佩丽卡"]);

  const sameCoordinate = normalizePathPoints([
    makePoint(ActionType.FIND, {find_target: "NodeA", find_stop: "StopNode"}),
    makePoint(ActionType.FIND, {find_target: "NodeB", find_stop: "StopNode"}),
  ]);
  assert.equal(sameCoordinate.length, 2);

  const withArrive = normalizePathPoints([
    makePoint(ActionType.FIND, {find_target: "NodeA", find_arrive: [10, 20]}),
    makePoint(ActionType.FIND, {find_target: "NodeA", find_arrive: [30, 40]}),
  ]);
  assert.equal(withArrive.length, 2);
  assert.deepEqual(withArrive[0].find_arrive, [10, 20]);
  assert.equal(normalizePathPoints([makePoint(ActionType.FIND, {find_arrive: [1]})])[0].find_arrive, undefined);
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

test("applying null fields to a mixed multi-selection keeps each point's own values", () => {
  const state = new AppState();
  state.setPoints([
    makePoint(ActionType.NAVMESH, {strict: true, required: true, target_tier: "TierA"}),
    makePoint(ActionType.RUN, {x: 300, strict: false}),
  ]);
  state.setSelection([0, 1], 0);

  state.editApplyActionToSelected(null, null, null, null);
  assert.deepEqual(state.points[0].actions, [ActionType.NAVMESH]);
  assert.deepEqual(state.points[1].actions, [ActionType.RUN]);
  assert.equal(state.points[0].strict, true);
  assert.equal(state.points[1].strict, false);
  assert.equal(state.points[0].required, true);
  assert.equal(state.points[1].required, undefined);
  assert.equal(state.points[0].target_tier, "TierA");
  assert.equal(state.points[1].target_tier, undefined);

  state.editApplyActionToSelected(null, true, false, "");
  assert.equal(state.points[0].strict, true);
  assert.equal(state.points[1].strict, true);
  assert.equal(state.points[0].required, undefined);
  assert.equal(state.points[0].target_tier, undefined);
  assert.deepEqual(state.points[1].actions, [ActionType.RUN]);
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
