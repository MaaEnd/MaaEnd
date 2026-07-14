/**
 * main.js — keystone orchestrator. Boots the app and owns every piece of glue the
 * other modules don't: the render loop (`_doRedraw` / `_paint`, mirroring
 * app_tk `_do_redraw`), the pointer state machine (`on_click`/`on_drag`/`on_release`
 * + right-button pan), wheel/keyboard, the three mutually-exclusive modes
 * (edit / assert / A*), zone navigation, fit-view, the copy actions, and the wiring
 * of the {@link ConnectionPanel} / {@link RecordingController} / {@link Importer}
 * controllers.
 *
 * Rendering is a hybrid stack sharing ONE {@link Camera}: the WebGL {@link Renderer}
 * draws basemap + mesh + walkable-dot layers, the 2D {@link Overlay} draws the
 * state-coupled vectors (path / nodes / assert rect / A* preview / selection box).
 *
 * Coordinate frames (DESIGN §9): edit/assert basemaps live in their zone's own px;
 * an A* view showing a *translated* tier is in tier-px (basemap = tier template,
 * mesh remapped base→tier); every other A* view is base-px. Routing/snap always run
 * in base-px on the parent geometry zone — only display converts to tier-px.
 *
 * @module main
 */

import { Camera } from './camera.js';
import { Renderer } from './gl/renderer.js';
import { Overlay } from './gl/overlay.js';
import { NavmeshField } from './navmesh_field.js';
import { AppState, Mode } from './state.js';
import { ACTION_NAMES, ActionType, ACTION_MENU_NAMES, ACTION_COLORS, ACTION_MENU_TYPES, getPointActions, normalizeZoneId } from './model.js';
import { compactNumber, roundHalfEven } from './rounding.js';
import { initFeedback, setStatus, setLocator } from './ui/toast.js';
import { ConnectionPanel } from './ui/connection.js';
import { RecordingController } from './ui/recording.js';
import { Importer } from './ui/importer.js';
import {
  getZones,
  getLoadStatus,
  getMesh,
  getZoneIds,
  basemapByZoneUrl,
  postRoute,
  postOffMeshProbe,
  exportPath,
  exportAssert,
  locateOnce,
} from './rpc.js';

const DRAG_ACTIVATION_DISTANCE = 4; // px (tk RouteEditorApp.DRAG_ACTIVATION_DISTANCE)
const ASTAR_PREVIEW_SNAP_RADIUS = 5.0; // px (tk ASTAR_PREVIEW_SNAP_RADIUS)
const LOAD_POLL_MS = 400;
// CSS px the floating left panel overlays the canvas; fit-view centers in the rest.
const LEFT_PANEL_FIT_OFFSET = 350;
// World px kept around the A* preview markers when framing them.
const ASTAR_HINT_FIT_PADDING = 200;

/** round(value, 2) with CPython banker's rounding — parity for assert-target export. */
function bankerRound2(value) {
  return roundHalfEven(value, 2);
}

class MapNavigatorApp {
  constructor() {
    this.els = this._queryElements();

    this.camera = new Camera();
    this.renderer = new Renderer(this.els.glCanvas);
    this.overlay = new Overlay(this.els.overlayCanvas);
    this.state = new AppState();

    /** @type {?NavmeshField} */
    this.field = null;
    /** @type {string[]} assert-mode zone options (backend fs scan). */
    this._availableZoneIds = [];

    // --- pointer state machine (mirrors the tk is_* flags) ---
    /** @type {string} current canvas tool (see {@link MapNavigatorApp#_setActiveTool}). */
    this.activeTool = 'select';
    this.isDragging = false;
    this.isPanCandidate = false;
    this.isPanning = false;
    this.isBoxSelecting = false;
    this.isAssertSelecting = false;
    this.pointerDownX = 0;
    this.pointerDownY = 0;
    this.dragStartX = 0;
    this.dragStartY = 0;
    this.boxStartX = 0;
    this.boxStartY = 0;
    this.assertStartWorldX = 0;
    this.assertStartWorldY = 0;

    // --- assert / A* view state ---
    /** @type {?number[]} raw drag rect `[x0,y0,x1,y1]` in display-frame world. */
    this.assertRectWorld = null;
    /** @type {?number[]} assert locate hint coordinate [x, y]. */
    this.assertLocateHint = null;
    /**
     * A* **preview** markers in base px — from a game locate fix or a hand-entered
     * coordinate. Preview only: never part of `state.points` (the real route).
     * @type {Array<{x:number, y:number, label:string}>}
     */
    this.astarLocateHints = [];
    /** @type {?{x0:number,y0:number,x1:number,y1:number}} canvas-px selection box. */
    this.selectionRect = null;
    /** @type {Array<number[]>} A* path finder waypoints in display-frame world. */
    this.astarPoints = [];
    /** @type {?{points:number[][], segment_breaks:number[], cost:number}} */
    this.astarRoute = null;
    /**
     * Straight lines the runtime walks with no navmesh under it, base px: `off` is the
     * point outside the mesh, `mesh` where the mesh takes over. Taken from the backend's
     * actual ladder result, so this is what the character does — not an estimate.
     * @type {Array<{off:number[], mesh:number[], distance:number, kind:string}>}
     */
    this.astarBlindWalks = [];
    /**
     * Off-mesh points to badge, base px. `exact` marks the ones whose distance came from a
     * real route (a blind walk); the rest are geometric nearest-mesh probes — a *goal*'s
     * blind walk depends on the start, so those two numbers are not interchangeable.
     * @type {Array<{point:number[], distance:?number, nearest:?number[], exact:boolean, label:string}>}
     */
    this.offMeshMarks = [];
    /**
     * EDIT-mode off-mesh badges, in the points' own zone frame. Same shape as
     * {@link offMeshMarks} but always `exact:false` — a waypoint is a *goal*, and the
     * runtime's blind walk to a goal depends on where it starts from.
     * @type {Array<Object>}
     */
    this.editOffMeshMarks = [];
    /** Guards against a stale probe response overwriting a newer one. @type {number} */
    this._probeToken = 0;
    /** @type {?string} NAVMESH-waypoint signature the edit-mode badges were probed for. */
    this._offMeshSig = null;
    /** @type {?number} debounce handle for the edit-mode probe. */
    this._offMeshTimer = null;

    // --- basemap texture bookkeeping (async <img> load) ---
    /** @type {?string} zone name currently uploaded to the renderer basemap. */
    this._bgZone = null;
    /** @type {?{width:number,height:number}} */
    this._basemapDims = null;
    this._basemapLoading = false;
    this._basemapToken = 0;
    this._fitPending = false;

    // --- A* mesh bookkeeping ---
    /** @type {?string} `${geomId}:${tierId}` of the uploaded mesh. */
    this._meshKey = null;
    this._meshToken = 0;

    this._cssW = 800;
    this._cssH = 600;
    this._pointerMoveBound = (e) => this._onPointerMove(e);
    this._pointerUpBound = (e) => this._onPointerUp(e);
    this._animating = false;
  }

  /** @returns {?number[]} the first A* click point, or null. */
  get astarStart() {
    return this.astarPoints[0] || null;
  }

  /** @returns {?number[]} the last A* click point once ≥2 exist, or null. */
  get astarGoal() {
    return this.astarPoints.length >= 2 ? this.astarPoints[this.astarPoints.length - 1] : null;
  }

  /** @returns {?{x:number, y:number, label:string}} the most recent A* preview marker (base px), or null. */
  get astarLastHint() {
    return this.astarLocateHints.length ? this.astarLocateHints[this.astarLocateHints.length - 1] : null;
  }

  /** Resolve every DOM element main.js touches. @returns {Object} */
  _queryElements() {
    const $ = (id) => document.getElementById(id);
    return {
      app: $('app'),
      glCanvas: $('gl-canvas'),
      overlayCanvas: $('overlay-canvas'),
      canvasWrap: $('canvas-wrap'),
      btnStart: $('btn-start'),
      btnStop: $('btn-stop'),
      btnCopyPath: $('btn-copy-path'),
      btnCopyAssert: $('btn-copy-assert'),
      btnImport: $('btn-import'),
      fileInput: $('file-input'),
      btnPrev: $('btn-prev'),
      btnNext: $('btn-next'),
      zoneLabel: $('zone-label'),
      btnZoomOut: $('btn-zoom-out'),
      btnZoomIn: $('btn-zoom-in'),
      actionMenu: $('action-menu'),
      btnApplyAction: $('btn-apply-action'),
      actionChainLabel: $('action-chain-label'),
      assertZoneCombo: $('assert-zone-combo'),
      chkStrict: $('chk-strict'),
      toolPan: $('tool-pan'),
      toolAdd: $('tool-add'),
      toolSelect: $('tool-select'),
      toolAstarSingle: $('tool-astar-single'),
      toolAstarMulti: $('tool-astar-multi'),
      toolAssertPan: $('tool-assert-pan'),
      toolAssertEdit: $('tool-assert-edit'),
      kindCombo: $('connection-kind-combo'),
      win32Group: $('win32-group'),
      win32Entry: $('win32-entry'),
      playcoverGroup: $('playcover-group'),
      playcoverAddrEntry: $('playcover-addr-entry'),
      playcoverUuidEntry: $('playcover-uuid-entry'),
      adbGroup: $('adb-group'),
      adbPathEntry: $('adb-path-entry'),
      adbTargetInput: $('adb-target-combo'),
      adbTargetList: $('adb-target-list'),
      btnRefreshAdb: $('btn-refresh-adb'),
      connectionSummary: $('connection-summary'),
      astarDisplayZoneCombo: $('astar-display-zone-combo'),
      astarZoneCombo: $('astar-zone-combo'),
      btnClearAstar: $('btn-clear-astar'),
      btnCopyNavmesh: $('btn-copy-navmesh'),
      loadProgress: $('load-progress'),
      loadProgressBar: $('load-progress-bar'),
      loadProgressLabel: $('load-progress-label'),
      statusLabel: $('status-label'),
      locatorLabel: $('locator-label'),
      importDialog: $('import-dialog'),
      importDialogRows: $('import-dialog-rows'),
      importDialogCancel: $('import-dialog-cancel'),
      importDialogOk: $('import-dialog-ok'),
      propertiesLegend: $('properties-legend'),
      tabEdit: $('tab-edit'),
      tabAstar: $('tab-astar'),
      tabAssert: $('tab-assert'),
      btnClearAssert: $('btn-clear-assert'),
      btnSelectTier: $('btn-select-tier'),
      btnSelectAssertTier: $('btn-select-assert-tier'),
      astarSelectedTierLabel: $('astar-selected-tier-label'),
      assertSelectedTierLabel: $('assert-selected-tier-label'),
      tierPickerDialog: $('tier-picker-dialog'),
      tierPickerBases: $('tier-picker-bases'),
      tierPickerGrid: $('tier-picker-grid'),
      tierPickerCancel: $('tier-picker-cancel'),
      btnFitView: $('btn-fit-view'),
      btnDelPointFloat: $('btn-del-point-float'),
      propertiesEmptyState: $('properties-empty-state'),
      propertiesEditor: $('properties-editor'),
      panelRecording: $('panel-recording'),
      panelProperties: $('panel-properties'),
      panelAstar: $('panel-astar'),
      panelAssert: $('panel-assert'),
      btnAssertLocate: $('btn-assert-locate'),
      btnAstarLocate: $('btn-astar-locate'),
      waypointList: $('waypoint-list'),
      astarCoordX: $('astar-coord-x'),
      astarCoordY: $('astar-coord-y'),
      btnAstarMarkCoord: $('btn-astar-mark-coord'),
      btnAstarImport: $('btn-astar-import'),
      btnAssertImport: $('btn-assert-import'),
    };
  }

  /** Boot: build controllers, wire events, size the canvas, kick the loads. @returns {Promise<void>} */
  async boot() {
    try {
      initFeedback({ status: this.els.statusLabel, locator: this.els.locatorLabel });

      this._populateActionMenu();
      this._resetPropertyControls();
      this._wireEvents();

      this.connection = new ConnectionPanel({
        kindCombo: this.els.kindCombo,
        win32Group: this.els.win32Group,
        win32Entry: this.els.win32Entry,
        playcoverGroup: this.els.playcoverGroup,
        playcoverAddrEntry: this.els.playcoverAddrEntry,
        playcoverUuidEntry: this.els.playcoverUuidEntry,
        adbGroup: this.els.adbGroup,
        adbPathEntry: this.els.adbPathEntry,
        adbTargetInput: this.els.adbTargetInput,
        adbTargetList: this.els.adbTargetList,
        btnRefreshAdb: this.els.btnRefreshAdb,
        summary: this.els.connectionSummary,
      });
      this.recording = new RecordingController({
        btnStart: this.els.btnStart,
        btnStop: this.els.btnStop,
        appEl: this.els.app,
        connection: this.connection,
        onFinished: (rawPoints) => this._onRecordingFinished(rawPoints),
      });
      this.importer = new Importer(
        {
          fileInput: this.els.fileInput,
          btnImport: this.els.btnImport,
          dialog: this.els.importDialog,
          dialogRows: this.els.importDialogRows,
          dialogOk: this.els.importDialogOk,
          dialogCancel: this.els.importDialogCancel,
        },
        {
          loadPoints: (points) => this._importLoadPoints(points),
          applyAssert: (zoneId, target) => this._importApplyAssert(zoneId, target),
        },
      );
      this.importer.init();
      this.connection.init(); // async; settles the connection row on its own

      this._resize();
      this._observeResize();
      this._syncAssertControls();
      this._syncAstarControls();
      this._refreshZoneLabel();
      this._syncModeTabUI();
      this._doRedraw();

      this._loadZoneIds();
      this._pollLoadStatus();
    } catch (err) {
      alert("BOOT ERROR: " + err.message + "\nStack: " + err.stack);
    }
  }

  // ==================================================================================
  //  Load lifecycle (navmesh field + assert zone list)
  // ==================================================================================

  /** Poll `/api/load-status` for the progress bar; build the field once ready. @returns {Promise<void>} */
  async _pollLoadStatus() {
    let status;
    try {
      status = await getLoadStatus();
    } catch {
      window.setTimeout(() => this._pollLoadStatus(), LOAD_POLL_MS * 2);
      return;
    }
    if (status && status.error) {
      setStatus(`寻路数据加载失败: ${status.error}`, '#ef4444');
      this._hideLoadProgress();
      return;
    }
    if (status && status.ready) {
      this._hideLoadProgress();
      if (!this.field) this._loadField();
      return;
    }
    this._showLoadProgress();
    this._updateLoadProgress(status ? status.progress || 0 : 0);
    window.setTimeout(() => this._pollLoadStatus(), LOAD_POLL_MS);
  }

