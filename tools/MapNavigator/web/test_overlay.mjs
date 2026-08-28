import assert from "node:assert/strict";
import test from "node:test";

import {Overlay} from "./static/js/gl/overlay.js";

function renderWithMarker(mode, markerKey) {
    const overlay = Object.create(Overlay.prototype);
    overlay.dpr = 1;
    overlay.cssW = 800;
    overlay.cssH = 600;
    overlay.ctx = {
        setTransform() {},
        clearRect() {},
    };
    overlay._drawPath = () => {};
    overlay._drawAstarPreview = () => {};
    overlay._drawNodes = () => {};
    overlay._drawAssertRect = () => {};
    overlay._drawAstarDiagnostics = () => {};
    overlay._drawLivePath = () => {};
    overlay._drawLogAnalysis = () => {};
    overlay._drawOffMeshMarks = () => {};
    overlay._drawSelectionRect = () => {};
    overlay._drawPlanningStartMarker = () => {};

    const markers = [];
    overlay._drawHintMarker = (_camera, x, y, label, rot) => markers.push({x, y, label, rot});
    overlay.render(
        {},
        {
            mode,
            points: [],
            [markerKey]: {x: 12, y: 34, label: "游戏当前位置", rot: 90},
        },
    );
    return markers;
}

test("draws the game-position reference marker in edit mode", () => {
    assert.deepEqual(renderWithMarker("edit", "editLocateHint"), [
        {x: 12, y: 34, label: "游戏当前位置", rot: 90},
    ]);
});

test("does not leak the edit reference marker into assert mode", () => {
    assert.deepEqual(renderWithMarker("assert", "editLocateHint"), []);
});

test("draws the manual planning start only in edit mode", () => {
    const overlay = Object.create(Overlay.prototype);
    overlay.dpr = 1;
    overlay.cssW = 800;
    overlay.cssH = 600;
    overlay.ctx = {
        setTransform() {},
        clearRect() {},
    };
    overlay._drawPath = () => {};
    overlay._drawAstarPreview = () => {};
    overlay._drawNodes = () => {};
    overlay._drawAssertRect = () => {};
    overlay._drawAstarDiagnostics = () => {};
    overlay._drawLivePath = () => {};
    overlay._drawLogAnalysis = () => {};
    overlay._drawOffMeshMarks = () => {};
    overlay._drawSelectionRect = () => {};
    overlay._drawHintMarker = () => {};

    const markers = [];
    overlay._drawPlanningStartMarker = (_camera, marker) => markers.push(marker);
    const marker = {x: 12, y: 34, label: "规划起点"};
    overlay.render({}, {mode: "edit", points: [], editPreviewStart: marker});
    overlay.render({}, {mode: "assert", points: [], editPreviewStart: marker});

    assert.deepEqual(markers, [marker]);
});

test("draws selected-route diagnostics in edit mode", () => {
    const overlay = Object.create(Overlay.prototype);
    overlay.dpr = 1;
    overlay.cssW = 800;
    overlay.cssH = 600;
    overlay.ctx = {
        setTransform() {},
        clearRect() {},
    };
    overlay._drawPath = () => {};
    overlay._drawAstarPreview = () => {};
    overlay._drawNodes = () => {};
    overlay._drawAssertRect = () => {};
    overlay._drawLivePath = () => {};
    overlay._drawLogAnalysis = () => {};
    overlay._drawOffMeshMarks = () => {};
    overlay._drawSelectionRect = () => {};
    overlay._drawHintMarker = () => {};

    const calls = [];
    overlay._drawAstarDiagnostics = (_camera, diagnostics, options) => calls.push({diagnostics, options});
    const diagnostics = [{astar_cells: [[1, 2]]}];
    const debugOptions = {search: true};
    overlay.render(
        {},
        {
            mode: "edit",
            points: [],
            editPreview: {diagnostics, debugOptions},
        },
    );

    assert.deepEqual(calls, [{diagnostics, options: debugOptions}]);
});

test("draws the runtime-reported failed leg in edit mode", () => {
    const overlay = Object.create(Overlay.prototype);
    overlay.dpr = 1;
    overlay.cssW = 800;
    overlay.cssH = 600;
    overlay.ctx = {
        setTransform() {},
        clearRect() {},
    };
    overlay._drawPath = () => {};
    overlay._drawAstarPreview = () => {};
    overlay._drawAstarDiagnostics = () => {};
    overlay._drawNodes = () => {};
    overlay._drawLivePath = () => {};
    overlay._drawOffMeshMarks = () => {};
    overlay._drawSelectionRect = () => {};

    const calls = [];
    overlay._drawRouteFailure = (_camera, failure) => calls.push(failure);
    const failure = {segment_start: [10, 20], segment_goal: [30, 40]};
    overlay.render({}, {mode: "edit", points: [], editPreview: {failure}});

    assert.deepEqual(calls, [failure]);
});

test("prefers the closest runtime gap over the whole failed leg", () => {
    const overlay = Object.create(Overlay.prototype);
    const lines = [];
    overlay.ctx = {
        save() {},
        restore() {},
        setLineDash() {},
        beginPath() {},
        moveTo(x, y) {
            lines.push(["move", x, y]);
        },
        lineTo(x, y) {
            lines.push(["line", x, y]);
        },
        stroke() {},
        arc() {},
        fill() {},
    };
    overlay._drawCaption = () => {};
    const camera = {worldToCanvas: (x, y) => [x, y]};

    overlay._drawRouteFailure(camera, {
        segment_start: [10, 20],
        segment_goal: [300, 400],
        gap_start: [101, 102],
        gap_goal: [111, 112],
        gap_distance: 14.1,
    });

    assert.deepEqual(lines.slice(0, 2), [
        ["move", 101, 102],
        ["line", 111, 112],
    ]);
});

test("draws a live test path in edit mode without a planned preview", () => {
    const overlay = Object.create(Overlay.prototype);
    overlay.dpr = 1;
    overlay.cssW = 800;
    overlay.cssH = 600;
    overlay.ctx = {
        setTransform() {},
        clearRect() {},
    };
    overlay._drawPath = () => {};
    overlay._drawAstarPreview = () => {};
    overlay._drawNodes = () => {};
    overlay._drawAssertRect = () => {};
    overlay._drawAstarDiagnostics = () => {};
    overlay._drawLogAnalysis = () => {};
    overlay._drawOffMeshMarks = () => {};
    overlay._drawSelectionRect = () => {};
    overlay._drawHintMarker = () => {};

    const calls = [];
    overlay._drawLivePath = (_camera, livePath) => calls.push(livePath);
    const livePath = {points: [{x: 1, y: 2}], current: {x: 1, y: 2}};
    overlay.render({}, {mode: "edit", points: [], livePath});

    assert.deepEqual(calls, [livePath]);
});
