/**
 * main.js — keystone orchestrator. Boots the app and owns every piece of glue the
 * other modules don't: the render loop (`_doRedraw` / `_paint`, mirroring
 * app_tk `_do_redraw`), the pointer state machine (`on_click`/`on_drag`/`on_release`
 * + right-button pan), wheel/keyboard, the three mutually-exclusive modes
 * (edit / assert / log analysis), zone navigation, fit-view, the copy actions, and the wiring
 * of the {@link ConnectionPanel} / {@link RecordingController} / {@link Importer}
 * controllers.
 *
 * Rendering is a hybrid stack sharing ONE {@link Camera}: the WebGL {@link Renderer}
 * draws basemap + mesh + walkable-dot layers, the 2D {@link Overlay} draws the
 * state-coupled vectors (path / nodes / assert rect / route preview / selection box).
 *
 * Coordinate frames (DESIGN §9): edit/assert basemaps live in their zone's own px.
 * A translated tier is displayed in tier-px while routing/snap always run in base-px
 * on the parent geometry zone.
 *
 * @module main
 */

import {Camera} from "./camera.js";
import {Renderer} from "./gl/renderer.js";
import {Overlay} from "./gl/overlay.js";
import {formatHeading, transformHeading} from "./heading.js";
import {buildEditPreviewPlan} from "./edit_preview.js";
import {advanceQuickRouteTest, buildQuickRouteTestRequest} from "./quick_route_test.js";
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
    matchTargetDeckHeight,
    normalizeZoneId,
} from "./model.js";
import {compactNumber, roundHalfEven} from "./rounding.js";
import {initFeedback, setStatus} from "./ui/toast.js";
import {nextWheelSelectIndex} from "./ui/select.js";
import {ConnectionPanel} from "./ui/connection.js";
import {RecordingController} from "./ui/recording.js";
import {NavTestController} from "./ui/navtest.js";
import {Importer} from "./ui/importer.js";
import {PositionReadout} from "./ui/position.js";
import {
    getZones,
    getLoadStatus,
    getMesh,
    getZoneIds,
    getZiplineFrames,
    basemapByZoneUrl,
    postRoutePreview,
    postOffMeshProbe,
    postDeckProbe,
    exportPath,
    exportAssert,
    locateOnce,
    RecordingSocket,
} from "./rpc.js";