  /** Fetch the zone table and build the {@link NavmeshField}. @returns {Promise<void>} */
  async _loadField() {
    let payload;
    try {
      payload = await getZones();
    } catch (err) {
      setStatus(`navmesh 加载失败: ${err && err.message ? err.message : err}`, '#ef4444');
      return;
    }
    this.field = new NavmeshField(payload && payload.zones ? payload.zones : []);
    this._populateAstarDisplayCombo();
    this._syncAstarControls();
    this._refreshZoneLabel();
    if (this.state.mode === Mode.ASTAR) {
      this._applyDefaultAstarZoneSelection();
      this._onAstarZoneChanged();
    }
  }

  /**
   * Pick a sensible A* display-zone/tier selection: the current edit zone when it
   * maps to a known base, otherwise the first base whose name contains `map01`,
   * otherwise the first base. Updates the combos only — callers follow up with
   * {@link MapNavigatorApp#_onAstarZoneChanged} to load the mesh and sync labels.
   * @returns {void}
   */
  _applyDefaultAstarZoneSelection() {
    if (!this.field) return;
    let matchedBaseName = '';
    let matchedLabel = '';

    const selectedZoneId = this.state.currentZone();
    if (selectedZoneId) {
      const zoneIdNum = parseInt(selectedZoneId, 10);
      if (!Number.isNaN(zoneIdNum)) {
        const base = this.field.zoneById(this.field.geometryZoneId(zoneIdNum));
        if (base && base.name) {
          matchedBaseName = base.name;
          matchedLabel = this.field.zoneLabel(zoneIdNum);
        }
      }
    }

    if (!matchedBaseName) {
      const baseNames = this.field.displayBaseNames();
      matchedBaseName =
        baseNames.find((name) => name.toLowerCase().includes('map01')) ||
        (baseNames.length ? baseNames[0] : '');
    }

    if (matchedBaseName) {
      this.els.astarDisplayZoneCombo.value = matchedBaseName;
      this._refreshAstarZoneChoices();
      if (matchedLabel) {
        this.els.astarZoneCombo.value = matchedLabel;
      }
    } else {
      if (!normalizeZoneId(this.els.astarDisplayZoneCombo.value)) {
        this.els.astarDisplayZoneCombo.value = this._defaultAstarDisplayZone();
      }
      this._refreshAstarZoneChoices();
    }
  }

  /** Fetch the assert-mode zone ids (backend fs scan) and fill the combo. @returns {Promise<void>} */
  async _loadZoneIds() {
    try {
      const payload = await getZoneIds();
      this._availableZoneIds = Array.isArray(payload.zone_ids) ? payload.zone_ids : [];
    } catch {
      this._availableZoneIds = [];
    }
    this._populateAssertZoneCombo();
    this._syncAssertControls();
  }

  /** @returns {void} */
  _showLoadProgress() {
    this.els.loadProgress.hidden = false;
  }

  /** @returns {void} */
  _hideLoadProgress() {
    this.els.loadProgress.hidden = true;
  }

  /**
   * Update the load progress bar and its phase label.
   * @param {number} progress 0..1
   * @returns {void}
   */
  _updateLoadProgress(progress) {
    const pct = Math.round(Math.min(1, Math.max(0, progress)) * 100);
    this.els.loadProgressBar.value = pct;
    let text = '读取文件...';
    if (progress >= 0.7) text = '生成预览图像...';
    else if (progress >= 0.25) text = '构建空间索引...';
    else if (progress >= 0.03) text = '解析 NavMesh 数据...';
    this.els.loadProgressLabel.textContent = text;
  }

  // ==================================================================================
  //  Combos
  // ==================================================================================

  /**
   * Fill the action dropdown and rebuild the legend card from {@link ACTION_COLORS}
   * / {@link ACTION_NAMES} so the legend always matches the overlay palette.
   * @returns {void}
   */
  _populateActionMenu() {
    const menu = this.els.actionMenu;
    menu.textContent = '';
    for (const name of ACTION_MENU_NAMES) {
      const opt = document.createElement('option');
      opt.value = name;
      opt.textContent = name;
      menu.appendChild(opt);
    }

    const legend = this.els.propertiesLegend;
    if (!legend) return;
    legend.textContent = '';

    const title = document.createElement('div');
    title.className = 'legend-title';
    title.textContent = '路点动作图例';
    legend.appendChild(title);

    const grid = document.createElement('div');
    grid.className = 'legend-grid';

    for (const type of ACTION_MENU_TYPES) {
      const item = document.createElement('div');
      item.className = 'legend-item';
      const dot = document.createElement('span');
      dot.className = 'legend-dot';
      dot.style.backgroundColor = ACTION_COLORS[type] || '#3498db';
      const text = document.createElement('span');
      text.textContent = ACTION_NAMES[type] || 'Unknown';
      item.appendChild(dot);
      item.appendChild(text);
      grid.appendChild(item);
    }

    const strictItem = document.createElement('div');
    strictItem.className = 'legend-item';
    const strictDot = document.createElement('span');
    strictDot.className = 'legend-dot-strict';
    const strictDotInner = document.createElement('span');
    strictDotInner.className = 'legend-dot-strict-inner';
    strictDot.appendChild(strictDotInner);
    const strictText = document.createElement('span');
    strictText.textContent = '严格模式';
    strictItem.appendChild(strictDot);
    strictItem.appendChild(strictText);
    grid.appendChild(strictItem);

    legend.appendChild(grid);
  }

  /** Refill the assert-zone combo from the backend zone-id scan, keeping the selection. @returns {void} */
  _populateAssertZoneCombo() {
    const combo = this.els.assertZoneCombo;
    const prev = combo.value;
    combo.textContent = '';
    for (const zoneId of this._availableZoneIds) {
      const opt = document.createElement('option');
      opt.value = zoneId;
      opt.textContent = zoneId;
      combo.appendChild(opt);
    }
    if (prev && this._availableZoneIds.includes(prev)) {
      combo.value = prev;
    } else {
      combo.value = this._defaultAssertZone();
    }

    const zoneId = combo.value;
    if (zoneId) {
      if (this.field) {
        const zoneIdNum = parseInt(zoneId, 10);
        if (!Number.isNaN(zoneIdNum)) {
          this.els.assertSelectedTierLabel.textContent = this.field.zoneLabel(zoneIdNum) || zoneId;
          return;
        }
      }
      this.els.assertSelectedTierLabel.textContent = zoneId;
    } else {
      this.els.assertSelectedTierLabel.textContent = '未选择';
    }
  }

  /** Refill the A* display-zone combo with the field's base names, keeping the selection. @returns {void} */
  _populateAstarDisplayCombo() {
    if (!this.field) return;
    const names = this.field.displayBaseNames();
    const combo = this.els.astarDisplayZoneCombo;
    const prev = combo.value;
    combo.textContent = '';
    for (const name of names) {
      const opt = document.createElement('option');
      opt.value = name;
      opt.textContent = name;
      combo.appendChild(opt);
    }
    if (prev && names.includes(prev)) combo.value = prev;
    else if (names.length) combo.value = names[0];
    this._refreshAstarZoneChoices();
  }

  /** Repopulate the tier dropdown with the current base's tiers (tk `_refresh_astar_zone_choices`). */
  _refreshAstarZoneChoices() {
    if (!this.field) return;
    const choices = this.field.zoneChoicesForBase(this._displayZoneId());
    const combo = this.els.astarZoneCombo;
    const prev = combo.value;
    combo.textContent = '';
    for (const choice of choices) {
      const opt = document.createElement('option');
      opt.value = choice.label;
      opt.textContent = choice.label;
      combo.appendChild(opt);
    }
    if (choices.length && !choices.some((c) => c.label === prev)) combo.value = choices[0].label;
    else combo.value = prev;
  }

  // ==================================================================================
  //  Event wiring
  // ==================================================================================

  /** Attach every DOM event listener (buttons, combos, tabs, canvas, keyboard). @returns {void} */
  _wireEvents() {
    const e = this.els;
    e.btnCopyPath.addEventListener('click', () => this._copyPath());
    e.btnCopyAssert.addEventListener('click', () => this._copyAssert());
    e.btnPrev.addEventListener('click', () => this._prevZone());
    e.btnNext.addEventListener('click', () => this._nextZone());
    e.btnZoomOut.addEventListener('click', () => this._zoomOut());
    e.btnZoomIn.addEventListener('click', () => this._zoomIn());
    e.btnApplyAction.addEventListener('click', () => this._applyAction());
    e.assertZoneCombo.addEventListener('change', () => this._onAssertZoneChanged());
    e.astarDisplayZoneCombo.addEventListener('change', () => this._onAstarDisplayZoneChanged());
    e.astarZoneCombo.addEventListener('change', () => this._onAstarZoneChanged());
    e.btnClearAstar.addEventListener('click', () => this._onClearAstar());
    e.btnCopyNavmesh.addEventListener('click', () => this._copyNavmesh());
    e.btnAssertLocate.addEventListener('click', () => this._onLocateCurrentPosition('assert'));
    e.btnAstarLocate.addEventListener('click', () => this._onLocateCurrentPosition('astar'));
    e.btnAstarMarkCoord.addEventListener('click', () => this._onAstarMarkCoord());
    for (const entry of [e.astarCoordX, e.astarCoordY]) {
      entry.addEventListener('keydown', (ev) => {
        if (ev.key === 'Enter') this._onAstarMarkCoord();
      });
    }
    // One file picker, three entry points (edit / A* / assert) — same two-phase import.
    e.btnAstarImport.addEventListener('click', () => this.importer.openPicker());
    e.btnAssertImport.addEventListener('click', () => this.importer.openPicker());
    e.tabEdit.addEventListener('click', () => this._selectModeTab('edit'));
    e.tabAstar.addEventListener('click', () => this._selectModeTab('astar'));
    e.tabAssert.addEventListener('click', () => this._selectModeTab('assert'));
    e.btnClearAssert.addEventListener('click', () => this._deleteSelectedPoint());
    e.btnSelectTier.addEventListener('click', () => this._openTierPicker());
    e.btnSelectAssertTier.addEventListener('click', () => this._openTierPicker());
    e.tierPickerCancel.addEventListener('click', () => { e.tierPickerDialog.hidden = true; });
    e.btnFitView.addEventListener('click', () => this._fitView());
    e.btnDelPointFloat.addEventListener('click', () => this._deleteSelectedPoint());
    e.toolPan.addEventListener('click', () => this._setActiveTool('pan'));
    e.toolAdd.addEventListener('click', () => this._setActiveTool('add'));
    e.toolSelect.addEventListener('click', () => this._setActiveTool('select'));
    e.toolAstarSingle.addEventListener('click', () => this._setActiveTool('astar-single'));
    e.toolAstarMulti.addEventListener('click', () => this._setActiveTool('astar-multi'));
    e.toolAssertPan.addEventListener('click', () => this._setActiveTool('assert-pan'));
    e.toolAssertEdit.addEventListener('click', () => this._setActiveTool('assert-edit'));

    this._wireWaypointList(e.waypointList);

    const canvas = e.overlayCanvas;
    canvas.addEventListener('mousedown', (ev) => this._onPointerDown(ev));
    window.addEventListener('mousemove', this._pointerMoveBound);
    window.addEventListener('mouseup', this._pointerUpBound);
    canvas.addEventListener('wheel', (ev) => this._onWheel(ev), { passive: false });
    canvas.addEventListener('contextmenu', (ev) => ev.preventDefault());

    document.addEventListener('keydown', (ev) => this._onKeyDown(ev));

    // Holding Alt temporarily swaps to the mode's pan tool; keyup restores the tool.
    this._altSavedTool = null;
    window.addEventListener('keydown', (e) => {
      const target = e.target;
      if (target && (target.tagName === 'INPUT' || target.tagName === 'SELECT' || target.tagName === 'TEXTAREA' || target.isContentEditable)) {
        return;
      }
      if (e.key === 'Alt' && !this._altSavedTool) {
        e.preventDefault();
        this._altSavedTool = this.activeTool;
        if (this.state.mode === Mode.EDIT) {
          this._setActiveTool('pan');
        } else if (this.state.mode === Mode.ASSERT) {
          this._setActiveTool('assert-pan');
        } else if (this.state.mode === Mode.ASTAR) {
          this._setActiveTool('astar-pan');
        }
      }
    });

    window.addEventListener('keyup', (e) => {
      if (e.key === 'Alt' && this._altSavedTool) {
        e.preventDefault();
        const saved = this._altSavedTool;
        this._altSavedTool = null;
        this._setActiveTool(saved);
      }
    });
  }

  /** Track canvas-wrap size changes (ResizeObserver, window resize fallback). @returns {void} */
  _observeResize() {
    if (typeof ResizeObserver === 'function') {
      const ro = new ResizeObserver(() => this._resize());
      ro.observe(this.els.canvasWrap);
    } else {
      window.addEventListener('resize', () => this._resize());
    }
  }

  /** Resize both canvases to the wrap's CSS size at the device pixel ratio. @returns {void} */
  _resize() {
    const wrap = this.els.canvasWrap;
    const cssW = wrap.clientWidth || 800;
    const cssH = wrap.clientHeight || 600;
    const dpr = window.devicePixelRatio || 1;
    this._cssW = cssW;
    this._cssH = cssH;
    this.renderer.resize(cssW, cssH, dpr);
    this.overlay.resize(cssW, cssH, dpr);
    this._paint();
  }

  // ==================================================================================
  //  Frame identity (mirrors app_tk._display_zone_id / _render_background_zone / tiers)
  // ==================================================================================

  /** @returns {string} the zone id string for the current mode's display frame. */
  _displayZoneId() {
    if (this.state.mode === Mode.ASTAR || this.state.mode === Mode.ASSERT) {
      return normalizeZoneId(this.els.astarDisplayZoneCombo.value, this._defaultAstarDisplayZone());
    }
    return this.state.currentZone();
  }

  /** @returns {number} zone id parsed from the tier combo's `"id:name"` value, or NaN. */
  _astarZoneId() {
    const raw = this.els.astarZoneCombo.value || '';
    const head = raw.split(':', 1)[0];
    return parseInt(head, 10);
  }

  /** @returns {?number} zone id of the translated tier backing the canvas, else null. */
  _activeDisplayTierId() {
    if ((this.state.mode !== Mode.ASTAR && this.state.mode !== Mode.ASSERT) || !this.field) return null;
    const zoneId = this._astarZoneId();
    if (Number.isNaN(zoneId)) return null;
    if (!this.field.isTier(zoneId)) return null;
    if (!this.field.isRealTier(zoneId)) return null;
    return zoneId;
  }

  /** @returns {string} zone name for the basemap (tk `_render_background_zone`). */
  _renderBackgroundZone() {
    const tierId = this._activeDisplayTierId();
    if (tierId !== null) {
      const zone = this.field.zoneById(tierId);
      if (zone && zone.name) return zone.name;
    }
    return this._displayZoneId();
  }

  /** @returns {string} the current display base if valid, else the first known base. */
  _defaultAstarDisplayZone() {
    const cur = normalizeZoneId(this.els.astarDisplayZoneCombo.value);
    const names = this.field ? this.field.displayBaseNames() : [];
    if (names.includes(cur)) return cur;
    return names.length ? names[0] : '';
  }

