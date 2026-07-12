/**
 * View camera: maps between **world** space (base-px, i.e. source-image pixels of
 * the geometry zone) and **canvas** space (CSS pixels on the drawing surface).
 *
 * This is the shared, authoritative view transform for both the WebGL renderer
 * and the 2D overlay canvas — they read the same {@link Camera} instance so their
 * layers stay pixel-aligned. State is plain numbers (serializable).
 *
 * The transform is `canvas = world * viewScale + offset`, i.e. `offset` is a pure
 * **canvas-pixel** translation applied *after* scaling. (This differs from the tk
 * `renderer_tk.py` convention `(world + offset) * scale` where the offset lived in
 * world space; the web contract standardises on canvas-space offsets so `panBy`
 * can add raw pointer deltas directly.)
 *
 * @module camera
 */

export class Camera {
  constructor() {
    /** @type {number} scale factor, canvas-px per world-px. Access via {@link Camera#viewScale}. */
    this._viewScale = 1;
    /** @type {number} canvas-px X translation applied after scaling. */
    this.offsetX = 0;
    /** @type {number} canvas-px Y translation applied after scaling. */
    this.offsetY = 0;
    /** @type {number} minimum allowed view scale (matches tk clamp). */
    this.minScale = 0.002;
    /** @type {number} maximum allowed view scale (matches tk clamp). */
    this.maxScale = 500;
  }

  /**
   * Current view scale (canvas-px per world-px).
   * @returns {number}
   */
  get viewScale() {
    return this._viewScale;
  }

  /**
   * Set the view scale, clamped to `[minScale, maxScale]`.
   * @param {number} value
   */
  set viewScale(value) {
    const v = Number(value);
    if (!Number.isFinite(v)) return;
    this._viewScale = Math.min(this.maxScale, Math.max(this.minScale, v));
  }

  /**
   * World → canvas. `cx = wx*viewScale + offsetX`, `cy = wy*viewScale + offsetY`.
   * @param {number} wx world X (base px)
   * @param {number} wy world Y (base px)
   * @returns {[number, number]} `[canvasX, canvasY]` in CSS px
   */
  worldToCanvas(wx, wy) {
    return [wx * this._viewScale + this.offsetX, wy * this._viewScale + this.offsetY];
  }

  /**
   * Canvas → world (inverse of {@link Camera#worldToCanvas}).
   * @param {number} cx canvas X (CSS px)
   * @param {number} cy canvas Y (CSS px)
   * @returns {[number, number]} `[worldX, worldY]` in base px
   */
  canvasToWorld(cx, cy) {
    return [(cx - this.offsetX) / this._viewScale, (cy - this.offsetY) / this._viewScale];
  }

  /**
   * Focus-preserving zoom: multiply the scale by `factor` while keeping the world
   * point currently under canvas `(cx, cy)` fixed on screen. Honours the scale
   * clamp — the focus point stays put even when the clamp caps the effective zoom.
   *
   * Callers pass the wheel factors (1.25 in / 0.8 out) or button/key factors.
   *
   * @param {number} cx canvas X (CSS px) to hold fixed (e.g. cursor, or canvas center)
   * @param {number} cy canvas Y (CSS px) to hold fixed
   * @param {number} factor multiplicative zoom factor (>1 zooms in, <1 out)
   * @returns {void}
   */
  zoomAt(cx, cy, factor) {
    // Resolve the world point under the cursor with the *current* scale first.
    const [wx, wy] = this.canvasToWorld(cx, cy);
    // Apply (and clamp) the new scale, then re-anchor so (wx,wy) maps back to (cx,cy).
    this.viewScale = this._viewScale * factor;
    const s = this._viewScale; // read back the clamped value
    this.offsetX = cx - wx * s;
    this.offsetY = cy - wy * s;
  }

