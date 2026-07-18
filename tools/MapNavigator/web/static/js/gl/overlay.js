/**
 * 2D-canvas overlay — the state-coupled vector layer drawn *above* the WebGL
 * canvas (basemap + mesh + dots). Ports the canvas drawing app_tk.py does inline:
 * the editable path (dashed white line + action-colored nodes + labels), the
 * assert rectangle, the locate-hint markers, and the A* preview (layered neon
 * route + flowing particle + S/G/waypoint badges).
 *
 * Frame-agnostic: it draws whatever world coordinates it is handed via
 * {@link Camera#worldToCanvas}. The caller (main.js) is responsible for handing it
 * coordinates already in the current *display frame* (base px, or tier px when a
 * translated tier backs the canvas) so overlay + WebGL layers stay aligned.
 *
 * @module gl/overlay
 */

import { ACTION_COLORS, getPointActions } from '../model.js';

/** @typedef {import('../camera.js').Camera} Camera */

const MONO = '"Consolas", "SFMono-Regular", ui-monospace, monospace';

const PATH_COLOR = '#f8fafc';
const NODE_DEFAULT_FILL = '#3498db';

const ASSERT_STROKE = '#f43f5e';
const ASSERT_FILL = 'rgba(244, 63, 94, 0.25)'; // tk stipple gray25 ≈ 25% alpha
const ASSERT_LABEL_FILL = '#fff1f2';

const SELECTION_RECT_STROKE = '#38bdf8';

// Off-mesh warnings share the amber of the hint marker — "look here", never "blocked".
const OFFMESH_COLOR = '#ffaa00';
const OFFMESH_DIM = 'rgba(255, 170, 0, 0.55)';

export class Overlay {
  /**
   * @param {HTMLCanvasElement} canvas the transparent 2D overlay canvas
   */
  constructor(canvas) {
    /** @type {HTMLCanvasElement} */
    this.canvas = canvas;
    /** @type {CanvasRenderingContext2D} */
    this.ctx = /** @type {CanvasRenderingContext2D} */ (canvas.getContext('2d'));
    this.cssW = 1;
    this.cssH = 1;
    this.dpr = 1;
  }

  /**
   * Match the backing store to the CSS size × dpr. The context is pre-scaled by dpr
   * so all draw calls work in CSS px (same space the camera outputs).
   * @param {number} cssW @param {number} cssH @param {number} [dpr=1]
   * @returns {void}
   */
  resize(cssW, cssH, dpr = 1) {
    this.dpr = dpr > 0 ? dpr : 1;
    this.cssW = Math.max(1, cssW || 1);
    this.cssH = Math.max(1, cssH || 1);
    const bw = Math.max(1, Math.round(this.cssW * this.dpr));
    const bh = Math.max(1, Math.round(this.cssH * this.dpr));
    if (this.canvas.width !== bw) this.canvas.width = bw;
    if (this.canvas.height !== bh) this.canvas.height = bh;
  }