  /** @returns {string} the current edit zone if set, else a `map01` base, else the first scanned zone. */
  _defaultAssertZone() {
    const cur = normalizeZoneId(this.state.currentZone());
    if (cur) return cur;
    const preferred = this._availableZoneIds.find(z => z.toLowerCase().includes('map01'));
    return preferred || this._availableZoneIds[0] || '';
  }

  // ==================================================================================
  //  Rendering
  // ==================================================================================

  /** Full redraw: ensure the right basemap/mesh are loaded, then paint (tk `_do_redraw`). */
  _doRedraw() {
    this._ensureBasemap(this._renderBackgroundZone());
    this._paint();
  }

  /** Draw the current frame — syncs the A* mesh, requests the GL render, draws the overlay. */
  _paint() {
    this._syncAstarMesh();

    const mode = this.state.mode;
    // A* has no real points at all (its marks are preview-only); Assert shows the real
    // points of the displayed map read-only; EDIT shows its own zone segment.
    let overlayPoints = [];
    if (mode === Mode.EDIT) overlayPoints = this._currentSegmentPoints();
    else if (mode === Mode.ASSERT) overlayPoints = this._displayRealPoints();

    if (mode === Mode.EDIT) this._scheduleEditOffMeshProbe();

    let displayAssertLocateHint = null;
    if (mode === Mode.ASSERT && this.assertLocateHint) {
      displayAssertLocateHint = this._baseToDisplay(this.assertLocateHint[0], this.assertLocateHint[1]);
    }
    const displayAstarLocateHints = mode === Mode.ASTAR ? this._astarDisplayHints() : [];

    const vm = {
      mode,
      points: overlayPoints,
      // Selection is local to the EDIT segment.
      selectedIdx: mode === Mode.EDIT ? this.state.selectedIdx : null,
      selectedIndices: mode === Mode.EDIT ? this.state.selectedIndices : new Set(),
      assertTarget: mode === Mode.ASSERT ? this._assertTargetForDisplay() : null,
      astar:
        mode === Mode.ASTAR
          ? {
              previewPoints: this._astarPreviewPoints(),
              segmentBreaks: this.astarRoute ? this.astarRoute.segment_breaks || [] : [],
              hasRoute: !!this.astarRoute,
              goalOnly: this.astarGoal && !this.astarRoute ? this.astarGoal : null,
              waypoints: this.astarPoints,
              blindWalks: this._blindWalksForDisplay(),
            }
          : null,
      selectionRect: this.selectionRect,
      assertLocateHint: displayAssertLocateHint,
      astarLocateHints: displayAstarLocateHints,
      offMeshMarks: this._offMeshForMode(mode),
    };
    this.renderer.requestRender(this.camera);
    this.overlay.render(this.camera, vm);

    if (mode === Mode.ASTAR && (this.astarRoute || this.astarStart || this.astarGoal)) {
      this._requestAnimationLoop();
    }
  }

  /** @returns {Array<Object>} the current zone segment's point objects. */
  _currentSegmentPoints() {
    return this.state.zonePointGlobalIndices().map((idx) => this.state.points[idx]);
  }

  /**
   * Resolve a point/zone string (a zone *name* like `map01_2f`, or a numeric id) to a
   * numeric zone id. `NaN` when unknown.
   * @param {unknown} zoneStr
   * @returns {number}
   */
  _resolveZoneId(zoneStr) {
    const zoneId = normalizeZoneId(typeof zoneStr === 'string' ? zoneStr : '');
    if (!zoneId || !this.field) return NaN;
    const zone = this.field.zoneByName(zoneId);
    return zone ? zone.zone_id : parseInt(zoneId, 10);
  }

  /**
   * base px → current display frame (tier px when a translated tier backs the canvas).
   * @param {number} bx @param {number} by
   * @returns {[number, number]}
   */
  _baseToDisplay(bx, by) {
    const tierId = this._activeDisplayTierId();
    if (tierId === null || !this.field) return [bx, by];
    return this.field.baseToTier(tierId, bx, by);
  }

  /** A* preview markers projected base → display frame. @returns {Array<{x:number,y:number,label:string}>} */
  _astarDisplayHints() {
    return this.astarLocateHints.map((hint) => {
      const [x, y] = this._baseToDisplay(hint.x, hint.y);
      return { x, y, label: hint.label };
    });
  }

  /**
   * Display-frame bbox of every A* preview marker, padded so a lone marker still gets
   * a window around it instead of a zero-size box.
   * @returns {?number[]} `[minX, minY, maxX, maxY]`, or null when there are no markers
   */
  _astarHintsBbox() {
    const hints = this._astarDisplayHints();
    if (!hints.length) return null;
    const xs = hints.map((hint) => hint.x);
    const ys = hints.map((hint) => hint.y);
    const pad = ASTAR_HINT_FIT_PADDING;
    return [Math.min(...xs) - pad, Math.min(...ys) - pad, Math.max(...xs) + pad, Math.max(...ys) + pad];
  }

  /** Frame every A* preview marker (locate fix / typed coord / imported JSON). @returns {void} */
  _focusAstarHints() {
    const bbox = this._astarHintsBbox();
    if (!bbox) return;
    this.camera.fitView(bbox, this._cssW, this._cssH, 60, LEFT_PANEL_FIT_OFFSET);
    this._paint();
  }

  /**
   * The real route points belonging to the map on screen, projected into its display
   * frame. A point's coords live in its *own* zone's frame (possibly a tier), so each
   * goes `own zone → base → display frame`; points of another map are dropped.
   * @returns {Array<Object>} copies of the points with display-frame `x`/`y`
   */
  _displayRealPoints() {
    if (!this.field || !this.state.points.length) return [];
    const displayZoneId = this._astarZoneId();
    if (Number.isNaN(displayZoneId)) return [];
    const displayGeomId = this.field.geometryZoneId(displayZoneId);

    const out = [];
    for (const point of this.state.points) {
      const pointZoneId = this._resolveZoneId(point.zone);
      if (Number.isNaN(pointZoneId)) continue;
      if (this.field.geometryZoneId(pointZoneId) !== displayGeomId) continue;
      const [bx, by] = this._pointToBase(pointZoneId, point.x, point.y);
      const [dx, dy] = this._baseToDisplay(bx, by);
      out.push({ ...point, x: dx, y: dy });
    }
    return out;
  }

  /**
   * A point's own-zone px → base px.
   * @param {number} zoneId @param {number} x @param {number} y
   * @returns {[number, number]}
   */
  _pointToBase(zoneId, x, y) {
    if (!this.field || Number.isNaN(zoneId) || !this.field.isTier(zoneId)) return [x, y];
    return this.field.tierToBase(zoneId, x, y);
  }

  /** A* route points expressed in the display frame (base→tier when a real tier shows). */
  _routeDisplayPoints() {
    if (!this.astarRoute || !this.astarRoute.points || !this.astarRoute.points.length) return [];
    const tierId = this._activeDisplayTierId();
    if (tierId === null) return this.astarRoute.points;
    return this.astarRoute.points.map((p) => this.field.baseToTier(tierId, p[0], p[1]));
  }

  /** Preview points for the overlay (route, else the lone start, else none). */
  _astarPreviewPoints() {
    if (this.astarRoute) return this._routeDisplayPoints();
    if (this.astarStart) return [this.astarStart];
    return [];
  }

  /** The runtime's blind-walk lines, base px → display frame. @returns {Array<Object>} */
  _blindWalksForDisplay() {
    return this.astarBlindWalks.map((w) => ({
      ...w,
      off: this._baseToDisplay(w.off[0], w.off[1]),
      mesh: this._baseToDisplay(w.mesh[0], w.mesh[1]),
    }));
  }

  /** Off-mesh badges, base px → display frame. @returns {Array<Object>} */
  _offMeshMarksForDisplay() {
    return this.offMeshMarks.map((m) => ({
      ...m,
      point: this._baseToDisplay(m.point[0], m.point[1]),
      nearest: m.nearest ? this._baseToDisplay(m.nearest[0], m.nearest[1]) : null,
    }));
  }

  /**
   * Off-mesh badges for the active mode. EDIT badges its NAVMESH waypoints (already in the
   * edit zone's own frame — no projection); A* badges its clicked points. ASSERT shows the
   * real route read-only and owns no badges of its own.
   * @param {string} mode @returns {Array<Object>}
   */
  _offMeshForMode(mode) {
    if (mode === Mode.EDIT) return this.editOffMeshMarks;
    if (mode === Mode.ASTAR) return this._offMeshMarksForDisplay();
    return [];
  }

  /**
   * The NAVMESH waypoints of the current edit segment, with their base-px coords.
   *
   * Only NAVMESH: those are the ones the runtime pathfinds to, so landing off the mesh
   * changes what it does. A RUN waypoint off the mesh is not a defect — the runtime walks
   * the recorded line as authored, mesh or no mesh — so badging those would be pure noise.
   * @returns {Array<{idx:number, base:number[], own:number[]}>}
   */
  _editNavmeshPoints() {
    if (!this.field) return [];
    const out = [];
    const points = this._currentSegmentPoints();
    for (let i = 0; i < points.length; i += 1) {
      const point = points[i];
      if (!getPointActions(point).includes(ActionType.NAVMESH)) continue;
      const zoneId = this._resolveZoneId(point.zone);
      if (Number.isNaN(zoneId)) continue;
      out.push({ idx: i, base: this._pointToBase(zoneId, point.x, point.y), own: [point.x, point.y], zoneId });
    }
    return out;
  }

  /**
   * Re-probe the edit segment's NAVMESH waypoints when they change. Called from every
   * paint, so it short-circuits on an unchanged signature (a pan or zoom must not hit the
   * backend) and debounces the ones that do change (dragging a node fires per mousemove).
   * @returns {void}
   */
  _scheduleEditOffMeshProbe() {
    const targets = this._editNavmeshPoints();
    const sig = targets.map((t) => `${t.zoneId}:${t.own[0]},${t.own[1]}`).join('|');
    if (sig === this._offMeshSig) return;
    this._offMeshSig = sig;

    if (!targets.length) {
      this.editOffMeshMarks = [];
      return;
    }
    clearTimeout(this._offMeshTimer);
    this._offMeshTimer = setTimeout(() => this._probeEditNavmeshPoints(targets), 150);
  }

  /**
   * Badge whichever NAVMESH waypoints sit off the walkable mesh.
   *
   * The distance shown is the straight line to the nearest mesh — *not* the blind walk the
   * runtime performs, which depends on where the character comes from (it probes back along
   * the goal→start line). The badge says "最近网格", never "盲走"; A* mode is where the
   * runtime's real number lives.
   * @param {Array<Object>} targets from {@link _editNavmeshPoints}
   * @returns {Promise<void>}
   */
  async _probeEditNavmeshPoints(targets) {
    const token = ++this._probeToken;
    const zoneId = this._resolveZoneId(this.state.currentZone());
    if (!this.field || Number.isNaN(zoneId) || !targets.length) return;

    let res;
    try {
      res = await postOffMeshProbe({
        zone_id: this.field.geometryZoneId(zoneId),
        points: targets.map((t) => t.base),
        snap_radius: ASTAR_PREVIEW_SNAP_RADIUS,
        floor_y: this.field.floorYFor(zoneId),
      });
    } catch {
      return; // 探针只是提示, 失败就静默放过, 别打断编辑
    }
    if (token !== this._probeToken || !res || !res.ok) return;

    const marks = [];
    (res.results || []).forEach((probe, i) => {
      if (!probe || !targets[i]) return;
      const target = targets[i];
      marks.push({
        point: target.own,
        // nearest 是基准图坐标, 点在自己的分层坐标系里 —— 换算回来才画得到一起。
        nearest: probe.nearest ? this._baseToOwn(target.zoneId, probe.nearest) : null,
        distance: probe.distance,
        budget: null, // 路点是终点, 真实盲走距离跟起点有关, 这里不下上限判断
        exact: false,
        label: `#${target.idx}`, // 跟节点圆圈上的编号一致 (0 基)
      });
    });
    this.editOffMeshMarks = marks;
    this._paint();
  }

  /** base px → a point's own zone frame (inverse of {@link _pointToBase}). */
  _baseToOwn(zoneId, base) {
    if (!this.field || Number.isNaN(zoneId) || !this.field.isTier(zoneId)) return base;
    return this.field.baseToTier(zoneId, base[0], base[1]);
  }

  /** Sorted-rect `[x,y,w,h]` in display-frame world for the assert overlay (unrounded). */
  _assertTargetForDisplay() {
    if (!this.assertRectWorld) return null;
    const [x0, y0, x1, y1] = this.assertRectWorld;
    const left = Math.min(x0, x1);
    const top = Math.min(y0, y1);
    return [left, top, Math.abs(x1 - x0), Math.abs(y1 - y0)];
  }

  /** Rounded assert target for status/export (tk `_current_assert_target`). @returns {?number[]} */
  _currentAssertTarget() {
    const display = this._assertTargetForDisplay();
    if (!display) return null;
    return [bankerRound2(display[0]), bankerRound2(display[1]), bankerRound2(display[2]), bankerRound2(display[3])];
  }

  // --- basemap texture (async <img>) ---

  /**
   * Make `zoneName`'s basemap the active texture (async). Idempotent for the current
   * zone. On settle, either performs a pending fit or repaints.
   * @param {string} zoneName
   * @returns {void}
   */
  _ensureBasemap(zoneName) {
    if (zoneName === this._bgZone) return; // in-flight load settles on its own; caller paints
    this._bgZone = zoneName;
    const token = (this._basemapToken += 1);
    if (!zoneName) {
      this._basemapLoading = false;
      this._basemapDims = null;
      this.renderer.setBasemapVisible(false);
      this._afterBasemapSettled(token);
      return;
    }
    this._basemapLoading = true;
    const img = new Image();
    img.onload = () => {
      if (token !== this._basemapToken) return;
      this._basemapLoading = false;
      this.renderer.setBasemap(img, { width: img.naturalWidth, height: img.naturalHeight });
      this.renderer.setBasemapVisible(true);
      this._basemapDims = { width: img.naturalWidth, height: img.naturalHeight };
      this._afterBasemapSettled(token);
    };
    img.onerror = () => {
      if (token !== this._basemapToken) return;
      this._basemapLoading = false;
      this._basemapDims = null;
      this.renderer.setBasemapVisible(false);
      this._afterBasemapSettled(token);
    };
    img.src = basemapByZoneUrl(zoneName);
  }

  /**
   * Basemap load/error settled: run the deferred fit-view or just repaint.
   * @param {number} token load-generation token; stale loads are ignored
   * @returns {void}
   */
  _afterBasemapSettled(token) {
    if (token !== this._basemapToken) return;
    if (this._fitPending) {
      this._fitPending = false;
      this._fitNow();
    } else {
      this._paint();
    }
  }

  // --- A* mesh (NMSH over GL) ---

