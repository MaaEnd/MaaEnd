/**
 * App state + editing domain logic — the JS port of `history_store.py`,
 * `zone_index.py`, `point_editing.py`, and the state-management glue in
 * `app_tk.py`. No DOM/GL: the overlay + UI read this, and `main.js` drives it.
 *
 * Editing is **zone-segment scoped** (mirrors the tk tool): the route is split
 * into maximal runs of same-`zone` points ({@link ZoneState}); the user navigates
 * segments and edits only the current one. `selectedIdx` is a *local* index into
 * the current segment's `zonePointGlobalIndices()`.
 *
 * @module state
 */

import {
  ACTION_NAMES,
  ActionType,
  getPointActions,
  normalizePathPoints,
  normalizeZoneId,
  setManualPointActions,
  setPointActions,
} from './model.js';
import { roundHalfEven } from './rounding.js';

/**
 * Deep-copy a points array for history snapshots (points are plain
 * primitive-valued objects, so structuredClone / JSON round-trip are both faithful).
 * @param {Array<Object>} points
 * @returns {Array<Object>}
 */
function deepCopyPoints(points) {
  if (typeof structuredClone === 'function') {
    try {
      return structuredClone(points);
    } catch {
      // fall through to JSON
    }
  }
  return JSON.parse(JSON.stringify(points));
}

// --- undo/redo (history_store.py) ----------------------------------------------------

/** Snapshot-based undo/redo stack. Port of `UndoRedoHistory`. */
export class UndoRedoHistory {
  /** @param {number} [maxDepth=50] */
  constructor(maxDepth = 50) {
    this._maxDepth = maxDepth;
    /** @type {Array<Array<Object>>} */
    this._undo = [];
    /** @type {Array<Array<Object>>} */
    this._redo = [];
  }

  /**
   * Push the current value and clear the redo stack.
   * @param {Array<Object>} value
   * @returns {void}
   */
  snapshot(value) {
    this._undo.push(deepCopyPoints(value));
    if (this._undo.length > this._maxDepth) this._undo.shift();
    this._redo.length = 0;
  }

  /**
   * @param {Array<Object>} current the live value (pushed onto redo)
   * @returns {?Array<Object>} the restored value, or null if nothing to undo
   */
  undo(current) {
    if (!this._undo.length) return null;
    this._redo.push(deepCopyPoints(current));
    return this._undo.pop();
  }

  /**
   * @param {Array<Object>} current the live value (pushed onto undo)
   * @returns {?Array<Object>} the restored value, or null if nothing to redo
   */
  redo(current) {
    if (!this._redo.length) return null;
    this._undo.push(deepCopyPoints(current));
    return this._redo.pop();
  }

  /** @returns {boolean} */
  canUndo() {
    return this._undo.length > 0;
  }

  /** @returns {boolean} */
  canRedo() {
    return this._redo.length > 0;
  }

  /** @returns {void} */
  clear() {
    this._undo.length = 0;
    this._redo.length = 0;
  }
}

// --- zone segments (zone_index.py) ---------------------------------------------------

/** One maximal run of same-zone points: `[startIdx, endIdx)` global. */
export class ZoneSegment {
  /** @param {string} zoneId @param {number} startIdx @param {number} endIdx */
  constructor(zoneId, startIdx, endIdx) {
    this.zoneId = zoneId;
    this.startIdx = startIdx;
    this.endIdx = endIdx;
  }
}

/** Contiguous-zone-segment navigation + point-index mapping. Port of `ZoneState`. */
export class ZoneState {
  constructor() {
    /** @type {ZoneSegment[]} */
    this.segments = [new ZoneSegment('', 0, 0)];
    this.currentSegmentIdx = 0;
  }

  /** @returns {ZoneSegment} */
  _currentSegment() {
    if (!this.segments.length) return new ZoneSegment('', 0, 0);
    const n = this.segments.length;
    return this.segments[((this.currentSegmentIdx % n) + n) % n];
  }

  /** @returns {string} */
  currentZone() {
    return this._currentSegment().zoneId;
  }