  /**
   * Clear and redraw every overlay layer for the current frame.
   *
   * @param {Camera} camera shared view camera
   * @param {Object} vm view model:
   *   @param {string} vm.mode 'edit' | 'assert' | 'astar'
   *   @param {Array<Object>} [vm.points] current-segment points (display-frame coords)
   *   @param {?number} [vm.selectedIdx] primary selection (local index into vm.points)
   *   @param {Set<number>} [vm.selectedIndices] multi-selection (local indices)
   *   @param {?number[]} [vm.assertTarget] `[x,y,w,h]` display-frame, or null
   *   @param {?number[]} [vm.assertLocateHint] `[x,y]` 游戏当前位置 marker (assert mode)
   *   @param {?Array<{x:number,y:number,label:string}>} [vm.astarLocateHints] preview markers (astar mode)
   *   @param {Object} [vm.astar] see {@link Overlay#_drawAstarPreview}
   *   @param {Array<Object>} [vm.offMeshMarks] points off the walkable mesh — see
   *     {@link Overlay#_drawOffMeshMarks} (drawn in every mode)
   *   @param {?Object} [vm.selectionRect] `{x0,y0,x1,y1}` canvas-px drag box, or null
   * @returns {void}
   */
  render(camera, vm) {
    const ctx = this.ctx;
    ctx.setTransform(this.dpr, 0, 0, this.dpr, 0, 0);
    ctx.clearRect(0, 0, this.cssW, this.cssH);

    const mode = vm.mode || 'edit';

    // Real route points in every mode (the caller decides which ones are in frame);
    // assert/A* artifacts are layered on top so they stay readable over a route.
    this._drawPath(camera, vm.points || []);
    this._drawNodes(camera, vm.points || [], vm.selectedIdx, vm.selectedIndices || new Set());

    if (mode === 'assert') {
      this._drawAssertRect(camera, vm.assertTarget || null);
      const hint = vm.assertLocateHint;
      if (hint && hint.length >= 2) this._drawHintMarker(camera, hint[0], hint[1], '游戏当前位置');
    }
    if (mode === 'astar') {
      for (const hint of vm.astarLocateHints || []) {
        this._drawHintMarker(camera, hint.x, hint.y, hint.label);
      }
      if (vm.astar) {
        this._drawAstarPreview(camera, vm.astar);
      }
    }
    // Topmost: a point sitting off the walkable mesh is the one thing that must never be
    // hidden under a route line.
    this._drawOffMeshMarks(camera, vm.offMeshMarks || []);
    if (vm.selectionRect) {
      this._drawSelectionRect(vm.selectionRect);
    }
  }

  /**
   * Straight lines the runtime walks with no navmesh under it (amber, dashed, hollow ring
   * where the mesh takes over). Drawn over the neon route, which otherwise renders these
   * exactly like a normal planned leg and hides the fact that nothing was planned at all.
   * @param {Camera} camera
   * @param {Array<{off:number[], mesh:number[]}>} walks display-frame world coords
   * @returns {void}
   */
  _drawBlindWalks(camera, walks) {
    if (!walks.length) return;
    const ctx = this.ctx;
    ctx.save();
    ctx.lineCap = 'round';

    for (const walk of walks) {
      const [ox, oy] = camera.worldToCanvas(walk.off[0], walk.off[1]);
      const [mx, my] = camera.worldToCanvas(walk.mesh[0], walk.mesh[1]);

      ctx.beginPath();
      ctx.moveTo(ox, oy);
      ctx.lineTo(mx, my);
      ctx.setLineDash([]);
      ctx.strokeStyle = 'rgba(0, 0, 0, 0.45)';
      ctx.lineWidth = 6.0;
      ctx.stroke();

      ctx.beginPath();
      ctx.moveTo(ox, oy);
      ctx.lineTo(mx, my);
      ctx.setLineDash([7, 5]);
      ctx.strokeStyle = OFFMESH_COLOR;
      ctx.lineWidth = 3.0;
      ctx.stroke();

      ctx.beginPath();
      ctx.arc(mx, my, 4.5, 0, Math.PI * 2);
      ctx.setLineDash([]);
      ctx.fillStyle = '#0b1220';
      ctx.fill();
      ctx.strokeStyle = OFFMESH_COLOR;
      ctx.lineWidth = 2;
      ctx.stroke();
    }
    ctx.restore();
  }