  /** Upload / hide the A* mesh for the current display zone (keyed, so cheap per paint). */
  _syncAstarMesh() {
    if (this.state.mode !== Mode.ASTAR || !this.field) {
      this._meshKey = null;
      this.renderer.setMeshVisible(false);
      this.renderer.setDotsVisible(false);
      return;
    }
    const displayZoneId = this._astarZoneId();
    if (Number.isNaN(displayZoneId)) {
      this._meshKey = null;
      this.renderer.setMeshVisible(false);
      return;
    }
    const geomId = this.field.geometryZoneId(displayZoneId);
    const tierId = this._activeDisplayTierId();
    const key = `${geomId}:${tierId}`;
    if (key === this._meshKey) return;
    this._meshKey = key;
    this.renderer.setMeshVisible(false);
    const token = (this._meshToken += 1);
    const dims = this.field.dims(displayZoneId);
    getMesh(geomId)
      .then((buffer) => {
        if (token !== this._meshToken) return;
        if (!buffer) {
          this.renderer.setMeshVisible(false);
          this.renderer.setDotsVisible(false);
          return;
        }
        const buf = tierId !== null ? this.field.remapNmshToTier(buffer, tierId) : buffer;
        this.renderer.setMesh(buf, { width: dims.width, height: dims.height });
        this.renderer.setMeshVisible(true);
        this.renderer.setDotsVisible(false);
        this.renderer.requestRender(this.camera);
      })
      .catch(() => {
        if (token === this._meshToken) this.renderer.setMeshVisible(false);
      });
  }

  // --- fit view ---

  /** Fit the current frame to the canvas (tk `fit_view`), deferring if the basemap is loading. */
  _fitView() {
    const renderZone = this._renderBackgroundZone();
    if (renderZone === this._bgZone && !this._basemapLoading) {
      this._fitNow();
      return;
    }
    this._fitPending = true;
    this._ensureBasemap(renderZone);
  }

  /** Compute the current mode's frame bbox and fit the camera to it. @returns {void} */
  _fitNow() {
    this._fitPending = false;
    const mode = this.state.mode;
    const points = mode === Mode.EDIT ? this._currentSegmentPoints() : [];

    let minX = 0;
    let maxX = 100;
    let minY = 0;
    let maxY = 100;
    const dims = this._basemapDims;
    if (dims) {
      maxX = dims.width;
      maxY = dims.height;
    }

    const assertTarget = this._currentAssertTarget();
    const routePoints = this._routeDisplayPoints();
    if (mode === Mode.ASSERT && assertTarget) {
      minX = assertTarget[0];
      maxX = assertTarget[0] + assertTarget[2];
      minY = assertTarget[1];
      maxY = assertTarget[1] + assertTarget[3];
    } else if (mode === Mode.ASSERT && this.assertLocateHint) {
      const pt = this._baseToDisplay(this.assertLocateHint[0], this.assertLocateHint[1]);
      minX = pt[0] - 200;
      maxX = pt[0] + 200;
      minY = pt[1] - 200;
      maxY = pt[1] + 200;
    } else if (mode === Mode.ASTAR && routePoints.length) {
      const xs = routePoints.map((p) => p[0]);
      const ys = routePoints.map((p) => p[1]);
      minX = Math.min(...xs);
      maxX = Math.max(...xs);
      minY = Math.min(...ys);
      maxY = Math.max(...ys);
    } else if (mode === Mode.ASTAR && this.astarLocateHints.length) {
      [minX, minY, maxX, maxY] = this._astarHintsBbox();
    } else if (mode === Mode.ASTAR && this.field && !Number.isNaN(this._astarZoneId()) && !dims) {
      const bounds = this.field.bounds(this._astarZoneId());
      if (bounds) [minX, minY, maxX, maxY] = bounds;
    } else if (points.length) {
      const xs = points.map((p) => p.x);
      const ys = points.map((p) => p.y);
      minX = Math.min(...xs);
      maxX = Math.max(...xs);
      minY = Math.min(...ys);
      maxY = Math.max(...ys);
    }

    this.camera.fitView([minX, minY, maxX, maxY], this._cssW, this._cssH, 60, LEFT_PANEL_FIT_OFFSET);
    this._paint();
  }

  /**
   * Frame a set of display-frame points. Used right after an import: in A* and Assert
   * `_fitNow` frames the whole basemap, which would leave the imported points a few
   * pixels wide. Falls back to the normal fit when there is nothing to frame.
   * @param {Array<{x:number, y:number}>} points
   * @returns {void}
   */
  _fitDisplayPoints(points) {
    if (!points.length) {
      this._fitView();
      return;
    }
    const xs = points.map((p) => p.x);
    const ys = points.map((p) => p.y);
    this.camera.fitView(
      [Math.min(...xs), Math.min(...ys), Math.max(...xs), Math.max(...ys)],
      this._cssW,
      this._cssH,
      60,
      LEFT_PANEL_FIT_OFFSET,
    );
    this._paint();
  }

  // ==================================================================================
  //  Pointer state machine (tk on_click / on_drag / on_release + right-button pan)
  // ==================================================================================

  /**
   * @param {MouseEvent} e
   * @returns {[number, number]} pointer position in canvas CSS px
   */
  _evtXY(e) {
    const rect = this.els.overlayCanvas.getBoundingClientRect();
    return [e.clientX - rect.left, e.clientY - rect.top];
  }

  /**
   * @param {number} sx @param {number} sy @param {number} cx @param {number} cy
   * @returns {boolean} true once the pointer moved past the drag-activation threshold
   */
  _movedExceeded(sx, sy, cx, cy) {
    return Math.abs(cx - sx) > DRAG_ACTIVATION_DISTANCE || Math.abs(cy - sy) > DRAG_ACTIVATION_DISTANCE;
  }

  /** @returns {(wx: number, wy: number) => [number, number]} bound world→canvas transform */
  _worldToCanvasFn() {
    return (wx, wy) => this.camera.worldToCanvas(wx, wy);
  }

  /**
   * Pointer-down entry of the interaction state machine. Right button always pans;
   * left button dispatches on mode + active tool (pan / A* click candidate / assert
   * rect / box select / insert candidate / node drag candidate).
   * @param {MouseEvent} e
   * @returns {void}
   */
  _onPointerDown(e) {
    if (e.button === 2) {
      e.preventDefault();
      const [x, y] = this._evtXY(e);
      this.isPanning = true;
      this.dragStartX = x;
      this.dragStartY = y;
      return;
    }
    if (e.button !== 0) return;
    const [x, y] = this._evtXY(e);

    if (
      this.activeTool === 'pan' ||
      this.activeTool === 'assert-pan' ||
      this.activeTool === 'astar-pan'
    ) {
      this.isPanning = true;
      this.dragStartX = x;
      this.dragStartY = y;
      this.els.overlayCanvas.style.cursor = 'grabbing';
      return;
    }

    if (this.state.mode === Mode.ASTAR) {
      // mouse-down is a click candidate; it becomes a pan once dragged past the threshold
      this.isDragging = false;
      this.isPanCandidate = true;
      this.isPanning = false;
      this.isBoxSelecting = false;
      this.isAssertSelecting = false;
      this.pointerDownX = x;
      this.pointerDownY = y;
      return;
    }

    if (this.state.mode === Mode.ASSERT) {
      if (this.activeTool === 'assert-edit') {
        const zoneId = this._displayZoneId();
        if (!zoneId) {
          setStatus('请先在 Assert 模式下选择地图。', '#ef4444');
          return;
        }
        this.isAssertSelecting = true;
        this.assertLocateHint = null;
        this.isDragging = false;
        this.isPanCandidate = false;
        this.isPanning = false;
        this.isBoxSelecting = false;
        const [wx, wy] = this.camera.canvasToWorld(x, y);
        this.assertStartWorldX = wx;
        this.assertStartWorldY = wy;
        this.assertRectWorld = [wx, wy, wx, wy];
        this._paint();
        return;
      }
    }

    if (this.state.mode === Mode.EDIT && (this.activeTool === 'add' || this.activeTool === 'select')) {
      const isSelectTool = this.activeTool === 'select';
      const hitIdx = this.state.hitTest(this._worldToCanvasFn(), x, y);
      if (hitIdx === null) {
        if (isSelectTool || e.ctrlKey || e.metaKey) {
          this.isBoxSelecting = true;
          this.boxStartX = x;
          this.boxStartY = y;
          this.isPanning = false;
          this.isDragging = false;
          this.isDragCandidate = false;
        } else {
          // insert happens on pointer-up, and only if the drag threshold was never exceeded
          this.isPanCandidate = true;
          this.isPanning = false;
          this.isDragging = false;
          this.isDragCandidate = false;
          this.pointerDownX = x;
          this.pointerDownY = y;
        }
      } else {
        this.isPanCandidate = false;
        this.isPanning = false;
        this.isDragging = false;
        this.isDragCandidate = true;
        this.dragHitIdx = hitIdx;
        this.pointerDownX = x;
        this.pointerDownY = y;
      }
    }
  }

  /**
   * Pointer-move: drive whichever gesture is active (pan, candidate promotion,
   * assert rect, box select, node drag).
   * @param {MouseEvent} e
   * @returns {void}
   */
  _onPointerMove(e) {
    const [x, y] = this._evtXY(e);

    if (this.isPanning) {
      this.camera.panBy(x - this.dragStartX, y - this.dragStartY);
      this.dragStartX = x;
      this.dragStartY = y;
      this._paint();
      return;
    }
    if (this.isPanCandidate) {
      if (!this._movedExceeded(this.pointerDownX, this.pointerDownY, x, y)) return;
      this.isPanCandidate = false;
      this.isPanning = true;
      this.dragStartX = x;
      this.dragStartY = y;
      return;
    }
    if (this.state.mode === Mode.ASTAR) return;

    if (this.state.mode === Mode.ASSERT) {
      if (!this.isAssertSelecting) return;
      const [wx, wy] = this.camera.canvasToWorld(x, y);
      this.assertRectWorld = [this.assertStartWorldX, this.assertStartWorldY, wx, wy];
      this._paint();
      return;
    }

    if (this.isBoxSelecting) {
      this.selectionRect = { x0: this.boxStartX, y0: this.boxStartY, x1: x, y1: y };
      this._paint();
      return;
    }

    if (this.isDragCandidate) {
      if (this._movedExceeded(this.pointerDownX, this.pointerDownY, x, y)) {
        this.isDragCandidate = false;
        this.isDragging = true;
        this.state.snapshot(); // push undo on start of real drag
        this.state.setSelection([this.dragHitIdx], this.dragHitIdx);
        this._syncActionControls();
      }
    }

    if (this.isDragging) {
      const [wx, wy] = this.camera.canvasToWorld(x, y);
      if (this.state.editMoveSelected(wx, wy, false)) this._paint();
    }
  }

  /**
   * Pointer-up: commit the active gesture — end pan, resolve an A* click, close
   * the assert rect, apply box selection, toggle-select on click, or insert a point.
   * @param {MouseEvent} e
   * @returns {void}
   */
  _onPointerUp(e) {
    const [x, y] = this._evtXY(e);

    if (this.isPanning) {
      this.isPanning = false;
      this._setActiveTool(this.activeTool);
      this._paint();
      return;
    }

    if (this.state.mode === Mode.ASTAR) {
      if (this.isPanCandidate) {
        this.isPanCandidate = false;
        if (this.activeTool !== 'astar-pan') {
          this._handleAstarClick(x, y);
        }
      }
      return;
    }

    if (this.state.mode === Mode.ASSERT) {
      if (!this.isAssertSelecting) return;
      const [wx, wy] = this.camera.canvasToWorld(x, y);
      this.assertRectWorld = [this.assertStartWorldX, this.assertStartWorldY, wx, wy];
      this.isAssertSelecting = false;
      const target = this._currentAssertTarget();
      if (target) {
        setStatus(
          `Assert 区域已更新: zone=${this._displayZoneId()} target=[${target[0].toFixed(1)}, ${target[1].toFixed(
            1,
          )}, ${target[2].toFixed(1)}, ${target[3].toFixed(1)}]`,
          '#10b981',
        );
      }
      this._paint();
      return;
    }

    if (this.isDragCandidate) {
      this.isDragCandidate = false;
      this.state.setSelection([this.dragHitIdx], this.dragHitIdx);
      this._syncActionControls();
      this._paint();
      return;
    }

    if (this.isBoxSelecting) {
      if (Math.abs(x - this.boxStartX) <= 4 && Math.abs(y - this.boxStartY) <= 4) {
        const hitIdx = this.state.hitTest(this._worldToCanvasFn(), x, y);
        if (hitIdx !== null) {
          const selected = new Set(this.state.selectedIndices);
          if (selected.has(hitIdx)) selected.delete(hitIdx);
          else selected.add(hitIdx);
          this.state.setSelection([...selected], hitIdx);
        }
      } else {
        const indices = this.state.collectIndicesInRect(this._worldToCanvasFn(), this.boxStartX, this.boxStartY, x, y);
        this.state.setSelection(indices);
      }
      this._syncActionControls();
      this.selectionRect = null;
      this.isBoxSelecting = false;
      this._paint();
      return;
    }

    if (this.isPanCandidate) {
      this.isPanCandidate = false;
      if (this.state.mode === Mode.EDIT && this.activeTool !== 'add') {
        this.state.clearSelection();
        this._syncActionControls();
        this._paint();
        return;
      }
      this.state.clearSelection();
      const [wx, wy] = this.camera.canvasToWorld(x, y);
      this.state.editInsertPoint(this._actionName(), this._strict(), wx, wy);
      this._resetPropertyControls();
      this._afterStructureChanged();
      return;
    }

    this.isDragging = false;
  }

  /**
   * Wheel input: plain wheel pans (trackpad-style), Ctrl/Cmd+wheel zooms at the cursor.
   * @param {WheelEvent} e
   * @returns {void}
   */
  _onWheel(e) {
    e.preventDefault();
    if (e.ctrlKey || e.metaKey) {
      const [x, y] = this._evtXY(e);
      const factor = Math.max(0.5, Math.min(2.0, Math.exp(-e.deltaY * 0.012)));
      this.camera.zoomAt(x, y, factor);
    } else {
      this.camera.panBy(-e.deltaX * 1.3, -e.deltaY * 1.3);
    }
    this._paint();
  }

  // ==================================================================================
  //  A* preview
  // ==================================================================================