  /**
   * Global point indices of the current segment (all points when the segment has no
   * zone). Clamped to `points.length` so stale segments never index out of range.
   * @param {Array<Object>} points
   * @returns {number[]}
   */
  pointIndices(points) {
    const segment = this._currentSegment();
    if (!segment.zoneId) {
      return Array.from({ length: points.length }, (_v, i) => i);
    }
    const endIdx = Math.min(segment.endIdx, points.length);
    const startIdx = Math.min(segment.startIdx, endIdx);
    const out = [];
    for (let i = startIdx; i < endIdx; i += 1) out.push(i);
    return out;
  }

  /**
   * Recompute segments from `points`, preserving the active segment across the edit
   * (match by zone id + start containment). Port of `ZoneState.rebuild`.
   * @param {Array<Object>} points
   * @returns {void}
   */
  rebuild(points) {
    const previous = this._currentSegment();
    /** @type {ZoneSegment[]} */
    const rebuilt = [];

    let segmentStart = null;
    let segmentZone = '';
    for (let idx = 0; idx < points.length; idx += 1) {
      const zoneId = normalizeZoneId(points[idx].zone === undefined ? '' : points[idx].zone);
      if (!zoneId) {
        if (segmentStart !== null) {
          rebuilt.push(new ZoneSegment(segmentZone, segmentStart, idx));
          segmentStart = null;
          segmentZone = '';
        }
        continue;
      }
      if (segmentStart === null) {
        segmentStart = idx;
        segmentZone = zoneId;
        continue;
      }
      if (zoneId !== segmentZone) {
        rebuilt.push(new ZoneSegment(segmentZone, segmentStart, idx));
        segmentStart = idx;
        segmentZone = zoneId;
      }
    }
    if (segmentStart !== null) {
      rebuilt.push(new ZoneSegment(segmentZone, segmentStart, points.length));
    }

    this.segments = rebuilt.length ? rebuilt : [new ZoneSegment('', 0, points.length)];

    let matchedIdx = null;
    if (previous.zoneId) {
      for (let idx = 0; idx < this.segments.length; idx += 1) {
        const segment = this.segments[idx];
        if (
          segment.zoneId === previous.zoneId &&
          segment.startIdx <= previous.startIdx &&
          previous.startIdx < segment.endIdx
        ) {
          matchedIdx = idx;
          break;
        }
      }
    }
    this.currentSegmentIdx = matchedIdx === null ? 0 : matchedIdx;
  }

  /** @returns {string} "片段 N/M: zone" or a no-zone placeholder. */
  labelText() {
    const segment = this._currentSegment();
    if (segment.zoneId) {
      return `片段 ${this.currentSegmentIdx + 1}/${this.segments.length}: ${segment.zoneId}`;
    }
    return '— 无区域信息 —';
  }

  /** @returns {void} */
  prevZone() {
    if (!this.segments.length) return;
    const n = this.segments.length;
    this.currentSegmentIdx = ((this.currentSegmentIdx - 1) % n + n) % n;
  }

  /** @returns {void} */
  nextZone() {
    if (!this.segments.length) return;
    const n = this.segments.length;
    this.currentSegmentIdx = (this.currentSegmentIdx + 1) % n;
  }
}

// --- point editing (point_editing.py) ------------------------------------------------

/** Perpendicular distance from `(px,py)` to segment `AB` + clamped projection `t∈[0,1]`. */
function distPointToSegment(px, py, ax, ay, bx, by) {
  const vx = bx - ax;
  const vy = by - ay;
  const wx = px - ax;
  const wy = py - ay;
  const vv = vx * vx + vy * vy;
  if (vv <= 1e-6) {
    const dx = px - ax;
    const dy = py - ay;
    return [Math.sqrt(dx * dx + dy * dy), 0.0];
  }
  let projection = (wx * vx + wy * vy) / vv;
  projection = Math.max(0.0, Math.min(1.0, projection));
  const cx = ax + projection * vx;
  const cy = ay + projection * vy;
  const dx = px - cx;
  const dy = py - cy;
  return [Math.sqrt(dx * dx + dy * dy), projection];
}

/** UI display action name → action int (defaults RUN). Mirrors `action_name_to_type`. */
export function actionNameToType(actionName) {
  for (const [type, name] of Object.entries(ACTION_NAMES)) {
    if (name === actionName) return Number(type);
  }
  return ActionType.RUN;
}

