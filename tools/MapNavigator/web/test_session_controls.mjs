import assert from "node:assert/strict";
import {readFileSync} from "node:fs";
import test from "node:test";

import {ConnectionPanel} from "./static/js/ui/connection.js";
import {NavTestController} from "./static/js/ui/navtest.js";
import {RecordingController} from "./static/js/ui/recording.js";

class FakeButton {
  constructor() {
    this.disabled = false;
    this.textContent = "";
  }

  addEventListener() {}
}

class FakeConnection {
  constructor() {
    this.connected = false;
    this.listeners = [];
  }

  onStatusChange(listener) {
    this.listeners.push(listener);
    listener(this.connected);
  }

  setConnected(connected) {
    this.connected = connected;
    for (const listener of this.listeners) listener(connected);
  }
}

const fakeClassList = () => ({add() {}, remove() {}});

test("connection panel publishes readiness changes", () => {
  globalThis.document = {getElementById: () => null};
  const panel = new ConnectionPanel({});
  const observed = [];

  panel.onStatusChange((connected) => observed.push(connected));
  panel._setConnected(true);
  panel._setConnected(true);
  panel._setConnected(false);

  assert.deepEqual(observed, [false, true, false]);
  assert.equal(panel.isConnected(), false);
});

test("suspending connection probes clears stale status-dot styles", () => {
  const removed = [];
  const statusDot = {classList: {remove: (...classes) => removed.push(classes)}};
  globalThis.document = {getElementById: (id) => (id === "status-dot" ? statusDot : null)};
  const panel = new ConnectionPanel({});

  panel.setSuspended(true);

  assert.deepEqual(removed, [["connected", "connecting"]]);
  assert.equal(panel.isConnected(), false);
});

