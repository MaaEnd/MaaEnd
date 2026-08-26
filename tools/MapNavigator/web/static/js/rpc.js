/**
 * Backend client — the single seam between the frontend and `serve.py`. Every
 * HTTP/WS call to the Python backend goes through here so the rest of the app
 * never hard-codes a URL or a fetch shape.
 *
 * All requests are same-origin (the backend serves this site via StaticFiles at
 * `/`), so paths are relative and no base URL / CORS is involved. The backend
 * binds 127.0.0.1 only.
 *
 * Convention: read endpoints throw {@link RpcError} on a non-2xx response (the
 * caller surfaces `err.message` — carrying the backend's Chinese `detail` — in
 * the status line).
 * `/api/route` is the deliberate exception — it returns `{ok:false, error}` with
 * a 200 for "no path found", so {@link postRoute} resolves that object instead of
 * throwing (an unreachable goal is a normal result, not an error).
 *
 * @module rpc
 */

/** Error carrying the backend's status + detail message. */
export class RpcError extends Error {
  /**
   * @param {string} message human-facing detail (usually the backend's `detail`)
   * @param {number} status HTTP status code (0 for network/transport failure)
   */
  constructor(message, status) {
    super(message);
    this.name = 'RpcError';
    /** @type {number} */
    this.status = status;
  }
}

/**
 * Extract the best error message from a failed `Response` — prefers the backend's
 * JSON `detail`/`error`, falls back to the raw body, then the status text.
 * @param {Response} res
 * @returns {Promise<string>}
 */
async function errorMessage(res) {
  let bodyText = '';
  try {
    bodyText = await res.text();
  } catch {
    bodyText = '';
  }
  if (bodyText) {
    try {
      const parsed = JSON.parse(bodyText);
      if (parsed && typeof parsed === 'object') {
        if (typeof parsed.detail === 'string' && parsed.detail) return parsed.detail;
        if (typeof parsed.error === 'string' && parsed.error) return parsed.error;
      }
    } catch {
      // non-JSON body — use it verbatim below
    }
    return bodyText;
  }
  return res.statusText || `HTTP ${res.status}`;
}

/**
 * GET `url` and parse JSON, throwing {@link RpcError} on non-2xx / transport error.
 * @param {string} url
 * @returns {Promise<any>}
 */
async function getJson(url) {
  let res;
  try {
    res = await fetch(url, { headers: { Accept: 'application/json' } });
  } catch (err) {
    throw new RpcError(`网络请求失败: ${url} (${err && err.message ? err.message : err})`, 0);
  }
  if (!res.ok) throw new RpcError(await errorMessage(res), res.status);
  return res.json();
}

/**
 * POST `body` as JSON to `url` and parse the JSON response, throwing on non-2xx.
 * @param {string} url
 * @param {any} body
 * @param {string} [method='POST']
 * @returns {Promise<any>}
 */
async function sendJson(url, body, method = 'POST') {
  let res;
  try {
    res = await fetch(url, {
      method,
      headers: { 'Content-Type': 'application/json', Accept: 'application/json' },
      body: JSON.stringify(body),
    });
  } catch (err) {
    throw new RpcError(`网络请求失败: ${url} (${err && err.message ? err.message : err})`, 0);
  }
  if (!res.ok) throw new RpcError(await errorMessage(res), res.status);
  return res.json();
}

// --- navmesh field + geometry --------------------------------------------------------

/**
 * Zone table (base geometry zones + tier overlays).
 * @returns {Promise<{zones: Array<Object>}>}
 */
export function getZones() {
  return getJson('/api/zones');
}

/**
 * Non-blocking navmesh load progress.
 * @returns {Promise<{ready:boolean, loading:boolean, progress:number, error:?string, path:string}>}
 */
export function getLoadStatus() {
  return getJson('/api/load-status');
}

/**
 * Fetch a geometry zone's NMSH mesh buffer. Resolves `null` when the zone has no
 * triangles (tier overlay / unknown zone → backend 404), which callers treat as
 * "nothing to render", not an error.
 * @param {number} zoneId geometry (or tier) zone id — backend resolves to parent geometry
 * @returns {Promise<ArrayBuffer|null>}
 */
export async function getMesh(zoneId) {
  let res;
  try {
    res = await fetch(`/mesh/${encodeURIComponent(String(zoneId))}`, { cache: 'no-store' });
  } catch (err) {
    throw new RpcError(`网络请求失败: /mesh/${zoneId} (${err && err.message ? err.message : err})`, 0);
  }
  if (res.status === 404) return null;
  if (!res.ok) throw new RpcError(await errorMessage(res), res.status);
  return res.arrayBuffer();
}

