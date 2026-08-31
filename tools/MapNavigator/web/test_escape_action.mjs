import assert from "node:assert/strict";
import test from "node:test";

import {EscapeAction, resolveEscapeAction} from "./static/js/escape_action.js";

test("path editing clears quick test before other selections", () => {
  assert.equal(
    resolveEscapeAction({
      mode: "edit",
      hasQuickRouteTest: true,
      hasEditContext: true,
      hasMapInspection: true,
    }),
    EscapeAction.CLEAR_QUICK_TEST,
  );
  assert.equal(resolveEscapeAction({mode: "edit", hasEditContext: true}), EscapeAction.CLEAR_EDIT_CONTEXT);
});

test("assert mode cancels an active gesture before clearing the completed frame selection", () => {
  assert.equal(
    resolveEscapeAction({
      mode: "assert",
      isAssertGesture: true,
      assertRectSelected: true,
      hasMapInspection: true,
    }),
    EscapeAction.CANCEL_ASSERT_GESTURE,
  );
  assert.equal(resolveEscapeAction({mode: "assert", assertRectSelected: true}), EscapeAction.CLEAR_ASSERT_CONTEXT);
});

test("log analysis clears point details or measurement and otherwise leaves Escape unhandled", () => {
  assert.equal(resolveEscapeAction({mode: "log", hasMapInspection: true}), EscapeAction.CLEAR_LOG_CONTEXT);
  assert.equal(resolveEscapeAction({mode: "log"}), EscapeAction.NONE);
  assert.equal(resolveEscapeAction({mode: "assert"}), EscapeAction.NONE);
});
