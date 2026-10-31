/**
 * Map-canvas view interaction shared by every page built on the map canvas: drag-to-pan,
 * box selection, wheel / key zoom, hold-Alt-to-pan and canvas resize tracking. Page-specific
 * gestures (picking, dragging nodes, drawing shapes) stay with the page and only call in here.
 *
 * @module map_view
 */

/** Pointer travel (px, either axis) before a press counts as a drag (tk RouteEditorApp.DRAG_ACTIVATION_DISTANCE). */
export const DRAG_ACTIVATION_DISTANCE = 4;
/** Zoom step of the zoom buttons and the `+` / `-` keys. */
export const ZOOM_IN_FACTOR = 1.25;
export const ZOOM_OUT_FACTOR = 0.8;
/** CSS px the floating left column covers (gap + card + gap); fit-view centers in the rest. */
export const LEFT_PANEL_FIT_OFFSET = 324;

// WheelEvent.DOM_DELTA_* without touching the global, so the helpers also run under node.
const DOM_DELTA_LINE = 1;
const DOM_DELTA_PAGE = 2;

/**
 * @param {number} sx @param {number} sy press point
 * @param {number} cx @param {number} cy current point
 * @returns {boolean} true once the pointer moved past the drag-activation threshold
 */
export function movedExceeded(sx, sy, cx, cy) {
  return Math.abs(cx - sx) > DRAG_ACTIVATION_DISTANCE || Math.abs(cy - sy) > DRAG_ACTIVATION_DISTANCE;
}

/**
 * @param {MouseEvent} e
 * @param {HTMLElement} canvas
 * @returns {[number, number]} pointer position in canvas CSS px
 */
export function canvasPoint(e, canvas) {
  const rect = canvas.getBoundingClientRect();
  return [e.clientX - rect.left, e.clientY - rect.top];
}

/**
 * Keyboard shortcuts stay quiet while a form control has focus.
 * @param {EventTarget|null} target
 * @returns {boolean}
 */
export function isFormField(target) {
  const el = /** @type {?HTMLElement} */ (target);
  return !!(
    el &&
    (el.tagName === "INPUT" || el.tagName === "SELECT" || el.tagName === "TEXTAREA" || el.isContentEditable)
  );
}

/**
 * Zoom factor for one wheel event, clamped per event; line / page deltas are scaled to px.
 * @param {WheelEvent} e
 * @param {number} viewportHeight canvas CSS height (one page)
 * @returns {number}
 */
export function wheelZoomFactor(e, viewportHeight) {
  const deltaScale = e.deltaMode === DOM_DELTA_LINE ? 16 : e.deltaMode === DOM_DELTA_PAGE ? viewportHeight : 1;
  return Math.max(0.5, Math.min(2.0, Math.exp(-e.deltaY * deltaScale * 0.0015)));
}

/** @param {KeyboardEvent} e @returns {boolean} `+` on the main row or keypad */
export function isZoomInKey(e) {
  return e.key === "+" || e.key === "=" || e.code === "NumpadAdd";
}

/** @param {KeyboardEvent} e @returns {boolean} `-` on the main row or keypad */
export function isZoomOutKey(e) {
  return e.key === "-" || e.key === "_" || e.code === "NumpadSubtract";
}

/**
 * Call `onResize` whenever `element` changes size (window resize where ResizeObserver is missing).
 * @param {HTMLElement} element
 * @param {() => void} onResize
 * @returns {void}
 */
export function observeSize(element, onResize) {
  if (typeof ResizeObserver === "function") {
    const ro = new ResizeObserver(() => onResize());
    ro.observe(element);
  } else {
    window.addEventListener("resize", () => onResize());
  }
}

/**
 * Holding a pan key (Alt by default) swaps to the page's pan tool; releasing it restores the
 * tool that was active. The saved tool is recorded before the swap and cleared before the
 * restore, so `setTool` can tell a temporary pan from a chosen one through
 * {@link TemporaryPan#savedTool}. Held keys auto-repeat; the repeats are swallowed too, so a
 * held Space never reaches a focused button.
 *
 * @param {{current: () => string, panTool: () => ?string, setTool: (tool: string) => void, keys?: string[]}} hooks
 *   `panTool` names the pan tool of the current view, or null when it has none; `keys` are
 *   `KeyboardEvent.key` values that pan while held
 * @returns {TemporaryPan}
 */
