/**
 * Resolve which walkable face a 2D locator fix stands on, so the 3D view can lift a
 * measured trajectory onto the right floor instead of a floor picked by a fixed height.
 *
 * Overlapping faces (bridge over street, corridor over hall) share the same [u, v]; a fix
 * alone cannot tell them apart. The tracker uses the previous fix instead: the face that
 * is edge-connected to the previous face wins, then the face whose height is closest to
 * the previous height, and an off-mesh fix keeps the previous height.
 */

const GRID_CELLS_PER_AXIS = 256;
/** How far (px) a fix may sit outside every face before it counts as off-mesh. */
const SNAP_RADIUS = 2;
/** Faces closer than this (world height units) are the same floor for the start choice. */
const FLOOR_MERGE_HEIGHT = 0.5;
/** Extra reach (px) of the connectivity search beyond the step between two fixes. */
const REACH_MARGIN = 6;
/** Cap on faces visited by one connectivity search. */
const REACH_VISIT_LIMIT = 4096;

/**
 * @typedef {{tri:number, height:number}} FaceHit
 * @typedef {{tri:number, height:number, u:number, v:number, onMesh:boolean}} TrailEntry
 */

export class LiveHeightTracker {
  /** @param {{vertices:Float32Array, indices:Uint32Array, vertexCount:number, triangleCount:number}} mesh */
  constructor(mesh) {
    this.vertices = mesh.vertices;
    this.indices = mesh.indices;
    this.triangleCount = mesh.triangleCount;
    this._buildBounds();
    this._buildGrid();
    this._buildAdjacency(mesh.vertexCount);
  }

  _buildBounds() {
    const n = this.triangleCount;
    this.triMinU = new Float32Array(n);
    this.triMaxU = new Float32Array(n);
    this.triMinV = new Float32Array(n);
    this.triMaxV = new Float32Array(n);
    let minU = Infinity;
    let maxU = -Infinity;
    let minV = Infinity;
    let maxV = -Infinity;
    for (let t = 0; t < n; t += 1) {
      const a = this.indices[t * 3] * 3;
      const b = this.indices[t * 3 + 1] * 3;
      const c = this.indices[t * 3 + 2] * 3;
      const v = this.vertices;
      this.triMinU[t] = Math.min(v[a], v[b], v[c]);
      this.triMaxU[t] = Math.max(v[a], v[b], v[c]);
      this.triMinV[t] = Math.min(v[a + 1], v[b + 1], v[c + 1]);
      this.triMaxV[t] = Math.max(v[a + 1], v[b + 1], v[c + 1]);
      minU = Math.min(minU, this.triMinU[t]);
      maxU = Math.max(maxU, this.triMaxU[t]);
      minV = Math.min(minV, this.triMinV[t]);
      maxV = Math.max(maxV, this.triMaxV[t]);
    }
    this.minU = n ? minU : 0;
    this.minV = n ? minV : 0;
    const spanU = n ? maxU - minU : 1;
    const spanV = n ? maxV - minV : 1;
    this.cellSize = Math.max(1, spanU / GRID_CELLS_PER_AXIS, spanV / GRID_CELLS_PER_AXIS);
    this.gridCols = Math.max(1, Math.ceil(spanU / this.cellSize) + 1);
    this.gridRows = Math.max(1, Math.ceil(spanV / this.cellSize) + 1);
  }

  _cellRange(minU, maxU, minV, maxV) {
    const clampCol = (value) => Math.min(this.gridCols - 1, Math.max(0, value));
    const clampRow = (value) => Math.min(this.gridRows - 1, Math.max(0, value));
    return {
      col0: clampCol(Math.floor((minU - this.minU) / this.cellSize)),
      col1: clampCol(Math.floor((maxU - this.minU) / this.cellSize)),
      row0: clampRow(Math.floor((minV - this.minV) / this.cellSize)),
      row1: clampRow(Math.floor((maxV - this.minV) / this.cellSize)),
    };
  }