/**
 * 追加一次性 query 参数。底图走 `new Image().src`，浏览器对这类脚本发起的请求在硬刷新时
 * 也常直接命中旧缓存条目、根本不回源，于是更新了模板图仍显示旧图。变一下 URL 就没有可命中
 * 的条目，必然回源；配合服务端 no-store，之后每次都是最新。
 * @param {string} url
 * @returns {string}
 */
function withCacheBust(url) {
  return `${url}${url.includes('?') ? '&' : '?'}_=${Date.now()}`;
}

/**
 * Same-origin URL for a basemap PNG (relative to `assets/resource/image`), suitable
 * for an `<img>.src`. `imagePath` comes from a zone's `image_path` field.
 * @param {string} imagePath posix path relative to MAP_IMAGE_DIR
 * @returns {string}
 */
export function basemapUrl(imagePath) {
  const parts = String(imagePath)
    .split('/')
    .map((seg) => encodeURIComponent(seg));
  return withCacheBust(`/basemap/${parts.join('/')}`);
}

/**
 * Same-origin URL that resolves an arbitrary zone STRING to its basemap PNG (backend
 * runs `resolve_zone_image`, the tk `_get_map_pil` path — fs existence checks + dir
 * scan). Used for edit-mode / assert-mode / A*-mode basemaps whose zone is a route
 * point / assert / tier string, not a `/api/zones` field zone. 404 when unresolved.
 * @param {string} zoneId any zone identifier string
 * @returns {string}
 */
export function basemapByZoneUrl(zoneId) {
  return withCacheBust(`/basemap-by-zone?zone_id=${encodeURIComponent(String(zoneId))}`);
}

/**
 * Zone ids available as assert-mode targets (backend `list_available_zone_ids`, an
 * fs scan of the map-image source dirs).
 * @returns {Promise<{zone_ids: string[]}>}
 */
export function getZoneIds() {
  return getJson('/api/zone-ids');
}

/**
 * Request an A* preview route. Resolves the backend payload verbatim, including the
 * `{ok:false, error}` "unreachable" case (see module note).
 *
 * `blind_start` / `blind_target` describe the straight lines the runtime walks with no
 * navmesh under it (they are the actual ladder result, not an estimate). Their `reason` is
 * `'off_mesh'` when the endpoint lies outside the mesh, `'disconnected'` when it stands on
 * mesh the other end simply cannot be reached from — do not report the second as the first.
 *
 * `off_mesh` rides along on the `ok:false` case so a failure can say *why* — an endpoint
 * outside the mesh reads very differently from two endpoints on disconnected pieces.
 *
 * `goal_deck_y` 是终点所在那张可走面的高度:`floor_y` 管吸附,它管在重叠面里选哪一张。
 * 不给时寻路取整格全部面、先够到哪张停哪张(也就是加这个字段之前的行为)。
 *
 * @param {{zone_id:number, start:number[], goal:number[], snap_radius?:number, floor_y?:?number,
 *   goal_deck_y?:?number}} req
 * @returns {Promise<{ok:boolean, points?:number[][], segment_breaks?:number[], cost?:number,
 *   blind_start?:?{entry:number[], distance:number, reason:string},
 *   blind_target?:?{reached:number[], gap:number, reason:string},
 *   off_mesh?:{start:?OffMeshProbe, goal:?OffMeshProbe}, error?:string}>}
 */
export function postRoute(req) {
  return sendJson('/api/route', {
    zone_id: req.zone_id,
    start: req.start,
    goal: req.goal,
    snap_radius: req.snap_radius === undefined ? 5.0 : req.snap_radius,
    floor_y: req.floor_y === undefined ? null : req.floor_y,
    goal_deck_y: req.goal_deck_y === undefined ? null : req.goal_deck_y,
  });
}

/**
 * @typedef {{distance:?number, nearest:?number[], budget:?number}} OffMeshProbe a point off the
 *   walkable mesh: how far the nearest mesh point is and where it lies (both null when there is
 *   no mesh at all within the runtime's blind-walk budget). `budget` is how far the runtime will
 *   blind-walk at that endpoint's role, so only {@link postRoute} fills it in — a bare point does
 *   not say whether it is a start or a goal, and the batch probe below leaves it null.
 */

/**
 * Ask which of `points` lie off the walkable mesh. Resolves one entry per input point,
 * `null` meaning "on the mesh" (the runtime snaps it silently — nothing to report).
 *
 * Geometry only. How far the runtime actually blind-walks to a *goal* depends on the start
 * (it probes back along the goal→start line), so that number comes from {@link postRoute}
 * alone — never present a `distance` from here as the blind walk.
 *
 * @param {{zone_id:number, points:number[][], snap_radius?:number, floor_y?:?number}} req
 * @returns {Promise<{ok:boolean, results?:Array<?OffMeshProbe>, error?:string}>}
 */