  /**
   * A* canvas click: astar-single restarts a 2-point start/goal pair; astar-multi
   * keeps appending waypoints. Recomputes the preview once ≥2 points exist.
   * @param {number} x canvas X (CSS px)
   * @param {number} y canvas Y (CSS px)
   * @returns {void}
   */
  _handleAstarClick(x, y) {
    if (!this.field) {
      setStatus('navmesh 尚未就绪。', '#ef4444');
      return;
    }
    const [wx, wy] = this.camera.canvasToWorld(x, y);

    if (this.activeTool === 'astar-single') {
      if (this.astarPoints.length === 0 || this.astarPoints.length >= 2) {
        this.astarPoints = [[wx, wy]];
        this.astarRoute = null;
        setStatus(`A* 起点: [${wx.toFixed(1)}, ${wy.toFixed(1)}]，再点击终点。`, '#3b82f6');
        // 探针/路线的同步开头会清掉上一轮的离网徽标, 所以先调它们、再 _paint(),
        // 免得这一帧还画着上一条路线的警示环。
        this._probeLoneAstarPoint();
        this._paint();
        return;
      }
      this.astarPoints.push([wx, wy]);
      setStatus('正在计算 A* 路径...', '#eab308');
      this._calculateAstarPreview();
      this._paint();
    } else {
      this.astarPoints.push([wx, wy]);
      if (this.astarPoints.length < 2) {
        setStatus('已设置 A* 起点，请继续点击后续路点以串联多段路径。', '#3b82f6');
        this._probeLoneAstarPoint();
        this._paint();
      } else {
        setStatus(`正在计算第 ${this.astarPoints.length - 1} 段 A* 路径...`, '#eab308');
        this._calculateAstarPreview();
        this._paint();
      }
    }
  }

  /**
   * Badge the lone A* point when it sits off the walkable mesh. With ≥2 points the route
   * owns the badges (it carries the runtime's real blind-walk numbers); this covers the
   * moment before any route exists, when a point clicked off the mesh looks no different
   * from one on it. Geometry only — see {@link postOffMeshProbe}.
   * @returns {Promise<void>}
   */
  async _probeLoneAstarPoint() {
    this._resetOffMeshOverlays();
    const token = this._probeToken;
    if (!this.field || this.astarPoints.length !== 1) return;

    const displayZoneId = this._astarZoneId();
    if (Number.isNaN(displayZoneId)) return;
    const tierId = this._activeDisplayTierId();
    const [px, py] = this.astarPoints[0];
    const base = tierId !== null ? this.field.tierToBase(tierId, px, py) : [px, py];

    let res;
    try {
      res = await postOffMeshProbe({
        zone_id: this.field.geometryZoneId(displayZoneId),
        points: [base],
        snap_radius: ASTAR_PREVIEW_SNAP_RADIUS,
        floor_y: this.field.floorYFor(displayZoneId),
      });
    } catch {
      return; // 探针只是提示, 失败就静默放过, 别打断编辑
    }
    if (token !== this._probeToken) return; // 期间点变了, 丢弃这次结果

    const probe = res && res.ok && res.results ? res.results[0] : null;
    if (!probe) return;
    this._addOffMeshMark(base, 'S', { ...probe, exact: false });
    const d = probe.distance === null ? '附近无网格' : `最近网格 ${probe.distance.toFixed(1)} 格`;
    setStatus(`⚠ 该点不在可走网格上（${d}）——运行时会直线盲走过去，请自行确认这条直线走得通。`, '#f59e0b');
    this._paint();
  }

  /**
   * Route every consecutive A* waypoint pair through `/api/route` (in base px on
   * the parent geometry zone) and merge the legs into one preview route. Adjacent
   * legs share their boundary point, so each leg after the first drops its first
   * point; per-leg `segment_breaks` are re-offset into the merged list and every
   * leg boundary is appended as a break.
   * @returns {Promise<void>}
   */
  async _calculateAstarPreview() {
    // 先同步清干净(并作废在途探针) —— 调用方随后 _paint() 时就不会再画着上一次的残留徽标,
    // 而点起点时发出的孤点探针也不会在路线算完之后才回来、盖掉路线自己的那行提示。
    this._resetOffMeshOverlays();
    if (!this.field || this.astarPoints.length < 2) return;
    const displayZoneId = this._astarZoneId();
    const geomId = this.field.geometryZoneId(displayZoneId);
    const tierId = this._activeDisplayTierId();
    const floorY = this.field.floorYFor(displayZoneId);

    const basePoints = this.astarPoints.map((p) =>
      tierId !== null ? this.field.tierToBase(tierId, p[0], p[1]) : p,
    );

    try {
      const combinedPoints = [];
      const combinedBreaks = [];
      let totalCost = 0;

      for (let i = 0; i < basePoints.length - 1; i++) {
        const legStart = basePoints[i];
        const legGoal = basePoints[i + 1];
        const res = await postRoute({
          zone_id: geomId,
          start: legStart,
          goal: legGoal,
          snap_radius: ASTAR_PREVIEW_SNAP_RADIUS,
          floor_y: floorY,
        });

        if (!res || !res.ok) {
          this._markFailedLeg(res && res.off_mesh, legStart, legGoal, i, basePoints.length);
          throw new Error(res?.error || `第 ${i + 1} 段 A* 寻路失败`);
        }

        // The runtime blind-walks a straight line onto the mesh (off-mesh start) or off it
        // (off-mesh goal). These are its real numbers, so draw the actual lines it walks.
        if (res.blind_start) {
          const { entry, distance, reason } = res.blind_start;
          this.astarBlindWalks.push({ off: legStart, mesh: entry, distance, kind: '起点' });
          this._addOffMeshMark(legStart, this._astarBadgeLabel(i, basePoints.length), {
            nearest: entry,
            distance,
            reason,
            exact: true,
          });
        }
        if (res.blind_target) {
          const { reached, gap, reason } = res.blind_target;
          this.astarBlindWalks.push({ off: legGoal, mesh: reached, distance: gap, kind: '终点' });
          this._addOffMeshMark(legGoal, this._astarBadgeLabel(i + 1, basePoints.length), {
            nearest: reached,
            distance: gap,
            reason,
            exact: true,
          });
        }

        const segmentPts = res.points || [];
        const dropped = combinedPoints.length > 0 && segmentPts.length > 0 ? 1 : 0;
        const offset = combinedPoints.length - dropped;
        for (const b of res.segment_breaks || []) {
          if (b > 0 && b < segmentPts.length) combinedBreaks.push(offset + b);
        }
        combinedPoints.push(...segmentPts.slice(dropped));
        totalCost += res.cost || 0;
        combinedBreaks.push(combinedPoints.length - 1);
      }

      this.astarRoute = {
        points: combinedPoints,
        segment_breaks: combinedBreaks,
        cost: totalCost,
      };
      const summary = `A* 路线已生成：共 ${this.astarPoints.length} 个关键点，包含 ${this.astarRoute.points.length} 个坐标。`;
      if (this.astarBlindWalks.length > 0) {
        const worst = Math.max(...this.astarBlindWalks.map((b) => b.distance)).toFixed(1);
        const kinds = [...new Set(this.astarBlindWalks.map((b) => b.kind))].join('/');
        // "接不上网格" 两种成因都覆盖(点离网 / 脚下网格不连通); 具体是哪种, 徽标上写着。
        setStatus(
          `${summary} ⚠ ${this.astarBlindWalks.length} 处盲走：${kinds}接不上网格，运行时会沿橙色虚线直着走过去（最长 ${worst} 格）——请自行确认这条直线走得通。`,
          '#f59e0b',
        );
      } else {
        setStatus(summary, '#10b981');
      }
      this._paint();
    } catch (err) {
      this.astarRoute = null;
      this.astarBlindWalks = [];
      const msg = err && err.message ? err.message : err;
      setStatus(`A* 寻路失败: ${msg}${this._offMeshHint()}`, '#ef4444');
      this._paint();
    }
  }

  /** Badge letter for A* waypoint `i` of `n` (matches the overlay's S / 2..n-1 / G). */
  _astarBadgeLabel(i, n) {
    if (i === 0) return 'S';
    if (i === n - 1) return 'G';
    return String(i + 1);
  }

  /**
   * Record an off-mesh point to badge. A waypoint shared by two legs is reported by both
   * (once as a goal, once as a start), so keep only the first badge per point — the two
   * blind-walk *lines* are both real and stay.
   * @param {number[]} point base px
   * @param {string} label badge letter the point already wears (S / 2..n-1 / G)
   * @param {{nearest:?number[], distance:?number, budget:?number, reason:?string,
   *   exact:boolean}} info `reason` is 'off_mesh' (the point is outside the mesh) or
   *   'disconnected' (it stands on mesh the destination can't be reached from)
   * @returns {void}
   */
  _addOffMeshMark(point, label, info) {
    const key = (p) => `${p[0].toFixed(2)},${p[1].toFixed(2)}`;
    if (this.offMeshMarks.some((m) => key(m.point) === key(point))) return;
    this.offMeshMarks.push({
      point,
      label,
      nearest: info.nearest || null,
      distance: info.distance === undefined ? null : info.distance,
      budget: info.budget || null,
      reason: info.reason || 'off_mesh',
      exact: !!info.exact,
    });
  }

  /**
   * Badge whichever endpoints of a failed leg are off the mesh, using the backend's probe.
   * Nothing badged means both endpoints sit on the mesh and the leg failed for another
   * reason (the two are on disconnected pieces of it) — a different problem, said plainly
   * rather than mislabeled "off-mesh".
   * @param {?{start:?Object, goal:?Object}} offMesh @param {number[]} legStart
   * @param {number[]} legGoal @param {number} i leg index @param {number} n waypoint count
   * @returns {void}
   */
  _markFailedLeg(offMesh, legStart, legGoal, i, n) {
    if (!offMesh) return;
    if (offMesh.start) this._addOffMeshMark(legStart, this._astarBadgeLabel(i, n), { ...offMesh.start, exact: false });
    if (offMesh.goal) this._addOffMeshMark(legGoal, this._astarBadgeLabel(i + 1, n), { ...offMesh.goal, exact: false });
  }

  /** Trailing clause for a failed-route status line, naming the off-mesh endpoints. @returns {string} */
  _offMeshHint() {
    if (!this.offMeshMarks.length) {
      return '（起终点都在网格上——多半是两点分属互不连通的网格块）';
    }
    const parts = this.offMeshMarks.map((m) => {
      if (m.distance === null || m.distance === undefined) return `${m.label} 附近没有可走网格`;
      // budget = 这个位置上运行时肯盲走的上限, 由后端按起点/终点的角色给。
      const over = m.budget && m.distance > m.budget ? `，超出盲走上限 ${m.budget} 格` : '';
      return `${m.label} 离网格 ${m.distance.toFixed(1)} 格${over}`;
    });
    return `（${parts.join('；')}）`;
  }

  /**
   * Drop the off-mesh overlays and void any probe still in flight, so a response that
   * arrives late can no longer re-add a badge for a point that has since changed.
   *
   * Synchronous on purpose: callers paint right after, and must paint the cleared state.
   * @returns {void}
   */
  _resetOffMeshOverlays() {
    this.astarBlindWalks = [];
    this.offMeshMarks = [];
    this._probeToken += 1;
  }

  /** Drop all A* click points, the computed route, and the preview markers. @returns {void} */
  _clearAstarPreview() {
    this.astarPoints = [];
    this.astarRoute = null;
    this.astarLocateHints = [];
    this._resetOffMeshOverlays();
  }

  /** Reset A* view state on a zone change. @returns {void} */
  _resetAstarViewState() {
    this._clearAstarPreview();
    this._meshKey = null; // force a mesh reload for the new zone
  }

  /** "清除预览" button. @returns {void} */
  _onClearAstar() {
    this._clearAstarPreview();
    setStatus('已清除 A* 预览。', '#10b981');
    this._paint();
  }

  /**
   * "定位当前位置" button: one-shot backend locate (`/api/locate-once`), then feed
   * the fix into the calling mode's flow — astar: mark a preview hint (switching
   * the displayed zone if the fix is elsewhere); assert: switch zone and drop the
   * drag-rect hint; edit: insert a route point after the current selection.
   * @param {'edit'|'assert'|'astar'} mode
   * @returns {Promise<void>}
   */
  async _onLocateCurrentPosition(mode) {
    if (!this.field) {
      setStatus('地图尚未就绪，无法定位。', '#ef4444');
      return;
    }

    const connectionPayload = this.connection ? this.connection.buildSession() : null;
    setStatus('正在连接游戏并获取位置，请保持游戏前台运行...', '#3b82f6');

    try {
      const res = await locateOnce(connectionPayload);
      if (res && res.ok) {
        const { x, y, zone } = res;
        setStatus(`定位成功: [${x.toFixed(1)}, ${y.toFixed(1)}] @ ${zone}`, '#10b981');

        if (mode === 'astar') {
          const zoneIdNum = this._resolveZoneId(zone);
          if (!Number.isNaN(zoneIdNum)) {
            if (this._astarZoneId() !== zoneIdNum) {
              // _onAstarZoneChanged resets the A* view state, so switch BEFORE marking.
              if (this._selectDisplayZoneById(zoneIdNum)) this._onAstarZoneChanged(false);
            }

            this._addAstarHint(x, y, '游戏当前位置');
            setStatus(`已标记 A* 定位预览点: [${x.toFixed(1)}, ${y.toFixed(1)}]。当前有 ${this.astarLocateHints.length} 个预览点。`, '#10b981');
            this._focusAstarHints();
          }
        } else if (mode === 'assert') {
          const matchedZoneId = normalizeZoneId(zone);
          if (matchedZoneId) {
            const zoneObj = this.field.zoneByName(matchedZoneId);
            const numericIdStr = zoneObj ? String(zoneObj.zone_id) : matchedZoneId;
            this._ensureAssertZoneOption(numericIdStr);
            if (normalizeZoneId(this.els.assertZoneCombo.value) !== numericIdStr) {
              this.els.assertZoneCombo.value = numericIdStr;
              this._onAssertZoneChanged();
            }
            this.assertLocateHint = [x, y];
            this._fitView();
            setStatus(`已定位到游戏当前位置 [${x.toFixed(1)}, ${y.toFixed(1)}]，请在此提示点周围拖拽鼠标来画出断言矩形。`, '#10b981');
            this._paint();
          }
        }
      } else {
        setStatus(`定位失败: ${res?.error || '未知错误'}`, '#ef4444');
      }
    } catch (err) {
      setStatus(`定位异常: ${err && err.message ? err.message : err}`, '#ef4444');
    }
  }

  // ==================================================================================
  //  A* preview markers (locate fix / hand-entered coordinate / imported JSON)
  // ==================================================================================

  /**
   * Append an A* preview marker. Coords are **base px** — the frame locate fixes and
   * exported NAVMESH targets use; `_paint` projects them into the display frame.
   * @param {number} x @param {number} y @param {string} label caption drawn under the marker
   * @returns {void}
   */
  _addAstarHint(x, y, label) {
    this.astarLocateHints.push({ x, y, label });
  }

  /**
   * Point the A* base/tier combos (which also drive the Assert display frame) at the
   * map owning `zoneId`. Does not reload the mesh — callers follow with
   * {@link MapNavigatorApp#_onAstarZoneChanged}.
   * @param {number} zoneId
   * @returns {boolean} whether the combos were pointed at a known base
   */
  _selectDisplayZoneById(zoneId) {
    if (!this.field || Number.isNaN(zoneId)) return false;
    const base = this.field.zoneById(this.field.geometryZoneId(zoneId));
    if (!base || !base.name) return false;
    this.els.astarDisplayZoneCombo.value = base.name;
    this._refreshAstarZoneChoices();
    const label = this.field.zoneLabel(zoneId);
    this.els.astarZoneCombo.value = label;
    this.els.astarSelectedTierLabel.textContent = label;
    return true;
  }

