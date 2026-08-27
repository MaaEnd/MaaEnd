/**
 * main.js — keystone orchestrator. Boots the app and owns every piece of glue the
 * other modules don't: the render loop (`_doRedraw` / `_paint`, mirroring
 * app_tk `_do_redraw`), the pointer state machine (`on_click`/`on_drag`/`on_release`
 * + right-button pan), wheel/keyboard, the four mutually-exclusive modes
 * (edit / assert / A* / log analysis), zone navigation, fit-view, the copy actions, and the wiring
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

import {Camera} from "./camera.js";
import {Renderer} from "./gl/renderer.js";
import {Overlay} from "./gl/overlay.js";
import {formatHeading, transformHeading} from "./heading.js";
import {NavmeshField} from "./navmesh_field.js";
import {AppState, Mode} from "./state.js";
import {logZiplineGeometry, logZiplineTowers, parseMapNavigatorLog} from "./log_analysis.js";
import {groupLogInputFiles, openZipArchive, selectMaaEndArchiveEntries} from "./log_archive.js";
import {
    measureZiplinePair,
    nextZiplineMeasurementSelection,
    projectZiplineRecords,
} from "./zipline_records.js";
import {
    ACTION_NAMES,
    ActionType,
    ACTION_MENU_NAMES,
    ACTION_COLORS,
    ACTION_MENU_TYPES,
    getPointActions,
    normalizeZoneId,
} from "./model.js";
import {compactNumber, roundHalfEven} from "./rounding.js";
import {initFeedback, setStatus} from "./ui/toast.js";
import {nextWheelSelectIndex} from "./ui/select.js";
import {ConnectionPanel} from "./ui/connection.js";
import {parsePastedCoordinatePair} from "./ui/coordinate.js";
import {RecordingController} from "./ui/recording.js";
import {NavTestController} from "./ui/navtest.js";
import {collectAstarImportBasePoints, completeAstarImportWithStart, Importer} from "./ui/importer.js";
import {PositionReadout} from "./ui/position.js";
import {
    getZones,
    getLoadStatus,
    getMesh,
    getZoneIds,
    getZiplineFrames,
    basemapByZoneUrl,
    postRoute,
    postOffMeshProbe,
    postDeckProbe,
    exportPath,
    exportAssert,
    locateOnce,
} from "./rpc.js";

const DRAG_ACTIVATION_DISTANCE = 4; // px (tk RouteEditorApp.DRAG_ACTIVATION_DISTANCE)
const ASTAR_PREVIEW_SNAP_RADIUS = 5.0; // px (tk ASTAR_PREVIEW_SNAP_RADIUS)
const LOAD_POLL_MS = 400;
// CSS px the floating left panel overlays the canvas; fit-view centers in the rest.
const LEFT_PANEL_FIT_OFFSET = 350;
// World px kept around the A* preview markers when framing them.
const ASTAR_HINT_FIT_PADDING = 200;
const COPY_FORMAT_COORDINATES = "coordinates";
const LOG_TOWER_HIT_RADIUS = 14;
const LOG_POINT_HIT_RADIUS = 12;

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
        /** @type {'astar'|'assert'|'edit'} route tool restored after leaving log analysis. */
        this._lastRouteMode = "astar";

        /** @type {?NavmeshField} */
        this.field = null;
        /** @type {string[]} assert-mode zone options (backend fs scan). */
        this._availableZoneIds = [];

        // --- pointer state machine (mirrors the tk is_* flags) ---
        /** @type {string} current canvas tool (see {@link MapNavigatorApp#_setActiveTool}). */
        this.activeTool = "select";
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
        /** @type {?{x:number,y:number,rot:?number,label:string}} assert locate hint in base px. */
        this.assertLocateHint = null;
        /**
         * A* **preview** markers in base px — from a game locate fix or a hand-entered
         * coordinate. Preview only: never part of `state.points` (the real route).
         * @type {Array<{x:number, y:number, label:string, rot:?number}>}
         */
        this.astarLocateHints = [];
        /** @type {number[][]} imported targets waiting for a manual start, in base px. */
        this.astarPendingTargets = [];
        /** @type {Array<?number>} imported targets' `target_deck_y`, aligned with pending targets. */
        this.astarPendingDecks = [];
        /** @type {?{x0:number,y0:number,x1:number,y1:number}} canvas-px selection box. */
        this.selectionRect = null;
        /** @type {Array<number[]>} A* path finder waypoints in display-frame world. */
        this.astarPoints = [];
        /** @type {?{points:number[][], segment_breaks:number[], cost:number}} */
        this.astarRoute = null;
        /** @type {Array<Object>} one real navmesh diagnostic payload per planned leg. */
        this.astarDiagnostics = [];
        /**
         * 每个 A* 路点声明的可走面高度(`target_deck_y`),与 {@link astarPoints} 同下标;
         * `null` = 不声明 = 寻路取整格全部面。`hintDeck` 是最后一个预览目标点的同一件事。
         * @type {Array<?number>}
         */
        this.astarDecks = [];
        /** @type {?number} */
        this.hintDeck = null;
        /** @type {?{index:number, decks:Array<{height:number, band:number[], thin:boolean}>}} */
        this.deckProbe = null;
        /** @type {?number} 正在预览的那一层高度。 */
        this.deckPreview = null;
        this._deckToken = 0;
        /**
         * Straight lines the runtime walks with no navmesh under it, base px: `off` is the
         * point outside the mesh, `mesh` where the mesh takes over. Taken from the backend's
         * actual ladder result, so this is what the character does — not an estimate.
         * @type {Array<{off:number[], mesh:number[], distance:number, kind:string}>}
         */
        this.astarBlindWalks = [];
        const readDebugFlag = (key, defaultValue) => {
            const stored = localStorage.getItem(key);
            return stored === null ? defaultValue : stored === "1";
        };
        this.navDebug = {
            search: readDebugFlag("maaend.mapnavigator.debugSearch", false),
            rerouted: readDebugFlag("maaend.mapnavigator.debugRerouted", false),
            stringPull: readDebugFlag("maaend.mapnavigator.debugStringPull", false),
            assembled: readDebugFlag("maaend.mapnavigator.debugAssembled", false),
            loopFixed: readDebugFlag("maaend.mapnavigator.debugLoopFixed", false),
            slim: readDebugFlag("maaend.mapnavigator.debugSlim", false),
            widenCorners: readDebugFlag("maaend.mapnavigator.debugWidenCorners", false),
            planned: readDebugFlag("maaend.mapnavigator.debugPlanned", true),
            live: readDebugFlag("maaend.mapnavigator.showLivePath", true),
        };
        this.showLivePath = this.navDebug.live;
        this._syncNavDebugControls();
        this.els.navDebugOptions.open = false;
        /** @type {Array<{x:number,y:number,rot:?number}>} measured points in base px. */
        this.livePathBase = [];
        /** @type {?{x:number,y:number,rot:?number}} latest measured position in base px. */
        this.livePositionBase = null;
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

        // --- read-only log analysis state ---
        /** @type {Array<Object>} parsed MapNavigateAction runs from all imported files. */
        this.logRuns = [];
        /** @type {?Object} */
        this.selectedLogRun = null;
        /** @type {Map<string,{label:string,records:?Object,sourceNames:string[]}>} */
        this.logArchiveGroups = new Map();
        /** @type {?Object} repository zipline_frames.json calibration. */
        this.ziplineFrameConfig = null;
        /** @type {string[]} measure keys for the current A/B zipline tower selection. */
        this.logDistanceSelection = [];
        /** @type {?Object} point selected by the default log inspection tool. */
        this.logInspectedPoint = null;
        /** @type {?Object} cached inspection candidates for hover hit-testing. */
        this._logInspectionCandidateCache = null;
        this.logLayers = {
            showAuthored: true,
            showWalk: true,
            showObserved: true,
            showBaseline: true,
            showZipline: true,
            showSelectedTowers: true,
            showRecordedTowers: true,
            showEstimates: true,
        };

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
            app: $("app"),
            glCanvas: $("gl-canvas"),
            overlayCanvas: $("overlay-canvas"),
            canvasWrap: $("canvas-wrap"),
            btnStart: $("btn-start"),
            btnStop: $("btn-stop"),
            btnCopyPath: $("btn-copy-path"),
            btnCopyAssert: $("btn-copy-assert"),
            assertCopyFormat: $("assert-copy-format"),
            btnImport: $("btn-import"),
            btnEditReadClipboard: $("btn-edit-read-clipboard"),
            btnPrev: $("btn-prev"),
            btnNext: $("btn-next"),
            zoneLabel: $("zone-label"),
            btnZoomOut: $("btn-zoom-out"),
            btnZoomIn: $("btn-zoom-in"),
            actionMenu: $("action-menu"),
            btnApplyAction: $("btn-apply-action"),
            actionChainLabel: $("action-chain-label"),
            targetTierEntry: $("target-tier-entry"),
            targetTierList: $("target-tier-list"),
            assertZoneCombo: $("assert-zone-combo"),
            chkStrict: $("chk-strict"),
            chkRequired: $("chk-required"),
            toolPan: $("tool-pan"),
            toolAdd: $("tool-add"),
            toolSelect: $("tool-select"),
            toolAstarSingle: $("tool-astar-single"),
            toolAstarMulti: $("tool-astar-multi"),
            toolAssertPan: $("tool-assert-pan"),
            toolAssertEdit: $("tool-assert-edit"),
            kindCombo: $("connection-kind-combo"),
            win32Group: $("win32-group"),
            win32Entry: $("win32-entry"),
            playcoverGroup: $("playcover-group"),
            playcoverAddrEntry: $("playcover-addr-entry"),
            playcoverUuidEntry: $("playcover-uuid-entry"),
            adbGroup: $("adb-group"),
            adbPathEntry: $("adb-path-entry"),
            adbTargetInput: $("adb-target-combo"),
            adbTargetList: $("adb-target-list"),
            btnRefreshAdb: $("btn-refresh-adb"),
            linuxGroup: $("linux-group"),
            linuxInstanceCombo: $("linux-instance-combo"),
            btnRefreshLinux: $("btn-refresh-linux"),
            connectionSummary: $("connection-summary"),
            astarDisplayZoneCombo: $("astar-display-zone-combo"),
            astarZoneCombo: $("astar-zone-combo"),
            navDebugOptions: $("nav-debug-options"),
            chkNavDebugSearch: $("chk-nav-debug-search"),
            chkNavDebugRerouted: $("chk-nav-debug-rerouted"),
            chkNavDebugStringPull: $("chk-nav-debug-string-pull"),
            chkNavDebugAssembled: $("chk-nav-debug-assembled"),
            chkNavDebugLoopFixed: $("chk-nav-debug-loop-fixed"),
            chkNavDebugSlim: $("chk-nav-debug-slim"),
            chkNavDebugWidenCorners: $("chk-nav-debug-widen-corners"),
            chkNavDebugPlanned: $("chk-nav-debug-planned"),
            chkNavDebugLivePath: $("chk-nav-debug-live-path"),
            btnClearAstar: $("btn-clear-astar"),
            btnCopyNavmesh: $("btn-copy-navmesh"),
            loadProgress: $("load-progress"),
            loadProgressBar: $("load-progress-bar"),
            loadProgressLabel: $("load-progress-label"),
            statusLabel: $("status-label"),
            locatorLabel: $("locator-label"),
            positionReadout: $("position-readout"),
            positionCoordinates: $("position-coordinates"),
            positionHeading: $("position-heading"),
            positionZone: $("position-zone"),
            positionHeadingArrow: $("position-heading-arrow"),
            importDialog: $("import-dialog"),
            importDialogRows: $("import-dialog-rows"),
            importDialogCancel: $("import-dialog-cancel"),
            importDialogOk: $("import-dialog-ok"),
            projectNodeDialog: $("project-node-dialog"),
            projectNodeHint: $("project-node-hint"),
            projectNodeKinds: $("project-node-kinds"),
            projectNodeKindAssert: $("project-node-kind-assert"),
            projectNodeKindPath: $("project-node-kind-path"),
            projectNodeSearch: $("project-node-search"),
            projectNodeList: $("project-node-list"),
            projectNodeCancel: $("project-node-cancel"),
            projectNodeOk: $("project-node-ok"),
            propertiesLegend: $("properties-legend"),
            tabRoute: $("tab-route"),
            tabEdit: $("tab-edit"),
            tabAstar: $("tab-astar"),
            tabAssert: $("tab-assert"),
            tabLog: $("tab-log"),
            btnClearAssert: $("btn-clear-assert"),
            btnSelectTier: $("btn-select-tier"),
            btnSelectAssertTier: $("btn-select-assert-tier"),
            astarSelectedTierLabel: $("astar-selected-tier-label"),
            assertSelectedTierLabel: $("assert-selected-tier-label"),
            tierPickerDialog: $("tier-picker-dialog"),
            tierPickerBases: $("tier-picker-bases"),
            tierPickerGrid: $("tier-picker-grid"),
            tierPickerCancel: $("tier-picker-cancel"),
            btnFitView: $("btn-fit-view"),
            btnDelPointFloat: $("btn-del-point-float"),
            propertiesEmptyState: $("properties-empty-state"),
            propertiesEditor: $("properties-editor"),
            panelRecording: $("panel-recording"),
            panelConnection: $("panel-connection"),
            panelNavtest: $("panel-navtest"),
            routeModeTabs: $("route-mode-tabs"),
            btnNavtestRun: $("btn-navtest-run"),
            btnNavtestStop: $("btn-navtest-stop"),
            navtestArmed: $("navtest-armed"),
            navtestHotkeyNote: $("navtest-hotkey-note"),
            navtestOverlay: $("navtest-overlay"),
            panelProperties: $("panel-properties"),
            panelAstar: $("panel-astar"),
            panelAssert: $("panel-assert"),
            panelLog: $("panel-log"),
            btnLogImport: $("btn-log-import"),
            btnLogClear: $("btn-log-clear"),
            logFileInput: $("log-file-input"),
            logImportMeta: $("log-import-meta"),
            logRunFilter: $("log-run-filter"),
            logRunSelect: $("log-run-select"),
            logShowAuthored: $("log-show-authored"),
            logShowWalk: $("log-show-walk"),
            logShowObserved: $("log-show-observed"),
            logShowBaseline: $("log-show-baseline"),
            logShowZipline: $("log-show-zipline"),
            logShowSelectedTowers: $("log-show-selected-towers"),
            logShowRecordedTowers: $("log-show-recorded-towers"),
            logShowEstimates: $("log-show-estimates"),
            logContextPanel: $("log-context-panel"),
            logInspectionBox: $("log-inspection-box"),
            btnLogPointClear: $("btn-log-point-clear"),
            logPointSummary: $("log-point-summary"),
            logDistanceBox: $("log-distance-box"),
            btnLogDistanceClear: $("btn-log-distance-clear"),
            logDistanceSummary: $("log-distance-summary"),
            logDecisionSummary: $("log-decision-summary"),
            btnLogMeasure: $("btn-log-measure"),
            logMeasureDivider: $("log-measure-divider"),
            btnAssertLocate: $("btn-assert-locate"),
            btnAstarLocate: $("btn-astar-locate"),
            waypointList: $("waypoint-list"),
            astarCoordX: $("astar-coord-x"),
            astarCoordY: $("astar-coord-y"),
            btnAstarMarkCoord: $("btn-astar-mark-coord"),
            btnAstarImport: $("btn-astar-import"),
            btnAssertImport: $("btn-assert-import"),
            btnAstarReadClipboard: $("btn-astar-read-clipboard"),
            btnAssertReadClipboard: $("btn-assert-read-clipboard"),
            astarDeckBox: $("astar-deck-box"),
            astarDeckTitle: $("astar-deck-title"),
            astarDeckList: $("astar-deck-list"),
        };
    }

    /** Boot: build controllers, wire events, size the canvas, kick the loads. @returns {Promise<void>} */
    async boot() {
        try {
            initFeedback({status: this.els.statusLabel, locator: this.els.locatorLabel});
            this.positionReadout = new PositionReadout({
                root: this.els.positionReadout,
                coordinates: this.els.positionCoordinates,
                heading: this.els.positionHeading,
                zone: this.els.positionZone,
                arrow: this.els.positionHeadingArrow,
            });

            this._populateActionMenu();
            this._resetPropertyControls();
            this._wireEvents();
            this._syncCopyButtonLabels();

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
                linuxGroup: this.els.linuxGroup,
                linuxInstanceCombo: this.els.linuxInstanceCombo,
                btnRefreshLinux: this.els.btnRefreshLinux,
                summary: this.els.connectionSummary,
            });
            this.recording = new RecordingController({
                btnStart: this.els.btnStart,
                btnStop: this.els.btnStop,
                appEl: this.els.app,
                connection: this.connection,
                onFinished: (rawPoints) => this._onRecordingFinished(rawPoints),
                onPosition: (fix) => this.positionReadout.update(fix),
                onPositionPending: () => this.positionReadout.setPending("正在获取实时位置与朝向..."),
                onPositionUnavailable: (message) => this.positionReadout.setPending(message),
            });
            this.navtest = new NavTestController({
                btnRun: this.els.btnNavtestRun,
                btnStop: this.els.btnNavtestStop,
                armedLabel: this.els.navtestArmed,
                overlay: this.els.navtestOverlay,
                hotkeyNote: this.els.navtestHotkeyNote,
                connection: this.connection,
                getRoute: () => this._navtestRoute(),
                onPosition: (fix) => this._onLivePosition(fix),
                onRunState: (running) => {
                    if (running) {
                        this._clearLivePath();
                        this.positionReadout.setPending("正在获取实时位置与朝向...");
                    }
                },
            });
            this.importer = new Importer(
                {
                    btnImport: this.els.btnImport,
                    dialog: this.els.importDialog,
                    dialogRows: this.els.importDialogRows,
                    dialogOk: this.els.importDialogOk,
                    dialogCancel: this.els.importDialogCancel,
                    projectDialog: this.els.projectNodeDialog,
                    projectHint: this.els.projectNodeHint,
                    projectKinds: this.els.projectNodeKinds,
                    projectKindAssert: this.els.projectNodeKindAssert,
                    projectKindPath: this.els.projectNodeKindPath,
                    projectSearch: this.els.projectNodeSearch,
                    projectList: this.els.projectNodeList,
                    projectCancel: this.els.projectNodeCancel,
                    projectOk: this.els.projectNodeOk,
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
            setStatus(`寻路数据加载失败: ${status.error}`, "#ef4444");
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
            setStatus(`navmesh 加载失败: ${err && err.message ? err.message : err}`, "#ef4444");
            return;
        }
        this.field = new NavmeshField(payload && payload.zones ? payload.zones : []);
        this._populateAstarDisplayCombo();
        this._syncAstarControls();
        this._refreshZoneLabel();
        if (this.state.mode === Mode.ASTAR) {
            this._applyDefaultAstarZoneSelection();
            this._onAstarZoneChanged();
        } else if (this.state.mode === Mode.LOG && this.selectedLogRun) {
            this._showSelectedLogRun({fit: true});
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
        let matchedBaseName = "";
        let matchedLabel = "";

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
                baseNames.find((name) => name.toLowerCase().includes("map01")) ||
                (baseNames.length ? baseNames[0] : "");
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
        this._populateTargetTierList();
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
        let text = "读取文件...";
        if (progress >= 0.7) text = "生成预览图像...";
        else if (progress >= 0.25) text = "构建空间索引...";
        else if (progress >= 0.03) text = "解析 NavMesh 数据...";
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
        menu.textContent = "";
        for (const name of ACTION_MENU_NAMES) {
            const opt = document.createElement("option");
            opt.value = name;
            opt.textContent = name;
            menu.appendChild(opt);
        }

        const legend = this.els.propertiesLegend;
        if (!legend) return;
        legend.textContent = "";

        const title = document.createElement("div");
        title.className = "legend-title";
        title.textContent = "路点动作图例";
        legend.appendChild(title);

        const grid = document.createElement("div");
        grid.className = "legend-grid";

        for (const type of ACTION_MENU_TYPES) {
            const item = document.createElement("div");
            item.className = "legend-item";
            const dot = document.createElement("span");
            dot.className = "legend-dot";
            dot.style.backgroundColor = ACTION_COLORS[type] || "#3498db";
            const text = document.createElement("span");
            text.textContent = ACTION_NAMES[type] || "Unknown";
            item.appendChild(dot);
            item.appendChild(text);
            grid.appendChild(item);
        }

        const strictItem = document.createElement("div");
        strictItem.className = "legend-item";
        const strictDot = document.createElement("span");
        strictDot.className = "legend-dot-strict";
        const strictDotInner = document.createElement("span");
        strictDotInner.className = "legend-dot-strict-inner";
        strictDot.appendChild(strictDotInner);
        const strictText = document.createElement("span");
        strictText.textContent = "严格模式";
        strictItem.appendChild(strictDot);
        strictItem.appendChild(strictText);
        grid.appendChild(strictItem);

        legend.appendChild(grid);
    }

    /** Refill the assert-zone combo from the backend zone-id scan, keeping the selection. @returns {void} */
    _populateAssertZoneCombo() {
        const combo = this.els.assertZoneCombo;
        const prev = combo.value;
        combo.textContent = "";
        for (const zoneId of this._availableZoneIds) {
            const opt = document.createElement("option");
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
            this.els.assertSelectedTierLabel.textContent = "未选择";
        }
    }

    /** Refill the waypoint target-tier suggestions from the backend zone-id scan. @returns {void} */
    _populateTargetTierList() {
        const list = this.els.targetTierList;
        list.textContent = "";
        for (const zoneId of this._availableZoneIds) {
            const option = document.createElement("option");
            option.value = zoneId;
            list.appendChild(option);
        }
    }

    /** Refill the A* display-zone combo with the field's base names, keeping the selection. @returns {void} */
    _populateAstarDisplayCombo() {
        if (!this.field) return;
        const names = this.field.displayBaseNames();
        const combo = this.els.astarDisplayZoneCombo;
        const prev = combo.value;
        combo.textContent = "";
        for (const name of names) {
            const opt = document.createElement("option");
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
        combo.textContent = "";
        for (const choice of choices) {
            const opt = document.createElement("option");
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

    /** Keep every diagnostic checkbox aligned with the persisted rendering state. */
    _syncNavDebugControls() {
        for (const [entry, key] of [
            [this.els.chkNavDebugSearch, "search"],
            [this.els.chkNavDebugRerouted, "rerouted"],
            [this.els.chkNavDebugStringPull, "stringPull"],
            [this.els.chkNavDebugAssembled, "assembled"],
            [this.els.chkNavDebugLoopFixed, "loopFixed"],
            [this.els.chkNavDebugSlim, "slim"],
            [this.els.chkNavDebugWidenCorners, "widenCorners"],
            [this.els.chkNavDebugPlanned, "planned"],
            [this.els.chkNavDebugLivePath, "live"],
        ]) {
            entry.checked = Boolean(this.navDebug[key]);
        }
        this.showLivePath = this.navDebug.live;
    }

    /** Attach every DOM event listener (buttons, combos, tabs, canvas, keyboard). @returns {void} */
    _wireEvents() {
        const e = this.els;
        e.btnCopyPath.addEventListener("click", () => this._copyPath());
        e.btnCopyAssert.addEventListener("click", () => this._copyAssert());
        e.assertCopyFormat.addEventListener("change", () => this._syncCopyButtonLabels());
        e.btnPrev.addEventListener("click", () => this._prevZone());
        e.btnNext.addEventListener("click", () => this._nextZone());
        e.btnZoomOut.addEventListener("click", () => this._zoomOut());
        e.btnZoomIn.addEventListener("click", () => this._zoomIn());
        e.btnApplyAction.addEventListener("click", () => this._applyAction());
        e.assertZoneCombo.addEventListener("change", () => this._onAssertZoneChanged());
        e.astarDisplayZoneCombo.addEventListener("change", () => this._onAstarDisplayZoneChanged());
        e.astarZoneCombo.addEventListener("change", () => this._onAstarZoneChanged());
        for (const [entry, key] of [
            [e.chkNavDebugSearch, "search"],
            [e.chkNavDebugRerouted, "rerouted"],
            [e.chkNavDebugStringPull, "stringPull"],
            [e.chkNavDebugAssembled, "assembled"],
            [e.chkNavDebugLoopFixed, "loopFixed"],
            [e.chkNavDebugSlim, "slim"],
            [e.chkNavDebugWidenCorners, "widenCorners"],
            [e.chkNavDebugPlanned, "planned"],
        ]) {
            entry.addEventListener("change", () => {
                this.navDebug[key] = entry.checked;
                localStorage.setItem(`maaend.mapnavigator.debug${key[0].toUpperCase()}${key.slice(1)}`, entry.checked ? "1" : "0");
                this._paint();
            });
        }
        e.chkNavDebugLivePath.addEventListener("change", () => {
            this.showLivePath = e.chkNavDebugLivePath.checked;
            this.navDebug.live = this.showLivePath;
            localStorage.setItem("maaend.mapnavigator.showLivePath", this.showLivePath ? "1" : "0");
            this._paint();
        });
        e.btnClearAstar.addEventListener("click", () => this._onClearAstar());
        e.btnCopyNavmesh.addEventListener("click", () => this._copyNavmesh());
        e.btnAssertLocate.addEventListener("click", () => this._onLocateCurrentPosition("assert"));
        e.btnAstarLocate.addEventListener("click", () => this._onLocateCurrentPosition("astar"));
        e.btnAstarMarkCoord.addEventListener("click", () => this._onAstarMarkCoord());
        for (const entry of [
            e.astarCoordX,
            e.astarCoordY,
        ]) {
            entry.addEventListener("paste", (ev) => this._onAstarCoordPaste(ev));
            entry.addEventListener("keydown", (ev) => {
                if (ev.key === "Enter") this._onAstarMarkCoord();
            });
        }
        e.btnAstarImport.addEventListener("click", () => this.importer.openProjectPicker("astar"));
        e.btnAssertImport.addEventListener("click", () => this.importer.openProjectPicker("assert"));
        e.btnEditReadClipboard.addEventListener("click", () => this.importer.readClipboard());
        e.btnAstarReadClipboard.addEventListener("click", () => this.importer.readClipboard());
        e.btnAssertReadClipboard.addEventListener("click", () => this.importer.readClipboard());
        e.tabRoute.addEventListener("click", () => this._selectModeTab(this._lastRouteMode));
        e.tabEdit.addEventListener("click", () => this._selectModeTab("edit"));
        e.tabAstar.addEventListener("click", () => this._selectModeTab("astar"));
        e.tabAssert.addEventListener("click", () => this._selectModeTab("assert"));
        e.tabLog.addEventListener("click", () => this._selectModeTab("log"));
        e.btnLogImport.addEventListener("click", () => e.logFileInput.click());
        e.btnLogClear.addEventListener("click", () => this._clearLogAnalysis());
        e.logFileInput.addEventListener("change", () => this._importLogFiles(e.logFileInput.files));
        e.logRunFilter.addEventListener("input", () => this._populateLogRunSelect());
        e.logRunSelect.addEventListener("change", () => this._onLogRunChanged());
        e.logRunSelect.addEventListener(
            "wheel",
            (event) => {
                const nextIndex = nextWheelSelectIndex(
                    e.logRunSelect.selectedIndex,
                    e.logRunSelect.options.length,
                    event.deltaY,
                );
                if (nextIndex === e.logRunSelect.selectedIndex) return;
                event.preventDefault();
                e.logRunSelect.selectedIndex = nextIndex;
                this._onLogRunChanged();
            },
            {passive: false},
        );
        e.btnLogDistanceClear.addEventListener("click", () => {
            this._clearLogDistanceSelection();
            setStatus("已清除滑索架测距。", "#10b981");
        });
        e.btnLogPointClear.addEventListener("click", () => {
            this._clearLogInspection();
            setStatus("已清除点位详情。", "#10b981");
        });
        e.btnLogMeasure.addEventListener("click", () => {
            const measuring = this.activeTool !== "log-measure";
            this._setActiveTool(measuring ? "log-measure" : "log-inspect");
            this._renderLogDistance();
            setStatus(
                measuring
                    ? "滑索架测距已启用：请依次选择 A、B 两座滑索架。"
                    : "已退出滑索架测距；单击地图点位可查看具体信息。",
                measuring ? "#3b82f6" : "#10b981",
            );
        });
        for (const [control, key] of [
            [e.logShowAuthored, "showAuthored"],
            [e.logShowWalk, "showWalk"],
            [e.logShowObserved, "showObserved"],
            [e.logShowBaseline, "showBaseline"],
            [e.logShowZipline, "showZipline"],
            [e.logShowSelectedTowers, "showSelectedTowers"],
            [e.logShowRecordedTowers, "showRecordedTowers"],
            [e.logShowEstimates, "showEstimates"],
        ]) {
            control.addEventListener("change", () => {
                this.logLayers[key] = control.checked;
                if (
                    this.logInspectedPoint &&
                    !this._logInspectionCandidates().some((candidate) => candidate.key === this.logInspectedPoint.key)
                ) {
                    this.logInspectedPoint = null;
                    this._renderLogInspection();
                }
                this._paint();
            });
        }
        e.btnClearAssert.addEventListener("click", () => this._deleteSelectedPoint());
        e.btnSelectTier.addEventListener("click", () => this._openTierPicker());
        e.btnSelectAssertTier.addEventListener("click", () => this._openTierPicker());
        e.tierPickerCancel.addEventListener("click", () => {
            e.tierPickerDialog.hidden = true;
        });
        e.btnFitView.addEventListener("click", () => this._fitView());
        e.btnDelPointFloat.addEventListener("click", () => this._deleteSelectedPoint());
        e.toolPan.addEventListener("click", () => this._setActiveTool("pan"));
        e.toolAdd.addEventListener("click", () => this._setActiveTool("add"));
        e.toolSelect.addEventListener("click", () => this._setActiveTool("select"));
        e.toolAstarSingle.addEventListener("click", () => this._setActiveTool("astar-single"));
        e.toolAstarMulti.addEventListener("click", () => this._setActiveTool("astar-multi"));
        e.toolAssertPan.addEventListener("click", () => this._setActiveTool("assert-pan"));
        e.toolAssertEdit.addEventListener("click", () => this._setActiveTool("assert-edit"));

        this._wireWaypointList(e.waypointList);

        const canvas = e.overlayCanvas;
        canvas.addEventListener("mousedown", (ev) => this._onPointerDown(ev));
        window.addEventListener("mousemove", this._pointerMoveBound);
        window.addEventListener("mouseup", this._pointerUpBound);
        canvas.addEventListener("wheel", (ev) => this._onWheel(ev), {passive: false});
        canvas.addEventListener("contextmenu", (ev) => ev.preventDefault());

        document.addEventListener("keydown", (ev) => this._onKeyDown(ev));

        // Holding Alt temporarily swaps to the mode's pan tool; keyup restores the tool.
        this._altSavedTool = null;
        window.addEventListener("keydown", (e) => {
            const target = e.target;
            if (
                target &&
                (target.tagName === "INPUT" ||
                    target.tagName === "SELECT" ||
                    target.tagName === "TEXTAREA" ||
                    target.isContentEditable)
            ) {
                return;
            }
            if (e.key === "Alt" && !this._altSavedTool) {
                e.preventDefault();
                this._altSavedTool = this.activeTool;
                if (this.state.mode === Mode.EDIT) {
                    this._setActiveTool("pan");
                } else if (this.state.mode === Mode.ASSERT) {
                    this._setActiveTool("assert-pan");
                } else if (this.state.mode === Mode.ASTAR) {
                    this._setActiveTool("astar-pan");
                } else if (this.state.mode === Mode.LOG) {
                    this._setActiveTool("log-pan");
                }
            }
        });

        window.addEventListener("keyup", (e) => {
            if (e.key === "Alt" && this._altSavedTool) {
                e.preventDefault();
                const saved = this._altSavedTool;
                this._altSavedTool = null;
                this._setActiveTool(saved);
            }
        });
    }

    /** Track canvas-wrap size changes (ResizeObserver, window resize fallback). @returns {void} */
    _observeResize() {
        if (typeof ResizeObserver === "function") {
            const ro = new ResizeObserver(() => this._resize());
            ro.observe(this.els.canvasWrap);
        } else {
            window.addEventListener("resize", () => this._resize());
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
        if (this.state.mode === Mode.ASTAR || this.state.mode === Mode.ASSERT || this.state.mode === Mode.LOG) {
            return normalizeZoneId(this.els.astarDisplayZoneCombo.value, this._defaultAstarDisplayZone());
        }
        return this.state.currentZone();
    }

    /** @returns {number} zone id parsed from the tier combo's `"id:name"` value, or NaN. */
    _astarZoneId() {
        const raw = this.els.astarZoneCombo.value || "";
        const head = raw.split(":", 1)[0];
        return parseInt(head, 10);
    }

    /** @returns {?number} zone id of the translated tier backing the canvas, else null. */
    _activeDisplayTierId() {
        if (
            (this.state.mode !== Mode.ASTAR && this.state.mode !== Mode.ASSERT && this.state.mode !== Mode.LOG) ||
            !this.field
        )
            return null;
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
        return names.length ? names[0] : "";
    }

    /** @returns {string} the current edit zone if set, else a `map01` base, else the first scanned zone. */
    _defaultAssertZone() {
        const cur = normalizeZoneId(this.state.currentZone());
        if (cur) return cur;
        const preferred = this._availableZoneIds.find((z) => z.toLowerCase().includes("map01"));
        return preferred || this._availableZoneIds[0] || "";
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
            const hint = this.assertLocateHint;
            const [
                x,
                y,
            ] = this._baseToDisplay(hint.x, hint.y);
            displayAssertLocateHint = {
                x,
                y,
                label: hint.label,
                rot: this._headingBaseToDisplay(hint.x, hint.y, hint.rot),
            };
        }
        const displayAstarLocateHints = mode === Mode.ASTAR ? this._astarDisplayHints() : [];
        const displayLogAnalysis = mode === Mode.LOG ? this._logAnalysisForDisplay() : null;

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
                          livePath: this._livePathForDisplay(),
                          diagnostics: this._astarDiagnosticsForDisplay(),
                          debugOptions: this.navDebug,
                          showPlannedPath: this.navDebug.planned,
                      }
                    : null,
            selectionRect: this.selectionRect,
            assertLocateHint: displayAssertLocateHint,
            astarLocateHints: displayAstarLocateHints,
            logAnalysis: displayLogAnalysis,
            offMeshMarks: this._offMeshForMode(mode),
        };
        this.renderer.requestRender(this.camera);
        this.overlay.render(this.camera, vm);
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
        const zoneId = normalizeZoneId(typeof zoneStr === "string" ? zoneStr : "");
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
        if (tierId === null || !this.field)
            return [
                bx,
                by,
            ];
        return this.field.baseToTier(tierId, bx, by);
    }

    /** Parsed author hints, retaining metadata while converting each point into base px. */
    _logAuthoredBaseEntries() {
        const run = this.selectedLogRun;
        if (!run) return [];
        const runZoneId = this._resolveZoneId(run.zone);
        const runGeometry =
            this.field && !Number.isNaN(runZoneId) ? this.field.geometryZoneId(runZoneId) : null;
        const entries = run.authoredPath || (run.authoredPoints || []).map((point) => ({point}));
        const result = [];
        for (const [index, entry] of entries.entries()) {
            if (!entry || !Array.isArray(entry.point)) continue;
            const frame = entry.targetTier || entry.zone || "";
            const zoneId = this._resolveZoneId(frame);
            let point = entry.point;
            if (this.field && !Number.isNaN(zoneId)) {
                if (runGeometry !== null && this.field.geometryZoneId(zoneId) !== runGeometry) continue;
                point = this._pointToBase(zoneId, entry.point[0], entry.point[1]);
            }
            result.push({...entry, index, sourcePoint: entry.point, point});
        }
        return result;
    }

    /** Parsed author hints converted from their own zone/tier frame into base px. */
    _logAuthoredBasePoints() {
        return this._logAuthoredBaseEntries().map((entry) => entry.point);
    }

    /** Geometry/base zone name used by zipline_frames.json (for example map02base). */
    _logGeometryZoneName(run) {
        if (!run || !this.field) return "";
        const zoneId = this._resolveZoneId(run.zone);
        if (Number.isNaN(zoneId)) return "";
        const base = this.field.zoneById(this.field.geometryZoneId(zoneId));
        return base ? base.name : "";
    }

    /** Candidate ZIP marks plus the tower positions observable in this run's existing logs. */
    _logTowerData(run = this.selectedLogRun) {
        if (!run) return {selected: [], recorded: []};
        const archiveGroup = run._archiveGroupKey ? this.logArchiveGroups.get(run._archiveGroupKey) : null;
        const recorded = projectZiplineRecords(
            archiveGroup && archiveGroup.records,
            this.ziplineFrameConfig,
            this._logGeometryZoneName(run),
        );
        const selected = [];
        let labelIndex = 1;
        for (const chain of run.ziplines || []) {
            for (const tower of logZiplineTowers(chain)) {
                let matchingRecord = null;
                let matchingDistance = Infinity;
                let matchingHeightDistance = Infinity;
                let matchingScore = Infinity;
                for (const candidate of recorded) {
                    const distance = Math.hypot(
                        candidate.point[0] - tower.point[0],
                        candidate.point[1] - tower.point[1],
                    );
                    const heightDistance =
                        Number.isFinite(tower.height) && Number.isFinite(candidate.height)
                            ? Math.abs(candidate.height - tower.height)
                            : 0;
                    const score = Math.hypot(distance, heightDistance);
                    if (score < matchingScore) {
                        matchingRecord = candidate;
                        matchingDistance = distance;
                        matchingHeightDistance = heightDistance;
                        matchingScore = score;
                    }
                }
                const matchedRecord =
                    matchingDistance <= 0.75 && matchingHeightDistance <= 0.75 ? matchingRecord : null;
                selected.push({
                    ...tower,
                    measureKey: `selected:${chain.chainIndex}:${tower.index}`,
                    chainIndex: chain.chainIndex,
                    towerIndex: tower.index,
                    label: `索${labelIndex}`,
                    height: matchedRecord ? matchedRecord.height : tower.height,
                    world: matchedRecord ? matchedRecord.world : null,
                    mapId: matchedRecord ? matchedRecord.mapId : "",
                    levelId: matchedRecord ? matchedRecord.levelId : "",
                    templateId: matchedRecord ? matchedRecord.templateId : "",
                });
                labelIndex += 1;
            }
        }
        return {selected, recorded};
    }

    /** Resolve the current A/B keys against freshly projected towers. */
    _logDistanceMeasurement(towerData = this._logTowerData()) {
        const byKey = new Map(
            [
                ...(towerData.selected || []),
                ...(towerData.recorded || []),
            ].map((tower) => [
                tower.measureKey,
                tower,
            ]),
        );
        const towers = this.logDistanceSelection
            .map((key) => byKey.get(key))
            .filter(Boolean)
            .slice(0, 2);
        if (towers.length !== this.logDistanceSelection.length) {
            this.logDistanceSelection = towers.map((tower) => tower.measureKey);
        }
        return {
            towers,
            result: towers.length === 2 ? measureZiplinePair(towers[0], towers[1], this.ziplineFrameConfig) : null,
        };
    }

    /** Visible log points that the default inspection tool can select. */
    _logInspectionCandidates() {
        const run = this.selectedLogRun;
        if (!run) return [];
        const visibilityKey = [
            this.logLayers.showAuthored,
            this.logLayers.showWalk,
            this.logLayers.showObserved,
            this.logLayers.showBaseline,
            this.logLayers.showZipline,
            this.logLayers.showSelectedTowers,
            this.logLayers.showRecordedTowers,
        ].join(":");
        const cached = this._logInspectionCandidateCache;
        if (cached && cached.run === run && cached.field === this.field && cached.visibilityKey === visibilityKey) {
            return cached.candidates;
        }
        const candidates = [];
        const pointText = (point) => `[${point[0].toFixed(2)}, ${point[1].toFixed(2)}]`;
        const numberText = (value) => (Number.isFinite(value) ? value.toFixed(2) : "未知");
        const add = (candidate) => {
            if (
                candidate &&
                Array.isArray(candidate.point) &&
                candidate.point.length >= 2 &&
                candidate.point.slice(0, 2).every(Number.isFinite)
            ) {
                candidates.push(candidate);
            }
        };
        const addTower = (tower, source) => {
            const selected = source === "selected";
            const details = [
                ["来源", selected ? "本次运行选择" : "ZIP 快照候选"],
                ["底图坐标", pointText(tower.point)],
                ["导航高度", numberText(tower.height)],
            ];
            if (selected) {
                details.splice(
                    1,
                    0,
                    ["执行状态", tower.confirmed ? "日志已确认经过" : "日志未确认经过"],
                    ["链 / 序号", `${tower.chainIndex + 1} / ${tower.towerIndex + 1}`],
                );
            }
            if (Array.isArray(tower.world) && tower.world.slice(0, 3).every(Number.isFinite)) {
                details.push([
                    "世界坐标",
                    `[${tower.world[0].toFixed(2)}, ${tower.world[1].toFixed(2)}, ${tower.world[2].toFixed(2)}] m`,
                ]);
            }
            details.push(["模板", tower.templateId || "未知"], ["Level", tower.levelId || "未知"]);
            add({
                key: `tower:${tower.measureKey}`,
                kind: "tower",
                point: tower.point,
                title: selected ? `${tower.label || "滑索架"} · 本次选择` : "ZIP 候选滑索架",
                color: selected ? (tower.confirmed ? "#22d3ee" : "#f59e0b") : "#a78bfa",
                details,
            });
        };

        const towers = this._logTowerData(run);
        if (this.logLayers.showSelectedTowers) {
            for (const tower of towers.selected) addTower(tower, "selected");
        }
        if (this.logLayers.showRecordedTowers) {
            for (const tower of towers.recorded) addTower(tower, "recorded");
        }

        if (this.logLayers.showAuthored) {
            for (const entry of this._logAuthoredBaseEntries()) {
                const frame = entry.targetTier || entry.zone || run.zone || "当前底图";
                const details = [
                    ["来源", "作者路径"],
                    ["序号", String(entry.index + 1)],
                    ["动作", entry.action || "RUN"],
                    ["坐标系", frame],
                ];
                if (frame !== "当前底图") details.push(["原始坐标", pointText(entry.sourcePoint)]);
                details.push(["底图坐标", pointText(entry.point)]);
                add({
                    key: `authored:${entry.index}`,
                    point: entry.point,
                    title: `作者路径点 ${entry.index + 1}`,
                    color: "#f8fafc",
                    details,
                });
            }
        }

        if (this.logLayers.showZipline) {
            for (const [chainIndex, chain] of (run.ziplines || []).entries()) {
                const geometry = logZiplineGeometry(chain);
                for (const [hopIndex, segment] of geometry.actual.entries()) {
                    for (const [endpoint, point] of [
                        ["起点", segment.from],
                        ["终点", segment.to],
                    ]) {
                        add({
                            key: `zipline:${chainIndex}:${hopIndex}:${endpoint}`,
                            point,
                            title: `实际滑索端点 · ${endpoint}`,
                            color: segment.landed ? "#22d3ee" : "#f59e0b",
                            details: [
                                ["来源", "运行日志"],
                                ["链 / 跳", `${chainIndex + 1} / ${hopIndex + 1}`],
                                ["端点", endpoint],
                                ["落地状态", segment.landed ? "已确认落地" : "未确认落地"],
                                ["底图坐标", pointText(point)],
                            ],
                        });
                    }
                }
            }
        }

        const addWalkPoints = (decision, visible, title, color, decisionText) => {
            if (!visible) return;
            for (const [walkIndex, walk] of (run.walks || [])
                .filter((item) => item.decision === decision)
                .entries()) {
                for (const [pointIndex, point] of (walk.points || []).entries()) {
                    add({
                        key: `${decision}:${walkIndex}:${pointIndex}`,
                        point,
                        title,
                        color,
                        details: [
                            ["来源", "NavMesh 规划"],
                            ["决策", decisionText],
                            ["段 / 点", `${walkIndex + 1} / ${pointIndex + 1}`],
                            ["成本", numberText(walk.cost)],
                            ["原因", walk.reason || "未记录"],
                            ["底图坐标", pointText(point)],
                        ],
                    });
                }
            }
        };
        addWalkPoints("walk", this.logLayers.showWalk, "采用的步行规划点", "#ff3b9d", "采用步行");
        addWalkPoints(
            "baseline",
            this.logLayers.showBaseline,
            "未采用的步行基线点",
            "#f59e0b",
            "滑索方案取代步行",
        );

        if (this.logLayers.showObserved) {
            for (const [walkIndex, points] of (run.observedWalks || []).entries()) {
                for (const [pointIndex, point] of points.entries()) {
                    add({
                        key: `observed:${walkIndex}:${pointIndex}`,
                        point,
                        title: "实测轨迹点",
                        color: "#22c55e",
                        details: [
                            ["来源", "MapLocator 实测"],
                            ["轨迹段 / 点", `${walkIndex + 1} / ${pointIndex + 1}`],
                            ["底图坐标", pointText(point)],
                        ],
                    });
                }
            }
        }
        this._logInspectionCandidateCache = {run, field: this.field, visibilityKey, candidates};
        return candidates;
    }

    /** Nearest visible point inside the click radius. */
    _hitLogPoint(candidates, canvasX, canvasY, radius = LOG_POINT_HIT_RADIUS) {
        let hit = null;
        let hitDistance = radius;
        for (const candidate of candidates) {
            const display = this._baseToDisplay(candidate.point[0], candidate.point[1]);
            const canvas = this.camera.worldToCanvas(display[0], display[1]);
            const distance = Math.hypot(canvas[0] - canvasX, canvas[1] - canvasY);
            if (distance < hitDistance) {
                hit = candidate;
                hitDistance = distance;
            }
        }
        return hit;
    }

    /** Show a hand only when the current log tool can act on the hovered point. */
    _updateLogHoverCursor(canvasX, canvasY) {
        if (this.state.mode !== Mode.LOG || this.isPanning) return;
        const canvas = this.els.overlayCanvas;
        if (this.activeTool === "log-inspect") {
            const hit = this._hitLogPoint(this._logInspectionCandidates(), canvasX, canvasY);
            canvas.style.cursor = hit ? "pointer" : "default";
        } else if (this.activeTool === "log-measure") {
            const towers = this._logInspectionCandidates().filter((candidate) => candidate.kind === "tower");
            const hit = this._hitLogPoint(towers, canvasX, canvasY, LOG_TOWER_HIT_RADIUS);
            canvas.style.cursor = hit ? "pointer" : "crosshair";
        }
    }

    /** Show the floating log result panel only while one of its contextual cards is visible. */
    _syncLogContextPanel() {
        const e = this.els;
        e.logContextPanel.hidden =
            this.state.mode !== Mode.LOG || (e.logInspectionBox.hidden && e.logDistanceBox.hidden);
    }

    /** Render the selected read-only point metadata. */
    _renderLogInspection() {
        const e = this.els;
        const host = e.logPointSummary;
        host.textContent = "";
        const visible = !!this.selectedLogRun && !!this.logInspectedPoint;
        e.logInspectionBox.hidden = !visible;
        e.btnLogPointClear.disabled = !visible;
        this._syncLogContextPanel();
        if (!visible) {
            return;
        }

        const selected = document.createElement("div");
        selected.className = "log-inspection-selected";
        selected.style.borderLeftColor = this.logInspectedPoint.color || "#22d3ee";
        const title = document.createElement("div");
        title.className = "log-distance-point-title";
        title.textContent = this.logInspectedPoint.title || "点位详情";
        const details = document.createElement("dl");
        details.className = "log-point-detail";
        for (const [label, value] of this.logInspectedPoint.details || []) {
            const term = document.createElement("dt");
            term.textContent = label;
            const description = document.createElement("dd");
            description.textContent = value;
            details.append(term, description);
        }
        selected.append(title, details);
        host.appendChild(selected);
        this._syncLogContextPanel();
    }

    /** Clear point inspection without changing the A/B measurement. */
    _clearLogInspection() {
        this.logInspectedPoint = null;
        this._renderLogInspection();
        this._paint();
    }

    /** Render selected tower coordinates, the distance formula, and the limited geometry verdict. */
    _renderLogDistance() {
        const e = this.els;
        const host = e.logDistanceSummary;
        host.textContent = "";
        const measurement = this._logDistanceMeasurement();
        const visible =
            this.activeTool === "log-measure" ||
            (this.activeTool === "log-pan" && this._altSavedTool === "log-measure");
        e.logDistanceBox.hidden = !visible;
        e.btnLogDistanceClear.disabled = measurement.towers.length === 0;
        this._syncLogContextPanel();

        if (!this.selectedLogRun) {
            host.textContent = this.logRuns.length
                ? "当前筛选没有匹配记录。"
                : "导入日志后，可点击地图右上角的测距工具选择两座滑索架。";
            return;
        }
        if (!measurement.towers.length) {
            host.textContent =
                this.activeTool === "log-measure"
                    ? "测距已启用，请单击紫色菱形或编号滑索架选择 A 点；拖动仍可平移"
                    : "点击地图右上角的测距工具后，再依次选择 A、B 两座滑索架";
            return;
        }

        for (const [
            index,
            tower,
        ] of measurement.towers.entries()) {
            const marker = index === 0 ? "A" : "B";
            const row = document.createElement("div");
            row.className = "log-distance-point";
            const title = document.createElement("div");
            title.className = "log-distance-point-title";
            const source =
                tower.label || (String(tower.measureKey || "").startsWith("record:") ? "ZIP 候选架" : "滑索架");
            title.textContent = `${marker} · ${source}`;
            const base = document.createElement("div");
            base.className = "log-distance-coordinates";
            const height = Number.isFinite(tower.height) ? `，导航高度 ${tower.height.toFixed(2)}` : "";
            base.textContent = `底图 [${tower.point[0].toFixed(2)}, ${tower.point[1].toFixed(2)}]${height}`;
            row.append(title, base);
            if (Array.isArray(tower.world) && tower.world.slice(0, 3).every(Number.isFinite)) {
                const world = document.createElement("div");
                world.className = "log-distance-coordinates";
                world.textContent = `世界 [${tower.world[0].toFixed(2)}, ${tower.world[1].toFixed(2)}, ${tower.world[2].toFixed(2)}] m`;
                row.appendChild(world);
            }
            const identity = document.createElement("div");
            identity.className = "log-distance-identity";
            identity.textContent = `类型 ${tower.templateId || "未知"} · level ${tower.levelId || "未知"}`;
            row.appendChild(identity);
            host.appendChild(row);
        }

        if (measurement.towers.length === 1) {
            const hint = document.createElement("div");
            hint.className = "log-distance-note";
            hint.textContent =
                this.activeTool === "log-measure"
                    ? "已选择 A 点；请再点一座滑索架作为 B 点。再次点 A 可取消。"
                    : "已保留 A 点；再次开启右上角测距工具后可继续选择 B 点。";
            host.appendChild(hint);
            return;
        }

        const result = measurement.result;
        const resultBox = document.createElement("div");
        resultBox.className = "log-distance-result";
        if (Number.isFinite(result.minimumWorldDistance) && Number.isFinite(result.maximumWorldDistance)) {
            const primary = document.createElement("div");
            primary.className = "log-distance-primary";
            primary.textContent = `可能中心跨度 ${result.minimumWorldDistance.toFixed(2)}～${result.maximumWorldDistance.toFixed(2)} m`;
            const formula = document.createElement("div");
            formula.className = "log-distance-formula";
            formula.textContent = `原始锚点三维距离 ${result.worldDistance.toFixed(2)} m · X/Z 轴合计不确定性 ±${result.uncertaintyX.toFixed(2)} / ±${result.uncertaintyZ.toFixed(2)} m`;
            const components = document.createElement("div");
            components.className = "log-distance-components";
            components.textContent = `可能水平距离 ${result.minimumHorizontalDistance.toFixed(2)}～${result.maximumHorizontalDistance.toFixed(2)} m · 高差 |ΔY| ${result.heightDelta.toFixed(2)} m（不偏移）`;
            resultBox.append(primary, formula, components);
        } else {
            const missing = document.createElement("div");
            missing.className = "log-distance-primary";
            missing.textContent = "缺少世界坐标，无法计算三维距离";
            resultBox.appendChild(missing);
        }
        if (Number.isFinite(result.baseDistance)) {
            const baseDistance = document.createElement("div");
            baseDistance.className = "log-distance-components";
            baseDistance.textContent = `底图直线距离 ${result.baseDistance.toFixed(2)} px`;
            resultBox.appendChild(baseDistance);
        }

        const verdict = document.createElement("div");
        const verdictClass =
            result.geometryConnected === true
                ? "connected"
                : result.geometryConnected === false
                  ? "disconnected"
                  : "unknown";
        verdict.className = `log-distance-verdict ${verdictClass}`;
        const verdictTitle =
            result.geometryConnected === true
                ? "几何上可连接"
                : result.geometryConnected === false
                  ? "几何上不可连接"
                  : "无法判断几何连接";
        verdict.textContent = `${verdictTitle}：${result.geometryReason}`;
        resultBox.appendChild(verdict);
        host.appendChild(resultBox);

        const warning = document.createElement("div");
        warning.className = "log-distance-note";
        warning.textContent = "几何满足不代表实际可用；供电、可达性、禁用边和成本规划仍以运行时结果为准。";
        host.appendChild(warning);
    }

    /** Clear the current A/B tower selection without changing the imported run. */
    _clearLogDistanceSelection() {
        this.logDistanceSelection = [];
        this._renderLogDistance();
        this._paint();
    }

    /** Build the display-frame geometry consumed by Overlay's read-only log layer. */
    _logAnalysisForDisplay() {
        const run = this.selectedLogRun;
        if (!run) return null;
        const displayPoint = (point) => this._baseToDisplay(point[0], point[1]);
        const displayPolyline = (points) => (points || []).map(displayPoint);
        const ziplines = [];
        const estimates = [];
        for (const chain of run.ziplines || []) {
            const geometry = logZiplineGeometry(chain);
            for (const segment of geometry.actual) {
                ziplines.push({...segment, from: displayPoint(segment.from), to: displayPoint(segment.to)});
            }
            for (const segment of geometry.estimated) {
                estimates.push({...segment, from: displayPoint(segment.from), to: displayPoint(segment.to)});
            }
        }
        const towers = this._logTowerData(run);
        const measurement = this._logDistanceMeasurement(towers);
        return {
            ...this.logLayers,
            authored: displayPolyline(this._logAuthoredBasePoints()),
            walks: (run.walks || [])
                .filter((walk) => walk.decision === "walk")
                .map((walk) => displayPolyline(walk.points)),
            observed: (run.observedWalks || []).map(displayPolyline),
            baselines: (run.walks || [])
                .filter((walk) => walk.decision === "baseline")
                .map((walk) => displayPolyline(walk.points)),
            ziplines,
            estimates,
            selectedTowers: towers.selected.map((tower) => ({
                ...tower,
                point: displayPoint(tower.point),
            })),
            recordedTowers: towers.recorded.map((tower) => ({
                ...tower,
                point: displayPoint(tower.point),
            })),
            measurement: {
                towers: measurement.towers.map((tower, index) => ({
                    ...tower,
                    marker: index === 0 ? "A" : "B",
                    point: displayPoint(tower.point),
                })),
                result: measurement.result,
            },
            inspection: this.logInspectedPoint
                ? {...this.logInspectedPoint, point: displayPoint(this.logInspectedPoint.point)}
                : null,
        };
    }

    /** All displayed log coordinates used by fit-view. */
    _logDisplayPoints() {
        const log = this._logAnalysisForDisplay();
        if (!log) return [];
        const points = [...log.authored];
        for (const path of [...log.walks, ...log.observed, ...log.baselines]) points.push(...path);
        for (const segment of [...log.ziplines, ...log.estimates]) points.push(segment.from, segment.to);
        for (const tower of log.selectedTowers || []) points.push(tower.point);
        return points;
    }

    /** A* preview markers projected base → display frame, including their heading vector. */
    _astarDisplayHints() {
        return this.astarLocateHints.map((hint) => {
            const [
                x,
                y,
            ] = this._baseToDisplay(hint.x, hint.y);
            return {x, y, label: hint.label, rot: this._headingBaseToDisplay(hint.x, hint.y, hint.rot)};
        });
    }

    /** MapLocator zone-frame heading → base-frame heading. @returns {?number} */
    _headingToBase(zoneId, x, y, rot) {
        return transformHeading(rot, (px, py) => this._pointToBase(zoneId, px, py), x, y);
    }

    /** Base-frame heading → current display-frame heading. @returns {?number} */
    _headingBaseToDisplay(x, y, rot) {
        return transformHeading(rot, (px, py) => this._baseToDisplay(px, py), x, y);
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
        return [
            Math.min(...xs) - pad,
            Math.min(...ys) - pad,
            Math.max(...xs) + pad,
            Math.max(...ys) + pad,
        ];
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
            const [
                bx,
                by,
            ] = this._pointToBase(pointZoneId, point.x, point.y);
            const [
                dx,
                dy,
            ] = this._baseToDisplay(bx, by);
            out.push({...point, x: dx, y: dy});
        }
        return out;
    }

    /** Consume one real locator fix and append it to the measured base-frame trajectory. */
    _onLivePosition(fix) {
        if (this.positionReadout) this.positionReadout.update(fix);
        if (!this.field || !fix) return;

        const zoneId = this._resolveZoneId(fix.zone);
        const displayZoneId = this._astarZoneId();
        if (Number.isNaN(zoneId) || Number.isNaN(displayZoneId)) return;
        if (this.field.geometryZoneId(zoneId) !== this.field.geometryZoneId(displayZoneId)) {
            this._clearLivePath();
            if (this.state.mode === Mode.ASTAR) this._paint();
            return;
        }

        const [x, y] = this._pointToBase(zoneId, fix.x, fix.y);
        const rot = this._headingToBase(zoneId, fix.x, fix.y, fix.rot);
        this.livePositionBase = {x, y, rot};
        const last = this.livePathBase[this.livePathBase.length - 1];
        if (!last || Math.hypot(last.x - x, last.y - y) >= 1) {
            this.livePathBase.push({x, y, rot});
        }
        if (this.state.mode === Mode.ASTAR) this._paint();
    }

    /** Clear measured live-path state without affecting the planned preview. */
    _clearLivePath() {
        this.livePathBase = [];
        this.livePositionBase = null;
    }

    /** Project measured base-frame points into the current A* display frame. */
    _livePathForDisplay() {
        if (!this.showLivePath || !this.field || this.state.mode !== Mode.ASTAR) return null;
        return {
            points: this.livePathBase.map((point) => {
                const [x, y] = this._baseToDisplay(point.x, point.y);
                return {x, y, rot: this._headingBaseToDisplay(point.x, point.y, point.rot)};
            }),
            current: this.livePositionBase
                ? (() => {
                      const [x, y] = this._baseToDisplay(this.livePositionBase.x, this.livePositionBase.y);
                      return {
                          x,
                          y,
                          rot: this._headingBaseToDisplay(
                              this.livePositionBase.x,
                              this.livePositionBase.y,
                              this.livePositionBase.rot,
                          ),
                      };
                  })()
                : null,
        };
    }

    /** Project the real per-leg navmesh diagnostics into the current display frame. */
    _astarDiagnosticsForDisplay() {
        const project = (points) => (points || []).map(([x, y]) => this._baseToDisplay(x, y));
        return this.astarDiagnostics.map((diag) => ({
            ...diag,
            astar_cells: project(diag.astar_cells),
            rerouted_points: project(diag.rerouted_points),
            string_pull_points: project(diag.string_pull_points),
            assembled_points: project(diag.assembled_points),
            loop_fixed_points: project(diag.loop_fixed_points),
            slim_points: project(diag.slim_points),
            widened_points: project(diag.widened_points),
            planned_points: project(diag.planned_points),
            start: project([diag.start])[0],
            goal: project([diag.goal])[0],
        }));
    }

    /**
     * A point's own-zone px → base px.
     * @param {number} zoneId @param {number} x @param {number} y
     * @returns {[number, number]}
     */
    _pointToBase(zoneId, x, y) {
        if (!this.field || Number.isNaN(zoneId) || !this.field.isTier(zoneId))
            return [
                x,
                y,
            ];
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
            out.push({
                idx: i,
                base: this._pointToBase(zoneId, point.x, point.y),
                own: [
                    point.x,
                    point.y,
                ],
                zoneId,
            });
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
        const sig = targets.map((t) => `${t.zoneId}:${t.own[0]},${t.own[1]}`).join("|");
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
        const [
            x0,
            y0,
            x1,
            y1,
        ] = this.assertRectWorld;
        const left = Math.min(x0, x1);
        const top = Math.min(y0, y1);
        return [
            left,
            top,
            Math.abs(x1 - x0),
            Math.abs(y1 - y0),
        ];
    }

    /** Rounded assert target for status/export (tk `_current_assert_target`). @returns {?number[]} */
    _currentAssertTarget() {
        const display = this._assertTargetForDisplay();
        if (!display) return null;
        return [
            bankerRound2(display[0]),
            bankerRound2(display[1]),
            bankerRound2(display[2]),
            bankerRound2(display[3]),
        ];
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
            this.renderer.setBasemap(img, {width: img.naturalWidth, height: img.naturalHeight});
            this.renderer.setBasemapVisible(true);
            this._basemapDims = {width: img.naturalWidth, height: img.naturalHeight};
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
                this.renderer.setMesh(buf, {width: dims.width, height: dims.height});
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
        const logPoints = mode === Mode.LOG ? this._logDisplayPoints() : [];
        if (mode === Mode.ASSERT && assertTarget) {
            minX = assertTarget[0];
            maxX = assertTarget[0] + assertTarget[2];
            minY = assertTarget[1];
            maxY = assertTarget[1] + assertTarget[3];
        } else if (mode === Mode.ASSERT && this.assertLocateHint) {
            const pt = this._baseToDisplay(this.assertLocateHint.x, this.assertLocateHint.y);
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
            [
                minX,
                minY,
                maxX,
                maxY,
            ] = this._astarHintsBbox();
        } else if (mode === Mode.ASTAR && this.field && !Number.isNaN(this._astarZoneId()) && !dims) {
            const bounds = this.field.bounds(this._astarZoneId());
            if (bounds)
                [
                    minX,
                    minY,
                    maxX,
                    maxY,
                ] = bounds;
        } else if (mode === Mode.LOG && logPoints.length) {
            const xs = logPoints.map((point) => point[0]);
            const ys = logPoints.map((point) => point[1]);
            minX = Math.min(...xs);
            maxX = Math.max(...xs);
            minY = Math.min(...ys);
            maxY = Math.max(...ys);
        } else if (points.length) {
            const xs = points.map((p) => p.x);
            const ys = points.map((p) => p.y);
            minX = Math.min(...xs);
            maxX = Math.max(...xs);
            minY = Math.min(...ys);
            maxY = Math.max(...ys);
        }

        this.camera.fitView(
            [
                minX,
                minY,
                maxX,
                maxY,
            ],
            this._cssW,
            this._cssH,
            60,
            LEFT_PANEL_FIT_OFFSET,
        );
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
            [
                Math.min(...xs),
                Math.min(...ys),
                Math.max(...xs),
                Math.max(...ys),
            ],
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
        return [
            e.clientX - rect.left,
            e.clientY - rect.top,
        ];
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
            const [
                x,
                y,
            ] = this._evtXY(e);
            this.isPanning = true;
            this.dragStartX = x;
            this.dragStartY = y;
            this.els.overlayCanvas.style.cursor = "grabbing";
            return;
        }
        if (e.button !== 0) return;
        const [
            x,
            y,
        ] = this._evtXY(e);

        if (
            this.state.mode === Mode.LOG &&
            ["log-inspect", "log-measure", "log-pan"].includes(this.activeTool)
        ) {
            this.isDragging = false;
            this.isPanCandidate = true;
            this.isPanning = false;
            this.isBoxSelecting = false;
            this.isAssertSelecting = false;
            this.pointerDownX = x;
            this.pointerDownY = y;
            return;
        }

        if (
            this.activeTool === "pan" ||
            this.activeTool === "assert-pan" ||
            this.activeTool === "astar-pan"
        ) {
            this.isPanning = true;
            this.dragStartX = x;
            this.dragStartY = y;
            this.els.overlayCanvas.style.cursor = "grabbing";
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
            if (this.activeTool === "assert-edit") {
                const zoneId = this._displayZoneId();
                if (!zoneId) {
                    setStatus("请先在 Assert 模式下选择地图。", "#ef4444");
                    return;
                }
                this.isAssertSelecting = true;
                this.assertLocateHint = null;
                this.isDragging = false;
                this.isPanCandidate = false;
                this.isPanning = false;
                this.isBoxSelecting = false;
                const [
                    wx,
                    wy,
                ] = this.camera.canvasToWorld(x, y);
                this.assertStartWorldX = wx;
                this.assertStartWorldY = wy;
                this.assertRectWorld = [
                    wx,
                    wy,
                    wx,
                    wy,
                ];
                this._paint();
                return;
            }
        }

        if (this.state.mode === Mode.EDIT && (this.activeTool === "add" || this.activeTool === "select")) {
            const isSelectTool = this.activeTool === "select";
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
        const [
            x,
            y,
        ] = this._evtXY(e);

        if (this.state.mode === Mode.LOG && !this.isPanning && !this.isPanCandidate) {
            this._updateLogHoverCursor(x, y);
        }

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
            this.els.overlayCanvas.style.cursor = "grabbing";
            return;
        }
        if (this.state.mode === Mode.ASTAR) return;

        if (this.state.mode === Mode.ASSERT) {
            if (!this.isAssertSelecting) return;
            const [
                wx,
                wy,
            ] = this.camera.canvasToWorld(x, y);
            this.assertRectWorld = [
                this.assertStartWorldX,
                this.assertStartWorldY,
                wx,
                wy,
            ];
            this._paint();
            return;
        }

        if (this.isBoxSelecting) {
            this.selectionRect = {x0: this.boxStartX, y0: this.boxStartY, x1: x, y1: y};
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
            const [
                wx,
                wy,
            ] = this.camera.canvasToWorld(x, y);
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
        const [
            x,
            y,
        ] = this._evtXY(e);

        if (this.isPanning) {
            this.isPanning = false;
            this._setActiveTool(this.activeTool);
            if (this.state.mode === Mode.LOG) this._updateLogHoverCursor(x, y);
            this._paint();
            return;
        }

        if (this.state.mode === Mode.LOG) {
            if (this.isPanCandidate) {
                this.isPanCandidate = false;
                if (this.activeTool === "log-measure") this._handleLogMeasureClick(x, y);
                else if (this.activeTool === "log-inspect") this._handleLogInspectClick(x, y);
            }
            return;
        }

        if (this.state.mode === Mode.ASTAR) {
            if (this.isPanCandidate) {
                this.isPanCandidate = false;
                if (this.activeTool !== "astar-pan") {
                    this._handleAstarClick(x, y);
                }
            }
            return;
        }

        if (this.state.mode === Mode.ASSERT) {
            if (!this.isAssertSelecting) return;
            const [
                wx,
                wy,
            ] = this.camera.canvasToWorld(x, y);
            this.assertRectWorld = [
                this.assertStartWorldX,
                this.assertStartWorldY,
                wx,
                wy,
            ];
            this.isAssertSelecting = false;
            const target = this._currentAssertTarget();
            if (target) {
                setStatus(
                    `Assert 区域已更新: zone=${this._displayZoneId()} target=[${target[0].toFixed(1)}, ${target[1].toFixed(
                        1,
                    )}, ${target[2].toFixed(1)}, ${target[3].toFixed(1)}]`,
                    "#10b981",
                );
            }
            if (this.navtest) this.navtest.routeChanged();
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
                const indices = this.state.collectIndicesInRect(
                    this._worldToCanvasFn(),
                    this.boxStartX,
                    this.boxStartY,
                    x,
                    y,
                );
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
            if (this.state.mode === Mode.EDIT && this.activeTool !== "add") {
                this.state.clearSelection();
                this._syncActionControls();
                this._paint();
                return;
            }
            this.state.clearSelection();
            const [
                wx,
                wy,
            ] = this.camera.canvasToWorld(x, y);
            this.state.editInsertPoint(this._actionName(), this._strict(), this._required(), wx, wy);
            this._resetPropertyControls();
            this._afterStructureChanged();
            return;
        }

        this.isDragging = false;
    }

    /** Towers currently visible and therefore eligible for measurement. */
    _visibleLogTowers(towerData = this._logTowerData()) {
        return [
            ...(this.logLayers.showSelectedTowers ? towerData.selected : []),
            ...(this.logLayers.showRecordedTowers ? towerData.recorded : []),
        ];
    }

    /** Nearest visible tower inside the dedicated tower hit radius. */
    _hitLogTower(canvasX, canvasY, towers) {
        return this._hitLogPoint(towers, canvasX, canvasY, LOG_TOWER_HIT_RADIUS);
    }

    /** Inspect the nearest visible log point without changing measurement state. */
    _handleLogInspectClick(canvasX, canvasY) {
        if (!this.selectedLogRun) {
            setStatus("请先导入并选择一条导航运行记录。", "#f59e0b");
            return;
        }
        const hit = this._hitLogPoint(this._logInspectionCandidates(), canvasX, canvasY);
        if (!hit) {
            this.logInspectedPoint = null;
            this._renderLogInspection();
            this._paint();
            setStatus("没有点中可查看的点位；已清除点位详情，拖动可平移地图。", "#f59e0b");
            return;
        }
        this.logInspectedPoint = hit;
        this._renderLogInspection();
        this._paint();
        setStatus(`正在查看：${hit.title}。`, hit.color || "#3b82f6");
    }

    /** Select the nearest visible tower for an A/B world-span measurement. */
    _handleLogMeasureClick(canvasX, canvasY) {
        if (!this.selectedLogRun) {
            setStatus("请先导入并选择一条导航运行记录。", "#f59e0b");
            return;
        }
        const towers = this._logTowerData();
        const hit = this._hitLogTower(canvasX, canvasY, this._visibleLogTowers(towers));
        if (!hit) {
            setStatus("没有点中滑索架；测距工具只选择紫色菱形或编号圆点，拖动可平移地图。", "#f59e0b");
            return;
        }

        this.logDistanceSelection = nextZiplineMeasurementSelection(this.logDistanceSelection, hit.measureKey);
        const measurement = this._logDistanceMeasurement(towers);
        if (!measurement.towers.length) {
            setStatus("已清除滑索架测距。", "#10b981");
        } else if (measurement.towers.length === 1) {
            setStatus("已选择 A 点；请再点一座滑索架作为 B 点。", "#3b82f6");
        } else if (
            Number.isFinite(measurement.result && measurement.result.minimumWorldDistance) &&
            Number.isFinite(measurement.result && measurement.result.maximumWorldDistance)
        ) {
            setStatus(
                `滑索架可能中心跨度：${measurement.result.minimumWorldDistance.toFixed(2)}～${measurement.result.maximumWorldDistance.toFixed(2)} m。`,
                "#10b981",
            );
        } else {
            setStatus("已选择 A/B，但其中一座缺少世界坐标，只能显示底图距离。", "#f59e0b");
        }
        this._renderLogDistance();
        this._paint();
    }

    /** Wheel input: zoom the map at the cursor. @param {WheelEvent} e @returns {void} */
    _onWheel(e) {
        e.preventDefault();
        const [
            x,
            y,
        ] = this._evtXY(e);
        const deltaScale =
            e.deltaMode === WheelEvent.DOM_DELTA_LINE ? 16 : e.deltaMode === WheelEvent.DOM_DELTA_PAGE ? this._cssH : 1;
        const factor = Math.max(0.5, Math.min(2.0, Math.exp(-e.deltaY * deltaScale * 0.0015)));
        this.camera.zoomAt(x, y, factor);
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
            setStatus("navmesh 尚未就绪。", "#ef4444");
            return;
        }
        const [
            wx,
            wy,
        ] = this.camera.canvasToWorld(x, y);

        const importedRoute =
            this.astarPoints.length === 0
                ? completeAstarImportWithStart(
                      [
                          wx,
                          wy,
                      ],
                      this.astarPendingTargets,
                      this.astarPendingDecks,
                      (bx, by) => this._baseToDisplay(bx, by),
                  )
                : null;
        if (importedRoute) {
            const importedCount = this.astarPendingTargets.length;
            this.astarPoints = importedRoute.points;
            this.astarDecks = importedRoute.decks;
            this.astarRoute = null;
            this.astarLocateHints = [];
            this.astarPendingTargets = [];
            this.astarPendingDecks = [];
            this.hintDeck = null;
            setStatus(`正在从手动起点规划经过 ${importedCount} 个导入点...`, "#eab308");
            this._calculateAstarPreview();
            this._astarRouteChanged();
            this._paint();
            return;
        }

        if (this.activeTool === "astar-single") {
            if (this.astarPoints.length === 0 || this.astarPoints.length >= 2) {
                this.astarPoints = [
                    [
                        wx,
                        wy,
                    ],
                ];
                this.astarDecks = [null];
                this.astarRoute = null;
                setStatus(`A* 起点: [${wx.toFixed(1)}, ${wy.toFixed(1)}]，再点击终点。`, "#3b82f6");
                // 探针/路线的同步开头会清掉上一轮的离网徽标, 所以先调它们、再 _paint(),
                // 免得这一帧还画着上一条路线的警示环。
                this._probeLoneAstarPoint();
                this._astarRouteChanged();
                this._paint();
                return;
            }
            this.astarPoints.push([
                wx,
                wy,
            ]);
            this.astarDecks.push(null);
            setStatus("正在计算 A* 路径...", "#eab308");
            this._calculateAstarPreview();
            this._astarRouteChanged();
            this._paint();
        } else {
            this.astarPoints.push([
                wx,
                wy,
            ]);
            this.astarDecks.push(null);
            if (this.astarPoints.length < 2) {
                setStatus("已设置 A* 起点，请继续点击后续路点以串联多段路径。", "#3b82f6");
                this._probeLoneAstarPoint();
            } else {
                setStatus(`正在计算第 ${this.astarPoints.length - 1} 段 A* 路径...`, "#eab308");
                this._calculateAstarPreview();
            }
            this._astarRouteChanged();
            this._paint();
        }
    }

    /**
     * 问最后一个预览点底下压着几张可走面,并把结果铺进侧栏。小地图是二维的,同一个坐标可能
     * 同时是走廊、天桥和屋顶;不声明的话寻路先够到哪张停哪张。
     * @returns {Promise<void>}
     */
    async _refreshDeckProbe() {
        const token = ++this._deckToken;
        const index = this.astarPoints.length >= 1 ? this.astarPoints.length - 1 : -1;
        const point =
            index >= 0
                ? this.astarPoints[index]
                : this.astarLastHint
                  ? [
                        this.astarLastHint.x,
                        this.astarLastHint.y,
                    ]
                  : null;
        this.deckProbe = null;
        this._setDeckPreview(null);
        if (!this.field || !point) {
            this._renderDeckList();
            return;
        }
        const displayZoneId = this._astarZoneId();
        if (Number.isNaN(displayZoneId)) {
            this._renderDeckList();
            return;
        }
        // 路点存的是显示帧; 预览点(astarLocateHints)本来就是 base 帧, 别再转一次。
        const tierId = index >= 0 ? this._activeDisplayTierId() : null;
        const base = tierId !== null ? this.field.tierToBase(tierId, point[0], point[1]) : point;

        let res;
        try {
            res = await postDeckProbe({zone_id: this.field.geometryZoneId(displayZoneId), point: base});
        } catch {
            return; // 探针只是提示, 失败就静默放过, 别打断编辑
        }
        if (token !== this._deckToken) return; // 期间点变了, 丢弃这次结果
        this.deckProbe = res && res.ok && res.decks ? {index, decks: res.decks} : null;
        this._renderDeckList();
    }

    /** 只有一张面时整块隐藏 —— 没有重叠就没有要选的东西。@returns {void} */
    _renderDeckList() {
        const box = this.els.astarDeckBox;
        const list = this.els.astarDeckList;
        if (!box || !list) return;
        const decks = this.deckProbe ? this.deckProbe.decks : [];
        list.replaceChildren();
        if (decks.length < 2) {
            box.hidden = true;
            return;
        }
        box.hidden = false;
        const index = this.deckProbe.index;
        const filled = index >= 0 ? this.astarDecks[index] : this.hintDeck;
        this.els.astarDeckTitle.textContent = `重叠面：该点底下压着 ${decks.length} 张可走面`;

        decks.forEach((deck, i) => {
            const row = document.createElement("div");
            row.className = "deck-item";
            if (this.deckPreview === deck.height) row.classList.add("is-preview");
            if (filled === deck.height) row.classList.add("is-filled");

            const pick = document.createElement("button");
            pick.type = "button";
            pick.className = "deck-pick";
            pick.title = "预览这一层";
            pick.textContent = deck.height.toFixed(2);
            const note = document.createElement("small");
            // 列表按高度从上往下排, 层号跟着自上而下数
            note.textContent = ` 自上而下第 ${i + 1} 层 / 共 ${decks.length} 层${deck.thin ? "（薄片，多半是墙顶）" : ""}`;
            pick.appendChild(note);
            pick.addEventListener("click", () => {
                this._setDeckPreview(this.deckPreview === deck.height ? null : deck.height);
                this._renderDeckList();
            });
            row.appendChild(pick);

            // 第一个点是角色起点不是导航目标, 复制路径时不会输出, 所以只给预览不给选择
            if (index !== 0) {
                const fill = document.createElement("button");
                fill.type = "button";
                fill.className = "btn btn-secondary btn-sm";
                fill.textContent = filled === deck.height ? "已选" : "选择";
                fill.addEventListener("click", () =>
                    this._fillDeck(index, filled === deck.height ? null : deck.height),
                );
                row.appendChild(fill);
            }
            list.appendChild(row);
        });
    }

    /**
     * 把落在该层高度带里的面点亮、其余压暗,好让开发者一眼看出这是屋顶还是底下那条走廊。
     * @param {?number} height @returns {void}
     */
    _setDeckPreview(height) {
        this.deckPreview = height;
        const band =
            this.deckProbe && height !== null
                ? (this.deckProbe.decks.find((d) => d.height === height) || {}).band
                : null;
        this.renderer.setDeckBand(band || null);
        this._paint();
    }

    /**
     * 记下该路点声明的可走面高度,并按新声明重算预览线 —— 预览线必须跟运行时选中同一张面。
     * @param {number} index @param {?number} height @returns {void}
     */
    _fillDeck(index, height) {
        if (index >= 0) {
            this.astarDecks[index] = height;
        } else {
            this.hintDeck = height;
        }
        this._renderDeckList();
        if (this.astarPoints.length >= 2) {
            this._calculateAstarPreview();
        }
        if (this.navtest) this.navtest.routeChanged();
        setStatus(
            height === null
                ? "已清除该点的 target_deck_y。"
                : `该点 target_deck_y = ${height.toFixed(2)}，复制路径时会带上。`,
            "#10b981",
        );
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
        this.astarDiagnostics = [];
        const token = this._probeToken;
        if (!this.field || this.astarPoints.length !== 1) return;

        const displayZoneId = this._astarZoneId();
        if (Number.isNaN(displayZoneId)) return;
        const tierId = this._activeDisplayTierId();
        const [
            px,
            py,
        ] = this.astarPoints[0];
        const base =
            tierId !== null
                ? this.field.tierToBase(tierId, px, py)
                : [
                      px,
                      py,
                  ];

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
        this._addOffMeshMark(base, "S", {...probe, exact: false});
        const d = probe.distance === null ? "附近无网格" : `最近网格 ${probe.distance.toFixed(1)} 格`;
        setStatus(`⚠ 该点不在可走网格上（${d}）——运行时会直线盲走过去，请自行确认这条直线走得通。`, "#f59e0b");
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
                // 声明只钉终点: 运行时重规划的起点是实时二维定位, 本来就没有面可言
                const res = await postRoute({
                    zone_id: geomId,
                    start: legStart,
                    goal: legGoal,
                    snap_radius: ASTAR_PREVIEW_SNAP_RADIUS,
                    floor_y: floorY,
                    goal_deck_y: this.astarDecks[i + 1] ?? null,
                });

                if (!res || !res.ok) {
                    this._markFailedLeg(res && res.off_mesh, legStart, legGoal, i, basePoints.length);
                    throw new Error(res?.error || `第 ${i + 1} 段 A* 寻路失败`);
                }

                // The runtime blind-walks a straight line onto the mesh (off-mesh start) or off it
                // (off-mesh goal). These are its real numbers, so draw the actual lines it walks.
                if (res.blind_start) {
                    const {entry, distance, reason} = res.blind_start;
                    this.astarBlindWalks.push({off: legStart, mesh: entry, distance, kind: "起点"});
                    this._addOffMeshMark(legStart, this._astarBadgeLabel(i, basePoints.length), {
                        nearest: entry,
                        distance,
                        reason,
                        exact: true,
                    });
                }
                if (res.blind_target) {
                    const {reached, gap, reason} = res.blind_target;
                    this.astarBlindWalks.push({off: legGoal, mesh: reached, distance: gap, kind: "终点"});
                    this._addOffMeshMark(legGoal, this._astarBadgeLabel(i + 1, basePoints.length), {
                        nearest: reached,
                        distance: gap,
                        reason,
                        exact: true,
                    });
                }

                const segmentPts = res.points || [];
                this.astarDiagnostics.push({
                    ...(res.debug || {}),
                    start: legStart,
                    goal: legGoal,
                });
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
                const kinds = [...new Set(this.astarBlindWalks.map((b) => b.kind))].join("/");
                // "接不上网格" 两种成因都覆盖(点离网 / 脚下网格不连通); 具体是哪种, 徽标上写着。
                setStatus(
                    `${summary} ⚠ ${this.astarBlindWalks.length} 处盲走：${kinds}接不上网格，运行时会沿橙色虚线直着走过去（最长 ${worst} 格）——请自行确认这条直线走得通。`,
                    "#f59e0b",
                );
            } else {
                setStatus(summary, "#10b981");
            }
            this._paint();
        } catch (err) {
            this.astarRoute = null;
            this.astarBlindWalks = [];
            const msg = err && err.message ? err.message : err;
            setStatus(`A* 寻路失败: ${msg}${this._offMeshHint()}`, "#ef4444");
            this._paint();
        }
    }

    /** Badge letter for A* waypoint `i` of `n` (matches the overlay's S / 2..n-1 / G). */
    _astarBadgeLabel(i, n) {
        if (i === 0) return "S";
        if (i === n - 1) return "G";
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
            reason: info.reason || "off_mesh",
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
        if (offMesh.start)
            this._addOffMeshMark(legStart, this._astarBadgeLabel(i, n), {...offMesh.start, exact: false});
        if (offMesh.goal)
            this._addOffMeshMark(legGoal, this._astarBadgeLabel(i + 1, n), {...offMesh.goal, exact: false});
    }

    /** Trailing clause for a failed-route status line, naming the off-mesh endpoints. @returns {string} */
    _offMeshHint() {
        if (!this.offMeshMarks.length) {
            return "（起终点都在网格上——多半是两点分属互不连通的网格块）";
        }
        const parts = this.offMeshMarks.map((m) => {
            if (m.distance === null || m.distance === undefined) return `${m.label} 附近没有可走网格`;
            // budget = 这个位置上运行时肯盲走的上限, 由后端按起点/终点的角色给。
            const over = m.budget && m.distance > m.budget ? `，超出盲走上限 ${m.budget} 格` : "";
            return `${m.label} 离网格 ${m.distance.toFixed(1)} 格${over}`;
        });
        return `（${parts.join("；")}）`;
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
        this.astarDecks = [];
        this.hintDeck = null;
        this.astarRoute = null;
        this.astarLocateHints = [];
        this.astarPendingTargets = [];
        this.astarPendingDecks = [];
        this.astarDiagnostics = [];
        this._resetOffMeshOverlays();
        this._clearLivePath();
        this._astarRouteChanged();
    }

    /** A* 预览线变了: 重探末点可走面, 并把新线装载到试跑会话。 @returns {void} */
    _astarRouteChanged() {
        this._refreshDeckProbe();
        if (this.navtest) this.navtest.routeChanged();
    }

    /** Reset A* view state on a zone change. @returns {void} */
    _resetAstarViewState() {
        this._clearAstarPreview();
        this._meshKey = null; // force a mesh reload for the new zone
    }

    /** "清除预览" button. @returns {void} */
    _onClearAstar() {
        this._clearAstarPreview();
        setStatus("已清除 A* 预览。", "#10b981");
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
            setStatus("地图尚未就绪，无法定位。", "#ef4444");
            return;
        }

        const connectionPayload = this.connection ? this.connection.buildSession() : null;
        const locateButton = mode === "assert" ? this.els.btnAssertLocate : this.els.btnAstarLocate;
        if (locateButton) locateButton.disabled = true;
        setStatus("正在连接游戏并获取位置，请保持游戏前台运行...", "#3b82f6");
        if (this.positionReadout) this.positionReadout.setPending("正在获取位置与朝向...");

        try {
            const res = await locateOnce(connectionPayload);
            if (res && res.ok) {
                const {x, y, zone, rot = null} = res;
                const headingText = formatHeading(rot);
                if (this.positionReadout) this.positionReadout.update({x, y, zone, rot});
                setStatus(`定位成功: [${x.toFixed(1)}, ${y.toFixed(1)}] · ${headingText} @ ${zone}`, "#10b981");

                if (mode === "astar") {
                    const zoneIdNum = this._resolveZoneId(zone);
                    if (!Number.isNaN(zoneIdNum)) {
                        if (this._astarZoneId() !== zoneIdNum) {
                            // _onAstarZoneChanged resets the A* view state, so switch BEFORE marking.
                            if (this._selectDisplayZoneById(zoneIdNum)) this._onAstarZoneChanged(false);
                        }

                        // 定位给的是所在 zone 自己的帧(tier 上即 tier px), 预览点存 base px。
                        const [
                            bx,
                            by,
                        ] = this._pointToBase(zoneIdNum, x, y);
                        const baseRot = this._headingToBase(zoneIdNum, x, y, rot);
                        this._addAstarHint(bx, by, `游戏当前位置 · ${headingText}`, baseRot);
                        setStatus(
                            `已标记 A* 定位预览点: [${x.toFixed(1)}, ${y.toFixed(1)}] · ${headingText}。当前有 ${this.astarLocateHints.length} 个预览点。`,
                            "#10b981",
                        );
                        this._focusAstarHints();
                    }
                } else if (mode === "assert") {
                    const matchedZoneId = normalizeZoneId(zone);
                    if (matchedZoneId) {
                        const zoneObj = this.field.zoneByName(matchedZoneId);
                        const numericIdStr = zoneObj ? String(zoneObj.zone_id) : matchedZoneId;
                        this._ensureAssertZoneOption(numericIdStr);
                        if (normalizeZoneId(this.els.assertZoneCombo.value) !== numericIdStr) {
                            this.els.assertZoneCombo.value = numericIdStr;
                            this._onAssertZoneChanged();
                        }
                        // 定位给的是所在 zone 自己的帧(tier 上即 tier px), 提示点存 base px。
                        const [
                            bx,
                            by,
                        ] = zoneObj
                            ? this._pointToBase(zoneObj.zone_id, x, y)
                            : [
                                  x,
                                  y,
                              ];
                        const baseRot = zoneObj ? this._headingToBase(zoneObj.zone_id, x, y, rot) : rot;
                        this.assertLocateHint = {x: bx, y: by, rot: baseRot, label: `游戏当前位置 · ${headingText}`};
                        this._fitView();
                        setStatus(
                            `已定位到游戏当前位置 [${x.toFixed(1)}, ${y.toFixed(1)}] · ${headingText}，请在此提示点周围拖拽鼠标来画出断言矩形。`,
                            "#10b981",
                        );
                        this._paint();
                    }
                }
            } else {
                setStatus(`定位失败: ${res?.error || "未知错误"}`, "#ef4444");
                if (this.positionReadout) this.positionReadout.setPending("未获取到位置与朝向");
            }
        } catch (err) {
            setStatus(`定位异常: ${err && err.message ? err.message : err}`, "#ef4444");
            if (this.positionReadout) this.positionReadout.setPending("未获取到位置与朝向");
        } finally {
            if (locateButton) locateButton.disabled = false;
        }
    }

    // ==================================================================================
    //  A* preview markers (locate fix / hand-entered coordinate / imported JSON)
    // ==================================================================================

    /**
     * Append an A* preview marker. Coords are **base px** (callers convert from the point's
     * own zone frame); `_paint` projects them into the display frame.
     * @param {number} x @param {number} y @param {string} label caption drawn under the marker
     * @param {?number} [rot=null] north-up clockwise heading in the base frame
     * @returns {void}
     */
    _addAstarHint(x, y, label, rot = null) {
        this.astarLocateHints.push({x, y, label, rot});
        this.astarPendingTargets = [];
        this.astarPendingDecks = [];
        this.hintDeck = null;
        this._refreshDeckProbe();
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
     * Paste in either coordinate box: a JSON `[x, y]` pair fills both boxes. Other
     * text keeps the browser's normal single-box paste behavior.
     * @param {ClipboardEvent} event
     * @returns {void}
     */
    _onAstarCoordPaste(event) {
        const text = event.clipboardData ? event.clipboardData.getData("text/plain") : "";
        const pair = parsePastedCoordinatePair(text);
        if (!pair) return;
        event.preventDefault();
        this.els.astarCoordX.value = String(pair[0]);
        this.els.astarCoordY.value = String(pair[1]);
        setStatus(`已从粘贴内容解析坐标: [${pair[0]}, ${pair[1]}]，点击「标点」即可显示。`, "#10b981");
    }

    /**
     * "标点" button / Enter in either coordinate box: mark a preview point at the typed
     * base-px coordinate, then frame the markers.
     * @returns {void}
     */
    _onAstarMarkCoord() {
        const x = Number(String(this.els.astarCoordX.value || "").trim());
        const y = Number(String(this.els.astarCoordY.value || "").trim());
        if (
            !this.els.astarCoordX.value.trim() ||
            !this.els.astarCoordY.value.trim() ||
            !Number.isFinite(x) ||
            !Number.isFinite(y)
        ) {
            setStatus("请在 X / Y 两个框中各填一个数字，例如 X=1234.5、Y=678.9", "#ef4444");
            return;
        }
        this._addAstarHint(x, y, `[${compactNumber(x)}, ${compactNumber(y)}]`);
        this.els.astarCoordX.value = "";
        this.els.astarCoordY.value = "";

        setStatus(
            `已标记坐标预览点: [${x}, ${y}]（底图坐标）。当前有 ${this.astarLocateHints.length} 个预览点。`,
            "#10b981",
        );
        this._focusAstarHints();
    }

    // ==================================================================================
    //  Read-only MapNavigator log analysis
    // ==================================================================================

    /** Read local maafw logs or MaaEnd ZIP parts and replace the current analysis set. */
    async _importLogFiles(fileList) {
        const files = Array.from(fileList || []);
        if (!files.length) return;
        setStatus(`正在检查 ${files.length} 个日志/ZIP 文件…`, "#3b82f6");
        this.els.btnLogImport.disabled = true;
        try {
            const inputs = groupLogInputFiles(files);
            let frameWarning = "";
            if (inputs.archiveGroups.length && !this.ziplineFrameConfig) {
                try {
                    this.ziplineFrameConfig = await getZiplineFrames();
                } catch (err) {
                    frameWarning = `；滑索标定加载失败：${err && err.message ? err.message : err}`;
                }
            }

            const runs = [];
            const archiveGroups = new Map();
            let parsedLogCount = 0;
            let ziplineSnapshotCount = 0;

            for (const file of inputs.plainFiles) {
                const sourceName = file.webkitRelativePath || file.name;
                setStatus(`正在解析 ${sourceName}…`, "#3b82f6");
                runs.push(...parseMapNavigatorLog(await file.text(), sourceName));
                parsedLogCount += 1;
            }

            for (const group of inputs.archiveGroups) {
                const groupState = {
                    label: group.label,
                    records: null,
                    sourceNames: group.files.map((file) => file.name),
                };
                archiveGroups.set(group.key, groupState);
                for (const file of group.files) {
                    setStatus(`正在读取 ${file.name} 的目录…`, "#3b82f6");
                    const archive = await openZipArchive(file, file.name);
                    const selected = selectMaaEndArchiveEntries(archive.entries);

                    for (const entry of selected.ziplineRecords) {
                        setStatus(`正在读取 ${file.name}/${entry.name}…`, "#3b82f6");
                        const parsed = JSON.parse(await archive.readText(entry));
                        if (!parsed || !Array.isArray(parsed.maps)) {
                            throw new Error(`${file.name}/${entry.name} 不是有效的 Ziplines.json`);
                        }
                        groupState.records = parsed;
                        ziplineSnapshotCount += 1;
                    }

                    for (const entry of selected.logs) {
                        const sourceName = `${file.name}/${entry.name}`;
                        setStatus(`正在解析 ${sourceName}…`, "#3b82f6");
                        const parsedRuns = parseMapNavigatorLog(await archive.readText(entry), sourceName);
                        for (const run of parsedRuns) run._archiveGroupKey = group.key;
                        runs.push(...parsedRuns);
                        parsedLogCount += 1;
                    }
                }
            }

            this.logArchiveGroups = archiveGroups;
            this.logRuns = runs.sort(
                (a, b) => b.timestamp.localeCompare(a.timestamp) || a.sourceName.localeCompare(b.sourceName),
            );
            this.logRuns.forEach((run, index) => {
                run._uiKey = String(index);
            });
            this.selectedLogRun = null;
            this.logDistanceSelection = [];
            this.logInspectedPoint = null;
            this.els.logRunFilter.value = "";
            this.els.logRunFilter.disabled = this.logRuns.length === 0;
            this.els.logRunSelect.disabled = this.logRuns.length === 0;
            const zipMeta = inputs.archiveGroups.length
                ? ` · ${inputs.archiveGroups.length} 个 ZIP 日志包 · ${ziplineSnapshotCount} 份滑索快照`
                : "";
            this.els.logImportMeta.textContent = `${files.length} 个文件 · ${parsedLogCount} 份 cpp-algo 日志${zipMeta} · 找到 ${this.logRuns.length} 次 MapNavigateAction${frameWarning}`;
            this._populateLogRunSelect();
            if (this.logRuns.length) {
                setStatus(
                    frameWarning
                        ? `日志解析完成：找到 ${this.logRuns.length} 次导航运行${frameWarning}`
                        : `日志解析完成：找到 ${this.logRuns.length} 次导航运行。`,
                    frameWarning ? "#f59e0b" : "#10b981",
                );
            } else {
                setStatus("日志中没有找到带路径数据的 MapNavigateAction。", "#f59e0b");
            }
        } catch (err) {
            setStatus(`日志解析失败：${err && err.message ? err.message : err}`, "#ef4444");
        } finally {
            this.els.btnLogImport.disabled = false;
            this.els.logFileInput.value = "";
        }
    }

    /** Apply the text filter and keep the selected run when it remains visible. */
    _populateLogRunSelect() {
        const combo = this.els.logRunSelect;
        const query = String(this.els.logRunFilter.value || "")
            .trim()
            .toLowerCase();
        const previous = this.selectedLogRun ? this.selectedLogRun._uiKey : "";
        combo.textContent = "";
        const visible = this.logRuns.filter((run) => {
            if (!query) return true;
            const failure = run.failure
                ? `${run.failure.reason || ""} ${run.failure.text || ""} ${run.failure.message || ""}`
                : "";
            const incidents = (run.incidents || [])
                .map((incident) => `${incident.reason || ""} ${incident.text || ""} ${incident.detail || ""}`)
                .join(" ");
            return `${run.timestamp} ${run.nodeName} ${run.sourceName} ${run.zone} ${failure} ${incidents}`
                .toLowerCase()
                .includes(query);
        });
        for (const run of visible) {
            const option = document.createElement("option");
            option.value = run._uiKey;
            option.textContent = this._logRunLabel(run);
            option.title = `${run.timestamp} · ${run.nodeName} · ${run.sourceName}`;
            combo.appendChild(option);
        }
        combo.disabled = visible.length === 0;
        if (!visible.length) {
            const option = document.createElement("option");
            option.value = "";
            option.textContent = this.logRuns.length ? "没有匹配的运行记录" : "请先导入日志";
            combo.appendChild(option);
            this.selectedLogRun = null;
            this.logDistanceSelection = [];
            this.logInspectedPoint = null;
            this._renderLogSummary();
            this._renderLogInspection();
            this._renderLogDistance();
            this._paint();
            return;
        }
        combo.value = visible.some((run) => run._uiKey === previous) ? previous : visible[0]._uiKey;
        this._onLogRunChanged();
    }

    /** @param {Object} run @returns {string} */
    _logRunLabel(run) {
        const result = run.completed === true ? "成功" : run.completed === false ? "失败" : "未结束";
        return `${run.timestamp || "时间未知"} · ${result} · ${run.nodeName} · ${run.sourceName}`;
    }

    /** Select the dropdown's run, switch to its basemap, and frame the recorded geometry. */
    _onLogRunChanged() {
        const key = this.els.logRunSelect.value;
        const nextRun = this.logRuns.find((run) => run._uiKey === key) || null;
        if (nextRun !== this.selectedLogRun) {
            this.logDistanceSelection = [];
            this.logInspectedPoint = null;
        }
        this.selectedLogRun = nextRun;
        this._showSelectedLogRun({fit: true});
    }

    /** @param {{fit?:boolean}} [opts] */
    _showSelectedLogRun(opts = {}) {
        this._renderLogSummary();
        this._renderLogInspection();
        this._renderLogDistance();
        const run = this.selectedLogRun;
        if (!run) {
            this._paint();
            return;
        }
        if (this.state.mode !== Mode.LOG) return;
        if (!this.field) {
            setStatus("运行记录已选择，等待 navmesh 区域表加载后显示底图。", "#3b82f6");
            this._paint();
            return;
        }
        const zone = this.field.zoneByName(run.zone) || this.field.zoneById(parseInt(run.zone, 10));
        if (!zone) {
            setStatus(`日志区域 ${run.zone || "未知"} 不在当前 navmesh 数据中。`, "#f59e0b");
            this._paint();
            return;
        }
        const base = this.field.zoneById(this.field.geometryZoneId(zone.zone_id));
        if (!base || !this.field.displayBaseNames().includes(base.name)) {
            setStatus(`日志区域 ${run.zone} 没有可显示的底图。`, "#f59e0b");
            this._paint();
            return;
        }
        this.els.astarDisplayZoneCombo.value = base.name;
        this._refreshAstarZoneChoices();
        if (this.els.astarZoneCombo.options.length) this.els.astarZoneCombo.selectedIndex = 0;
        this._refreshZoneLabel();
        if (opts.fit) this._fitView();
        else this._doRedraw();
    }

    /** Clear imported log data without touching the editor's route. */
    _clearLogAnalysis() {
        this.logRuns = [];
        this.selectedLogRun = null;
        this.logDistanceSelection = [];
        this.logInspectedPoint = null;
        this.logArchiveGroups = new Map();
        this.els.logRunFilter.value = "";
        this.els.logRunFilter.disabled = true;
        this.els.logRunSelect.disabled = true;
        this.els.logRunSelect.textContent = "";
        const option = document.createElement("option");
        option.value = "";
        option.textContent = "请先导入日志";
        this.els.logRunSelect.appendChild(option);
        this.els.logImportMeta.textContent = "尚未导入日志";
        this._renderLogSummary();
        this._renderLogInspection();
        this._renderLogDistance();
        setStatus("已清除日志分析。", "#10b981");
        this._paint();
    }

    /** Render the terminal navigation reason and the nearest preceding recovery event. */
    _renderLogFailure(host, run) {
        if (run.completed !== false) return;

        const failure = run.failure;
        const card = document.createElement("div");
        card.className = "log-decision-card failure";

        const title = document.createElement("div");
        title.className = "log-failure-title";
        title.textContent = "失败原因";
        card.appendChild(title);

        const summary = document.createElement("div");
        summary.className = "log-failure-summary";
        summary.textContent = failure
            ? failure.text
            : "日志只记录了运行失败，没有找到可解析的 MapNavigator 终止原因。";
        card.appendChild(summary);

        if (failure?.reason) {
            const reason = document.createElement("div");
            reason.className = "log-decision-raw";
            reason.textContent = `reason=${failure.reason}`;
            card.appendChild(reason);
        }
        if (failure?.message) {
            const message = document.createElement("div");
            message.className = "log-failure-message";
            message.textContent = failure.message;
            card.appendChild(message);
        }

        const metrics = failure?.metrics ? Object.entries(failure.metrics) : [];
        if (metrics.length) {
            const raw = document.createElement("div");
            raw.className = "log-decision-raw";
            raw.textContent = metrics.map(([name, value]) => `${name}=${value}`).join(" · ");
            card.appendChild(raw);
        }

        const incidents = run.incidents || [];
        const incident = incidents.length ? incidents[incidents.length - 1] : null;
        if (incident) {
            const incidentBox = document.createElement("div");
            incidentBox.className = "log-failure-incident";

            const incidentTitle = document.createElement("div");
            incidentTitle.className = "log-failure-incident-title";
            incidentTitle.textContent = `最近一次恢复事件${incident.timestamp ? ` · ${incident.timestamp}` : ""}`;
            incidentBox.appendChild(incidentTitle);

            const incidentSummary = document.createElement("div");
            incidentSummary.textContent = incident.text;
            incidentBox.appendChild(incidentSummary);

            const rawParts = [];
            if (incident.reason) rawParts.push(`reason=${incident.reason}`);
            if (incident.detail) rawParts.push(`detail=${incident.detail}`);
            if (Number.isFinite(incident.dropped)) rawParts.push(`dropped=${incident.dropped}`);
            if (Array.isArray(incident.position)) {
                rawParts.push(`position=[${incident.position.map((value) => value.toFixed(2)).join(", ")}]`);
            }
            if (rawParts.length) {
                const incidentRaw = document.createElement("div");
                incidentRaw.className = "log-decision-raw";
                incidentRaw.textContent = rawParts.join(" · ");
                incidentBox.appendChild(incidentRaw);
            }
            card.appendChild(incidentBox);
        }

        host.appendChild(card);
    }

    /** Render costs, savings, raw reason identifiers, and execution confirmation. */
    _renderLogSummary() {
        const host = this.els.logDecisionSummary;
        host.textContent = "";
        const run = this.selectedLogRun;
        if (!run) {
            host.textContent = this.logRuns.length
                ? "当前筛选没有匹配记录。"
                : "导入日志后，这里会列出每段的成本计算和选择原因。";
            return;
        }

        const heading = document.createElement("div");
        heading.className = "log-run-heading";
        heading.textContent = run.nodeName;
        host.appendChild(heading);

        const facts = document.createElement("div");
        facts.className = "log-run-facts";
        const towerData = this._logTowerData(run);
        const result = run.completed === true ? "成功" : run.completed === false ? "失败" : "未记录结束";
        const landed = (run.ziplines || []).reduce((sum, chain) => sum + (chain.landed || 0), 0);
        const launched = (run.ziplines || []).reduce((sum, chain) => sum + (chain.launches || []).length, 0);
        const ziplineFact = launched ? `滑索 ${landed}/${launched} 跳确认落地` : "无实际滑索发射";
        const observedSegments = (run.observedWalks || []).length;
        const observedPoints = (run.observedWalks || []).reduce((sum, points) => sum + points.length, 0);
        const observedFact = observedPoints
            ? `实测地面轨迹 ${observedSegments} 段/${observedPoints} 点`
            : "无实测地面轨迹";
        const recordedFact = run._archiveGroupKey
            ? `ZIP 候选滑索架 ${towerData.recorded.length} 座`
            : "未关联 ZIP 滑索快照";
        facts.textContent = `${run.timestamp || "时间未知"} · ${run.zone || "区域未知"} · ${result} · ${ziplineFact} · ${observedFact} · ${recordedFact} · ${run.sourceName}`;
        host.appendChild(facts);

        this._renderLogFailure(host, run);

        if (towerData.selected.length) {
            const towerList = document.createElement("div");
            towerList.className = "log-tower-list";
            for (const tower of towerData.selected) {
                const row = document.createElement("div");
                row.className = "log-tower-row";
                const height = Number.isFinite(tower.height) ? tower.height.toFixed(2) : "?";
                const coordinates = document.createElement("span");
                coordinates.textContent = `${tower.label} [${tower.point[0].toFixed(2)}, ${tower.point[1].toFixed(2)}, ${height}]`;
                const state = document.createElement("span");
                state.className = `log-tower-state ${tower.confirmed ? "confirmed" : "unconfirmed"}`;
                state.textContent = tower.confirmed ? "实际经过" : "未确认经过";
                row.append(coordinates, state);
                towerList.appendChild(row);
            }
            host.appendChild(towerList);

            const expectedTowers = (run.ziplines || []).reduce(
                (sum, chain) => sum + (Number.isFinite(chain.towerCount) ? chain.towerCount : 0),
                0,
            );
            if (expectedTowers > towerData.selected.length) {
                const limited = document.createElement("div");
                limited.className = "log-run-facts";
                limited.textContent = `运行日志只保留首尾架和实际发射落点，当前可定位 ${towerData.selected.length}/${expectedTowers} 座；其余位置请对照 ZIP 候选背景点。`;
                host.appendChild(limited);
            }
        }

        const decisions = run.decisions || [];
        if (!decisions.length) {
            const card = document.createElement("div");
            card.className = "log-decision-card warning";
            card.textContent = run.zipRequested
                ? "请求启用了滑索，但这份日志没有记录可解析的路线决策。"
                : "本次请求未启用滑索；仅显示作者提示和已生成的步行规划。";
            host.appendChild(card);
            return;
        }

        for (const decision of decisions) {
            const card = document.createElement("div");
            const kindClass = decision.kind === "zipline" ? "zipline" : decision.kind === "walk" ? "walk" : "warning";
            card.className = `log-decision-card ${kindClass}`;

            if (decision.kind === "zipline") {
                const formula = document.createElement("div");
                formula.className = "log-decision-formula";
                if (
                    decision.walkingBaselineAvailable &&
                    Number.isFinite(decision.baselineLength) &&
                    Number.isFinite(decision.cost)
                ) {
                    const saving = decision.baselineLength - decision.cost;
                    const percent = decision.baselineLength > 0 ? (saving / decision.baselineLength) * 100 : 0;
                    formula.textContent = `${decision.baselineLength.toFixed(3)} − ${decision.cost.toFixed(3)} = ${saving.toFixed(3)}（节省 ${percent.toFixed(2)}%）`;
                } else {
                    const cost = Number.isFinite(decision.cost) ? `（滑索代价 ${decision.cost.toFixed(3)}）` : "";
                    formula.textContent = `步行基线不可达，使用滑索桥接${cost}`;
                }
                card.appendChild(formula);
                const detail = document.createElement("div");
                const towers = Number.isFinite(decision.towerCount) ? decision.towerCount : null;
                detail.textContent = towers
                    ? `选择滑索：${towers} 座滑索架，实际链长 ${Math.max(0, towers - 1)} 跳。`
                    : "选择滑索。";
                card.appendChild(detail);
            } else {
                const detail = document.createElement("div");
                const cost = Number.isFinite(decision.cost) ? `（步行代价 ${decision.cost.toFixed(3)}）` : "";
                detail.textContent = `${decision.text || "路线决策"}${cost}`;
                card.appendChild(detail);
            }

            if (decision.reason) {
                const raw = document.createElement("div");
                raw.className = "log-decision-raw";
                raw.textContent = `why=${decision.reason}`;
                card.appendChild(raw);
            }
            host.appendChild(card);
        }
    }

    // ==================================================================================
    //  Mode switching (mutually exclusive: edit / assert / A* / log)
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
        if (this.navtest) this.navtest.routeChanged();
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
        if (
            base &&
            this.field.displayBaseNames().includes(base.name) &&
            this.els.astarDisplayZoneCombo.value !== base.name
        ) {
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
        const next = (((index + delta) % names.length) + names.length) % names.length;
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

    /** @returns {boolean} whether the selected point is a required global-planning boundary. */
    _required() {
        return this.els.chkRequired.checked;
    }

    /** @returns {string} the explicitly declared coordinate frame for selected points. */
    _targetTier() {
        return normalizeZoneId(this.els.targetTierEntry.value);
    }

    /** "设为该动作" button: apply the dropdown action + strict flag to the selection. @returns {void} */
    _applyAction() {
        const result = this.state.editApplyActionToSelected(
            this._actionName(),
            this._strict(),
            this._required(),
            this._targetTier(),
        );
        if (result.selectionEmpty) {
            setStatus("请先点击选中一个点", "#f59e0b");
            return;
        }
        if (result.changed) this._afterStructureChanged();
    }

    /** Delete per mode: A* preview, assert rect, or the selected route points. @returns {void} */
    _deleteSelectedPoint() {
        if (this.state.mode === Mode.LOG) {
            setStatus("日志分析模式为只读；请用“清除”移除导入的日志。", "#f59e0b");
            return;
        }
        if (this.state.mode === Mode.ASTAR) {
            this._clearAstarPreview();
            setStatus("已清除 A* 预览。", "#10b981");
            this._paint();
            return;
        }
        if (this.state.mode === Mode.ASSERT) {
            if (!this.assertRectWorld) {
                setStatus("当前没有可删除的 Assert 区域", "#f59e0b");
                return;
            }
            this.assertRectWorld = null;
            this.isAssertSelecting = false;
            setStatus("已清除 Assert 区域。", "#10b981");
            if (this.navtest) this.navtest.routeChanged();
            this._paint();
            return;
        }
        const result = this.state.editDeleteSelected();
        if (result.selectionEmpty) {
            setStatus("请先点击选中一个点", "#f59e0b");
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

    /** Keep each copy button's label aligned with its selected output format. @returns {void} */
    _syncCopyButtonLabels() {
        this.els.btnCopyNavmesh.textContent = "复制路径";
        this.els.btnCopyAssert.textContent =
            this.els.assertCopyFormat.value === COPY_FORMAT_COORDINATES ? "复制坐标" : "复制断言";
    }

    /** Export the route as a MapNavigator path node (backend, tk-byte-identical) and copy it. @returns {Promise<void>} */
    async _copyPath() {
        if (!this.state.points.length) {
            setStatus("当前没有任何轨迹数据", "#ef4444");
            return;
        }
        try {
            const result = await exportPath(this.state.points);
            await this._copyText(result.text);
            setStatus("MapNavigator path 已复制到剪贴板", "#10b981");
        } catch (err) {
            const msg = err && err.message ? err.message : err;
            setStatus(`复制失败: ${msg}`, "#ef4444");
        }
    }

    /** Export the assert rect as a full node or a routes.json NavAssert coordinate array. @returns {Promise<void>} */
    async _copyAssert() {
        const zoneId = this._displayZoneId();
        if (!zoneId) {
            setStatus("请先选择 Assert 地图", "#ef4444");
            return;
        }
        const target = this._currentAssertTarget();
        if (!target) {
            setStatus("请先在地图上拖拽画出断言矩形", "#ef4444");
            return;
        }
        try {
            if (this.els.assertCopyFormat.value === COPY_FORMAT_COORDINATES) {
                await this._copyText(JSON.stringify(target, null, 4));
                setStatus(`环境监测 NavAssert 坐标已复制: [${target.join(", ")}]`, "#10b981");
                return;
            }
            const result = await exportAssert(zoneId, target);
            await this._copyText(result.text);
            setStatus("MapLocateAssertLocation 节点已复制到剪贴板", "#10b981");
        } catch (err) {
            const msg = err && err.message ? err.message : err;
            setStatus(`复制失败: ${msg}`, "#ef4444");
        }
    }

    /**
     * The A* waypoints after the start, as NAVMESH action payloads. Requires a display
     * zone and ≥2 points. 复制路径与实机试跑都走这一处, 跑的就是复制出来的那一份。
     * @returns {Array<Object>}
     */
    _navmeshTargets() {
        const tierId = this._activeDisplayTierId();
        let tierName = "";
        if (tierId !== null) {
            const zone = this.field.zoneById(tierId);
            if (zone && zone.name) {
                tierName = zone.name;
            }
        }

        const targets = [];
        for (let i = 1; i < this.astarPoints.length; i++) {
            const pt = this.astarPoints[i];
            // 画的点本就是显示帧 px(有 tier 时即 tier px), 正是 target_tier 要的帧; 不带 tier 才转 base px。
            const target = tierName ? pt : tierId !== null ? this.field.tierToBase(tierId, pt[0], pt[1]) : pt;
            const payload = {
                action: "NAVMESH",
                target: [
                    compactNumber(target[0]),
                    compactNumber(target[1]),
                ],
            };
            if (tierName) {
                payload.target_tier = tierName;
            }
            if (this.astarDecks[i] !== null && this.astarDecks[i] !== undefined) {
                payload.target_deck_y = this.astarDecks[i];
            }
            targets.push(payload);
        }
        return targets;
    }

    /**
     * 试跑要跑的那一份, 取自当前页签: 路径编辑交编辑器原始路点 (导出在服务侧, 只此一处口径),
     * A* 交已导出的 NAVMESH 节点 —— tier 变换与显示底图只有前端有, 后端换算不出来。
     * 断言模式没有线, 交那个框, 由后端导成 MapLocateAssertLocation 节点认一次。
     * @returns {{path: Array, exported: boolean, assert_target: ?Object}}
     */
    _navtestRoute() {
        if (this.state.mode === Mode.LOG) {
            return {path: [], exported: false, assert_target: null};
        }
        if (this.state.mode === Mode.ASTAR) {
            const ready = this.field && this._displayZoneId() && this.astarPoints.length >= 2;
            return { path: ready ? this._navmeshTargets() : [], exported: true };
        }
        if (this.state.mode === Mode.ASSERT) {
            const zoneId = this._displayZoneId();
            const target = this._currentAssertTarget();
            // 零面积的框 (点一下没拖) 后端会拒, 这里就当作没画: 免得白连一次游戏。
            const drawn = !!target && target[2] > 0 && target[3] > 0;
            return {
                path: [],
                exported: false,
                assert_target: zoneId && drawn ? { zone_id: zoneId, target } : null,
            };
        }
        return { path: this.state.points, exported: false };
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
            setStatus("请先选择 NAVMESH 底图", "#ef4444");
            return;
        }
        if (this.astarPoints.length < 2) {
            const hint = this.astarLastHint;
            if (hint) {
                const tierId = this._activeDisplayTierId();
                let tierName = "";
                if (tierId !== null) {
                    const zone = this.field.zoneById(tierId);
                    if (zone && zone.name) {
                        tierName = zone.name;
                    }
                }
                // 预览点存的是 base px; 带 target_tier 时运行时按 tier px 解读 target, 所以反投回去。
                const target = tierName
                    ? this.field.baseToTier(tierId, hint.x, hint.y)
                    : [
                          hint.x,
                          hint.y,
                      ];
                const payload = {
                    action: "NAVMESH",
                    target: [
                        compactNumber(target[0]),
                        compactNumber(target[1]),
                    ],
                };
                if (tierName) {
                    payload.target_tier = tierName;
                }
                if (this.hintDeck !== null) {
                    payload.target_deck_y = this.hintDeck;
                }
                await this._copyText(JSON.stringify(payload, null, 4));
                const tierNote = tierName ? ` target_tier=${tierName}` : "";
                setStatus(
                    `NAVMESH 目标已复制: zone=${zoneId} target=[${payload.target[0]}, ${payload.target[1]}]${tierNote}`,
                    "#10b981",
                );
                return;
            }
            setStatus("请先标出一个预览点（定位 / 填坐标 / 导入 JSON），或在地图上画一条预览路线", "#ef4444");
            return;
        }
        const targets = this._navmeshTargets();
        const tierName = targets[0].target_tier || "";

        if (targets.length === 1) {
            await this._copyText(JSON.stringify(targets[0], null, 4));
            const tierNote = tierName ? ` target_tier=${tierName}` : "";
            setStatus(
                `NAVMESH 目标已复制: zone=${zoneId} target=[${targets[0].target[0]}, ${targets[0].target[1]}]${tierNote}`,
                "#10b981",
            );
        } else {
            await this._copyText(JSON.stringify(targets, null, 4));
            setStatus(`多段 A* NAVMESH 路径已复制: 共 ${targets.length} 个目标路点`, "#10b981");
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
            const textarea = document.createElement("textarea");
            textarea.value = text;
            textarea.style.position = "fixed";
            textarea.style.opacity = "0";
            document.body.appendChild(textarea);
            textarea.focus();
            textarea.select();
            const ok = document.execCommand("copy");
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
            (target.tagName === "INPUT" ||
                target.tagName === "SELECT" ||
                target.tagName === "TEXTAREA" ||
                target.isContentEditable)
        ) {
            return;
        }
        const ctrl = e.ctrlKey || e.metaKey;
        if (ctrl && (e.key === "z" || e.key === "Z")) {
            if (e.shiftKey) this._redo();
            else this._undo();
            e.preventDefault();
            return;
        }
        if (ctrl && (e.key === "y" || e.key === "Y")) {
            this._redo();
            e.preventDefault();
            return;
        }
        if (e.key === "Delete" || e.key === "Backspace") {
            if (this.state.mode === Mode.ASTAR) {
                if (this.astarPoints.length > 0) {
                    this.astarPoints.pop();
                    this.astarDecks.pop();
                    this.astarRoute = null;
                    if (this.astarPoints.length >= 2) {
                        this._calculateAstarPreview();
                    } else {
                        if (this.astarPoints.length === 1) {
                            setStatus(
                                `A* 起点: [${this.astarPoints[0][0].toFixed(1)}, ${this.astarPoints[0][1].toFixed(1)}]。`,
                                "#3b82f6",
                            );
                        } else {
                            setStatus("A* 点已清空。", "#3b82f6");
                        }
                        this._probeLoneAstarPoint();
                        this._paint();
                    }
                    this._astarRouteChanged();
                    e.preventDefault();
                    return;
                }
            }
            this._deleteSelectedPoint();
            e.preventDefault();
            return;
        }
        if (e.key === "1") {
            if (this.state.mode === Mode.EDIT) this._setActiveTool("add");
            else if (this.state.mode === Mode.ASTAR) this._setActiveTool("astar-single");
            else if (this.state.mode === Mode.ASSERT) this._setActiveTool("assert-edit");
            e.preventDefault();
            return;
        }
        if (e.key === "2") {
            if (this.state.mode === Mode.EDIT) this._setActiveTool("select");
            else if (this.state.mode === Mode.ASTAR) this._setActiveTool("astar-multi");
            e.preventDefault();
            return;
        }
        if (e.key === "+" || e.key === "=" || e.code === "NumpadAdd") {
            this._zoomIn();
            e.preventDefault();
            return;
        }
        if (e.key === "-" || e.key === "_" || e.code === "NumpadSubtract") {
            this._zoomOut();
            e.preventDefault();
            return;
        }
        if (e.key === "c" || e.key === "C") {
            this._copyCoordKey();
        }
    }

    /**
     * Switch the active canvas tool, updating toolbar highlight + canvas cursor.
     * @param {'pan'|'add'|'select'|'astar-single'|'astar-multi'|'astar-pan'|'assert-pan'|'assert-edit'|'log-inspect'|'log-measure'|'log-pan'} tool
     * @returns {void}
     */
    _setActiveTool(tool) {
        this.activeTool = tool;

        const e = this.els;
        if (e.toolPan) e.toolPan.classList.toggle("active", tool === "pan");
        if (e.toolAdd) e.toolAdd.classList.toggle("active", tool === "add");
        if (e.toolSelect) e.toolSelect.classList.toggle("active", tool === "select");
        if (e.toolAstarSingle) e.toolAstarSingle.classList.toggle("active", tool === "astar-single");
        if (e.toolAstarMulti) e.toolAstarMulti.classList.toggle("active", tool === "astar-multi");
        if (e.toolAssertPan) e.toolAssertPan.classList.toggle("active", tool === "assert-pan");
        if (e.toolAssertEdit) e.toolAssertEdit.classList.toggle("active", tool === "assert-edit");
        if (e.btnLogMeasure) {
            const measuring = tool === "log-measure";
            e.btnLogMeasure.classList.toggle("active", measuring);
            e.btnLogMeasure.setAttribute("aria-pressed", String(measuring));
        }

        const canvas = e.overlayCanvas;
        if (tool === "pan" || tool === "assert-pan" || tool === "astar-pan" || tool === "log-pan") {
            canvas.style.cursor = "grab";
        } else if (tool === "log-measure") {
            canvas.style.cursor = "crosshair";
        } else if (tool === "log-inspect") {
            canvas.style.cursor = "default";
        } else if (tool === "add" || tool === "astar-single" || tool === "astar-multi") {
            canvas.style.cursor = "crosshair";
        } else if (tool === "select") {
            canvas.style.cursor = "default";
        } else if (tool === "assert-edit") {
            canvas.style.cursor = "cell";
        } else {
            canvas.style.cursor = "default";
        }
        if (e.logDistanceBox) {
            const showMeasurement =
                tool === "log-measure" || (tool === "log-pan" && this._altSavedTool === "log-measure");
            e.logDistanceBox.hidden = !showMeasurement;
            this._syncLogContextPanel();
        }
    }

    /** @returns {void} */
    _undo() {
        if (this.state.mode === Mode.ASTAR || this.state.mode === Mode.LOG) return;
        if (this.state.undo()) this._afterHistory();
    }

    /** @returns {void} */
    _redo() {
        if (this.state.mode === Mode.ASTAR || this.state.mode === Mode.LOG) return;
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
        if (this.state.mode === Mode.LOG) return;
        if (this.state.mode === Mode.ASTAR) {
            let points = this.astarRoute && this.astarRoute.points ? this.astarRoute.points : [];
            if (!points.length) {
                points = [
                    this.astarStart,
                    this.astarGoal,
                ].filter(Boolean);
                const tierId = this._activeDisplayTierId();
                if (tierId !== null) points = points.map((p) => this.field.tierToBase(tierId, p[0], p[1]));
            }
            if (!points.length) {
                setStatus("当前没有可复制的 A* 预览点。", "#f59e0b");
                return;
            }
            const text = JSON.stringify(
                points.map((p) => [
                    compactNumber(p[0]),
                    compactNumber(p[1]),
                ]),
                null,
                4,
            );
            this._copyText(text).then((ok) => {
                if (ok) setStatus(`📋 已复制 A* 预览点：${points.length} 个`, "#10b981");
            });
            return;
        }

        if (this.recording && this.recording.recording) return;

        const selected = [...this.state.selectedIndices].sort((a, b) => a - b);
        if (!selected.length) {
            setStatus("请先选中一个点再按 C 复制坐标。", "#f59e0b");
            return;
        }
        const zoneIndices = this.state.zonePointGlobalIndices();
        let text;
        let status;
        if (selected.length === 1) {
            const point = this.state.points[zoneIndices[selected[0]]];
            text = `[${compactNumber(point.x)}, ${compactNumber(point.y)}]`;
            const zone = normalizeZoneId(point.zone || "");
            status = `📋 已复制坐标: ${text}${zone ? `  (zone: ${zone})` : ""}`;
        } else {
            text = selected
                .map((idx) => {
                    const point = this.state.points[zoneIndices[idx]];
                    return `[${compactNumber(point.x)}, ${compactNumber(point.y)}]`;
                })
                .join(",\n");
            status = `📋 已复制 ${selected.length} 个点的坐标`;
        }
        this._copyText(text).then((ok) => {
            if (ok) setStatus(status, "#10b981");
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
        this.els.chkRequired.checked = false;
        this.els.targetTierEntry.value = "";
        this.els.actionChainLabel.textContent = "Run";
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
        if (!point) return "Run";
        return getPointActions(point)
            .map((action) => ACTION_NAMES[action] || "Run")
            .join(" -> ");
    }

    /** Reflect the current selection into the action/strict/chain controls (tk `_sync_action_controls`). */
    _syncActionControls() {
        this._renderWaypointList();
        // 路线可能刚被改过: 重新装载到试跑会话, 让 F3 跑的始终是屏幕上这一条。
        if (this.navtest) this.navtest.routeChanged();
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
            const requireds = new Set(points.map((p) => !!p.required));
            const targetTiers = new Set(points.map((p) => normalizeZoneId(p.target_tier || "")));
            if (chains.size === 1) {
                const actions = getPointActions(points[0]);
                this.els.actionMenu.value = ACTION_NAMES[actions[actions.length - 1]] || "Run";
            }
            if (stricts.size === 1) this.els.chkStrict.checked = [...stricts][0];
            if (requireds.size === 1) this.els.chkRequired.checked = [...requireds][0];
            this.els.targetTierEntry.value = targetTiers.size === 1 ? [...targetTiers][0] : "";
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
        this.els.actionMenu.value = ACTION_NAMES[actions[actions.length - 1]] || "Run";
        this.els.chkStrict.checked = !!point.strict;
        this.els.chkRequired.checked = !!point.required;
        this.els.targetTierEntry.value = normalizeZoneId(point.target_tier || "");
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
        host.textContent = "";

        if (!zoneIndices.length) {
            const empty = document.createElement("div");
            empty.className = "wp-empty";
            empty.textContent = "当前片段暂无路点";
            host.appendChild(empty);
            return;
        }

        const primary = this.state.selectedIdx;
        const selectedSet = this.state.selectedIndices;
        for (let idx = 0; idx < zoneIndices.length; idx += 1) {
            const point = this.state.points[zoneIndices[idx]];
            const actions = getPointActions(point);
            const displayAction = actions[actions.length - 1];
            const row = document.createElement("div");
            row.className = "wp-row";
            row.draggable = true;
            row.dataset.local = String(idx);
            if (selectedSet.has(idx)) row.classList.add("wp-row-selected");
            if (idx === primary) row.classList.add("wp-row-primary");

            const handle = document.createElement("span");
            handle.className = "wp-handle";
            handle.textContent = "⠿";

            const num = document.createElement("span");
            num.className = "wp-idx";
            num.textContent = actions.length > 1 ? `${idx}*` : String(idx);

            const dot = document.createElement("span");
            dot.className = "wp-dot";
            dot.style.background = ACTION_COLORS[displayAction] || "#64748b";

            const name = document.createElement("span");
            name.className = "wp-action";
            name.textContent = this._formatActionChain(point);

            const coord = document.createElement("span");
            coord.className = "wp-coord";
            coord.textContent = `${compactNumber(point.x)}, ${compactNumber(point.y)}`;

            row.append(handle, num, dot, name, coord);
            if (point.strict) {
                const strict = document.createElement("span");
                strict.className = "wp-strict";
                strict.textContent = "严";
                strict.title = "严格到达";
                row.appendChild(strict);
            }
            if (point.required) {
                const required = document.createElement("span");
                required.className = "wp-strict";
                required.textContent = "必";
                required.title = "路径必经边界";
                row.appendChild(required);
            }
            if (point.target_tier) {
                const tier = document.createElement("span");
                tier.className = "wp-tier";
                tier.textContent = "层";
                tier.title = `坐标层级: ${point.target_tier}`;
                row.appendChild(tier);
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

        host.addEventListener("click", (ev) => {
            const row = ev.target.closest(".wp-row");
            if (!row) return;
            const local = Number(row.dataset.local);
            this.state.setSelection([local], local);
            this._syncActionControls();
            this._focusLocalPoint(local);
        });

        host.addEventListener("dragstart", (ev) => {
            const row = ev.target.closest(".wp-row");
            if (!row) return;
            this._wpDragFrom = Number(row.dataset.local);
            row.classList.add("wp-row-dragging");
            ev.dataTransfer.effectAllowed = "move";
            ev.dataTransfer.setData("text/plain", row.dataset.local);
        });

        host.addEventListener("dragover", (ev) => {
            if (this._wpDragFrom == null) return;
            ev.preventDefault();
            ev.dataTransfer.dropEffect = "move";
            this._wpShowDropGap(this._wpDropGap(ev));
        });

        host.addEventListener("drop", (ev) => {
            if (this._wpDragFrom == null) return;
            ev.preventDefault();
            const gap = this._wpDropGap(ev);
            const from = this._wpDragFrom;
            this._wpDragFrom = null;
            this._reorderWaypoint(from, gap);
        });

        host.addEventListener("dragend", () => {
            this._wpDragFrom = null;
            for (const r of host.querySelectorAll(".wp-row")) {
                r.classList.remove("wp-row-dragging", "wp-drop-before", "wp-drop-after");
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
        const rows = [...this.els.waypointList.querySelectorAll(".wp-row")];
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
        const rows = [...this.els.waypointList.querySelectorAll(".wp-row")];
        for (const r of rows) r.classList.remove("wp-drop-before", "wp-drop-after");
        if (!rows.length) return;
        if (gap < rows.length) rows[gap].classList.add("wp-drop-before");
        else rows[rows.length - 1].classList.add("wp-drop-after");
    }

    /** Update the zone label for the current mode's frame. @returns {void} */
    _refreshZoneLabel() {
        if (this.state.mode === Mode.ASTAR) {
            const zoneId = this._displayZoneId();
            this.els.zoneLabel.textContent = zoneId ? `A*: ${zoneId}` : "A*: 请选择底图";
            return;
        }
        if (this.state.mode === Mode.ASSERT) {
            const zoneId = this._displayZoneId();
            this.els.zoneLabel.textContent = zoneId ? `Assert: ${zoneId}` : "Assert: 请选择地图";
            return;
        }
        if (this.state.mode === Mode.LOG) {
            const run = this.selectedLogRun;
            this.els.zoneLabel.textContent = run ? `Log: ${run.zone || "未知区域"}` : "Log: 未选择运行记录";
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
            this.els.astarSelectedTierLabel.textContent = this.els.astarZoneCombo.value || "未选择层级";
            this.els.astarSelectedTierLabel.style.display = "inline-block";
        } else {
            this.els.btnSelectTier.disabled = true;
            this.els.astarSelectedTierLabel.style.display = "none";
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
            "录制结束。滚轮缩放，右键或 Alt+拖拽平移；添加工具左键点击插点，拖拽路点微调，Ctrl+拖拽框选批量操作，C 键复制选中点坐标。",
            "#10b981",
        );
        this._fitView();
    }

    /**
     * {@link Importer} parsed a path import.
     * - EDIT / Assert: the points become the real route (Assert draws them read-only).
     * - A*: imported coordinates wait as targets until a manual start is clicked.
     * @param {object[]} points
     * @returns {{text?:string, color?:string}|void} a status lead-in replacing the importer's default
     */
    _importLoadPoints(points) {
        if (this.state.mode === Mode.ASTAR) return this._importAsAstarRoute(points);

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
        if (!drawn.length) return this._noNavmeshBasemapNote(points, "断言模式画不出这些点（路线已载入）");
    }

    /**
     * A* import: keep every coordinate on the first navmesh geometry as a pending target.
     * The next map click prepends a manual start and then plans through all targets in the
     * imported order. The editor's real route remains untouched.
     * @param {object[]} points
     * @returns {{text?:string, color?:string}|void} the status lead-in
     */
    _importAsAstarRoute(points) {
        if (!this.field || !points.length) return;

        const imported = collectAstarImportBasePoints(
            points,
            (zone) => this._resolveZoneId(zone),
            (zoneId) => this.field.geometryZoneId(zoneId),
            (zoneId, x, y) => this._pointToBase(zoneId, x, y),
        );
        if (imported.firstZoneId === null) return this._noNavmeshBasemapNote(points, "A* 模式无法规划这些点");

        // The zone switch clears A* state, so it must happen before installing imported waypoints.
        if (!this._selectDisplayZoneById(imported.firstZoneId))
            return this._noNavmeshBasemapNote(points, "A* 模式无法规划这些点");
        this._onAstarZoneChanged(false);

        const displayPoints = imported.basePoints.map(([x, y]) => this._baseToDisplay(x, y));
        const importedCount = displayPoints.length;
        const skippedNote = imported.skipped ? `，${imported.skipped} 个点不属于当前底图，已跳过` : "";
        if (importedCount === 0) {
            return {
                text: `没有可用于 A* 规划的坐标${skippedNote}`,
                color: "#f59e0b",
            };
        }
        this.astarPoints = [];
        this.astarDecks = [];
        this.astarRoute = null;
        this.astarLocateHints = imported.basePoints.map(([x, y], index) => ({
            x,
            y,
            label: importedCount === 1 ? "导入目标" : `导入点 ${index + 1}`,
            rot: null,
        }));
        this.astarPendingTargets = imported.basePoints.map((point) => point.slice(0, 2));
        this.astarPendingDecks = imported.decks.slice();
        this.hintDeck = null;
        this._setActiveTool(importedCount === 1 ? "astar-single" : "astar-multi");
        this._astarRouteChanged();
        this._refreshDeckProbe();
        this._fitDisplayPoints(displayPoints.map(([x, y]) => ({x, y})));
        return {
            text: `已导入 ${importedCount} 个目标点；请在地图上单击起点，随后将按导入顺序自动规划${skippedNote}`,
            color: "#3b82f6",
        };
    }

    /**
     * Status note for a route whose zone has no navmesh basemap —— such a zone lives in its
     * own image frame, which only 路径编辑 can show.
     * @param {object[]} points @param {string} what what the current mode cannot do
     * @returns {{text:string, color:string}}
     */
    _noNavmeshBasemapNote(points, what) {
        const zone = normalizeZoneId((points[0] && points[0].zone) || "") || "未知";
        return {
            text: `zone=${zone} 不是 navmesh 底图区域，${what}，可切到「路径编辑」模式查看`,
            color: "#f59e0b",
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
        const [
            x,
            y,
            w,
            h,
        ] = target;
        this.assertRectWorld = [
            x,
            y,
            x + w,
            y + h,
        ];
        this.isAssertSelecting = false;
        this._syncAssertControls();
        this._syncAstarControls();
        this._refreshZoneLabel();
        this._syncModeTabUI();
        if (this.navtest) this.navtest.routeChanged();
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
            const opt = document.createElement("option");
            opt.value = zoneId;
            opt.textContent = zoneId;
            combo.appendChild(opt);
        }
    }

    /**
     * Sidebar mode-tab click: switch to edit / assert / A* / log, clearing the other
     * modes' canvas artifacts and picking a default zone where needed.
     * @param {'edit'|'assert'|'astar'|'log'} modeName
     * @returns {void}
     */
    _selectModeTab(modeName) {
        const e = this.els;

        if (modeName === "log" && this.navtest && this.navtest.running) {
            setStatus("请先按 F4 终止当前实机试跑，再进入日志分析模式。", "#f59e0b");
            return;
        }

        if (modeName !== "astar") {
            this._clearAstarPreview();
        }
        if (modeName !== "assert") {
            this.assertRectWorld = null;
            this.isAssertSelecting = false;
        }

        if (modeName === "edit") {
            this.state.mode = Mode.EDIT;
            this._lastRouteMode = "edit";
            setStatus("返回路径编辑模式。", "#10b981");
        } else if (modeName === "assert") {
            this.state.mode = Mode.ASSERT;
            if (!this._availableZoneIds.length && !normalizeZoneId(this.state.currentZone())) {
                setStatus("未找到可用 zone 底图，无法进入断言模式。", "#ef4444");
                this._selectModeTab("edit");
                return;
            }
            if (!normalizeZoneId(e.assertZoneCombo.value)) {
                e.assertZoneCombo.value = this._defaultAssertZone();
            }
            this._lastRouteMode = "assert";
            setStatus("断言模式：先选地图，再用左键拖拽框出断言区域；Delete 或清除按钮可清除。", "#3b82f6");
        } else if (modeName === "astar") {
            this.state.mode = Mode.ASTAR;
            this._lastRouteMode = "astar";
            if (this.field) {
                if (!e.astarZoneCombo.value) {
                    this._applyDefaultAstarZoneSelection();
                }
                this._onAstarZoneChanged(false);
            }
            setStatus("A* 模式：左键点起点，再点终点生成预览路线。", "#3b82f6");
        } else if (modeName === "log") {
            this.state.mode = Mode.LOG;
            this._showSelectedLogRun({fit: true});
            setStatus(
                this.selectedLogRun
                    ? "日志分析模式：默认单击查看点位；右上角可开启滑索架测距，拖动仍可平移。"
                    : "日志分析模式：请选择 MaaEnd ZIP 分包或解压后的 cpp-algo/debug/maafw*.log。",
                "#3b82f6",
            );
        }

        this._syncAssertControls();
        this._syncAstarControls();
        this._syncActionControls();
        this._refreshZoneLabel();
        this._syncModeTabUI();
        if (this.navtest) this.navtest.routeChanged();
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

        e.tabEdit.classList.remove("active");
        e.tabAstar.classList.remove("active");
        e.tabAssert.classList.remove("active");
        e.tabLog.classList.remove("active");
        e.tabRoute.classList.remove("active");

        const logWorkspace = mode === Mode.LOG;
        e.tabLog.classList.toggle("active", logWorkspace);
        e.tabLog.setAttribute("aria-pressed", String(logWorkspace));
        e.tabRoute.classList.toggle("active", !logWorkspace);
        e.tabRoute.setAttribute("aria-pressed", String(!logWorkspace));
        e.panelConnection.hidden = logWorkspace;
        e.panelNavtest.hidden = logWorkspace;
        e.routeModeTabs.hidden = logWorkspace;
        e.positionReadout.hidden = logWorkspace;
        e.btnLogMeasure.hidden = !logWorkspace;
        e.logMeasureDivider.hidden = !logWorkspace;
        if (this.connection) this.connection.setSuspended(logWorkspace);

        e.panelRecording.hidden = true;
        e.panelProperties.hidden = true;
        e.panelAstar.hidden = true;
        e.panelAssert.hidden = true;
        e.panelLog.hidden = true;
        e.btnDelPointFloat.hidden = mode === Mode.LOG;
        if (this.navtest) this.navtest.setDisabled(mode === Mode.LOG);

        if (mode === Mode.ASTAR) {
            e.tabAstar.classList.add("active");
            e.panelAstar.hidden = false;
            e.canvasWrap.classList.remove("mode-edit", "mode-assert", "mode-log");
            e.canvasWrap.classList.add("mode-astar");
            document.body.classList.remove("mode-edit", "mode-assert", "mode-log");
            document.body.classList.add("mode-astar");
            this._setActiveTool("astar-single");
        } else if (mode === Mode.ASSERT) {
            e.tabAssert.classList.add("active");
            e.panelAssert.hidden = false;
            e.canvasWrap.classList.remove("mode-edit", "mode-astar", "mode-log");
            e.canvasWrap.classList.add("mode-assert");
            document.body.classList.remove("mode-edit", "mode-astar", "mode-log");
            document.body.classList.add("mode-assert");
            this._setActiveTool("assert-edit");
        } else if (mode === Mode.LOG) {
            e.panelLog.hidden = false;
            e.canvasWrap.classList.remove("mode-edit", "mode-astar", "mode-assert");
            e.canvasWrap.classList.add("mode-log");
            document.body.classList.remove("mode-edit", "mode-astar", "mode-assert");
            document.body.classList.add("mode-log");
            this._setActiveTool(
                this.activeTool === "log-measure" || this.activeTool === "log-inspect"
                    ? this.activeTool
                    : "log-inspect",
            );
        } else {
            e.tabEdit.classList.add("active");
            e.panelRecording.hidden = false;
            e.panelProperties.hidden = false;
            e.canvasWrap.classList.remove("mode-astar", "mode-assert", "mode-log");
            e.canvasWrap.classList.add("mode-edit");
            document.body.classList.remove("mode-astar", "mode-assert", "mode-log");
            document.body.classList.add("mode-edit");
            this._setActiveTool("add");
        }
        e.tabAstar.setAttribute("aria-pressed", String(mode === Mode.ASTAR));
        e.tabAssert.setAttribute("aria-pressed", String(mode === Mode.ASSERT));
        e.tabEdit.setAttribute("aria-pressed", String(mode === Mode.EDIT));
    }

    /** Open the tier-picker dialog with base buttons + the current base's tier grid. @returns {void} */
    _openTierPicker() {
        if (!this.field) return;
        const e = this.els;
        const basesContainer = e.tierPickerBases;
        const gridContainer = e.tierPickerGrid;
        basesContainer.textContent = "";
        gridContainer.textContent = "";

        const baseNames = this.field.displayBaseNames();
        if (!baseNames.length) return;

        const currentBase = this._displayZoneId()
            ? this.field.geometryZoneId(this.field.zoneByName(this._displayZoneId())?.zone_id || this._displayZoneId())
            : null;
        const currentBaseZone = currentBase ? this.field.zoneById(currentBase) : null;
        const defaultActiveBase =
            currentBaseZone && baseNames.includes(currentBaseZone.name) ? currentBaseZone.name : baseNames[0];

        baseNames.forEach((name) => {
            const btn = document.createElement("button");
            btn.type = "button";
            btn.className = "tier-base-btn";
            if (name === defaultActiveBase) btn.classList.add("active");
            btn.textContent = name;
            btn.addEventListener("click", () => {
                basesContainer.querySelectorAll(".tier-base-btn").forEach((b) => b.classList.remove("active"));
                btn.classList.add("active");
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
        gridContainer.textContent = "";

        const choices = this.field.zoneChoicesForBase(baseName);

        let currentSelectedLabel = "";
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

        choices.forEach((choice) => {
            const card = document.createElement("div");
            card.className = "tier-card";
            const isActive =
                activeDisplayTierId === choice.id ||
                (activeDisplayTierId === null && choice.label === currentSelectedLabel);
            if (isActive) card.classList.add("active");

            const thumb = document.createElement("div");
            thumb.className = "tier-card-thumb";
            thumb.style.backgroundImage = `url('/basemap-by-zone?zone_id=${encodeURIComponent(choice.name)}')`;
            card.appendChild(thumb);

            if (isActive) {
                const badge = document.createElement("div");
                badge.className = "tier-card-badge";
                badge.textContent = "当前";
                card.appendChild(badge);
            }

            const info = document.createElement("div");
            info.className = "tier-card-info";

            const splitIdx = choice.label.indexOf(":");
            const idText = splitIdx !== -1 ? choice.label.substring(0, splitIdx) : "";
            const nameText = splitIdx !== -1 ? choice.label.substring(splitIdx + 1) : choice.label;

            const title = document.createElement("div");
            title.className = "tier-card-title";
            title.textContent = nameText;
            info.appendChild(title);

            const desc = document.createElement("div");
            desc.className = "tier-card-desc";
            desc.textContent = `ID: ${idText}`;
            info.appendChild(desc);

            card.appendChild(info);

            card.addEventListener("click", () => {
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
if (document.readyState === "loading") {
    document.addEventListener("DOMContentLoaded", () => appInstance.boot());
} else {
    appInstance.boot();
}