/**
 * Zone-scoped point-editing primitives (hit-test, insert, attributes, delete, move).
 * Pure functions over `(points, zoneIndices, ...)` — the caller owns snapshot/reindex.
 * Port of `PointEditingService`.
 */
export const PointEditing = {
  /**
   * Nearest point (local index into `zoneIndices`) within `hitRadius` canvas px, or null.
   * @param {Array<Object>} points
   * @param {number[]} zoneIndices
   * @param {(wx:number, wy:number)=>[number,number]} worldToCanvas
   * @param {number} eventX @param {number} eventY
   * @param {number} [hitRadius=12]
   * @returns {?number}
   */
  hitTest(points, zoneIndices, worldToCanvas, eventX, eventY, hitRadius = 12.0) {
    let bestIndex = null;
    let bestDist2 = hitRadius * hitRadius;
    for (let index = 0; index < zoneIndices.length; index += 1) {
      const point = points[zoneIndices[index]];
      const [cx, cy] = worldToCanvas(point.x, point.y);
      const dx = eventX - cx;
      const dy = eventY - cy;
      const dist2 = dx * dx + dy * dy;
      if (dist2 < bestDist2) {
        bestDist2 = dist2;
        bestIndex = index;
      }
    }
    return bestIndex;
  },

  /**
   * Insert a point into the current zone at the best segment position (mirrors
   * `insert_point`: nearest segment; append past the last segment when projection > 0.85).
   * Mutates `points`.
   * @returns {void}
   */
  insertPoint(points, zoneIndices, currentZone, actionName, strictArrival, worldX, worldY) {
    const actionType = actionNameToType(actionName);
    const newPoint = {
      x: roundCoord(worldX),
      y: roundCoord(worldY),
      action: actionType,
      actions: [actionType],
      zone: currentZone,
      strict: strictArrival,
    };

    if (zoneIndices.length < 2) {
      points.push(newPoint);
      return;
    }

    let bestSegment = 0;
    let bestDistance = Infinity;
    let bestProjection = 0.0;
    for (let k = 0; k < zoneIndices.length - 1; k += 1) {
      const a = points[zoneIndices[k]];
      const b = points[zoneIndices[k + 1]];
      const [distance, projection] = distPointToSegment(worldX, worldY, a.x, a.y, b.x, b.y);
      if (distance < bestDistance) {
        bestDistance = distance;
        bestSegment = k;
        bestProjection = projection;
      }
    }

    const isLastSegment = bestSegment === zoneIndices.length - 2;
    const insertPos =
      isLastSegment && bestProjection > 0.85
        ? zoneIndices[bestSegment + 1] + 1
        : zoneIndices[bestSegment + 1];
    points.splice(insertPos, 0, newPoint);
  },

  /**
   * Set the selected point's action + strict. Uses `setPointActions` when the chain is
   * unchanged (preserve auto/suppress flags), else `setManualPointActions`. Mirrors
   * `apply_attributes`. Mutates `points`.
   * @returns {boolean} whether it applied
   */
  applyAttributes(points, zoneIndices, selectedIdx, actionName, strictArrival) {
    if (selectedIdx === null || selectedIdx >= zoneIndices.length) return false;
    const globalIdx = zoneIndices[selectedIdx];
    const currentActions = getPointActions(points[globalIdx]);
    const newActions = [actionNameToType(actionName)];
    if (currentActions.length === newActions.length && currentActions[0] === newActions[0]) {
      setPointActions(points[globalIdx], newActions);
    } else {
      setManualPointActions(points[globalIdx], newActions);
    }
    points[globalIdx].strict = strictArrival;
    return true;
  },

  /** @returns {boolean} Removed the selected point (mirrors `delete_selected`). Mutates `points`. */
  deleteSelected(points, zoneIndices, selectedIdx) {
    if (selectedIdx === null || selectedIdx >= zoneIndices.length) return false;
    points.splice(zoneIndices[selectedIdx], 1);
    return true;
  },

  /** @returns {boolean} Moved the selected point to `(worldX, worldY)` (mirrors `move_selected`). Mutates `points`. */
  moveSelected(points, zoneIndices, selectedIdx, worldX, worldY) {
    if (selectedIdx === null || selectedIdx >= zoneIndices.length) return false;
    const globalIdx = zoneIndices[selectedIdx];
    points[globalIdx].x = roundCoord(worldX);
    points[globalIdx].y = roundCoord(worldY);
    return true;
  },
};