  /**
   * Pan the view by a raw canvas-pixel delta (e.g. a pointer drag delta).
   * @param {number} dxCanvas canvas-px delta X
   * @param {number} dyCanvas canvas-px delta Y
   * @returns {void}
   */
  panBy(dxCanvas, dyCanvas) {
    this.offsetX += dxCanvas;
    this.offsetY += dyCanvas;
  }

  /**
   * Pan (keeping the current scale) so world point `(wx, wy)` lands at the center
   * of the visible strip — the canvas minus the floating left panel's `leftOffset`.
   * @param {number} wx world X (base px)
   * @param {number} wy world Y (base px)
   * @param {number} canvasW canvas width (CSS px)
   * @param {number} canvasH canvas height (CSS px)
   * @param {number} [leftOffset=0] CSS px covered by the left panel
   * @returns {void}
   */
  centerOn(wx, wy, canvasW, canvasH, leftOffset = 0) {
    const s = this._viewScale;
    const visualCenterX = leftOffset + (canvasW - leftOffset) / 2;
    this.offsetX = visualCenterX - wx * s;
    this.offsetY = canvasH / 2 - wy * s;
  }

  /**
   * Fit a world-space bounding box to the canvas: choose the largest scale that
   * fits the box inside the canvas minus `padding` on every edge, then center it.
   * Mirrors `renderer_tk.py` fit behaviour (`min((W-2p)/boxW,(H-2p)/boxH)`).
   *
   * Degenerate / empty / non-finite boxes fall back to `0..100`. If exactly one
   * axis is degenerate, the finite axis governs the scale (via `min`).
   *
   * @param {number[]} bbox `[minX, minY, maxX, maxY]` in world px
   * @param {number} canvasW canvas width (CSS px)
   * @param {number} canvasH canvas height (CSS px)
   * @param {number} [padding=60] edge padding (CSS px)
   * @param {number} [leftOffset=0] CSS px shaved off the canvas' left edge (the floating
   *   left panel overlays the canvas); the fit centers in the remaining visible strip
   * @returns {void}
   */
  fitView(bbox, canvasW, canvasH, padding = 60, leftOffset = 0) {
    let minX;
    let minY;
    let maxX;
    let maxY;
    if (!Array.isArray(bbox) || bbox.length < 4 || !bbox.slice(0, 4).every(Number.isFinite)) {
      [minX, minY, maxX, maxY] = [0, 0, 100, 100];
    } else {
      [minX, minY, maxX, maxY] = bbox;
    }

    let boxW = maxX - minX;
    let boxH = maxY - minY;
    // Both axes degenerate (a single point / all-coincident points): there is no
    // extent to fit, but the box still has a meaningful center — frame a default
    // window AROUND that center rather than snapping to the world origin (which
    // would leave the point off-screen). Matches the ±200 hint-padding elsewhere.
    if (!(boxW > 0) && !(boxH > 0)) {
      const cx = minX;
      const cy = minY;
      const half = 200;
      minX = cx - half;
      minY = cy - half;
      maxX = cx + half;
      maxY = cy + half;
      boxW = 2 * half;
      boxH = 2 * half;
    }

    const availW = Math.max(1, canvasW - leftOffset - 2 * padding);
    const availH = Math.max(1, canvasH - 2 * padding);
    const sw = boxW > 0 ? availW / boxW : Infinity;
    const sh = boxH > 0 ? availH / boxH : Infinity;
    let scale = Math.min(sw, sh);
    if (!Number.isFinite(scale) || scale <= 0) scale = 1;

    this.viewScale = scale; // clamps to [minScale, maxScale]
    scale = this._viewScale; // use the clamped scale for centering

    const cxWorld = (minX + maxX) / 2;
    const cyWorld = (minY + maxY) / 2;
    const visualCenterStartX = leftOffset + (canvasW - leftOffset) / 2;
    this.offsetX = visualCenterStartX - cxWorld * scale;
    this.offsetY = canvasH / 2 - cyWorld * scale;
  }
}