export function postOffMeshProbe(req) {
  return sendJson('/api/offmesh-probe', {
    zone_id: req.zone_id,
    points: req.points,
    snap_radius: req.snap_radius === undefined ? 5.0 : req.snap_radius,
    floor_y: req.floor_y === undefined ? null : req.floor_y,
  });
}

/**
 * @typedef {{height:number, band:number[], on_surface:boolean, thin:boolean}} Deck 一张压在该
 *   坐标底下的可走面。`height` 就是 target_deck_y 要填的数,`band` 是寻路认这张面的高度带。
 */

/**
 * 问这个坐标底下压着几张可走面。小地图是二维的,走廊 / 天桥 / 屋顶会重叠在同一个点上。
 * 返回按高度从高到低排,只有一项时说明这个点不存在重叠、不用标 deck。
 *
 * @param {{zone_id:number, point:number[]}} req
 * @returns {Promise<{ok:boolean, decks?:Deck[], error?:string}>}
 */
export function postDeckProbe(req) {
  return sendJson('/api/deck-probe', { zone_id: req.zone_id, point: req.point });
}

/**
 * Trigger a single location capture. Connects to the game, captures the third valid location frame,
 * terminates connection and returns {ok: true, x, y, zone, rot}.
 * @param {Object} [connection] optional connection override, defaults to settings store config.
 * @returns {Promise<{ok: boolean, x: number, y: number, zone: string, rot: ?number}>}
 */
export function locateOnce(connection) {
  return sendJson('/api/locate-once', { connection });
}

// --- import / export (backend keeps json_import.py) -----------------------------------

/**
 * Analyze an uploaded JSON (phase 1 of import, mirrors the head of tk `import_json`).
 * The backend tries a route import first, falling back to an AssertLocation import.
 * Discriminated by `kind`:
 *   - `{ok:true, kind:'path', needs_assignment:false, points, route_count}`
 *   - `{ok:true, kind:'path', needs_assignment:true, raw_points, segments, zone_options, route_count}`
 *   - `{ok:true, kind:'assert', zone_id, target, condition_count}`
 *   - `{ok:false, error}` (verbatim Chinese message)
 * @param {string} text raw file contents
 * @returns {Promise<Object>}
 */
export function importAnalyze(text) {
  return sendJson('/api/import/analyze', { text });
}

/**
 * Finalize a route import after the user assigns a zone per segment (phase 2, mirrors
 * tk `confirm()` + the post-dialog normalize tail).
 * @param {Array<Object>} rawPoints the `raw_points` from {@link importAnalyze}
 * @param {Array<{start:number, end:number, zone:string}>} zoneAssignments per-segment zone
 * @returns {Promise<{ok:boolean, points?:Array<Object>, error?:string}>}
 */
export function importFinalize(rawPoints, zoneAssignments) {
  return sendJson('/api/import/finalize', { raw_points: rawPoints, zone_assignments: zoneAssignments });
}

/**
 * Export points to `path` nodes + a byte-parity JSON string (backend uses the same
 * `json.dumps(..., indent=4, ensure_ascii=False)` as the tk tool).
 * @param {Array<Object>} points
 * @returns {Promise<{nodes:Array<any>, text:string}>}
 */
export function exportPath(points) {
  return sendJson('/api/export/path', { points });
}

/**
 * Export an AssertLocation node + its JSON string.
 * @param {string} zoneId
 * @param {number[]} target `[x, y, w, h]`
 * @returns {Promise<{node:Object, text:string}>}
 */
export function exportAssert(zoneId, target) {
  return sendJson('/api/export/assert', { zone_id: zoneId, target });
}

// --- settings + adb ------------------------------------------------------------------

/**
 * The OS the backend (= the user's machine) runs on, and which connection kinds it can
 * actually reach.
 * @returns {Promise<{platform:string, supported_kinds:string[], default_kind:string}>}
 */
export function getPlatform() {
  return getJson('/api/platform');
}

/** @returns {Promise<Object>} persisted settings */
export function getSettings() {
  return getJson('/api/settings');
}

/**
 * @param {Object} payload settings to persist
 * @returns {Promise<Object>} the saved settings
 */
export function putSettings(payload) {
  return sendJson('/api/settings', payload, 'PUT');
}

/**
 * Check connection status with backend.
 * @param {Object} payload settings payload to check
 * @returns {Promise<{connected:boolean, message:string}>}
 */
export function checkConnection(payload) {
  return sendJson('/api/connection/check', payload);
}

/**
 * Enumerate ADB devices. Never rejects on a stopped adb server — the backend
 * returns `{devices:[], error}` instead.
 * @param {string} [adbPath] override adb binary path
 * @returns {Promise<{devices:Array<Object>, error?:string}>}
 */
export function getAdbDevices(adbPath = '') {
  const q = adbPath ? `?adb_path=${encodeURIComponent(adbPath)}` : '';
  return getJson(`/api/adb/devices${q}`);
}