/** round(value, 2) matching CPython banker's rounding on coordinate writes. */
function roundCoord(value) {
  // Editing writes use Python's round(x, 2); the single correct implementation
  // lives in rounding.js (rounding.js has no imports, so no cycle).
  return roundHalfEven(value, 2);
}

// --- app state orchestrator (app_tk.py glue) -----------------------------------------

/** Editing modes (three-mode toolbar). */
export const Mode = Object.freeze({ EDIT: 'edit', ASSERT: 'assert', ASTAR: 'astar' });

/**
 * Top-level editable state: the point list, zone-segment navigation, selection, and
 * undo/redo — with the snapshot→edit→reindex flow the tk app centralises. UI reads
 * fields; call the `edit*` helpers (they snapshot + reindex) to mutate.
 */
export class AppState {
  constructor() {
    /** @type {Array<Object>} route points (canonical, normalized) */
    this.points = [];
    /** @type {ZoneState} */
    this.zoneState = new ZoneState();
    /** @type {UndoRedoHistory} */
    this.history = new UndoRedoHistory(50);
    /** @type {?number} primary selection: local index into current segment */
    this.selectedIdx = null;
    /** @type {Set<number>} multi-selection: local indices into current segment */
    this.selectedIndices = new Set();
    /** @type {string} */
    this.mode = Mode.ASTAR;
  }

  /** @returns {number[]} global indices of the current segment's points. */
  zonePointGlobalIndices() {
    return this.zoneState.pointIndices(this.points);
  }

  /** @returns {string} current segment's zone id. */
  currentZone() {
    return this.zoneState.currentZone();
  }

  /**
   * Replace the whole point list (e.g. from import/recording), re-segment, and reset
   * selection. Clears history unless `keepHistory`.
   * @param {Array<Object>} points
   * @param {{keepHistory?:boolean}} [opts]
   * @returns {void}
   */
  setPoints(points, opts = {}) {
    this.points = normalizePathPoints(points);
    if (!opts.keepHistory) this.history.clear();
    this.zoneState.rebuild(this.points);
    this._normalizeSelection();
  }

  /** Snapshot current points for undo (call before a mutating edit). @returns {void} */
  snapshot() {
    this.history.snapshot(this.points);
  }

  /**
   * Re-normalize points, rebuild segments, and clamp selection. Run after every edit
   * (mirrors app_tk's post-edit `normalize_path_points` + `zone_state.rebuild`).
   * @returns {void}
   */
  reindex() {
    this.points = normalizePathPoints(this.points);
    this.zoneState.rebuild(this.points);
    this._normalizeSelection();
  }

  /** @returns {void} */
  _normalizeSelection() {
    const validCount = this.zonePointGlobalIndices().length;
    const kept = new Set();
    for (const idx of this.selectedIndices) {
      if (idx >= 0 && idx < validCount) kept.add(idx);
    }
    this.selectedIndices = kept;
    if (this.selectedIdx === null || this.selectedIdx >= validCount) {
      this.selectedIdx = kept.size ? Math.min(...kept) : null;
    } else if (!kept.has(this.selectedIdx) && kept.size) {
      this.selectedIdx = Math.min(...kept);
    }
    if (this.selectedIdx !== null) this.selectedIndices.add(this.selectedIdx);
  }

  /**
   * Set the primary selection (local index), replacing any multi-selection.
   * @param {?number} localIdx
   * @returns {void}
   */
  select(localIdx) {
    this.selectedIdx = localIdx;
    this.selectedIndices = localIdx === null ? new Set() : new Set([localIdx]);
  }

  /** Drop all selection (tk `_clear_selection`). @returns {void} */
  clearSelection() {
    this.selectedIdx = null;
    this.selectedIndices = new Set();
  }

  /**
   * Replace the multi-selection with `indices`, choosing `primaryIdx` as the primary
   * when it's among them (else the smallest). Port of tk `_set_selection`.
   * @param {number[]} indices local indices into the current segment
   * @param {?number} [primaryIdx]
   * @returns {void}
   */
  setSelection(indices, primaryIdx = null) {
    this.selectedIndices = new Set(indices);
    if (!this.selectedIndices.size) {
      this.clearSelection();
      return;
    }
    this.selectedIdx =
      primaryIdx !== null && this.selectedIndices.has(primaryIdx)
        ? primaryIdx
        : Math.min(...this.selectedIndices);
  }

