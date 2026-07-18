/**
 * Byte-faithful port of `tools/MapNavigator/model.py` — the shared path-point data
 * model (recording + editing + export all use it). Numerics defer to {@link module:rounding}.
 *
 * A `PathPoint` is a plain object:
 *   { x:number, y:number, action:number, actions:number[], zone:string, strict:boolean,
 *     auto_portal?:true, suppress_auto_portal?:true }
 * Invariant on `actions`: either `[RUN]` or a list of non-RUN/NONE actions; `action`
 * always mirrors the last element of the normalised chain.
 * @module model
 */

import { roundHalfEven } from './rounding.js';

/** Action-type enum (parallels `model.ActionType`). Plain numbers — no TS enum. */
export const ActionType = Object.freeze({
  NONE: -1,
  RUN: 0,
  SPRINT: 1,
  JUMP: 2,
  FIGHT: 3,
  INTERACT: 4,
  PORTAL: 5,
  TRANSFER: 6,
  COLLECT: 7,
  DIG: 8,
  NAVMESH: 9,
});

const VALID_ACTION_INTS = new Set(Object.values(ActionType));

/** @type {Object<number,string>} node fill colour per action. */
export const ACTION_COLORS = {
  [ActionType.NONE]: '#64748b',
  [ActionType.RUN]: '#2563eb',
  [ActionType.SPRINT]: '#f97316',
  [ActionType.JUMP]: '#00ffff',
  [ActionType.FIGHT]: '#a855f7',
  [ActionType.INTERACT]: '#10b981',
  [ActionType.PORTAL]: '#eab308',
  [ActionType.TRANSFER]: '#ff00ff',
  [ActionType.COLLECT]: '#ff0000',
  [ActionType.DIG]: '#7c2d12',
  [ActionType.NAVMESH]: '#ffffff',
};

/** @type {Object<number,string>} display name per action. */
export const ACTION_NAMES = {
  [ActionType.NONE]: 'None',
  [ActionType.RUN]: 'Run',
  [ActionType.SPRINT]: 'Sprint',
  [ActionType.JUMP]: 'Jump',
  [ActionType.FIGHT]: 'Fight',
  [ActionType.INTERACT]: 'Interact',
  [ActionType.PORTAL]: 'Portal',
  [ActionType.TRANSFER]: 'Transfer',
  [ActionType.COLLECT]: 'Collect',
  [ActionType.DIG]: 'Dig',
  [ActionType.NAVMESH]: 'Navmesh',
};

/** @type {Object<number,string>} export token per action (RUN..NAVMESH; NONE has none). */
export const ACTION_TOKENS = {
  [ActionType.RUN]: 'RUN',
  [ActionType.SPRINT]: 'SPRINT',
  [ActionType.JUMP]: 'JUMP',
  [ActionType.FIGHT]: 'FIGHT',
  [ActionType.INTERACT]: 'INTERACT',
  [ActionType.PORTAL]: 'PORTAL',
  [ActionType.TRANSFER]: 'TRANSFER',
  [ActionType.COLLECT]: 'COLLECT',
  [ActionType.DIG]: 'DIG',
  [ActionType.NAVMESH]: 'NAVMESH',
};

/** @type {Object<string,number>} upper-case token → action int. */
export const ACTION_NAME_LOOKUP = {
  NONE: ActionType.NONE,
  RUN: ActionType.RUN,
  SPRINT: ActionType.SPRINT,
  JUMP: ActionType.JUMP,
  FIGHT: ActionType.FIGHT,
  INTERACT: ActionType.INTERACT,
  PORTAL: ActionType.PORTAL,
  TRANSFER: ActionType.TRANSFER,
  COLLECT: ActionType.COLLECT,
  DIG: ActionType.DIG,
  NAVMESH: ActionType.NAVMESH,
};

/** Actions shown in the UI dropdown, in order (RUN..NAVMESH). */
export const ACTION_MENU_TYPES = [
  ActionType.RUN,
  ActionType.SPRINT,
  ActionType.JUMP,
  ActionType.FIGHT,
  ActionType.INTERACT,
  ActionType.PORTAL,
  ActionType.TRANSFER,
  ActionType.COLLECT,
  ActionType.DIG,
  ActionType.NAVMESH,
];

/** @type {string[]} dropdown labels matching {@link ACTION_MENU_TYPES}. */
export const ACTION_MENU_NAMES = ACTION_MENU_TYPES.map((t) => ACTION_NAMES[t]);

/** Base-nav display zone → (dir, region, file) under assets/resource/image. */
export const BASE_NAV_ZONE_IMAGE_PARTS = {
  map01base: ['MapLocator', 'ValleyIV', 'Base.png'],
  map02base: ['MapLocator', 'Wuling', 'Base.png'],
  base01: ['MapLocator', 'OMVBase', 'OMVBase01.png'],
  dung01: ['MapLocator', 'Dung', 'Dung01Base.png'],
};

