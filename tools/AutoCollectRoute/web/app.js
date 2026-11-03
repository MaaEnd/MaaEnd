/**
 * 采集路线生成器：按采集物筛选、在底图上框选，生成 AutoCollect 路线与整套任务注册；已登记的路线可打开查看、
 * 改选采集点后重新生成或删除。
 *
 * 地图前端整套复用 MapNavigator（服务端挂在 /mapnav/）：视图交互走 {@link module:map_view}，
 * 路线、运行时展开与选框由编辑器的 {@link Overlay} 画，命中与框选沿用 {@link PointEditing}；
 * 这一页自己只画采集物标记与传送点。
 *
 * @module autocollect-route/app
 */

import {Camera} from "/mapnav/js/camera.js";
import {routePreviewData, routePreviewForDisplay, routePreviewStatus} from "/mapnav/js/edit_preview.js";
import {Overlay} from "/mapnav/js/gl/overlay.js";
import {Renderer} from "/mapnav/js/gl/renderer.js";
import {
  BoxSelection,
  LEFT_PANEL_FIT_OFFSET,
  PanGesture,
  ZOOM_IN_FACTOR,
  ZOOM_OUT_FACTOR,
  bindTemporaryPan,
  canvasPoint,
  isFormField,
  isZoomInKey,
  isZoomOutKey,
  observeSize,
  wheelZoomFactor,
} from "/mapnav/js/map_view.js";
import {ACTION_COLORS, ACTION_NAMES, ActionType} from "/mapnav/js/model.js";
import {basemapUrl, getJson, postRoutePreview, sendJson} from "/mapnav/js/rpc.js";
import {compactNumber} from "/mapnav/js/rounding.js";
import {PointEditing, UndoRedoHistory} from "/mapnav/js/state.js";
import {copyText} from "/mapnav/js/ui/clipboard.js";
import {initFeedback, setStatus} from "/mapnav/js/ui/toast.js";

/** @returns {Promise<any>} collectible types, points, maps and teleport anchors */
const getCatalog = () => getJson("/api/catalog");
/** @returns {Promise<any>} registered routes as drawn on the map, and the files a removal rewrites */
const getRoutes = () => getJson("/api/routes");
/** @param {any} req @returns {Promise<any>} planned route and the pipeline files it would write */
const previewRoute = (req) => sendJson("/api/preview", req);
/** @param {any} req @returns {Promise<any>} same as {@link previewRoute}, after writing the files */
const applyRoute = (req) => sendJson("/api/apply", req);
/** @param {string} id @returns {Promise<any>} the files rewritten by the removal */
const removeRoute = (id) => sendJson("/api/remove", {id});

const COLOR_INFO = "#3b82f6";
const COLOR_OK = "#10b981";
const COLOR_ERROR = "#ef4444";
const PROGRESS_POLL_MS = 500;

const HIT_RADIUS = 12;
// 视图比例低于它时标记收成圆点，图标挤在一起认不出来
const ICON_SCALE_MIN = 1.5;
const BADGE_RADIUS = 12;
const ICON_SIZE = 18;
const DOT_RADIUS = 3.5;
const SELECTED_COLOR = "#f59e0b";
const EXCLUDED_COLOR = "#ef4444";
const COVERED_COLOR = "#10b981";
const OUTLINE_COLOR = "#0f172a";
const BADGE_FILL = "rgba(11, 18, 32, 0.82)";
const ANCHOR_COLOR = "#22d3ee";
const MONO = '"Consolas", "SFMono-Regular", ui-monospace, monospace';
// 左键默认框选；按住 Alt 时左键临时用来平移
const TOOL_CURSORS = {pan: "grab", select: "crosshair"};
const KIND_NAMES = {rare: "稀有", common: "常规"};
const MAP_ICON =
  '<svg width="20" height="20" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.6" stroke-linecap="round" stroke-linejoin="round"><path d="M9 4 3 6v14l6-2 6 2 6-2V4l-6 2-6-2Z"></path><path d="M9 4v14M15 6v14"></path></svg>';

/**
 * @param {string} id
 * @returns {any}
 */
function $(id) {
  return document.getElementById(id);
}

/**
 * Stable per-item hue so dots of different types stay apart when zoomed out.
 * @param {string} key
 * @returns {string}
 */
function itemColor(key) {
  let hash = 0;
  for (const ch of key) hash = (hash * 31 + ch.charCodeAt(0)) >>> 0;
  return `hsl(${hash % 360} 75% 58%)`;
}

/**
 * The generated `path` as editor path points, so {@link Overlay} draws it exactly like the editor.
 * @param {Array<any>} path
 * @returns {Array<{x:number,y:number,action:number,actions:number[],strict:boolean}>}
 */
function routePoints(path) {
  const points = [];
  for (const step of path) {
    if (Array.isArray(step)) {
      const action = step[2] === "DIG" ? ActionType.DIG : ActionType.COLLECT;
      points.push({x: step[0], y: step[1], action, actions: [action], strict: step[3] === true});
    } else if (step.action === "NAVMESH") {
      const [x, y] = step.target;
      points.push({x, y, action: ActionType.NAVMESH, actions: [ActionType.NAVMESH], strict: false});
    }
  }
  return points;
}

/**
 * @param {string} text
 * @returns {HTMLDivElement} the placeholder a list shows when it has no rows
 */
function emptyRow(text) {
  const empty = document.createElement("div");
  empty.className = "wp-empty";
  empty.textContent = text;
  return empty;
}

/**
 * @param {string} text
 * @param {string} className
 * @param {string} [title]
 * @returns {HTMLSpanElement}
 */
function rowTag(text, className, title = "") {
  const tag = document.createElement("span");
  tag.className = className;
  tag.textContent = text;
  tag.title = title;
  return tag;
}

/**
 * @param {Array<[string, string]>} pairs
 * @returns {HTMLElement[]} `<dt>` / `<dd>` pairs of a detail list
 */
function detailRows(pairs) {
  return pairs.flatMap(([term, value]) => {
    const dt = document.createElement("dt");
    dt.textContent = term;
    const dd = document.createElement("dd");
    dd.textContent = value;
    return [dt, dd];
  });
}

/**
 * @param {?any} anchor
 * @returns {string}
 */
function anchorText(anchor) {
  if (!anchor) return "未找到传送入口";
  return anchor.name ? `${anchor.name} · ${anchor.entry}` : anchor.entry;
}