  /**
   * "标点" button / Enter in either coordinate box: mark a preview point at the typed
   * base-px coordinate, then frame the markers.
   * @returns {void}
   */
  _onAstarMarkCoord() {
    const x = Number(String(this.els.astarCoordX.value || '').trim());
    const y = Number(String(this.els.astarCoordY.value || '').trim());
    if (!this.els.astarCoordX.value.trim() || !this.els.astarCoordY.value.trim() || !Number.isFinite(x) || !Number.isFinite(y)) {
      setStatus('请在 X / Y 两个框中各填一个数字，例如 X=1234.5、Y=678.9', '#ef4444');
      return;
    }
    this._addAstarHint(x, y, `[${compactNumber(x)}, ${compactNumber(y)}]`);
    this.els.astarCoordX.value = '';
    this.els.astarCoordY.value = '';

    setStatus(
      `已标记坐标预览点: [${x}, ${y}]（底图坐标）。当前有 ${this.astarLocateHints.length} 个预览点。`,
      '#10b981',
    );
    this._focusAstarHints();
  }

  // ==================================================================================
  //  Mode switching (mutually exclusive: edit / assert / A*)
  // ==================================================================================

  /**
   * Assert-zone combo change: reset the drag rect, sync the tier label, and mirror
   * the selection into the A* combos so switching modes keeps the same map.
   * @returns {void}
   */
  _onAssertZoneChanged() {
    const zoneId = normalizeZoneId(this.els.assertZoneCombo.value);
    if (!zoneId) return;
    this.els.assertZoneCombo.value = zoneId;
    this.assertRectWorld = null;
    this.isAssertSelecting = false;
    this._refreshZoneLabel();
    if (this.field) {
      const zoneIdNum = parseInt(zoneId, 10);
      if (!Number.isNaN(zoneIdNum)) {
        const label = this.field.zoneLabel(zoneIdNum) || zoneId;
        this.els.assertSelectedTierLabel.textContent = label;
        const baseId = this.field.geometryZoneId(zoneIdNum);
        const base = this.field.zoneById(baseId);
        if (base && base.name) {
          this.els.astarDisplayZoneCombo.value = base.name;
          this._refreshAstarZoneChoices();
          this.els.astarZoneCombo.value = label;
          this.els.astarSelectedTierLabel.textContent = label;
        }
      } else {
        this.els.assertSelectedTierLabel.textContent = zoneId;
      }
    } else {
      this.els.assertSelectedTierLabel.textContent = zoneId;
    }
    this._fitView();
  }

  /**
   * A* display-zone (base map) combo change: repopulate the tier choices, drop the
   * A* click/preview state, and optionally refit the view.
   * @param {boolean} [fitView=true]
   * @returns {void}
   */
  _onAstarDisplayZoneChanged(fitView = true) {
    const zoneId = normalizeZoneId(this.els.astarDisplayZoneCombo.value);
    if (!zoneId) return;
    this.els.astarDisplayZoneCombo.value = zoneId;
    this._refreshAstarZoneChoices();
    this._resetAstarViewState();
    this._refreshZoneLabel();
    if (fitView) this._fitView();
  }

  /**
   * A* tier combo change: align the display-zone combo, drop the A* click/preview
   * state, and mirror the selection into the assert combo/label.
   * @param {boolean} [fitView=true]
   * @returns {void}
   */
  _onAstarZoneChanged(fitView = true) {
    this._selectAstarDisplayForZone();
    this._resetAstarViewState();
    this._refreshZoneLabel();
    if (fitView) this._fitView();
    this._doRedraw();
    if (this.els.astarZoneCombo.value) {
      const label = this.els.astarZoneCombo.value;
      this.els.astarSelectedTierLabel.textContent = label;
      const zoneId = this._astarZoneId();
      if (!Number.isNaN(zoneId)) {
        const zoneStr = String(zoneId);
        this._ensureAssertZoneOption(zoneStr);
        this.els.assertZoneCombo.value = zoneStr;
        this.els.assertSelectedTierLabel.textContent = label;
      }
    }
  }

  /**
   * Point the display-zone combo at the base map that owns the selected A* tier.
   * @returns {void}
   */
  _selectAstarDisplayForZone() {
    if (!this.field) return;
    const zoneId = this._astarZoneId();
    if (Number.isNaN(zoneId)) return;
    const baseId = this.field.geometryZoneId(zoneId);
    const base = this.field.zoneById(baseId);
    if (base && this.field.displayBaseNames().includes(base.name) && this.els.astarDisplayZoneCombo.value !== base.name) {
      this.els.astarDisplayZoneCombo.value = base.name;
      this._refreshAstarZoneChoices();
    }
  }

  // ==================================================================================
  //  Zone navigation
  // ==================================================================================

  /** Step to the previous zone segment (edit/assert) or base map (A*). @returns {void} */
  _prevZone() {
    if (this.state.mode === Mode.ASTAR) {
      this._moveAstarDisplayZone(-1);
      return;
    }
    this.state.zoneState.prevZone();
    this.state.clearSelection();
    this._syncActionControls();
    this._refreshZoneLabel();
    this._fitView();
  }

  /** Step to the next zone segment (edit/assert) or base map (A*). @returns {void} */
  _nextZone() {
    if (this.state.mode === Mode.ASTAR) {
      this._moveAstarDisplayZone(1);
      return;
    }
    this.state.zoneState.nextZone();
    this.state.clearSelection();
    this._syncActionControls();
    this._refreshZoneLabel();
    this._fitView();
  }

  /**
   * Cycle the A* display-zone combo by `delta` (wraps around) and apply the change.
   * @param {number} delta ±1
   * @returns {void}
   */
  _moveAstarDisplayZone(delta) {
    const names = this.field ? this.field.displayBaseNames() : [];
    if (!names.length) return;
    const cur = normalizeZoneId(this.els.astarDisplayZoneCombo.value, this._defaultAstarDisplayZone());
    let index = names.indexOf(cur);
    if (index < 0) index = 0;
    const next = ((index + delta) % names.length + names.length) % names.length;
    this.els.astarDisplayZoneCombo.value = names[next];
    this._onAstarDisplayZoneChanged();
  }

  // ==================================================================================
  //  Action-chain editing (设单 / 追加 / 退一) + delete
  // ==================================================================================

  /** @returns {string} the action name selected in the dropdown. */
  _actionName() {
    return this.els.actionMenu.value;
  }

  /** @returns {boolean} the strict-arrival checkbox state. */
  _strict() {
    return this.els.chkStrict.checked;
  }

  /** "设为该动作" button: apply the dropdown action + strict flag to the selection. @returns {void} */
  _applyAction() {
    const result = this.state.editApplyActionToSelected(this._actionName(), this._strict());
    if (result.selectionEmpty) {
      setStatus('请先点击选中一个点', '#f59e0b');
      return;
    }
    if (result.changed) this._afterStructureChanged();
  }

  /** Delete per mode: A* preview, assert rect, or the selected route points. @returns {void} */
  _deleteSelectedPoint() {
    if (this.state.mode === Mode.ASTAR) {
      this._clearAstarPreview();
      setStatus('已清除 A* 预览。', '#10b981');
      this._paint();
      return;
    }
    if (this.state.mode === Mode.ASSERT) {
      if (!this.assertRectWorld) {
        setStatus('当前没有可删除的 Assert 区域', '#f59e0b');
        return;
      }
      this.assertRectWorld = null;
      this.isAssertSelecting = false;
      setStatus('已清除 Assert 区域。', '#10b981');
      this._paint();
      return;
    }
    const result = this.state.editDeleteSelected();
    if (result.selectionEmpty) {
      setStatus('请先点击选中一个点', '#f59e0b');
      return;
    }
    this._resetPropertyControls();
    this._afterStructureChanged();
  }

  /** tk `_on_points_structure_changed` tail (points already reindexed by the edit helper). */
  _afterStructureChanged() {
    this._syncActionControls();
    this._refreshZoneLabel();
    this._doRedraw();
  }

  // ==================================================================================
  //  Copy actions
  // ==================================================================================

  /** Export the route as a MapNavigator path node (backend, tk-byte-identical) and copy it. @returns {Promise<void>} */
  async _copyPath() {
    if (!this.state.points.length) {
      setStatus('当前没有任何轨迹数据', '#ef4444');
      return;
    }
    try {
      const result = await exportPath(this.state.points);
      await this._copyText(result.text);
      setStatus('MapNavigator path 已复制到剪贴板', '#10b981');
    } catch (err) {
      const msg = err && err.message ? err.message : err;
      setStatus(`复制失败: ${msg}`, '#ef4444');
    }
  }

  /** Export the assert rect as a MapLocateAssertLocation node and copy it. @returns {Promise<void>} */
  async _copyAssert() {
    const zoneId = this._displayZoneId();
    if (!zoneId) {
      setStatus('请先选择 Assert 地图', '#ef4444');
      return;
    }
    const target = this._currentAssertTarget();
    if (!target) {
      setStatus('请先在地图上拖拽画出断言矩形', '#ef4444');
      return;
    }
    try {
      const result = await exportAssert(zoneId, target);
      await this._copyText(result.text);
      setStatus('MapLocateAssertLocation 节点已复制到剪贴板', '#10b981');
    } catch (err) {
      const msg = err && err.message ? err.message : err;
      setStatus(`复制失败: ${msg}`, '#ef4444');
    }
  }

  /**
   * Copy the A* waypoints (all clicked points after the start, in base px) as
   * NAVMESH action payloads — a single object for one target, an array for a
   * multi-leg route. With no route planned, falls back to the locate hint.
   * @returns {Promise<void>}
   */
  async _copyNavmesh() {
    const zoneId = this._displayZoneId();
    if (!zoneId) {
      setStatus('请先选择 NAVMESH 底图', '#ef4444');
      return;
    }
    if (this.astarPoints.length < 2) {
      const hint = this.astarLastHint;
      if (hint) {
        const tierId = this._activeDisplayTierId();
        let tierName = '';
        if (tierId !== null) {
          const zone = this.field.zoneById(tierId);
          if (zone && zone.name) {
            tierName = zone.name;
          }
        }
        // Preview markers are already base px (locate fixes / hand-entered coords).
        const payload = {
          action: 'NAVMESH',
          target: [compactNumber(hint.x), compactNumber(hint.y)]
        };
        if (tierName) {
          payload.target_tier = tierName;
        }
        await this._copyText(JSON.stringify(payload, null, 4));
        const tierNote = tierName ? ` target_tier=${tierName}` : '';
        setStatus(
          `NAVMESH 目标已复制: zone=${zoneId} target=[${payload.target[0]}, ${payload.target[1]}]${tierNote}`,
          '#10b981',
        );
        return;
      }
      setStatus('请先标出一个预览点（定位 / 填坐标 / 导入 JSON），或在地图上画一条预览路线', '#ef4444');
      return;
    }
    const tierId = this._activeDisplayTierId();
    let tierName = '';
    if (tierId !== null) {
      const zone = this.field.zoneById(tierId);
      if (zone && zone.name) {
        tierName = zone.name;
      }
    }

    const targets = [];
    for (let i = 1; i < this.astarPoints.length; i++) {
      const pt = this.astarPoints[i];
      const basePt = tierId !== null ? this.field.tierToBase(tierId, pt[0], pt[1]) : pt;
      const payload = {
        action: 'NAVMESH',
        target: [compactNumber(basePt[0]), compactNumber(basePt[1])]
      };
      if (tierName) {
        payload.target_tier = tierName;
      }
      targets.push(payload);
    }

    if (targets.length === 1) {
      await this._copyText(JSON.stringify(targets[0], null, 4));
      const tierNote = tierName ? ` target_tier=${tierName}` : '';
      setStatus(
        `NAVMESH 目标已复制: zone=${zoneId} target=[${targets[0].target[0]}, ${targets[0].target[1]}]${tierNote}`,
        '#10b981',
      );
    } else {
      await this._copyText(JSON.stringify(targets, null, 4));
      setStatus(`多段 A* NAVMESH 路径已复制: 共 ${targets.length} 个目标路点`, '#10b981');
    }
  }

  /** Copy `text` to the OS clipboard (async clipboard API + hidden-textarea fallback). */
  async _copyText(text) {
    try {
      if (navigator.clipboard && navigator.clipboard.writeText) {
        await navigator.clipboard.writeText(text);
        return true;
      }
    } catch {
      // fall through to the legacy path
    }
    try {
      const textarea = document.createElement('textarea');
      textarea.value = text;
      textarea.style.position = 'fixed';
      textarea.style.opacity = '0';
      document.body.appendChild(textarea);
      textarea.focus();
      textarea.select();
      const ok = document.execCommand('copy');
      document.body.removeChild(textarea);
      return ok;
    } catch {
      return false;
    }
  }

  // ==================================================================================
  //  Keyboard
  // ==================================================================================

  /**
   * Global keyboard shortcuts (skipped while a form control has focus): undo/redo,
   * Delete (A* pop-last first), 1/2 tool switch, +/- zoom, C copy coords.
   * @param {KeyboardEvent} e
   * @returns {void}
   */
  _onKeyDown(e) {
    const target = e.target;
    if (
      target &&
      (target.tagName === 'INPUT' || target.tagName === 'SELECT' || target.tagName === 'TEXTAREA' || target.isContentEditable)
    ) {
      return;
    }
    const ctrl = e.ctrlKey || e.metaKey;
    if (ctrl && (e.key === 'z' || e.key === 'Z')) {
      if (e.shiftKey) this._redo();
      else this._undo();
      e.preventDefault();
      return;
    }
    if (ctrl && (e.key === 'y' || e.key === 'Y')) {
      this._redo();
      e.preventDefault();
      return;
    }
    if (e.key === 'Delete' || e.key === 'Backspace') {
      if (this.state.mode === Mode.ASTAR) {
        if (this.astarPoints.length > 0) {
          this.astarPoints.pop();
          this.astarRoute = null;
          if (this.astarPoints.length >= 2) {
            this._calculateAstarPreview();
          } else {
            if (this.astarPoints.length === 1) {
              setStatus(`A* 起点: [${this.astarPoints[0][0].toFixed(1)}, ${this.astarPoints[0][1].toFixed(1)}]。`, '#3b82f6');
            } else {
              setStatus('A* 点已清空。', '#3b82f6');
            }
            this._probeLoneAstarPoint();
            this._paint();
          }
          e.preventDefault();
          return;
        }
      }
      this._deleteSelectedPoint();
      e.preventDefault();
      return;
    }
    if (e.key === '1') {
      if (this.state.mode === Mode.EDIT) this._setActiveTool('add');
      else if (this.state.mode === Mode.ASTAR) this._setActiveTool('astar-single');
      else if (this.state.mode === Mode.ASSERT) this._setActiveTool('assert-edit');
      e.preventDefault();
      return;
    }
    if (e.key === '2') {
      if (this.state.mode === Mode.EDIT) this._setActiveTool('select');
      else if (this.state.mode === Mode.ASTAR) this._setActiveTool('astar-multi');
      e.preventDefault();
      return;
    }
    if (e.key === '+' || e.key === '=' || e.code === 'NumpadAdd') {
      this._zoomIn();
      e.preventDefault();
      return;
    }
    if (e.key === '-' || e.key === '_' || e.code === 'NumpadSubtract') {
      this._zoomOut();
      e.preventDefault();
      return;
    }
    if (e.key === 'c' || e.key === 'C') {
      this._copyCoordKey();
    }
  }