  /**
   * Off-mesh badge: a dashed amber ring around the point plus a one-line caption. Purely
   * informational — the developer decides whether the straight line is walkable.
   *
   * `exact` marks a distance the backend measured on a real route (the blind walk the
   * runtime performs). Otherwise it is the straight-line distance to the nearest mesh,
   * which is *not* the same number, and the caption says so rather than implying it is.
   *
   * @param {Camera} camera
   * @param {Array<{point:number[], nearest:?number[], distance:?number, budget:?number,
   *   exact:boolean, label:string}>} marks display-frame world coords
   * @returns {void}
   */
  _drawOffMeshMarks(camera, marks) {
    if (!marks.length) return;
    const ctx = this.ctx;

    for (const mark of marks) {
      const [cx, cy] = camera.worldToCanvas(mark.point[0], mark.point[1]);
      if (!Number.isFinite(cx) || !Number.isFinite(cy)) continue;
      ctx.save();

      // A probe has no blind-walk line of its own — show where the mesh actually starts.
      if (!mark.exact && mark.nearest) {
        const [nx, ny] = camera.worldToCanvas(mark.nearest[0], mark.nearest[1]);
        ctx.beginPath();
        ctx.moveTo(cx, cy);
        ctx.lineTo(nx, ny);
        ctx.setLineDash([4, 4]);
        ctx.strokeStyle = OFFMESH_DIM;
        ctx.lineWidth = 1.5;
        ctx.stroke();

        ctx.beginPath();
        ctx.arc(nx, ny, 4, 0, Math.PI * 2);
        ctx.setLineDash([]);
        ctx.strokeStyle = OFFMESH_DIM;
        ctx.lineWidth = 1.5;
        ctx.stroke();
      }

      ctx.beginPath();
      ctx.arc(cx, cy, 18, 0, Math.PI * 2);
      ctx.setLineDash([4, 4]);
      ctx.strokeStyle = OFFMESH_COLOR;
      ctx.lineWidth = 1.5;
      ctx.stroke();

      // Glyph rides the ring's upper-right; the caption clears it (the plate is opaque and
      // would otherwise paint straight over it).
      this._drawWarningGlyph(cx + 20, cy - 20);
      this._drawCaption(cx, cy - 40, this._offMeshCaption(mark));
      ctx.restore();
    }
  }

  /**
   * Caption for an off-mesh badge.
   *
   * A blind walk has two very different causes and the caption must not conflate them: the
   * point is off the mesh, or it stands *on* mesh that simply can't reach the destination
   * (a small disconnected island). Saying "不在网格上" about an on-mesh point would be a lie.
   * @param {Object} mark @returns {string}
   */
  _offMeshCaption(mark) {
    const where = mark.reason === 'disconnected' ? '网格不连通' : '不在网格上';
    if (mark.distance === null || mark.distance === undefined) {
      return `${where} · 附近无网格`;
    }
    const d = mark.distance.toFixed(1);
    if (mark.exact) return `${where} · 盲走 ${d} 格`;
    const over = mark.budget && mark.distance > mark.budget ? `（超上限 ${mark.budget}）` : '';
    return `${where} · 最近网格 ${d} 格${over}`;
  }

  /** Small filled warning triangle. @param {number} cx @param {number} cy @returns {void} */
  _drawWarningGlyph(cx, cy) {
    const ctx = this.ctx;
    const r = 8;
    ctx.save();
    ctx.setLineDash([]);
    ctx.beginPath();
    ctx.moveTo(cx, cy - r);
    ctx.lineTo(cx + r * 0.9, cy + r * 0.65);
    ctx.lineTo(cx - r * 0.9, cy + r * 0.65);
    ctx.closePath();
    ctx.fillStyle = OFFMESH_COLOR;
    ctx.fill();
    ctx.strokeStyle = 'rgba(0, 0, 0, 0.55)';
    ctx.lineWidth = 1;
    ctx.stroke();

    ctx.fillStyle = '#1a1200';
    ctx.font = `bold 9px ${MONO}`;
    ctx.textAlign = 'center';
    ctx.textBaseline = 'middle';
    ctx.fillText('!', cx, cy + r * 0.1);
    ctx.restore();
  }

  /** Amber caption on a dark plate, centered at (cx, cy). @returns {void} */
  _drawCaption(cx, cy, text) {
    const ctx = this.ctx;
    ctx.save();
    ctx.setLineDash([]);
    ctx.font = `bold 10px ${MONO}`;
    ctx.textAlign = 'center';
    ctx.textBaseline = 'middle';

    const padX = 5;
    const w = ctx.measureText(text).width + padX * 2;
    const h = 15;
    ctx.fillStyle = 'rgba(11, 18, 32, 0.85)';
    ctx.strokeStyle = OFFMESH_COLOR;
    ctx.lineWidth = 1;
    ctx.beginPath();
    ctx.rect(cx - w / 2, cy - h / 2, w, h);
    ctx.fill();
    ctx.stroke();

    ctx.fillStyle = OFFMESH_COLOR;
    ctx.fillText(text, cx, cy);
    ctx.restore();
  }