  /**
   * Local indices of the current segment whose points fall inside the canvas rect
   * `[x0,y0]`–`[x1,y1]` (corners in any order). Port of tk `_collect_indices_in_rect`.
   * @param {(wx:number, wy:number)=>[number,number]} worldToCanvas
   * @param {number} x0 @param {number} y0 @param {number} x1 @param {number} y1
   * @returns {number[]}
   */
  collectIndicesInRect(worldToCanvas, x0, y0, x1, y1) {
    const left = Math.min(x0, x1);
    const right = Math.max(x0, x1);
    const top = Math.min(y0, y1);
    const bottom = Math.max(y0, y1);
    const zoneIndices = this.zonePointGlobalIndices();
    const selected = [];
    for (let localIdx = 0; localIdx < zoneIndices.length; localIdx += 1) {
      const point = this.points[zoneIndices[localIdx]];
      const [cx, cy] = worldToCanvas(point.x, point.y);
      if (left <= cx && cx <= right && top <= cy && cy <= bottom) selected.push(localIdx);
    }
    return selected;
  }

  /** @returns {?Object} the primary selected point, or null. */
  selectedPoint() {
    const zoneIndices = this.zonePointGlobalIndices();
    if (this.selectedIdx === null || this.selectedIdx >= zoneIndices.length) return null;
    return this.points[zoneIndices[this.selectedIdx]];
  }

  /**
   * Undo/redo: swap the point list and re-segment. Selection is reset.
   * @returns {boolean} whether it moved
   */
  undo() {
    const restored = this.history.undo(this.points);
    if (restored === null) return false;
    this.points = restored;
    this.zoneState.rebuild(this.points);
    this._normalizeSelection();
    return true;
  }

  /** @returns {boolean} */
  redo() {
    const restored = this.history.redo(this.points);
    if (restored === null) return false;
    this.points = restored;
    this.zoneState.rebuild(this.points);
    this._normalizeSelection();
    return true;
  }

  // --- edit helpers: snapshot → mutate → reindex ---------------------------------

  /**
   * @param {(wx:number, wy:number)=>[number,number]} worldToCanvas
   * @param {number} eventX @param {number} eventY @param {number} [hitRadius=12]
   * @returns {?number} local index of hit point
   */
  hitTest(worldToCanvas, eventX, eventY, hitRadius = 12.0) {
    return PointEditing.hitTest(
      this.points,
      this.zonePointGlobalIndices(),
      worldToCanvas,
      eventX,
      eventY,
      hitRadius,
    );
  }

  /**
   * Insert a point into the current zone.
   * @returns {void}
   */
  editInsertPoint(actionName, strictArrival, worldX, worldY) {
    this.snapshot();
    PointEditing.insertPoint(
      this.points,
      this.zonePointGlobalIndices(),
      this.currentZone(),
      actionName,
      strictArrival,
      worldX,
      worldY,
    );
    this.reindex();
  }

  /**
   * Apply action + strict to the primary selection.
   * @returns {boolean}
   */
  editApplyAttributes(actionName, strictArrival) {
    this.snapshot();
    const applied = PointEditing.applyAttributes(
      this.points,
      this.zonePointGlobalIndices(),
      this.selectedIdx,
      actionName,
      strictArrival,
    );
    if (applied) this.reindex();
    return applied;
  }

  /**
   * Apply the action + strict to EVERY selected point (tk `apply_action_to_selected`).
   * @param {string} actionName @param {boolean} strictArrival
   * @returns {{selectionEmpty:boolean, changed:boolean}}
   */
  editApplyActionToSelected(actionName, strictArrival) {
    if (!this.selectedIndices.size) return { selectionEmpty: true, changed: false };
    this.snapshot();
    const zoneIndices = this.zonePointGlobalIndices();
    let changed = false;
    for (const localIdx of [...this.selectedIndices].sort((a, b) => a - b)) {
      changed =
        PointEditing.applyAttributes(this.points, zoneIndices, localIdx, actionName, strictArrival) ||
        changed;
    }
    if (changed) this.reindex();
    return { selectionEmpty: false, changed };
  }

