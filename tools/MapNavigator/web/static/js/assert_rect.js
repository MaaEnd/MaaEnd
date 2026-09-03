/**
 * Assert-rectangle interaction helpers. Rectangles use display-frame world
 * coordinates while hit testing deliberately happens in canvas CSS pixels so
 * resize handles remain usable at every zoom level.
 *
 * @module assert_rect
 */

export const ASSERT_RECT_HIT_RADIUS = 8;

/** @typedef {'draw'|'move'|'n'|'ne'|'e'|'se'|'s'|'sw'|'w'|'nw'} AssertRectDragKind */

/**
 * Sort a raw `[x0,y0,x1,y1]` rectangle into `[left,top,right,bottom]`.
 * @param {?number[]} rect
 * @returns {?number[]}
 */
export function normalizeAssertRect(rect) {
  if (!Array.isArray(rect) || rect.length < 4 || !rect.slice(0, 4).every(Number.isFinite)) return null;
  const [x0, y0, x1, y1] = rect;
  return [Math.min(x0, x1), Math.min(y0, y1), Math.max(x0, x1), Math.max(y0, y1)];
}

/**
 * Resolve the resize edge/corner or move body under a canvas-space pointer.
 * @param {?number[]} rect raw world rectangle
 * @param {(x:number,y:number)=>[number,number]} worldToCanvas
 * @param {number} canvasX
 * @param {number} canvasY
 * @param {number} [radius]
 * @returns {?Exclude<AssertRectDragKind, 'draw'>}
 */
export function hitTestAssertRect(rect, worldToCanvas, canvasX, canvasY, radius = ASSERT_RECT_HIT_RADIUS) {
  const normalized = normalizeAssertRect(rect);
  if (!normalized || typeof worldToCanvas !== "function") return null;
  const [worldLeft, worldTop, worldRight, worldBottom] = normalized;
  const [cx0, cy0] = worldToCanvas(worldLeft, worldTop);
  const [cx1, cy1] = worldToCanvas(worldRight, worldBottom);
  if (![cx0, cy0, cx1, cy1, canvasX, canvasY].every(Number.isFinite)) return null;

  const left = Math.min(cx0, cx1);
  const top = Math.min(cy0, cy1);
  const right = Math.max(cx0, cx1);
  const bottom = Math.max(cy0, cy1);
  const hitRadius = Math.max(0, Number(radius) || 0);
  if (
    canvasX < left - hitRadius ||
    canvasX > right + hitRadius ||
    canvasY < top - hitRadius ||
    canvasY > bottom + hitRadius
  ) {
    return null;
  }

  const nearLeft = Math.abs(canvasX - left) <= hitRadius;
  const nearRight = Math.abs(canvasX - right) <= hitRadius;
  const nearTop = Math.abs(canvasY - top) <= hitRadius;
  const nearBottom = Math.abs(canvasY - bottom) <= hitRadius;

  if (nearLeft && nearTop) return "nw";
  if (nearRight && nearTop) return "ne";
  if (nearRight && nearBottom) return "se";
  if (nearLeft && nearBottom) return "sw";
  if (nearTop && canvasX >= left && canvasX <= right) return "n";
  if (nearRight && canvasY >= top && canvasY <= bottom) return "e";
  if (nearBottom && canvasX >= left && canvasX <= right) return "s";
  if (nearLeft && canvasY >= top && canvasY <= bottom) return "w";
  if (canvasX >= left && canvasX <= right && canvasY >= top && canvasY <= bottom) return "move";
  return null;
}

/**
 * Cursor matching an assert-rectangle drag kind.
 * @param {?AssertRectDragKind} kind
 * @returns {?string}
 */
export function assertRectCursor(kind) {
  if (kind === "nw" || kind === "se") return "nwse-resize";
  if (kind === "ne" || kind === "sw") return "nesw-resize";
  if (kind === "n" || kind === "s") return "ns-resize";
  if (kind === "e" || kind === "w") return "ew-resize";
  if (kind === "move") return "move";
  if (kind === "draw") return "crosshair";
  return null;
}

/**
 * Apply a draw, move, or edge/corner drag in world coordinates.
 * Crossing the opposite edge flips the normalized rectangle naturally.
 * @param {?number[]} rect rectangle captured at pointer-down
 * @param {AssertRectDragKind} kind
 * @param {[number,number]} startWorld
 * @param {[number,number]} currentWorld
 * @returns {?number[]} normalized `[left,top,right,bottom]`
 */
export function applyAssertRectDrag(rect, kind, startWorld, currentWorld) {
  if (
    !Array.isArray(startWorld) ||
    !Array.isArray(currentWorld) ||
    ![...startWorld.slice(0, 2), ...currentWorld.slice(0, 2)].every(Number.isFinite)
  ) {
    return null;
  }
  const [startX, startY] = startWorld;
  const [currentX, currentY] = currentWorld;
  if (kind === "draw") return normalizeAssertRect([startX, startY, currentX, currentY]);

  const normalized = normalizeAssertRect(rect);
  if (!normalized) return null;
  let [left, top, right, bottom] = normalized;
  if (kind === "move") {
    const dx = currentX - startX;
    const dy = currentY - startY;
    return [left + dx, top + dy, right + dx, bottom + dy];
  }

  if (kind.includes("w")) left = currentX;
  if (kind.includes("e")) right = currentX;
  if (kind.includes("n")) top = currentY;
  if (kind.includes("s")) bottom = currentY;
  return normalizeAssertRect([left, top, right, bottom]);
}
