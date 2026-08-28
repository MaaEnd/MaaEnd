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