/** @type {string[]} */
export const BASE_NAV_DISPLAY_ZONE_IDS = Object.keys(BASE_NAV_ZONE_IMAGE_PARTS);

const INVALID_ZONE_IDS = new Set(['NONE', 'NULL', 'N/A']);

/**
 * @param {number[]} a
 * @param {number[]} b
 * @returns {boolean}
 */
function arraysEqual(a, b) {
  if (a.length !== b.length) return false;
  for (let i = 0; i < a.length; i += 1) if (a[i] !== b[i]) return false;
  return true;
}

/**
 * Collapse a chain to non-RUN/NONE actions, or `[RUN]` when nothing survives.
 * @param {number[]} actions
 * @returns {number[]}
 */
function normalizeActionChain(actions) {
  const nonRun = actions.filter((a) => a !== ActionType.NONE && a !== ActionType.RUN);
  return nonRun.length ? nonRun : [ActionType.RUN];
}

/**
 * Lenient parse of an action value (number, enum-name, or UI display name).
 * Mirrors `model.try_parse_action_type` — note booleans are rejected before ints.
 * @param {unknown} value
 * @returns {number|null}
 */
export function tryParseActionType(value) {
  if (typeof value === 'boolean') return null;
  if (typeof value === 'number') {
    if (Number.isInteger(value)) return VALID_ACTION_INTS.has(value) ? value : null;
    return null; // non-integer float → no match (Python falls through to None)
  }
  if (typeof value !== 'string') return null;

  const text = value.trim();
  if (!text) return null;
  if (/^-?\d+$/.test(text)) return tryParseActionType(parseInt(text, 10));

  const upper = text.toUpperCase();
  if (Object.prototype.hasOwnProperty.call(ACTION_NAME_LOOKUP, upper)) return ACTION_NAME_LOOKUP[upper];

  for (const [at, name] of Object.entries(ACTION_NAMES)) {
    if (name.toUpperCase() === upper) return Number(at);
  }
  return null;
}

/**
 * @param {unknown} value
 * @param {number} [def=ActionType.RUN]
 * @returns {number}
 */
export function coerceActionType(value, def = ActionType.RUN) {
  const parsed = tryParseActionType(value);
  return parsed === null ? def : parsed;
}

/**
 * @param {unknown} value
 * @param {number} [def=ActionType.RUN]
 * @returns {number[]}
 */
export function coerceActionChain(value, def = ActionType.RUN) {
  if (Array.isArray(value)) {
    const actions = [];
    for (const item of value) {
      const parsed = tryParseActionType(item);
      if (parsed !== null) actions.push(parsed);
    }
    return normalizeActionChain(actions.length ? actions : [def]);
  }
  return normalizeActionChain([coerceActionType(value, def)]);
}

/**
 * @param {unknown} value
 * @param {string} [def='']
 * @returns {string}
 */
export function normalizeZoneId(value, def = '') {
  if (typeof value !== 'string') return def;
  const zoneId = value.trim();
  if (!zoneId) return def;
  if (INVALID_ZONE_IDS.has(zoneId.toUpperCase())) return def;
  return zoneId;
}

/**
 * @param {PathPoint} point
 * @returns {number[]}
 */
export function getPointActions(point) {
  const fallback = coerceActionType(point.action, ActionType.RUN);
  return coerceActionChain(point.actions, fallback);
}

/**
 * @param {number[]} actions
 * @returns {number}
 */
export function getDisplayAction(actions) {
  const normalized = normalizeActionChain(actions);
  return normalized[normalized.length - 1];
}

/**
 * @param {PathPoint} point
 * @param {number[]} actions
 * @returns {void}
 */
export function setPointActions(point, actions) {
  const normalized = coerceActionChain(actions, ActionType.RUN);
  point.actions = normalized;
  point.action = getDisplayAction(normalized);
}

/**
 * As {@link setPointActions} but records the user's intent: clears `auto_portal`,
 * and flags `suppress_auto_portal` iff the manual chain is exactly `[RUN]`.
 * @param {PathPoint} point
 * @param {number[]} actions
 * @returns {void}
 */
export function setManualPointActions(point, actions) {
  setPointActions(point, actions);
  delete point.auto_portal;
  if (arraysEqual(getPointActions(point), [ActionType.RUN])) {
    point.suppress_auto_portal = true;
  } else {
    delete point.suppress_auto_portal;
  }
}

/**
 * @param {unknown} value
 * @param {boolean} [def=false]
 * @returns {boolean}
 */