class CollectApp {
  constructor() {
    this.els = {
      canvasWrap: $("canvas-wrap"),
      glCanvas: $("gl-canvas"),
      markerCanvas: $("marker-canvas"),
      overlayCanvas: $("overlay-canvas"),
    };
    this.camera = new Camera();
    this.renderer = new Renderer(this.els.glCanvas);
    this.renderer.setMeshVisible(false);
    this.overlay = new Overlay(this.els.overlayCanvas);
    this.markerCtx = this.els.markerCanvas.getContext("2d");
    this.pan = new PanGesture(this.camera, this.els.overlayCanvas);
    this.box = new BoxSelection();
    /** Selection snapshots for Ctrl/⌘+Z / Ctrl/⌘+Shift+Z, cleared when the map or kind changes. */
    this.history = new UndoRedoHistory();
    /** @type {'replace'|'add'|'remove'} how the open box changes the selection */
    this.boxMode = "replace";
    /** @type {'select'|'pan'} */
    this.tool = "select";

    /** @type {any} */
    this.catalog = null;
    this.mapId = "";
    /** @type {Array<any>} points of the current map, `x`/`y` in basemap px */
    this.mapPoints = [];
    /** @type {Array<any>} the shown subset of {@link CollectApp#mapPoints} */
    this.visible = [];
    /** @type {Set<string>} */
    this.hiddenItems = new Set();
    /** @type {Set<string>} */
    this.selected = new Set();
    this.kind = "rare";
    /** @type {?Array<any>} registered routes, null until loaded */
    this.routes = null;
    this.routesError = "";
    /** @type {string[]} registry and generated files every removal rewrites */
    this.routeFiles = [];
    /** @type {Map<string, string[]>} point id -> ids of the registered routes collecting it */
    this.coverage = new Map();
    /** @type {?any} the registered route being viewed or regenerated */
    this.opened = null;
    /** @type {any} */
    this.result = null;
    /** @type {?{anchor:?any, segments:Array<any>}} the preview as a single segment */
    this.resultDisplay = null;
    /** @type {?{anchor:?any, segments:Array<any>}} what the map draws: the preview, else the opened route */
    this.display = null;
    /** @type {Array<any>} points of every segment of {@link CollectApp#display} */
    this.route = [];
    /** @type {Set<string>} selected points the route cannot walk to */
    this.excluded = new Set();
    /** @type {?ReturnType<typeof routePreviewData>} runtime expansion of the route */
    this.expanded = null;
    /** @type {?Object} */
    this.expandFailure = null;
    /** @type {'pending'|'done'|'failed'} */
    this.expandState = "pending";
    this.expandedCount = 0;
    this.expandedLength = 0;
    this.expandToken = 0;
    /** Bumped whenever the route is replaced or dropped, so a slower preview reply for an older selection is discarded. */
    this.previewToken = 0;
    /** True while a write request is in flight; blocks a second write of the same route. */
    this.applying = false;
    /** @type {'apply'|'remove'} what confirming the dialog does */
    this.dialogAction = "apply";
    /** @type {Map<string, HTMLImageElement>} */
    this.icons = new Map();
    /** @type {?{width:number,height:number}} */
    this.dims = null;
    this.basemapToken = 0;
    this.cssW = 800;
    this.cssH = 600;
    this.dpr = 1;
  }

  async boot() {
    initFeedback({status: $("status-label"), locator: $("view-label")});
    this.bindEvents();
    this.setTool(this.tool);
    observeSize(this.els.canvasWrap, () => this.resize());
    this.resize();
    this.renderLegend();
    try {
      this.catalog = await getCatalog();
    } catch (err) {
      setStatus(`读取采集物数据失败：${err.message}`, COLOR_ERROR);
      return;
    }
    this.renderMapRail();
    setStatus("正在读取已有路线…", COLOR_INFO);
    const loaded = this.loadRoutes();
    const shown = this.catalog.maps.length > 0 && (await this.setMap(this.catalog.maps[0].map));
    if (!(await loaded) || !shown) return;
    setStatus(
      `已载入 ${this.catalog.groups.length} 种采集物、${this.catalog.points.length} 个点与 ${this.routes.length} 条已有路线。` +
        "单击左侧路线查看或修改，或在地图上框选新建。",
      COLOR_INFO,
    );
  }

  // --- wiring -------------------------------------------------------------------------

  bindEvents() {
    const canvas = this.els.overlayCanvas;
    canvas.addEventListener("mousedown", (e) => this.onPointerDown(e));
    window.addEventListener("mousemove", (e) => this.onPointerMove(e));
    window.addEventListener("mouseup", (e) => this.onPointerUp(e));
    canvas.addEventListener("wheel", (e) => this.onWheel(e), {passive: false});
    canvas.addEventListener("contextmenu", (e) => e.preventDefault());
    document.addEventListener("keydown", (e) => this.onKeyDown(e));
    bindTemporaryPan({
      current: () => this.tool,
      panTool: () => "pan",
      setTool: (tool) => this.setTool(tool),
      keys: ["Alt", " "],
    });
    $("btn-zoom-in").addEventListener("click", () => this.zoomBy(ZOOM_IN_FACTOR));
    $("btn-zoom-out").addEventListener("click", () => this.zoomBy(ZOOM_OUT_FACTOR));
    $("btn-fit-view").addEventListener("click", () => this.fitView());
    $("btn-clear-view").addEventListener("click", () => this.clearView());

    $("type-search").addEventListener("input", () => this.renderTypeList());
    $("types-all").addEventListener("click", () => {
      this.hiddenItems.clear();
      this.afterFilterChange();
    });
    $("types-none").addEventListener("click", () => {
      for (const group of this.mapGroups()) this.hiddenItems.add(group.item);
      this.afterFilterChange();
    });
    $("selection-clear").addEventListener("click", () => this.updateSelection(new Set()));
    for (const button of $("route-kind").querySelectorAll("[data-kind]")) {
      button.addEventListener("click", () => this.setKind(button.dataset.kind));
    }
    $("route-preview").addEventListener("click", () => void this.preview());
    $("route-clear").addEventListener("click", () => (this.result ? this.setResult(null) : this.closeRoute()));
    $("copy-route").addEventListener("click", () => void this.copy(this.result?.route_text, "路线文件"));
    $("copy-path").addEventListener("click", () => void this.copy(this.result?.path_text, "path"));
    $("route-apply").addEventListener("click", () => this.openDialog("apply"));
    $("route-remove").addEventListener("click", () => this.openDialog("remove"));
    $("apply-dialog-cancel").addEventListener("click", () => ($("apply-dialog").hidden = true));
    $("apply-dialog-ok").addEventListener("click", () => void this.confirm());
  }

  resize() {
    const wrap = this.els.canvasWrap;
    this.cssW = wrap.clientWidth || 800;
    this.cssH = wrap.clientHeight || 600;
    this.dpr = window.devicePixelRatio || 1;
    this.renderer.resize(this.cssW, this.cssH, this.dpr);
    this.overlay.resize(this.cssW, this.cssH, this.dpr);
    const marker = this.els.markerCanvas;
    marker.width = Math.max(1, Math.round(this.cssW * this.dpr));
    marker.height = Math.max(1, Math.round(this.cssH * this.dpr));
    this.paint();
  }

