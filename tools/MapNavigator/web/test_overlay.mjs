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