  /**
   * Append one action to the chain of every selected point (tk `append_action_to_selected`).
   * Always uses the manual setter (preserves the explicit chain).
   * @param {string} actionName
   * @returns {{selectionEmpty:boolean, changed:boolean}}
   */
  editAppendActionToSelected(actionName) {
    if (!this.selectedIndices.size) return { selectionEmpty: true, changed: false };
    this.snapshot();
    const zoneIndices = this.zonePointGlobalIndices();
    const actionType = actionNameToType(actionName);
    for (const localIdx of [...this.selectedIndices].sort((a, b) => a - b)) {
      const point = this.points[zoneIndices[localIdx]];
      setManualPointActions(point, [...getPointActions(point), actionType]);
    }
    this.reindex();
    return { selectionEmpty: false, changed: true };
  }

  /**
   * Pop the last action of every selected point, flooring at `[RUN]` (tk `pop_action_from_selected`).
   * @returns {{selectionEmpty:boolean, changed:boolean}}
   */
  editPopActionFromSelected() {
    if (!this.selectedIndices.size) return { selectionEmpty: true, changed: false };
    this.snapshot();
    const zoneIndices = this.zonePointGlobalIndices();
    for (const localIdx of [...this.selectedIndices].sort((a, b) => a - b)) {
      const point = this.points[zoneIndices[localIdx]];
      const actions = getPointActions(point);
      if (actions.length <= 1) setManualPointActions(point, [ActionType.RUN]);
      else setManualPointActions(point, actions.slice(0, -1));
    }
    this.reindex();
    return { selectionEmpty: false, changed: true };
  }

  /**
   * Delete every selected point (tk `delete_selected_point` edit branch): pops the
   * global indices high→low, then clears the selection. Returns `selectionEmpty` so
   * the caller can surface the tk "请先点击选中一个点" hint.
   * @returns {{selectionEmpty:boolean, deleted:boolean}}
   */
  editDeleteSelected() {
    if (!this.selectedIndices.size) return { selectionEmpty: true, deleted: false };
    this.snapshot();
    const zoneIndices = this.zonePointGlobalIndices();
    const globalIndices = [...this.selectedIndices]
      .map((localIdx) => zoneIndices[localIdx])
      .sort((a, b) => b - a);
    for (const globalIdx of globalIndices) this.points.splice(globalIdx, 1);
    this.clearSelection();
    this.reindex();
    return { selectionEmpty: false, deleted: true };
  }

  /**
   * Move the current-segment point at local index `fromLocal` to local index
   * `toLocal`, renumbering the segment. Zone-segments are contiguous runs in
   * `points`, so this is an in-place array move bounded to the segment — the
   * point keeps its zone and no other segment shifts. Snapshots, reindexes, and
   * leaves the moved point selected.
   * @param {number} fromLocal source local index
   * @param {number} toLocal destination local index
   * @returns {boolean} whether it moved
   */
  editReorderWithinZone(fromLocal, toLocal) {
    const zoneIndices = this.zonePointGlobalIndices();
    const count = zoneIndices.length;
    if (count < 2) return false;
    const from = Math.max(0, Math.min(count - 1, fromLocal));
    const to = Math.max(0, Math.min(count - 1, toLocal));
    if (from === to) return false;
    const start = zoneIndices[0];
    this.snapshot();
    const [moved] = this.points.splice(start + from, 1);
    this.points.splice(start + to, 0, moved);
    this.reindex();
    this.setSelection([to], to);
    return true;
  }

  /**
   * Commit a drag-move of the primary selection. Pass `takeSnapshot=false` for
   * intermediate frames of a drag; `true` on the final commit.
   * @returns {boolean}
   */
  editMoveSelected(worldX, worldY, takeSnapshot = true) {
    if (takeSnapshot) this.snapshot();
    const moved = PointEditing.moveSelected(
      this.points,
      this.zonePointGlobalIndices(),
      this.selectedIdx,
      worldX,
      worldY,
    );
    // No reindex mid-drag (would re-segment & fight the pointer); caller reindexes on release.
    return moved;
  }
}