  /** @param {'select'|'pan'} tool `pan` only while Alt is held */
  setTool(tool) {
    this.tool = tool;
    this.els.overlayCanvas.style.cursor = TOOL_CURSORS[tool];
  }

  /**
   * Rare and common routes collect disjoint sets of collectibles, so the kind is also the filter:
   * switching it lists and shows only that kind and starts a fresh selection.
   * @param {'rare'|'common'} kind
   */
  setKind(kind) {
    if (kind === this.kind) return;
    this.kind = kind;
    for (const button of $("route-kind").querySelectorAll("[data-kind]")) {
      const active = button.dataset.kind === kind;
      button.classList.toggle("active", active);
      button.setAttribute("aria-pressed", String(active));
    }
    this.opened = null;
    this.selected = new Set();
    this.history.clear();
    this.setResult(null);
    this.refreshVisible();
    this.renderRouteList();
    this.renderTypeList();
    this.renderSelection();
    this.renderViewLabel();
    this.paint();
  }

  // --- map + filter -------------------------------------------------------------------

  renderMapRail() {
    $("map-rail").replaceChildren(
      ...this.catalog.maps.map((map) => {
        const button = document.createElement("button");
        button.type = "button";
        button.className = "rail-item";
        button.dataset.map = map.map;
        button.innerHTML = MAP_ICON;
        button.append(map.name);
        button.addEventListener("click", () => void this.setMap(map.map));
        return button;
      }),
    );
  }

  /**
   * @param {string} mapId
   * @returns {Promise<boolean>} false when the basemap failed to load
   */
  async setMap(mapId) {
    const map = this.catalog.maps.find((item) => item.map === mapId);
    this.mapId = mapId;
    this.mapPoints = this.catalog.points
      .filter((point) => point.map === mapId)
      .map((point) => ({...point, x: point.u, y: point.v}));
    this.opened = null;
    this.selected = new Set();
    this.history.clear();
    this.setResult(null);
    for (const button of $("map-rail").children) {
      const active = button.dataset.map === mapId;
      button.classList.toggle("active", active);
      button.setAttribute("aria-pressed", String(active));
    }
    this.refreshVisible();
    this.renderRouteList();
    this.renderTypeList();
    this.renderSelection();

    const token = ++this.basemapToken;
    const image = new Image();
    image.src = basemapUrl(map.image);
    try {
      await image.decode();
    } catch {
      setStatus(`底图加载失败：${map.image}`, COLOR_ERROR);
      return false;
    }
    if (token !== this.basemapToken) return true;
    this.dims = {width: image.naturalWidth, height: image.naturalHeight};
    this.renderer.setBasemap(image, this.dims);
    this.fitView();
    return true;
  }

  /** @returns {Array<any>} groups of the current kind with points on the current map */
  mapGroups() {
    return this.catalog.groups.filter((group) => group.kind === this.kind && (group.counts[this.mapId] || 0) > 0);
  }

  refreshVisible() {
    const kindOf = new Map(this.catalog.groups.map((group) => [group.item, group.kind]));
    this.visible = this.mapPoints.filter(
      (point) => kindOf.get(point.item) === this.kind && !this.hiddenItems.has(point.item),
    );
  }

  afterFilterChange() {
    this.refreshVisible();
    this.renderTypeList();
    const shown = new Set(this.visible.map((point) => point.id));
    this.updateSelection(new Set([...this.selected].filter((id) => shown.has(id))));
    this.renderViewLabel();
  }

  /** @returns {Map<string, number>} selected point count per item */
  selectedPerItem() {
    const counts = new Map();
    for (const point of this.mapPoints) {
      if (this.selected.has(point.id)) counts.set(point.item, (counts.get(point.item) || 0) + 1);
    }
    return counts;
  }

  /** @returns {Map<string, number>} per item, points of the current map collected by a registered route */
  coveredPerItem() {
    const counts = new Map();
    for (const point of this.mapPoints) {
      if (this.coverage.has(point.id)) counts.set(point.item, (counts.get(point.item) || 0) + 1);
    }
    return counts;
  }

  renderTypeList() {
    const query = $("type-search").value.trim().toLowerCase();
    const picked = this.selectedPerItem();
    const covered = this.coveredPerItem();
    const rows = this.mapGroups()
      .filter((group) => !query || Object.values(group.names).some((name) => name.toLowerCase().includes(query)))
      .map((group) => {
        const shown = !this.hiddenItems.has(group.item);
        const row = document.createElement("label");
        row.className = "wp-row chk collect-type";
        row.classList.toggle("off", !shown);
        row.classList.toggle("wp-row-selected", picked.has(group.item));
        row.title = group.item;

        const box = document.createElement("input");
        box.type = "checkbox";
        box.checked = shown;
        box.addEventListener("change", () => {
          if (box.checked) this.hiddenItems.delete(group.item);
          else this.hiddenItems.add(group.item);
          this.afterFilterChange();
        });
        const name = document.createElement("span");
        name.className = "wp-action";
        name.textContent = group.names.zh_cn;
        row.append(box, this.iconElement(group.icon), name);
        if (group.action === "DIG") row.append(rowTag("挖掘", "wp-tier"));
        if (picked.has(group.item)) row.append(rowTag(`已选 ${picked.get(group.item)}`, "wp-strict"));
        const count = document.createElement("span");
        count.className = "wp-coord";
        count.textContent = `${covered.get(group.item) || 0}/${group.counts[this.mapId]}`;
        row.append(count);
        return row;
      });
    if (!rows.length) rows.push(emptyRow(query ? "没有匹配的采集物" : "当前地图没有该类采集物"));
    $("type-list").replaceChildren(...rows);
  }

  /**
   * @param {string} name icon file stem under `UI/Item/`
   * @returns {HTMLImageElement} a list-row icon, hidden when the file is missing
   */
  iconElement(name) {
    const icon = document.createElement("img");
    icon.className = "collect-type-icon";
    icon.alt = "";
    icon.addEventListener("error", () => (icon.style.visibility = "hidden"), {once: true});
    icon.src = this.icon(name).src;
    return icon;
  }

  /**
   * @param {string} name icon file stem under `UI/Item/`
   * @returns {HTMLImageElement}
   */
  icon(name) {
    let image = this.icons.get(name);
    if (!image) {
      image = new Image();
      image.addEventListener("load", () => this.paint());
      image.src = basemapUrl(`UI/Item/${name}.png`);
      this.icons.set(name, image);
    }
    return image;
  }

  // --- registered routes --------------------------------------------------------------