  /** Uniform grid in CSR form: cellStart[cell]..cellStart[cell+1] index into cellTris. */
  _buildGrid() {
    const cellCount = this.gridCols * this.gridRows;
    const counts = new Uint32Array(cellCount + 1);
    const visitCells = (t, fn) => {
      const {col0, col1, row0, row1} = this._cellRange(
        this.triMinU[t],
        this.triMaxU[t],
        this.triMinV[t],
        this.triMaxV[t],
      );
      for (let row = row0; row <= row1; row += 1) {
        for (let col = col0; col <= col1; col += 1) fn(row * this.gridCols + col);
      }
    };
    for (let t = 0; t < this.triangleCount; t += 1) visitCells(t, (cell) => (counts[cell + 1] += 1));
    for (let cell = 0; cell < cellCount; cell += 1) counts[cell + 1] += counts[cell];
    this.cellStart = counts;
    this.cellTris = new Uint32Array(counts[cellCount]);
    const fill = counts.slice(0, cellCount);
    for (let t = 0; t < this.triangleCount; t += 1) {
      visitCells(t, (cell) => {
        this.cellTris[fill[cell]] = t;
        fill[cell] += 1;
      });
    }
  }

  /** Edge adjacency in CSR form; stacked floors never share vertices, so they stay apart. */
  _buildAdjacency(vertexCount) {
    const edgeOwners = new Map();
    const edgeKey = (a, b) => Math.min(a, b) * vertexCount + Math.max(a, b);
    for (let t = 0; t < this.triangleCount; t += 1) {
      for (let k = 0; k < 3; k += 1) {
        const key = edgeKey(this.indices[t * 3 + k], this.indices[t * 3 + ((k + 1) % 3)]);
        const owners = edgeOwners.get(key);
        if (owners) owners.push(t);
        else edgeOwners.set(key, [t]);
      }
    }
    const counts = new Uint32Array(this.triangleCount + 1);
    for (const owners of edgeOwners.values()) {
      if (owners.length < 2) continue;
      for (const t of owners) counts[t + 1] += owners.length - 1;
    }
    for (let t = 0; t < this.triangleCount; t += 1) counts[t + 1] += counts[t];
    this.adjStart = counts;
    this.adjTris = new Uint32Array(counts[this.triangleCount]);
    const fill = counts.slice(0, this.triangleCount);
    for (const owners of edgeOwners.values()) {
      if (owners.length < 2) continue;
      for (const t of owners) {
        for (const other of owners) {
          if (other === t) continue;
          this.adjTris[fill[t]] = other;
          fill[t] += 1;
        }
      }
    }
  }

  /** Barycentric height of triangle `t` at [u, v]; NaN when the point is outside it. */
  _heightInside(t, u, v) {
    const a = this.indices[t * 3] * 3;
    const b = this.indices[t * 3 + 1] * 3;
    const c = this.indices[t * 3 + 2] * 3;
    const p = this.vertices;
    const det = (p[b] - p[a]) * (p[c + 1] - p[a + 1]) - (p[c] - p[a]) * (p[b + 1] - p[a + 1]);
    if (Math.abs(det) < 1e-12) return NaN;
    const wb = ((u - p[a]) * (p[c + 1] - p[a + 1]) - (p[c] - p[a]) * (v - p[a + 1])) / det;
    const wc = ((p[b] - p[a]) * (v - p[a + 1]) - (u - p[a]) * (p[b + 1] - p[a + 1])) / det;
    const wa = 1 - wb - wc;
    const eps = -1e-6;
    if (wa < eps || wb < eps || wc < eps) return NaN;
    return wa * p[a + 2] + wb * p[b + 2] + wc * p[c + 2];
  }

  /** Squared 2D distance from [u, v] to the closest edge of triangle `t`, and the height there. */
  _nearestOnEdges(t, u, v) {
    const p = this.vertices;
    let best = Infinity;
    let height = NaN;
    for (let k = 0; k < 3; k += 1) {
      const a = this.indices[t * 3 + k] * 3;
      const b = this.indices[t * 3 + ((k + 1) % 3)] * 3;
      const du = p[b] - p[a];
      const dv = p[b + 1] - p[a + 1];
      const len = du * du + dv * dv;
      const s = len > 0 ? Math.max(0, Math.min(1, ((u - p[a]) * du + (v - p[a + 1]) * dv) / len)) : 0;
      const eu = p[a] + du * s - u;
      const ev = p[a + 1] + dv * s - v;
      const d = eu * eu + ev * ev;
      if (d < best) {
        best = d;
        height = p[a + 2] + (p[b + 2] - p[a + 2]) * s;
      }
    }
    return {distance: best, height};
  }