  /**
   * Dashed white polyline through the points (skipped for ≤1 point).
   * @param {Camera} camera @param {Array<Object>} points
   * @returns {void}
   */
  _drawPath(camera, points) {
    if (points.length <= 1) return;
    const ctx = this.ctx;
    ctx.save();
    ctx.beginPath();
    for (let i = 0; i < points.length; i += 1) {
      const [cx, cy] = camera.worldToCanvas(points[i].x, points[i].y);
      if (i === 0) ctx.moveTo(cx, cy);
      else ctx.lineTo(cx, cy);
    }
    ctx.strokeStyle = PATH_COLOR;
    ctx.lineWidth = 2;
    ctx.setLineDash([4, 2]);
    ctx.stroke();
    ctx.restore();
  }

  /**
   * Action-colored node circles + index labels, with selection/strict outlines.
   * @param {Camera} camera @param {Array<Object>} points
   * @param {?number} selectedIdx @param {Set<number>} selectedIndices
   * @returns {void}
   */
  _drawNodes(camera, points, selectedIdx, selectedIndices) {
    const ctx = this.ctx;
    const baseRadius = 9.5;

    for (let idx = 0; idx < points.length; idx += 1) {
      const point = points[idx];
      const [cx, cy] = camera.worldToCanvas(point.x, point.y);
      const actionColor = ACTION_COLORS[point.action] || NODE_DEFAULT_FILL;
      const isStrict = !!point.strict;
      const actionCount = getPointActions(point).length;
      const isPrimary = selectedIdx === idx;
      const isSelected = selectedIndices.has(idx);

      ctx.save();
      ctx.setLineDash([]);

      if (isPrimary || isSelected) {
        ctx.beginPath();
        ctx.arc(cx, cy, baseRadius + 3.0, 0, Math.PI * 2);
        ctx.lineWidth = 1.5;
        ctx.strokeStyle = isPrimary ? '#f43f5e' : '#f59e0b';
        ctx.stroke();
      }

      ctx.beginPath();
      ctx.arc(cx, cy, baseRadius, 0, Math.PI * 2);
      ctx.fillStyle = actionColor;
      ctx.fill();

      ctx.beginPath();
      ctx.arc(cx, cy, baseRadius, 0, Math.PI * 2);
      if (isStrict) {
        const rainbowGrad = ctx.createConicGradient(0, cx, cy);
        rainbowGrad.addColorStop(0.0, '#ff0000');
        rainbowGrad.addColorStop(0.17, '#f97316');
        rainbowGrad.addColorStop(0.33, '#eab308');
        rainbowGrad.addColorStop(0.5, '#10b981');
        rainbowGrad.addColorStop(0.67, '#00ffff');
        rainbowGrad.addColorStop(0.83, '#a855f7');
        rainbowGrad.addColorStop(1.0, '#ff0000');
        ctx.lineWidth = 2.0;
        ctx.strokeStyle = rainbowGrad;
      } else {
        ctx.lineWidth = 1.0;
        ctx.strokeStyle = '#0f172a';
      }
      ctx.stroke();

      const label = actionCount > 1 ? `${idx}*` : String(idx);
      ctx.font = `bold 8px ${MONO}`;
      ctx.textAlign = 'center';
      ctx.textBaseline = 'middle';
      ctx.fillStyle = '#ffffff';
      ctx.fillText(label, cx, cy);

      ctx.restore();
    }
  }