  /** @returns {Promise<boolean>} false when the routes could not be read */
  async loadRoutes() {
    try {
      const {routes, files} = await getRoutes();
      this.routes = routes;
      this.routeFiles = files;
      this.routesError = "";
    } catch (err) {
      this.routes = null;
      this.routesError = err.message;
      setStatus(`读取已有路线失败：${err.message}`, COLOR_ERROR);
    }
    this.coverage = new Map();
    for (const route of this.routes || []) {
      for (const id of route.point_ids) this.coverage.set(id, [...(this.coverage.get(id) || []), route.id]);
    }
    this.renderRouteList();
    this.renderTypeList();
    this.paint();
    return this.routes !== null;
  }

  /** @returns {Array<any>} registered routes of the current map and kind */
  mapRoutes() {
    return (this.routes || []).filter((route) => route.map === this.mapId && route.kind === this.kind);
  }

  renderRouteList() {
    const routes = this.mapRoutes();
    $("route-count").textContent = this.routes ? `${routes.length} 条` : "";
    const rows = routes.map((route) => {
      const row = document.createElement("div");
      row.className = "wp-row collect-type";
      row.classList.toggle("wp-row-selected", this.opened?.id === route.id);
      row.title = `${route.id} · ${route.file}`;
      row.addEventListener("click", () => this.openRoute(route));
      const name = document.createElement("span");
      name.className = "wp-action";
      name.textContent = route.name;
      row.append(...route.items.map((item) => this.iconElement(item.icon)), name);
      if (route.error) row.append(rowTag("读取失败", "wp-strict", route.error));
      else if (route.notes.length) row.append(rowTag("特殊结构", "wp-tier", route.notes.join("；")));
      row.append(rowTag(`${route.point_ids.length} 点`, "wp-coord"));
      return row;
    });
    if (!rows.length) {
      const text = this.routesError
        ? "读取失败，详见状态栏"
        : this.routes
          ? `当前地图还没有${KIND_NAMES[this.kind]}路线`
          : "正在读取…";
      rows.push(emptyRow(text));
    }
    $("route-list").replaceChildren(...rows);
  }

  /**
   * Show a registered route as its file stands and select the points it collects, so a box edit
   * followed by「重新生成」replaces it.
   * @param {any} route
   */
  openRoute(route) {
    if (route.error) {
      setStatus(`${route.id} 读取失败：${route.error}`, COLOR_ERROR);
      return;
    }
    this.opened = route;
    for (const item of route.items) this.hiddenItems.delete(item.item);
    this.selected = new Set(route.point_ids);
    this.history.clear();
    this.setResult(null);
    this.refreshVisible();
    this.renderRouteList();
    this.renderTypeList();
    this.renderSelection();
    this.renderViewLabel();
    this.fitView();
    setStatus(`已打开 ${route.id}，框选增减采集点后点「重新生成」可替换该路线。`, COLOR_INFO);
  }

  closeRoute() {
    if (!this.opened) return;
    this.opened = null;
    this.selected = new Set();
    this.history.clear();
    this.setResult(null);
    this.renderRouteList();
    this.renderTypeList();
    this.renderSelection();
  }

  // --- selection ----------------------------------------------------------------------

  /**
   * @param {Set<string>} next
   * @param {{record?: boolean}} [options] `record: false` for undo / redo, which restore a snapshot
   */
  updateSelection(next, {record = true} = {}) {
    const changed = next.size !== this.selected.size || [...next].some((id) => !this.selected.has(id));
    if (changed && record) this.history.snapshot([...this.selected]);
    this.selected = next;
    if (changed) {
      this.setResult(null);
      this.renderTypeList();
    }
    this.renderSelection();
    this.paint();
  }

  renderSelection() {
    const picked = this.selectedPerItem();
    const names = new Map(this.catalog ? this.catalog.groups.map((group) => [group.item, group.names.zh_cn]) : []);
    $("selection-count").textContent = this.selected.size ? `已选 ${this.selected.size} 个采集物` : "未选择采集物";
    $("selection-clear").disabled = this.selected.size === 0;
    const summary = $("selection-summary");
    summary.hidden = picked.size === 0;
    summary.textContent = [...picked].map(([item, count]) => `${names.get(item) || item} ×${count}`).join(" · ");
    $("route-preview").disabled = this.selected.size === 0;
    $("route-preview").textContent = this.opened ? `重新生成 ${this.opened.id}` : `生成${KIND_NAMES[this.kind]}路线`;
  }

  /** @returns {(wx:number, wy:number) => [number, number]} */
  worldToCanvasFn() {
    return (wx, wy) => this.camera.worldToCanvas(wx, wy);
  }

  /**
   * @param {number} x @param {number} y canvas px
   * @returns {?any} nearest shown point under the pointer
   */
  hit(x, y) {
    const indices = this.visible.map((_, i) => i);
    const idx = PointEditing.hitTest(this.visible, indices, this.worldToCanvasFn(), x, y, HIT_RADIUS);
    return idx === null ? null : this.visible[idx];
  }

  /** @param {number} x @param {number} y */
  toggleAt(x, y) {
    const point = this.hit(x, y);
    if (!point) return;
    const next = new Set(this.selected);
    if (next.has(point.id)) next.delete(point.id);
    else next.add(point.id);
    this.updateSelection(next);
  }

  /**
   * @param {{x0:number,y0:number,x1:number,y1:number}} rect
   * @param {'replace'|'add'|'remove'} mode
   */
  selectRect(rect, mode) {
    const indices = PointEditing.indicesInRect(
      this.visible,
      this.visible.map((_, i) => i),
      this.worldToCanvasFn(),
      rect.x0,
      rect.y0,
      rect.x1,
      rect.y1,
    );
    const next = mode === "replace" ? new Set() : new Set(this.selected);
    for (const idx of indices) {
      if (mode === "remove") next.delete(this.visible[idx].id);
      else next.add(this.visible[idx].id);
    }
    this.updateSelection(next);
  }

  // --- pointer + keys: left drag boxes, a click toggles one point, right / middle / Alt drag pans ---

  /** @param {MouseEvent} e */
  onPointerDown(e) {
    const [x, y] = canvasPoint(e, this.els.overlayCanvas);
    if (e.button === 1 || e.button === 2 || (e.button === 0 && this.tool === "pan")) {
      e.preventDefault();
      this.pan.start(x, y);
      return;
    }
    if (e.button !== 0) return;
    this.box.start(x, y);
    this.boxMode = e.ctrlKey || e.metaKey ? "remove" : e.shiftKey ? "add" : "replace";
  }

  /** @param {MouseEvent} e */
  onPointerMove(e) {
    const [x, y] = canvasPoint(e, this.els.overlayCanvas);
    if (this.pan.active) {
      this.pan.move(x, y);
      this.paint();
      return;
    }
    if (this.box.active) {
      this.box.move(x, y);
      this.paint();
      return;
    }
    if (e.target === this.els.overlayCanvas) this.updateHover(x, y);
  }