test("live-position buttons start disabled and use the primary blue style", () => {
  const html = readFileSync(new URL("./static/index.html", import.meta.url), "utf8");
  for (const id of ["btn-edit-locate", "btn-assert-locate"]) {
    const tag = html.match(new RegExp(`<button[^>]*id="${id}"[^>]*>`))?.[0] || "";
    assert.match(tag, /class="[^"]*btn-primary[^"]*"/);
    assert.match(tag, /\bdisabled\b/);
  }
  for (const id of ["btn-start", "btn-navtest-run"]) {
    const tag = html.match(new RegExp(`<button[^>]*id="${id}"[^>]*>`))?.[0] || "";
    assert.match(tag, /\bdisabled\b/);
  }
  assert.doesNotMatch(html, /id="(?:tab|panel|btn|tool)-astar/);
});

test("live location stays off until the user enables it", () => {
  const html = readFileSync(new URL("./static/index.html", import.meta.url), "utf8");
  const source = readFileSync(new URL("./static/js/main.js", import.meta.url), "utf8");
  const button = html.match(/<button[^>]*id="btn-live-locate"[^>]*>[\s\S]*?<\/button>/)?.[0] || "";

  assert.match(button, /class="[^"]*position-live-btn[^"]*"/);
  assert.doesNotMatch(button, /class="[^"]*btn-danger[^"]*"/);
  assert.match(button, /\bdisabled\b/);
  assert.match(button, /开启实时定位/);
  assert.doesNotMatch(source, /_liveLocateAutoStarted/);
  assert.equal(source.match(/this\._toggleLiveLocate\(\)/g)?.length, 1);
});

test("edit and assert use separate but matching map and location cards", () => {
  const html = readFileSync(new URL("./static/index.html", import.meta.url), "utf8");
  const editMapStart = html.indexOf('id="panel-edit-map"');
  const assertMapStart = html.indexOf('id="panel-assert-map"');
  const navtestStart = html.indexOf('id="panel-navtest"');
  const recordingStart = html.indexOf('id="panel-recording"');
  const propertiesStart = html.indexOf('id="panel-properties"');
  const assertStart = html.indexOf('id="panel-assert"');
  const loadProgressStart = html.indexOf('id="load-progress"');

  assert.ok(editMapStart > 0 && assertMapStart > editMapStart && navtestStart > assertMapStart);
  const editMap = html.slice(editMapStart, assertMapStart);
  const assertMap = html.slice(assertMapStart, navtestStart);
  const recording = html.slice(recordingStart, propertiesStart);
  const assertPanel = html.slice(assertStart, loadProgressStart);

  for (const card of [editMap, assertMap]) {
    assert.match(card, /<span class="section-title">地图与定位<\/span>/);
    assert.match(card, />\s*选择底图与层级\s*<\/button>/);
    assert.match(card, />\s*标出游戏内当前位置（参考点）\s*<\/button>/);
    assert.match(card, />当前层级:<\/span>/);
  }
  assert.match(editMap, /id="btn-select-tier"/);
  assert.match(editMap, /id="btn-edit-locate"/);
  assert.match(assertMap, /id="btn-select-assert-tier"/);
  assert.match(assertMap, /id="btn-assert-locate"/);
  assert.doesNotMatch(recording, /id="btn-select-tier"|id="btn-edit-locate"/);
  assert.doesNotMatch(assertPanel, /id="btn-select-assert-tier"|id="btn-assert-locate"/);
});

test("edit planning exposes a preview-only manual start control", () => {
  const html = readFileSync(new URL("./static/index.html", import.meta.url), "utf8");
  const recordingStart = html.indexOf('id="panel-recording"');
  const propertiesStart = html.indexOf('id="panel-properties"');
  const editorToolbarStart = html.indexOf('class="canvas-editor-toolbar"');
  const floatingToolbarStart = html.indexOf('class="canvas-floating-controls"');
  const floatingToolbarEnd = html.indexOf('id="context-panel"');
  const recording = html.slice(recordingStart, propertiesStart);
  const editorToolbar = html.slice(editorToolbarStart, floatingToolbarStart);
  const floatingToolbar = html.slice(floatingToolbarStart, floatingToolbarEnd);

  assert.doesNotMatch(editorToolbar, /id="tool-edit-start"/);
  assert.match(floatingToolbar, /id="tool-edit-start"\s+class="btn btn-float"/);
  assert.match(floatingToolbar, /id="edit-start-divider"[^>]*hidden/);
  assert.doesNotMatch(recording, /id="tool-edit-start"|id="btn-edit-start"/);
  assert.match(recording, /手动起点只用于规划预览，不会写入作者路径/);
  assert.match(html, /id="edit-inspection-box"[^>]*hidden/);
});

test("all 2D modes share one map-layer panel separate from route zipline planning", () => {
  const html = readFileSync(new URL("./static/index.html", import.meta.url), "utf8");
  const floatingToolbarStart = html.indexOf('class="canvas-floating-controls"');
  const floatingToolbarEnd = html.indexOf('class="canvas-right-panels"');
  const floatingToolbar = html.slice(floatingToolbarStart, floatingToolbarEnd);
  const layerPanelStart = html.indexOf('id="map-layer-panel"');
  const layerPanelEnd = html.indexOf('id="context-panel"');
  const layerPanel = html.slice(layerPanelStart, layerPanelEnd);
  const logPanelStart = html.indexOf('id="panel-log"');
  const logSidebar = html.slice(logPanelStart, floatingToolbarStart);

  assert.match(floatingToolbar, /id="btn-map-layers"[^>]*aria-controls="map-layer-panel"[^>]*aria-expanded="false"/);
  assert.match(floatingToolbar, /id="btn-zipline-measure"[^>]*aria-pressed="false"[^>]*hidden/);
  assert.match(floatingToolbar, /id="zipline-measure-divider"[^>]*hidden/);
  for (const id of ["map-show-basemap", "map-show-navmesh", "map-show-ziplines"]) {
    assert.match(layerPanel, new RegExp(`id="${id}"`));
  }
  for (const id of [
    "log-show-authored",
    "log-show-walk",
    "log-show-observed",
    "log-show-baseline",
    "log-show-zipline",
    "log-show-selected-towers",
    "log-show-estimates",
  ]) {
    assert.match(layerPanel, new RegExp(`id="${id}"`));
    assert.doesNotMatch(logSidebar, new RegExp(`id="${id}"`));
  }
  assert.doesNotMatch(html, /id="btn-toggle-ziplines"|id="log-show-recorded-towers"/);
  assert.doesNotMatch(floatingToolbar, /id="chk-edit-zipline"/);
  assert.match(html, /id="chk-edit-zipline" type="checkbox" \/> 启用滑索规划/);
});

test("edit objects and shared point details expose matching cancel-selection controls", () => {
  const html = readFileSync(new URL("./static/index.html", import.meta.url), "utf8");

  assert.match(html, /id="btn-edit-selection-clear"[^>]*>取消选择<\/button>/);
  assert.match(html, /id="btn-point-clear"[^>]*>取消选择<\/button>/);
  assert.match(html, /id="point-inspection-box"[^>]*hidden/);
  assert.match(html, /id="zipline-distance-box"[^>]*hidden/);
});

test("recording start follows the probed connection state", () => {
  const connection = new FakeConnection();
  const btnStart = new FakeButton();
  const btnStop = new FakeButton();
  new RecordingController({
    btnStart,
    btnStop,
    appEl: null,
    connection,
  });

  assert.equal(btnStart.disabled, true);
  assert.equal(btnStop.disabled, true);

  connection.setConnected(true);
  assert.equal(btnStart.disabled, false);

  connection.setConnected(false);
  assert.equal(btnStart.disabled, true);
});

test("first navtest run follows the probe while a live session keeps its own state", () => {
  const connection = new FakeConnection();
  const btnRun = new FakeButton();
  const btnStop = new FakeButton();
  const armedLabel = {textContent: ""};
  const hotkeyNote = {innerHTML: "hotkeys", textContent: "", classList: fakeClassList()};
  const controller = new NavTestController({
    btnRun,
    btnStop,
    armedLabel,
    overlay: {hidden: true},
    hotkeyNote,
    connection,
    getRoute: () => ({
      path: [[1, 2]],
      exported: false,
      assert_target: null,
    }),
  });

  assert.equal(btnRun.disabled, true);
  assert.match(armedLabel.textContent, /连接状态未就绪/);

  connection.setConnected(true);
  assert.equal(btnRun.disabled, false);

  controller.socket = {};
  controller.connected = true;
  connection.setConnected(false);
  assert.equal(btnRun.disabled, false);
});