  /**
   * Switch the active canvas tool, updating toolbar highlight + canvas cursor.
   * @param {'pan'|'add'|'select'|'astar-single'|'astar-multi'|'astar-pan'|'assert-pan'|'assert-edit'} tool
   * @returns {void}
   */
  _setActiveTool(tool) {
    this.activeTool = tool;

    const e = this.els;
    if (e.toolPan) e.toolPan.classList.toggle('active', tool === 'pan');
    if (e.toolAdd) e.toolAdd.classList.toggle('active', tool === 'add');
    if (e.toolSelect) e.toolSelect.classList.toggle('active', tool === 'select');
    if (e.toolAstarSingle) e.toolAstarSingle.classList.toggle('active', tool === 'astar-single');
    if (e.toolAstarMulti) e.toolAstarMulti.classList.toggle('active', tool === 'astar-multi');
    if (e.toolAssertPan) e.toolAssertPan.classList.toggle('active', tool === 'assert-pan');
    if (e.toolAssertEdit) e.toolAssertEdit.classList.toggle('active', tool === 'assert-edit');

    const canvas = e.overlayCanvas;
    if (tool === 'pan' || tool === 'assert-pan' || tool === 'astar-pan') {
      canvas.style.cursor = 'grab';
    } else if (tool === 'add' || tool === 'astar-single' || tool === 'astar-multi') {
      canvas.style.cursor = 'crosshair';
    } else if (tool === 'select') {
      canvas.style.cursor = 'default';
    } else if (tool === 'assert-edit') {
      canvas.style.cursor = 'cell';
    } else {
      canvas.style.cursor = 'default';
    }
  }

  /** @returns {void} */
  _undo() {
    if (this.state.mode === Mode.ASTAR) return;
    if (this.state.undo()) this._afterHistory();
  }

  /** @returns {void} */
  _redo() {
    if (this.state.mode === Mode.ASTAR) return;
    if (this.state.redo()) this._afterHistory();
  }

  /** Refresh controls + repaint after an undo/redo restored a snapshot. @returns {void} */
  _afterHistory() {
    this._syncActionControls();
    this._refreshZoneLabel();
    this._doRedraw();
  }

  /** C key: copy coords (tk `_on_copy_coord_key`). */
  _copyCoordKey() {
    if (this.state.mode === Mode.ASTAR) {
      let points = this.astarRoute && this.astarRoute.points ? this.astarRoute.points : [];
      if (!points.length) {
        points = [this.astarStart, this.astarGoal].filter(Boolean);
        const tierId = this._activeDisplayTierId();
        if (tierId !== null) points = points.map((p) => this.field.tierToBase(tierId, p[0], p[1]));
      }
      if (!points.length) {
        setStatus('当前没有可复制的 A* 预览点。', '#f59e0b');
        return;
      }
      const text = JSON.stringify(
        points.map((p) => [compactNumber(p[0]), compactNumber(p[1])]),
        null,
        4,
      );
      this._copyText(text).then((ok) => {
        if (ok) setStatus(`📋 已复制 A* 预览点：${points.length} 个`, '#10b981');
      });
      return;
    }

    if (this.recording && this.recording.recording) return;

    const selected = [...this.state.selectedIndices].sort((a, b) => a - b);
    if (!selected.length) {
      setStatus('请先选中一个点再按 C 复制坐标。', '#f59e0b');
      return;
    }
    const zoneIndices = this.state.zonePointGlobalIndices();
    let text;
    let status;
    if (selected.length === 1) {
      const point = this.state.points[zoneIndices[selected[0]]];
      text = `[${compactNumber(point.x)}, ${compactNumber(point.y)}]`;
      const zone = normalizeZoneId(point.zone || '');
      status = `📋 已复制坐标: ${text}${zone ? `  (zone: ${zone})` : ''}`;
    } else {
      text = selected
        .map((idx) => {
          const point = this.state.points[zoneIndices[idx]];
          return `[${compactNumber(point.x)}, ${compactNumber(point.y)}]`;
        })
        .join(',\n');
      status = `📋 已复制 ${selected.length} 个点的坐标`;
    }
    this._copyText(text).then((ok) => {
      if (ok) setStatus(status, '#10b981');
    });
  }

  // ==================================================================================
  //  Zoom
  // ==================================================================================

  /** Zoom in around the canvas center (button / `+` key). @returns {void} */
  _zoomIn() {
    this.camera.zoomAt(this._cssW / 2, this._cssH / 2, 1.25);
    this._paint();
  }

  /** Zoom out around the canvas center (button / `-` key). @returns {void} */
  _zoomOut() {
    this.camera.zoomAt(this._cssW / 2, this._cssH / 2, 0.8);
    this._paint();
  }

  // ==================================================================================
  //  Property controls + labels
  // ==================================================================================

  /** Reset the property panel to its no-selection defaults. @returns {void} */
  _resetPropertyControls() {
    this.els.actionMenu.value = ACTION_NAMES[ActionType.RUN];
    this.els.chkStrict.checked = false;
    this.els.actionChainLabel.textContent = 'Run';
    if (this.els.propertiesEmptyState && this.els.propertiesEditor) {
      this.els.propertiesEmptyState.hidden = false;
      this.els.propertiesEditor.hidden = true;
    }
  }

  /**
   * @param {?object} point
   * @returns {string} the point's action chain as `"Run -> Jump"` style text
   */
  _formatActionChain(point) {
    if (!point) return 'Run';
    return getPointActions(point)
      .map((action) => ACTION_NAMES[action] || 'Run')
      .join(' -> ');
  }

  /** Reflect the current selection into the action/strict/chain controls (tk `_sync_action_controls`). */
  _syncActionControls() {
    this._renderWaypointList();
    const zoneIndices = this.state.zonePointGlobalIndices();
    const selected = [...this.state.selectedIndices].sort((a, b) => a - b);

    if (!selected.length) {
      this._resetPropertyControls();
      this.els.propertiesEmptyState.hidden = false;
      this.els.propertiesEditor.hidden = true;
      return;
    }

    this.els.propertiesEmptyState.hidden = true;
    this.els.propertiesEditor.hidden = false;

    if (selected.length > 1) {
      const points = selected.map((idx) => this.state.points[zoneIndices[idx]]);
      const chains = new Set(points.map((p) => JSON.stringify(getPointActions(p))));
      const stricts = new Set(points.map((p) => !!p.strict));
      if (chains.size === 1) {
        const actions = getPointActions(points[0]);
        this.els.actionMenu.value = ACTION_NAMES[actions[actions.length - 1]] || 'Run';
      }
      if (stricts.size === 1) this.els.chkStrict.checked = [...stricts][0];
      this.els.actionChainLabel.textContent = `多选 ${selected.length} 点`;
      return;
    }
    const point = this.state.selectedPoint();
    if (!point) {
      this._resetPropertyControls();
      this.els.propertiesEmptyState.hidden = false;
      this.els.propertiesEditor.hidden = true;
      return;
    }
    const actions = getPointActions(point);
    this.els.actionMenu.value = ACTION_NAMES[actions[actions.length - 1]] || 'Run';
    this.els.chkStrict.checked = !!point.strict;
    this.els.actionChainLabel.textContent = this._formatActionChain(point);
  }

  /**
   * Rebuild the sidebar waypoint list from the current zone segment. Each row is a
   * numbered, draggable entry (local index within the segment) — click to select +
   * center the map on it, drag to reorder within the segment. Only the current
   * segment's points are listed (segments are contiguous runs in `points`).
   * @returns {void}
   */
  _renderWaypointList() {
    const host = this.els.waypointList;
    if (!host) return;
    const zoneIndices = this.state.zonePointGlobalIndices();
    host.textContent = '';

    if (!zoneIndices.length) {
      const empty = document.createElement('div');
      empty.className = 'wp-empty';
      empty.textContent = '当前片段暂无路点';
      host.appendChild(empty);
      return;
    }

    const primary = this.state.selectedIdx;
    const selectedSet = this.state.selectedIndices;
    for (let idx = 0; idx < zoneIndices.length; idx += 1) {
      const point = this.state.points[zoneIndices[idx]];
      const actions = getPointActions(point);
      const displayAction = actions[actions.length - 1];
      const row = document.createElement('div');
      row.className = 'wp-row';
      row.draggable = true;
      row.dataset.local = String(idx);
      if (selectedSet.has(idx)) row.classList.add('wp-row-selected');
      if (idx === primary) row.classList.add('wp-row-primary');

      const handle = document.createElement('span');
      handle.className = 'wp-handle';
      handle.textContent = '⠿';

      const num = document.createElement('span');
      num.className = 'wp-idx';
      num.textContent = actions.length > 1 ? `${idx}*` : String(idx);

      const dot = document.createElement('span');
      dot.className = 'wp-dot';
      dot.style.background = ACTION_COLORS[displayAction] || '#64748b';

      const name = document.createElement('span');
      name.className = 'wp-action';
      name.textContent = this._formatActionChain(point);

      const coord = document.createElement('span');
      coord.className = 'wp-coord';
      coord.textContent = `${compactNumber(point.x)}, ${compactNumber(point.y)}`;

      row.append(handle, num, dot, name, coord);
      if (point.strict) {
        const strict = document.createElement('span');
        strict.className = 'wp-strict';
        strict.textContent = '严';
        strict.title = '严格到达';
        row.appendChild(strict);
      }
      host.appendChild(row);
    }
  }

  /**
   * Pan the view to center the current segment's point at local index `localIdx`.
   * Keeps the current zoom; used by waypoint-list row clicks.
   * @param {number} localIdx local index within the current zone segment
   * @returns {void}
   */
  _focusLocalPoint(localIdx) {
    const zoneIndices = this.state.zonePointGlobalIndices();
    if (localIdx < 0 || localIdx >= zoneIndices.length) return;
    const point = this.state.points[zoneIndices[localIdx]];
    this.camera.centerOn(point.x, point.y, this._cssW, this._cssH, LEFT_PANEL_FIT_OFFSET);
    this._paint();
  }

  /**
   * Reorder the dragged waypoint (`fromLocal`) to a drop `gap` (an insertion slot in
   * `[0, count]`). Converts the gap to a destination index, no-ops when the drop
   * lands on either side of the source, then applies the move + refresh.
   * @param {number} fromLocal source local index
   * @param {number} gap drop insertion slot in `[0, count]`
   * @returns {void}
   */
  _reorderWaypoint(fromLocal, gap) {
    if (gap === fromLocal || gap === fromLocal + 1) return;
    const toLocal = gap > fromLocal ? gap - 1 : gap;
    if (this.state.editReorderWithinZone(fromLocal, toLocal)) {
      this._afterStructureChanged();
    }
  }

  /**
   * Attach the waypoint-list interactions via event delegation (rows are rebuilt on
   * every render, so listeners live on the container). Click selects + centers;
   * native HTML5 drag reorders within the segment.
   * @param {?HTMLElement} host the `#waypoint-list` container
   * @returns {void}
   */
  _wireWaypointList(host) {
    if (!host) return;
    this._wpDragFrom = null;

    host.addEventListener('click', (ev) => {
      const row = ev.target.closest('.wp-row');
      if (!row) return;
      const local = Number(row.dataset.local);
      this.state.setSelection([local], local);
      this._syncActionControls();
      this._focusLocalPoint(local);
    });

    host.addEventListener('dragstart', (ev) => {
      const row = ev.target.closest('.wp-row');
      if (!row) return;
      this._wpDragFrom = Number(row.dataset.local);
      row.classList.add('wp-row-dragging');
      ev.dataTransfer.effectAllowed = 'move';
      ev.dataTransfer.setData('text/plain', row.dataset.local);
    });

    host.addEventListener('dragover', (ev) => {
      if (this._wpDragFrom == null) return;
      ev.preventDefault();
      ev.dataTransfer.dropEffect = 'move';
      this._wpShowDropGap(this._wpDropGap(ev));
    });

    host.addEventListener('drop', (ev) => {
      if (this._wpDragFrom == null) return;
      ev.preventDefault();
      const gap = this._wpDropGap(ev);
      const from = this._wpDragFrom;
      this._wpDragFrom = null;
      this._reorderWaypoint(from, gap);
    });

    host.addEventListener('dragend', () => {
      this._wpDragFrom = null;
      for (const r of host.querySelectorAll('.wp-row')) {
        r.classList.remove('wp-row-dragging', 'wp-drop-before', 'wp-drop-after');
      }
    });
  }

  /**
   * Resolve the drop insertion slot from a drag event's pointer Y: the first row
   * whose vertical midpoint the pointer sits above, else past the last row.
   * @param {DragEvent} ev
   * @returns {number} insertion slot in `[0, rowCount]`
   */
  _wpDropGap(ev) {
    const rows = [...this.els.waypointList.querySelectorAll('.wp-row')];
    for (let i = 0; i < rows.length; i += 1) {
      const rect = rows[i].getBoundingClientRect();
      if (ev.clientY < rect.top + rect.height / 2) return i;
    }
    return rows.length;
  }

  /**
   * Paint the drop indicator for insertion slot `gap` (a line before that row, or
   * after the last row when dropping at the end).
   * @param {number} gap insertion slot in `[0, rowCount]`
   * @returns {void}
   */
  _wpShowDropGap(gap) {
    const rows = [...this.els.waypointList.querySelectorAll('.wp-row')];
    for (const r of rows) r.classList.remove('wp-drop-before', 'wp-drop-after');
    if (!rows.length) return;
    if (gap < rows.length) rows[gap].classList.add('wp-drop-before');
    else rows[rows.length - 1].classList.add('wp-drop-after');
  }

  /** Update the zone label for the current mode's frame. @returns {void} */
  _refreshZoneLabel() {
    if (this.state.mode === Mode.ASTAR) {
      const zoneId = this._displayZoneId();
      this.els.zoneLabel.textContent = zoneId ? `A*: ${zoneId}` : 'A*: 请选择底图';
      return;
    }
    if (this.state.mode === Mode.ASSERT) {
      const zoneId = this._displayZoneId();
      this.els.zoneLabel.textContent = zoneId ? `Assert: ${zoneId}` : 'Assert: 请选择地图';
      return;
    }
    this.els.zoneLabel.textContent = this.state.zoneState.labelText();
  }