/**
 * Enumerate running gamescope instances (display no, PipeWire node, EIS socket).
 * @returns {Promise<{instances:Array<{display_no:number, pw_node_id:number, eis_socket_path:string}>}>}
 */
export function getGamescopeInstances() {
  return getJson('/api/gamescope/instances');
}

// --- game sessions (WebSocket) -------------------------------------------------------

/**
 * Shared plumbing for the two backend session sockets (`/ws/record`, `/ws/navtest`):
 * connect, hand over the first payload, relay JSON both ways, expose lifecycle
 * callbacks. Subclasses only decide what that first payload looks like.
 */
class SessionSocket {
  /** @param {string} path backend WebSocket route */
  constructor(path) {
    this._path = path;
    /** @type {WebSocket|null} */
    this._ws = null;
    /** @type {(msg:Object)=>void} */
    this.onMessage = () => {};
    /** @type {()=>void} */
    this.onOpen = () => {};
    /** @type {(ev:CloseEvent)=>void} */
    this.onClose = () => {};
    /** @type {(err:any)=>void} */
    this.onError = () => {};
  }

  /** @param {Object} firstPayload sent as soon as the socket opens @returns {void} */
  _open(firstPayload) {
    const proto = location.protocol === 'https:' ? 'wss:' : 'ws:';
    const ws = new WebSocket(`${proto}//${location.host}${this._path}`);
    this._ws = ws;
    ws.addEventListener('open', () => {
      this._send(firstPayload);
      this.onOpen();
    });
    ws.addEventListener('message', (ev) => {
      let msg;
      try {
        msg = JSON.parse(ev.data);
      } catch {
        return;
      }
      this.onMessage(msg);
    });
    ws.addEventListener('close', (ev) => this.onClose(ev));
    ws.addEventListener('error', (ev) => this.onError(ev));
  }

  /** @param {Object} payload @returns {void} */
  _send(payload) {
    if (this._ws && this._ws.readyState === WebSocket.OPEN) {
      try {
        this._ws.send(JSON.stringify(payload));
      } catch (err) {
        this.onError(err);
      }
    }
  }

  /** Force-close the socket without a graceful stop. @returns {void} */
  close() {
    if (this._ws) {
      try {
        this._ws.close();
      } catch {
        // ignore
      }
      this._ws = null;
    }
  }
}

/**
 * Recording session. The backend drives the whole lifecycle; `stop` makes it emit a
 * `finished` message and then close. One socket per recording session.
 */
export class RecordingSocket extends SessionSocket {
  constructor() {
    super('/ws/record');
  }

  /**
   * @param {Object} sessionConfig `{kind:'win32'|'adb', win32?, adb?}`
   * @returns {void}
   */
  start(sessionConfig) {
    this._open(sessionConfig || {});
  }

  /** Ask the backend to stop recording. @returns {void} */
  stop() {
    this._send({ type: 'stop' });
  }
}

/**
 * Live test-run session. Stays open across runs: `arm` loads what the F3 hotkey runs
 * (a route to walk, or an assert rect to check), `run` starts one, `abort` stops it.
 * See serve.py `ws_navtest`.
 */
export class NavTestSocket extends SessionSocket {
  constructor() {
    super('/ws/navtest');
  }

  /**
   * Open the session and walk `route` as soon as the game is connected.
   * @param {Object} sessionConfig `{kind:'win32'|'adb'|..., win32?, adb?}`
   * @param {{path: Array, exported: boolean, assert_target: ?Object}} route see {@link NavTestSocket#arm}
   * @returns {void}
   */
  start(sessionConfig, route) {
    this._open({ start: sessionConfig || {}, ...this._route(route) });
  }

  /**
   * Load what F3 (and the next `run`) will run. `exported` false means editor waypoints
   * the backend still has to export, true means ready pipeline nodes. `assert_target`
   * `{zone_id, target:[x,y,w,h]}` runs the assert rect instead and wins over `path`.
   * @param {{path: Array, exported: boolean, assert_target: ?Object}} route
   * @returns {void}
   */
  arm(route) {
    this._send({ type: 'arm', ...this._route(route) });
  }

  /** Re-arm with `route` and run it once. @returns {void} */
  run(route) {
    this._send({ type: 'run', ...this._route(route) });
  }

  /** @returns {{path: Array, exported: boolean, assert_target: ?Object}} */
  _route(route) {
    return {
      path: (route && route.path) || [],
      exported: !!(route && route.exported),
      assert_target: (route && route.assert_target) || null,
    };
  }

  /** Stop the run in flight (same effect as F4). @returns {void} */
  abort() {
    this._send({ type: 'abort' });
  }

  /** End the session; the backend tears down the agent. @returns {void} */
  stop() {
    this._send({ type: 'stop' });
  }
}