export function coerceStrictArrival(value, def = false) {
  if (typeof value === 'boolean') return value;
  if (typeof value === 'number') {
    if (Number.isInteger(value)) {
      if (value === 0 || value === 1) return Boolean(value);
      return def;
    }
    return def;
  }
  if (typeof value !== 'string') return def;

  const text = value.trim().toLowerCase();
  if (['true', '1', 'yes', 'y', 'on'].includes(text)) return true;
  if (['false', '0', 'no', 'n', 'off'].includes(text)) return false;
  return def;
}

/**
 * @param {unknown} value
 * @returns {string} the export token (defaults to "RUN").
 */
export function exportActionToken(value) {
  const token = ACTION_TOKENS[coerceActionType(value)];
  return token === undefined ? 'RUN' : token;
}

/**
 * Keep `auto_portal`/`suppress_auto_portal` only while they still describe the
 * point's chain; drop otherwise. Mirrors `model._sync_portal_flags`.
 * @param {PathPoint} point
 * @returns {void}
 */
function syncPortalFlags(point) {
  if (Boolean(point.auto_portal) && arraysEqual(getPointActions(point), [ActionType.PORTAL])) {
    point.auto_portal = true;
  } else {
    delete point.auto_portal;
  }
  if (Boolean(point.suppress_auto_portal) && arraysEqual(getPointActions(point), [ActionType.RUN])) {
    point.suppress_auto_portal = true;
  } else {
    delete point.suppress_auto_portal;
  }
}

/**
 * Clean every point and auto-insert PORTAL actions at cross-zone boundaries.
 * Four passes, ported verbatim from `model.normalize_path_points`:
 *   (A) per-point clean + flag sync, (B) boundary set, (C) boundary auto-PORTAL /
 *   non-boundary revert, (D) merge adjacent duplicates.
 * @param {PathPoint[]} points
 * @returns {PathPoint[]}
 */
export function normalizePathPoints(points) {
  /** @type {PathPoint[]} */
  const normalized = [];
  for (const point of points) {
    const actionChain = coerceActionChain(
      point.actions,
      coerceActionType(point.action, ActionType.RUN),
    );
    /** @type {PathPoint} */
    const np = {
      x: roundHalfEven(Number(point.x), 2),
      y: roundHalfEven(Number(point.y), 2),
      action: getDisplayAction(actionChain),
      actions: actionChain,
      zone: normalizeZoneId(point.zone === undefined ? '' : point.zone),
      strict: coerceStrictArrival(point.strict, false),
    };
    if (Boolean(point.auto_portal)) np.auto_portal = true;
    if (Boolean(point.suppress_auto_portal)) np.suppress_auto_portal = true;
    syncPortalFlags(np);
    normalized.push(np);
  }

  /** @type {Set<number>} */
  const boundaryIndices = new Set();
  for (let idx = 0; idx < normalized.length - 1; idx += 1) {
    const currentZone = normalized[idx].zone;
    const nextZone = normalized[idx + 1].zone;
    if (currentZone && nextZone && currentZone !== nextZone) {
      boundaryIndices.add(idx);
      boundaryIndices.add(idx + 1);
    }
  }

  for (let idx = 0; idx < normalized.length; idx += 1) {
    const point = normalized[idx];
    const actions = getPointActions(point);
    const isBoundary = boundaryIndices.has(idx);

    if (isBoundary) {
      if (Boolean(point.suppress_auto_portal)) {
        delete point.auto_portal;
        syncPortalFlags(point);
        continue;
      }
      if (!arraysEqual(actions, [ActionType.PORTAL])) {
        setPointActions(point, [ActionType.PORTAL]);
        point.auto_portal = true;
      }
      syncPortalFlags(point);
      continue;
    }

    if (Boolean(point.auto_portal) && arraysEqual(actions, [ActionType.PORTAL])) {
      setPointActions(point, [ActionType.RUN]);
    }
    delete point.auto_portal;
    delete point.suppress_auto_portal;
  }

  /** @type {PathPoint[]} */
  const merged = [];
  for (const point of normalized) {
    const last = merged[merged.length - 1];
    if (
      last &&
      last.x === point.x &&
      last.y === point.y &&
      last.zone === point.zone &&
      last.strict === point.strict
    ) {
      const mergedAutoPortal = Boolean(last.auto_portal) || Boolean(point.auto_portal);
      const mergedSuppressed = Boolean(last.suppress_auto_portal) || Boolean(point.suppress_auto_portal);
      setPointActions(last, getPointActions(last).concat(getPointActions(point)));
      if (mergedAutoPortal) last.auto_portal = true;
      if (mergedSuppressed) last.suppress_auto_portal = true;
      syncPortalFlags(last);
      continue;
    }
    merged.push(point);
  }

  return merged;
}
