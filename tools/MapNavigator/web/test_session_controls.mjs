import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
import test from 'node:test';

import { ConnectionPanel } from './static/js/ui/connection.js';
import { NavTestController } from './static/js/ui/navtest.js';
import { RecordingController } from './static/js/ui/recording.js';

class FakeButton {
  constructor() {
    this.disabled = false;
    this.textContent = '';
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

const fakeClassList = () => ({ add() {}, remove() {} });

test('connection panel publishes readiness changes', () => {
  globalThis.document = { getElementById: () => null };
  const panel = new ConnectionPanel({});
  const observed = [];

  panel.onStatusChange((connected) => observed.push(connected));
  panel._setConnected(true);
  panel._setConnected(true);
  panel._setConnected(false);

  assert.deepEqual(observed, [false, true, false]);
  assert.equal(panel.isConnected(), false);
});

test('suspending connection probes clears stale status-dot styles', () => {
  const removed = [];
  const statusDot = { classList: { remove: (...classes) => removed.push(classes) } };
  globalThis.document = { getElementById: (id) => (id === 'status-dot' ? statusDot : null) };
  const panel = new ConnectionPanel({});

  panel.setSuspended(true);

  assert.deepEqual(removed, [['connected', 'connecting']]);
  assert.equal(panel.isConnected(), false);
});

test('live-position buttons start disabled and use the primary blue style', () => {
  const html = readFileSync(new URL('./static/index.html', import.meta.url), 'utf8');
  for (const id of ['btn-edit-locate', 'btn-assert-locate', 'btn-astar-locate']) {
    const tag = html.match(new RegExp(`<button[^>]*id="${id}"[^>]*>`))?.[0] || '';
    assert.match(tag, /class="[^"]*btn-primary[^"]*"/);
    assert.match(tag, /\bdisabled\b/);
  }
  for (const id of ['btn-start', 'btn-navtest-run']) {
    const tag = html.match(new RegExp(`<button[^>]*id="${id}"[^>]*>`))?.[0] || '';
    assert.match(tag, /\bdisabled\b/);
  }
});

test('edit and assert use separate but matching map and location cards', () => {
  const html = readFileSync(new URL('./static/index.html', import.meta.url), 'utf8');
  const editMapStart = html.indexOf('id="panel-edit-map"');
  const assertMapStart = html.indexOf('id="panel-assert-map"');
  const navtestStart = html.indexOf('id="panel-navtest"');
  const recordingStart = html.indexOf('id="panel-recording"');
  const propertiesStart = html.indexOf('id="panel-properties"');
  const assertStart = html.indexOf('id="panel-assert"');
  const astarStart = html.indexOf('id="panel-astar"');

  assert.ok(editMapStart > 0 && assertMapStart > editMapStart && navtestStart > assertMapStart);
  const editMap = html.slice(editMapStart, assertMapStart);
  const assertMap = html.slice(assertMapStart, navtestStart);
  const recording = html.slice(recordingStart, propertiesStart);
  const assertPanel = html.slice(assertStart, astarStart);

  for (const card of [editMap, assertMap]) {
    assert.match(card, /<span class="section-title">地图与定位<\/span>/);
    assert.match(card, />选择底图与层级<\/button>/);
    assert.match(card, />标出游戏内当前位置（参考点）<\/button>/);
    assert.match(card, />当前层级:<\/span>/);
  }
  assert.match(editMap, /id="btn-select-tier"/);
  assert.match(editMap, /id="btn-edit-locate"/);
  assert.match(assertMap, /id="btn-select-assert-tier"/);
  assert.match(assertMap, /id="btn-assert-locate"/);
  assert.doesNotMatch(recording, /id="btn-select-tier"|id="btn-edit-locate"/);
  assert.doesNotMatch(assertPanel, /id="btn-select-assert-tier"|id="btn-assert-locate"/);
});

test('edit planning exposes a preview-only manual start control', () => {
  const html = readFileSync(new URL('./static/index.html', import.meta.url), 'utf8');
  const recordingStart = html.indexOf('id="panel-recording"');
  const propertiesStart = html.indexOf('id="panel-properties"');
  const editorToolbarStart = html.indexOf('class="canvas-editor-toolbar"');
  const floatingToolbarStart = html.indexOf('class="canvas-floating-controls"');
  const floatingToolbarEnd = html.indexOf('id="log-context-panel"');
  const recording = html.slice(recordingStart, propertiesStart);
  const editorToolbar = html.slice(editorToolbarStart, floatingToolbarStart);
  const floatingToolbar = html.slice(floatingToolbarStart, floatingToolbarEnd);

  assert.doesNotMatch(editorToolbar, /id="tool-edit-start"/);
  assert.match(floatingToolbar, /id="tool-edit-start" class="btn btn-float"/);
  assert.match(floatingToolbar, /id="edit-start-divider"[^>]*hidden/);
  assert.doesNotMatch(recording, /id="tool-edit-start"|id="btn-edit-start"/);
  assert.match(recording, /手动起点只用于规划预览，不会写入作者路径/);
  assert.match(html, /id="edit-inspection-box"[^>]*hidden/);
});

test('edit and log point details expose matching cancel-selection controls', () => {
  const html = readFileSync(new URL('./static/index.html', import.meta.url), 'utf8');

  assert.match(html, /id="btn-edit-selection-clear"[^>]*>取消选择<\/button>/);
  assert.match(html, /id="btn-log-point-clear"[^>]*>取消选择<\/button>/);
});

test('recording start follows the probed connection state', () => {
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

test('first navtest run follows the probe while a live session keeps its own state', () => {
  const connection = new FakeConnection();
  const btnRun = new FakeButton();
  const btnStop = new FakeButton();
  const armedLabel = { textContent: '' };
  const hotkeyNote = { innerHTML: 'hotkeys', textContent: '', classList: fakeClassList() };
  const controller = new NavTestController({
    btnRun,
    btnStop,
    armedLabel,
    overlay: { hidden: true },
    hotkeyNote,
    connection,
    getRoute: () => ({ path: [[1, 2]], exported: false, assert_target: null }),
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
