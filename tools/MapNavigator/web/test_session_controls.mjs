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