  /**
   * Every face under [u, v], highest first. A fix within SNAP_RADIUS of a face edge counts
   * as standing on that face, which absorbs locator noise along mesh borders.
   * @returns {FaceHit[]}
   */
  facesAt(u, v) {
    if (!Number.isFinite(u) || !Number.isFinite(v)) return [];
    const inside = [];
    const near = [];
    const {col0, col1, row0, row1} = this._cellRange(
      u - SNAP_RADIUS,
      u + SNAP_RADIUS,
      v - SNAP_RADIUS,
      v + SNAP_RADIUS,
    );
    const seen = new Set();
    for (let row = row0; row <= row1; row += 1) {
      for (let col = col0; col <= col1; col += 1) {
        const cell = row * this.gridCols + col;
        for (let i = this.cellStart[cell]; i < this.cellStart[cell + 1]; i += 1) {
          const t = this.cellTris[i];
          if (seen.has(t)) continue;
          seen.add(t);
          if (
            u < this.triMinU[t] - SNAP_RADIUS ||
            u > this.triMaxU[t] + SNAP_RADIUS ||
            v < this.triMinV[t] - SNAP_RADIUS ||
            v > this.triMaxV[t] + SNAP_RADIUS
          )
            continue;
          const height = this._heightInside(t, u, v);
          if (Number.isFinite(height)) {
            inside.push({tri: t, height});
            continue;
          }
          const edge = this._nearestOnEdges(t, u, v);
          if (edge.distance <= SNAP_RADIUS * SNAP_RADIUS)
            near.push({tri: t, height: edge.height, distance: edge.distance});
        }
      }
    }
    // Edge-snapped faces only matter where no face contains the point; otherwise they are the
    // neighbours of the containing face and would fake a second floor.
    const hits = inside.length ? inside : near;
    hits.sort((a, b) => b.height - a.height);
    return this._mergeFloors(hits);
  }

  /** Collapse faces of one floor (same sheet, nearly equal height) into a single hit. */
  _mergeFloors(hits) {
    const merged = [];
    for (const hit of hits) {
      const last = merged[merged.length - 1];
      if (last && Math.abs(last.height - hit.height) < FLOOR_MERGE_HEIGHT) continue;
      merged.push({tri: hit.tri, height: hit.height});
    }
    return merged;
  }

  /**
   * Whether `target` is reachable from `from` across shared edges, exploring only faces
   * within `reach` px of [u, v].
   */
  _reachable(from, target, u, v, reach) {
    if (from === target) return true;
    const visited = new Set([from]);
    const queue = [from];
    while (queue.length && visited.size < REACH_VISIT_LIMIT) {
      const t = queue.shift();
      for (let i = this.adjStart[t]; i < this.adjStart[t + 1]; i += 1) {
        const next = this.adjTris[i];
        if (next === target) return true;
        if (visited.has(next)) continue;
        const du = Math.max(this.triMinU[next] - u, 0, u - this.triMaxU[next]);
        const dv = Math.max(this.triMinV[next] - v, 0, v - this.triMaxV[next]);
        if (du * du + dv * dv > reach * reach) continue;
        visited.add(next);
        queue.push(next);
      }
    }
    return false;
  }

  /**
   * Pick the face for the next fix given the previous resolved entry.
   * @param {?TrailEntry} prev
   * @param {number} u
   * @param {number} v
   * @returns {{entry:?TrailEntry, choices:?FaceHit[]}} `choices` is set when the caller has
   *   to ask which floor the trajectory starts on; `entry` is null only while that is pending.
   */
  resolve(prev, u, v) {
    const faces = this.facesAt(u, v);
    if (faces.length === 0) {
      if (!prev) return {entry: null, choices: null};
      return {entry: {tri: prev.tri, height: prev.height, u, v, onMesh: false}, choices: null};
    }
    if (!prev) {
      if (faces.length > 1) return {entry: null, choices: faces};
      return {entry: {...faces[0], u, v, onMesh: true}, choices: null};
    }
    let chosen = null;
    if (faces.length > 1 && prev.tri >= 0) {
      const reach = Math.hypot(u - prev.u, v - prev.v) + REACH_MARGIN;
      const connected = faces.filter((face) => this._reachable(prev.tri, face.tri, u, v, reach));
      if (connected.length) chosen = this._closestHeight(connected, prev.height);
    }
    if (!chosen) chosen = this._closestHeight(faces, prev.height);
    return {entry: {...chosen, u, v, onMesh: true}, choices: null};
  }