  /** @param {MouseEvent} e */
  onPointerUp(e) {
    const [x, y] = canvasPoint(e, this.els.overlayCanvas);
    if (this.pan.active) {
      this.pan.cancel();
      this.setTool(this.tool);
      this.updateHover(x, y);
      this.paint();
      return;
    }
    const box = this.box.finish(x, y);
    if (box) {
      if (box.click) this.toggleAt(x, y);
      else this.selectRect(box, this.boxMode);
      this.paint();
    }
  }

  /** @param {number} x @param {number} y */
  updateHover(x, y) {
    if (!this.catalog) return;
    const point = this.tool === "pan" ? null : this.hit(x, y);
    this.els.overlayCanvas.style.cursor = point ? "pointer" : TOOL_CURSORS[this.tool];
    if (!point) {
      this.renderViewLabel();
      return;
    }
    const group = this.catalog.groups.find((item) => item.item === point.item);
    const state = this.selected.has(point.id) ? "已选" : "未选";
    const routes = this.coverage.get(point.id);
    const covered = routes ? ` · 已有路线 ${routes.join("、")}` : "";
    $("view-label").textContent =
      `${group ? group.names.zh_cn : point.item} · [${point.u}, ${point.v}] · ${state}${covered}`;
  }

  renderViewLabel() {
    const map = this.catalog?.maps.find((item) => item.map === this.mapId);
    $("view-label").textContent = map ? `${map.name} · 显示 ${this.visible.length} 个点` : "";
  }

  /** @param {WheelEvent} e */
  onWheel(e) {
    e.preventDefault();
    const [x, y] = canvasPoint(e, this.els.overlayCanvas);
    this.camera.zoomAt(x, y, wheelZoomFactor(e, this.cssH));
    this.paint();
  }

  /** @param {number} factor */
  zoomBy(factor) {
    this.camera.zoomAt(this.cssW / 2, this.cssH / 2, factor);
    this.paint();
  }

  /** @param {KeyboardEvent} e */
  onKeyDown(e) {
    if (!$("apply-dialog").hidden) {
      if (e.key === "Escape") $("apply-dialog").hidden = true;
      return;
    }
    if (isFormField(e.target)) return;
    if (e.key === "Escape") {
      this.escapeStep()?.run();
      e.preventDefault();
      return;
    }
    const ctrl = e.ctrlKey || e.metaKey;
    const key = e.key.toLowerCase();
    if (ctrl && key === "a") {
      this.updateSelection(new Set(this.visible.map((point) => point.id)));
      e.preventDefault();
      return;
    }
    if (ctrl && (key === "z" || key === "y")) {
      this.stepHistory(key === "y" || e.shiftKey);
      e.preventDefault();
      return;
    }
    if (e.key === "Delete" || e.key === "Backspace") {
      if (this.selected.size) {
        this.updateSelection(new Set());
        setStatus("已移除全部所选采集物。", COLOR_OK);
      }
      e.preventDefault();
      return;
    }
    if (!ctrl && key === "c") {
      void this.copySelectedCoordinates();
      return;
    }
    if (isZoomInKey(e)) {
      this.zoomBy(ZOOM_IN_FACTOR);
      e.preventDefault();
    } else if (isZoomOutKey(e)) {
      this.zoomBy(ZOOM_OUT_FACTOR);
      e.preventDefault();
    }
  }

  /**
   * What Esc cancels next: the open box, then the preview, then the opened route, then the selection.
   * @returns {?{label:string, run:() => void}}
   */
  escapeStep() {
    if (this.box.active) {
      return {
        label: "取消框选",
        run: () => {
          this.box.cancel();
          this.paint();
        },
      };
    }
    if (this.result) return {label: "收起预览", run: () => this.setResult(null)};
    if (this.opened) {
      return {
        label: "关闭路线",
        run: () => {
          this.closeRoute();
          setStatus("已关闭路线。", COLOR_OK);
        },
      };
    }
    if (this.selected.size) {
      return {
        label: "取消选择",
        run: () => {
          this.updateSelection(new Set());
          setStatus("已取消当前选择。", COLOR_OK);
        },
      };
    }
    return null;
  }

  /** Back to a blank map: drop the preview, close the opened route and clear the selection. */
  clearView() {
    this.box.cancel();
    if (this.opened) this.closeRoute();
    else this.updateSelection(new Set());
    setStatus("已清空地图显示。", COLOR_OK);
  }

  /** The toolbar spells out what Esc and Delete would do right now. */
  renderKeyHints() {
    const escape = this.escapeStep()?.label;
    const hints = escape ? [`Esc ${escape}`] : [];
    if (this.selected.size && escape !== "取消选择") hints.push("Delete 清空选择");
    const text = hints.join(" · ");
    const hint = $("key-hint");
    if (hint.textContent !== text) hint.textContent = text;
    hint.hidden = !text;
    $("btn-clear-view").disabled = !this.result && !this.opened && !this.selected.size;
  }

  /** @param {boolean} redo */
  stepHistory(redo) {
    const current = [...this.selected];
    const restored = redo ? this.history.redo(current) : this.history.undo(current);
    if (!restored) {
      setStatus(redo ? "没有可重做的选择。" : "没有可撤销的选择。", COLOR_INFO);
      return;
    }
    this.updateSelection(new Set(restored), {record: false});
    setStatus(`${redo ? "已重做" : "已撤销"}选择，当前已选 ${this.selected.size} 个采集物。`, COLOR_OK);
  }

  /** C: the selected collectibles' coordinates as `[u, v]`, one per line (same format as the editor). */
  async copySelectedCoordinates() {
    const points = this.mapPoints.filter((point) => this.selected.has(point.id));
    if (!points.length) return;
    const text = points.map((point) => `[${compactNumber(point.u)}, ${compactNumber(point.v)}]`).join(",\n");
    if (!(await copyText(text))) {
      setStatus("复制坐标失败。", COLOR_ERROR);
      return;
    }
    setStatus(points.length === 1 ? `已复制坐标：${text}` : `已复制 ${points.length} 个采集物的坐标。`, COLOR_OK);
  }

  /** Frame the route, else the selection, else the whole basemap. */
  fitView() {
    if (!this.dims) return;
    const anchor = this.display?.anchor;
    const points = this.display
      ? [...this.route, ...(anchor && "u" in anchor ? [{x: anchor.u, y: anchor.v}] : [])]
      : this.mapPoints.filter((point) => this.selected.has(point.id));
    if (points.length) {
      const xs = points.map((point) => point.x);
      const ys = points.map((point) => point.y);
      this.camera.fitView(
        [Math.min(...xs), Math.min(...ys), Math.max(...xs), Math.max(...ys)],
        this.cssW,
        this.cssH,
        60,
        LEFT_PANEL_FIT_OFFSET,
      );
    } else {
      this.camera.fitView([0, 0, this.dims.width, this.dims.height], this.cssW, this.cssH, 60, LEFT_PANEL_FIT_OFFSET);
    }
    this.renderViewLabel();
    this.paint();
  }