  /**
   * Rose translucent assert rect + its `Assert [x, y, w, h]` label.
   * @param {Camera} camera @param {?number[]} target `[x,y,w,h]` display-frame world
   * @returns {void}
   */
  _drawAssertRect(camera, target) {
    if (!target || target.length < 4) return;
    const ctx = this.ctx;
    const [tx, ty, tw, th] = target;
    const [x0, y0] = camera.worldToCanvas(tx, ty);
    const [x1, y1] = camera.worldToCanvas(tx + tw, ty + th);
    const left = Math.min(x0, x1);
    const top = Math.min(y0, y1);
    const w = Math.abs(x1 - x0);
    const h = Math.abs(y1 - y0);

    ctx.save();
    ctx.setLineDash([]);
    ctx.fillStyle = ASSERT_FILL;
    ctx.fillRect(left, top, w, h);
    ctx.lineWidth = 3;
    ctx.strokeStyle = ASSERT_STROKE;
    ctx.strokeRect(left, top, w, h);

    const label = `Assert [${tx.toFixed(1)}, ${ty.toFixed(1)}, ${tw.toFixed(1)}, ${th.toFixed(1)}]`;
    ctx.fillStyle = ASSERT_LABEL_FILL;
    ctx.font = `bold 9px ${MONO}`;
    ctx.textAlign = 'left';
    ctx.textBaseline = 'top';
    ctx.fillText(label, left + 8, top + 8);
    ctx.restore();
  }

  /**
   * Amber **preview** marker: dashed outer ring + soft inner ring + solid core +
   * caption. Used for both locate fixes and hand-entered coordinates — visually
   * distinct from a real route node (which is an action-colored numbered circle).
   * @param {Camera} camera
   * @param {number} wx @param {number} wy display-frame world coords
   * @param {string} [label='游戏当前位置'] caption under the marker
   * @returns {void}
   */
  _drawHintMarker(camera, wx, wy, label = '游戏当前位置') {
    if (!Number.isFinite(wx) || !Number.isFinite(wy)) return;
    const ctx = this.ctx;
    const [cx, cy] = camera.worldToCanvas(wx, wy);
    ctx.save();

    ctx.beginPath();
    ctx.arc(cx, cy, 24, 0, Math.PI * 2);
    ctx.strokeStyle = '#ffaa00';
    ctx.lineWidth = 1.5;
    ctx.setLineDash([4, 4]);
    ctx.stroke();

    ctx.beginPath();
    ctx.arc(cx, cy, 12, 0, Math.PI * 2);
    ctx.strokeStyle = 'rgba(255, 170, 0, 0.4)';
    ctx.lineWidth = 3;
    ctx.setLineDash([]);
    ctx.stroke();

    ctx.beginPath();
    ctx.arc(cx, cy, 6, 0, Math.PI * 2);
    ctx.fillStyle = '#ffaa00';
    ctx.fill();
    ctx.lineWidth = 1;
    ctx.strokeStyle = '#ffffff';
    ctx.stroke();

    ctx.fillStyle = '#ffaa00';
    ctx.font = `bold 10px ${MONO}`;
    ctx.textAlign = 'center';
    ctx.textBaseline = 'top';
    ctx.fillText(label || '', cx, cy + 28);
    ctx.restore();
  }

