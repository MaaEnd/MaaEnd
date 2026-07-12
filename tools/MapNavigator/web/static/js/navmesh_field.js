/**
 * Client-side zone table + coordinate transforms — the JS mirror of the field
 * methods `app_tk.py` calls on `BaseNavField` (tier↔base affine, geometry-zone
 * resolution, tier grouping, display bounds). Built once from the `/api/zones`
 * payload; holds no GL/DOM state, so it is pure logic and node-checkable.
 *
 * Coordinate frames (DESIGN §9): a geometry (base) zone's mesh + image live in
 * **base px**; a tier zone has its own image in **tier px** and *no* triangles —
 * its mesh is the parent geometry zone's, mapped base→tier via the baked affine
 * `base = s·tier + t`, `transform = [sx, tx, sy, ty]`. Routing/snap always run in
 * the geometry zone's base px; only display converts to tier px.
 *
 * @module navmesh_field
 */

import { BASE_NAV_DISPLAY_ZONE_IDS } from './model.js';

const NMSH_MAGIC = 'NMSH';
const NMSH_HEADER_BYTES = 16;

/**
 * @typedef {Object} Zone
 * @property {number} zone_id
 * @property {string} name
 * @property {boolean} is_tier
 * @property {number} geometry_zone_id  parent geometry zone (self for base zones)
 * @property {number} width  image width in this zone's own px
 * @property {number} height image height in this zone's own px
 * @property {number[]} transform `[sx, tx, sy, ty]` tier→base affine (identity for base)
 * @property {?number} floor_y baked dominant-floor world-Y, or null
 * @property {number} triangle_count
 * @property {boolean} has_image
 * @property {?string} image_path posix path under MAP_IMAGE_DIR, or null
 */

export class NavmeshField {
  /**
   * @param {Zone[]} zones the `zones` array from `/api/zones`
   */
  constructor(zones) {
    /** @type {Zone[]} */
    this.zones = Array.isArray(zones) ? zones : [];
    /** @type {Map<number, Zone>} */
    this._byId = new Map();
    /** @type {Map<string, Zone>} */
    this._byName = new Map();
    for (const zone of this.zones) {
      this._byId.set(zone.zone_id, zone);
      if (zone.name) this._byName.set(zone.name, zone);
    }
  }

  /** @param {number} id @returns {?Zone} */
  zoneById(id) {
    return this._byId.get(id) || null;
  }

  /** @param {string} name @returns {?Zone} */
  zoneByName(name) {
    return this._byName.get(name) || null;
  }

  /** @param {number} id @returns {boolean} */
  isTier(id) {
    const zone = this.zoneById(id);
    return !!(zone && zone.is_tier);
  }

  /**
   * Geometry (mesh-owning) zone for `id`: the parent for a tier, else `id` itself.
   * Snap / A* / mesh always address this zone. Mirrors `geometry_zone_id`.
   * @param {number} id
   * @returns {number}
   */
  geometryZoneId(id) {
    const zone = this.zoneById(id);
    return zone && zone.is_tier ? zone.geometry_zone_id : id;
  }

  /** @param {number} id @returns {?number} baked floor-Y, or null */
  floorYFor(id) {
    const zone = this.zoneById(id);
    if (!zone) return null;
    return zone.floor_y === undefined ? null : zone.floor_y;
  }

  /** @param {number} id @returns {{width:number, height:number}} image dims in this zone's px */
  dims(id) {
    const zone = this.zoneById(id);
    return zone ? { width: zone.width, height: zone.height } : { width: 0, height: 0 };
  }

  /** @param {number} id @returns {?string} basemap path relative to MAP_IMAGE_DIR */
  imagePath(id) {
    const zone = this.zoneById(id);
    return zone ? zone.image_path || null : null;
  }

  /**
   * World bounds to frame this zone's view: `[0, 0, width, height]` in its own px.
   * Mirrors `zone_bounds` (which ignores the display-zone hint).
   * @param {number} id
   * @returns {?number[]} `[minX, minY, maxX, maxY]`, or null for unknown zones
   */
  bounds(id) {
    const zone = this.zoneById(id);
    return zone ? [0, 0, zone.width, zone.height] : null;
  }

  /**
   * tier px → base px via `base = s·tier + t`. Identity for non-tier/unknown zones.
   * @param {number} id @param {number} x @param {number} y
   * @returns {[number, number]}
   */
  tierToBase(id, x, y) {
    const zone = this.zoneById(id);
    if (!zone || !zone.is_tier) return [x, y];
    const [sx, tx, sy, ty] = zone.transform;
    return [sx * x + tx, sy * y + ty];
  }