  // --- drawing ------------------------------------------------------------------------

  paint() {
    this.renderer.requestRender(this.camera);
    this.renderKeyHints();
    this.drawMarkers();
    // 路线、运行时展开与选框都按路径编辑器的画法出图；底图就是 base，展开结果无需换坐标系
    this.overlay.render(this.camera, {
      mode: "edit",
      points: this.route,
      selectedIdx: null,
      selectedIndices: new Set(),
      editPreview: routePreviewForDisplay(this.expanded, this.expandFailure, (point) => point),
      selectionRect: this.box.rect,
    });
  }

  /**
   * @param {number} x @param {number} y canvas px
   * @param {number} margin
   * @returns {boolean}
   */
  onScreen(x, y, margin) {
    return x > -margin && y > -margin && x < this.cssW + margin && y < this.cssH + margin;
  }

  drawMarkers() {
    const ctx = this.markerCtx;
    ctx.setTransform(this.dpr, 0, 0, this.dpr, 0, 0);
    ctx.clearRect(0, 0, this.cssW, this.cssH);
    if (!this.catalog) return;
    this.drawAnchors(ctx);
    const badges = this.camera.viewScale >= ICON_SCALE_MIN;
    const icons = new Map(this.catalog.groups.map((group) => [group.item, group.icon]));
    // selected markers go last so a crowded neighbour never hides the ring
    for (const selected of [false, true]) {
      for (const point of this.visible) {
        if (this.selected.has(point.id) !== selected) continue;
        const [x, y] = this.camera.worldToCanvas(point.x, point.y);
        if (!this.onScreen(x, y, BADGE_RADIUS + 4)) continue;
        const ring = selected
          ? this.excluded.has(point.id)
            ? EXCLUDED_COLOR
            : SELECTED_COLOR
          : this.coverage.has(point.id)
            ? COVERED_COLOR
            : null;
        if (badges) this.drawBadge(ctx, x, y, this.icon(icons.get(point.item)), point.item, ring);
        else this.drawDot(ctx, x, y, point.item, ring);
      }
    }
  }

  /**
   * Item icon on a round dark badge; the selection ring matches the editor's selected nodes.
   * @param {CanvasRenderingContext2D} ctx
   * @param {number} x @param {number} y
   * @param {HTMLImageElement} image
   * @param {string} item
   * @param {?string} ring selection or coverage ring color, null for neither
   */
  drawBadge(ctx, x, y, image, item, ring) {
    ctx.save();
    ctx.beginPath();
    ctx.arc(x, y, BADGE_RADIUS, 0, Math.PI * 2);
    ctx.fillStyle = BADGE_FILL;
    ctx.fill();
    ctx.lineWidth = ring ? 2 : 1;
    ctx.strokeStyle = ring || "rgba(255, 255, 255, 0.35)";
    ctx.stroke();
    if (image.complete && image.naturalWidth > 0) {
      ctx.save();
      ctx.beginPath();
      ctx.arc(x, y, BADGE_RADIUS - 1.5, 0, Math.PI * 2);
      ctx.clip();
      ctx.drawImage(image, x - ICON_SIZE / 2, y - ICON_SIZE / 2, ICON_SIZE, ICON_SIZE);
      ctx.restore();
    } else {
      ctx.beginPath();
      ctx.arc(x, y, DOT_RADIUS, 0, Math.PI * 2);
      ctx.fillStyle = itemColor(item);
      ctx.fill();
    }
    if (ring) {
      ctx.beginPath();
      ctx.arc(x, y, BADGE_RADIUS + 3, 0, Math.PI * 2);
      ctx.lineWidth = 1.5;
      ctx.strokeStyle = ring;
      ctx.stroke();
    }
    ctx.restore();
  }

  /**
   * @param {CanvasRenderingContext2D} ctx
   * @param {number} x @param {number} y
   * @param {string} item
   * @param {?string} ring selection or coverage ring color, null for neither
   */
  drawDot(ctx, x, y, item, ring) {
    ctx.save();
    ctx.beginPath();
    ctx.arc(x, y, DOT_RADIUS, 0, Math.PI * 2);
    ctx.fillStyle = itemColor(item);
    ctx.fill();
    ctx.lineWidth = 1;
    ctx.strokeStyle = OUTLINE_COLOR;
    ctx.stroke();
    if (ring) {
      ctx.beginPath();
      ctx.arc(x, y, DOT_RADIUS + 3, 0, Math.PI * 2);
      ctx.lineWidth = 1.5;
      ctx.strokeStyle = ring;
      ctx.stroke();
    }
    ctx.restore();
  }

  /**
   * Teleport anchors of the current map; the route's start anchor is enlarged and named. Until the
   * runtime expansion arrives, a dashed leg ties it to the first point.
   * @param {CanvasRenderingContext2D} ctx
   */
  drawAnchors(ctx) {
    const chosen = this.display?.anchor;
    if (chosen && "u" in chosen && this.route.length && !this.expanded) {
      const [ax, ay] = this.camera.worldToCanvas(chosen.u, chosen.v);
      const [fx, fy] = this.camera.worldToCanvas(this.route[0].x, this.route[0].y);
      ctx.save();
      ctx.setLineDash([6, 5]);
      ctx.lineWidth = 2;
      ctx.strokeStyle = ANCHOR_COLOR;
      ctx.beginPath();
      ctx.moveTo(ax, ay);
      ctx.lineTo(fx, fy);
      ctx.stroke();
      ctx.restore();
    }
    for (const anchor of this.catalog.anchors) {
      if (anchor.map !== this.mapId) continue;
      const [x, y] = this.camera.worldToCanvas(anchor.u, anchor.v);
      if (!this.onScreen(x, y, 80)) continue;
      const isChosen = chosen?.id === anchor.id;
      const size = isChosen ? 8 : 5;
      ctx.save();
      ctx.translate(x, y);
      ctx.beginPath();
      ctx.moveTo(0, -size);
      ctx.lineTo(size, 0);
      ctx.lineTo(0, size);
      ctx.lineTo(-size, 0);
      ctx.closePath();
      ctx.fillStyle = isChosen ? ANCHOR_COLOR : "rgba(34, 211, 238, 0.45)";
      ctx.fill();
      ctx.lineWidth = isChosen ? 2 : 1.5;
      ctx.strokeStyle = "rgba(8, 15, 28, 0.85)";
      ctx.stroke();
      ctx.restore();
      if (isChosen) this.drawCaption(ctx, x, y - size - 12, anchor.name || chosen.name);
    }
  }