  /** Enable/disable assert-mode controls for the current mode. @returns {void} */
  _syncAssertControls() {
    const assertMode = this.state.mode === Mode.ASSERT;
    this.els.btnPrev.disabled = assertMode;
    this.els.btnNext.disabled = assertMode;
    this.els.assertZoneCombo.disabled = !(assertMode && this._availableZoneIds.length);
  }

  /** Enable/disable A*-mode controls + tier label for the current mode. @returns {void} */
  _syncAstarControls() {
    const active = this.state.mode === Mode.ASTAR;
    this.els.astarZoneCombo.disabled = !(active && this.field);
    this.els.astarDisplayZoneCombo.disabled = !(active && this.field && this.field.displayBaseNames().length);
    if (this.state.mode !== Mode.ASSERT) {
      this.els.btnPrev.disabled = false;
      this.els.btnNext.disabled = false;
    }
    if (active) {
      this.els.btnSelectTier.disabled = !(this.field && this.field.displayBaseNames().length);
      this.els.astarSelectedTierLabel.textContent = this.els.astarZoneCombo.value || '未选择层级';
      this.els.astarSelectedTierLabel.style.display = 'inline-block';
    } else {
      this.els.btnSelectTier.disabled = true;
      this.els.astarSelectedTierLabel.style.display = 'none';
    }
  }

  // ==================================================================================
  //  Recording + import callbacks
  // ==================================================================================

  /**
   * {@link RecordingController} finished: adopt the recorded points as the route.
   * @param {object[]} rawPoints
   * @returns {void}
   */
  _onRecordingFinished(rawPoints) {
    this.state.setPoints(rawPoints);
    this.state.clearSelection();
    this._syncActionControls();
    this._refreshZoneLabel();
    setStatus(
      '录制结束。Ctrl+滚轮缩放，滚轮或右键平移；添加工具左键点击插点，拖拽路点微调，Ctrl+拖拽框选批量操作，C 键复制选中点坐标。',
      '#10b981',
    );
    this._fitView();
  }

  /**
   * {@link Importer} parsed a path import.
   * - EDIT / Assert: the points become the real route (Assert draws them read-only).
   * - A*: it holds no route, only target marks — the coordinates become preview points.
   * @param {object[]} points
   * @returns {{text?:string, color?:string}|void} a status lead-in replacing the importer's default
   */
  _importLoadPoints(points) {
    if (this.state.mode === Mode.ASTAR) return this._importAsAstarHints(points);

    this.state.setPoints(points);
    this.state.clearSelection();
    this._resetPropertyControls();
    this._afterStructureChanged();

    if (this.state.mode !== Mode.ASSERT || !this.field || !this.state.points.length) {
      this._fitView();
      return;
    }

    const zoneId = this._resolveZoneId(this.state.points[0].zone);
    if (!Number.isNaN(zoneId) && this._selectDisplayZoneById(zoneId)) this._onAstarZoneChanged(false);
    const drawn = this._displayRealPoints();
    this._fitDisplayPoints(drawn);
    if (!drawn.length) return this._noNavmeshBasemapNote(points, '断言模式画不出这些点（路线已载入）');
  }

  /**
   * A* import: mark every imported coordinate as a preview point (base px), after
   * switching the display frame to the route's own map. The editor's route is left alone.
   * @param {object[]} points
   * @returns {{text?:string, color?:string}|void} the status lead-in
   */
  _importAsAstarHints(points) {
    if (!this.field || !points.length) return;

    const zoneIds = points.map((point) => this._resolveZoneId(point.zone));
    const firstZoneId = zoneIds.find((id) => !Number.isNaN(id));
    if (firstZoneId === undefined) return this._noNavmeshBasemapNote(points, 'A* 模式无法标点');

    // Resets the A* view state (drops the previous marks), so it must run before marking.
    if (this._selectDisplayZoneById(firstZoneId)) this._onAstarZoneChanged(false);

    const displayGeomId = this.field.geometryZoneId(firstZoneId);
    let skipped = 0;
    points.forEach((point, i) => {
      const zoneId = zoneIds[i];
      if (Number.isNaN(zoneId) || this.field.geometryZoneId(zoneId) !== displayGeomId) {
        skipped += 1;
        return;
      }
      const [bx, by] = this._pointToBase(zoneId, point.x, point.y);
      this._addAstarHint(bx, by, String(i + 1));
    });
    this._focusAstarHints();

    const marked = this.astarLocateHints.length;
    if (skipped) return { text: `已在 A* 模式标出 ${marked} 个预览点，${skipped} 个点属于其它底图，已跳过`, color: '#f59e0b' };
    return { text: `已在 A* 模式标出 ${marked} 个预览点` };
  }

  /**
   * Status note for a route whose zone has no navmesh basemap (MapTracker routes such as
   * `map02_lv005` live in their own image frame, which only 路径编辑 can show).
   * @param {object[]} points @param {string} what what the current mode cannot do
   * @returns {{text:string, color:string}}
   */
  _noNavmeshBasemapNote(points, what) {
    const zone = normalizeZoneId((points[0] && points[0].zone) || '') || '未知';
    return {
      text: `zone=${zone} 不是 navmesh 底图区域（MapTracker 路线），${what}，可切到「路径编辑」模式查看`,
      color: '#f59e0b',
    };
  }

  /**
   * {@link Importer} parsed a MapLocateAssertLocation import: enter assert mode on
   * that zone and show the rect.
   * @param {string} zoneId
   * @param {[number, number, number, number]} target `[x, y, w, h]` in zone px
   * @returns {void}
   */
  _importApplyAssert(zoneId, target) {
    this.state.mode = Mode.ASSERT;
    this._ensureAssertZoneOption(zoneId);
    this.els.assertZoneCombo.value = zoneId;
    // The A* combos drive the basemap in Assert mode too; this mirrors the zone into
    // them, and clears assertRectWorld — so it must run before the rect is set.
    this._onAssertZoneChanged();
    const [x, y, w, h] = target;
    this.assertRectWorld = [x, y, x + w, y + h];
    this.isAssertSelecting = false;
    this._syncAssertControls();
    this._syncAstarControls();
    this._refreshZoneLabel();
    this._syncModeTabUI();
    this._fitView();
  }

  /**
   * Make sure the assert combo has an option for `zoneId` (imports/locate can
   * reference zones the backend scan didn't list).
   * @param {string} zoneId
   * @returns {void}
   */
  _ensureAssertZoneOption(zoneId) {
    const combo = this.els.assertZoneCombo;
    if (![...combo.options].some((opt) => opt.value === zoneId)) {
      const opt = document.createElement('option');
      opt.value = zoneId;
      opt.textContent = zoneId;
      combo.appendChild(opt);
    }
  }

  /**
   * Sidebar mode-tab click: switch to edit / assert / A*, clearing the other
   * modes' canvas artifacts and picking a default zone where needed.
   * @param {'edit'|'assert'|'astar'} modeName
   * @returns {void}
   */
  _selectModeTab(modeName) {
    const e = this.els;

    if (modeName !== 'astar') {
      this._clearAstarPreview();
    }
    if (modeName !== 'assert') {
      this.assertRectWorld = null;
      this.isAssertSelecting = false;
    }

    if (modeName === 'edit') {
      this.state.mode = Mode.EDIT;
      setStatus('返回路径编辑模式。', '#10b981');
    } else if (modeName === 'assert') {
      this.state.mode = Mode.ASSERT;
      if (!this._availableZoneIds.length && !normalizeZoneId(this.state.currentZone())) {
        setStatus('未找到可用 zone 底图，无法进入断言模式。', '#ef4444');
        this._selectModeTab('edit');
        return;
      }
      if (!normalizeZoneId(e.assertZoneCombo.value)) {
        e.assertZoneCombo.value = this._defaultAssertZone();
      }
      setStatus('断言模式：先选地图，再用左键拖拽框出断言区域；Delete 或清除按钮可清除。', '#3b82f6');
    } else if (modeName === 'astar') {
      this.state.mode = Mode.ASTAR;
      if (this.field) {
        if (!e.astarZoneCombo.value) {
          this._applyDefaultAstarZoneSelection();
        }
        this._onAstarZoneChanged(false);
      }
      setStatus('A* 模式：左键点起点，再点终点生成预览路线。', '#3b82f6');
    }

    this._syncAssertControls();
    this._syncAstarControls();
    this._syncActionControls();
    this._refreshZoneLabel();
    this._syncModeTabUI();
    this._doRedraw();
  }

  /**
   * Reflect `state.mode` into the chrome: tab highlight, visible sidebar panels,
   * mode-* classes on canvas/body, and the mode's default tool.
   * @returns {void}
   */
  _syncModeTabUI() {
    const e = this.els;
    const mode = this.state.mode;

    e.tabEdit.classList.remove('active');
    e.tabAstar.classList.remove('active');
    e.tabAssert.classList.remove('active');

    e.panelRecording.hidden = true;
    e.panelProperties.hidden = true;
    e.panelAstar.hidden = true;
    e.panelAssert.hidden = true;

    if (mode === Mode.ASTAR) {
      e.tabAstar.classList.add('active');
      e.panelAstar.hidden = false;
      e.canvasWrap.classList.remove('mode-edit', 'mode-assert');
      e.canvasWrap.classList.add('mode-astar');
      document.body.classList.remove('mode-edit', 'mode-assert');
      document.body.classList.add('mode-astar');
      this._setActiveTool('astar-single');
    } else if (mode === Mode.ASSERT) {
      e.tabAssert.classList.add('active');
      e.panelAssert.hidden = false;
      e.canvasWrap.classList.remove('mode-edit', 'mode-astar');
      e.canvasWrap.classList.add('mode-assert');
      document.body.classList.remove('mode-edit', 'mode-astar');
      document.body.classList.add('mode-assert');
      this._setActiveTool('assert-edit');
    } else {
      e.tabEdit.classList.add('active');
      e.panelRecording.hidden = false;
      e.panelProperties.hidden = false;
      e.canvasWrap.classList.remove('mode-astar', 'mode-assert');
      e.canvasWrap.classList.add('mode-edit');
      document.body.classList.remove('mode-astar', 'mode-assert');
      document.body.classList.add('mode-edit');
      this._setActiveTool('add');
    }
  }

  /**
   * Run a rAF repaint loop while an A* route/marker is visible (drives the flowing
   * route animation); stops itself once nothing animated remains.
   * @returns {void}
   */
  _requestAnimationLoop() {
    if (this._animating) return;
    this._animating = true;
    const loop = () => {
      if (this.state.mode !== Mode.ASTAR || !(this.astarRoute || this.astarStart || this.astarGoal)) {
        this._animating = false;
        return;
      }
      this._paint();
      requestAnimationFrame(loop);
    };
    requestAnimationFrame(loop);
  }

  /** Open the tier-picker dialog with base buttons + the current base's tier grid. @returns {void} */
  _openTierPicker() {
    if (!this.field) return;
    const e = this.els;
    const basesContainer = e.tierPickerBases;
    const gridContainer = e.tierPickerGrid;
    basesContainer.textContent = '';
    gridContainer.textContent = '';

    const baseNames = this.field.displayBaseNames();
    if (!baseNames.length) return;

    const currentBase = this._displayZoneId() ? this.field.geometryZoneId(this.field.zoneByName(this._displayZoneId())?.zone_id || this._displayZoneId()) : null;
    const currentBaseZone = currentBase ? this.field.zoneById(currentBase) : null;
    const defaultActiveBase = currentBaseZone && baseNames.includes(currentBaseZone.name) ? currentBaseZone.name : baseNames[0];

    baseNames.forEach(name => {
      const btn = document.createElement('button');
      btn.type = 'button';
      btn.className = 'tier-base-btn';
      if (name === defaultActiveBase) btn.classList.add('active');
      btn.textContent = name;
      btn.addEventListener('click', () => {
        basesContainer.querySelectorAll('.tier-base-btn').forEach(b => b.classList.remove('active'));
        btn.classList.add('active');
        this._renderTierGrid(name);
      });
      basesContainer.appendChild(btn);
    });

    this._renderTierGrid(defaultActiveBase);
    e.tierPickerDialog.hidden = false;
  }

  /**
   * Fill the tier-picker grid with `baseName`'s tiers as thumbnail cards; clicking
   * a card applies the tier to the current mode (assert combo or A* combos).
   * @param {string} baseName
   * @returns {void}
   */
  _renderTierGrid(baseName) {
    const e = this.els;
    const gridContainer = e.tierPickerGrid;
    gridContainer.textContent = '';

    const choices = this.field.zoneChoicesForBase(baseName);

    let currentSelectedLabel = '';
    let activeDisplayTierId = null;
    const isAssertMode = this.state.mode === Mode.ASSERT;

    if (isAssertMode) {
      const currentZoneId = normalizeZoneId(e.assertZoneCombo.value);
      if (currentZoneId) {
        const zoneIdNum = parseInt(currentZoneId, 10);
        if (!Number.isNaN(zoneIdNum)) {
          activeDisplayTierId = zoneIdNum;
        }
      }
    } else {
      currentSelectedLabel = e.astarZoneCombo.value;
      activeDisplayTierId = this._activeDisplayTierId();
    }

    choices.forEach(choice => {
      const card = document.createElement('div');
      card.className = 'tier-card';
      const isActive = activeDisplayTierId === choice.id || (activeDisplayTierId === null && choice.label === currentSelectedLabel);
      if (isActive) card.classList.add('active');

      const thumb = document.createElement('div');
      thumb.className = 'tier-card-thumb';
      thumb.style.backgroundImage = `url('/basemap-by-zone?zone_id=${encodeURIComponent(choice.name)}')`;
      card.appendChild(thumb);

      if (isActive) {
        const badge = document.createElement('div');
        badge.className = 'tier-card-badge';
        badge.textContent = '当前';
        card.appendChild(badge);
      }

      const info = document.createElement('div');
      info.className = 'tier-card-info';

      const splitIdx = choice.label.indexOf(':');
      const idText = splitIdx !== -1 ? choice.label.substring(0, splitIdx) : '';
      const nameText = splitIdx !== -1 ? choice.label.substring(splitIdx + 1) : choice.label;

      const title = document.createElement('div');
      title.className = 'tier-card-title';
      title.textContent = nameText;
      info.appendChild(title);

      const desc = document.createElement('div');
      desc.className = 'tier-card-desc';
      desc.textContent = `ID: ${idText}`;
      info.appendChild(desc);

      card.appendChild(info);

      card.addEventListener('click', () => {
        if (isAssertMode) {
          this._ensureAssertZoneOption(choice.name);
          e.assertZoneCombo.value = choice.name;
          this._onAssertZoneChanged();
        } else {
          e.astarDisplayZoneCombo.value = baseName;
          this._refreshAstarZoneChoices();
          e.astarZoneCombo.value = choice.label;
          this._onAstarZoneChanged();
        }
        e.tierPickerDialog.hidden = true;
      });

      gridContainer.appendChild(card);
    });
  }
}

const appInstance = new MapNavigatorApp();
if (document.readyState === 'loading') {
  document.addEventListener('DOMContentLoaded', () => appInstance.boot());
} else {
  appInstance.boot();
}