  /**
   * A* preview: layered neon route (dark contrast rim → pink glow → hot-pink core
   * → white hairline), one flowing particle looping the whole path, white dots at
   * segment breaks, and S/G/waypoint badges.
   *
   * @param {Camera} camera
   * @param {Object} astar
   *   @param {number[][]} astar.previewPoints route polyline (display-frame world)
   *   @param {number[]} [astar.segmentBreaks] break indices into previewPoints
   *   @param {boolean} astar.hasRoute false → previewPoints is a straight placeholder
   *   @param {?number[]} [astar.goalOnly] `[x,y]` goal marker while no route yet
   *   @param {number[][]} [astar.waypoints] multi-leg click points (S, 2..n-1, G badges)
   *   @param {Array<Object>} [astar.blindWalks] straight lines the runtime walks off-mesh —
   *     see {@link Overlay#_drawBlindWalks}
   * @returns {void}
   */
  _drawAstarPreview(camera, astar) {
    const ctx = this.ctx;
    const previewPoints = astar.previewPoints || [];

    if (previewPoints.length >= 2) {
      ctx.save();
      const segments = this._astarSegments(previewPoints, astar.segmentBreaks, astar.hasRoute);

      ctx.lineCap = 'round';
      ctx.lineJoin = 'round';
      ctx.setLineDash([]);
      ctx.strokeStyle = 'rgba(0, 0, 0, 0.35)';
      ctx.lineWidth = 6.0;
      this._strokeSegments(camera, segments);

      ctx.strokeStyle = 'rgba(255, 0, 127, 0.2)';
      ctx.lineWidth = 5.0;
      this._strokeSegments(camera, segments);

      ctx.strokeStyle = '#ff007f';
      ctx.lineWidth = 3.0;
      this._strokeSegments(camera, segments);

      ctx.strokeStyle = '#ffffff';
      ctx.lineWidth = 1.0;
      this._strokeSegments(camera, segments);

      this._drawFlowingParticle(camera, segments);
      ctx.restore();
    }

    this._drawBlindWalks(camera, astar.blindWalks || []);

    if (astar.segmentBreaks && astar.segmentBreaks.length) {
      const breakRadius = Math.max(1.5, Math.min(4, 2 * camera.viewScale));
      for (const idx of astar.segmentBreaks) {
        if (idx <= 0 || idx >= previewPoints.length - 1) continue;
        const [cx, cy] = camera.worldToCanvas(previewPoints[idx][0], previewPoints[idx][1]);
        ctx.beginPath();
        ctx.arc(cx, cy, breakRadius, 0, Math.PI * 2);
        ctx.fillStyle = '#ffffff';
        ctx.fill();
        ctx.lineWidth = 1.0;
        ctx.strokeStyle = '#ff007f';
        ctx.stroke();
      }
    }

    const drawBadge = (pt, label, colorHex, shadowColorHex) => {
      const [cx, cy] = camera.worldToCanvas(pt[0], pt[1]);
      ctx.save();
      ctx.setLineDash([]);

      ctx.beginPath();
      ctx.arc(cx, cy, 12, 0, Math.PI * 2);
      ctx.strokeStyle = shadowColorHex;
      ctx.lineWidth = 2;
      ctx.stroke();

      ctx.beginPath();
      ctx.arc(cx, cy, 8, 0, Math.PI * 2);
      ctx.fillStyle = colorHex;
      ctx.fill();
      ctx.lineWidth = 1.5;
      ctx.strokeStyle = '#ffffff';
      ctx.stroke();

      ctx.fillStyle = '#ffffff';
      ctx.font = `bold 10px ${MONO}`;
      ctx.textAlign = 'center';
      ctx.textBaseline = 'middle';
      ctx.fillText(label, cx, cy);

      ctx.restore();
    };

    const waypoints = astar.waypoints || [];
    if (waypoints.length > 0) {
      for (let i = 0; i < waypoints.length; i += 1) {
        const pt = waypoints[i];
        if (i === 0) {
          drawBadge(pt, 'S', '#00e1ff', 'rgba(0, 225, 255, 0.4)');
        } else if (i === waypoints.length - 1) {
          drawBadge(pt, 'G', '#ff007f', 'rgba(255, 0, 127, 0.4)');
        } else {
          drawBadge(pt, String(i + 1), '#ffaa00', 'rgba(255, 170, 0, 0.4)');
        }
      }
    } else {
      if (previewPoints.length > 0) {
        drawBadge(previewPoints[0], 'S', '#00e1ff', 'rgba(0, 225, 255, 0.4)');
      }
      if (previewPoints.length > 1) {
        drawBadge(previewPoints[previewPoints.length - 1], 'G', '#ff007f', 'rgba(255, 0, 127, 0.4)');
      }
    }

    if (astar.goalOnly && !astar.hasRoute) {
      drawBadge(astar.goalOnly, 'G', '#ff007f', 'rgba(255, 0, 127, 0.4)');
    }
  }

