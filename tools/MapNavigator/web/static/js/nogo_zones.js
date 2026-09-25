/**
 * Virtual no-go zones — the polygons the planner reads from `data/MapNavigator/nogo_zones.json`.
 *
 * Rings are stored in the geometry (base) zone's px frame, keyed by the same
 * zone name the planner receives, so what is drawn here is what the planner
 * stamps out. The frontend converts from the display frame before it calls in.
 *
 * A polygon drawn on a tier view remembers that tier's name; the planner then
 * blocks only the layer at that tier's floor height. Drawn on the base it has
 * no tier and blocks every layer under the ring.
 *
 * `polyContains` mirrors the planner's even-odd rule so the editor's own hit
 * testing and the planner's cell stamping agree on self-intersecting rings.
 *
 * @module nogo_zones
 */

export const NOGO_DOC_VERSION = 1;

/** Minimum vertices that still enclose an area. */
export const NOGO_MIN_RING = 3;

/** @returns {{version:number, zones:Object<string, Array<Object>>}} a doc with no zones */
export function emptyNoGoDoc() {
  return {version: NOGO_DOC_VERSION, zones: {}};
}

/**
 * Normalize whatever the backend returned into the in-memory doc shape,
 * dropping rings that could never enclose an area.
 * @param {*} raw
 * @returns {{version:number, zones:Object<string, Array<Object>>}}
 */
export function parseNoGoDoc(raw) {
  const doc = emptyNoGoDoc();
  if (!raw || typeof raw !== "object" || !raw.zones || typeof raw.zones !== "object") return doc;
  for (const [zoneName, list] of Object.entries(raw.zones)) {
    if (!zoneName || !Array.isArray(list)) continue;
    const polys = [];
    for (const entry of list) {
      const ring = normalizeRing(entry && entry.poly);
      if (!ring) continue;
      polys.push({id: typeof entry.id === "string" ? entry.id : "", ring, tier: normalizeTier(entry.tier)});
    }
    if (polys.length) doc.zones[zoneName] = polys;
  }
  return doc;
}

/**
 * @param {*} raw
 * @returns {?Array<[number, number]>} finite `[x,y]` pairs, or null when too few
 */
function normalizeRing(raw) {
  if (!Array.isArray(raw)) return null;
  const ring = [];
  for (const point of raw) {
    if (!Array.isArray(point) || point.length < 2) continue;
    const x = Number(point[0]);
    const y = Number(point[1]);
    if (!Number.isFinite(x) || !Number.isFinite(y)) continue;
    ring.push([x, y]);
  }
  return ring.length >= NOGO_MIN_RING ? ring : null;
}

/**
 * @param {*} raw
 * @returns {?string} the tier zone name, or null when the polygon covers every layer
 */
function normalizeTier(raw) {
  return typeof raw === "string" && raw.trim() ? raw.trim() : null;
}

/**
 * @param {Object} doc
 * @param {string} zoneName
 * @returns {Array<Object>} that zone's polygons (empty when it has none)
 */
export function zonePolys(doc, zoneName) {
  if (!doc || !doc.zones || !zoneName) return [];
  return doc.zones[zoneName] || [];
}

/**
 * Append a ring to a zone. Ids only have to be unique inside their zone —
 * the planner quotes one back when a route's goal lands inside it.
 * @param {Object} doc
 * @param {string} zoneName
 * @param {Array<[number, number]>} ring base-px vertices
 * @param {?string} [tier] tier zone name the ring was drawn on; null = every layer
 * @returns {?Object} the stored polygon, or null when the ring encloses nothing
 */
export function addPoly(doc, zoneName, ring, tier = null) {
  const normalized = normalizeRing(ring);
  if (!normalized || !zoneName) return null;
  const list = doc.zones[zoneName] || (doc.zones[zoneName] = []);
  const used = new Set(list.map((poly) => poly.id));
  let n = list.length + 1;
  while (used.has(`nogo-${n}`)) n += 1;
  const poly = {id: `nogo-${n}`, ring: normalized, tier: normalizeTier(tier)};
  list.push(poly);
  return poly;
}

/**
 * @param {Object} doc @param {string} zoneName @param {number} index
 * @returns {boolean} true when a polygon was removed
 */
export function removePoly(doc, zoneName, index) {
  const list = zonePolys(doc, zoneName);
  if (index < 0 || index >= list.length) return false;
  list.splice(index, 1);
  if (!list.length) delete doc.zones[zoneName];
  return true;
}

/**
 * Whether a polygon belongs on the layer being viewed. The base view shows the
 * whole zone; a tier view shows its own polygons plus the every-layer ones.
 * Paint and hit testing both go through here so nothing invisible is clickable.
 * @param {Object} poly
 * @param {?string} viewTier tier zone name on screen, null for the base
 * @returns {boolean}
 */
export function polyOnLayer(poly, viewTier) {
  return viewTier === null || !poly.tier || poly.tier === viewTier;
}

/**
 * Even-odd containment, matching the planner's cell test.
 * @param {Array<[number, number]>} ring
 * @param {number} x @param {number} y
 * @returns {boolean}
 */
export function polyContains(ring, x, y) {
  if (!Array.isArray(ring) || ring.length < NOGO_MIN_RING) return false;
  let inside = false;
  for (let i = 0, j = ring.length - 1; i < ring.length; j = i++) {
    const [ax, ay] = ring[j];
    const [bx, by] = ring[i];
    if (ay > y !== by > y && x < ax + ((y - ay) * (bx - ax)) / (by - ay)) inside = !inside;
  }
  return inside;
}

/**
 * Doc → the exact JSON the planner parses. Coordinates are rounded so hand
 * inspection of the saved file stays readable; the planner's grid is 0.25 px.
 * @param {Object} doc
 * @returns {Object}
 */
export function serializeNoGoDoc(doc) {
  const zones = {};
  for (const [zoneName, list] of Object.entries((doc && doc.zones) || {})) {
    const polys = [];
    for (const poly of list) {
      if (!poly || !Array.isArray(poly.ring) || poly.ring.length < NOGO_MIN_RING) continue;
      const entry = {poly: poly.ring.map(([x, y]) => [round3(x), round3(y)])};
      if (poly.id) entry.id = poly.id;
      if (poly.tier) entry.tier = poly.tier;
      polys.push(entry);
    }
    if (polys.length) zones[zoneName] = polys;
  }
  return {version: NOGO_DOC_VERSION, zones};
}

/** @param {number} v @returns {number} */
function round3(v) {
  return Math.round(v * 1000) / 1000;
}