  /**
   * base px → tier px via the inverse affine. Identity for non-tier/unknown/degenerate.
   * @param {number} id @param {number} x @param {number} y
   * @returns {[number, number]}
   */
  baseToTier(id, x, y) {
    const zone = this.zoneById(id);
    if (!zone || !zone.is_tier) return [x, y];
    const [sx, tx, sy, ty] = zone.transform;
    if (sx === 0 || sy === 0) return [x, y];
    return [(x - tx) / sx, (y - ty) / sy];
  }

  /**
   * True only for a *translated* tier — one whose template must back the canvas in
   * its own px. The identity "…_Base" tier maps tier px == base px and shows the base
   * image, so it is treated as base (false). Mirrors `_active_display_tier_id`'s test.
   * @param {number} id
   * @returns {boolean}
   */
  isRealTier(id) {
    const zone = this.zoneById(id);
    if (!zone || !zone.is_tier) return false;
    const [sx, tx, sy, ty] = zone.transform;
    return !(tx === 0 && ty === 0 && sx === 1 && sy === 1);
  }

  /**
   * Tier zone ids whose parent geometry zone is `parentId`, identity ("…_Base") first
   * so a dropdown defaults to the whole-base view. Mirrors `tier_zone_ids_for`'s
   * `(transform.tx!=0 or transform.ty!=0, name)` sort key.
   * @param {number} parentId
   * @returns {number[]}
   */
  tierZoneIdsForBase(parentId) {
    const tiers = this.zones.filter((z) => z.is_tier && z.geometry_zone_id === parentId);
    tiers.sort((a, b) => {
      const ka = a.transform[1] !== 0 || a.transform[3] !== 0 ? 1 : 0;
      const kb = b.transform[1] !== 0 || b.transform[3] !== 0 ? 1 : 0;
      if (ka !== kb) return ka - kb;
      return a.name < b.name ? -1 : a.name > b.name ? 1 : 0;
    });
    return tiers.map((z) => z.zone_id);
  }

  /** @param {number} id @returns {string} `"<id>:<name>"`, mirroring `zone_label`. */
  zoneLabel(id) {
    const zone = this.zoneById(id);
    return zone ? `${zone.zone_id}:${zone.name}` : String(id);
  }

  /**
   * Tier choices for the base named `baseName`: its tiers (identity-first), or the base
   * itself when it has no tiers (dropdown never empty). Mirrors `zone_choices_for_base`.
   * @param {string} baseName
   * @returns {Array<{id:number, name:string, label:string}>}
   */
  zoneChoicesForBase(baseName) {
    const base = this.zoneByName(baseName);
    if (!base) return [];
    const tierIds = this.tierZoneIdsForBase(base.zone_id);
    const ids = tierIds.length ? tierIds : [base.zone_id];
    return ids.map((id) => {
      const zone = this.zoneById(id);
      return { id, name: zone ? zone.name : '', label: this.zoneLabel(id) };
    });
  }

  /**
   * The canonical base display zones ({@link BASE_NAV_DISPLAY_ZONE_IDS}) actually
   * present in this field, in canonical order.
   * @returns {string[]}
   */
  displayBaseNames() {
    return BASE_NAV_DISPLAY_ZONE_IDS.filter((name) => this._byName.has(name));
  }

  /**
   * Rewrite a base-px §2.4 NMSH buffer into `tierId`'s tier-px frame (each vertex's
   * u,v mapped base→tier; height + indices untouched), for a tier-px display frame.
   * Returns the input unchanged for a non-real/degenerate tier. Does not mutate `buffer`.
   * @param {ArrayBuffer} buffer base-px NMSH bytes
   * @param {number} tierId
   * @returns {ArrayBuffer}
   */
  remapNmshToTier(buffer, tierId) {
    const zone = this.zoneById(tierId);
    if (!zone || !zone.is_tier) return buffer;
    const [sx, tx, sy, ty] = zone.transform;
    if (sx === 0 || sy === 0) return buffer;

    const head = new DataView(buffer);
    const magic = String.fromCharCode(head.getUint8(0), head.getUint8(1), head.getUint8(2), head.getUint8(3));
    if (magic !== NMSH_MAGIC) return buffer;
    const vertexCount = head.getUint32(8, true);

    const out = buffer.slice(0);
    const dv = new DataView(out);
    for (let i = 0; i < vertexCount; i += 1) {
      const off = NMSH_HEADER_BYTES + i * 12;
      const u = dv.getFloat32(off, true);
      const v = dv.getFloat32(off + 4, true);
      dv.setFloat32(off, (u - tx) / sx, true);
      dv.setFloat32(off + 4, (v - ty) / sy, true);
      // height (off + 8) unchanged
    }
    return out;
  }
}