  /**
   * Stroke every segment polyline with the context's current stroke style.
   * @param {Camera} camera @param {number[][][]} segments
   * @returns {void}
   */
  _strokeSegments(camera, segments) {
    const ctx = this.ctx;
    for (const segment of segments) {
      if (segment.length < 2) continue;
      ctx.beginPath();
      for (let i = 0; i < segment.length; i += 1) {
        const [cx, cy] = camera.worldToCanvas(segment[i][0], segment[i][1]);
        if (i === 0) ctx.moveTo(cx, cy);
        else ctx.lineTo(cx, cy);
      }
      ctx.stroke();
    }
  }

  /**
   * One white glowing particle that traces the whole route in a 6s loop (the
   * animation-frame loop in main.js keeps rendering while a preview is shown).
   * @param {Camera} camera @param {number[][][]} segments
   * @returns {void}
   */
  _drawFlowingParticle(camera, segments) {
    const ctx = this.ctx;
    const speedMs = 6000;
    const pulseT = (Date.now() % speedMs) / speedMs;

    const pts = [];
    for (const segment of segments) {
      for (const p of segment) {
        const pt = camera.worldToCanvas(p[0], p[1]);
        if (pts.length > 0) {
          const last = pts[pts.length - 1];
          if (Math.hypot(last[0] - pt[0], last[1] - pt[1]) < 0.1) continue;
        }
        pts.push(pt);
      }
    }
    if (pts.length < 2) return;

    const lengths = [];
    let totalLength = 0;
    for (let i = 0; i < pts.length - 1; i += 1) {
      const dist = Math.hypot(pts[i + 1][0] - pts[i][0], pts[i + 1][1] - pts[i][1]);
      lengths.push(dist);
      totalLength += dist;
    }
    if (!(totalLength > 0)) return;

    let target = totalLength * pulseT;
    let mx = pts[pts.length - 1][0];
    let my = pts[pts.length - 1][1];
    for (let i = 0; i < lengths.length; i += 1) {
      if (target <= lengths[i]) {
        const ratio = lengths[i] > 0 ? target / lengths[i] : 0;
        mx = pts[i][0] + (pts[i + 1][0] - pts[i][0]) * ratio;
        my = pts[i][1] + (pts[i + 1][1] - pts[i][1]) * ratio;
        break;
      }
      target -= lengths[i];
    }

    ctx.fillStyle = '#ffffff';
    ctx.shadowColor = '#ffffff';
    ctx.shadowBlur = 5;
    ctx.beginPath();
    ctx.arc(mx, my, 3.0, 0, Math.PI * 2);
    ctx.fill();
  }

  /**
   * Split preview points at `segmentBreaks` (mirrors `_astar_preview_segments`); a
   * single whole-route segment when there are no breaks / no route.
   * @param {number[][]} points @param {number[]} segmentBreaks @param {boolean} hasRoute
   * @returns {number[][][]}
   */
  _astarSegments(points, segmentBreaks, hasRoute) {
    if (!hasRoute || !segmentBreaks || !segmentBreaks.length) return [points];
    const segments = [];
    let start = 0;
    for (const breakIndex of segmentBreaks) {
      if (start < breakIndex) segments.push(points.slice(start, breakIndex + 1));
      start = breakIndex;
    }
    if (start < points.length) segments.push(points.slice(start));
    return segments;
  }

  /**
   * Dashed sky-blue drag selection box (canvas-px corners).
   * @param {{x0:number,y0:number,x1:number,y1:number}} rect
   * @returns {void}
   */
  _drawSelectionRect(rect) {
    const ctx = this.ctx;
    ctx.save();
    ctx.strokeStyle = SELECTION_RECT_STROKE;
    ctx.lineWidth = 2;
    ctx.setLineDash([4, 2]);
    const left = Math.min(rect.x0, rect.x1);
    const top = Math.min(rect.y0, rect.y1);
    ctx.strokeRect(left, top, Math.abs(rect.x1 - rect.x0), Math.abs(rect.y1 - rect.y0));
    ctx.restore();
  }
}