const DRAG_ACTIVATION_DISTANCE = 4; // px (tk RouteEditorApp.DRAG_ACTIVATION_DISTANCE)
const NAVMESH_PROBE_SNAP_RADIUS = 5.0;
const LOAD_POLL_MS = 400;
// CSS px the floating left panel overlays the canvas; fit-view centers in the rest.
const LEFT_PANEL_FIT_OFFSET = 350;
// World px kept around locate markers when framing them.
const LOCATE_HINT_FIT_PADDING = 200;
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
        /** @type {'assert'|'edit'} route tool restored after leaving log analysis. */
        this._lastRouteMode = "edit";

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
        this.isEditPreviewStartDragCandidate = false;
        this.isEditPreviewStartDragging = false;
        this.pointerDownX = 0;
        this.pointerDownY = 0;
        this.dragStartX = 0;
        this.dragStartY = 0;
        this.boxStartX = 0;
        this.boxStartY = 0;
        this.assertStartWorldX = 0;
        this.assertStartWorldY = 0;

        // --- edit / assert view state ---
        /** @type {?{x:number,y:number,rot:?number,label:string,geometryZoneId:number}} edit locate hint in base px. */
        this.editLocateHint = null;
        /** @type {?number[]} raw drag rect `[x0,y0,x1,y1]` in display-frame world. */
        this.assertRectWorld = null;
        /** @type {?number[]} assert rect restored when Escape cancels an in-progress redraw. */
        this._assertRectBeforeDrag = null;
        /** @type {?{x:number,y:number,rot:?number,label:string}} assert locate hint in base px. */
        this.assertLocateHint = null;
        /** @type {?{x0:number,y0:number,x1:number,y1:number}} canvas-px selection box. */
        this.selectionRect = null;
        /** @type {?{globalIndex:number, decks:Array<{height:number, band:number[], thin:boolean}>}} */
        this.editDeckProbe = null;
        /** @type {?number} 正在预览的那一层高度。 */
        this.deckPreview = null;
        this._editDeckToken = 0;
        /** @type {?string} 当前路径编辑 NAVMESH 路点的重叠面探针签名。 */
        this._editDeckSig = null;
        /** @type {?number} 路点拖动时重叠面探针的防抖句柄。 */
        this._editDeckTimer = null;
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
        this._syncNavTimingLabels();
        this.els.navDebugOptions.open = false;
        /** @type {Array<{x:number,y:number,rot:?number}>} measured points in base px. */
        this.livePathBase = [];
        /** @type {?{x:number,y:number,rot:?number}} latest measured position in base px. */
        this.livePositionBase = null;
        this._initialLiveHeightColored = false;
        /**
         * EDIT-mode off-mesh badges, in the points' own zone frame. Same shape as
         * {@link offMeshMarks} but always `exact:false` — a waypoint is a *goal*, and the
         * runtime's blind walk to a goal depends on where it starts from.
         * @type {Array<Object>}
         */
        this.editOffMeshMarks = [];
        /** Runtime-expanded preview and selected-leg diagnostics for the current EDIT segment, in base px. */
        this.editRoute = null;
        /** Runtime-reported failed leg for the current EDIT segment, in base px. */
        this.editRouteFailure = null;
        /** @type {?{x:number,y:number,position:number[],positionZone:string,geometryZoneId:number,segmentIndex:number}} */
        this.editPreviewStart = null;
        this.editPreviewStartSelected = false;
        /** @type {?string} tool restored after the one-shot manual-start click. */
        this._editPreviewStartReturnTool = null;
        /** @type {?{start:Object,goal:?Object}} two-click route test, isolated from authored points. */
        this.quickRouteTest = null;
        this.quickRouteTestRoute = null;
        this.quickRouteTestFailure = null;
        this._quickRouteTestToken = 0;
        this._quickRouteTestPending = false;
        /** Guards against stale edit previews replacing a newer request or route state. */
        this._editRouteToken = 0;
        this._editRoutePending = false;
        /** Guards against a stale off-mesh probe overwriting newer edit state. */
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

        // --- navmesh bookkeeping ---
        /** @type {?string} `${geomId}:${tierId}` of the uploaded mesh. */
        this._meshKey = null;
        this._meshToken = 0;
        this.viewMode = "2d";
        this.threeNavigationMode = "free";
        this.threeView = null;
        this._threeViewPromise = null;
        /** @type {?{key:string,buffer:ArrayBuffer}} current display-frame NMSH payload. */
        this._latest3DMesh = null;

        this._cssW = 800;
        this._cssH = 600;
        this._pointerMoveBound = (e) => this._onPointerMove(e);
        this._pointerUpBound = (e) => this._onPointerUp(e);
        this._animating = false;
    }

    /** Resolve every DOM element main.js touches. @returns {Object} */
    _queryElements() {
        const $ = (id) => document.getElementById(id);
        return {
            app: $("app"),
            glCanvas: $("gl-canvas"),
            overlayCanvas: $("overlay-canvas"),
            threeCanvas: $("three-canvas"),
            canvasWrap: $("canvas-wrap"),
            viewModeToggle: $("view-mode-toggle"),
            viewMode2d: $("view-mode-2d"),
            viewMode3d: $("view-mode-3d"),
            viewModeDivider: $("view-mode-divider"),
            threeNavigationRow: $("three-navigation-row"),
            threeNavigationMode: $("three-navigation-mode"),
            threeSpeedRow: $("three-speed-row"),
            threeFlightSpeed: $("three-flight-speed"),
            threeFlightSpeedValue: $("three-flight-speed-value"),
            threeRecolorRow: $("three-recolor-row"),
            btnThreeRecolor: $("btn-three-recolor"),
            threeNavDebugOptions: $("three-nav-debug-options"),
            threeChkNavDebugSearch: $("three-chk-nav-debug-search"),
            threeChkNavDebugRerouted: $("three-chk-nav-debug-rerouted"),
            threeChkNavDebugStringPull: $("three-chk-nav-debug-string-pull"),
            threeChkNavDebugAssembled: $("three-chk-nav-debug-assembled"),
            threeChkNavDebugLoopFixed: $("three-chk-nav-debug-loop-fixed"),
            threeChkNavDebugSlim: $("three-chk-nav-debug-slim"),
            threeChkNavDebugWidenCorners: $("three-chk-nav-debug-widen-corners"),
            threeChkNavDebugPlanned: $("three-chk-nav-debug-planned"),
            threeChkNavDebugLivePath: $("three-chk-nav-debug-live-path"),
            btnStart: $("btn-start"),
            btnStop: $("btn-stop"),
            btnCopyPath: $("btn-copy-path"),
            btnEditPlan: $("btn-edit-plan"),
            btnEditPlanClear: $("btn-edit-plan-clear"),
            editPlanStartLabel: $("edit-plan-start-label"),
            chkEditZipline: $("chk-edit-zipline"),
            chkEditExactSlim: $("chk-edit-exact-slim"),
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
            editDeckBox: $("edit-deck-box"),
            editDeckTitle: $("edit-deck-title"),
            editDeckList: $("edit-deck-list"),
            assertZoneCombo: $("assert-zone-combo"),
            chkStrict: $("chk-strict"),
            chkRequired: $("chk-required"),
            toolPan: $("tool-pan"),
            toolAdd: $("tool-add"),
            toolSelect: $("tool-select"),
            toolRouteTest: $("tool-route-test"),
            toolEditStart: $("tool-edit-start"),
            editStartDivider: $("edit-start-divider"),
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
            displayZoneCombo: $("display-zone-combo"),
            displayTierCombo: $("display-tier-combo"),
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
            tabAssert: $("tab-assert"),
            tabLog: $("tab-log"),
            btnClearAssert: $("btn-clear-assert"),
            btnSelectTier: $("btn-select-tier"),
            btnSelectAssertTier: $("btn-select-assert-tier"),
            editSelectedTierLabel: $("edit-selected-tier-label"),
            assertSelectedTierLabel: $("assert-selected-tier-label"),
            tierPickerDialog: $("tier-picker-dialog"),
            tierPickerBases: $("tier-picker-bases"),
            tierPickerGrid: $("tier-picker-grid"),
            tierPickerCancel: $("tier-picker-cancel"),
            btnFitView: $("btn-fit-view"),
            btnDelPointFloat: $("btn-del-point-float"),
            editDeleteDivider: $("edit-delete-divider"),
            propertiesEmptyState: $("properties-empty-state"),
            propertiesEditor: $("properties-editor"),
            panelRecording: $("panel-recording"),
            panelEditMap: $("panel-edit-map"),
            panelAssertMap: $("panel-assert-map"),
            panelConnection: $("panel-connection"),
            panelNavtest: $("panel-navtest"),
            routeModeTabs: $("route-mode-tabs"),
            btnNavtestRun: $("btn-navtest-run"),
            btnNavtestStop: $("btn-navtest-stop"),
            navtestArmed: $("navtest-armed"),
            navtestHotkeyNote: $("navtest-hotkey-note"),
            navtestOverlay: $("navtest-overlay"),
            panelProperties: $("panel-properties"),
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
            editInspectionBox: $("edit-inspection-box"),
            editPointSummary: $("edit-point-summary"),
            btnEditSelectionClear: $("btn-edit-selection-clear"),
            logInspectionBox: $("log-inspection-box"),
            btnLogPointClear: $("btn-log-point-clear"),
            logPointSummary: $("log-point-summary"),
            logDistanceBox: $("log-distance-box"),
            btnLogDistanceClear: $("btn-log-distance-clear"),
            logDistanceSummary: $("log-distance-summary"),
            logDecisionSummary: $("log-decision-summary"),
            btnLogMeasure: $("btn-log-measure"),
            logMeasureDivider: $("log-measure-divider"),
            btnEditLocate: $("btn-edit-locate"),
            btnLiveLocate: $("btn-live-locate"),
            btnAssertLocate: $("btn-assert-locate"),
            waypointList: $("waypoint-list"),
            btnAssertImport: $("btn-assert-import"),
            btnAssertReadClipboard: $("btn-assert-read-clipboard"),
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
                onBeforeOpen: () => this._stopLiveLocate(),
                onRunState: (running) => {
                    if (running) {
                        this._clearLivePath();
                        this.showLivePath = true;
                        this.positionReadout.setPending("正在获取实时位置与朝向...");
                        this._paint();
                    }
                },
            });
        this.connection.onStatusChange((connected) => this._syncLocateActions(connected));
        this.liveLocateSocket = null;
        this._liveLocateAutoStarted = false;
        this.els.btnLiveLocate.addEventListener("click", () => this._toggleLiveLocate());
        this.connection.onStatusChange((connected) => {
            if (connected && !this._liveLocateAutoStarted && !this.liveLocateSocket) {
                this._liveLocateAutoStarted = true;
                this._toggleLiveLocate();
            }
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
                    loadPoints: (points, options) => this._importLoadPoints(points, options),
                    applyAssert: (zoneId, target) => this._importApplyAssert(zoneId, target),
                },
            );
            this.importer.init();
            this.connection.init(); // async; settles the connection row on its own

            this._resize();
            this._observeResize();
            this._syncAssertControls();
            this._syncMapControls();
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
        this._populateDisplayZoneCombo();
        this._syncMapControls();
        this._refreshZoneLabel();
        if (this.state.mode === Mode.EDIT && !this.state.points.length) {
            this._applyDefaultDisplayZoneSelection();
            this._onDisplayTierChanged();
        } else if (this.state.mode === Mode.LOG && this.selectedLogRun) {
            this._showSelectedLogRun({fit: true});
        }
    }

    /**
     * Pick a sensible display-zone/tier selection: the current edit zone when it
     * maps to a known base, otherwise the first base whose name contains `map01`,
     * otherwise the first base. Updates the combos only — callers follow up with
     * {@link MapNavigatorApp#_onDisplayTierChanged} to load the mesh and sync labels.
     * @returns {void}
     */
    _applyDefaultDisplayZoneSelection() {
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
            this.els.displayZoneCombo.value = matchedBaseName;
            this._refreshDisplayTierChoices();
            if (matchedLabel) {
                this.els.displayTierCombo.value = matchedLabel;
            }
        } else {
            if (!normalizeZoneId(this.els.displayZoneCombo.value)) {
                this.els.displayZoneCombo.value = this._defaultDisplayZone();
            }
            this._refreshDisplayTierChoices();
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

    /** Refill the display-zone combo with the field's base names, keeping the selection. @returns {void} */
    _populateDisplayZoneCombo() {
        if (!this.field) return;
        const names = this.field.displayBaseNames();
        const combo = this.els.displayZoneCombo;
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
        this._refreshDisplayTierChoices();
    }

    /** Repopulate the tier dropdown with the current base's tiers. */
    _refreshDisplayTierChoices() {
        if (!this.field) return;
        const choices = this.field.zoneChoicesForBase(this._displayZoneId());
        const combo = this.els.displayTierCombo;
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
        const controls = [
            [this.els.chkNavDebugSearch, "search"],
            [this.els.chkNavDebugRerouted, "rerouted"],
            [this.els.chkNavDebugStringPull, "stringPull"],
            [this.els.chkNavDebugAssembled, "assembled"],
            [this.els.chkNavDebugLoopFixed, "loopFixed"],
            [this.els.chkNavDebugSlim, "slim"],
            [this.els.chkNavDebugWidenCorners, "widenCorners"],
            [this.els.chkNavDebugPlanned, "planned"],
            [this.els.chkNavDebugLivePath, "live"],
        ];
        for (const [entry, key] of controls) {
            entry.checked = Boolean(this.navDebug[key]);
        }
        const threeControls = [
            [this.els.threeChkNavDebugSearch, "search"],
            [this.els.threeChkNavDebugRerouted, "rerouted"],
            [this.els.threeChkNavDebugStringPull, "stringPull"],
            [this.els.threeChkNavDebugAssembled, "assembled"],
            [this.els.threeChkNavDebugLoopFixed, "loopFixed"],
            [this.els.threeChkNavDebugSlim, "slim"],
            [this.els.threeChkNavDebugWidenCorners, "widenCorners"],
            [this.els.threeChkNavDebugPlanned, "planned"],
            [this.els.threeChkNavDebugLivePath, "live"],
        ];
        for (const [entry, key] of threeControls) entry.checked = Boolean(this.navDebug[key]);
        this.showLivePath = this.navDebug.live;
    }

    /** Show the cumulative planner time for every visible diagnostic stage. */
    _syncNavTimingLabels() {
        const diagnostics = (this.quickRouteTestRoute || this.editRoute)?.diagnostics || [];
        const totals = new Map();
        for (const diagnostic of diagnostics) {
            for (const [stage, value] of Object.entries(diagnostic.timing_ms || {})) {
                if (Number.isFinite(value)) totals.set(stage, (totals.get(stage) || 0) + value);
            }
        }
        const count = diagnostics.length;
        for (const entry of document.querySelectorAll("[data-nav-timing]")) {
            const value = totals.get(entry.dataset.navTiming);
            entry.textContent = Number.isFinite(value) ? `${value >= 10 ? value.toFixed(1) : value.toFixed(2)} ms` : "—";
            entry.title = count > 0 ? `当前预览的 ${count} 条步行规划腿合计耗时` : "尚未规划";
        }
    }

    /** Attach every DOM event listener (buttons, combos, tabs, canvas, keyboard). @returns {void} */
    _wireEvents() {
        const e = this.els;
        e.btnCopyPath.addEventListener("click", () => this._copyPath());
        e.btnEditPlan.addEventListener("click", () => this._calculateEditPreview());
        e.btnEditPlanClear.addEventListener("click", () => {
            if (this._hasQuickRouteTest()) {
                this._clearQuickRouteTest();
                setStatus("已清除快速测试。", "#10b981");
            } else {
                this._clearEditPreview();
                setStatus("已清除当前片段的规划预览。", "#10b981");
            }
            this._paint();
        });
        e.toolEditStart.addEventListener("click", () => {
            const returnTool =
                this.activeTool === "edit-start" || this.activeTool === "route-test" ? "add" : this.activeTool;
            this._clearQuickRouteTest();
            this._editPreviewStartReturnTool = returnTool;
            this._setActiveTool("edit-start");
            this._paint();
            setStatus("请在地图上点击规划起点。", "#3b82f6");
        });
        e.chkEditZipline.addEventListener("change", () => {
            this._syncCopyButtonLabels();
            if (this.navtest) this.navtest.routeChanged();
            if (this.quickRouteTest?.goal) this._calculateQuickRouteTest();
            else if (this.editRoute) this._calculateEditPreview();
        });
        e.chkEditExactSlim.addEventListener("change", () => {
            if (this.navtest) this.navtest.routeChanged();
            if (this.quickRouteTest?.goal) this._calculateQuickRouteTest();
            else if (this.editRoute) this._calculateEditPreview();
        });
        e.btnCopyAssert.addEventListener("click", () => this._copyAssert());
        e.assertCopyFormat.addEventListener("change", () => this._syncCopyButtonLabels());
        e.btnPrev.addEventListener("click", () => this._prevZone());
        e.btnNext.addEventListener("click", () => this._nextZone());
        e.btnZoomOut.addEventListener("click", () => this._zoomOut());
        e.btnZoomIn.addEventListener("click", () => this._zoomIn());
        e.viewMode2d.addEventListener("click", () => this._setViewMode("2d"));
        e.viewMode3d.addEventListener("click", () => this._setViewMode("3d"));
        e.threeNavigationMode.addEventListener("change", () =>
            this._setThreeNavigationMode(e.threeNavigationMode.value),
        );
        e.threeFlightSpeed.addEventListener("input", () => this._setThreeFlightSpeed(e.threeFlightSpeed.value));
        e.btnThreeRecolor.addEventListener("click", () => this._recolorThreeByLiveHeight());
        e.btnApplyAction.addEventListener("click", () => this._applyAction());
        e.assertZoneCombo.addEventListener("change", () => this._onAssertZoneChanged());
        e.displayZoneCombo.addEventListener("change", () => this._onDisplayZoneChanged());
        e.displayTierCombo.addEventListener("change", () => this._onDisplayTierChanged());
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
                this._syncThreeOverlays();
            });
        }
        e.chkNavDebugLivePath.addEventListener("change", () => {
            this.showLivePath = e.chkNavDebugLivePath.checked;
            this.navDebug.live = this.showLivePath;
            localStorage.setItem("maaend.mapnavigator.showLivePath", this.showLivePath ? "1" : "0");
            this._paint();
            this._syncThreeOverlays();
        });
        for (const [entry, key] of [
            [e.threeChkNavDebugSearch, "search"],
            [e.threeChkNavDebugRerouted, "rerouted"],
            [e.threeChkNavDebugStringPull, "stringPull"],
            [e.threeChkNavDebugAssembled, "assembled"],
            [e.threeChkNavDebugLoopFixed, "loopFixed"],
            [e.threeChkNavDebugSlim, "slim"],
            [e.threeChkNavDebugWidenCorners, "widenCorners"],
            [e.threeChkNavDebugPlanned, "planned"],
        ]) {
            entry.addEventListener("change", () => {
                this.navDebug[key] = entry.checked;
                localStorage.setItem(`maaend.mapnavigator.debug${key[0].toUpperCase()}${key.slice(1)}`, entry.checked ? "1" : "0");
                this._syncNavDebugControls();
                this._paint();
                this._syncThreeOverlays();
            });
        }
        e.threeChkNavDebugLivePath.addEventListener("change", () => {
            this.showLivePath = e.threeChkNavDebugLivePath.checked;
            this.navDebug.live = this.showLivePath;
            localStorage.setItem("maaend.mapnavigator.showLivePath", this.showLivePath ? "1" : "0");
            this._syncNavDebugControls();
            this._paint();
            this._syncThreeOverlays();
        });
        e.btnEditLocate.addEventListener("click", () => this._onLocateCurrentPosition("edit"));
        e.btnAssertLocate.addEventListener("click", () => this._onLocateCurrentPosition("assert"));
        e.btnAssertImport.addEventListener("click", () => this.importer.openProjectPicker("assert"));
        e.btnEditReadClipboard.addEventListener("click", () => this.importer.readClipboard());
        e.btnAssertReadClipboard.addEventListener("click", () => this.importer.readClipboard());
        e.tabRoute.addEventListener("click", () => this._selectModeTab(this._lastRouteMode));
        e.tabEdit.addEventListener("click", () => this._selectModeTab("edit"));
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
        e.btnEditSelectionClear.addEventListener("click", () => {
            this._clearEditSelection();
            setStatus("已取消当前选择。", "#10b981");
        });
        e.btnLogPointClear.addEventListener("click", () => {
            this._clearLogInspection();
            setStatus("已取消点位选择。", "#10b981");
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
        e.toolPan.addEventListener("click", () => this._activateEditTool("pan"));
        e.toolAdd.addEventListener("click", () => this._activateEditTool("add"));
        e.toolSelect.addEventListener("click", () => this._activateEditTool("select"));
        e.toolRouteTest.addEventListener("click", () => {
            this._setActiveTool("route-test");
            setStatus(
                this.quickRouteTest?.start && !this.quickRouteTest.goal
                    ? "请在地图上单击测试终点。"
                    : "请在地图上依次单击测试起点和终点。",
                "#3b82f6",
            );
        });
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
                } else if (this.state.mode === Mode.LOG) {
                    this._setActiveTool("log-pan");
                }
            }
        });

        window.addEventListener("keyup", (e) => {
            if (this.threeView?.setMovementKey(e.code, false, {ctrlKey: e.ctrlKey})) {
                e.preventDefault();
            }
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
        if (this.threeView) this.threeView.resize(cssW, cssH, dpr);
        this._paint();
    }

    // ==================================================================================
    //  Frame identity (mirrors app_tk._display_zone_id / _render_background_zone / tiers)
    // ==================================================================================

    /** @returns {string} the zone id string for the current mode's display frame. */
    _displayZoneId() {
        if (this.state.mode === Mode.ASSERT || this.state.mode === Mode.LOG) {
            return normalizeZoneId(this.els.displayZoneCombo.value, this._defaultDisplayZone());
        }
        return normalizeZoneId(
            this.state.currentZone(),
            normalizeZoneId(this.els.displayZoneCombo.value, this._defaultDisplayZone()),
        );
    }

    /** @returns {number} zone id parsed from the tier combo's `"id:name"` value, or NaN. */
    _displayTierZoneId() {
        const raw = this.els.displayTierCombo.value || "";
        const head = raw.split(":", 1)[0];
        return parseInt(head, 10);
    }

    /** @returns {?number} zone id of the translated tier backing the canvas, else null. */
    _activeDisplayTierId() {
        if (!this.field) return null;
        const editZone = this.state.mode === Mode.EDIT ? normalizeZoneId(this.state.currentZone()) : "";
        const zoneId = editZone ? this._resolveZoneId(editZone) : this._displayTierZoneId();
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
    _defaultDisplayZone() {
        const cur = normalizeZoneId(this.els.displayZoneCombo.value);
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

    /** Draw the current frame — syncs the navmesh, requests the GL render, draws the overlay. */
    _paint() {
        this._syncNavmesh();

        const mode = this.state.mode;
        // Assert shows the real points of the displayed map read-only; EDIT shows its own zone segment.
        let overlayPoints = [];
        if (mode === Mode.EDIT) overlayPoints = this._currentSegmentPoints();
        else if (mode === Mode.ASSERT) overlayPoints = this._displayRealPoints();

        if (mode === Mode.EDIT) {
            this._scheduleEditOffMeshProbe();
            this._scheduleEditDeckProbe();
        }

        const displayEditLocateHint = mode === Mode.EDIT ? this._editLocateHintForDisplay() : null;
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
        const displayLogAnalysis = mode === Mode.LOG ? this._logAnalysisForDisplay() : null;
        const displayQuickRouteTest = mode === Mode.EDIT ? this._quickRouteTestForDisplay() : null;
        const displayEditPreview =
            mode === Mode.EDIT
                ? displayQuickRouteTest
                    ? this._editPreviewForDisplay(this.quickRouteTestRoute, this.quickRouteTestFailure)
                    : this._editPreviewForDisplay()
                : null;
        const displayEditPreviewStart =
            mode === Mode.EDIT && !displayQuickRouteTest ? this._editPreviewStartForDisplay() : null;
        const displayLivePath = mode === Mode.EDIT ? this._livePathForDisplay() : null;

        const vm = {
            mode,
            points: overlayPoints,
            // Selection is local to the EDIT segment.
            selectedIdx: mode === Mode.EDIT ? this.state.selectedIdx : null,
            selectedIndices: mode === Mode.EDIT ? this.state.selectedIndices : new Set(),
            editPreview: displayEditPreview,
            editPreviewStart: displayEditPreviewStart,
            quickRouteTest: displayQuickRouteTest,
            assertTarget: mode === Mode.ASSERT ? this._assertTargetForDisplay() : null,
            selectionRect: this.selectionRect,
            livePath: displayLivePath,
            editLocateHint: displayEditLocateHint,
            assertLocateHint: displayAssertLocateHint,
            logAnalysis: displayLogAnalysis,
            offMeshMarks: mode === Mode.EDIT ? this.editOffMeshMarks : [],
        };
        this.renderer.requestRender(this.camera);
        this.overlay.render(this.camera, vm);
    }

    /** @returns {Array<Object>} the current zone segment's point objects. */
    _currentSegmentPoints() {
        return this.state.zonePointGlobalIndices().map((idx) => this.state.points[idx]);
    }

    /** Runtime preview projected from base px into the current EDIT segment's display frame. */
    _editPreviewForDisplay(route = this.editRoute, failure = this.editRouteFailure) {
        if ((!route && !failure) || !this.field) return null;
        const project = (point) => this._baseToDisplay(point[0], point[1]);
        const previewRoute = route || {};
        return {
            previewPoints: (previewRoute.points || []).map(project),
            segmentBreaks: [],
            hasRoute: !!route,
            waypoints: [],
            blindWalks: [],
            diagnostics: this._diagnosticsForDisplay(previewRoute.diagnostics || []),
            debugOptions: this.navDebug,
            showPlannedPath: this.navDebug.planned,
            walkSegments: (previewRoute.walk_segments || []).map((segment) => segment.map(project)),
            ziplineSegments: (previewRoute.zipline_segments || [])
                .filter((segment) => Array.isArray(segment?.from) && Array.isArray(segment?.to))
                .map((segment) => ({
                    ...segment,
                    from: project(segment.from),
                    to: project(segment.to),
                    mount_restand: Array.isArray(segment.mount_restand) ? project(segment.mount_restand) : null,
                })),
            failure: failure
                ? {
                      ...failure,
                      segment_start: failure.segment_start ? project(failure.segment_start) : null,
                      segment_goal: failure.segment_goal ? project(failure.segment_goal) : null,
                      gap_start: failure.gap_start ? project(failure.gap_start) : null,
                      gap_goal: failure.gap_goal ? project(failure.gap_goal) : null,
                  }
                : null,
        };
    }

    /** The current segment's manual preview start projected from base px into the display frame. */
    _editPreviewStartForDisplay() {
        const start = this._activeEditPreviewStart();
        if (!start) return null;
        const [x, y] = this._baseToDisplay(start.x, start.y);
        return {x, y, label: "规划起点", selected: this.editPreviewStartSelected};
    }

    /** The active quick-test endpoints projected from base px into the display frame. */
    _quickRouteTestForDisplay() {
        const test = this.quickRouteTest;
        if (!test?.start || !this.field || test.start.segmentIndex !== this.state.zoneState.currentSegmentIdx) {
            return null;
        }
        const displayZoneId = this._resolveZoneId(this._displayZoneId());
        if (Number.isNaN(displayZoneId)) return null;
        if (this.field.geometryZoneId(displayZoneId) !== test.start.geometryZoneId) return null;
        const project = (endpoint, label) => {
            if (!endpoint) return null;
            const [x, y] = this._baseToDisplay(endpoint.x, endpoint.y);
            return {x, y, label};
        };
        return {
            start: project(test.start, "测试起点"),
            goal: project(test.goal, "测试终点"),
        };
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

    /** Show the shared top-right context panel for EDIT selections or LOG inspection cards. */
    _syncLogContextPanel() {
        const e = this.els;
        const editVisible = this.state.mode === Mode.EDIT && !e.editInspectionBox.hidden;
        const logVisible =
            this.state.mode === Mode.LOG && (!e.logInspectionBox.hidden || !e.logDistanceBox.hidden);
        e.logContextPanel.hidden = !editVisible && !logVisible;
    }

    /** Render metadata for the selected author waypoint or manual planning start. */
    _renderEditInspection() {
        const e = this.els;
        const host = e.editPointSummary;
        host.textContent = "";

        let title = "";
        let color = "#22d3ee";
        let details = [];
        const manualStart = this.editPreviewStartSelected ? this._activeEditPreviewStart() : null;
        if (manualStart) {
            title = "规划起点";
            details = [
                ["类型", "离线规划起点"],
                ["坐标", `[${manualStart.position[0].toFixed(2)}, ${manualStart.position[1].toFixed(2)}]`],
                ["层级", manualStart.positionZone],
                ["底图坐标", `[${manualStart.x.toFixed(2)}, ${manualStart.y.toFixed(2)}]`],
                ["片段", String(manualStart.segmentIndex + 1)],
            ];
        } else {
            const selectedIndices = [...this.state.selectedIndices].sort((a, b) => a - b);
            const zoneIndices = this.state.zonePointGlobalIndices();
            if (selectedIndices.length === 1) {
                const localIndex = selectedIndices[0];
                const point = this.state.points[zoneIndices[localIndex]];
                if (point) {
                    title = `作者路点 #${localIndex}`;
                    color = ACTION_COLORS[point.action] || "#3498db";
                    const flags = [point.strict ? "严格抵达" : "", point.required ? "路径必经" : ""].filter(Boolean);
                    details = [
                        ["动作", this._formatActionChain(point)],
                        ["坐标", `[${point.x.toFixed(2)}, ${point.y.toFixed(2)}]`],
                        ["区域", normalizeZoneId(point.zone) || "未声明"],
                        ["层级", normalizeZoneId(point.target_tier) || "跟随区域"],
                        ["目标面", Number.isFinite(point.target_deck_y) ? point.target_deck_y.toFixed(2) : "自动"],
                        ["标志", flags.join(" / ") || "无"],
                    ];
                }
            } else if (selectedIndices.length > 1) {
                title = `已选择 ${selectedIndices.length} 个作者路点`;
                details = [
                    ["序号", selectedIndices.join(", ")],
                    ["操作", "可批量修改动作或删除"],
                ];
            }
        }

        const visible = this.state.mode === Mode.EDIT && !!title;
        e.editInspectionBox.hidden = !visible;
        e.btnEditSelectionClear.disabled = !visible;
        if (visible) {
            const selected = document.createElement("div");
            selected.className = "log-inspection-selected";
            selected.style.borderLeftColor = color;
            const heading = document.createElement("div");
            heading.className = "log-distance-point-title";
            heading.textContent = title;
            const list = document.createElement("dl");
            list.className = "log-point-detail";
            for (const [label, value] of details) {
                const term = document.createElement("dt");
                term.textContent = label;
                const description = document.createElement("dd");
                description.textContent = value;
                list.append(term, description);
            }
            selected.append(heading, list);
            host.appendChild(selected);
        }
        this._syncLogContextPanel();
    }

    /** Clear EDIT object selection and optionally leave the one-shot start-setting tool. */
    _clearEditSelection({cancelTool = false} = {}) {
        const wasSettingStart = this.activeTool === "edit-start";
        const hadTransientGesture =
            this.isBoxSelecting || this.isEditPreviewStartDragCandidate || this.isEditPreviewStartDragging;
        const hadSelection = this.state.selectedIndices.size > 0 || this.editPreviewStartSelected;
        if (!hadSelection && !hadTransientGesture && !(cancelTool && wasSettingStart)) return false;

        this.state.clearSelection();
        this.editPreviewStartSelected = false;
        this.isBoxSelecting = false;
        this.isEditPreviewStartDragCandidate = false;
        this.isEditPreviewStartDragging = false;
        this.selectionRect = null;
        if (cancelTool && wasSettingStart) {
            const returnTool = this._editPreviewStartReturnTool || "add";
            this._editPreviewStartReturnTool = null;
            this._setActiveTool(returnTool);
        }
        this._syncActionControls();
        this._paint();
        return true;
    }

    /** Apply Escape consistently to transient selections without deleting authored data. */
    _cancelCurrentSelection() {
        if (this.state.mode === Mode.EDIT) {
            if (this._hasQuickRouteTest()) {
                this._clearQuickRouteTest();
                this._paint();
                setStatus("已清除快速测试。", "#10b981");
                return true;
            }
            const changed = this._clearEditSelection({cancelTool: true});
            if (changed) setStatus("已取消当前选择。", "#10b981");
            return changed;
        }
        if (this.state.mode === Mode.ASSERT) {
            if (!this.isAssertSelecting) return false;
            this.isAssertSelecting = false;
            this.assertRectWorld = this._assertRectBeforeDrag;
            this._assertRectBeforeDrag = null;
            this._paint();
            setStatus("已取消本次断言区域绘制。", "#10b981");
            return true;
        }
        if (this.state.mode !== Mode.LOG) return false;

        const wasMeasuring = this.activeTool === "log-measure";
        const changed = !!this.logInspectedPoint || this.logDistanceSelection.length > 0 || wasMeasuring;
        if (!changed) return false;
        this.logInspectedPoint = null;
        this.logDistanceSelection = [];
        if (wasMeasuring) this._setActiveTool("log-inspect");
        this._renderLogInspection();
        this._renderLogDistance();
        this._paint();
        setStatus("已取消当前选择。", "#10b981");
        return true;
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

    /** EDIT locate marker projected into the current path frame, or null on another basemap. */
    _editLocateHintForDisplay() {
        const hint = this.editLocateHint;
        if (!hint || !this.field) return null;
        const displayZoneId = this._resolveZoneId(this._displayZoneId());
        if (Number.isNaN(displayZoneId)) return null;
        if (this.field.geometryZoneId(displayZoneId) !== hint.geometryZoneId) return null;
        const [x, y] = this._baseToDisplay(hint.x, hint.y);
        return {x, y, label: hint.label, rot: this._headingBaseToDisplay(hint.x, hint.y, hint.rot)};
    }

    /** Frame the EDIT locate marker without changing the author route. */
    _focusEditLocateHint() {
        const hint = this._editLocateHintForDisplay();
        if (!hint) return;
        const pad = LOCATE_HINT_FIT_PADDING;
        this.camera.fitView(
            [hint.x - pad, hint.y - pad, hint.x + pad, hint.y + pad],
            this._cssW,
            this._cssH,
            60,
            LEFT_PANEL_FIT_OFFSET,
        );
        this._paint();
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
     * The real route points belonging to the map on screen, projected into its display
     * frame. A point's coords live in its *own* zone's frame (possibly a tier), so each
     * goes `own zone → base → display frame`; points of another map are dropped.
     * @returns {Array<Object>} copies of the points with display-frame `x`/`y`
     */
    _displayRealPoints() {
        if (!this.field || !this.state.points.length) return [];
        const displayZoneId = this._displayTierZoneId();
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
        if (Number.isNaN(zoneId)) return;

        // Continuous locate follows the game across maps/tiers, just like the one-shot
        // locate action: point the editor at the live zone before projecting the marker.
        if (this.state.mode === Mode.EDIT && this.liveLocateSocket) {
            const displayZoneId = this._displayTierZoneId();
            if (Number.isNaN(displayZoneId) || this.field.geometryZoneId(zoneId) !== this.field.geometryZoneId(displayZoneId) || String(displayZoneId) !== String(zoneId)) {
                if (this._selectDisplayZoneById(zoneId)) this._onDisplayTierChanged(false);
            }
        }

        const editZoneId = this.state.mode === Mode.EDIT ? this._resolveZoneId(this.state.currentZone()) : NaN;
        const displayZoneId = Number.isNaN(editZoneId) ? this._displayTierZoneId() : editZoneId;
        if (Number.isNaN(displayZoneId)) return;
        if (this.field.geometryZoneId(zoneId) !== this.field.geometryZoneId(displayZoneId)) {
            this._clearLivePath();
            if (this.state.mode === Mode.EDIT) this._paint();
            return;
        }

        const [x, y] = this._pointToBase(zoneId, fix.x, fix.y);
        const rot = this._headingToBase(zoneId, fix.x, fix.y, fix.rot);
        this.livePositionBase = {x, y, rot};
        const last = this.livePathBase[this.livePathBase.length - 1];
        if (!last || Math.hypot(last.x - x, last.y - y) >= 1) {
            this.livePathBase.push({x, y, rot});
        }
        if (this.state.mode === Mode.EDIT) this._paint();
        if (this.threeView && this._is3DView()) {
            const [u, v] = this._baseToDisplay(x, y);
            this.threeView.setLivePosition({u, v, rot: this._headingBaseToDisplay(x, y, rot)});
            const live = this._livePathForDisplay();
            this.threeView.setLivePath((live?.points || []).map((point) => [point.x, point.y]));
            if (!this._initialLiveHeightColored) {
                const height = this.threeView.getHeightAt(u, v);
                if (Number.isFinite(height)) {
                    this.threeView.setHeightFocus(height);
                    this._initialLiveHeightColored = true;
                    this._syncThreeOverlays();
                }
            }
        }
    }

    /** Clear measured live-path state without affecting the planned preview. */
    _clearLivePath() {
        this.livePathBase = [];
        this.livePositionBase = null;
    }

    /** Project measured base-frame points into the current path-edit display frame. */
    _livePathForDisplay() {
        if (!this.showLivePath || !this.field || this.state.mode !== Mode.EDIT) return null;
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
    _diagnosticsForDisplay(diagnostics) {
        const project = (points) =>
            (points || []).map((point) => {
                if (!Array.isArray(point) || point.length < 2) return point;
                const [x, y] = this._baseToDisplay(point[0], point[1]);
                return Number.isFinite(point[2]) ? [x, y, point[2]] : [x, y];
            });
        return (diagnostics || []).map((diag) => ({
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
     * Resolve the single selected EDIT-mode NAVMESH point into the base geometry used by
     * `/api/deck-probe`. An explicit target_tier owns the coordinate transform; without
     * one the runtime keeps the legacy target in base pixels even when the route zone is a tier.
     * @returns {?{globalIndex:number, point:Object, geometryZoneId:number, base:number[], signature:string}}
     */
    _selectedEditDeckTarget() {
        if (this.state.mode !== Mode.EDIT || !this.field || this.state.selectedIndices.size !== 1) return null;
        const zoneIndices = this.state.zonePointGlobalIndices();
        const localIndex = [...this.state.selectedIndices][0];
        const globalIndex = zoneIndices[localIndex];
        const point = this.state.points[globalIndex];
        if (!point || !getPointActions(point).includes(ActionType.NAVMESH)) return null;

        const targetTier = normalizeZoneId(point.target_tier || "");
        const routeZoneId = this._resolveZoneId(point.zone);
        const frameZoneId = targetTier ? this._resolveZoneId(targetTier) : routeZoneId;
        if (Number.isNaN(frameZoneId)) return null;
        const base = targetTier
            ? this._pointToBase(frameZoneId, point.x, point.y)
            : [
                  point.x,
                  point.y,
              ];
        const geometryZoneId = this.field.geometryZoneId(frameZoneId);
        return {
            globalIndex,
            point,
            geometryZoneId,
            base,
            signature: `${globalIndex}:${geometryZoneId}:${base[0]},${base[1]}:${targetTier}`,
        };
    }

    /** Query overlapping surfaces whenever the selected NAVMESH target changes. @returns {void} */
    _scheduleEditDeckProbe() {
        const target = this._selectedEditDeckTarget();
        const signature = target ? target.signature : "";
        if (signature === this._editDeckSig) return;
        this._editDeckSig = signature;
        this._editDeckToken += 1;
        clearTimeout(this._editDeckTimer);
        this.editDeckProbe = null;
        this.deckPreview = null;
        this.renderer.setDeckBand(null);
        this._renderEditDeckList();
        if (!target) return;
        this._editDeckTimer = setTimeout(() => this._probeEditDeckTarget(target), 100);
    }

    /** @param {{globalIndex:number, geometryZoneId:number, base:number[]}} target @returns {Promise<void>} */
    async _probeEditDeckTarget(target) {
        const token = ++this._editDeckToken;
        let res;
        try {
            res = await postDeckProbe({zone_id: target.geometryZoneId, point: target.base});
        } catch {
            return; // 探针只辅助选层，失败不能阻断路径编辑
        }
        if (token !== this._editDeckToken) return;
        this.editDeckProbe =
            res && res.ok && Array.isArray(res.decks)
                ? {globalIndex: target.globalIndex, decks: res.decks}
                : null;
        this._renderEditDeckList();
    }

    /** Render the selected NAVMESH point's overlapping surfaces in the property panel. @returns {void} */
    _renderEditDeckList() {
        const box = this.els.editDeckBox;
        const list = this.els.editDeckList;
        if (!box || !list) return;
        list.replaceChildren();

        const target = this._selectedEditDeckTarget();
        if (!target) {
            box.hidden = true;
            return;
        }
        const probe =
            this.editDeckProbe && this.editDeckProbe.globalIndex === target.globalIndex
                ? this.editDeckProbe
                : null;
        const decks = probe ? probe.decks : [];
        const filled =
            typeof target.point.target_deck_y === "number" && Number.isFinite(target.point.target_deck_y)
                ? target.point.target_deck_y
                : null;
        const matchedHeight = matchTargetDeckHeight(decks, filled);
        if (decks.length < 2 && filled === null) {
            box.hidden = true;
            return;
        }

        box.hidden = false;
        this.els.editDeckTitle.textContent =
            decks.length >= 2
                ? `重叠面：该点底下压着 ${decks.length} 张可走面`
                : `目标面：当前 target_deck_y = ${filled.toFixed(2)}`;

        decks.forEach((deck, i) => {
            const row = document.createElement("div");
            row.className = "deck-item";
            if (this.deckPreview === deck.height) row.classList.add("is-preview");
            if (matchedHeight === deck.height) row.classList.add("is-filled");

            const pick = document.createElement("button");
            pick.type = "button";
            pick.className = "deck-pick";
            pick.title = "预览这一层";
            pick.textContent = deck.height.toFixed(2);
            const note = document.createElement("small");
            note.textContent = ` 自上而下第 ${i + 1} 层 / 共 ${decks.length} 层${deck.thin ? "（薄片，多半是墙顶）" : ""}`;
            pick.appendChild(note);
            pick.addEventListener("click", () => {
                this._setEditDeckPreview(this.deckPreview === deck.height ? null : deck.height, probe);
                this._renderEditDeckList();
            });
            row.appendChild(pick);

            const fill = document.createElement("button");
            fill.type = "button";
            fill.className = "btn btn-secondary btn-sm";
            fill.textContent = matchedHeight === deck.height ? "清除" : "选择";
            fill.addEventListener("click", () =>
                this._fillEditDeck(matchedHeight === deck.height ? null : deck.height),
            );
            row.appendChild(fill);
            list.appendChild(row);
        });

        if (filled !== null && matchedHeight === null) {
            const row = document.createElement("div");
            row.className = "deck-item is-filled";
            const current = document.createElement("span");
            current.className = "deck-pick";
            current.textContent = `${filled.toFixed(2)} 当前配置（未在可选重叠面中匹配）`;
            row.appendChild(current);
            const clear = document.createElement("button");
            clear.type = "button";
            clear.className = "btn btn-secondary btn-sm";
            clear.textContent = "清除";
            clear.addEventListener("click", () => this._fillEditDeck(null));
            row.appendChild(clear);
            list.appendChild(row);
        }
    }

    /** Highlight one overlapping surface band for the selected edit waypoint. */
    _setEditDeckPreview(height, probe = this.editDeckProbe) {
        this.deckPreview = height;
        const band =
            probe && height !== null ? (probe.decks.find((deck) => deck.height === height) || {}).band : null;
        this.renderer.setDeckBand(band || null);
        this._paint();
    }

    /** Persist a selected deck on the current NAVMESH waypoint. @param {?number} height @returns {void} */
    _fillEditDeck(height) {
        const result = this.state.editSetSelectedTargetDeck(height);
        if (result.unsupported) {
            setStatus("请先单独选中一个 NAVMESH 路点。", "#f59e0b");
            return;
        }
        if (!result.changed) return;
        this._afterStructureChanged();
        setStatus(
            height === null
                ? "已清除该路点的 target_deck_y。"
                : `该路点 target_deck_y = ${height.toFixed(2)}，规划、复制和试跑都会使用该目标面。`,
            "#10b981",
        );
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
     * the goal→start line). The badge says "最近网格", never "盲走".
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
                snap_radius: NAVMESH_PROBE_SNAP_RADIUS,
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

    // --- navmesh (NMSH over GL) ---

    /** Upload or hide the navmesh for the edit display zone (keyed, so cheap per paint). */
    _syncNavmesh() {
        if (this.state.mode !== Mode.EDIT || !this.field) {
            if (this._meshKey !== null) {
                this._meshToken += 1;
                this._latest3DMesh = null;
                if (this.threeView) this.threeView.clearMesh();
            }
            this._meshKey = null;
            this.renderer.setMeshVisible(false);
            this.renderer.setDotsVisible(false);
            return;
        }
        const displayZoneId = this._resolveZoneId(this._displayZoneId());
        if (Number.isNaN(displayZoneId)) {
            if (this._meshKey !== null) {
                this._meshToken += 1;
                this._latest3DMesh = null;
                if (this.threeView) this.threeView.clearMesh();
            }
            this._meshKey = null;
            this.renderer.setMeshVisible(false);
            this.renderer.setDotsVisible(false);
            return;
        }
        const geomId = this.field.geometryZoneId(displayZoneId);
        const tierId = this._activeDisplayTierId();
        const key = `${geomId}:${tierId}`;
        if (key === this._meshKey) return;
        this._meshKey = key;
        this._latest3DMesh = null;
        if (this.threeView) this.threeView.clearMesh();
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
                this._latest3DMesh = {key, buffer: buf};
                if (this.threeView) this._setThreeViewMesh(buf);
                this.renderer.setMeshVisible(true);
                this.renderer.setDotsVisible(false);
                this.renderer.requestRender(this.camera);
            })
            .catch(() => {
                if (token === this._meshToken) this.renderer.setMeshVisible(false);
            });
    }

    /** Whether the read-only Three.js surface is the active map view. */
    _is3DView() {
        return this.state.mode === Mode.EDIT && this.viewMode === "3d";
    }

    /** Lazily load Three.js so the established 2D editor remains independent. */
    async _ensureThreeView() {
        if (this.threeView) return this.threeView;
        if (this._threeViewPromise) return this._threeViewPromise;
        this._threeViewPromise = import("./three_navmesh_view.js")
            .then(({ThreeNavmeshView}) => {
                this.threeView = new ThreeNavmeshView({
                    canvas: this.els.threeCanvas,
                    onPick: ({u, v, height}) => {
                        setStatus(
                            `3D 点位: [${compactNumber(u)}, ${compactNumber(v)}]  高度 ${compactNumber(height)}`,
                            "#10b981",
                        );
                    },
                });
                this.threeView.setNavigationMode(this.threeNavigationMode);
                this.threeView.setMovementSpeed(Number(this.els.threeFlightSpeed.value));
                this.threeView.resize(this._cssW, this._cssH, window.devicePixelRatio || 1);
                if (this._latest3DMesh) this._setThreeViewMesh(this._latest3DMesh.buffer);
                this.threeView.setVisible(this._is3DView());
                return this.threeView;
            })
            .catch((error) => {
                console.error("Failed to initialize the 3D navmesh view", error);
                this.viewMode = "2d";
                this._syncViewModeUI();
                this._paint();
                setStatus(`3D 视图加载失败: ${error && error.message ? error.message : error}`, "#ef4444");
                return null;
            })
            .finally(() => {
                this._threeViewPromise = null;
            });
        return this._threeViewPromise;
    }

    /** Upload the current NMSH payload without affecting the working 2D renderer. */
    _setThreeViewMesh(buffer) {
        try {
            this._initialLiveHeightColored = false;
            this.threeView.setMesh(buffer);
            this._syncThreeOverlays();
        } catch (error) {
            console.error("Failed to render the 3D navmesh", error);
            setStatus(`3D 网格加载失败: ${error && error.message ? error.message : error}`, "#ef4444");
        }
    }

    /** Keep the read-only 3D scene in sync with 2D planning and live location state. */
    _syncThreeOverlays() {
        if (!this.threeView) return;
        const route = this.quickRouteTestRoute || this.editRoute;
        const live = this._livePathForDisplay();
        const position = this.livePositionBase || this.editLocateHint;
        if (position) {
            const [u, v] = this._baseToDisplay(position.x, position.y);
            this.threeView.setLivePosition({
                u,
                v,
                rot: this._headingBaseToDisplay(position.x, position.y, position.rot),
            });
            if (this.livePositionBase && !this._initialLiveHeightColored) {
                const height = this.threeView.getHeightAt(u, v);
                if (Number.isFinite(height)) {
                    this.threeView.setHeightFocus(height);
                    this._initialLiveHeightColored = true;
                }
            }
        } else {
            this.threeView.clearLivePosition();
        }
        this.threeView.setLivePath((live?.points || []).map((point) => [point.x, point.y]));
        const diagnostics = this._diagnosticsForDisplay(route?.diagnostics || []);
        const plannedPoints = diagnostics.flatMap((diagnostic) => diagnostic.planned_points || []);
        const routePoints = (route?.points || []).map((point) => {
            const [u, v] = this._baseToDisplay(point[0], point[1]);
            let best = null;
            let bestDistance = Infinity;
            for (const candidate of plannedPoints) {
                if (!Array.isArray(candidate) || candidate.length < 3 || !Number.isFinite(candidate[2])) continue;
                const distance = Math.hypot(candidate[0] - u, candidate[1] - v);
                if (distance < bestDistance) {
                    bestDistance = distance;
                    best = candidate[2];
                }
            }
            return Number.isFinite(best) ? [u, v, best] : [u, v];
        });
        this.threeView.setRoute(this.navDebug.planned ? routePoints : []);
        this.threeView.setDiagnostics(diagnostics, this.navDebug);
    }

    /** Switch between the editable 2D map and the read-only 3D mesh. */
    _setViewMode(mode, {announce = true} = {}) {
        const nextMode = mode === "3d" ? "3d" : "2d";
        if (nextMode === "3d" && this.state.mode !== Mode.EDIT) return;

        this.viewMode = nextMode;
        this._syncViewModeUI();

        if (nextMode === "3d") {
            void this._ensureThreeView();
            if (announce) setStatus("已切换到 3D 视图。", "#10b981");
        } else {
            if (this.threeView) this.threeView.setVisible(false);
            this._paint();
            if (announce) setStatus("已切换到 2D 视图。", "#10b981");
        }
    }

    /** Select how mouse and WASD input navigate the active 3D view. */
    _setThreeNavigationMode(mode, {announce = true} = {}) {
        const nextMode = mode === "orbit" ? "orbit" : "free";
        this.threeNavigationMode = nextMode;
        this.els.threeNavigationMode.value = nextMode;
        if (this.threeView) this.threeView.setNavigationMode(nextMode);
        else if (this._is3DView()) void this._ensureThreeView();

        if (announce) {
            setStatus(`3D 视角导航已切换为${nextMode === "orbit" ? "轨道环绕" : "自由飞行"}。`, "#10b981");
        }
    }

    _setThreeFlightSpeed(value) {
        const speed = Math.max(0.1, Math.min(3, Number(value) || 1));
        this.els.threeFlightSpeed.value = String(speed);
        this.els.threeFlightSpeedValue.textContent = `${speed.toFixed(1)}x`;
        if (this.threeView) this.threeView.setMovementSpeed(speed);
    }

    /** Synchronize segmented buttons, canvases, and controls that only edit 2D state. */
    _syncViewModeUI() {
        const editMode = this.state.mode === Mode.EDIT;
        const show3D = editMode && this.viewMode === "3d";
        const e = this.els;

        e.viewModeToggle.hidden = !editMode;
        e.viewModeDivider.hidden = !editMode;
        e.viewMode2d.classList.toggle("active", !show3D);
        e.viewMode3d.classList.toggle("active", show3D);
        e.viewMode2d.setAttribute("aria-pressed", String(!show3D));
        e.viewMode3d.setAttribute("aria-pressed", String(show3D));
        e.threeNavigationRow.hidden = !show3D;
        e.threeSpeedRow.hidden = !show3D;
        e.threeRecolorRow.hidden = !show3D;
        e.threeNavDebugOptions.hidden = !show3D;
        e.threeNavigationMode.value = this.threeNavigationMode;
        e.canvasWrap.classList.toggle("view-3d", show3D);
        document.body.classList.toggle("view-3d", show3D);
        e.glCanvas.hidden = show3D;
        e.overlayCanvas.hidden = show3D;
        e.threeCanvas.hidden = !show3D;
        if (this.threeView) this.threeView.setVisible(show3D);

        e.toolRouteTest.hidden = !editMode || show3D;
        e.toolEditStart.hidden = !editMode || show3D;
        e.editStartDivider.hidden = !editMode || show3D;
        e.btnDelPointFloat.hidden = this.state.mode === Mode.LOG || show3D;
        e.editDeleteDivider.hidden = this.state.mode === Mode.LOG || show3D;
        if (editMode) {
            e.panelRecording.hidden = show3D;
            e.panelProperties.hidden = show3D;
            e.panelNavtest.hidden = show3D;
            if (this.navtest) this.navtest.setDisabled(show3D);
        }
    }

    // --- fit view ---

    /** Fit the current frame to the canvas (tk `fit_view`), deferring if the basemap is loading. */
    _fitView() {
        if (this._is3DView()) {
            if (this.threeView) this.threeView.fitView();
            return;
        }
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
     * Frame a set of display-frame points. Used right after an import: in Assert
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
     * left button dispatches on mode + active tool (pan / assert
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
            this.activeTool === "assert-pan"
        ) {
            this.isPanning = true;
            this.dragStartX = x;
            this.dragStartY = y;
            this.els.overlayCanvas.style.cursor = "grabbing";
            return;
        }

        if (this.state.mode === Mode.ASSERT) {
            if (this.activeTool === "assert-edit") {
                const zoneId = this._displayZoneId();
                if (!zoneId) {
                    setStatus("请先在 Assert 模式下选择地图。", "#ef4444");
                    return;
                }
                this._assertRectBeforeDrag = this.assertRectWorld ? [...this.assertRectWorld] : null;
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
            if (this._hitEditPreviewStart(x, y)) {
                this.isEditPreviewStartDragCandidate = true;
                this.isEditPreviewStartDragging = false;
                this.isPanCandidate = false;
                this.isPanning = false;
                this.isDragging = false;
                this.isDragCandidate = false;
                this.isBoxSelecting = false;
                this.editPreviewStartSelected = true;
                this.pointerDownX = x;
                this.pointerDownY = y;
                this.state.clearSelection();
                this._syncActionControls();
                this._paint();
                return;
            }
            this.editPreviewStartSelected = false;
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
            return;
        }

        if (
            this.state.mode === Mode.EDIT &&
            (this.activeTool === "edit-start" || this.activeTool === "route-test")
        ) {
            this.isDragging = false;
            this.isPanCandidate = true;
            this.isPanning = false;
            this.isBoxSelecting = false;
            this.isAssertSelecting = false;
            this.pointerDownX = x;
            this.pointerDownY = y;
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

        if (this.isEditPreviewStartDragCandidate) {
            if (!this._movedExceeded(this.pointerDownX, this.pointerDownY, x, y)) return;
            this.isEditPreviewStartDragCandidate = false;
            this.isEditPreviewStartDragging = true;
            this._clearEditPreview();
        }

        if (this.isEditPreviewStartDragging) {
            if (this._setEditPreviewStartAtCanvas(x, y, false)) {
                this._syncEditPreviewStartControls();
                this._renderEditInspection();
                this._paint();
            }
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
     * Pointer-up: commit the active gesture — end pan, close
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
            this._assertRectBeforeDrag = null;
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

        if (this.isEditPreviewStartDragCandidate) {
            this.isEditPreviewStartDragCandidate = false;
            this._renderEditInspection();
            this._paint();
            return;
        }

        if (this.isEditPreviewStartDragging) {
            this.isEditPreviewStartDragging = false;
            this._setEditPreviewStartAtCanvas(x, y, false);
            this._syncEditPreviewStartControls();
            this._renderEditInspection();
            setStatus("规划起点已移动。", "#10b981");
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
                } else {
                    this.state.clearSelection();
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
            if (this.state.mode === Mode.EDIT && this.activeTool === "edit-start") {
                this._handleEditPreviewStartClick(x, y);
                return;
            }
            if (this.state.mode === Mode.EDIT && this.activeTool === "route-test") {
                this._handleQuickRouteTestClick(x, y);
                return;
            }
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
            const frame = this._manualEditFrame();
            if (!frame.zone) {
                setStatus("请先选择路径底图与层级。", "#f59e0b");
                return;
            }
            this.state.editInsertManualNavmeshPoint(wx, wy, frame.zone, frame.targetTier);
            this._resetPropertyControls();
            this._afterStructureChanged();
            return;
        }

        if (this.isDragging) {
            this.isDragging = false;
            this._clearEditPreview();
            if (this.navtest) this.navtest.routeChanged();
            this._renderEditInspection();
            this._doRedraw();
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
    //  Runtime route previews
    // ==================================================================================

    /** Drop the runtime preview without changing any authored point. */
    _clearEditPreview() {
        this._editRouteToken += 1;
        this.editRoute = null;
        this.editRouteFailure = null;
        this._editRoutePending = false;
        this._syncEditPlanControls();
    }

    /** Whether the temporary S/G test currently owns any visible or pending state. */
    _hasQuickRouteTest() {
        return Boolean(
            this.quickRouteTest?.start ||
                this.quickRouteTestRoute ||
                this.quickRouteTestFailure,
        );
    }

    /** Keep shared planning controls aligned with both preview sources and pending requests. */
    _syncEditPlanControls() {
        this.els.btnEditPlan.disabled = this._editRoutePending || this._quickRouteTestPending;
        this.els.btnEditPlanClear.disabled = !(
            this.editRoute ||
            this.editRouteFailure ||
            this._hasQuickRouteTest()
        );
        this._syncNavTimingLabels();
    }

    /** Drop every quick-test artifact without touching authored or manual-preview data. */
    _clearQuickRouteTest() {
        this._quickRouteTestToken += 1;
        this.quickRouteTest = null;
        this.quickRouteTestRoute = null;
        this.quickRouteTestFailure = null;
        this._quickRouteTestPending = false;
        this._syncEditPlanControls();
    }

    /** Return the manual preview start only while its original edit segment and geometry are active. */
    _activeEditPreviewStart() {
        const start = this.editPreviewStart;
        if (!start || !this.field || start.segmentIndex !== this.state.zoneState.currentSegmentIdx) return null;
        const displayZoneId = this._resolveZoneId(this._displayZoneId());
        if (Number.isNaN(displayZoneId)) return null;
        return this.field.geometryZoneId(displayZoneId) === start.geometryZoneId ? start : null;
    }

    /** Whether a canvas position hits the current segment's planning-start badge. */
    _hitEditPreviewStart(canvasX, canvasY) {
        const marker = this._editPreviewStartForDisplay();
        if (!marker) return false;
        const [x, y] = this.camera.worldToCanvas(marker.x, marker.y);
        return Math.hypot(x - canvasX, y - canvasY) <= 16;
    }

    /** Refresh the manual-start status and controls for the current edit segment. */
    _syncEditPreviewStartControls() {
        const start = this._activeEditPreviewStart();
        if (!start) {
            this.els.editPlanStartLabel.textContent = "规划起点：当前片段第一个路点";
            return;
        }
        const [x, y] = this._baseToDisplay(start.x, start.y);
        this.els.editPlanStartLabel.textContent = `规划起点：手动 [${x.toFixed(1)}, ${y.toFixed(1)}]`;
    }

    /** Drop the preview-only manual start without changing any authored point. */
    _clearEditPreviewStart() {
        this.editPreviewStart = null;
        this.editPreviewStartSelected = false;
        this._syncEditPreviewStartControls();
        this._renderEditInspection();
    }

    /** Convert an EDIT canvas point into runtime coordinates plus a base-px display anchor. */
    _planningEndpointAtCanvas(canvasX, canvasY) {
        if (!this.field) {
            setStatus("navmesh 尚未就绪。", "#ef4444");
            return null;
        }
        const tierId = this._activeDisplayTierId();
        const coordinateZoneId = tierId === null ? this._resolveZoneId(this._displayZoneId()) : tierId;
        if (Number.isNaN(coordinateZoneId)) {
            setStatus("请先选择路径底图与层级。", "#f59e0b");
            return null;
        }
        const geometryZoneId = this.field.geometryZoneId(coordinateZoneId);
        const geometryZone = this.field.zoneById(geometryZoneId);
        const coordinateZone = this.field.zoneById(coordinateZoneId);
        const positionZone = normalizeZoneId(coordinateZone && coordinateZone.name);
        if (!geometryZone || !positionZone) {
            setStatus("当前层级缺少坐标定义，无法设置规划点。", "#ef4444");
            return null;
        }

        const [wx, wy] = this.camera.canvasToWorld(canvasX, canvasY);
        const [x, y] = this._pointToBase(coordinateZoneId, wx, wy);
        return {
            x,
            y,
            position: [wx, wy],
            positionZone,
            targetTier:
                this.field.isTier(coordinateZoneId) && this.field.isRealTier(coordinateZoneId)
                    ? positionZone
                    : "",
            geometryZoneId,
            segmentIndex: this.state.zoneState.currentSegmentIdx,
        };
    }

    /** Update the preview start from an EDIT canvas point, preserving its original tier frame. */
    _setEditPreviewStartAtCanvas(canvasX, canvasY, clearPreview = true) {
        const endpoint = this._planningEndpointAtCanvas(canvasX, canvasY);
        if (!endpoint) return false;
        if (clearPreview) this._clearEditPreview();
        this.editPreviewStart = endpoint;
        return true;
    }

    /** Set a preview-only start, then restore the tool active before the one-shot action. */
    _handleEditPreviewStartClick(canvasX, canvasY) {
        if (!this._setEditPreviewStartAtCanvas(canvasX, canvasY)) return;
        this.editPreviewStartSelected = true;
        this.state.clearSelection();
        this._syncEditPreviewStartControls();
        this._syncActionControls();
        const returnTool = this._editPreviewStartReturnTool || "add";
        this._editPreviewStartReturnTool = null;
        this._setActiveTool(returnTool);
        const start = this.editPreviewStart;
        setStatus(
            `规划起点已设置: [${start.position[0].toFixed(1)}, ${start.position[1].toFixed(1)}]。`,
            "#10b981",
        );
        this._paint();
    }

    /** Advance the quick S/G state and automatically plan after the goal click. */
    _handleQuickRouteTestClick(canvasX, canvasY) {
        const endpoint = this._planningEndpointAtCanvas(canvasX, canvasY);
        if (!endpoint) return;
        const next = advanceQuickRouteTest(this.quickRouteTest, endpoint);
        if (!next.ok) {
            setStatus(next.error, "#ef4444");
            return;
        }

        if (!next.shouldPlan) {
            this._clearQuickRouteTest();
            this._clearEditPreview();
        } else {
            this._quickRouteTestToken += 1;
            this.quickRouteTestRoute = null;
            this.quickRouteTestFailure = null;
        }
        this.quickRouteTest = next.state;
        this.state.clearSelection();
        this.editPreviewStartSelected = false;
        this._syncActionControls();
        this._syncEditPlanControls();
        this._paint();

        if (!next.shouldPlan) {
            const start = next.state.start.position;
            setStatus(
                `测试起点已设置: [${start[0].toFixed(1)}, ${start[1].toFixed(1)}]；请单击终点。`,
                "#10b981",
            );
            return;
        }
        void this._calculateQuickRouteTest();
    }

    /** Normalize a successful runtime response into the overlay's route shape. */
    _routePreviewData(result) {
        return {
            points: result.points || [],
            walk_segments: result.walk_segments || [],
            zipline_segments: result.zipline_segments || [],
            diagnostics: result.diagnostics || [],
            zipline: result.zipline || {},
        };
    }

    /** User-facing summary shared by authored and quick route previews. */
    _routePreviewStatus(route, result, subject) {
        const hops = route.zipline_segments.length;
        const expanded = result.expanded_waypoints || route.points.length;
        if (hops > 0) {
            return [`${subject}：采用 ${hops} 跳滑索，展开为 ${expanded} 个运行时路点。`, "#10b981"];
        }
        if (route.zipline.no_data) {
            return [`${subject}：当前区域没有可用的滑索记录，已回退为步行路线。`, "#f59e0b"];
        }
        if (route.zipline.not_chosen) {
            return [`${subject}：滑索没有显著优于步行，运行时会采用当前步行路线。`, "#10b981"];
        }
        return [`${subject}：展开为 ${expanded} 个运行时路点。`, "#10b981"];
    }

    /** Focus a runtime-confirmed route gap, or repaint when no precise gap exists. */
    _showRoutePreviewFailure(failure) {
        if (failure?.gap_start && failure?.gap_goal) {
            const start = this._baseToDisplay(failure.gap_start[0], failure.gap_start[1]);
            const goal = this._baseToDisplay(failure.gap_goal[0], failure.gap_goal[1]);
            this._fitDisplayPoints([
                {x: start[0], y: start[1]},
                {x: goal[0], y: goal[1]},
            ]);
            return;
        }
        this._paint();
    }

    /** Plan the temporary S/G pair through the runtime MapNavigateAction preview. */
    async _calculateQuickRouteTest() {
        const built = buildQuickRouteTestRequest(this.quickRouteTest, {
            zip: this.els.chkEditZipline.checked,
            exact_slim: this.els.chkEditExactSlim.checked,
        });
        if (!built.ok) {
            setStatus(built.error, "#f59e0b");
            return;
        }

        const token = ++this._quickRouteTestToken;
        this.quickRouteTestRoute = null;
        this.quickRouteTestFailure = null;
        this._quickRouteTestPending = true;
        this._syncEditPlanControls();
        this._paint();
        setStatus("正在按运行时语义测试起终点…", "#3b82f6");

        try {
            const result = await postRoutePreview(built.request);
            if (token !== this._quickRouteTestToken || (result && result.stale)) return;
            if (!result || !result.ok) {
                this.quickRouteTestFailure = result?.failure || null;
                throw new Error(result?.error || "路线展开失败");
            }

            this.quickRouteTestRoute = this._routePreviewData(result);
            this._syncThreeOverlays();
            this._syncEditPlanControls();
            setStatus(...this._routePreviewStatus(this.quickRouteTestRoute, result, "快速测试完成"));
            this._paint();
        } catch (err) {
            if (token !== this._quickRouteTestToken) return;
            const message = err && err.message ? err.message : err;
            this.quickRouteTestRoute = null;
            this._syncEditPlanControls();
            setStatus(`快速测试失败: ${message}`, "#ef4444");
            this._showRoutePreviewFailure(this.quickRouteTestFailure);
        } finally {
            if (token === this._quickRouteTestToken) {
                this._quickRouteTestPending = false;
                this._syncEditPlanControls();
            }
        }
    }

    /** Expand the current EDIT zone segment with the same planner used by MapNavigateAction. */
    async _calculateEditPreview() {
        const clearedQuickTest = this._hasQuickRouteTest();
        this._clearQuickRouteTest();
        if (clearedQuickTest) this._paint();
        const points = this._currentSegmentPoints();
        const plan = buildEditPreviewPlan(points, this._activeEditPreviewStart());
        if (!plan.ok) {
            setStatus(plan.error, "#f59e0b");
            return;
        }

        const token = ++this._editRouteToken;
        this.editRoute = null;
        this.editRouteFailure = null;
        this._editRoutePending = true;
        this._syncEditPlanControls();
        this._paint();
        setStatus("正在按运行时语义规划当前片段…", "#3b82f6");

        try {
            // 默认模式把第一个作者点视作已抵达的起点；手动起点则保留全部作者点为目标。
            const exported = await exportPath(plan.targets);
            const customActionParam = {path: exported.nodes || []};
            if (this.els.chkEditZipline.checked) customActionParam.zip = true;
            if (this.els.chkEditExactSlim.checked) customActionParam.exact_slim = true;
            const result = await postRoutePreview({
                position: plan.position,
                position_zone: plan.positionZone,
                custom_action_param: customActionParam,
            });
            if (token !== this._editRouteToken || (result && result.stale)) return;
            if (!result || !result.ok) {
                this.editRouteFailure = result?.failure || null;
                throw new Error(result?.error || "路线展开失败");
            }

            this.editRoute = this._routePreviewData(result);
            this._syncThreeOverlays();
            this._syncEditPlanControls();
            setStatus(...this._routePreviewStatus(this.editRoute, result, "当前片段已规划"));
            this._paint();
        } catch (err) {
            if (token !== this._editRouteToken) return;
            const message = err && err.message ? err.message : err;
            this.editRoute = null;
            this._syncEditPlanControls();
            setStatus(`规划失败: ${message}`, "#ef4444");
            this._showRoutePreviewFailure(this.editRouteFailure);
        } finally {
            if (token === this._editRouteToken) {
                this._editRoutePending = false;
                this._syncEditPlanControls();
            }
        }
    }

    /**
     * "定位当前位置" button: one-shot backend locate (`/api/locate-once`), then feed
     * the fix into the calling mode's flow. Edit marks a read-only reference point;
     * Assert switches zone and drops the drag-rect hint.
     * @param {'edit'|'assert'} mode
     * @returns {Promise<void>}
     */
    async _onLocateCurrentPosition(mode) {
        if (!this.connection || !this.connection.isConnected()) {
            setStatus("请先确认游戏连接状态正常。", "#ef4444");
            return;
        }
        if (!this.field) {
            setStatus("地图尚未就绪，无法定位。", "#ef4444");
            return;
        }

        const connectionPayload = this.connection ? this.connection.buildSession() : null;
        const locateButton = mode === "edit" ? this.els.btnEditLocate : this.els.btnAssertLocate;
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

                if (mode === "edit") {
                    const zoneIdNum = this._resolveZoneId(zone);
                    if (Number.isNaN(zoneIdNum)) {
                        this.editLocateHint = null;
                        setStatus(`定位成功，但区域 ${zone} 没有可用的 navmesh 底图。`, "#f59e0b");
                        this._paint();
                    } else {
                        if (!this.state.points.length) {
                            if (this._selectDisplayZoneById(zoneIdNum)) this._onDisplayTierChanged(false);
                        }
                        const displayZoneId = this._resolveZoneId(this._displayZoneId());
                        const geometryZoneId = this.field.geometryZoneId(zoneIdNum);
                        if (
                            Number.isNaN(displayZoneId) ||
                            this.field.geometryZoneId(displayZoneId) !== geometryZoneId
                        ) {
                            this.editLocateHint = null;
                            setStatus(
                                `定位成功，但当前位置 ${zone} 不属于当前路径底图；参考点未显示。`,
                                "#f59e0b",
                            );
                            this._paint();
                        } else {
                            const [bx, by] = this._pointToBase(zoneIdNum, x, y);
                            const baseRot = this._headingToBase(zoneIdNum, x, y, rot);
                            this.editLocateHint = {
                                x: bx,
                                y: by,
                                rot: baseRot,
                                label: `游戏当前位置 · ${headingText}`,
                                geometryZoneId,
                            };
                            this._focusEditLocateHint();
                            setStatus(
                                `已在当前路径底图标出游戏当前位置 [${x.toFixed(1)}, ${y.toFixed(1)}] · ${headingText}；未新增路点。`,
                                "#10b981",
                            );
                        }
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
            if (locateButton) locateButton.disabled = !this.connection.isConnected();
        }
    }

    /** Enable live-position actions only after the current connection probe succeeds. */
    _syncLocateActions(connected) {
        for (const button of [this.els.btnEditLocate, this.els.btnAssertLocate]) {
            if (button) button.disabled = !connected;
        }
        if (this.els.btnLiveLocate && !this.liveLocateSocket) this.els.btnLiveLocate.disabled = !connected;
    }

    _toggleLiveLocate() {
        if (this.liveLocateSocket) {
            this._stopLiveLocate();
            return;
        }
        if (!this.connection?.isConnected()) return;
        const socket = new RecordingSocket();
        this.liveLocateSocket = socket;
        socket.onMessage = (msg) => {
            if (msg?.type === "position") this._onLivePosition({x: msg.x, y: msg.y, zone: msg.zone, rot: msg.rot});
        };
        socket.onClose = () => {
            if (this.liveLocateSocket === socket) {
                this.liveLocateSocket = null;
                this.showLivePath = false;
                this.els.btnLiveLocate.textContent = "开启实时定位";
                this.els.btnLiveLocate.classList.remove("btn-danger");
                this.els.btnLiveLocate.classList.add("btn-secondary");
            }
        };
        socket.start(this.connection.buildSession());
        this._clearLivePath();
        this._initialLiveHeightColored = false;
        this.showLivePath = true;
        this.els.btnLiveLocate.textContent = "关闭实时定位";
        this.els.btnLiveLocate.classList.remove("btn-secondary");
        this.els.btnLiveLocate.classList.add("btn-danger");
        setStatus("实时定位已开启。", "#10b981");
    }

    /** Keep standalone locating and route running mutually exclusive. */
    _stopLiveLocate() {
        const socket = this.liveLocateSocket;
        if (socket) socket.stop();
        this.liveLocateSocket = null;
        this.showLivePath = false;
        this._clearLivePath();
        this.els.btnLiveLocate.textContent = "开启实时定位";
        this.els.btnLiveLocate.classList.remove("btn-danger");
        this.els.btnLiveLocate.classList.add("btn-secondary");
        this._syncThreeOverlays();
    }

    _recolorThreeByLiveHeight() {
        if (!this.threeView || !this.livePositionBase) {
            setStatus("尚未获取到实时位置，暂时无法重着色。", "#f59e0b");
            return;
        }
        const [u, v] = this._baseToDisplay(this.livePositionBase.x, this.livePositionBase.y);
        const height = this.threeView.getHeightAt(u, v);
        if (!Number.isFinite(height)) {
            setStatus("当前 3D 网格尚未加载，无法重着色。", "#f59e0b");
            return;
        }
        this.threeView.setHeightFocus(height);
        this._initialLiveHeightColored = true;
        this._syncThreeOverlays();
        setStatus(`已按当前位置高度 ${height.toFixed(1)} m 固定重着色。`, "#10b981");
    }

    /**
     * Point the shared base/tier controls at the map owning `zoneId`.
     * Callers reload the mesh through {@link MapNavigatorApp#_onDisplayTierChanged}.
     * @param {number} zoneId
     * @returns {boolean} whether the controls were pointed at a known base
     */
    _selectDisplayZoneById(zoneId) {
        if (!this.field || Number.isNaN(zoneId)) return false;
        const base = this.field.zoneById(this.field.geometryZoneId(zoneId));
        if (!base || !base.name) return false;
        this.els.displayZoneCombo.value = base.name;
        this._refreshDisplayTierChoices();
        const label = this.field.zoneLabel(zoneId);
        this.els.displayTierCombo.value = label;
        this.els.editSelectedTierLabel.textContent = label;
        return true;
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
        this.els.displayZoneCombo.value = base.name;
        this._refreshDisplayTierChoices();
        if (this.els.displayTierCombo.options.length) this.els.displayTierCombo.selectedIndex = 0;
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
    //  Mode switching (mutually exclusive: edit / assert / log)
    // ==================================================================================

    /**
     * Assert-zone combo change: reset the drag rect, sync the tier label, and mirror
     * the selection into the display controls so switching modes keeps the same map.
     * @returns {void}
     */
    _onAssertZoneChanged() {
        const zoneId = normalizeZoneId(this.els.assertZoneCombo.value);
        if (!zoneId) return;
        this.els.assertZoneCombo.value = zoneId;
        this.assertRectWorld = null;
        this.isAssertSelecting = false;
        this._assertRectBeforeDrag = null;
        this._refreshZoneLabel();
        if (this.field) {
            const zoneIdNum = parseInt(zoneId, 10);
            if (!Number.isNaN(zoneIdNum)) {
                const label = this.field.zoneLabel(zoneIdNum) || zoneId;
                this.els.assertSelectedTierLabel.textContent = label;
                const baseId = this.field.geometryZoneId(zoneIdNum);
                const base = this.field.zoneById(baseId);
                if (base && base.name) {
                    this.els.displayZoneCombo.value = base.name;
                    this._refreshDisplayTierChoices();
                    this.els.displayTierCombo.value = label;
                    this.els.editSelectedTierLabel.textContent = label;
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
     * Display-zone (base map) change: repopulate the tier choices and optionally refit.
     * @param {boolean} [fitView=true]
     * @returns {void}
     */
    _onDisplayZoneChanged(fitView = true) {
        const zoneId = normalizeZoneId(this.els.displayZoneCombo.value);
        if (!zoneId) return;
        this.els.displayZoneCombo.value = zoneId;
        this._refreshDisplayTierChoices();
        this._meshKey = null;
        this._refreshZoneLabel();
        if (fitView) this._fitView();
    }

    /**
     * Display-tier change: align the base map and mirror the selection into the assert
     * combo and labels.
     * @param {boolean} [fitView=true]
     * @returns {void}
     */
    _onDisplayTierChanged(fitView = true) {
        this._selectDisplayForTier();
        this._meshKey = null;
        this._refreshZoneLabel();
        if (fitView) this._fitView();
        this._doRedraw();
        if (this.els.displayTierCombo.value) {
            const label = this.els.displayTierCombo.value;
            this.els.editSelectedTierLabel.textContent = label;
            const zoneId = this._displayTierZoneId();
            if (!Number.isNaN(zoneId)) {
                const zoneStr = String(zoneId);
                this._ensureAssertZoneOption(zoneStr);
                this.els.assertZoneCombo.value = zoneStr;
                this.els.assertSelectedTierLabel.textContent = label;
            }
        }
    }

    /**
     * Point the display-zone combo at the base map that owns the selected tier.
     * @returns {void}
     */
    _selectDisplayForTier() {
        if (!this.field) return;
        const zoneId = this._displayTierZoneId();
        if (Number.isNaN(zoneId)) return;
        const baseId = this.field.geometryZoneId(zoneId);
        const base = this.field.zoneById(baseId);
        if (
            base &&
            this.field.displayBaseNames().includes(base.name) &&
            this.els.displayZoneCombo.value !== base.name
        ) {
            this.els.displayZoneCombo.value = base.name;
            this._refreshDisplayTierChoices();
        }
    }

    // ==================================================================================
    //  Zone navigation
    // ==================================================================================

    /** Step to the previous route segment. @returns {void} */
    _prevZone() {
        this._clearEditPreview();
        this.editPreviewStartSelected = false;
        this.state.zoneState.prevZone();
        this.state.clearSelection();
        this._syncActionControls();
        this._refreshZoneLabel();
        this._fitView();
    }

    /** Step to the next route segment. @returns {void} */
    _nextZone() {
        this._clearEditPreview();
        this.editPreviewStartSelected = false;
        this.state.zoneState.nextZone();
        this.state.clearSelection();
        this._syncActionControls();
        this._refreshZoneLabel();
        this._fitView();
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

    /** Coordinate frame used by a newly hand-authored NAVMESH waypoint. */
    _manualEditFrame() {
        if (!this.field) return {zone: normalizeZoneId(this.state.currentZone()), targetTier: ""};

        const currentZone = normalizeZoneId(this.state.currentZone());
        const zoneId = currentZone ? this._resolveZoneId(currentZone) : this._displayTierZoneId();
        if (Number.isNaN(zoneId)) return {zone: currentZone, targetTier: ""};
        const zone = this.field.zoneById(zoneId);
        const zoneName = normalizeZoneId(zone && zone.name, currentZone);
        return {
            zone: zoneName,
            targetTier: this.field.isTier(zoneId) && this.field.isRealTier(zoneId) ? zoneName : "",
        };
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

    /** Delete the assert rect or selected route points. @returns {void} */
    _deleteSelectedPoint() {
        if (this.state.mode === Mode.LOG) {
            setStatus("日志分析模式为只读；请用“清除”移除导入的日志。", "#f59e0b");
            return;
        }
        if (this.state.mode === Mode.ASSERT) {
            if (!this.assertRectWorld) {
                setStatus("当前没有可删除的 Assert 区域", "#f59e0b");
                return;
            }
            this.assertRectWorld = null;
            this.isAssertSelecting = false;
            this._assertRectBeforeDrag = null;
            setStatus("已清除 Assert 区域。", "#10b981");
            if (this.navtest) this.navtest.routeChanged();
            this._paint();
            return;
        }
        if (this._hasQuickRouteTest()) {
            this._clearQuickRouteTest();
            setStatus("已清除快速测试。", "#10b981");
            this._paint();
            return;
        }
        if (this.editPreviewStartSelected && this._activeEditPreviewStart()) {
            this._clearEditPreview();
            this._clearEditPreviewStart();
            setStatus("已删除手动规划起点。", "#10b981");
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
        this._clearEditPreview();
        this._syncActionControls();
        this._syncMapControls();
        this._refreshZoneLabel();
        if (this.navtest) this.navtest.routeChanged();
        this._doRedraw();
    }

    // ==================================================================================
    //  Copy actions
    // ==================================================================================

    /** Keep each copy button's label aligned with its selected output format. @returns {void} */
    _syncCopyButtonLabels() {
        this.els.btnCopyPath.textContent = this.els.chkEditZipline.checked || this.els.chkEditExactSlim.checked ? "复制完整参数" : "复制路径";
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
            if (this.els.chkEditZipline.checked || this.els.chkEditExactSlim.checked) {
                const param = {path: result.nodes};
                if (this.els.chkEditZipline.checked) param.zip = true;
                if (this.els.chkEditExactSlim.checked) param.exact_slim = true;
                await this._copyText(JSON.stringify(param, null, 4));
                setStatus("MapNavigator 完整参数已复制到剪贴板", "#10b981");
            } else {
                await this._copyText(result.text);
                setStatus("MapNavigator path 已复制到剪贴板", "#10b981");
            }
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
     * 路径编辑试跑直接使用编辑器原始路点；其他模式不向试跑会话装载内容。
     * @returns {{path: Array, exported: boolean, zip: boolean, assert_target: ?Object}}
     */
    _navtestRoute() {
        if (this.state.mode !== Mode.EDIT) {
            return {path: [], exported: false, zip: false, assert_target: null};
        }
        return {
            path: this.state.points,
            exported: false,
            zip: this.els.chkEditZipline.checked,
            exact_slim: this.els.chkEditExactSlim.checked,
        };
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
     * Delete, 1/2 tool switch, +/- zoom, C copy coords.
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
        if (this._is3DView()) {
            if (e.key === "Escape" && this.threeView?.clearSelection()) {
                setStatus("已清除 3D 点位选择。", "#10b981");
                e.preventDefault();
            } else if (e.key === "+" || e.key === "=" || e.code === "NumpadAdd") {
                this._zoomIn();
                e.preventDefault();
            } else if (e.key === "-" || e.key === "_" || e.code === "NumpadSubtract") {
                this._zoomOut();
                e.preventDefault();
            } else if (
                !e.metaKey &&
                !e.altKey &&
                this.threeView?.setMovementKey(e.code, true, {ctrlKey: e.ctrlKey})
            ) {
                e.preventDefault();
            }
            return;
        }
        if (e.key === "Escape" && this._cancelCurrentSelection()) {
            e.preventDefault();
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
            this._deleteSelectedPoint();
            e.preventDefault();
            return;
        }
        if (e.key === "1") {
            if (this.state.mode === Mode.EDIT) this._activateEditTool("add");
            else if (this.state.mode === Mode.ASSERT) this._setActiveTool("assert-edit");
            e.preventDefault();
            return;
        }
        if (e.key === "2") {
            if (this.state.mode === Mode.EDIT) this._activateEditTool("select");
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

    /** Switch EDIT tools and discard temporary S/G state when explicitly leaving quick test. */
    _activateEditTool(tool) {
        const clearedQuickTest = this.activeTool === "route-test" && this._hasQuickRouteTest();
        if (clearedQuickTest) this._clearQuickRouteTest();
        this._setActiveTool(tool);
        if (clearedQuickTest) {
            this._paint();
            setStatus("已离开快速测试并清除临时点。", "#10b981");
        }
    }

    /**
     * Switch the active canvas tool, updating toolbar highlight + canvas cursor.
     * @param {'pan'|'add'|'select'|'route-test'|'edit-start'|'assert-pan'|'assert-edit'|'log-inspect'|'log-measure'|'log-pan'} tool
     * @returns {void}
     */
    _setActiveTool(tool) {
        this.activeTool = tool;

        const e = this.els;
        if (e.toolPan) e.toolPan.classList.toggle("active", tool === "pan");
        if (e.toolAdd) e.toolAdd.classList.toggle("active", tool === "add");
        if (e.toolSelect) e.toolSelect.classList.toggle("active", tool === "select");
        if (e.toolRouteTest) {
            const testingRoute = tool === "route-test";
            e.toolRouteTest.classList.toggle("active", testingRoute);
            e.toolRouteTest.setAttribute("aria-pressed", String(testingRoute));
        }
        if (e.toolEditStart) {
            const settingEditStart = tool === "edit-start";
            e.toolEditStart.classList.toggle("active", settingEditStart);
            e.toolEditStart.setAttribute("aria-pressed", String(settingEditStart));
        }
        if (e.toolAssertPan) e.toolAssertPan.classList.toggle("active", tool === "assert-pan");
        if (e.toolAssertEdit) e.toolAssertEdit.classList.toggle("active", tool === "assert-edit");
        if (e.btnLogMeasure) {
            const measuring = tool === "log-measure";
            e.btnLogMeasure.classList.toggle("active", measuring);
            e.btnLogMeasure.setAttribute("aria-pressed", String(measuring));
        }

        const canvas = e.overlayCanvas;
        if (tool === "pan" || tool === "assert-pan" || tool === "log-pan") {
            canvas.style.cursor = "grab";
        } else if (tool === "log-measure") {
            canvas.style.cursor = "crosshair";
        } else if (tool === "log-inspect") {
            canvas.style.cursor = "default";
        } else if (tool === "add" || tool === "edit-start" || tool === "route-test") {
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
        if (this.state.mode === Mode.LOG) return;
        if (this.state.undo()) this._afterHistory();
    }

    /** @returns {void} */
    _redo() {
        if (this.state.mode === Mode.LOG) return;
        if (this.state.redo()) this._afterHistory();
    }

    /** Refresh controls + repaint after an undo/redo restored a snapshot. @returns {void} */
    _afterHistory() {
        this._clearEditPreview();
        this._syncActionControls();
        this._syncMapControls();
        this._refreshZoneLabel();
        if (this.navtest) this.navtest.routeChanged();
        this._doRedraw();
    }

    /** C key: copy coords (tk `_on_copy_coord_key`). */
    _copyCoordKey() {
        if (this.state.mode === Mode.LOG) return;
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
        if (this._is3DView()) {
            if (this.threeView) this.threeView.zoomBy(1.25);
            return;
        }
        this.camera.zoomAt(this._cssW / 2, this._cssH / 2, 1.25);
        this._paint();
    }

    /** Zoom out around the canvas center (button / `-` key). @returns {void} */
    _zoomOut() {
        if (this._is3DView()) {
            if (this.threeView) this.threeView.zoomBy(0.8);
            return;
        }
        this.camera.zoomAt(this._cssW / 2, this._cssH / 2, 0.8);
        this._paint();
    }

    // ==================================================================================
    //  Property controls + labels
    // ==================================================================================

    /** Reset the property panel to its no-selection defaults. @returns {void} */
    _resetPropertyControls() {
        this.els.actionMenu.value = ACTION_NAMES[ActionType.NAVMESH];
        this.els.chkStrict.checked = false;
        this.els.chkRequired.checked = false;
        this.els.targetTierEntry.value = "";
        this.els.actionChainLabel.textContent = "Navmesh";
        if (this.els.editDeckBox) this.els.editDeckBox.hidden = true;
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
        this._renderEditInspection();
        this._renderWaypointList();
        this._renderEditDeckList();
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
        this._syncEditPreviewStartControls();
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
        if (!this.state.points.length) {
            const label = this.els.displayTierCombo.value || "未选择层级";
            this.els.zoneLabel.textContent = `新路径: ${label}`;
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

    /** Enable the shared map picker for an empty edit route. */
    _syncMapControls() {
        const emptyEdit = this.state.mode === Mode.EDIT && !this.state.points.length;
        this.els.displayTierCombo.disabled = true;
        this.els.displayZoneCombo.disabled = true;
        if (this.state.mode !== Mode.ASSERT) {
            this.els.btnPrev.disabled = false;
            this.els.btnNext.disabled = false;
        }
        if (emptyEdit) {
            this.els.btnSelectTier.disabled = !(this.field && this.field.displayBaseNames().length);
            this.els.editSelectedTierLabel.textContent = this.els.displayTierCombo.value || "未选择层级";
            this.els.editSelectedTierLabel.style.display = "inline-block";
        } else if (this.state.mode === Mode.EDIT) {
            this.els.btnSelectTier.disabled = true;
            const zone = normalizeZoneId(this.state.currentZone());
            const zoneId = this._resolveZoneId(zone);
            this.els.editSelectedTierLabel.textContent =
                !Number.isNaN(zoneId) && this.field ? this.field.zoneLabel(zoneId) || zone : zone || "未选择层级";
            this.els.editSelectedTierLabel.style.display = "inline-block";
        } else {
            this.els.btnSelectTier.disabled = true;
            this.els.editSelectedTierLabel.style.display = "none";
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
        this._clearEditPreview();
        this._clearQuickRouteTest();
        this._clearEditPreviewStart();
        this.state.setPoints(rawPoints);
        this.state.clearSelection();
        this._syncActionControls();
        this._syncMapControls();
        this._refreshZoneLabel();
        setStatus(
            "录制结束。滚轮缩放，右键或 Alt+拖拽平移；添加工具左键点击插点，拖拽路点微调，Ctrl+拖拽框选批量操作，C 键复制选中点坐标。",
            "#10b981",
        );
        this._fitView();
    }

    /**
     * {@link Importer} parsed a path import.
     * Edit uses the points as its route; Assert draws them read-only as a reference.
     * @param {object[]} points
     * @param {{zipEnabled?:boolean}} [options]
     * @returns {{text?:string, color?:string}|void} a status lead-in replacing the importer's default
     */
    _importLoadPoints(points, options = {}) {
        this._clearEditPreview();
        this._clearQuickRouteTest();
        this._clearEditPreviewStart();
        if (this.state.mode === Mode.EDIT) {
            this.els.chkEditZipline.checked = !!options.zipEnabled;
            this._syncCopyButtonLabels();
        }
        this.state.setPoints(points);
        this.state.clearSelection();
        this._resetPropertyControls();
        this._afterStructureChanged();

        if (this.state.mode !== Mode.ASSERT || !this.field || !this.state.points.length) {
            this._fitView();
            return;
        }

        const zoneId = this._resolveZoneId(this.state.points[0].zone);
        if (!Number.isNaN(zoneId) && this._selectDisplayZoneById(zoneId)) this._onDisplayTierChanged(false);
        const drawn = this._displayRealPoints();
        this._fitDisplayPoints(drawn);
        if (!drawn.length) return this._noNavmeshBasemapNote(points, "断言模式画不出这些点（路线已载入）");
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
        if (this.viewMode !== "2d") this._setViewMode("2d", {announce: false});
        this.state.mode = Mode.ASSERT;
        this._ensureAssertZoneOption(zoneId);
        this.els.assertZoneCombo.value = zoneId;
        // The display controls drive the basemap in Assert mode too. This mirrors the
        // zone and clears assertRectWorld, so it must run before the rect is set.
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
        this._assertRectBeforeDrag = null;
        this._syncAssertControls();
        this._syncMapControls();
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
     * Sidebar mode-tab click: switch to edit / assert / log, clearing the other
     * modes' canvas artifacts and picking a default zone where needed.
     * @param {'edit'|'assert'|'log'} modeName
     * @returns {void}
     */
    _selectModeTab(modeName) {
        const e = this.els;

        if (modeName !== "edit" && this.navtest && this.navtest.running) {
            setStatus("请先按 F4 终止当前实机试跑，再切换模式。", "#f59e0b");
            return;
        }
        if (modeName !== "edit" && this.viewMode !== "2d") {
            this._setViewMode("2d", {announce: false});
        }

        this.deckPreview = null;
        this.renderer.setDeckBand(null);

        if (modeName !== "edit") {
            this._clearEditPreview();
            this._clearQuickRouteTest();
        }
        if (modeName !== "assert") {
            this.assertRectWorld = null;
            this.isAssertSelecting = false;
            this._assertRectBeforeDrag = null;
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
        this._syncMapControls();
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
        e.tabAssert.classList.remove("active");
        e.tabLog.classList.remove("active");
        e.tabRoute.classList.remove("active");

        const logWorkspace = mode === Mode.LOG;
        const navtestAvailable = mode === Mode.EDIT;
        e.tabLog.classList.toggle("active", logWorkspace);
        e.tabLog.setAttribute("aria-pressed", String(logWorkspace));
        e.tabRoute.classList.toggle("active", !logWorkspace);
        e.tabRoute.setAttribute("aria-pressed", String(!logWorkspace));
        e.panelConnection.hidden = logWorkspace;
        e.panelNavtest.hidden = !navtestAvailable;
        e.routeModeTabs.hidden = logWorkspace;
        e.positionReadout.hidden = logWorkspace;
        e.toolRouteTest.hidden = mode !== Mode.EDIT;
        e.toolEditStart.hidden = mode !== Mode.EDIT;
        e.editStartDivider.hidden = mode !== Mode.EDIT;
        e.btnLogMeasure.hidden = !logWorkspace;
        e.logMeasureDivider.hidden = !logWorkspace;
        if (this.connection) this.connection.setSuspended(logWorkspace);

        e.panelRecording.hidden = true;
        e.panelEditMap.hidden = true;
        e.panelAssertMap.hidden = true;
        e.panelProperties.hidden = true;
        e.panelAssert.hidden = true;
        e.panelLog.hidden = true;
        e.btnDelPointFloat.hidden = mode === Mode.LOG;
        if (this.navtest) this.navtest.setDisabled(!navtestAvailable);

        if (mode === Mode.ASSERT) {
            e.tabAssert.classList.add("active");
            e.panelAssertMap.hidden = false;
            e.panelAssert.hidden = false;
            e.canvasWrap.classList.remove("mode-edit", "mode-log");
            e.canvasWrap.classList.add("mode-assert");
            document.body.classList.remove("mode-edit", "mode-log");
            document.body.classList.add("mode-assert");
            this._setActiveTool("assert-edit");
        } else if (mode === Mode.LOG) {
            e.panelLog.hidden = false;
            e.canvasWrap.classList.remove("mode-edit", "mode-assert");
            e.canvasWrap.classList.add("mode-log");
            document.body.classList.remove("mode-edit", "mode-assert");
            document.body.classList.add("mode-log");
            this._setActiveTool(
                this.activeTool === "log-measure" || this.activeTool === "log-inspect"
                    ? this.activeTool
                    : "log-inspect",
            );
        } else {
            e.tabEdit.classList.add("active");
            e.panelEditMap.hidden = false;
            e.panelRecording.hidden = false;
            e.panelProperties.hidden = false;
            e.canvasWrap.classList.remove("mode-assert", "mode-log");
            e.canvasWrap.classList.add("mode-edit");
            document.body.classList.remove("mode-assert", "mode-log");
            document.body.classList.add("mode-edit");
            this._setActiveTool("add");
        }
        e.tabAssert.setAttribute("aria-pressed", String(mode === Mode.ASSERT));
        e.tabEdit.setAttribute("aria-pressed", String(mode === Mode.EDIT));
        this._syncViewModeUI();
        this._renderEditInspection();
        this._syncLogContextPanel();
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
     * a card applies the tier to the current mode.
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
            currentSelectedLabel = e.displayTierCombo.value;
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
                    e.displayZoneCombo.value = baseName;
                    this._refreshDisplayTierChoices();
                    e.displayTierCombo.value = choice.label;
                    this._onDisplayTierChanged();
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
