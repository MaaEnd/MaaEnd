/**
 * Shared user-feedback primitives: the two footer status lines (mirrors tk's
 * `_set_status` / `_set_locator_debug`). Pure DOM; imported by main.js + panels.
 *
 * @module ui/toast
 */

/** @type {?HTMLElement} */
let statusEl = null;
/** @type {?HTMLElement} */
let locatorEl = null;

/**
 * Bind the shared elements once at boot. Safe to call again (re-binds).
 * @param {{status:HTMLElement, locator:HTMLElement}} els
 * @returns {void}
 */
export function initFeedback(els) {
  statusEl = els.status || null;
  locatorEl = els.locator || null;
}

/**
 * Set the primary status line text + color (tk `_set_status(text, color)`).
 * @param {string} text @param {string} [color] CSS color; defaults to the muted grey
 * @returns {void}
 */
export function setStatus(text, color) {
  if (!statusEl) return;
  statusEl.textContent = text == null ? '' : String(text);
  statusEl.style.color = color || '#64748b';
}

/**
 * Set the secondary locator-debug line (tk `_set_locator_debug`).
 * @param {string} text
 * @returns {void}
 */
export function setLocator(text) {
  if (!locatorEl) return;
  locatorEl.textContent = text == null ? '' : String(text);
}
