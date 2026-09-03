/** Escape-key behavior shared by the three 2D map modes. */

export const EscapeAction = Object.freeze({
  NONE: "none",
  CLEAR_QUICK_TEST: "clear-quick-test",
  CLEAR_EDIT_CONTEXT: "clear-edit-context",
  CANCEL_ASSERT_GESTURE: "cancel-assert-gesture",
  CLEAR_ASSERT_CONTEXT: "clear-assert-context",
  CLEAR_LOG_CONTEXT: "clear-log-context",
});

/**
 * Choose the highest-priority cancellable state for the active 2D map mode.
 * @param {{
 *   mode:string,
 *   hasQuickRouteTest?:boolean,
 *   hasEditContext?:boolean,
 *   hasMapInspection?:boolean,
 *   isAssertGesture?:boolean,
 *   assertRectSelected?:boolean,
 * }} state
 * @returns {string}
 */
export function resolveEscapeAction({
  mode,
  hasQuickRouteTest = false,
  hasEditContext = false,
  hasMapInspection = false,
  isAssertGesture = false,
  assertRectSelected = false,
}) {
  if (mode === "edit") {
    if (hasQuickRouteTest) return EscapeAction.CLEAR_QUICK_TEST;
    if (hasEditContext || hasMapInspection) return EscapeAction.CLEAR_EDIT_CONTEXT;
    return EscapeAction.NONE;
  }
  if (mode === "assert") {
    if (isAssertGesture) return EscapeAction.CANCEL_ASSERT_GESTURE;
    if (assertRectSelected || hasMapInspection) return EscapeAction.CLEAR_ASSERT_CONTEXT;
    return EscapeAction.NONE;
  }
  if (mode === "log" && hasMapInspection) return EscapeAction.CLEAR_LOG_CONTEXT;
  return EscapeAction.NONE;
}