export function bindTemporaryPan({current, panTool, setTool, keys = ["Alt"]}) {
  const temporary = new TemporaryPan();
  window.addEventListener("keydown", (e) => {
    if (isFormField(e.target) || !keys.includes(e.key)) return;
    if (!temporary.savedTool) {
      e.preventDefault();
      temporary.savedTool = current();
      temporary.key = e.key;
      const tool = panTool();
      if (tool) setTool(tool);
    } else if (e.key === temporary.key) {
      e.preventDefault();
    }
  });
  window.addEventListener("keyup", (e) => {
    if (temporary.savedTool && e.key === temporary.key) {
      e.preventDefault();
      const saved = temporary.savedTool;
      temporary.savedTool = null;
      temporary.key = null;
      setTool(saved);
    }
  });
  return temporary;
}

/** State of a held pan key; see {@link bindTemporaryPan}. */
export class TemporaryPan {
  constructor() {
    /** @type {?string} tool to restore on release, null while no pan key is held */
    this.savedTool = null;
    /** @type {?string} the pan key being held */
    this.key = null;
  }
}

/**
 * Drag-to-pan. {@link PanGesture#start} pans right away (pan tool, middle / right button);
 * {@link PanGesture#arm} waits until the press travels past {@link DRAG_ACTIVATION_DISTANCE},
 * and a press released before that is the page's click.
 */
export class PanGesture {
  /**
   * @param {import('./camera.js').Camera} camera
   * @param {HTMLElement} canvas element whose cursor shows the grab
   */
  constructor(camera, canvas) {
    this.camera = camera;
    this.canvas = canvas;
    /** Panning now. */
    this.active = false;
    /** Pressed but not yet moved far enough to pan. */
    this.armed = false;
    this._lastX = 0;
    this._lastY = 0;
    this._downX = 0;
    this._downY = 0;
  }

  /** @param {number} x @param {number} y */
  start(x, y) {
    this.active = true;
    this.armed = false;
    this._lastX = x;
    this._lastY = y;
    this.canvas.style.cursor = "grabbing";
  }

  /** @param {number} x @param {number} y */
  arm(x, y) {
    this.armed = true;
    this.active = false;
    this._downX = x;
    this._downY = y;
  }

  /**
   * Pan by the pointer delta, or turn an armed press into a pan once it moves far enough
   * (the promoting move itself does not pan).
   * @param {number} x @param {number} y
   * @returns {void}
   */
  move(x, y) {
    if (this.active) {
      this.camera.panBy(x - this._lastX, y - this._lastY);
      this._lastX = x;
      this._lastY = y;
    } else if (this.armed && movedExceeded(this._downX, this._downY, x, y)) {
      this.start(x, y);
    }
  }

  /** Drop both the pan and an armed press. */
  cancel() {
    this.active = false;
    this.armed = false;
  }
}

/** Rubber-band selection rectangle in canvas px. */
export class BoxSelection {
  constructor() {
    this.active = false;
    /** @type {?{x0:number,y0:number,x1:number,y1:number}} the box to draw, null until the pointer moves */
    this.rect = null;
    this._x0 = 0;
    this._y0 = 0;
  }

  /** @param {number} x @param {number} y */
  start(x, y) {
    this.active = true;
    this.rect = null;
    this._x0 = x;
    this._y0 = y;
  }

  /** @param {number} x @param {number} y */
  move(x, y) {
    if (this.active) this.rect = {x0: this._x0, y0: this._y0, x1: x, y1: y};
  }

  /**
   * Close the box.
   * @param {number} x @param {number} y
   * @returns {?{click:boolean, x0:number, y0:number, x1:number, y1:number}} null when no box
   *   was open; `click` when the pointer never left the press point
   */
  finish(x, y) {
    if (!this.active) return null;
    this.cancel();
    return {click: !movedExceeded(this._x0, this._y0, x, y), x0: this._x0, y0: this._y0, x1: x, y1: y};
  }

  cancel() {
    this.active = false;
    this.rect = null;
  }
}