  _closestHeight(faces, height) {
    let best = faces[0];
    for (const face of faces) {
      if (Math.abs(face.height - height) < Math.abs(best.height - height)) best = face;
    }
    return best;
  }
}

/**
 * Incrementally lift a growing 2D trajectory onto the mesh. Keeps resolved entries per
 * point so each new fix costs one lookup; a shrunken or rewritten prefix restarts it.
 */
export class LiveTrail {
  /** @param {LiveHeightTracker} tracker */
  constructor(tracker) {
    this.tracker = tracker;
    this.reset();
  }

  reset() {
    /** @type {TrailEntry[]} */
    this.entries = [];
    this.consumed = 0;
    this.startU = NaN;
    this.startV = NaN;
    /** @type {?FaceHit[]} */
    this.pendingChoices = null;
    this.startHeight = null;
  }

  /** Whether the trail is waiting for the caller to pick the starting floor. */
  get pending() {
    return this.pendingChoices !== null;
  }

  /** @returns {?TrailEntry} */
  get last() {
    return this.entries.length ? this.entries[this.entries.length - 1] : null;
  }

  /**
   * Feed the whole 2D trajectory; only the unseen tail is resolved.
   * @param {Array<[number, number]>} points
   * @returns {boolean} whether a start-floor choice became pending during this call
   */
  update(points) {
    if (!Array.isArray(points) || points.length === 0) {
      this.reset();
      return false;
    }
    const [firstU, firstV] = points[0];
    if (points.length < this.consumed || firstU !== this.startU || firstV !== this.startV) {
      this.reset();
      this.startU = firstU;
      this.startV = firstV;
    }
    if (this.pending) {
      this.consumed = points.length;
      return false;
    }
    let becamePending = false;
    for (; this.consumed < points.length; this.consumed += 1) {
      const [u, v] = points[this.consumed];
      const prev = this.last;
      let result;
      if (!prev && this.startHeight !== null) {
        result = this._resolveStart(u, v);
      } else {
        result = this.tracker.resolve(prev, u, v);
      }
      if (result.choices) {
        this.pendingChoices = result.choices;
        this.consumed = points.length;
        becamePending = true;
        break;
      }
      if (result.entry) this.entries.push(result.entry);
    }
    return becamePending;
  }

  /** The first on-mesh fix after a start choice must land on the chosen floor. */
  _resolveStart(u, v) {
    const faces = this.tracker.facesAt(u, v);
    if (faces.length === 0) return {entry: null, choices: null};
    return {entry: {...this.tracker._closestHeight(faces, this.startHeight), u, v, onMesh: true}, choices: null};
  }

  /**
   * Answer a pending start choice and lift every buffered point.
   * @param {number} height one of the pending choice heights
   * @param {Array<[number, number]>} points the same trajectory passed to {@link update}
   */
  chooseStart(height, points) {
    if (!this.pending || !Number.isFinite(height)) return;
    this.pendingChoices = null;
    this.startHeight = height;
    this.entries = [];
    this.consumed = 0;
    this.update(points);
  }

  /**
   * Height for a point that is not (yet) part of the trajectory, such as the live marker
   * that moves between two recorded fixes.
   * @returns {?number}
   */
  heightFor(u, v) {
    const prev = this.last;
    if (!prev) {
      if (this.pending) return null;
      const faces = this.tracker.facesAt(u, v);
      if (faces.length === 0) return null;
      if (this.startHeight !== null) return this.tracker._closestHeight(faces, this.startHeight).height;
      return faces.length === 1 ? faces[0].height : null;
    }
    return this.tracker.resolve(prev, u, v).entry?.height ?? prev.height;
  }
}