  /**
   * @param {CanvasRenderingContext2D} ctx
   * @param {number} cx @param {number} cy
   * @param {string} text
   */
  drawCaption(ctx, cx, cy, text) {
    if (!text) return;
    ctx.save();
    ctx.font = `bold 11px ${MONO}`;
    ctx.textAlign = "center";
    ctx.textBaseline = "middle";
    const w = ctx.measureText(text).width + 12;
    const h = 18;
    ctx.fillStyle = "rgba(11, 18, 32, 0.85)";
    ctx.strokeStyle = ANCHOR_COLOR;
    ctx.lineWidth = 1;
    ctx.beginPath();
    ctx.rect(cx - w / 2, cy - h / 2, w, h);
    ctx.fill();
    ctx.stroke();
    ctx.fillStyle = ANCHOR_COLOR;
    ctx.fillText(text, cx, cy);
    ctx.restore();
  }

  renderLegend() {
    const item = (label, style, strict = false) => {
      const row = document.createElement("div");
      row.className = "legend-item";
      const dot = document.createElement("span");
      dot.className = strict ? "legend-dot-strict" : "legend-dot";
      Object.assign(dot.style, style);
      if (strict) {
        const core = document.createElement("span");
        core.className = "legend-dot-strict-inner";
        core.style.backgroundColor = ACTION_COLORS[ActionType.COLLECT];
        dot.append(core);
      }
      const text = document.createElement("span");
      text.textContent = label;
      row.append(dot, text);
      return row;
    };
    const title = document.createElement("div");
    title.className = "legend-title";
    title.textContent = "图例";
    const grid = document.createElement("div");
    grid.className = "legend-grid";
    grid.append(
      item("已选采集物", {background: "transparent", borderColor: SELECTED_COLOR, borderWidth: "2px"}),
      item("已有路线", {background: "transparent", borderColor: COVERED_COLOR, borderWidth: "2px"}),
      item("不连通", {background: "transparent", borderColor: EXCLUDED_COLOR, borderWidth: "2px"}),
      item("传送点", {background: ANCHOR_COLOR, borderRadius: "1px", transform: "rotate(45deg)"}),
      item("运行时展开", {backgroundColor: "#ff007f", borderRadius: "2px", height: "3px", width: "12px"}),
      item(ACTION_NAMES[ActionType.COLLECT], {backgroundColor: ACTION_COLORS[ActionType.COLLECT]}),
      item(ACTION_NAMES[ActionType.DIG], {backgroundColor: ACTION_COLORS[ActionType.DIG]}),
      item(ACTION_NAMES[ActionType.NAVMESH], {backgroundColor: ACTION_COLORS[ActionType.NAVMESH]}),
      item("严格模式", {}, true),
    );
    $("collect-legend").replaceChildren(title, grid);
  }

  // --- route --------------------------------------------------------------------------

  /** @param {any} result preview response, or null to drop it */
  setResult(result) {
    this.result = result;
    this.resultDisplay = result && {
      anchor: result.anchor,
      segments: [
        {
          start: {position: [result.anchor.u, result.anchor.v], zone: result.zone_id},
          path: result.path,
          points: routePoints(result.path),
        },
      ],
    };
    this.excluded = new Set(result ? [...result.unreachable, ...result.blocked] : []);
    this.previewToken += 1;
    this.refreshDisplay();
    this.renderResult();
    this.paint();
  }

  /** Draw the preview, else the opened route, and expand it whenever that changes. */
  refreshDisplay() {
    const display = this.resultDisplay || this.opened;
    if (display === this.display) return;
    this.display = display;
    this.route = display ? display.segments.flatMap((segment) => segment.points) : [];
    this.expanded = null;
    this.expandFailure = null;
    this.expandState = "pending";
    this.expandToken += 1;
    if (display) void this.expand();
  }

  /** Expand each navigation segment with the same runtime preview the editor uses, from where the route starts it. */
  async expand() {
    const display = this.display;
    const token = ++this.expandToken;
    this.expandState = "pending";
    this.renderResult();
    const merged = {points: [], walk_segments: [], zipline_segments: [], rope_segments: []};
    let count = 0;
    let length = 0;
    try {
      for (const [index, segment] of display.segments.entries()) {
        const response = await postRoutePreview({
          position: segment.start.position,
          position_zone: segment.start.zone,
          custom_action_param: {path: segment.path},
        });
        if (token !== this.expandToken || response?.stale) return;
        if (!response || !response.ok) {
          this.expandFailure = response?.failure || null;
          const where = display.segments.length > 1 ? `第 ${index + 1} 段 ${segment.node}：` : "";
          throw new Error(`${where}${response?.error || "路线展开失败"}`);
        }
        const points = response.points || [];
        for (const key of Object.keys(merged)) merged[key].push(...(response[key] || []));
        count += response.expanded_waypoints || points.length;
        length += points
          .slice(1)
          .reduce((sum, point, i) => sum + Math.hypot(point[0] - points[i][0], point[1] - points[i][1]), 0);
      }
      this.expandState = "done";
      if (this.result) {
        const subject = `${this.result.id} 已按运行时展开`;
        setStatus(...routePreviewStatus(routePreviewData(merged), {expanded_waypoints: count}, subject));
      }
    } catch (err) {
      if (token !== this.expandToken) return;
      this.expandState = "failed";
      setStatus(`路线展开失败：${err.message}`, COLOR_ERROR);
    }
    this.expanded = merged.points.length ? routePreviewData(merged) : null;
    this.expandedCount = count;
    this.expandedLength = Math.round(length);
    this.renderResult();
    this.paint();
  }

  /** @returns {{map:string, ids:string[], kind:string, replace:string}} */
  request() {
    return {map: this.mapId, ids: [...this.selected], kind: this.kind, replace: this.opened?.id || ""};
  }

  async preview() {
    if (!this.selected.size) return;
    const token = ++this.previewToken;
    $("route-preview").disabled = true;
    setStatus("正在按步行距离排序采集点…", COLOR_INFO);
    let sorting = true;
    const poll = window.setInterval(async () => {
      try {
        const {queries} = await getJson("/api/progress");
        if (sorting && token === this.previewToken) {
          setStatus(`正在按步行距离排序采集点（已规划 ${queries} 段）…`, COLOR_INFO);
        }
      } catch {
        // the status line keeps the last reported progress
      }
    }, PROGRESS_POLL_MS);
    try {
      const result = await previewRoute(this.request());
      sorting = false;
      if (token !== this.previewToken) return;
      this.setResult(result);
      this.fitView();
      if (this.excluded.size) {
        setStatus(
          `${this.excluded.size} 个采集物与起点不连通（隔水、断崖或需从其他传送点进入），已标红并排除，可另行生成路线。`,
          COLOR_ERROR,
        );
      } else {
        setStatus("正在按运行时语义展开路线…", COLOR_INFO);
      }
    } catch (err) {
      sorting = false;
      if (token !== this.previewToken) return;
      this.setResult(null);
      setStatus(`生成失败：${err.message}`, COLOR_ERROR);
    } finally {
      window.clearInterval(poll);
      $("route-preview").disabled = this.selected.size === 0;
    }
  }

