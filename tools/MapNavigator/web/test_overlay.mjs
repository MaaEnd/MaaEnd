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
  assert.deepEqual(renderWithMarker("edit", "editLocateHint"), [{x: 12, y: 34, label: "游戏当前位置", rot: 90}]);
});

test("shows assert resize handles only while the assert frame is selected", () => {
  const overlay = Object.create(Overlay.prototype);
  overlay.dpr = 1;
  overlay.cssW = 800;
  overlay.cssH = 600;
  overlay.ctx = {setTransform() {}, clearRect() {}};
  overlay._drawMapZiplines = () => {};
  overlay._drawPath = () => {};
  overlay._drawNodes = () => {};
  overlay._drawPointInspection = () => {};
  overlay._drawZiplineMeasurement = () => {};
  overlay._drawOffMeshMarks = () => {};

  const calls = [];
  overlay._drawAssertRect = (_camera, target, selected) => calls.push({target, selected});
  const target = [10, 20, 30, 40];
  overlay.render({}, {mode: "assert", points: [], assertTarget: target, assertSelected: true});
  overlay.render({}, {mode: "assert", points: [], assertTarget: target, assertSelected: false});

  assert.deepEqual(calls, [
    {target, selected: true},
    {target, selected: false},
  ]);
});

test("draws the recorded zipline layer in edit and assert modes", () => {
  const overlay = Object.create(Overlay.prototype);
  overlay.dpr = 1;
  overlay.cssW = 800;
  overlay.cssH = 600;
  overlay.ctx = {setTransform() {}, clearRect() {}};
  overlay._drawPath = () => {};
  overlay._drawNodes = () => {};
  overlay._drawAssertRect = () => {};
  overlay._drawOffMeshMarks = () => {};
  overlay._drawSelectionRect = () => {};

  const calls = [];
  overlay._drawMapZiplines = (_camera, towers) => calls.push(towers);
  const towers = [
    {
      point: [12, 34],
    },
  ];
  overlay.render({}, {mode: "edit", points: [], mapZiplines: towers});
  overlay.render({}, {mode: "assert", points: [], mapZiplines: towers});
  overlay.render({}, {mode: "log", points: [], mapZiplines: towers});

  assert.deepEqual(calls, [towers, towers]);
});

test("shares zipline inspection and measurement overlays across all 2D modes", () => {
  const overlay = Object.create(Overlay.prototype);
  overlay.dpr = 1;
  overlay.cssW = 800;
  overlay.cssH = 600;
  overlay.ctx = {setTransform() {}, clearRect() {}};
  overlay._drawPath = () => {};
  overlay._drawNodes = () => {};
  overlay._drawAssertRect = () => {};
  overlay._drawLogAnalysis = () => {};
  overlay._drawOffMeshMarks = () => {};
  overlay._drawSelectionRect = () => {};

  const inspections = [];
  const measurements = [];
  overlay._drawPointInspection = (_camera, inspection) => inspections.push(inspection);
  overlay._drawZiplineMeasurement = (_camera, measurement) => measurements.push(measurement);
  const inspection = {point: [12, 34], title: "滑索架"};
  const measurement = {towers: [{point: [12, 34], marker: "A"}]};

  overlay.render({}, {mode: "edit", points: [], pointInspection: inspection, ziplineMeasurement: measurement});
  overlay.render(
    {},
    {mode: "log", points: [], logAnalysis: {}, pointInspection: inspection, ziplineMeasurement: measurement},
  );
  overlay.render({}, {mode: "assert", points: [], pointInspection: inspection, ziplineMeasurement: measurement});

  assert.deepEqual(inspections, [inspection, inspection, inspection]);
  assert.deepEqual(measurements, [measurement, measurement, measurement]);
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

test("draws quick-test endpoints only in edit mode", () => {
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
  overlay._drawHintMarker = () => {};

  const calls = [];
  overlay._drawQuickRouteTestMarkers = (_camera, routeTest) => calls.push(routeTest);
  const routeTest = {
    start: {x: 12, y: 34, label: "测试起点"},
    goal: {x: 56, y: 78, label: "测试终点"},
  };
  overlay.render({}, {mode: "edit", points: [], quickRouteTest: routeTest});
  overlay.render({}, {mode: "assert", points: [], quickRouteTest: routeTest});

  assert.deepEqual(calls, [routeTest]);
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
  const diagnostics = [
    {
      astar_cells: [[1, 2]],
    },
  ];
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
  const failure = {
    segment_start: [10, 20],
    segment_goal: [30, 40],
  };
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
  const camera = {
    worldToCanvas: (x, y) => [x, y],
  };

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
  const livePath = {
    points: [{x: 1, y: 2}],
    current: {x: 1, y: 2},
  };
  overlay.render({}, {mode: "edit", points: [], livePath});

  assert.deepEqual(calls, [livePath]);
});