  renderResult() {
    const result = this.result;
    const route = this.opened;
    $("route-box").hidden = !result && !route;
    if (!result && !route) return;
    $("route-clear").textContent = result ? "清除" : "关闭";
    $("route-copy").hidden = !result;
    $("route-apply").hidden = !result;
    $("route-remove").hidden = !!result;
    if (result) {
      const renamed = result.replace && result.replace !== result.id ? `${result.replace} → ` : "";
      $("route-title").textContent = `${result.replace ? "重新生成" : "路线预览"} · ${renamed}${result.id}`;
    } else {
      $("route-title").textContent = `已有路线 · ${route.id}`;
    }
    $("route-facts").replaceChildren(...detailRows(result ? this.resultFacts(result) : this.routeFacts(route)));
  }

  /** @returns {string} */
  expansionText() {
    return {
      pending: "正在展开…",
      done: `${this.expandedCount} 个运行时路点，步行 ${this.expandedLength} px`,
      failed: "展开失败，详见状态栏",
    }[this.expandState];
  }

  /**
   * @param {any} result
   * @returns {Array<[string, string]>}
   */
  resultFacts(result) {
    const collects = result.path.filter((step) => Array.isArray(step));
    const digs = collects.filter((step) => step[2] === "DIG").length;
    const navmesh = result.path.filter((step) => step.action === "NAVMESH").length;
    const facts = [
      ["文件", result.route_file],
      ["起点传送", anchorText(result.anchor)],
      ["采集点", `${collects.length} 个${digs ? `（挖掘 ${digs}）` : ""}`],
      ["运行时寻路", `${navmesh} 段`],
      ["运行时展开", this.expansionText()],
      ["产物", result.items.map((item) => `${item.names.zh_cn} ×${item.count}`).join("、")],
    ];
    if (result.replace) {
      const before = new Set(this.opened.point_ids);
      const after = new Set(result.order);
      const added = [...after].filter((id) => !before.has(id)).length;
      const removed = [...before].filter((id) => !after.has(id)).length;
      facts.push(["改动", added || removed ? `新增 ${added} 个、移除 ${removed} 个采集点` : "采集点不变"]);
    }
    if (this.excluded.size) facts.push(["不连通", `${this.excluded.size} 个，已排除（红圈标记）`]);
    return facts;
  }

  /**
   * @param {any} route
   * @returns {Array<[string, string]>}
   */
  routeFacts(route) {
    const facts = [
      ["文件", route.file],
      ["起点传送", anchorText(route.anchor)],
      ["导航", `${route.segments.length} 段`],
      ["采集点", `${route.point_ids.length} 个`],
      ["运行时展开", this.expansionText()],
      ["产物", route.items.map((item) => item.names.zh_cn).join("、")],
    ];
    if (route.notes.length) facts.push(["特殊结构", route.notes.join("；")]);
    return facts;
  }

  /**
   * @param {string|undefined} text
   * @param {string} what
   */
  async copy(text, what) {
    if (!text) return;
    if (await copyText(text)) setStatus(`已复制${what}。`, COLOR_OK);
    else setStatus(`复制${what}失败。`, COLOR_ERROR);
  }

  /**
   * List every file the write or removal touches, and what it would lose: the hand-written structure of
   * the route being replaced, and files elsewhere that reference nodes about to disappear.
   * @param {'apply'|'remove'} action
   */
  openDialog(action) {
    const result = this.result;
    const route = this.opened;
    if (this.applying || (action === "apply" ? !result : !route)) return;
    this.dialogAction = action;
    let hint;
    let files;
    const warnings = [];
    if (action === "remove") {
      hint = `将删除 ${route.id}，并改动以下文件：`;
      files = [{path: route.file, action: "删除"}, ...this.routeFiles.map((path) => ({path, action: "修改"}))];
    } else if (!result.replace) {
      hint = `将新增 ${result.id}，并改动以下文件：`;
      files = result.files;
    } else {
      hint =
        result.id === result.replace
          ? `将用新生成的路线覆盖 ${result.id}：`
          : `采集物有变，将用 ${result.id} 替换 ${result.replace}，并改动以下文件：`;
      files = result.files;
      if (route.notes.length) warnings.push(`原路线的特殊结构不会保留：${route.notes.join("；")}。`);
    }
    for (const {file, nodes} of action === "remove" ? route.references : result.references) {
      warnings.push(`${file} 引用了 ${nodes.join("、")}，需同步修改。`);
    }
    $("apply-dialog-title").textContent = action === "remove" ? "删除路线" : "写入任务";
    $("apply-dialog-ok").textContent = action === "remove" ? "删除" : "写入";
    $("apply-dialog-ok").classList.toggle("btn-danger", action === "remove");
    $("apply-dialog-ok").classList.toggle("btn-primary", action !== "remove");
    $("apply-dialog-hint").textContent = hint;
    $("apply-dialog-files").replaceChildren(...detailRows(files.map((file) => [file.action, file.path])));
    $("apply-dialog-warnings").hidden = warnings.length === 0;
    $("apply-dialog-warnings").replaceChildren(
      ...warnings.map((text) => {
        const item = document.createElement("li");
        item.textContent = text;
        return item;
      }),
    );
    $("apply-dialog").hidden = false;
    $("apply-dialog-ok").focus();
  }

  async confirm() {
    if (this.applying) return;
    const action = this.dialogAction;
    this.applying = true;
    $("route-apply").disabled = true;
    $("route-remove").disabled = true;
    $("apply-dialog-ok").disabled = true;
    $("apply-dialog").hidden = true;
    try {
      if (action === "remove") {
        const removed = await removeRoute(this.opened.id);
        this.closeRoute();
        await this.loadRoutes();
        setStatus(`已删除 ${removed.id}：改动 ${removed.written.length} 个文件。`, COLOR_OK);
      } else {
        const written = await applyRoute(this.request());
        this.opened = null;
        this.setResult(null);
        this.renderSelection();
        await this.loadRoutes();
        const route = (this.routes || []).find((item) => item.id === written.id);
        if (route) this.openRoute(route);
        setStatus(`已写入 ${written.id}：${written.written.length} 个文件。`, COLOR_OK);
      }
    } catch (err) {
      setStatus(`${action === "remove" ? "删除" : "写入"}失败：${err.message}`, COLOR_ERROR);
    } finally {
      this.applying = false;
      $("route-apply").disabled = false;
      $("route-remove").disabled = false;
      $("apply-dialog-ok").disabled = false;
    }
  }
}

new CollectApp().boot();
